import asyncio
import unittest

from livekit import rtc
from livekit.agents import Agent, AgentSession

from family_voice.audio_io import PCMInput, PCMOutput


def frame():
    return rtc.AudioFrame(b'\0' * 960, 24000, 1, 480)


class AudioTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.events = []
        async def send(event):
            self.events.append(event)
        self.sink = PCMOutput(send, settle_timeout=.02, normal_slack=.04)

    async def asyncTearDown(self):
        await self.sink.aclose()

    async def test_seal_requires_matching_receipt_and_old_receipt_cannot_complete_new(self):
        await self.sink.capture_frame(frame())
        first = self.sink.active_identity
        self.sink.flush()
        waiter = asyncio.create_task(self.sink.wait_for_playout())
        await asyncio.sleep(0)
        self.assertFalse(waiter.done())
        self.assertFalse(self.sink.receipt(first['block_id'], first['epoch'] + 1, 480))
        self.assertTrue(self.sink.receipt(first['block_id'], first['epoch'], 480))
        self.assertFalse((await waiter).interrupted)
        await self.sink.capture_frame(frame())
        second = self.sink.active_identity
        self.sink.flush()
        waiter = asyncio.create_task(self.sink.wait_for_playout())
        self.assertFalse(self.sink.receipt(first['block_id'], first['epoch'], 480))
        self.assertFalse(waiter.done())
        self.assertTrue(self.sink.receipt(second['block_id'], second['epoch'], 480))
        await waiter

    async def test_cancel_allows_new_output_while_old_prefix_settles(self):
        await self.sink.capture_frame(frame())
        await self.sink.capture_frame(frame())
        old = self.sink.active_identity
        self.sink.clear_buffer()
        await self.sink.capture_frame(frame())
        new = self.sink.active_identity
        self.assertNotEqual(old['epoch'], new['epoch'])
        self.sink.flush()
        self.assertTrue(self.sink.receipt(new['block_id'], new['epoch'], 480))
        self.assertTrue(self.sink.receipt(old['block_id'], old['epoch'], 480))
        result = await asyncio.wait_for(self.sink.wait_for_playout(), .3)
        self.assertFalse(result.interrupted)
        self.assertEqual(self.sink.last_observation['block_id'], new['block_id'])

    async def test_invalid_and_missing_receipts_unlock_sdk_as_unknown(self):
        for invalid in (False, True):
            await self.sink.capture_frame(frame())
            ident = self.sink.active_identity
            self.sink.flush()
            if invalid:
                self.assertFalse(self.sink.receipt(ident['block_id'], ident['epoch'], 0, valid=False))
            result = await asyncio.wait_for(self.sink.wait_for_playout(), .3)
            self.assertTrue(result.interrupted)
            self.assertFalse(self.sink.last_observation['valid'])
            self.assertFalse(self.sink.receipt(ident['block_id'], ident['epoch'], 480))

    async def test_input_is_bounded_and_rejects_malformed_audio(self):
        source = PCMInput(capacity=1)
        source.push(b'\0' * 640)
        with self.assertRaises(BufferError):
            source.push(b'\0' * 640)
        self.assertEqual((await source.__anext__()).samples_per_channel, 320)
        with self.assertRaises(ValueError):
            source.push(b'\0')
        source.close()
        with self.assertRaises(StopAsyncIteration):
            await source.__anext__()

    async def test_invalid_progress_does_not_finish_an_unsealed_sdk_segment(self):
        await self.sink.capture_frame(frame())
        ident = self.sink.active_identity
        self.assertFalse(self.sink.receipt(ident['block_id'], ident['epoch'], 0, valid=False, finished=False))
        self.assertIsNone(self.sink.last_observation)
        self.sink.flush()
        result = await asyncio.wait_for(self.sink.wait_for_playout(), .3)
        self.assertTrue(result.interrupted)
        self.assertFalse(self.sink.last_observation['valid'])

    async def test_real_session_early_cancel_and_fixed_text_excluded_from_history(self):
        session = AgentSession(turn_handling={'interruption': {'resume_false_interruption': False}})
        session.output.audio = self.sink
        await session.start(Agent(instructions='SDK lifecycle verification'), record=False)
        try:
            gate = asyncio.Event()
            async def held_audio():
                await gate.wait()
                yield frame()
            session.say('尚未开始', audio=held_audio(), add_to_chat_ctx=False)
            await asyncio.sleep(.01)
            await asyncio.wait_for(session.interrupt(), 1)
            async def audio():
                yield frame()
            speech = session.say('固定原稿', audio=audio(), add_to_chat_ctx=False)
            await asyncio.wait_for(speech.wait_for_playout(), 1)
            self.assertNotIn('固定原稿', [getattr(item, 'text_content', '') for item in session.history.items])
            await asyncio.wait_for(session.interrupt(), 1)
        finally:
            await session.aclose()


if __name__ == '__main__':
    unittest.main()
