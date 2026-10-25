import unittest

from state import Turn
from guard import verdict, guarded_stream


def source():
    turn = Turn(1)
    turn.user = '念一段编号'
    turn.generated = '一、二、三、四、五、六、七、八、九、十。'
    turn.sent = turn.completed = 7200 * 24
    turn.cut = True
    turn.tracker.add({'sentence_id': 1, 'sentence_end': True, 'words': [
        {'text': '9', 'begin_time': 100, 'end_time': 500},
        {'text': ' ', 'begin_time': 500, 'end_time': 600},
        {'text': '10', 'begin_time': 600, 'end_time': 1000}]}, 1000)
    return turn


class Native:
    def __init__(self, text):
        self.text, self.opened, self.closed = text, False, False

    async def stream(self, pcm):
        yield 'pcm', b'\0' * 2880
        yield 'text', self.text

    async def open(self, instructions):
        self.opened = True

    async def close(self):
        self.closed = True


class GuardTests(unittest.IsolatedAsyncioTestCase):
    def test_unqualified_stop_is_rejected_for_conservative_evidence(self):
        self.assertFalse(verdict([source()], '刚才数到十了。')['allowed'])

    def test_hedged_supported_anchor_is_allowed(self):
        self.assertTrue(verdict([source()], '记录显示大概数到十附近。')['allowed'])

    def test_hedge_cannot_authorize_unsupported_number(self):
        self.assertFalse(verdict([source()], '大概数到五附近。')['allowed'])
        self.assertFalse(verdict([source()], '大概数到了五附近。')['allowed'])

    def test_no_record_claim_is_rejected_when_observations_exist(self):
        self.assertFalse(verdict([source()], '我这边没有记录。')['allowed'])

    def test_story_position_also_requires_qualification(self):
        self.assertFalse(verdict([source()], '刚才讲到森林了。')['allowed'])
        self.assertTrue(verdict([source()], '大概讲到森林附近。')['allowed'])

    def test_coarse_story_opening_is_not_an_exact_stop_claim(self):
        turn = source()
        turn.generated = '清晨的薄雾还没散尽，小兔走出家门。'
        turn.tracker.sentences[1]['words'] = [
            {'text': '清晨的薄雾还没散尽小兔', 'begin_time': 100, 'end_time': 1000}]
        self.assertTrue(verdict([turn], '刚才的故事讲到了清晨薄雾和小兔出场的开头。')['allowed'])

    def test_explicit_unfinished_fragment_qualifies_story_boundary(self):
        self.assertTrue(verdict([source()], '刚才讲到小兔的住处，后面还没说完。')['allowed'])

    def test_guard_uses_same_recent_window_as_model_context(self):
        old = source()
        old.cut = False
        old.ended = True
        old.generated = '数到五十。'
        recent = [Turn(i) for i in range(2, 18)]
        self.assertFalse(verdict([old, *recent], '大概数到五十附近。')['allowed'])

    def test_last_asr_word_survives_digit_run_without_separators(self):
        turn = source()
        turn.tracker.sentences[1]['words'] = [
            {'text': str(i), 'begin_time': i * 100, 'end_time': (i + 1) * 100}
            for i in (1, 2, 3)]
        self.assertTrue(verdict([turn], '记录显示大概念到三附近。')['allowed'])

    async def test_rejected_draft_pcm_is_not_released_repair_is_model_generated(self):
        first, repair = Native('刚才数到五了。'), Native('记录里至少能确认数到十附近。')
        events, rejected = [], []
        async for event in guarded_stream(first, lambda: repair, 'instructions', b'input',
                lambda: '刚才数到几了？', [source()], rejected.append):
            events.append(event)
        self.assertEqual(events[0], ('text', repair.text))
        self.assertEqual(sum(len(value) for kind, value in events if kind == 'pcm'), 2880)
        self.assertEqual(len(rejected), 1)
        self.assertTrue(repair.opened and repair.closed)

    async def test_normal_chat_remains_streaming(self):
        events = []
        async for event in guarded_stream(Native('五'), lambda: None, '', b'input',
                lambda: '二加三等于几', [source()], lambda _: None):
            events.append(event)
        self.assertEqual(events[0][0], 'pcm')

    async def test_second_bad_answer_raises_without_emitting_audio(self):
        events = []
        with self.assertRaisesRegex(ValueError, 'recall_grounding_unconfirmed'):
            async for event in guarded_stream(Native('刚才数到五了。'), lambda: Native('大概数到五附近。'),
                    '', b'input', lambda: '刚才数到几了？', [source()], lambda _: None):
                events.append(event)
        self.assertEqual(events, [])


if __name__ == '__main__':
    unittest.main()
