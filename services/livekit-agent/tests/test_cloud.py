import asyncio
import base64
import unittest
import tempfile

from aiohttp import web

from family_voice.cloud import Cloud, CloudError
from family_voice.config import Settings


class CloudTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.calls = 0
        app = web.Application()
        app.router.add_get('/tts', self.tts)
        app.router.add_get('/asr', self.asr)
        self.runner = web.AppRunner(app)
        await self.runner.setup()
        site = web.TCPSite(self.runner, '127.0.0.1', 0)
        await site.start()
        port = site._server.sockets[0].getsockname()[1]
        self.cfg = Settings.from_env({
            'DASHSCOPE_API_KEY': 'synthetic-test-key',
            'VOICE_ASR_URL': f'ws://127.0.0.1:{port}/asr',
            'VOICE_TTS_URL': f'ws://127.0.0.1:{port}/tts',
            'VOICE_LLM_URL': 'https://example.com/v1',
            'VOICE_AUDIT_DIR': self.temp.name,
        })
        self.bad_audio = False
        self.reject = False
        self.hold_tts = False
        self.tts_waiting, self.release_tts = asyncio.Event(), asyncio.Event()

    async def asyncTearDown(self):
        await self.runner.cleanup()
        self.temp.cleanup()

    async def tts(self, request):
        self.calls += 1
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        await ws.send_json({'type': 'session.created'})
        update = await ws.receive_json()
        self.assertEqual(update['type'], 'session.update')
        self.assertEqual(update['session']['sample_rate'], 24000)
        await ws.send_json({'type': 'session.updated'})
        self.assertEqual((await ws.receive_json())['type'], 'input_text_buffer.append')
        self.assertEqual((await ws.receive_json())['type'], 'input_text_buffer.commit')
        if self.reject:
            await ws.send_json({'type': 'error', 'error': {'code': 'Denied', 'message': 'synthetic-test-key must never escape'}})
        else:
            audio = b'\x01\x00' * (479 if self.bad_audio else 480)
            if self.bad_audio:
                audio += b'\x01'
            await ws.send_json({'type': 'response.audio.delta', 'response_id': 'r1', 'delta': base64.b64encode(audio).decode()})
            if self.hold_tts:
                self.tts_waiting.set()
                await self.release_tts.wait()
                return ws
            await ws.send_json({'type': 'response.done', 'response': {'id': 'r1', 'status': 'completed', 'usage': {'characters': 2}}})
        await ws.close()
        return ws

    async def asr(self, request):
        self.calls += 1
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        run = await ws.receive_json()
        task = run['header']['task_id']
        self.assertEqual(run['payload']['model'], 'fun-asr-realtime')
        self.assertEqual(run['payload']['parameters']['sample_rate'], 16000)
        await ws.send_json({'header': {'event': 'task-started', 'task_id': task}})
        message = await ws.receive()
        self.assertEqual(message.data, b'\0' * 640)
        for sentence in (
            {'heartbeat': True, 'text': 'ignored', 'sentence_end': False},
            {'text': '一加', 'sentence_end': False},
            {'text': '一加一等于几？', 'sentence_end': True},
        ):
            await ws.send_json({'header': {'event': 'result-generated', 'task_id': task}, 'payload': {'output': {'sentence': sentence}}})
        finish = await ws.receive_json()
        self.assertEqual(finish['header']['action'], 'finish-task')
        await ws.send_json({'header': {'event': 'task-finished', 'task_id': task}})
        await ws.close()
        return ws

    async def test_tts_pcm_and_completed_terminal(self):
        cloud = Cloud(self.cfg)
        pcm = b''.join([chunk async for chunk in cloud.tts('你好')])
        self.assertEqual(pcm, b'\x01\x00' * 480)
        self.assertEqual(cloud.last_usage, {'characters': 2})
        self.assertEqual(self.calls, 1)

    async def test_bad_pcm_and_error_do_not_retry_or_expose_error_body(self):
        self.bad_audio = True
        with self.assertRaises(CloudError):
            _ = [chunk async for chunk in Cloud(self.cfg).tts('你好')]
        self.reject, self.bad_audio = True, False
        with self.assertRaises(CloudError) as caught:
            _ = [chunk async for chunk in Cloud(self.cfg).tts('你好')]
        self.assertNotIn('synthetic-test-key', str(caught.exception))
        self.assertEqual(self.calls, 2)

    async def test_asr_heartbeat_and_interim_final_remain_distinct(self):
        async def frames():
            yield b'\0' * 640
        result = [ev async for ev in Cloud(self.cfg).asr(frames())]
        self.assertEqual([(ev['text'], ev['final']) for ev in result], [('一加', False), ('一加一等于几？', True)])

    async def test_cancelled_tts_closes_without_waiting_for_provider_terminal(self):
        self.hold_tts = True
        async def consume():
            return [chunk async for chunk in Cloud(self.cfg).tts('一')]
        job = asyncio.create_task(consume())
        try:
            await asyncio.wait_for(self.tts_waiting.wait(), 1)
            job.cancel()
            done, _ = await asyncio.wait((job,), timeout=.5)
            self.assertIn(job, done, 'cancelled prefetch must not wait ten seconds on WebSocket close')
            self.assertTrue(job.cancelled())
        finally:
            self.release_tts.set()
            await asyncio.gather(job, return_exceptions=True)

    async def test_real_sdk_adapters_preserve_pcm_and_final_transcript(self):
        from livekit import rtc
        from livekit.agents import stt
        from livekit.agents.types import APIConnectOptions
        from family_voice.providers import DashScopeSTT, DashScopeTTS
        tts = DashScopeTTS(self.cfg)
        async with tts.synthesize('你好', conn_options=APIConnectOptions(max_retry=0)) as speech:
            audio = b''.join([bytes(ev.frame.data) async for ev in speech])
        self.assertEqual(audio, b'\x01\x00' * 480)
        recognizer = DashScopeSTT(self.cfg)
        async with recognizer.stream(conn_options=APIConnectOptions(max_retry=0)) as stream:
            stream.push_frame(rtc.AudioFrame(b'\0' * 640, 16000, 1, 320))
            stream.end_input()
            events = [ev async for ev in stream]
        finals = [ev.alternatives[0].text for ev in events if ev.type == stt.SpeechEventType.FINAL_TRANSCRIPT]
        self.assertEqual(finals, ['一加一等于几？'])


class ConfigTests(unittest.TestCase):
    def test_workspace_endpoints_and_hidden_key(self):
        cfg = Settings.from_env({'DASHSCOPE_API_KEY': 'synthetic-secret', 'QWEN_REALTIME_URL': 'wss://workspace.cn-beijing.maas.aliyuncs.com/api-ws/v1/realtime?model=old'})
        self.assertEqual(cfg.asr_url, 'wss://workspace.cn-beijing.maas.aliyuncs.com/api-ws/v1/inference')
        self.assertEqual(cfg.llm_url, 'https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1')
        self.assertIn('model=qwen3-tts-flash-realtime', cfg.tts_url)
        self.assertNotIn('synthetic-secret', repr(cfg))

    def test_nonlocal_plaintext_endpoints_are_rejected(self):
        with self.assertRaises(ValueError):
            Settings.from_env({'DASHSCOPE_API_KEY': 'synthetic-secret', 'VOICE_TTS_URL': 'ws://example.com/tts'})


if __name__ == '__main__':
    unittest.main()
