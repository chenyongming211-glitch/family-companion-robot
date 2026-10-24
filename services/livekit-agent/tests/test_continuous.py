"""Real SDK/Silero with server-private synthetic speech and controlled providers."""
import asyncio
import base64
import json
import os
import tempfile
import unittest
import wave
import random
from array import array
from pathlib import Path

from aiohttp import web
from family_voice.config import Settings
from family_voice.runtime import VoiceSession

FIXTURES = Path(os.environ.get('VOICE_TEST_FIXTURES', '/var/lib/family-robot-livekit-dev/smoke-20261024-1'))


@unittest.skipUnless((FIXTURES / 'first.wav').exists(), 'requires server-private synthetic speech fixtures')
class ContinuousTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.events, self.asr_calls, self.llm_calls = [], [], []
        self.hold_playout, self.long_output = False, False
        self.transcripts = ['一加一等于几？', '二加三等于几？', '一加一等于几？']
        self.hold_first_asr = False
        self.hold_second_asr = False
        self.first_asr_waiting, self.release_first_asr = asyncio.Event(), asyncio.Event()
        self.second_asr_waiting, self.release_second_asr = asyncio.Event(), asyncio.Event()
        self.asr_closed = set()
        app = web.Application()
        app.router.add_get('/asr', self.asr)
        app.router.add_get('/tts', self.tts)
        app.router.add_post('/v1/chat/completions', self.llm)
        self.runner = web.AppRunner(app)
        await self.runner.setup()
        site = web.TCPSite(self.runner, '127.0.0.1', 0)
        await site.start()
        port = site._server.sockets[0].getsockname()[1]
        self.cfg = Settings.from_env({'DASHSCOPE_API_KEY': 'synthetic-key', 'VOICE_ASR_URL': f'ws://127.0.0.1:{port}/asr', 'VOICE_TTS_URL': f'ws://127.0.0.1:{port}/tts', 'VOICE_LLM_URL': f'http://127.0.0.1:{port}/v1', 'VOICE_AUDIT_DIR': self.temp.name})
        self.voice = None

    async def asyncTearDown(self):
        if self.voice:
            await self.voice.close()
        await self.runner.cleanup()
        self.temp.cleanup()

    async def start(self, **options):
        async def send(event):
            self.events.append(event)
            if event['type'] == 'audio.end' and not self.hold_playout:
                self.voice.receipt({**event, 'played_samples': event['sent_samples'], 'valid': True, 'finished': True})
            if event['type'] == 'audio.cancel':
                for block in tuple(self.voice.output.pending):
                    if block.epoch < event['next_epoch']:
                        self.voice.receipt({'block_id': block.id, 'epoch': block.epoch, 'played_samples': min(block.sent, 24000 * 16), 'valid': True, 'finished': True})
        self.voice = VoiceSession(self.cfg, send, automatic=True, **options)
        await self.voice.start()

    async def feed(self, name):
        with wave.open(str(FIXTURES / name), 'rb') as f:
            self.assertEqual((f.getnchannels(), f.getsampwidth(), f.getframerate()), (1, 2, 16000))
            pcm = f.readframes(f.getnframes())
        await self.pcm(pcm + b'\0' * 32000)

    async def pcm(self, pcm):
        for i in range(0, len(pcm), 640):
            self.voice.feed(pcm[i:i + 640])
            await asyncio.sleep(.002)
        await self.voice.input.drain()

    async def until(self, predicate, timeout=5):
        async with asyncio.timeout(timeout):
            while not predicate():
                await asyncio.sleep(.01)

    async def asr(self, request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        header = (await ws.receive_json())['header']
        task = header['task_id']
        index = len(self.asr_calls)
        self.asr_calls.append(0)
        await ws.send_json({'header': {'event': 'task-started', 'task_id': task}})
        async for message in ws:
            if message.type == web.WSMsgType.BINARY:
                self.asr_calls[index] += len(message.data)
            elif message.type == web.WSMsgType.TEXT:
                if (self.hold_first_asr and index == 0) or (self.hold_second_asr and index == 1):
                    waiting, release = (self.first_asr_waiting, self.release_first_asr) if index == 0 else (self.second_asr_waiting, self.release_second_asr)
                    waiting.set()
                    closed = asyncio.create_task(ws.receive())
                    released = asyncio.create_task(release.wait())
                    done, pending = await asyncio.wait((closed, released), return_when=asyncio.FIRST_COMPLETED)
                    for job in pending:
                        job.cancel()
                    await asyncio.gather(*pending, return_exceptions=True)
                    if closed in done:
                        self.asr_closed.add(index)
                        await ws.close()
                        return ws
                await ws.send_json({'header': {'event': 'result-generated', 'task_id': task}, 'payload': {'output': {'sentence': {'text': self.transcripts[min(index, 2)], 'sentence_end': True}}}})
                await ws.send_json({'header': {'event': 'task-finished', 'task_id': task}})
                break
        await ws.close()
        return ws

    async def llm(self, request):
        body = await request.json()
        self.llm_calls.append(body)
        question = body['messages'][-1]['content']
        answer = '5' if '二加三' in question else '2'
        response = web.StreamResponse(headers={'Content-Type': 'text/event-stream'})
        await response.prepare(request)
        for delta, finish in (({'role': 'assistant', 'content': answer}, None), ({}, 'stop')):
            data = {'id': 'continuous-test', 'created': 1, 'model': 'qwen-plus', 'object': 'chat.completion.chunk', 'choices': [{'index': 0, 'delta': delta, 'finish_reason': finish}]}
            await response.write(('data: ' + json.dumps(data) + '\n\n').encode())
        await response.write(b'data: [DONE]\n\n')
        return response

    async def tts(self, request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        await ws.send_json({'type': 'session.created'})
        await ws.receive_json()
        await ws.send_json({'type': 'session.updated'})
        await ws.receive_json(); await ws.receive_json()
        for _ in range(1500 if self.long_output else 1):
            await ws.send_json({'type': 'response.audio.delta', 'response_id': 'r', 'delta': base64.b64encode(b'\1\0' * 480).decode()})
        await ws.send_json({'type': 'response.done', 'response': {'id': 'r', 'status': 'completed'}})
        await ws.close()
        return ws

    async def test_silence_and_light_noise_make_no_cloud_calls_then_two_utterances_answer(self):
        await self.start()
        await self.pcm(b'\0' * 64000)
        rng = random.Random(7)
        await self.pcm(array('h', (rng.randint(-3, 3) for _ in range(32000))).tobytes())
        await asyncio.sleep(.1)
        self.assertEqual(self.asr_calls, [])
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'reply' and e.get('text') == '2' for e in self.events))
        await self.feed('second.wav')
        await self.until(lambda: any(e['type'] == 'reply' and e.get('text') == '5' for e in self.events))
        self.assertEqual(len(self.asr_calls), 2)
        self.assertEqual(len(self.llm_calls), 2)
        self.assertTrue(any(e['type'] == 'user.state' and e['state'] == 'speaking' for e in self.events))

    async def test_real_vad_speech_interrupts_pending_playout_and_answers_new_question(self):
        await self.start()
        self.long_output = self.hold_playout = True
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'audio.end' for e in self.events))
        old = next(e['epoch'] for e in self.events if e['type'] == 'audio')
        self.assertTrue(self.voice.output.pending)
        self.long_output = self.hold_playout = False
        start = len(self.events)
        await self.feed('second.wav')
        await self.until(lambda: any(e['type'] == 'reply' and e.get('text') == '5' for e in self.events[start:]))
        self.assertTrue(any(e['type'] == 'audio.cancel' for e in self.events[start:]))
        self.assertTrue(any(e['type'] == 'audio' and e['epoch'] > old for e in self.events[start:]))

    async def test_limit_stops_before_next_llm_and_idle_returns_standby(self):
        await self.start(automatic_max_turns=1)
        await self.feed('first.wav')
        await self.until(lambda: any(e['type'] == 'reply' for e in self.events))
        await self.feed('second.wav')
        await self.until(lambda: any(e['type'] == 'sleep' and e['code'] == 'turn_limit' for e in self.events))
        self.assertEqual(len(self.llm_calls), 1)

    async def test_idle_emits_sleep_without_model_calls(self):
        await self.start(automatic_idle_seconds=.05)
        await self.until(lambda: any(e['type'] == 'sleep' and e['code'] == 'idle' for e in self.events), timeout=1)
        self.assertEqual(self.asr_calls, [])
        self.assertEqual(self.llm_calls, [])

    async def test_continuous_speech_limit_emits_sleep_before_asr_or_llm(self):
        await self.start(automatic_utterance_seconds=.05)
        with wave.open(str(FIXTURES / 'first.wav'), 'rb') as f:
            pcm = f.readframes(f.getnframes())
        await self.pcm(pcm)
        await self.until(lambda: any(e['type'] == 'sleep' and e['code'] == 'utterance_limit' for e in self.events), timeout=1)
        self.assertEqual(self.llm_calls, [])

    async def test_delayed_first_asr_cannot_answer_obsolete_question_after_new_speech(self):
        self.hold_first_asr = True
        await self.start()
        try:
            await self.feed('first.wav')
            await asyncio.wait_for(self.first_asr_waiting.wait(), 2)
            await self.feed('second.wav')
            self.release_first_asr.set()
            await self.until(lambda: any(e['type'] == 'reply' and e.get('text') == '5' for e in self.events))
            self.assertFalse(any(e['type'] == 'reply' and e.get('text') == '2' for e in self.events))
            self.assertEqual(len(self.llm_calls), 1)
        finally:
            self.release_first_asr.set()

    async def test_three_quick_utterances_retire_both_old_asr_and_only_answer_latest(self):
        self.hold_first_asr = self.hold_second_asr = True
        await self.start()
        try:
            await self.feed('first.wav')
            await asyncio.wait_for(self.first_asr_waiting.wait(), 2)
            await self.feed('second.wav')
            # The second recognition must start while the first provider is
            # still withholding its terminal. This also verifies real closure.
            await asyncio.wait_for(self.second_asr_waiting.wait(), 2)
            await self.feed('first.wav')
            await self.until(lambda: any(e['type'] == 'reply' and e.get('text') == '2' for e in self.events))
            self.assertEqual(len(self.llm_calls), 1)
            self.assertEqual(self.asr_closed, {0, 1})
            self.assertEqual([e['count'] for e in self.events if e['type'] == 'turn'], [1])
        finally:
            self.release_first_asr.set()
            self.release_second_asr.set()
