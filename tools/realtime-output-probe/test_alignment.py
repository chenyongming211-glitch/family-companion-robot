import unittest
from alignment import PrefixTracker, select_prefix, validate_receipt


class AlignmentTests(unittest.TestCase):
    def setUp(self):
        self.sentence = {'sentence_end': True, 'text': '小兔发现小溪。', 'words': [
            {'text': '小兔', 'begin_time': 100, 'end_time': 400},
            {'text': '发现', 'begin_time': 400, 'end_time': 800},
            {'text': '小溪', 'begin_time': 800, 'end_time': 1200, 'punctuation': '。'},
        ]}

    def test_partial_word_does_not_become_spoken(self):
        result = select_prefix([self.sentence], '小兔发现小溪。后面有狐狸。', 700, margin_ms=100)
        self.assertEqual(result['prefix'], '小兔')
        self.assertEqual(result['word_end_ms'], 400)

    def test_unplayed_tail_is_never_restored(self):
        result = select_prefix([self.sentence], '小兔发现小溪。后面有狐狸。', 1350, margin_ms=100)
        self.assertEqual(result['prefix'], '小兔发现小溪。')
        self.assertNotIn('狐狸', result['prefix'])

    def test_mismatched_transcript_stays_unknown(self):
        self.assertEqual(select_prefix([self.sentence], '小猫在吃饭。', 1500)['status'], 'unknown')

    def test_interim_words_are_not_stable_evidence(self):
        self.sentence['sentence_end'] = False
        self.assertEqual(select_prefix([self.sentence], '小兔发现小溪。', 1500)['status'], 'unknown')

    def test_backward_timestamps_do_not_create_prefix(self):
        self.sentence['words'][1]['begin_time'] = 50
        self.assertEqual(select_prefix([self.sentence], '小兔发现小溪。', 1500)['status'], 'unknown')

    def test_receipt_cannot_exceed_scheduled_or_change_response(self):
        valid = {'type': 'cut', 'id': 'one', 'basis': 'CoreAudio_dataPlayedBack',
                 'completed_samples': 4320, 'accepted_samples': 5760, 'microphone': False}
        self.assertEqual(validate_receipt(valid, 'one', 6000), 180)
        for field, value in [('id', 'other'), ('completed_samples', 9000), ('basis', 'i2s_dma'),
                             ('completed_samples', True), ('microphone', True)]:
            invalid = dict(valid, **{field: value})
            with self.assertRaises(ValueError):
                validate_receipt(invalid, 'one', 6000)

    def test_stable_prefix_is_ready_without_waiting_for_later_sentences(self):
        tracker = PrefixTracker('小兔发现小溪。后面有狐狸。', 5000)
        tracker.add(dict(self.sentence, sentence_id=1), 737)
        self.assertTrue(tracker.covers(700))
        self.assertEqual(tracker.candidate(700)['prefix'], '小兔')
        self.assertEqual(tracker.first_usable_ms(700), 737)
        self.assertFalse(tracker.covers(3000))

    def test_missing_first_sentence_cannot_be_used_as_beginning(self):
        tracker = PrefixTracker('小兔发现小溪。', 5000)
        tracker.add(dict(self.sentence, sentence_id=2), 100)
        self.assertFalse(tracker.covers(700))
        self.assertEqual(tracker.candidate(700)['status'], 'unknown')


if __name__ == '__main__':
    unittest.main()
