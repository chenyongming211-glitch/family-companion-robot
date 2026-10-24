import asyncio
import unittest

from livekit import rtc
from device_sink_probe import ReceiptProbe


def frame():
    return rtc.AudioFrame(
        data=b"\0" * 960,
        sample_rate=24000,
        num_channels=1,
        samples_per_channel=480,
    )


class ReceiptTests(unittest.IsolatedAsyncioTestCase):
    async def test_flush_waits_for_matching_device_finish(self):
        sink = ReceiptProbe()
        await sink.capture_frame(frame())
        await sink.capture_frame(frame())
        block = sink.block_id
        sink.flush()
        waiter = asyncio.create_task(sink.wait_for_playout())
        await asyncio.sleep(0)
        self.assertFalse(waiter.done())
        self.assertFalse(sink.receipt(block + 1, 960))
        self.assertFalse(sink.receipt(block, 1440))
        self.assertFalse(sink.receipt(block, 480))
        self.assertFalse(waiter.done())
        self.assertTrue(sink.receipt(block, 960))
        result = await asyncio.wait_for(waiter, 0.2)
        self.assertAlmostEqual(result.playback_position, 0.04)
        self.assertFalse(result.interrupted)
        self.assertIsNone(result.synchronized_transcript)

    async def test_clear_uses_partial_finish_and_old_id_cannot_finish_new(self):
        sink = ReceiptProbe()
        await sink.capture_frame(frame())
        await sink.capture_frame(frame())
        old = sink.block_id
        sink.clear_buffer()
        waiter = asyncio.create_task(sink.wait_for_playout())
        await asyncio.sleep(0)
        self.assertFalse(waiter.done())
        self.assertTrue(sink.receipt(old, 480))
        result = await asyncio.wait_for(waiter, 0.2)
        self.assertTrue(result.interrupted)
        self.assertAlmostEqual(result.playback_position, 0.02)
        await sink.capture_frame(frame())
        new = sink.block_id
        self.assertNotEqual(new, old)
        sink.flush()
        waiter = asyncio.create_task(sink.wait_for_playout())
        await asyncio.sleep(0)
        self.assertFalse(sink.receipt(old, 480))
        self.assertFalse(waiter.done())
        self.assertTrue(sink.receipt(new, 480))
        await asyncio.wait_for(waiter, 0.2)

    async def test_invalid_receipt_cannot_be_upgraded_to_measured_completion(self):
        sink = ReceiptProbe()
        await sink.capture_frame(frame())
        block = sink.block_id
        sink.flush()
        waiter = asyncio.create_task(sink.wait_for_playout())
        await asyncio.sleep(0)
        self.assertFalse(sink.receipt(block, 0, valid=False))
        self.assertFalse(sink.receipt(block, 480, valid=True))
        self.assertFalse(waiter.done())
        waiter.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await waiter


if __name__ == "__main__":
    unittest.main()
