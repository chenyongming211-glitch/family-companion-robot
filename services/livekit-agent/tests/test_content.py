"""Content checkpoints use immutable text and definitive playback receipts."""
import asyncio
import re
import unittest

from family_voice.content import ContentController, split_sentences


class SplitTests(unittest.TestCase):
    def test_chinese_and_english_sentences_preserve_normalized_original(self):
        text = '  第一段。\n第二段！ “Hello, world.”  Is this safe? Yes; end  '
        units = split_sentences(text)
        self.assertEqual(''.join(units), re.sub(r'\s+', ' ', text).strip())
        self.assertEqual(units[:2], ['第一段。 ', '第二段！ '])
        self.assertTrue(all(0 < len(unit) <= 80 for unit in units))
        self.assertIn('“Hello, world.” ', units)

    def test_period_inside_decimal_version_or_url_is_not_a_sentence_boundary(self):
        self.assertEqual(split_sentences('圆周率约3.14。下一句。'), ['圆周率约3.14。', '下一句。'])
        text = 'Use v1.2.3 at https://example.com/a. Then stop.'
        self.assertEqual(split_sentences(text), ['Use v1.2.3 at https://example.com/a. ', 'Then stop.'])
        self.assertEqual(split_sentences('“Hello.” Next.'), ['“Hello.” ', 'Next.'])

    def test_long_sentence_is_bounded_without_silent_tail_loss(self):
        text = '前' * 1198 + '尾。'
        units = split_sentences(text)
        self.assertEqual(''.join(units), text)
        self.assertLessEqual(len(units), 40)
        self.assertTrue(all(len(unit) <= 80 for unit in units))
        self.assertEqual(units[-1][-2:], '尾。')

    def test_oversized_text_and_too_many_units_are_rejected_instead_of_truncated(self):
        for text in ('长' * 1201, '句。' * 41, None):
            with self.subTest(text=str(text)[:40]), self.assertRaises(ValueError):
                split_sentences(text)
        self.assertEqual(split_sentences(' \n\t '), [])


class Gates:
    def __init__(self):
        self.prepared, self.played, self.cancelled_prepares, self.cancelled_plays = [], [], [], []
        self.prepares, self.plays, self.settlers = {}, [], []

    async def prepare(self, text):
        self.prepared.append(text)
        future = asyncio.get_running_loop().create_future()
        self.prepares[text] = future
        try:
            return await future
        except asyncio.CancelledError:
            self.cancelled_prepares.append(text)
            raise

    async def play(self, revision, index, text, pcm, settle):
        self.played.append((revision, index, text, pcm))
        self.settlers.append(settle)
        future = asyncio.get_running_loop().create_future()
        self.plays.append(future)
        try:
            await future
        except asyncio.CancelledError:
            self.cancelled_plays.append(index)
            raise

    def finish(self, position, complete):
        self.settlers[position](complete)
        if not self.plays[position].done():
            self.plays[position].set_result(None)


class ContentTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.controllers, self.events = [], []

    async def asyncTearDown(self):
        for controller in self.controllers:
            await controller.close()

    def controller(self, prepare, play, **kwargs):
        controller = ContentController(prepare, play, self.events.append, **kwargs)
        self.controllers.append(controller)
        return controller

    async def until(self, predicate):
        async with asyncio.timeout(1):
            while not predicate():
                await asyncio.sleep(0)

    async def ticks(self):
        for _ in range(15):
            await asyncio.sleep(0)

    async def ready(self, gates, controller, text='零。壹。贰。叁。'):
        controller.start(text, original_input='讲个故事', kind='story')
        await self.until(lambda: bool(gates.prepares))
        gates.prepares[controller.snapshot['units'][0]].set_result(b'\1\0')
        await self.until(lambda: bool(gates.plays))

    async def test_full_then_partial_resumes_same_immutable_unit(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play, name='main', prefetch=1)
        self.assertIsNone(controller.snapshot)
        await self.ready(gates, controller, '零。壹。贰。')
        initial = controller.snapshot
        self.assertEqual(initial['task_id'], 'main:1')
        gates.finish(0, True)
        await self.until(lambda: '壹。' in gates.prepares)
        gates.prepares['壹。'].set_result(b'\2\0')
        await self.until(lambda: len(gates.plays) == 2)
        gates.finish(1, False)
        await self.until(lambda: controller.snapshot['state'] == 'paused')
        self.assertEqual(controller.snapshot['confirmed_count'], 1)
        self.assertEqual(controller.snapshot['next'], 1)
        controller.resume()
        await self.until(lambda: len(gates.plays) == 3)
        self.assertEqual([p[1] for p in gates.played], [0, 1, 1])
        self.assertEqual(gates.prepared.count('壹。'), 1)
        self.assertEqual(controller.snapshot['revision'], initial['revision'])
        self.assertEqual(controller.snapshot['original_input'], '讲个故事')
        self.assertEqual(controller.snapshot['units'], initial['units'])
        initial['units'][0] = 'changed'
        initial['next'] = 100
        self.assertEqual(controller.snapshot['units'][0], '零。')
        self.assertEqual(controller.snapshot['next'], 1)

    async def test_late_full_drain_after_sdk_cancel_advances_and_resume_uses_next(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play, prefetch=1)
        await self.ready(gates, controller, '零。壹。')
        controller.pause()
        await self.until(lambda: gates.cancelled_plays == [0])
        self.assertEqual(controller.snapshot['state'], 'draining')
        self.assertEqual(controller.snapshot['next'], 0)
        gates.settlers[0](True)
        self.assertEqual(controller.snapshot['state'], 'paused')
        self.assertEqual(controller.snapshot['next'], 1)
        controller.resume()
        await self.until(lambda: '壹。' in gates.prepares)
        gates.prepares['壹。'].set_result(b'\2\0')
        await self.until(lambda: len(gates.plays) == 2)
        self.assertEqual([p[1] for p in gates.played], [0, 1])
        gates.finish(1, True)
        await self.until(lambda: controller.snapshot['state'] == 'completed')
        self.assertEqual(controller.snapshot['confirmed_count'], 2)
        controller.resume()
        await self.ticks()
        self.assertEqual(len(gates.plays), 2)

    async def test_resume_waits_for_partial_drain_then_repeats_current(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play, prefetch=1)
        await self.ready(gates, controller, '零。壹。')
        controller.pause()
        controller.resume()
        await self.ticks()
        self.assertEqual(len(gates.plays), 1)
        self.assertEqual(controller.snapshot['state'], 'draining')
        gates.settlers[0](False)
        await self.until(lambda: len(gates.plays) == 2)
        self.assertEqual([p[1] for p in gates.played], [0, 0])
        self.assertEqual(gates.prepared, ['零。'])

    async def test_repause_cancels_queued_resume_without_cancelling_receipt(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play, prefetch=1)
        await self.ready(gates, controller, '零。壹。')
        controller.pause()
        controller.resume()
        await self.ticks()
        controller.pause()
        await self.ticks()
        gates.settlers[0](True)
        await self.ticks()
        self.assertEqual(controller.snapshot['state'], 'paused')
        self.assertEqual(controller.snapshot['next'], 1)
        self.assertEqual(len(gates.plays), 1)
        controller.resume()
        await self.until(lambda: '壹。' in gates.prepares)
        self.assertEqual(gates.prepared, ['零。', '壹。'])

    async def test_stale_receipts_after_replace_discard_and_close_do_not_mutate(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play, prefetch=1)
        await self.ready(gates, controller, '旧。末。')
        old = gates.settlers[0]
        controller.start('新。末。')
        revision = controller.snapshot['revision']
        old(True)
        self.assertEqual(controller.snapshot['next'], 0)
        self.assertEqual(revision, 2)
        await self.until(lambda: '新。' in gates.prepares)
        gates.prepares['新。'].set_result(b'\1\0')
        await self.until(lambda: len(gates.plays) == 2)
        discarded = gates.settlers[1]
        controller.discard()
        discarded(True)
        self.assertEqual(controller.snapshot['state'], 'discarded')
        self.assertEqual(controller.snapshot['next'], 0)
        controller.resume()
        await controller.close()
        discarded(True)
        self.assertEqual(controller.snapshot['next'], 0)
        with self.assertRaises(RuntimeError):
            controller.start('再。')

    async def test_pause_keeps_bounded_ready_and_inflight_pcm_without_new_preparation(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        await self.ready(gates, controller)
        await self.until(lambda: len(gates.prepares) == 3)
        controller.pause()
        for text in ('壹。', '贰。'):
            gates.prepares[text].set_result(b'\2\0')
        await self.ticks()
        self.assertEqual(gates.prepared, ['零。', '壹。', '贰。'])
        self.assertEqual(gates.cancelled_prepares, [])
        gates.settlers[0](False)
        controller.resume()
        await self.until(lambda: len(gates.plays) == 2)
        self.assertEqual(len(gates.prepared), 3)
        gates.finish(1, True)
        await self.until(lambda: '叁。' in gates.prepares)
        await self.until(lambda: len(gates.plays) == 3)
        self.assertEqual(gates.played[2][2:], ('壹。', b'\2\0'))
        self.assertEqual(len(controller._prepared), 3)

    async def test_pause_before_prepare_finishes_never_plays_or_starts_new_prepare(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        controller.start('零。壹。贰。叁。')
        await self.until(lambda: len(gates.prepares) == 3)
        controller.pause()
        gates.prepares['零。'].set_result(b'\1\0')
        await self.ticks()
        self.assertEqual(controller.snapshot['state'], 'paused')
        self.assertEqual(gates.played, [])
        self.assertEqual(gates.prepared, ['零。', '壹。', '贰。'])
        controller.resume()
        await self.until(lambda: bool(gates.plays))
        self.assertEqual(gates.played[0][3], b'\1\0')

    async def test_immediate_resume_pause_does_not_release_waiting_prepares(self):
        prepared = []
        controller = None
        async def prepare(text):
            prepared.append(text)
            controller.pause()
            return b'\0\0'
        async def play(*args):
            self.fail('paused content must not play')
        controller = self.controller(prepare, play)
        controller.start('零。壹。贰。叁。')
        await self.until(lambda: controller.snapshot['state'] == 'paused')
        controller.resume()
        controller.pause()
        await self.ticks()
        self.assertEqual(prepared, ['零。'])

    async def test_normal_return_without_settle_is_unknown_and_cannot_be_upgraded_late(self):
        callbacks = []
        async def prepare(text):
            return b'\0\0'
        async def play(revision, index, text, pcm, settle):
            callbacks.append(settle)
        controller = self.controller(prepare, play)
        controller.start('零。壹。')
        await self.until(lambda: controller.snapshot['state'] == 'paused')
        callbacks[0](True)
        self.assertEqual(controller.snapshot['next'], 0)
        self.assertEqual(controller.snapshot['confirmed_count'], 0)

    async def test_resume_retires_settled_play_cleanup_before_dispatching_again(self):
        started, cleanup, release = asyncio.Event(), asyncio.Event(), asyncio.Event()
        calls = []
        callbacks = []
        async def prepare(text):
            return b'\0\0'
        async def play(revision, index, text, pcm, settle):
            calls.append(index)
            callbacks.append(settle)
            if len(calls) == 1:
                settle(False)
                started.set()
                try:
                    await release.wait()
                finally:
                    cleanup.set()
            else:
                self.assertTrue(cleanup.is_set())
                settle(False)
        controller = self.controller(prepare, play, prefetch=1)
        controller.start('零。壹。')
        await started.wait()
        try:
            controller.resume()
            await self.until(lambda: len(calls) == 2)
            self.assertTrue(cleanup.is_set())
            self.assertEqual(calls, [0, 0])
        finally:
            release.set()

    async def test_prepare_unknown_failure_is_terminal_even_while_paused(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        controller.start('零。壹。贰。叁。')
        await self.until(lambda: len(gates.prepares) == 3)
        controller.pause()
        gates.prepares['壹。'].set_exception(RuntimeError('submission unknown'))
        await self.until(lambda: controller.snapshot['state'] == 'failed')
        controller.resume()
        await self.ticks()
        self.assertEqual(gates.prepared, ['零。', '壹。', '贰。'])
        self.assertEqual(gates.played, [])
        self.assertEqual(set(gates.cancelled_prepares), {'零。', '贰。'})

    async def test_malformed_or_overlong_pcm_fails_without_play(self):
        for pcm in (b'', b'\0', b'\0' * (24000 * 2 * 12 + 2), 'not bytes'):
            played = []
            async def prepare(text):
                return pcm
            async def play(*args):
                played.append(args)
            controller = self.controller(prepare, play)
            controller.start('句。')
            await self.until(lambda: controller.snapshot['state'] == 'failed')
            self.assertEqual(played, [])
            self.assertEqual(controller.snapshot['next'], 0)

    async def test_close_joins_workers_and_prepares_without_waiting_for_receipt(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        await self.ready(gates, controller)
        await self.until(lambda: len(gates.prepares) == 3)
        controller.pause()
        controller.resume()
        await self.ticks()
        await asyncio.wait_for(controller.close(), .3)
        gates.settlers[0](True)
        self.assertEqual(controller.snapshot['next'], 0)
        self.assertEqual(controller._tasks, set())
        self.assertEqual(controller._prepared, {})
        self.assertEqual(set(gates.cancelled_prepares), {'壹。', '贰。'})
        self.assertEqual(gates.cancelled_plays, [0])

    async def test_events_contain_only_metadata_and_invalid_start_preserves_plan(self):
        gates = Gates()
        controller = self.controller(gates.prepare, gates.play)
        controller.start('秘密内容。', original_input='秘密问题', kind='story')
        snapshot = controller.snapshot
        for text in ('', '长' * 1201, '句。' * 41, None):
            with self.assertRaises(ValueError):
                controller.start(text)
            self.assertEqual(controller.snapshot, snapshot)
        expected = {'type', 'task_id', 'revision', 'kind', 'total', 'confirmed_count', 'next', 'state'}
        self.assertTrue(self.events)
        self.assertTrue(all(set(event) == expected for event in self.events))
        self.assertNotIn('秘密', str(self.events))
        for prefetch in (0, 4, True, 1.5):
            with self.assertRaises(ValueError):
                self.controller(gates.prepare, gates.play, prefetch=prefetch)


if __name__ == '__main__':
    unittest.main()
