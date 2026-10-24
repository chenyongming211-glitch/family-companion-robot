import asyncio
import unittest

from livekit import rtc
from livekit.agents import Agent, AgentSession

try:
    from family_voice.drain_output import DrainOutput
except ModuleNotFoundError:
    DrainOutput = None


def frame():
    return rtc.AudioFrame(b'\1\0' * 480, 24000, 1, 480)


class DrainOutputTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.assertIsNotNone(DrainOutput, 'drain output is not implemented')
        self.events, self.settled = [], []
        async def send(event):
            self.events.append(event)
        self.sink = DrainOutput(send, window_samples=960, staged_samples=480, hard_samples=1440, receipt_timeout=.4)

    async def asyncTearDown(self):
        if hasattr(self, 'sink'):
            await self.sink.aclose()

    async def until(self, predicate, timeout=1):
        async with asyncio.timeout(timeout):
            while not predicate():
                await asyncio.sleep(.001)

    async def test_real_progress_releases_credit_and_invalid_identity_cannot(self):
        await self.sink.capture_frame(frame())
        await self.sink.capture_frame(frame())
        old = self.sink.active_identity
        blocked = asyncio.create_task(self.sink.capture_frame(frame()))
        await asyncio.sleep(.01)
        self.assertFalse(blocked.done())
        self.assertFalse(self.sink.receipt(old['block_id'], old['epoch'] + 1, 480, finished=False))
        self.assertFalse(blocked.done())
        self.assertTrue(self.sink.receipt(old['block_id'], old['epoch'], 480, finished=False))
        await asyncio.wait_for(blocked, .2)
        self.assertEqual(self.sink.outstanding_samples, 960)

    async def test_generation_cancel_seals_and_releases_sdk_but_preserves_real_confirmation(self):
        self.sink.bind_unit(1, 0, 960, self.settled.append, task_id='main:1')
        await self.sink.capture_frame(frame())
        await self.sink.capture_frame(frame())
        old = self.sink.active_identity
        handoff = next(e for e in self.events if e['type'] == 'output.handoff')
        self.assertEqual((handoff['content_id'], handoff['content_revision'], handoff['unit_index']), ('main:1', 1, 0))
        self.sink.flush()
        self.sink.clear_buffer()
        event = await asyncio.wait_for(self.sink.wait_for_playout(), .2)
        self.assertTrue(event.interrupted)
        self.assertEqual(self.settled, [])
        self.assertNotIn('audio.cancel', [e['type'] for e in self.events])
        self.assertTrue(any(e['type'] == 'output.drain' for e in self.events))
        self.assertTrue(self.sink.receipt(old['block_id'], old['epoch'], 960))
        self.assertEqual(self.settled, [True])
        self.assertTrue(self.sink.receipt(old['block_id'], old['epoch'], 960), 'final duplicate must be acknowledged idempotently')
        self.assertEqual(self.settled, [True])

    async def test_full_sent_prefix_is_not_complete_content_if_source_was_cut_short(self):
        self.sink.bind_unit(1, 0, 1440, self.settled.append)
        await self.sink.capture_frame(frame())
        old = self.sink.active_identity
        self.sink.clear_buffer()
        await self.sink.wait_for_playout()
        self.assertTrue(self.sink.receipt(old['block_id'], old['epoch'], 480))
        self.assertEqual(self.settled, [False])

    async def test_new_audio_stages_behind_old_and_cannot_claim_it_played_first(self):
        await self.sink.capture_frame(frame())
        old = self.sink.active_identity
        self.sink.clear_buffer()
        await self.sink.wait_for_playout()
        await self.sink.capture_frame(frame())
        new = self.sink.active_identity
        self.sink.flush()
        await self.until(lambda: any(e['type'] == 'audio.end' and e['block_id'] == new['block_id'] for e in self.events))
        handoff = next(e for e in self.events if e['type'] == 'output.handoff' and e['block_id'] == new['block_id'])
        self.assertIn(old['block_id'], handoff['after'])
        self.assertFalse(self.sink.receipt(new['block_id'], new['epoch'], 480))
        self.assertTrue(self.sink.receipt(old['block_id'], old['epoch'], 480))
        self.assertTrue(self.sink.receipt(new['block_id'], new['epoch'], 480))

    async def test_cancelled_staging_keeps_real_ledger_until_client_discard_receipt(self):
        await self.sink.capture_frame(frame())
        old = self.sink.active_identity
        self.sink.clear_buffer()
        await self.sink.wait_for_playout()
        await self.sink.capture_frame(frame())
        staged = self.sink.active_identity
        self.sink.clear_buffer()
        await self.sink.wait_for_playout()
        self.assertTrue(any(e['type'] == 'output.revoke' and staged['block_id'] in e['blocks'] for e in self.events))
        self.assertGreater(self.sink.outstanding_samples, 0)
        self.assertTrue(self.sink.receipt(staged['block_id'], staged['epoch'], 0, discarded=True))
        self.assertTrue(self.sink.receipt(old['block_id'], old['epoch'], 480))
        self.assertEqual(self.sink.outstanding_samples, 0)

    async def test_receipt_loss_becomes_failure_not_a_fake_playback_confirmation(self):
        self.sink.receipt_timeout = .025
        self.sink.bind_unit(1, 0, 480, self.settled.append)
        await self.sink.capture_frame(frame())
        self.sink.flush()
        await self.until(lambda: any(e['type'] == 'error' and e['code'] == 'playback_unknown' for e in self.events))
        self.assertEqual(self.settled, [False])
        with self.assertRaises(RuntimeError):
            await self.sink.capture_frame(frame())

    async def test_real_sdk_cancel_and_new_fixed_speech_do_not_wait_old_device_drain(self):
        session = AgentSession(turn_handling={'interruption': {'resume_false_interruption': False}})
        session.output.audio = self.sink
        await session.start(Agent(instructions='drain SDK test'), record=False)
        try:
            async def pcm():
                yield frame()
            self.sink.bind_unit(1, 0, 480, self.settled.append)
            first = session.say('原稿一句', audio=pcm(), add_to_chat_ctx=False)
            await self.until(lambda: any(e['type'] == 'audio.end' for e in self.events))
            old = next(e for e in self.events if e['type'] == 'audio.end')
            first.interrupt(force=True)
            await asyncio.wait_for(first.wait_for_playout(), .2)
            second = session.say('新回答', audio=pcm(), add_to_chat_ctx=False)
            await self.until(lambda: any(e['type'] == 'audio' and e['block_id'] != old['block_id'] for e in self.events))
            self.assertEqual(self.settled, [])
            new = next(e for e in self.events if e['type'] == 'audio' and e['block_id'] != old['block_id'])
            await self.until(lambda: any(e['type'] == 'audio.end' and e['block_id'] == new['block_id'] for e in self.events))
            self.sink.receipt(old['block_id'], old['epoch'], 480)
            self.sink.receipt(new['block_id'], new['epoch'], 480)
            await asyncio.wait_for(second.wait_for_playout(), .2)
            self.assertEqual(self.settled, [True])
            self.assertFalse(any(getattr(i, 'role', None) == 'assistant' for i in session.history.items))
        finally:
            await session.aclose()

    async def test_clear_fences_a_credit_blocked_old_writer_before_new_epoch(self):
        await self.sink.capture_frame(frame())
        await self.sink.capture_frame(frame())
        blocked = asyncio.create_task(self.sink.capture_frame(frame()))
        await asyncio.sleep(.01)
        self.sink.clear_buffer()
        with self.assertRaises(asyncio.CancelledError):
            await asyncio.wait_for(blocked, .2)
        await self.sink.wait_for_playout()
        end = next(i for i, e in enumerate(self.events) if e['type'] == 'audio.end')
        self.assertFalse(any(e['type'] == 'audio' and e['epoch'] == 0 for e in self.events[end + 1:]))

    async def test_clear_while_first_handoff_send_is_pending_cannot_use_cleared_active(self):
        entered, release = asyncio.Event(), asyncio.Event()
        async def send(event):
            self.events.append(event)
            if event['type'] == 'output.handoff':
                entered.set()
                await release.wait()
        self.sink.send = send
        capture = asyncio.create_task(self.sink.capture_frame(frame()))
        await entered.wait()
        self.sink.clear_buffer()
        release.set()
        with self.assertRaises(asyncio.CancelledError):
            await capture
        await self.sink.wait_for_playout()
        self.assertFalse(any(e['type'] == 'audio' for e in self.events))
        self.assertEqual(next(e for e in self.events if e['type'] == 'audio.end')['sent_samples'], 0)

    async def test_optional_notice_retired_during_handoff_cannot_admit_first_pcm(self):
        entered, release = asyncio.Event(), asyncio.Event()
        allowed = True
        async def send(event):
            self.events.append(event)
            if event['type'] == 'output.handoff':
                entered.set()
                await release.wait()
        self.sink.send = send
        self.sink.bind_unit(1, 0, 480, self.settled.append, task_id='progress:1', allow_start=lambda: allowed)
        capture = asyncio.create_task(self.sink.capture_frame(frame()))
        await entered.wait()
        allowed = False
        release.set()
        with self.assertRaises(asyncio.CancelledError):
            await capture
        self.assertFalse(any(e['type'] == 'audio' for e in self.events))
        self.sink.clear_buffer()
        await self.sink.wait_for_playout()
        self.assertEqual(next(e for e in self.events if e['type'] == 'audio.end')['sent_samples'], 0)
