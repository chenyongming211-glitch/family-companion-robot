import asyncio
import base64
import unittest

from family_voice.smoke import SimulatedPlayer, answer_matches


class SmokeTests(unittest.IsolatedAsyncioTestCase):
    async def test_cancelled_generation_is_never_played_into_new_output(self):
        receipts = []
        async def send(event):
            receipts.append(event)
        player = SimulatedPlayer(send)
        packet = {'type': 'audio', 'epoch': 0, 'block_id': 1, 'samples': 480, 'data': base64.b64encode(b'\0' * 960).decode()}
        await player.event(packet)
        await player.event({'type': 'audio.cancel', 'epoch': 0, 'next_epoch': 1})
        await player.event(packet)
        self.assertEqual(player.dropped_old_frames, 1)
        self.assertEqual(player.played_samples, 0)
        await player.event({**packet, 'epoch': 1, 'block_id': 2})
        await player.event({'type': 'audio.end', 'epoch': 1, 'block_id': 2, 'sent_samples': 480})
        await asyncio.wait_for(player.drain(), .5)
        self.assertEqual(player.played_samples, 480)
        self.assertTrue(any(r['block_id'] == 2 and r['played_samples'] == 480 for r in receipts))
        await player.close()

    async def test_math_verdict_accepts_equivalent_notation_but_not_negation(self):
        self.assertTrue(answer_matches('一加一等于二。', 2))
        self.assertTrue(answer_matches('5', 5))
        self.assertFalse(answer_matches('一加一不等于二。', 2))
        self.assertFalse(answer_matches('不是5，是6。', 5))


if __name__ == '__main__':
    unittest.main()
