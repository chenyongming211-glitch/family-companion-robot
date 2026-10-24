"""Real SDK/VAD; controlled providers and explicit playback observations."""
import asyncio
import base64
import json
import unittest
from dataclasses import replace

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

    async def start(self, **options):
        self.assertIsNotNone(DrainVoiceSession, 'drain content runtime is not implemented')
        self.hold_playout = False
        self.tts_texts = []
        self.answers = ['小狐狸来到森林。它找到一条红围巾。最后它高兴地回家。', '二加三等于五。']
        self.tool_name = None
        self.finish_reason = 'stop'
        async def send(event):
            self.events.append(event)
            if self.hold_playout:
                return
            if event['type'] == 'audio':
                block = self.voice.output.blocks[event['block_id']]
                self.voice.receipt({**event, 'played_samples': block.sent, 'finished': False})
            elif event['type'] == 'audio.end':
                self.voice.receipt({**event, 'played_samples': event['sent_samples'], 'finished': True})
        self.voice = DrainVoiceSession(self.cfg, send, **options)
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
        for value, finish in ((delta, None), ({}, 'tool_calls' if self.tool_name else self.finish_reason)):
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
        from pathlib import Path
        self.cfg = replace(self.cfg, text_record_dir=Path(self.temp.name) / 'conversations')
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
        await self.voice.conversation.flush()
        from family_voice.conversation_log import read_conversation
        rows = read_conversation(self.cfg.text_record_dir, self.voice.conversation.id)['events']
        played = [e for e in rows if e['kind'] == 'assistant.played']
        self.assertEqual([(e['turn_id'], e['text']) for e in played], [(first.id, '原故事一句。')])

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
        self.finish_reason = 'length'
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'turn.error' and e.get('code') == 'draft_failed' for e in self.events))
        records = [json.loads(p.read_text()) for p in Path(self.temp.name).glob('*.json')]
        failed = next(r for r in records if r['stage'] == 'llm' and r['state'] == 'outcome_unknown')
        self.assertEqual(failed['failure_code'], 'finish_length')
        self.assertNotIn(self.answers[0], json.dumps(records))
        self.finish_reason = 'stop'
        self.answers = ['五']
        await self.feed('second.wav')
        await self.until(lambda: self.voice.side.snapshot and self.voice.side.snapshot['state'] == 'completed')
        self.assertEqual(self.tts_texts, ['五'])
        self.assertIsNotNone(self.voice.unprepared_main, 'failed original request stays available for an explicit resume')
        self.assertFalse(any(e['type'] == 'error' for e in self.events))

    async def test_normal_provider_stop_accepts_complete_reply_without_final_punctuation(self):
        await self.start()
        self.answers = ['二加三等于五（5）']
        await self.feed('first.wav')
        await self.until(lambda: self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
        self.assertEqual(self.tts_texts, self.answers)
        self.assertFalse(any(e['type'] in ('error', 'turn.error') for e in self.events))

    async def test_decorative_emoji_never_becomes_a_separate_tts_request(self):
        await self.start()
        self.answers = ['谢谢夸奖！😊']
        await self.feed('first.wav')
        await self.until(lambda: self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
        self.assertEqual(self.tts_texts, ['谢谢夸奖！'])
        self.assertEqual(self.voice.main.snapshot['units'], ['谢谢夸奖！'])

    async def test_text_archive_keeps_transcript_and_separates_partial_from_played(self):
        from pathlib import Path
        self.assertTrue(hasattr(self.cfg, 'text_record_dir'), 'text recording configuration is missing')
        folder = Path(self.temp.name) / 'conversations'
        self.cfg = replace(self.cfg, text_record_dir=folder)
        await self.start()
        self.assertIsNotNone(getattr(self.voice, 'conversation', None), 'text archive is not attached')
        from family_voice.conversation_log import read_conversation
        self.hold_playout = True
        await self.feed('first.wav')
        await self.until(lambda: self.voice.output.outstanding_samples == 7200)
        journal = self.voice.conversation
        await journal.flush()
        rows = read_conversation(folder, journal.id)['events']
        self.assertEqual([e['text'] for e in rows if e['kind'] == 'user.transcript'], ['一加一等于几？'])
        self.assertTrue(any(e['kind'] == 'assistant.generated' and '小狐狸' in e['text'] for e in rows))
        content = next(e for e in rows if e['kind'] == 'assistant.content')
        self.assertEqual(content['content_id'], self.voice.main.snapshot['task_id'])
        self.assertEqual(content['text'], ''.join(self.voice.main.snapshot['units']))
        self.assertFalse(any(e['kind'] == 'assistant.played' for e in rows))
        old = next(iter(self.voice.output.blocks.values()))
        await self.voice.interrupt()
        self.voice.receipt({'block_id': old.id, 'epoch': old.epoch, 'played_samples': old.sent, 'finished': True})
        await self.until(lambda: self.voice.main.snapshot['state'] == 'paused')
        await journal.flush()
        rows = read_conversation(folder, journal.id)['events']
        self.assertTrue(any(e['kind'] == 'assistant.delivery_unknown' for e in rows))
        self.assertFalse(any(e['kind'] == 'assistant.played' for e in rows))
        self.hold_playout = False
        self.voice.content_action('resume', 'main')
        await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
        await self.voice.close()
        rows = read_conversation(folder, journal.id)['events']
        played = [e['text'] for e in rows if e['kind'] == 'assistant.played']
        self.assertEqual(played, self.voice.main.snapshot['units'])
        self.assertEqual(rows[-1]['kind'], 'session.end')
        self.assertFalse(any('data' in e or 'api_key' in e for e in rows))

    async def test_text_archive_includes_played_notice_without_overwriting_originals(self):
        from pathlib import Path
        from family_voice.conversation_log import read_conversation
        self.cfg = replace(self.cfg, text_record_dir=Path(self.temp.name) / 'conversations')
        await self.start()
        self.hold_playout = True
        async def short(text):
            return b'\1\0' * 480
        self.voice.prepare_content = short
        self.voice._notice('当前没有保留这段原稿。')
        await self.until(lambda: any(e['type'] == 'audio.end' for e in self.events))
        journal = self.voice.conversation
        await journal.flush()
        rows = read_conversation(self.cfg.text_record_dir, journal.id)['events']
        notice = next(e for e in rows if e['kind'] == 'assistant.notice')
        self.assertFalse(any(e['kind'] == 'assistant.played' for e in rows))
        old = next(iter(self.voice.output.blocks.values()))
        self.voice.receipt({'block_id': old.id, 'epoch': old.epoch, 'played_samples': old.sent, 'finished': True})
        await self.until(lambda: self.voice.notice_job.done())
        await journal.flush()
        rows = read_conversation(self.cfg.text_record_dir, journal.id)['events']
        played = next(e for e in rows if e['kind'] == 'assistant.played')
        self.assertEqual((played['text'], played['unit_id']), (notice['text'], notice['unit_id']))
        self.assertIsNone(self.voice.main.snapshot)
        self.assertIsNone(self.voice.side.snapshot)

    async def test_text_archive_records_final_transcript_only(self):
        from pathlib import Path
        from livekit.agents.voice.events import UserInputTranscribedEvent
        from family_voice.conversation_log import read_conversation
        self.cfg = replace(self.cfg, text_record_dir=Path(self.temp.name) / 'conversations')
        await self.start()
        self.voice.session.emit('user_input_transcribed', UserInputTranscribedEvent(transcript='未定稿', is_final=False))
        self.voice.session.emit('user_input_transcribed', UserInputTranscribedEvent(transcript='最终转写。', is_final=True))
        await self.voice.conversation.flush()
        rows = read_conversation(self.cfg.text_record_dir, self.voice.conversation.id)['events']
        self.assertEqual([e['text'] for e in rows if e['kind'] == 'user.transcript'], ['最终转写。'])

    async def test_text_archive_is_disabled_by_default(self):
        from pathlib import Path
        await self.start()
        self.assertIsNone(self.voice.conversation)
        await self.feed('first.wav')
        await self.until(lambda: self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
        self.assertEqual(list(Path(self.temp.name).rglob('*.jsonl')), [])

    async def test_emoji_only_reply_is_rejected_before_tts(self):
        await self.start()
        self.answers = ['😊']
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'turn.error' for e in self.events))
        self.assertEqual(self.tts_texts, [])
        self.assertIsNone(self.voice.main.snapshot)

    async def test_failed_second_unit_resume_retries_only_unplayed_unit(self):
        await self.start()
        prepared = []
        async def prepare(text):
            prepared.append(text)
            if text == '接着回答问题。' and prepared.count(text) == 1:
                raise TimeoutError('synthetic TTS failure')
            return b'\1\0' * 480
        self.voice.main._prepare = prepare
        self.voice.main.start('谢谢夸奖！接着回答问题。')
        await self.until(lambda: self.voice.main.snapshot['state'] == 'failed')
        self.assertEqual(self.voice.main.snapshot['confirmed_count'], 1)
        revision = self.voice.main.snapshot['revision']
        self.voice.content_action('resume', 'main')
        await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
        self.assertEqual(prepared, ['谢谢夸奖！', '接着回答问题。', '接着回答问题。'])
        self.assertEqual(self.voice.main.snapshot['revision'], revision)
        self.assertEqual(self.voice.main.snapshot['confirmed_count'], 2)
        self.assertFalse(any('从头' in text for text in self.tts_texts))

    async def test_punctuation_does_not_admit_length_or_missing_provider_terminal(self):
        for finish in ('length', None):
            if self.voice:
                await self.voice.close()
            self.events.clear()
            await self.start()
            self.answers = ['看起来有句号的未完成稿。']
            self.finish_reason = finish
            await self.feed('first.wav')
            await self.until(lambda: any(e['type'] == 'turn.error' for e in self.events))
            self.assertEqual(self.tts_texts, [])
            self.assertIsNone(self.voice.main.snapshot)

    async def test_keep_open_survives_idle_and_ninth_real_sdk_turn(self):
        self.cfg = replace(self.cfg, test_keep_open=True)
        await self.start(automatic_idle_seconds=.02)
        self.answers = ['五。']
        for index in range(64):
            self.voice.session.history.add_message(role='user', content=f'旧消息{index}')
        await asyncio.sleep(.07)
        self.assertFalse(self.voice.sleeping)
        for index in range(9):
            await self.feed('first.wav')
            await self.until(lambda: self.voice.turns == index + 1 and self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
        self.assertEqual(len(self.llm_calls), 9)
        self.assertFalse(self.voice.sleeping)
        self.assertIsNone(self.voice.automatic_max_turns)
        self.assertLessEqual(len(self.voice.session.history.items), 33)

    async def test_keep_open_discards_long_utterance_without_closing_session(self):
        self.cfg = replace(self.cfg, test_keep_open=True)
        await self.start(automatic_utterance_seconds=.03)
        fast_pcm = self.pcm
        async def paced(pcm):
            for offset in range(0, len(pcm), 640):
                self.voice.feed(pcm[offset:offset + 640])
                await asyncio.sleep(.02)
            await self.voice.input.drain()
        self.pcm = paced
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'input.limit' for e in self.events))
        self.assertFalse(self.voice.sleeping)
        self.assertEqual(self.llm_calls, [])
        self.assertEqual(self.asr_calls, [])
        prior_asr = len(self.asr_calls)
        self.voice.automatic_utterance_seconds = 25
        self.pcm = fast_pcm
        await self.feed('second.wav')
        await self.until(lambda: self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
        self.assertEqual(len(self.asr_calls), prior_asr + 1)

    async def test_keep_open_sdk_event_history_is_bounded_without_losing_callbacks(self):
        self.cfg = replace(self.cfg, test_keep_open=True)
        await self.start()
        observed = []
        # Exercise the SDK emitter without triggering user-state side effects.
        self.voice.session.on('history_bound_probe', observed.append)
        for index in range(300):
            self.voice.session.emit('history_bound_probe', llm.ChatMessage(role='user', content=[str(index)]))
        self.assertEqual(len(observed), 300)
        self.assertLessEqual(len(self.voice.session._recorded_events), 128)
