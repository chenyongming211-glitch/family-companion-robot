"""Real SDK/VAD; controlled providers and explicit playback observations."""
import asyncio
import base64
import json
import unittest

from aiohttp import web
from livekit.agents import llm
import test_continuous as fixtures

FIXTURES = fixtures.FIXTURES

try:
    from family_voice.content_runtime import DrainVoiceSession, play_content_unit
except ModuleNotFoundError:
    DrainVoiceSession = None


@unittest.skipUnless((FIXTURES / 'first.wav').exists(), 'server synthetic fixtures required')
class ContentRuntimeTests(unittest.IsolatedAsyncioTestCase):
    asyncSetUp = fixtures.ContinuousTests.asyncSetUp
    asyncTearDown = fixtures.ContinuousTests.asyncTearDown
    feed = fixtures.ContinuousTests.feed
    pcm = fixtures.ContinuousTests.pcm
    until = fixtures.ContinuousTests.until
    asr = fixtures.ContinuousTests.asr

    async def start(self):
        self.assertIsNotNone(DrainVoiceSession, 'drain content runtime is not implemented')
        self.hold_playout = False
        self.tts_texts = []
        self.answers = ['小狐狸来到森林。它找到一条红围巾。最后它高兴地回家。', '二加三等于五。']
        self.tool_name = None
        async def send(event):
            self.events.append(event)
            if self.hold_playout:
                return
            if event['type'] == 'audio':
                block = self.voice.output.blocks[event['block_id']]
                self.voice.receipt({**event, 'played_samples': block.sent, 'finished': False})
            elif event['type'] == 'audio.end':
                self.voice.receipt({**event, 'played_samples': event['sent_samples'], 'finished': True})
        self.voice = DrainVoiceSession(self.cfg, send)
        await self.voice.start()

    async def llm(self, request):
        body = await request.json()
        self.llm_calls.append(body)
        response = web.StreamResponse(headers={'Content-Type': 'text/event-stream'})
        await response.prepare(request)
        answer = self.answers[min(len(self.llm_calls) - 1, len(self.answers) - 1)]
        if self.tool_name:
            delta = {'role': 'assistant', 'content': '我会继续。', 'tool_calls': [{'index': 0, 'id': 'resume-content', 'type': 'function', 'function': {'name': self.tool_name, 'arguments': '{"target":"main"}'}}]}
        else:
            delta = {'role': 'assistant', 'content': answer}
        for value, finish in ((delta, None), ({}, 'tool_calls' if self.tool_name else 'stop')):
            data = {'id': 'content-test', 'created': 1, 'model': 'qwen-plus', 'object': 'chat.completion.chunk', 'choices': [{'index': 0, 'delta': value, 'finish_reason': finish}]}
            await response.write(('data: ' + json.dumps(data) + '\n\n').encode())
        await response.write(b'data: [DONE]\n\n')
        return response

    async def tts(self, request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        await ws.send_json({'type': 'session.created'})
        await ws.receive_json()
        await ws.send_json({'type': 'session.updated'})
        text = await ws.receive_json()
        self.tts_texts.append(text.get('text', ''))
        await ws.receive_json()
        for _ in range(30):
            await ws.send_json({'type': 'response.audio.delta', 'response_id': 'r', 'delta': base64.b64encode(b'\1\0' * 480).decode()})
        await ws.send_json({'type': 'response.done', 'response': {'id': 'r', 'status': 'completed'}})
        await ws.close()
        return ws

    async def test_story_freezes_partial_drain_side_answer_then_resumes_same_sentence(self):
        self.transcripts = ['讲一个短故事', '二加三等于几', '那继续刚才的故事吧']
        await self.start()
        self.hold_playout = True
        await self.feed('first.wav')
        await self.until(lambda: self.voice.output.outstanding_samples == 7200)
        original = self.voice.main.snapshot['units']
        old = next(iter(self.voice.output.blocks.values()))
        await self.feed('second.wav')
        await self.until(lambda: self.voice.side.snapshot is not None)
        await self.until(lambda: any(b.epoch > old.epoch for b in self.voice.output.blocks.values()))
        self.assertEqual(self.voice.main.snapshot['state'], 'draining')
        self.assertEqual(len(self.llm_calls), 2, 'new answer prepares before old drain receipt')
        self.assertFalse(any(e['type'] == 'audio.cancel' for e in self.events))
        self.assertTrue(self.voice.receipt({'block_id': old.id, 'epoch': old.epoch, 'played_samples': old.sent, 'finished': True}))
        self.assertEqual(self.voice.main.snapshot['next'], 0, 'partial sent sentence cannot be called complete')
        self.hold_playout = False
        # Deliver observations held for the already staged new answer.
        for block in tuple(self.voice.output.blocks.values()):
            self.voice.receipt({'block_id': block.id, 'epoch': block.epoch, 'played_samples': block.sent, 'finished': block.sealed})
        await self.until(lambda: self.voice.side.snapshot['state'] == 'completed')
        await self.feed('first.wav')
        await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
        self.assertEqual(self.voice.main.snapshot['units'], original)
        self.assertEqual(len(self.llm_calls), 2, 'direct resume plays frozen original, no regenerated story')
        self.assertEqual(self.voice.main.snapshot['confirmed_count'], 3)
        self.assertFalse(any(getattr(i, 'role', None) == 'assistant' for i in self.voice.session.history.items), 'unplayed drafts are not spoken history')

    async def test_tool_resume_preamble_is_not_spoken_or_saved_as_new_content(self):
        self.transcripts = ['讲一个短故事', '把森林里没说完的那部分接回来']
        await self.start()
        self.hold_playout = True
        await self.feed('first.wav')
        await self.until(lambda: self.voice.output.outstanding_samples == 7200)
        self.tool_name = 'resume_content'
        await self.feed('second.wav')
        await self.until(lambda: len(self.llm_calls) == 2)
        await self.until(lambda: any(e['type'] == 'content.action' and e['action'] == 'resume' for e in self.events))
        self.assertNotIn('我会继续。', self.tts_texts)
        self.assertIsNone(self.voice.side.snapshot)
        old = next(iter(self.voice.output.blocks.values()))
        self.hold_playout = False
        self.voice.receipt({'block_id': old.id, 'epoch': old.epoch, 'played_samples': old.sent})
        await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')

    async def test_explicit_main_and_side_targets_and_ambiguous_resume_keep_both_originals(self):
        await self.start()
        self.hold_playout = True
        self.voice.main.start('原故事第一句。原故事第二句。')
        await self.until(lambda: self.voice.output.outstanding_samples == 7200)
        await self.voice.interrupt()
        old = next(iter(self.voice.output.blocks.values()))
        self.voice.receipt({'block_id': old.id, 'epoch': old.epoch, 'played_samples': old.sent})
        self.voice.side.start('刚刚回答的第一句。回答的第二句。')
        await self.until(lambda: self.voice.output.outstanding_samples == 7200)
        await self.voice.interrupt()
        for block in tuple(self.voice.output.blocks.values()):
            self.voice.receipt({'block_id': block.id, 'epoch': block.epoch, 'played_samples': block.sent})
        before = [self.voice.main.snapshot, self.voice.side.snapshot]
        self.voice.content_action('resume', 'auto')
        self.assertEqual([self.voice.main.snapshot, self.voice.side.snapshot], before)
        await self.until(lambda: any(e['type'] == 'content.ambiguous' for e in self.events))
        self.voice.content_action('resume', 'main')
        self.assertIn(self.voice.main.snapshot['state'], ('playing', 'draining'))
        self.assertEqual(self.voice.side.snapshot['state'], 'paused')

    async def test_replaced_draft_rejects_stale_response_and_old_tool_epoch(self):
        await self.start()
        epoch = self.voice.user_epoch
        self.voice.on_speech_started()
        self.assertFalse(self.voice.accept_draft('迟到旧回答。', '旧问题', epoch))
        self.assertIsNone(self.voice.main.snapshot)
        self.assertFalse(self.voice.tool_is_current(object()))

    async def test_interruption_before_draft_ready_keeps_original_request_for_explicit_resume(self):
        await self.start()
        first = llm.ChatMessage(role='user', content=['讲一个森林故事'])
        await self.voice.agent.on_user_turn_completed(None, first)
        self.voice.on_speech_started()
        question = llm.ChatMessage(role='user', content=['二加三等于几'])
        await self.voice.agent.on_user_turn_completed(None, question)
        self.assertTrue(self.voice.accept_draft('二加三等于五。', '二加三等于几', self.voice.user_epoch))
        self.assertIsNone(self.voice.main.snapshot)
        self.assertEqual(self.voice.side.snapshot['original_input'], '二加三等于几')
        self.assertEqual(self.voice.unprepared_main, '讲一个森林故事')

    async def test_hard_stop_retires_inflight_notice_before_audio_can_start(self):
        await self.start()
        entered, release = asyncio.Event(), asyncio.Event()
        async def prepare(text):
            entered.set()
            await release.wait()
            return b'\1\0' * 480
        self.voice.prepare_content = prepare
        self.voice._notice('测试状态。')
        await entered.wait()
        await self.voice.hard_stop()
        release.set()
        await asyncio.sleep(.03)
        self.assertFalse(any(e['type'] == 'audio' for e in self.events))

    async def test_first_ordinary_answer_resume_targets_main_when_no_side_exists(self):
        self.transcripts = ['解释一下月亮为什么发光', '继续刚才的回答']
        await self.start()
        self.hold_playout = True
        await self.feed('first.wav')
        await self.until(lambda: self.voice.output.outstanding_samples == 7200)
        await self.feed('second.wav')
        await self.until(lambda: any(e['type'] == 'content.action' and e['action'] == 'resume' for e in self.events))
        action = next(e for e in self.events if e['type'] == 'content.action')
        self.assertEqual(action['target'], 'main')
        self.assertEqual(len(self.llm_calls), 1)

    async def test_completed_old_side_does_not_mask_new_main_reply_resume(self):
        self.transcripts = ['解释一下月亮为什么发光', '继续刚才的回答']
        await self.start()
        self.voice.side.start('旧的旁支已经讲完。')
        await self.until(lambda: self.voice.side.snapshot['state'] == 'completed')
        self.hold_playout = True
        await self.feed('first.wav')
        await self.until(lambda: self.voice.output.outstanding_samples == 7200)
        await self.feed('second.wav')
        await self.until(lambda: any(e['type'] == 'content.action' for e in self.events))
        self.assertEqual(next(e for e in self.events if e['type'] == 'content.action')['target'], 'main')

    async def test_new_unprepared_main_survives_after_prior_completed_answer(self):
        await self.start()
        self.voice.accept_draft('先前的回答。', '老问题', self.voice.user_epoch)
        await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
        self.voice.on_speech_started()
        await self.voice.agent.on_user_turn_completed(None, llm.ChatMessage(role='user', content=['讲一个新故事']))
        self.voice.on_speech_started()
        await self.voice.agent.on_user_turn_completed(None, llm.ChatMessage(role='user', content=['二加三等于几']))
        self.voice.accept_draft('五。', '二加三等于几', self.voice.user_epoch)
        self.assertEqual(self.voice.unprepared_main, '讲一个新故事')
        self.assertEqual(self.voice.side.snapshot['original_input'], '二加三等于几')

    async def test_unprepared_first_reply_can_resume_via_answer_wording(self):
        await self.start()
        await self.voice.agent.on_user_turn_completed(None, llm.ChatMessage(role='user', content=['解释一下月亮']))
        self.voice.on_speech_started()
        await self.voice.agent.on_user_turn_completed(None, llm.ChatMessage(role='user', content=['继续刚才的回答']))
        self.assertEqual(self.voice.last_input, '解释一下月亮')

    async def test_confirmed_text_survives_replacement_in_next_model_context(self):
        self.transcripts = ['第一问题', '第二问题']
        await self.start()
        await self.feed('first.wav')
        await self.until(lambda: self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
        await self.feed('second.wav')
        await self.until(lambda: len(self.llm_calls) == 2)
        spoken = ''.join(m.get('content', '') for m in self.llm_calls[1]['messages'] if m['role'] == 'assistant')
        self.assertIn('小狐狸来到森林。', spoken)
        self.assertIn('最后它高兴地回家。', spoken)

    async def test_three_turn_history_keeps_confirmations_after_their_user_turn(self):
        self.transcripts = ['第一问题', '第二问题', '第三问题']
        await self.start()
        for index in range(3):
            await self.feed('first.wav')
            await self.until(lambda: len(self.llm_calls) == index + 1)
            await self.until(lambda: self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed' and self.voice.turns == index + 1)
        roles = [m['role'] for m in self.llm_calls[2]['messages'] if m['role'] in ('user', 'assistant')]
        first_second_user = roles.index('user', 1)
        self.assertIn('assistant', roles[1:first_second_user], 'first answer must precede second user')

    async def test_late_full_receipt_keeps_unit_dispatch_turn_anchor(self):
        await self.start()
        self.hold_playout = True
        async def short(text):
            return b'\1\0' * 480
        self.voice.main._prepare = short
        first = llm.ChatMessage(role='user', content=['原故事请求'])
        await self.voice.agent.on_user_turn_completed(None, first)
        self.voice.accept_draft('原故事一句。', first.text_content, self.voice.user_epoch)
        await self.until(lambda: any(e['type'] == 'audio.end' for e in self.events))
        old = next(iter(self.voice.output.blocks.values()))
        self.voice.on_speech_started()
        second = llm.ChatMessage(role='user', content=['旁支问题'])
        await self.voice.agent.on_user_turn_completed(None, second)
        self.voice.receipt({'block_id':old.id,'epoch':old.epoch,'played_samples':old.sent,'finished':True})
        self.assertEqual(self.voice.spoken_anchors[self.voice.spoken_history[-1].id], first.id)

    async def test_pause_before_output_lock_acquired_settles_unbound_unit(self):
        await self.start()
        await self.voice.play_lock.acquire()
        try:
            self.voice.main.start('尚未取得输出锁的一句。')
            await self.until(lambda: self.voice.main._pending is not None)
            self.voice.main.pause()
            await self.until(lambda: self.voice.main.snapshot['state'] == 'paused')
        finally:
            self.voice.play_lock.release()
        self.voice.main.resume()
        await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')

    async def test_cancel_while_waiting_previous_speech_settles_before_binding(self):
        await self.start()
        entered, release = asyncio.Event(), asyncio.Event()
        class Previous:
            def done(self): return False
            async def wait_for_playout(self):
                entered.set()
                await release.wait()
        class Session:
            current_speech = Previous()
        settled = []
        job = asyncio.create_task(play_content_unit(Session(), self.voice.output, 1, 0, '一句。', b'\1\0' * 480, settled.append))
        await entered.wait()
        job.cancel()
        with self.assertRaises(asyncio.CancelledError): await job
        self.assertEqual(settled, [False])

    async def test_draft_rejection_records_reason_without_draft_body(self):
        from pathlib import Path
        await self.start()
        self.answers = ['unfinished synthetic reply']
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'error' and e.get('code') == 'draft_failed' for e in self.events))
        records = [json.loads(p.read_text()) for p in Path(self.temp.name).glob('*.json')]
        failed = next(r for r in records if r['stage'] == 'llm' and r['state'] == 'outcome_unknown')
        self.assertEqual(failed['failure_code'], 'ending')
        self.assertNotIn(self.answers[0], json.dumps(records))
