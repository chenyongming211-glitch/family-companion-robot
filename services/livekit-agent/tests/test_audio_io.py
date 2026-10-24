import asyncio
import unittest
from types import SimpleNamespace

from livekit import rtc
from livekit.agents import Agent, AgentSession
from livekit.agents import StopResponse
from livekit.agents.voice.speech_handle import SpeechHandle

from family_voice.audio_io import PCMInput, PCMOutput
from family_voice import runtime


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
        self.assertEqual(self.sink.last_observation['sent_samples'], 480)
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

    async def test_count_unit_requires_complete_receipt_and_stays_out_of_history(self):
        play = getattr(runtime, 'play_count_unit', None)
        self.assertTrue(callable(play), 'counting needs SDK playback evidence, not LLM history')
        session = AgentSession(turn_handling={'interruption': {'resume_false_interruption': False}})
        session.output.audio = self.sink
        await session.start(Agent(instructions='count playback verification'), record=False)
        try:
            for receipt in ('full', 'partial', 'unknown'):
                first = self.sink.sequence
                job = asyncio.create_task(play(session, self.sink, 7, b'\1\0' * 960))
                async with asyncio.timeout(1):
                    while not any(e['type'] == 'audio.end' and e['block_id'] > first for e in self.events):
                        await asyncio.sleep(.001)
                block = self.sink.pending[-1]
                if receipt == 'full':
                    self.sink.receipt(block.id, block.epoch, block.sent)
                elif receipt == 'partial':
                    session.interrupt()
                    self.sink.receipt(block.id, block.epoch, 480)
                self.assertEqual(await asyncio.wait_for(job, 1), receipt == 'full')
            self.assertFalse(any('7' in getattr(item, 'text_content', '') for item in session.history.items))
        finally:
            await session.aclose()

    async def test_cancelled_count_unit_interrupts_sdk_playback(self):
        play = getattr(runtime, 'play_count_unit', None)
        self.assertTrue(callable(play))
        session = AgentSession(turn_handling={'interruption': {'resume_false_interruption': False}})
        session.output.audio = self.sink
        await session.start(Agent(instructions='count cancellation verification'), record=False)
        try:
            job = asyncio.create_task(play(session, self.sink, 7, b'\1\0' * 960))
            async with asyncio.timeout(1):
                while not any(e['type'] == 'audio.end' for e in self.events):
                    await asyncio.sleep(.001)
            job.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await job
            self.assertFalse(self.sink.pending, 'retired SDK playback must settle before the callback releases a replacement worker')
            async with asyncio.timeout(1):
                while not any(e['type'] == 'audio.cancel' for e in self.events):
                    await asyncio.sleep(.001)
        finally:
            await session.aclose()

    async def test_retired_resume_tool_cannot_reopen_counting_after_new_speech(self):
        calls = []
        owner = SimpleNamespace(closed=False, sleeping=False, user_epoch=0, session=SimpleNamespace(user_state='listening'), resume_count=calls.append)
        agent = runtime.AuditedAgent(None, owner)
        if not hasattr(agent, 'response_epochs'):
            agent.response_epochs = {}
        self.assertEqual(len(agent.tools), 1)
        tool = agent.tools[0]
        for closed, sleeping, interrupted, speaking in ((True, False, False, False), (False, True, False, False), (False, False, True, False), (False, False, False, True)):
            owner.closed, owner.sleeping = closed, sleeping
            owner.session.user_state = 'speaking' if speaking else 'listening'
            handle = SpeechHandle.create()
            agent.response_epochs[handle] = 0
            if interrupted:
                handle.interrupt(force=True)
            with self.assertRaises(StopResponse):
                await tool(SimpleNamespace(speech_handle=handle))
        self.assertEqual(calls, [])
        owner.closed = owner.sleeping = False
        owner.session.user_state = 'listening'
        owner.user_epoch = 1  # A short new onset has already returned to listening.
        old = SpeechHandle.create()
        agent.response_epochs[old] = 0
        with self.assertRaises(StopResponse):
            await tool(SimpleNamespace(speech_handle=old))
        self.assertEqual(calls, [], 'old tools cannot resume after a new user onset, even without SDK interruption')
        current = SpeechHandle.create()
        agent.response_epochs[current] = owner.user_epoch
        with self.assertRaises(StopResponse):
            await tool(SimpleNamespace(speech_handle=current))
        self.assertEqual(calls, ['tool'])

    async def test_count_unit_waits_for_preceding_sdk_speech_before_recording_identity(self):
        session = AgentSession(turn_handling={'interruption': {'resume_false_interruption': False}})
        session.output.audio = self.sink
        await session.start(Agent(instructions='前序语音检查'), record=False)
        gate = asyncio.Event()
        try:
            async def prefix():
                await gate.wait()
                yield frame()
            previous = session.say('前序说明', audio=prefix(), add_to_chat_ctx=False)
            async with asyncio.timeout(1):
                while session.current_speech is not previous:
                    await asyncio.sleep(.001)
            counting = asyncio.create_task(runtime.play_count_unit(session, self.sink, 7, b'\1\0' * 960))
            await asyncio.sleep(.01)
            self.assertEqual(self.sink.sequence, 0)
            gate.set()
            for ident in (1, 2):
                async with asyncio.timeout(1):
                    while not any(e['type'] == 'audio.end' and e['block_id'] == ident for e in self.events):
                        await asyncio.sleep(.001)
                block = self.sink.pending[-1]
                self.sink.receipt(block.id, block.epoch, block.sent)
            self.assertTrue(await asyncio.wait_for(counting, 1))
        finally:
            gate.set()
            await session.aclose()


if __name__ == '__main__':
    unittest.main()
