import asyncio
import json
from pathlib import Path
import tempfile
import unittest
from types import SimpleNamespace

try:
    from family_voice.confirmation import QuickConfirmation, parse_confirmation
except ImportError:
    QuickConfirmation = parse_confirmation = None


class ConfirmationParsingTests(unittest.TestCase):
    def parse(self, value, finish='stop', called=False):
        self.assertIsNotNone(parse_confirmation, 'quick confirmation parser is missing')
        return parse_confirmation(value, finish, called)

    def test_only_complete_short_single_sentence_json_can_speak(self):
        self.assertEqual(self.parse(json.dumps({'text': '我来讲个小故事。'})), '我来讲个小故事。')
        self.assertIsNone(self.parse('{"text":""}'))
        for raw, finish, called in (
                ('{"text":"我来看看。"}', 'length', False),
                ('{"text":"我来看看。"}', None, False),
                ('{"text":"我来看看。"}', 'stop', True),
                ('{"text":"还没完整', 'stop', False),
                (json.dumps({'text': '先说一句。再说一句。'}), 'stop', False),
                (json.dumps({'text': '长' * 21}), 'stop', False),
                ('{"text":"。"}', 'stop', False),
                ('{"text":"我来看看。","extra":1}', 'stop', False)):
            with self.subTest(raw=raw):
                self.assertIsNone(self.parse(raw, finish, called))


class ConfirmationTaskTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.assertIsNotNone(QuickConfirmation, 'quick model task is missing')
        self.directory = tempfile.TemporaryDirectory()
        self.delivered, self.expired = [], []
        self.valid = True
        self.quick = QuickConfirmation(SimpleNamespace(audit_dir=Path(self.directory.name)),
            lambda text, epoch, turn, deadline: self.delivered.append((text, epoch, turn)) or True,
            lambda epoch, turn: self.valid, lambda epoch, turn: self.expired.append((epoch, turn)),
            model=SimpleNamespace(aclose=self.model_close), budget=.05)
        self.closed = False

    async def model_close(self):
        self.closed = True

    async def asyncTearDown(self):
        await self.quick.aclose()
        self.directory.cleanup()

    async def test_start_is_parallel_and_deadline_expires_pending_notice(self):
        entered, release = asyncio.Event(), asyncio.Event()
        async def generate(*args):
            entered.set()
            await release.wait()
            return '我来讲个小故事。'
        self.quick.generate = generate
        self.quick.start('讲个故事', {}, 1, 'turn1')
        await entered.wait()
        self.assertEqual(self.delivered, [])
        release.set()
        await asyncio.sleep(.08)
        self.assertEqual(self.delivered, [('我来讲个小故事。', 1, 'turn1')])
        self.assertEqual(self.expired, [(1, 'turn1')])

    async def test_cancelled_or_expired_generation_never_delivers(self):
        entered = asyncio.Event()
        async def generate(*args):
            entered.set()
            await asyncio.Event().wait()
        self.quick.generate = generate
        self.quick.start('讲个故事', {}, 1, 'turn1')
        await entered.wait()
        self.quick.cancel()
        await asyncio.sleep(.07)
        self.assertEqual(self.delivered, [])
        self.quick.start('讲个故事', {}, 2, 'turn2')
        await asyncio.sleep(.08)
        self.assertEqual(self.delivered, [])
        await self.quick.aclose()
        self.assertTrue(self.closed)

    async def test_retiring_jobs_are_bounded_and_new_turn_can_recover_capacity(self):
        release = asyncio.Event()
        started = []
        async def generate(*args):
            started.append(args)
            try:
                await release.wait()
            except asyncio.CancelledError:
                await release.wait()
            return '这就开始讲。'
        self.quick.generate = generate
        for index in range(10):
            self.quick.start('讲个故事', {}, index, str(index))
            await asyncio.sleep(0)
        self.assertLessEqual(len(self.quick._jobs), 4)
        self.assertLessEqual(len(started), 4)
        release.set()
        await asyncio.sleep(.01)
        self.assertEqual(self.delivered, [])
        self.quick.start('讲个故事', {}, 11, 'new')
        await asyncio.sleep(.01)
        self.assertEqual(self.delivered, [('这就开始讲。', 11, 'new')])

    async def test_stale_generation_cannot_deliver_even_if_cancellation_is_ignored(self):
        entered, release = asyncio.Event(), asyncio.Event()
        async def generate(*args):
            entered.set()
            try:
                await release.wait()
            except asyncio.CancelledError:
                await release.wait()
            return '这就开始讲。'
        self.quick.generate = generate
        self.quick.start('讲个故事', {}, 1, 'turn1')
        await entered.wait()
        self.valid = False
        self.quick.cancel()
        release.set()
        await asyncio.sleep(.01)
        self.assertEqual(self.delivered, [])
