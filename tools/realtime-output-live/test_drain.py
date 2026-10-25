"""Playback handoff tests, without microphone, providers or billable calls."""
import asyncio
import tempfile
import unittest
from pathlib import Path

from server import Session
from state import Turn


def receipt(turn, completed, terminal='progress', accepted=None):
    return {'id': turn.id, 'completed': completed,
        'accepted': completed if accepted is None else accepted,
        'basis': 'CoreAudio_dataPlayedBack', 'terminal': terminal}


class Socket:
    def __init__(self):
        self.events = []
        self.closed = False

    async def send_json(self, event):
        self.events.append(event)

    async def send_bytes(self, packet):
        self.events.append(packet)

    async def close(self, **_):
        self.closed = True


class DrainTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.socket = Socket()
        self.session = Session(self.socket, {}, Path(self.directory.name))
        self.old = Turn(1)
        self.old.begun = True
        self.old.sent = 7200
        self.old.completed = 2880
        self.session.history.append(self.old)
        self.session.output = self.old

    async def asyncTearDown(self):
        for task in list(self.session.tasks):
            task.cancel()
        await asyncio.gather(*self.session.tasks, return_exceptions=True)
        self.session.input_codec.close()
        self.directory.cleanup()

    async def test_interrupt_seals_exact_sent_boundary_without_clear(self):
        await self.session.interrupt()
        self.assertEqual(self.socket.events, [{'type': 'drain', 'id': 1, 'sent': 7200}])
        self.assertEqual(self.old.drain_target, 7200)
        self.assertFalse(self.old.drain_done.is_set())
        self.assertFalse(await self.session.binary(self.old, b'late'))
        self.assertEqual(self.old.sent, 7200)
        await self.session.interrupt()
        self.assertEqual(len(self.socket.events), 1)

    async def test_context_waits_for_full_drain_receipt_not_just_equal_counters(self):
        await self.session.interrupt()
        new = Turn(2)
        self.session.history.append(new)
        settling = asyncio.create_task(self.session.settle(new))
        await asyncio.sleep(.01)
        self.assertFalse(settling.done())
        self.assertTrue(self.old.receipt(receipt(self.old, 7200)))
        await asyncio.sleep(.01)
        self.assertFalse(settling.done())
        self.assertFalse(self.old.receipt(receipt(self.old, 5760, 'drained', 7200)))
        self.assertTrue(self.old.receipt(receipt(self.old, 7200, 'drained')))
        await asyncio.wait_for(settling, .2)

    async def test_inflight_send_finishes_before_seal_and_is_included(self):
        entered, release = asyncio.Event(), asyncio.Event()
        async def blocked(packet):
            entered.set()
            await release.wait()
            self.socket.events.append(packet)
        self.socket.send_bytes = blocked
        sending = asyncio.create_task(self.session.binary(self.old, b'packet'))
        await asyncio.wait_for(entered.wait(), .2)
        cutting = asyncio.create_task(self.session.interrupt())
        await asyncio.sleep(.01)
        self.assertFalse(cutting.done())
        release.set()
        self.assertTrue(await sending)
        await cutting
        self.assertEqual(self.socket.events[-1], {'type': 'drain', 'id': 1, 'sent': 8640})

    async def test_missing_drain_receipt_closes_instead_of_releasing_new_audio(self):
        self.session.drain_timeout = .03
        await self.session.interrupt()
        await asyncio.sleep(.07)
        self.assertTrue(self.socket.closed)
        self.assertFalse(self.old.drain_done.is_set())
        self.assertIn({'type': 'status', 'code': 'drain_failed'}, self.socket.events)
        self.assertFalse(any(e.get('type') == 'begin' for e in self.socket.events if isinstance(e, dict)))

    async def test_unsent_cancel_needs_no_client_receipt(self):
        self.old.begun = False
        self.old.sent = self.old.completed = 0
        await self.session.interrupt()
        self.assertEqual(self.socket.events, [])
        self.assertTrue(self.old.drain_done.is_set())

    async def test_old_completion_cannot_release_another_drain(self):
        await self.session.interrupt()
        other = Turn(2)
        other.sent = 1440
        other.seal_for_drain()
        self.assertTrue(self.old.receipt(receipt(self.old, 7200, 'drained')))
        self.assertFalse(other.receipt(receipt(self.old, 7200, 'drained')))
        self.assertFalse(other.drain_done.is_set())

    async def test_second_utterance_cancels_waiting_reply_but_retains_first_drain(self):
        await self.session.interrupt()
        waiting = Turn(2)
        self.session.history.append(waiting)
        self.session.output = waiting
        task = self.session.reply_task = self.session.spawn(self.session.settle(waiting))
        await asyncio.sleep(.01)
        await self.session.interrupt()
        await asyncio.gather(task, return_exceptions=True)
        self.assertTrue(task.cancelled())
        newest = Turn(3)
        self.session.history.append(newest)
        settling = asyncio.create_task(self.session.settle(newest))
        await asyncio.sleep(.01)
        self.assertFalse(settling.done())
        self.assertTrue(self.old.receipt(receipt(self.old, 7200, 'drained')))
        await asyncio.wait_for(settling, .2)
        self.assertEqual(self.socket.events, [{'type': 'drain', 'id': 1, 'sent': 7200}])

    async def test_normal_end_racing_interrupt_still_requires_seal_ack(self):
        self.old.ended = True
        await self.session.interrupt()
        self.assertTrue(self.old.receipt(receipt(self.old, 7200, 'played')))
        self.assertFalse(self.old.drain_done.is_set())
        self.assertTrue(self.old.receipt(receipt(self.old, 7200, 'drained')))
        self.assertTrue(self.old.drain_done.is_set())


if __name__ == '__main__':
    unittest.main()
