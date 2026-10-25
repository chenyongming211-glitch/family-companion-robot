"""Synthetic public examples; real conversation text stays on the server."""
import json
import unittest

from state import Turn, context_for


def add(turn, words, identity=1):
    turn.tracker.add({'sentence_id': identity, 'sentence_end': True,
        'words': [{'text': text, 'begin_time': start, 'end_time': end}
            for text, start, end in words]}, 1000)


def entry(turn):
    return json.loads(context_for([turn]))['recent_turns'][0]


class HistoryTests(unittest.TestCase):
    def interrupted(self, draft='小兔走进森林。后面有狐狸。', ms=2400):
        turn = Turn(1)
        turn.generated = draft
        turn.sent = turn.completed = ms * 24
        turn.cut = True
        return turn

    def test_format_difference_preserves_observation_and_aligns_safe_cardinal(self):
        turn = self.interrupted('刚才念到十七。接下来十八。')
        add(turn, [('刚才念到', 100, 500), ('17', 500, 1000)])
        item = entry(turn)
        self.assertEqual(item['assistant_spoken_prefix'], '刚才念到十七。')
        self.assertEqual(item['playback_evidence']['observed_text'], '刚才念到17')
        self.assertEqual(item['playback_evidence']['alignment'], 'equivalent_prefix')
        self.assertNotIn('十八', json.dumps(item, ensure_ascii=False))

    def test_mismatched_words_remain_as_source_labelled_asr_not_original_script(self):
        turn = self.interrupted()
        add(turn, [('小鹿', 100, 500), ('走进森林', 500, 1300)])
        evidence = entry(turn)['playback_evidence']
        self.assertEqual(evidence['observed_text'], '小鹿走进森林')
        self.assertEqual(evidence['text_source'], 'output_asr')
        self.assertEqual(evidence['alignment'], 'unmatched')
        self.assertFalse(evidence['exact_stop_known'])
        self.assertNotIn('狐狸', json.dumps(evidence, ensure_ascii=False))

    def test_contiguous_recognition_survives_later_missing_sentence(self):
        turn = self.interrupted()
        add(turn, [('小兔走进森林', 100, 900)])
        add(turn, [('后面有狐狸', 1000, 1500)], identity=3)
        evidence = entry(turn)['playback_evidence']
        self.assertEqual(evidence['observed_text'], '小兔走进森林')
        self.assertEqual(evidence['boundary'], 'recognition_gap')
        self.assertNotIn('狐狸', json.dumps(evidence, ensure_ascii=False))

    def test_invalid_later_word_does_not_erase_previous_good_words(self):
        turn = self.interrupted()
        add(turn, [('小兔', 100, 500), ('走进', 400, 900), ('森林', 900, 1300)])
        evidence = entry(turn)['playback_evidence']
        self.assertEqual(evidence['observed_text'], '小兔')
        self.assertEqual(evidence['boundary'], 'invalid_timing')

    def test_partial_and_unplayed_words_never_enter_observed_text(self):
        turn = self.interrupted(ms=1200)
        turn.sent = 2400 * 24
        add(turn, [('小兔', 100, 500), ('走进', 500, 1120), ('森林', 1120, 1800)])
        evidence = entry(turn)['playback_evidence']
        self.assertEqual(evidence['observed_text'], '小兔')
        self.assertEqual(evidence['boundary'], 'playback_guard')
        self.assertEqual(evidence['completed_ms'], 1200)
        self.assertNotIn('走进', json.dumps(evidence, ensure_ascii=False))

    def test_complete_normal_output_keeps_native_transcript_without_asr(self):
        turn = Turn(1)
        turn.generated = '小兔走进森林。'
        turn.sent = turn.completed = turn.accepted = 2400 * 24
        turn.ended = True
        evidence = entry(turn)['playback_evidence']
        self.assertEqual(evidence['spoken_text'], turn.generated)
        self.assertEqual(evidence['text_source'], 'native_transcript_full_playback')
        self.assertEqual(evidence['boundary'], 'complete_response')

    def test_complete_counter_without_normal_generation_end_cannot_expose_draft(self):
        turn = self.interrupted()
        evidence = entry(turn)['playback_evidence']
        self.assertIsNone(evidence['spoken_text'])
        self.assertEqual(evidence['boundary'], 'no_recognition')
        self.assertNotIn('狐狸', json.dumps(evidence, ensure_ascii=False))

    def test_digit_sequence_does_not_equal_single_cardinal(self):
        turn = self.interrupted('一、二。后面有狐狸。')
        add(turn, [('12', 100, 500)])
        evidence = entry(turn)['playback_evidence']
        self.assertEqual(evidence['alignment'], 'unmatched')
        self.assertEqual(evidence['observed_text'], '12')

    def test_ambiguous_colloquial_cardinal_is_not_normalized_as_exact(self):
        turn = self.interrupted('二百五。后面有狐狸。')
        add(turn, [('205', 100, 500)])
        self.assertEqual(entry(turn)['playback_evidence']['alignment'], 'unmatched')

    def test_unplayed_or_unstable_asr_does_not_create_heard_content(self):
        turn = self.interrupted()
        turn.completed = 0
        add(turn, [('森林', 100, 500)])
        self.assertIsNone(entry(turn)['playback_evidence']['spoken_text'])
        turn.completed = turn.sent
        turn.tracker.sentences[1]['sentence_end'] = False
        self.assertIsNone(entry(turn)['playback_evidence']['spoken_text'])

    def test_unknown_boundary_is_not_replaced_by_target_or_earlier_self_report(self):
        turn = self.interrupted('一、二、三、四、五、六。')
        turn.user = '请数到一百'
        add(turn, [('1', 100, 400), ('2', 400, 700), ('3', 700, 1000)])
        item = entry(turn)
        self.assertFalse(item['playback_evidence']['exact_stop_known'])
        self.assertEqual(item['playback_evidence']['claim_scope'], 'this_response_only')
        self.assertEqual(item['playback_evidence']['observed_text'], '123')

    def test_recall_answer_is_retained_but_not_in_source_playback_index(self):
        source = self.interrupted('小兔走进森林。后面有狐狸。')
        source.user = '讲个小兔的故事'
        add(source, [('小兔走进森林', 100, 1000)])
        claim = Turn(2)
        claim.user = '刚才的故事讲到哪里了？'
        claim.generated = '刚才讲到狐狸了。'
        claim.sent = claim.completed = 2400 * 24
        claim.ended = True
        context = json.loads(context_for([source, claim]))
        self.assertEqual([e['id'] for e in context['source_playback_records']], [1])
        self.assertEqual(context['recent_turns'][1]['assistant_spoken_prefix'], claim.generated)
        self.assertEqual(context['recent_turns'][1]['record_role'], 'reference_answer_not_source_progress')

    def test_continuation_request_remains_source_content(self):
        turn = self.interrupted()
        turn.user = '继续刚才讲到的地方'
        context = json.loads(context_for([turn]))
        self.assertEqual([item['id'] for item in context['source_playback_records']], [1])


if __name__ == '__main__':
    unittest.main()
