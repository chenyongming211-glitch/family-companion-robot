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
        self.tool_plan = []
        self.finish_reason = 'stop'
        self.raw_text = False
        self.extra_tool = None
        self.query_until_hidden = False
        self.quick_calls = []
        self.quick_reply, self.quick_finish, self.quick_gate = {'text': ''}, 'stop', None
        self.quick_entered = asyncio.Event()
        self.main_gate, self.main_gate_after = None, 0
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
        if body.get('response_format', {}).get('type') == 'json_object':
            self.quick_calls.append(body)
            self.quick_entered.set()
            response = web.StreamResponse(headers={'Content-Type': 'text/event-stream'})
            await response.prepare(request)
            if self.quick_gate:
                await self.quick_gate.wait()
            try:
                for delta, finish in (({'role': 'assistant', 'content': json.dumps(self.quick_reply)}, None),
                                      ({}, self.quick_finish)):
                    data = {'id': 'quick-test', 'created': 1, 'model': 'qwen-plus', 'object': 'chat.completion.chunk',
                            'choices': [{'index': 0, 'delta': delta, 'finish_reason': finish}]}
                    await response.write(('data: ' + json.dumps(data) + '\n\n').encode())
                await response.write(b'data: [DONE]\n\n')
            except ConnectionResetError:
                pass
            return response
        self.llm_calls.append(body)
        if self.main_gate and len(self.llm_calls) > self.main_gate_after:
            await self.main_gate.wait()
        response = web.StreamResponse(headers={'Content-Type': 'text/event-stream'})
        await response.prepare(request)
        answer = self.answers[min(len(self.llm_calls) - 1, len(self.answers) - 1)]
        planned = self.tool_plan.pop(0) if self.tool_plan else None
        name, arguments = planned if planned else (self.tool_name or 'respond',
                                                   {'target': 'main'} if self.tool_name else {'text': answer})
        if self.query_until_hidden:
            names = {t['function']['name'] for t in body.get('tools', [])}
            name, arguments = ('query_content', {'target': 'main'}) if 'query_content' in names else ('respond', {'text': answer})
        if self.raw_text:
            name = None
        if name:
            delta = {'role': 'assistant', 'content': '我会继续。', 'tool_calls': [{'index': 0, 'id': f'tool-{len(self.llm_calls)}', 'type': 'function', 'function': {'name': name, 'arguments': json.dumps(arguments)}}]}
            if self.extra_tool:
                extra_name, extra_args = self.extra_tool
                delta['tool_calls'].append({'index': 1, 'id': f'extra-{len(self.llm_calls)}', 'type': 'function',
                                           'function': {'name': extra_name, 'arguments': json.dumps(extra_args)}})
        else:
            delta = {'role': 'assistant', 'content': answer}
        terminal = 'tool_calls' if name and self.finish_reason == 'stop' else self.finish_reason
        for value, finish in ((delta, None), ({}, terminal)):
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

    async def test_quick_preparation_plays_while_main_memory_and_generation_are_blocked(self):
        from pathlib import Path
        from family_voice.conversation_log import read_conversation
        self.cfg = replace(self.cfg, text_record_dir=Path(self.temp.name) / 'quick-conversations')
        await self.start()
        self.voice.main.start('保留的主故事。')
        self.voice.main.pause()
        self.voice.side.start('保留的旁支。')
        self.voice.side.pause()
        before = [self.voice.main.snapshot, self.voice.side.snapshot]
        entered, release = asyncio.Event(), asyncio.Event()
        async def memory():
            entered.set()
            await release.wait()
            return None
        self.voice.memory_context = memory
        self.quick_reply = {'text': '我来讲个小故事。'}
        try:
            await self.feed('first.wav')
            await asyncio.wait_for(entered.wait(), 3)
            await asyncio.wait_for(self.quick_entered.wait(), 3)
            await self.until(lambda: any(e['type'] == 'audio' for e in self.events), timeout=3)
            self.assertEqual([self.voice.main.snapshot, self.voice.side.snapshot], before)
            self.assertEqual(len(self.quick_calls), 1)
            self.assertEqual(self.quick_calls[0]['max_tokens'], 48)
            self.assertFalse(self.quick_calls[0].get('tools'))
            self.assertIn(self.quick_calls[0].get('tool_choice'), (None, 'none'))
            self.assertEqual(self.llm_calls, [], 'main model remains behind memory read')
            self.assertIn(self.quick_reply['text'], self.tts_texts)
            self.assertFalse(release.is_set())
            release.set()
            await self.until(lambda: self.voice.side.snapshot['state'] == 'completed')
            self.assertEqual(self.voice.main.snapshot, before[0])
            await self.voice.conversation.flush()
            rows = read_conversation(self.cfg.text_record_dir, self.voice.conversation.id)['events']
            self.assertTrue(any(e['kind'] == 'assistant.played' and e.get('source') == 'progress'
                                and e['text'] == self.quick_reply['text'] for e in rows))
        finally:
            release.set()

    async def test_ready_answer_cancels_pending_quick_model_without_late_notice(self):
        await self.start()
        self.quick_reply = {'text': '这就开始讲。'}
        self.quick_gate = asyncio.Event()
        self.answers = ['二加三等于五。']
        try:
            await self.feed('first.wav')
            await self.until(lambda: self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
            self.quick_gate.set()
            await asyncio.sleep(.05)
            self.assertEqual(self.tts_texts, self.answers)
            self.assertFalse(any(e['type'] == 'content.state' and e['task_id'].startswith('progress:') for e in self.events))
        finally:
            self.quick_gate.set()

    async def test_new_speech_retires_pending_quick_response_and_old_main_read(self):
        await self.start()
        entered, release = asyncio.Event(), asyncio.Event()
        self.quick_reply = {'text': '我来讲个小故事。'}
        self.quick_gate = asyncio.Event()
        async def memory():
            entered.set()
            await release.wait()
            return None
        self.voice.memory_context = memory
        try:
            await self.feed('first.wav')
            await asyncio.wait_for(self.quick_entered.wait(), 3)
            self.voice.on_speech_started()
            release.set()
            self.quick_gate.set()
            await asyncio.sleep(.1)
            self.assertEqual(self.tts_texts, [])
            self.assertEqual(self.llm_calls, [])
        finally:
            release.set()
            self.quick_gate.set()

    async def test_truncated_quick_response_does_not_fail_substantive_answer(self):
        await self.start()
        self.quick_reply, self.quick_finish = {'text': '我来讲个小故事。'}, 'length'
        await self.feed('first.wav')
        await self.until(lambda: self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
        self.assertNotIn(self.quick_reply['text'], self.tts_texts)
        self.assertFalse(any(e['type'] == 'turn.error' for e in self.events))

    async def test_quick_preparation_and_history_notice_share_one_per_turn_limit(self):
        await self.start()
        entered, release = asyncio.Event(), asyncio.Event()
        async def memory():
            entered.set()
            await release.wait()
            return None
        self.voice.memory_context = memory
        self.quick_reply = {'text': '我来看看前面的内容。'}
        self.tool_plan = [('conversation_history', {'acknowledgement': '我再翻一下旧记录。'})]
        self.answers = ['之前讲的是森林。']
        try:
            await self.feed('first.wav')
            await asyncio.wait_for(entered.wait(), 3)
            await self.until(lambda: self.quick_reply['text'] in self.tts_texts)
            release.set()
            await self.until(lambda: self.answers[0] in self.tts_texts)
            self.assertNotIn('我再翻一下旧记录。', self.tts_texts)
            progress = {e['task_id'] for e in self.events if e['type'] == 'content.state' and e['task_id'].startswith('progress:')}
            self.assertEqual(len(progress), 1)
        finally:
            release.set()

    async def test_invalid_substantive_body_retires_late_quick_model(self):
        await self.start()
        self.answers = ['😊']
        self.quick_reply = {'text': '这就开始讲。'}
        self.quick_gate = asyncio.Event()
        try:
            await self.feed('first.wav')
            await self.until(lambda: any(e['type'] == 'turn.error' for e in self.events))
            self.quick_gate.set()
            await asyncio.sleep(.15)
            self.assertEqual(self.tts_texts, [])
        finally:
            self.quick_gate.set()

    async def test_intermediate_discard_keeps_preparation_for_new_content(self):
        await self.start()
        self.voice.main.start('原来的故事。')
        self.voice.main.pause()
        self.quick_reply = {'text': '这就开始数。'}
        self.quick_gate = asyncio.Event()
        self.main_gate, self.main_gate_after = asyncio.Event(), 1
        self.tool_plan = [('discard_content', {'target': 'main'}),
                          ('respond', {'text': '1、2、3。', 'action': 'replace'})]
        try:
            await self.feed('first.wav')
            await self.until(lambda: len(self.llm_calls) == 2)
            self.quick_gate.set()
            await self.until(lambda: any(e['type'] == 'audio' for e in self.events), timeout=3)
            self.assertFalse(self.main_gate.is_set())
            self.main_gate.set()
            await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
            self.assertIn(self.quick_reply['text'], self.tts_texts)
            self.assertEqual(''.join(self.voice.main.snapshot['units']), '1、2、3。')
        finally:
            self.quick_gate.set()
            self.main_gate.set()

    async def test_resume_does_not_admit_a_late_quick_preparation(self):
        await self.start()
        self.voice.main.start('需要继续的故事。')
        self.voice.main.pause()
        self.quick_reply = {'text': '这就继续讲。'}
        self.quick_gate = asyncio.Event()
        self.tool_name = 'resume_content'
        try:
            await self.feed('first.wav')
            await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
            self.quick_gate.set()
            await asyncio.sleep(.05)
            self.assertNotIn(self.quick_reply['text'], self.tts_texts)
        finally:
            self.quick_gate.set()

    async def test_quick_deadline_retires_notice_waiting_at_output_barrier(self):
        await self.start()
        entered, release = asyncio.Event(), asyncio.Event()
        gate = asyncio.get_running_loop().create_future()
        deliver = self.voice.confirmation.deliver
        def held_delivery(*args):
            # VAD onset replaces the output barrier, so install this after
            # onset at the exact preparation admission boundary.
            self.voice.output._barrier = gate
            return deliver(*args)
        self.voice.confirmation.deliver = held_delivery
        self.voice.confirmation.budget = .5
        self.quick_reply = {'text': '我来讲个小故事。'}
        async def memory():
            entered.set()
            await release.wait()
            return None
        self.voice.memory_context = memory
        try:
            await self.feed('first.wav')
            await asyncio.wait_for(entered.wait(), 3)
            await self.until(lambda: self.voice.progress.snapshot is not None)
            await asyncio.sleep(.55)
            gate.set_result(None)
            release.set()
            await self.until(lambda: self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
            self.assertFalse(any(b.binding and b.binding.task_id.startswith('progress:') and b.sent
                                 for b in (*self.voice.output.blocks.values(), *self.voice.output.history.values())))
        finally:
            if not gate.done():
                gate.set_result(None)
            release.set()

    async def test_speech_during_preparation_drains_admitted_pcm_without_claiming_full_notice(self):
        await self.start()
        self.hold_playout = True
        self.quick_reply = {'text': '我来讲个小故事。'}
        release = asyncio.Event()
        async def memory():
            await release.wait()
            return None
        self.voice.memory_context = memory
        try:
            await self.feed('first.wav')
            await self.until(lambda: self.voice.output.outstanding_samples == 7200)
            old = next(iter(self.voice.output.blocks.values()))
            self.assertTrue(old.binding.task_id.startswith('progress:'))
            self.voice.on_speech_started()
            await self.until(lambda: old.sealed)
            self.assertTrue(self.voice.receipt({'block_id': old.id, 'epoch': old.epoch,
                                              'played_samples': old.sent, 'finished': True}))
            release.set()
            await asyncio.sleep(.05)
            self.assertEqual(self.voice.progress.snapshot['next'], 0)
            self.assertFalse(any(e['type'] == 'audio.cancel' for e in self.events))
            self.assertFalse(any(item.text_content == self.quick_reply['text'] for item in self.voice.spoken_history))
        finally:
            release.set()

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
        self.tool_name = 'resume_content'
        await self.feed('first.wav')
        await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
        self.assertEqual(self.voice.main.snapshot['units'], original)
        self.assertEqual(len(self.llm_calls), 3, 'model selects resume tool; frozen original is not regenerated')
        self.assertEqual(self.voice.main.snapshot['confirmed_count'], 3)
        self.assertFalse(any(getattr(i, 'role', None) == 'assistant' for i in self.voice.session.history.items), 'unplayed drafts are not spoken history')

    async def test_continue_reaches_model_and_model_authors_clarification(self):
        self.transcripts = ['继续', '小狐狸的故事']
        await self.start()
        question = '你想接着聊小狐狸，还是晚饭的事情？'
        self.tool_plan = [('ask_clarification', {'question': question})]
        await self.feed('first.wav')
        await self.until(lambda: bool(self.llm_calls))
        await self.until(lambda: question in self.tts_texts)
        await self.until(lambda: self.voice.dialogue.snapshot['state'] == 'completed')
        self.assertIsNone(self.voice.main.snapshot)
        self.assertIsNone(self.voice.side.snapshot)
        await self.feed('second.wav')
        await self.until(lambda: len(self.llm_calls) == 2)
        spoken = ''.join(m.get('content', '') for m in self.llm_calls[-1]['messages'] if m['role'] == 'assistant')
        self.assertIn(question, spoken, 'next yes/no reply must see the actual model question')

    async def test_archive_older_than_sdk_history_is_sent_to_model_after_reconnect(self):
        from pathlib import Path
        from family_voice.conversation_log import ConversationLog
        folder = Path(self.temp.name) / 'conversations'
        old = await ConversationLog.open(folder)
        old.append('user.transcript', role='user', text='早期小猫叫松果。')
        old.append('assistant.played', role='assistant', text='松果喜欢森林。', unit_id='old')
        old.append('assistant.generated', role='assistant', text='从未播出的秘密。')
        await old.close()
        self.cfg = replace(self.cfg, text_record_dir=folder)
        self.transcripts = ['上次的小猫叫什么']
        await self.start()
        await self.feed('first.wav')
        await self.until(lambda: bool(self.llm_calls))
        text = json.dumps(self.llm_calls[0]['messages'], ensure_ascii=False)
        self.assertIn('松果喜欢森林', text)
        self.assertNotIn('从未播出的秘密', text)

    async def test_query_returns_facts_and_model_speaks_without_replacing_original(self):
        self.transcripts = ['刚刚讲到哪里了']
        await self.start()
        self.voice.main.start('小狐狸来到森林。')
        await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
        before = self.voice.main.snapshot
        self.tool_plan = [('query_content', {'target': 'main'})]
        self.answers = ['刚才讲到小狐狸走进森林那里。']
        await self.feed('first.wav')
        await self.until(lambda: len(self.llm_calls) == 2)
        await self.until(lambda: self.answers[0] in self.tts_texts)
        self.assertEqual(self.voice.main.snapshot, before)
        self.assertIsNone(self.voice.side.snapshot)
        self.assertFalse(any('已确认完整播放' in text for text in self.tts_texts))

    async def test_history_tool_returns_original_to_same_model_and_keeps_playback_separate(self):
        from pathlib import Path
        self.cfg = replace(self.cfg, text_record_dir=Path(self.temp.name) / 'conversations')
        await self.start()
        self.voice.record_text('user.transcript', role='user', text='合成旧事实：围巾是红色。', input_epoch=0)
        self.tool_plan = [('conversation_history', {'mode': 'read', 'session_id': 'current'})]
        self.answers = ['我们刚才聊了红围巾。']
        await self.feed('first.wav')
        await self.until(lambda: len(self.llm_calls) == 2)
        tool_text = ''.join(m.get('content', '') for m in self.llm_calls[-1]['messages'] if m['role'] == 'tool')
        self.assertIn('围巾是红色', tool_text)
        await self.until(lambda: self.answers[0] in self.tts_texts)

    async def test_history_read_finishing_after_new_speech_cannot_trigger_old_reply(self):
        await self.start()
        entered, release = asyncio.Event(), asyncio.Event()
        async def delayed(*args, **kwargs):
            entered.set()
            await release.wait()
            return {'status': 'ok', 'records': [{'text': '过期旧资料'}]}
        self.voice.recall = delayed
        self.tool_plan = [('conversation_history', {'mode': 'read'})]
        await self.feed('first.wav')
        await asyncio.wait_for(entered.wait(), 3)
        self.voice.on_speech_started()
        release.set()
        await asyncio.sleep(.1)
        self.assertEqual(len(self.llm_calls), 1)
        self.assertEqual(self.tts_texts, [])
        self.assertFalse(any(e['type'] == 'audio' for e in self.events))

    async def test_current_snapshot_avoids_redundant_position_query_round(self):
        await self.start()
        self.query_until_hidden = True
        self.answers = ['刚才讲到小狐狸走进森林。']
        self.voice.main.start('小狐狸走进森林。')
        await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'turn.error' for e in self.events)
                         or self.answers[0] in self.tts_texts)
        self.assertIn(self.answers[0], self.tts_texts)
        self.assertEqual(len(self.llm_calls), 1)
        self.assertEqual(sum(e['type'] == 'content.action' and e['action'] == 'query' for e in self.events), 0)
        self.assertFalse(any(e['type'] == 'turn.error' for e in self.events))

    async def test_long_numerical_reply_is_split_before_tts_duration_guard(self):
        await self.start()
        text = '这个海洋观测站建于2026年，位于北纬32度和东经120度，平时记录温度盐度和洋流变化，遇到台风时会把最新数据发回岸上的研究中心。'
        self.answers = [text]
        async def bounded(text):
            # Controlled speech duration reproduces the 12-second rejection
            # without retaining the user's private failed reply as a fixture.
            if len(text) > 60:
                raise ValueError('content audio exceeds 12 seconds')
            return b'\1\0' * 480
        self.voice.main._prepare = bounded
        self.voice.side._prepare = bounded
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'turn.error' for e in self.events)
                         or self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
        self.assertFalse(any(e['type'] == 'turn.error' for e in self.events))
        plan = self.voice.main.snapshot
        self.assertEqual(''.join(plan['units']), text)
        self.assertEqual(plan['next'], len(plan['units']))

    async def test_next_clause_prepares_while_first_clause_playback_is_pending(self):
        await self.start()
        entered = []
        async def prepare(text):
            entered.append(text)
            return b'\1\0' * 480
        self.voice.main._prepare = prepare
        self.hold_playout = True
        self.voice.main.start('第一句还在播放。下一句应该准备好。')
        await self.until(lambda: any(e['type'] == 'audio' for e in self.events))
        await asyncio.sleep(.05)
        self.assertEqual(entered, ['第一句还在播放。', '下一句应该准备好。'])

    async def test_prefetch_cannot_fail_reply_on_a_punctuation_only_tail(self):
        await self.start()
        text = '甲' * 24 + '。'
        self.answers = [text]
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'turn.error' for e in self.events)
                         or self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
        self.assertFalse(any(e['type'] == 'turn.error' for e in self.events))
        self.assertEqual(''.join(self.voice.main.snapshot['units']), text)

    async def test_model_replaces_topic_and_body_in_one_call(self):
        await self.start()
        self.voice.main.start('原来的故事。')
        self.voice.main.pause()
        answer = '地球有七大洲。亚洲面积最大。'
        self.answers = [answer]
        self.tool_plan = [('respond', {'text': answer, 'action': 'replace'})]
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'content.state' and e['state'] == 'completed' for e in self.events))
        self.assertEqual(''.join(self.voice.main.snapshot['units']), answer)
        self.assertEqual(len(self.llm_calls), 1)

    async def test_three_distinct_history_reads_leave_sdk_round_for_answer(self):
        await self.start()
        async def read(*args, **kwargs):
            return {'status': 'ok', 'records': []}
        self.voice.recall = read
        self.answers = ['前面的记录里没有找到这个细节。']
        self.tool_plan = [('conversation_history', {'after': index}) for index in range(3)]
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'turn.error' for e in self.events)
                         or self.answers[0] in self.tts_texts)
        self.assertIn(self.answers[0], self.tts_texts)
        self.assertEqual(len(self.llm_calls), 4)
        names = {t['function']['name'] for t in self.llm_calls[-1]['tools']}
        self.assertEqual(names, {'respond', 'ask_clarification'})
        self.assertFalse(any(e['type'] == 'turn.error' for e in self.events))

    async def test_model_lookup_ack_starts_while_history_read_is_still_pending(self):
        await self.start()
        self.voice.main.start('暂停的故事。')
        self.voice.main.pause()
        self.voice.side.start('暂停的旁支。')
        self.voice.side.pause()
        before = [self.voice.main.snapshot, self.voice.side.snapshot]
        entered, release = asyncio.Event(), asyncio.Event()
        async def delayed(*args, **kwargs):
            entered.set()
            await release.wait()
            return {'status': 'ok', 'records': [{'role': 'assistant', 'delivery': 'confirmed', 'text': '合成地理事实'}]}
        self.voice.recall = delayed
        acknowledgement = '我翻一下刚才的记录。'
        self.tool_plan = [('conversation_history', {'mode': 'read', 'acknowledgement': acknowledgement})]
        try:
            await self.feed('first.wav')
            try:
                await asyncio.wait_for(entered.wait(), 3)
            except TimeoutError:
                self.fail('model-authored lookup acknowledgement was not accepted')
            await self.until(lambda: any(e['type'] == 'audio' for e in self.events), timeout=3)
            self.assertIn(acknowledgement, self.tts_texts)
            self.assertEqual([self.voice.main.snapshot, self.voice.side.snapshot], before)
            self.assertFalse(release.is_set())
        finally:
            release.set()

    async def test_final_answer_cancels_lookup_ack_that_has_not_started(self):
        await self.start()
        self.assertIsNotNone(getattr(self.voice, 'progress', None), 'progress speech slot is missing')
        entered, release = asyncio.Event(), asyncio.Event()
        async def slow_prepare(text):
            entered.set()
            await release.wait()
            return b'\1\0' * 480
        self.voice.progress._prepare = slow_prepare
        async def read(*args, **kwargs):
            await entered.wait()
            return {'status': 'ok', 'records': []}
        self.voice.recall = read
        self.answers = ['之前讲的是森林。']
        self.tool_plan = [('conversation_history', {'acknowledgement': '我看看前面的记录。'})]
        try:
            await self.feed('first.wav')
            await self.until(lambda: self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
            release.set()
            await asyncio.sleep(.05)
            self.assertEqual(self.tts_texts, self.answers)
            self.assertFalse(any(e['type'] == 'turn.error' for e in self.events))
        finally:
            release.set()

    async def test_optional_lookup_ack_failure_does_not_fail_the_actual_answer(self):
        await self.start()
        self.assertIsNotNone(getattr(self.voice, 'progress', None))
        async def failed(text):
            raise TimeoutError('synthetic optional progress failure')
        self.voice.progress._prepare = failed
        async def read(*args, **kwargs):
            await asyncio.sleep(.05)
            return {'status': 'ok', 'records': []}
        self.voice.recall = read
        self.answers = ['之前说的是森林。']
        self.tool_plan = [('conversation_history', {'acknowledgement': '我翻一下之前的记录。'})]
        await self.feed('first.wav')
        await self.until(lambda: self.voice.main.snapshot and self.voice.main.snapshot['state'] == 'completed')
        self.assertEqual(self.tts_texts, self.answers)
        self.assertFalse(any(e['type'] == 'turn.error' for e in self.events))

    async def test_resume_retires_lookup_ack_still_being_prepared(self):
        await self.start()
        self.voice.main.start('需要继续的故事。')
        self.voice.main.pause()
        entered, release = asyncio.Event(), asyncio.Event()
        async def slow_prepare(text):
            entered.set()
            await release.wait()
            return b'\1\0' * 480
        self.voice.progress._prepare = slow_prepare
        async def read(*args, **kwargs):
            await entered.wait()
            return {'status': 'ok', 'records': []}
        self.voice.recall = read
        self.tool_plan = [('conversation_history', {'acknowledgement': '我看看前面的记录。'}),
                          ('resume_content', {'target': 'main'})]
        try:
            await self.feed('first.wav')
            await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
            release.set()
            await asyncio.sleep(.15)
            self.assertFalse(any(b.binding and b.binding.task_id.startswith('progress:')
                                 for b in (*self.voice.output.blocks.values(), *self.voice.output.history.values())))
        finally:
            release.set()

    async def test_ready_answer_retires_ack_blocked_before_pcm_admission(self):
        await self.start()
        gate = asyncio.get_running_loop().create_future()
        self.voice.output._barrier = gate
        self.voice.start_progress('我看看前面的记录。')
        await self.until(lambda: self.voice.output._binding is not None)
        # The SDK may request audio before capture_frame clears its barrier.
        await asyncio.sleep(.05)
        self.voice.retire_unstarted_progress()
        gate.set_result(None)
        await asyncio.sleep(.15)
        self.assertFalse(any(e['type'] == 'audio' for e in self.events))

    async def test_interrupted_lookup_ack_cannot_resume_old_read_after_user_speaks(self):
        await self.start()
        entered, release = asyncio.Event(), asyncio.Event()
        async def delayed(*args, **kwargs):
            entered.set()
            await release.wait()
            return {'status': 'ok', 'records': []}
        self.voice.recall = delayed
        self.tool_plan = [('conversation_history', {'acknowledgement': '我看看前面的记录。'})]
        try:
            await self.feed('first.wav')
            await entered.wait()
            await self.until(lambda: any(e['type'] == 'audio' for e in self.events))
            self.voice.on_speech_started()
            release.set()
            await asyncio.sleep(.1)
            self.assertEqual(len(self.llm_calls), 1)
            self.assertIsNone(self.voice.main.snapshot)
            self.assertIsNone(self.voice.side.snapshot)
        finally:
            release.set()

    async def test_invalid_single_call_replacement_preserves_both_originals(self):
        await self.start()
        self.voice.main.start('不能丢失的原故事。')
        self.voice.main.pause()
        self.voice.side.start('不能丢失的旁支。')
        self.voice.side.pause()
        before = [self.voice.main.snapshot, self.voice.side.snapshot]
        self.tool_plan = [('respond', {'text': '😊', 'action': 'replace'})]
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'turn.error' for e in self.events))
        self.assertEqual([self.voice.main.snapshot, self.voice.side.snapshot], before)

    async def test_ambiguous_tool_returns_candidates_without_fixed_speech(self):
        await self.start()
        self.hold_playout = True
        self.voice.main.start('原故事。')
        self.voice.side.start('旁支解释。')
        self.voice.main.pause()
        self.voice.side.pause()
        before = [self.voice.main.snapshot, self.voice.side.snapshot]
        result = self.voice.content_action('resume', 'auto')
        self.assertIsInstance(result, dict, 'tool must return facts for model interpretation')
        self.assertEqual(result['status'], 'needs_clarification')
        self.assertEqual([self.voice.main.snapshot, self.voice.side.snapshot], before)
        await asyncio.sleep(.03)
        self.assertEqual(self.tts_texts, [])

    async def test_unrouted_model_text_cannot_overwrite_a_paused_side_original(self):
        await self.start()
        self.raw_text = True
        self.answers = ['你想修改哪个故事？']
        self.voice.main.start('小狐狸原故事。')
        self.voice.main.pause()
        self.voice.side.start('木屋原解释。')
        self.voice.side.pause()
        before = [self.voice.main.snapshot, self.voice.side.snapshot]
        await self.feed('first.wav')
        await self.until(lambda: bool(self.llm_calls))
        await self.until(lambda: any(e['type'] == 'turn.error' for e in self.events)
                         or [self.voice.main.snapshot, self.voice.side.snapshot] != before)
        self.assertEqual([self.voice.main.snapshot, self.voice.side.snapshot], before)
        self.assertEqual(self.llm_calls[0].get('tool_choice'), 'required')

    async def test_current_continue_request_does_not_hide_a_completed_original(self):
        await self.start()
        self.voice.main.start('原来的故事讲完了。')
        await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
        before = self.voice.main.snapshot
        self.voice.on_speech_started()
        await self.voice.agent.on_user_turn_completed(None, llm.ChatMessage(role='user', content=['继续这个故事']))
        result = self.voice.content_action('resume', 'main')
        self.assertEqual(result['status'], 'completed')
        self.assertEqual(self.voice.main.snapshot, before)

    async def test_interrupted_position_answer_cannot_turn_query_into_unprepared_story(self):
        await self.start()
        self.voice.main.start('已经完整讲完的故事。')
        await self.until(lambda: self.voice.main.snapshot['state'] == 'completed')
        self.voice.on_speech_started()
        await self.voice.agent.on_user_turn_completed(None, llm.ChatMessage(role='user', content=['讲到哪里了']))
        self.assertEqual(self.voice.content_action('query', 'main')['status'], 'position')
        self.voice.on_speech_started()
        await self.voice.agent.on_user_turn_completed(None, llm.ChatMessage(role='user', content=['继续这个故事']))
        result = self.voice.content_action('resume', 'main')
        self.assertEqual(result['status'], 'completed')
        self.assertIsNone(self.voice.unprepared_main)

    async def test_multiple_state_changing_tools_are_rejected_before_execution(self):
        await self.start()
        self.voice.main.start('必须保留的故事。')
        self.voice.main.pause()
        before = self.voice.main.snapshot
        self.extra_tool = ('discard_content', {'target': 'main'})
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'turn.error' for e in self.events)
                         or self.voice.main.snapshot != before)
        self.assertEqual(self.voice.main.snapshot, before)
        self.assertEqual(self.tts_texts, [])

    async def test_history_read_then_replacement_stays_resumable_main_content(self):
        await self.start()
        self.voice.main.start('旧狐狸故事。')
        self.voice.main.pause()
        self.tool_plan = [('conversation_history', {'mode': 'read'}), ('replace_content', {})]
        self.answers = ['新故事中松果戴着蓝围巾。它高兴地回家。']
        await self.feed('first.wav')
        await self.until(lambda: len(self.llm_calls) == 3)
        await self.until(lambda: any(e['type'] == 'content.state' and e['state'] == 'completed'
                                    for e in self.events))
        self.assertEqual(''.join(self.voice.main.snapshot['units']), self.answers[0])
        self.assertEqual(self.voice.main.snapshot['state'], 'completed')
        self.assertIsNone(self.voice.dialogue.snapshot)

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
        self.voice.dialogue._prepare = prepare
        self.voice.speak_dialogue('你想听哪个故事？')
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
        self.tool_name = 'resume_content'
        await self.feed('second.wav')
        await self.until(lambda: any(e['type'] == 'content.action' and e['action'] == 'resume' for e in self.events))
        action = next(e for e in self.events if e['type'] == 'content.action')
        self.assertEqual(action['target'], 'main')
        self.assertEqual(len(self.llm_calls), 2)

    async def test_completed_old_side_does_not_mask_new_main_reply_resume(self):
        self.transcripts = ['解释一下月亮为什么发光', '继续刚才的回答']
        await self.start()
        self.voice.side.start('旧的旁支已经讲完。')
        await self.until(lambda: self.voice.side.snapshot['state'] == 'completed')
        self.hold_playout = True
        await self.feed('first.wav')
        await self.until(lambda: self.voice.output.outstanding_samples == 7200)
        self.tool_name = 'resume_content'
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
        self.assertEqual(self.voice.last_input, '继续刚才的回答', 'keep the real current utterance')
        result = self.voice.content_action('resume', 'side')
        self.assertEqual(result['status'], 'needs_draft')
        self.assertEqual(self.voice.draft_request, '解释一下月亮')

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

    async def test_text_archive_includes_model_dialogue_without_overwriting_originals(self):
        from pathlib import Path
        from family_voice.conversation_log import read_conversation
        self.cfg = replace(self.cfg, text_record_dir=Path(self.temp.name) / 'conversations')
        await self.start()
        self.hold_playout = True
        async def short(text):
            return b'\1\0' * 480
        self.voice.dialogue._prepare = short
        self.voice.speak_dialogue('你想听哪个故事？')
        await self.until(lambda: any(e['type'] == 'audio.end' for e in self.events))
        journal = self.voice.conversation
        await journal.flush()
        rows = read_conversation(self.cfg.text_record_dir, journal.id)['events']
        draft = next(e for e in rows if e['kind'] == 'assistant.content' and e['source'] == 'dialogue')
        self.assertFalse(any(e['kind'] == 'assistant.played' for e in rows))
        old = next(iter(self.voice.output.blocks.values()))
        self.voice.receipt({'block_id': old.id, 'epoch': old.epoch, 'played_samples': old.sent, 'finished': True})
        await self.until(lambda: self.voice.dialogue.snapshot['state'] == 'completed')
        await journal.flush()
        rows = read_conversation(self.cfg.text_record_dir, journal.id)['events']
        played = next(e for e in rows if e['kind'] == 'assistant.played')
        self.assertEqual(played['text'], draft['text'])
        self.assertEqual(played['source'], 'dialogue')
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
