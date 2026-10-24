import unittest

from state import AudioRing, Turn, context_for


def sentence(text, end=1000):
    return {'sentence_id': 1, 'sentence_end': True, 'words': [
        {'text': text, 'begin_time': 0, 'end_time': end}]}


class StateTests(unittest.TestCase):
    def test_audio_ring_uses_sample_indices_and_refuses_evicted_input(self):
        ring = AudioRing(capacity=16)
        ring.append(bytes(range(24)))
        self.assertEqual(ring.slice(2, 6), bytes(range(4, 12)))
        ring.append(bytes(range(24, 40)))
        self.assertIsNone(ring.slice(0, 5))
        self.assertEqual(ring.slice(10, 20), bytes(range(20, 40)))

    def test_window_requires_completed_playback_and_rejects_stale_receipt(self):
        turn = Turn(2)
        for _ in range(5):
            self.assertTrue(turn.can_send())
            turn.sent += 1440
        self.assertFalse(turn.can_send())
        self.assertFalse(turn.receipt({'id': 1, 'completed': 7200, 'accepted': 7200, 'basis': 'CoreAudio_dataPlayedBack'}))
        self.assertFalse(turn.can_send())
        self.assertTrue(turn.receipt({'id': 2, 'completed': 1440, 'accepted': 7200, 'basis': 'CoreAudio_dataPlayedBack'}))
        self.assertTrue(turn.can_send())
        self.assertFalse(turn.receipt({'id': 2, 'completed': 0, 'accepted': 7200, 'basis': 'CoreAudio_dataPlayedBack'}))

    def test_receipt_cannot_claim_unsent_or_partial_packet(self):
        turn = Turn(1)
        turn.sent = 1440
        for completed, accepted in ((2880, 2880), (1, 1440), (1440, 0), (True, 1440)):
            self.assertFalse(turn.receipt({'id': 1, 'completed': completed, 'accepted': accepted, 'basis': 'CoreAudio_dataPlayedBack'}))
        self.assertEqual(turn.completed, 0)

    def test_boolean_cannot_masquerade_as_response_identity(self):
        turn = Turn(1)
        turn.sent = 1440
        self.assertFalse(turn.receipt({'id': True, 'completed': 1440, 'accepted': 1440,
            'basis': 'CoreAudio_dataPlayedBack'}))

    def test_unplayed_draft_is_excluded_from_context(self):
        turn = Turn(1)
        turn.user = '讲森林故事'
        turn.generated = '小兔进森林。后面有狐狸。'
        turn.sent, turn.completed = 48000, 28800
        turn.tracker.total_audio_ms = 2000
        turn.tracker.add(sentence('小兔进森林'), 1100)
        value = context_for([turn])
        self.assertIn('小兔进森林。', value)
        self.assertNotIn('狐狸', value)

    def test_mismatch_is_unknown_without_generated_tail(self):
        turn = Turn(1)
        turn.user, turn.generated = '讲故事', '未播的秘密'
        turn.sent, turn.completed = 48000, 28800
        turn.tracker.total_audio_ms = 2000
        turn.tracker.add(sentence('识别错误'), 1100)
        value = context_for([turn])
        self.assertIn('unknown', value)
        self.assertNotIn('未播的秘密', value)


if __name__ == '__main__':
    unittest.main()
