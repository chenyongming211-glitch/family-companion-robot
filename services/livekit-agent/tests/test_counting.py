"""Deterministic checkpoints, independent of SDK/model/browser behavior."""
import asyncio
import unittest

from family_voice.counting import CountController, is_count_resume, parse_count_request


class ParsingTests(unittest.TestCase):
    def test_explicit_chinese_and_arabic_ranges(self):
        for text, expected in (
            ('请从一一直数到五十', (1, 50)),
            ('从1数到50，中间不要停。', (1, 50)),
            ('帮我从零数到九十九', (0, 99)),
            ('从九百九十九数到一千', (999, 1000)),
            ('请数1到50', (1, 50)),
            ('从一百零一开始数到一百二十', (101, 120)),
        ):
            with self.subTest(text=text):
                self.assertEqual(parse_count_request(text), expected)

    def test_recognized_but_oversized_request_is_left_for_start_to_reject(self):
        self.assertEqual(parse_count_request('从1数到1001'), (1, 1001))
        self.assertEqual(parse_count_request('从50数到1'), (50, 1))
        self.assertEqual(parse_count_request('从-1数到2'), (-1, 2))

    def test_unrelated_or_negated_requests_do_not_start_counting(self):
        for text in ('二加三等于几', '继续讲故事', '不要从1数到50', '从前有50个人', '从一百猫数到五十'):
            with self.subTest(text=text):
                self.assertIsNone(parse_count_request(text))

    def test_resume_is_specific_to_counting_or_bare_continue(self):
        for text in ('那继续数吧', '继续数', '继续数数。', '接着数', '继续', '好，那继续数吧'):
            self.assertTrue(is_count_resume(text), text)
        for text in ('继续讲故事', '不要继续数', '继续说说为什么', '停止数数'):
            self.assertFalse(is_count_resume(text), text)

    def test_spoken_fillers_and_checkpoint_reference_still_resume_same_task(self):
        for text in ('啊那继续数吧。', '嗯，你接着数吧', '请你继续吧', '从刚才停的地方继续数', '继续刚才的数数', '啊，继续。'):
            with self.subTest(text=text):
                self.assertTrue(is_count_resume(text))
        for text in ('啊，不要继续数', '你别继续数', '继续讲故事', '从头重新数', '你继续说为什么'):
            with self.subTest(text=text):
                self.assertFalse(is_count_resume(text))

    def test_spoken_fillers_do_not_bypass_explicit_count_task(self):
        self.assertEqual(parse_count_request('啊，麻烦你从一数到五十吧。'), (1, 50))
        self.assertIsNone(parse_count_request('啊，你不要从一数到五十'))


class Gates:
    def __init__(self):
        self.prepared, self.played = [], []
        self.prepares, self.plays = {}, []
        self.cancelled_prepares, self.cancelled_plays = [], []

    async def prepare(self, number):
        self.prepared.append(number)
        gate = asyncio.get_running_loop().create_future()
        self.prepares[number] = gate
        try:
            return await gate
        except asyncio.CancelledError:
            self.cancelled_prepares.append(number)
            raise

    async def play(self, number, pcm):
        self.played.append((number, pcm))
        gate = asyncio.get_running_loop().create_future()
        self.plays.append(gate)
        try:
            return await gate
        except asyncio.CancelledError:
            self.cancelled_plays.append(number)
            raise


class CountTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.controllers = []
        self.events = []

    async def asyncTearDown(self):
        for controller in getattr(self, 'controllers', ()):
            await controller.close()

    def controller(self, prepare, play, **kwargs):
        controller = CountController(prepare, play, self.events.append, **kwargs)
        self.controllers.append(controller)
        return controller

    async def until(self, predicate):
        async with asyncio.timeout(1):
            while not predicate():
                await asyncio.sleep(0)

    async def ticks(self):
        for _ in range(12):
            await asyncio.sleep(0)

    async def test_full_confirmations_complete_at_original_endpoint(self):
        prepared, played = [], []
        async def prepare(number):
            prepared.append(number)
            return b'\0\0'
        async def play(number, pcm):
            played.append(number)
            return True
        controller = self.controller(prepare, play)
        self.assertIsNone(controller.snapshot)
        controller.start(1, 50)
        await self.until(lambda: controller.snapshot['state'] == 'completed')
        self.assertEqual(played, list(range(1, 51)))
        self.assertEqual(prepared, list(range(1, 51)))
        self.assertEqual(controller.snapshot, {'start': 1, 'end': 50, 'next': 51, 'state': 'completed'})
        controller.resume()
        await self.ticks()
        self.assertEqual(played[-1], 50)
        self.assertEqual(len(played), 50)
        self.assertEqual([event['next'] for event in self.events if event['type'] == 'count.state'], list(range(1, 52)))
        self.assertTrue(all(set(event) == {'type', 'start', 'end', 'next', 'state'} for event in self.events))
        snapshot = controller.snapshot
        snapshot['end'] = 100
        self.assertEqual(controller.snapshot['end'], 50)

    async def test_partial_or_unknown_playback_does_not_advance(self):
        for result in (False, None):
            with self.subTest(result=result):
                gates = Gates()
                controller = self.controller(gates.prepare, gates.play, prefetch=1)
                controller.start(7, 9)
                await self.until(lambda: 7 in gates.prepares)
                gates.prepares[7].set_result(b'\1\0')
                await self.until(lambda: bool(gates.plays))
                gates.plays[0].set_result(result)
                await self.until(lambda: controller.snapshot['state'] == 'paused')
                self.assertEqual(controller.snapshot['next'], 7)
                self.assertEqual(gates.prepared, [7])
                controller.resume()
                await self.until(lambda: len(gates.plays) == 2)
                self.assertEqual(gates.prepared, [7])
                self.assertEqual([number for number, _ in gates.played], [7, 7])
                await controller.close()

    async def test_pause_keeps_inflight_and_ready_audio_and_starts_no_new_prepare(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        controller.start(1, 5)
        await self.until(lambda: len(gates.prepares) == 3)
        gates.prepares[1].set_result(b'\1\0')
        await self.until(lambda: bool(gates.plays))
        controller.pause()
        gates.prepares[2].set_result(b'\2\0')
        gates.prepares[3].set_result(b'\3\0')
        await self.ticks()
        self.assertEqual(controller.snapshot, {'start': 1, 'end': 5, 'next': 1, 'state': 'paused'})
        self.assertEqual(gates.prepared, [1, 2, 3])
        self.assertEqual(gates.cancelled_prepares, [])
        controller.resume()
        await self.until(lambda: len(gates.plays) == 2)
        self.assertEqual(gates.played[:2], [(1, b'\1\0'), (1, b'\1\0')])
        gates.plays[1].set_result(True)
        await self.until(lambda: len(gates.plays) == 3)
        self.assertEqual(gates.played[2], (2, b'\2\0'))
        self.assertEqual(gates.prepared.count(1), 1)
        self.assertEqual(gates.prepared.count(2), 1)

    async def test_pause_before_current_prepare_finishes_does_not_cancel_or_play_it(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        controller.start(1, 5)
        await self.until(lambda: len(gates.prepares) == 3)
        controller.pause()
        gates.prepares[1].set_result(b'\1\0')
        await self.ticks()
        self.assertEqual(gates.played, [])
        self.assertEqual(gates.prepared, [1, 2, 3])
        controller.resume()
        await self.until(lambda: bool(gates.plays))
        self.assertEqual(gates.prepared, [1, 2, 3])

    async def test_quick_resume_then_pause_does_not_start_waiting_prepares(self):
        prepared = []
        controller = None
        async def prepare(number):
            prepared.append(number)
            if number == 1:
                controller.pause()
            return b'\0\0'
        async def play(number, pcm):
            return True
        controller = self.controller(prepare, play)
        controller.start(1, 5)
        await self.until(lambda: controller.snapshot['state'] == 'paused')
        await self.ticks()
        controller.resume()
        controller.pause()
        await self.ticks()
        self.assertEqual(prepared, [1])
        self.assertEqual(controller.snapshot['next'], 1)

    async def test_interrupted_one_to_fifty_replays_current_number_then_stops_at_fifty(self):
        played = []
        reached_three = asyncio.Event()
        async def prepare(number):
            return number.to_bytes(2, 'little')
        async def play(number, pcm):
            played.append(number)
            if played == [1, 2, 3]:
                reached_three.set()
                await asyncio.Future()
            return True
        controller = self.controller(prepare, play)
        controller.start(1, 50)
        await reached_three.wait()
        controller.pause()
        self.assertEqual(controller.snapshot, {'start': 1, 'end': 50, 'next': 3, 'state': 'paused'})
        controller.resume()
        await self.until(lambda: controller.snapshot['state'] == 'completed')
        self.assertEqual(played, [1, 2, 3] + list(range(3, 51)))
        controller.resume()
        await self.ticks()
        self.assertNotIn(51, played)

    async def test_stale_cancel_suppressing_play_cannot_advance_resumed_task(self):
        started, cancelled, release_old = asyncio.Event(), asyncio.Event(), asyncio.Event()
        plays = []
        async def prepare(number):
            return b'\0\0'
        async def play(number, pcm):
            plays.append(number)
            if len(plays) == 1:
                started.set()
                try:
                    await asyncio.Future()
                except asyncio.CancelledError:
                    cancelled.set()
                    await release_old.wait()
                    return True
            return False
        controller = self.controller(prepare, play)
        controller.start(1, 3)
        await started.wait()
        controller.pause()
        await cancelled.wait()
        controller.resume()
        try:
            await self.ticks()
            self.assertEqual(plays, [1])
            self.assertEqual(controller.snapshot['next'], 1)
        finally:
            release_old.set()
        await self.until(lambda: len(plays) == 2)
        self.assertEqual(plays, [1, 1])
        self.assertEqual(controller.snapshot['next'], 1)

    async def test_new_start_retires_old_generation_and_its_prepares(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        controller.start(1, 5)
        await self.until(lambda: len(gates.prepares) == 3)
        gates.prepares[1].set_result(b'\1\0')
        await self.until(lambda: bool(gates.plays))
        controller.start(10, 11)
        await self.until(lambda: 10 in gates.prepares)
        self.assertEqual(controller.snapshot, {'start': 10, 'end': 11, 'next': 10, 'state': 'playing'})
        self.assertEqual(gates.cancelled_plays, [1])
        self.assertEqual(set(gates.cancelled_prepares), {2, 3})
        self.assertEqual([n for n, _ in gates.played], [1])

    async def test_failed_prepare_is_terminal_and_not_automatically_retried(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        controller.start(1, 5)
        await self.until(lambda: len(gates.prepares) == 3)
        gates.prepares[1].set_exception(RuntimeError('submission outcome unknown'))
        await self.until(lambda: controller.snapshot['state'] == 'failed')
        controller.resume()
        await self.ticks()
        self.assertEqual(gates.prepared, [1, 2, 3])
        self.assertEqual(controller.snapshot['next'], 1)
        self.assertEqual(gates.played, [])
        self.assertEqual(set(gates.cancelled_prepares), {2, 3})

    async def test_play_error_preserves_checkpoint_and_requires_explicit_resume(self):
        async def prepare(number):
            return b'\0\0'
        async def play(number, pcm):
            raise RuntimeError('receipt unavailable')
        controller = self.controller(prepare, play)
        controller.start(1, 3)
        await self.until(lambda: controller.snapshot['state'] == 'paused')
        self.assertEqual(controller.snapshot['next'], 1)

    async def test_prefetch_counts_playing_unit_and_is_bounded_until_confirmation(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        controller.start(1, 20)
        await self.until(lambda: len(gates.prepares) == 3)
        for n in (1, 2, 3):
            gates.prepares[n].set_result(b'\0\0')
        await self.until(lambda: bool(gates.plays))
        await self.ticks()
        self.assertEqual(gates.prepared, [1, 2, 3])
        gates.plays[0].set_result(True)
        await self.until(lambda: 4 in gates.prepares)
        self.assertEqual(gates.prepared, [1, 2, 3, 4])
        self.assertEqual(controller.snapshot['next'], 2)

    async def test_malformed_or_overlong_pcm_is_never_played(self):
        for pcm in (b'', b'\0', b'\0' * (24000 * 2 * 5 + 2), 'not pcm'):
            calls = []
            async def prepare(number):
                return pcm
            async def play(number, audio):
                calls.append(number)
                return True
            controller = self.controller(prepare, play)
            controller.start(1, 1)
            await self.until(lambda: controller.snapshot['state'] == 'failed')
            self.assertEqual(calls, [])
            self.assertEqual(controller.snapshot['next'], 1)

    async def test_ranges_and_prefetch_reject_unbounded_work_without_mutating_plan(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        for start, end in ((-1, 2), (0, 1000), (1, 101), (1, 1001), (5, 4), (True, 3), (1.0, 3)):
            with self.subTest(start=start, end=end), self.assertRaises(ValueError):
                controller.start(start, end)
        self.assertIsNone(controller.snapshot)
        self.assertEqual(gates.prepared, [])
        for prefetch in (0, 4, -1, 1.5, True):
            with self.assertRaises(ValueError):
                self.controller(gates.prepare, gates.play, prefetch=prefetch)
        controller.start(901, 1000)
        self.assertEqual(controller.snapshot['end'], 1000)

    async def test_close_awaits_cancellation_of_play_and_prefetched_resources(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        controller.start(1, 5)
        await self.until(lambda: len(gates.prepares) == 3)
        gates.prepares[1].set_result(b'\0\0')
        await self.until(lambda: bool(gates.plays))
        await asyncio.wait_for(controller.close(), .3)
        self.assertEqual(gates.cancelled_plays, [1])
        self.assertEqual(set(gates.cancelled_prepares), {2, 3})
        self.assertEqual(controller.snapshot['state'], 'paused')
        controller.resume()
        controller.pause()
        await controller.close()
        await self.ticks()
        self.assertEqual(gates.prepared, [1, 2, 3])
        with self.assertRaises(RuntimeError):
            controller.start(1, 2)


if __name__ == '__main__':
    unittest.main()
