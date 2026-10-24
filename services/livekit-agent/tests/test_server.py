import unittest
import asyncio
import base64
import tempfile
import json
from dataclasses import replace
from pathlib import Path
from unittest.mock import patch

from aiohttp import WSServerHandshakeError
from aiohttp.test_utils import TestClient, TestServer

from family_voice.config import Settings
from family_voice.server import create_app


class TestVoice:
    def __init__(self, cfg, send):
        self.send = send
        self.frames = 0
        self.commits = 0
    async def start(self):
        pass
    async def begin_input(self):
        pass
    def feed(self, pcm):
        self.frames += len(pcm) // 2
    async def commit(self):
        self.commits += 1
        await self.send({'type': 'committed'})
    async def interrupt(self):
        pass
    def receipt(self, event):
        return False
    async def close(self):
        pass


class ServerTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.temp = tempfile.TemporaryDirectory()
        cfg = self.cfg = Settings.from_env({'DASHSCOPE_API_KEY': 'synthetic-key', 'VOICE_CLIENT_TOKEN': 'synthetic-token', 'VOICE_AUDIT_DIR': self.temp.name})
        self.voices = []
        def factory(cfg, send):
            voice = TestVoice(cfg, send)
            self.voices.append(voice)
            return voice
        self.client = TestClient(TestServer(create_app(cfg, session_factory=factory)))
        await self.client.start_server()

    async def asyncTearDown(self):
        await self.client.close()
        self.temp.cleanup()

    async def test_health_and_auth_and_single_session(self):
        response = await self.client.get('/healthz')
        self.assertEqual((await response.json())['active_sessions'], 0)
        with self.assertRaises(WSServerHandshakeError) as denied:
            await self.client.ws_connect('/v1/audio')
        self.assertEqual(denied.exception.status, 401)
        ws = await self.client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
        ready = await ws.receive_json()
        self.assertEqual(ready['type'], 'ready')
        self.assertEqual(ready['session_seconds'], 900)
        self.assertEqual(ready['max_turns'], 8)
        with self.assertRaises(WSServerHandshakeError) as busy:
            await self.client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
        self.assertEqual(busy.exception.status, 409)
        await ws.close()

    async def test_text_record_failure_closes_manual_and_continuous_connections(self):
        class FailedRecordVoice(TestVoice):
            def __init__(self, cfg, send, **options):
                super().__init__(cfg, send)
            async def start(self):
                await self.send({'type': 'error', 'code': 'text_recording_failed'})
        for mode in ('manual', 'continuous'):
            client = TestClient(TestServer(create_app(self.cfg, session_factory=FailedRecordVoice)))
            await client.start_server()
            try:
                ws = await client.ws_connect('/v1/audio?mode=' + mode, headers={'Authorization': 'Bearer synthetic-token'})
                self.assertEqual((await ws.receive_json())['type'], 'ready')
                self.assertEqual((await ws.receive_json())['code'], 'text_recording_failed')
                await asyncio.wait_for(ws.receive(), .2)
                self.assertTrue(ws.closed, 'recording failure must not leave an unrecorded voice connection running')
            finally:
                await client.close()

    async def test_text_record_close_failure_is_not_reported_as_normal_disconnect(self):
        from family_voice.conversation_log import ConversationLogError
        class FailedCloseVoice(TestVoice):
            async def close(self):
                raise ConversationLogError('PRIVATE_FIXTURE_ERROR')
        client = TestClient(TestServer(create_app(self.cfg, session_factory=FailedCloseVoice)))
        await client.start_server()
        try:
            ws = await client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
            await ws.receive_json()
            await ws.close()
            async with asyncio.timeout(1):
                while (await (await client.get('/healthz')).json())['active_sessions']:
                    await asyncio.sleep(.01)
            records = [json.loads(p.read_text()) for p in Path(self.temp.name).glob('*.json')]
            session = next(e for e in records if e['stage'] == 'client_session')
            self.assertEqual(session['state'], 'text_recording_failed')
            self.assertNotIn('PRIVATE_FIXTURE_ERROR', json.dumps(session))
        finally:
            await client.close()

    async def test_text_record_failure_closes_even_with_saturated_runtime_send_jobs(self):
        from family_voice.content_runtime import DrainVoiceSession
        voices = []
        def factory(cfg, send):
            voice = DrainVoiceSession(cfg, send)
            voices.append(voice)
            return voice
        client = TestClient(TestServer(create_app(self.cfg, drain_factory=factory)))
        await client.start_server()
        try:
            ws = await client.ws_connect('/v1/audio?mode=drain', headers={'Authorization': 'Bearer synthetic-token'})
            self.assertEqual((await ws.receive_json())['type'], 'ready')
            voice = voices[0]
            for _ in range(32):
                voice.jobs.add(asyncio.create_task(asyncio.sleep(60)))
            voice._recording_failed()
            async with asyncio.timeout(.5):
                while True:
                    event = await ws.receive_json()
                    if event.get('code') == 'text_recording_failed':
                        break
                await ws.receive()
            self.assertTrue(ws.closed)
        finally:
            await client.close()

    async def test_text_record_failure_closes_even_if_error_message_write_blocks(self):
        from family_voice.content_runtime import DrainVoiceSession
        from aiohttp import web
        original = web.WebSocketResponse.send_json
        async def blocked(socket, data, *args, **kwargs):
            if data.get('code') == 'text_recording_failed':
                await asyncio.Event().wait()
            return await original(socket, data, *args, **kwargs)
        voices = []
        def factory(cfg, send):
            voice = DrainVoiceSession(cfg, send)
            voices.append(voice)
            return voice
        client = TestClient(TestServer(create_app(self.cfg, drain_factory=factory)))
        await client.start_server()
        try:
            with patch.object(web.WebSocketResponse, 'send_json', new=blocked):
                ws = await client.ws_connect('/v1/audio?mode=drain', headers={'Authorization': 'Bearer synthetic-token'})
                self.assertEqual((await ws.receive_json())['type'], 'ready')
                voices[0]._recording_failed()
                async with asyncio.timeout(1):
                    while not ws.closed:
                        await ws.receive()
                self.assertTrue(ws.closed)
        finally:
            await client.close()

    async def test_input_identity_and_malformed_audio_close_connection(self):
        ws = await self.client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
        await ws.receive_json()
        await ws.send_json({'type': 'input.start'})
        ready = await ws.receive_json()
        self.assertEqual(ready['type'], 'input.ready')
        await ws.send_json({'type': 'input.audio', 'input_id': ready['input_id'] - 1, 'data': 'AAA='})
        result = await ws.receive_json()
        self.assertEqual(result['type'], 'error')
        self.assertEqual(result['code'], 'invalid_input')
        await ws.close()

    async def test_close_failure_releases_single_session_lease(self):
        original = TestVoice.close
        async def failing_close(self):
            raise RuntimeError('synthetic close failure')
        TestVoice.close = failing_close
        try:
            ws = await self.client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
            await ws.receive_json()
            await ws.close()
            async with asyncio.timeout(1):
                while (await (await self.client.get('/healthz')).json())['active_sessions']:
                    await asyncio.sleep(.01)
        finally:
            TestVoice.close = original

    async def test_commit_failure_is_reported_instead_of_silent_wait(self):
        original = TestVoice.commit
        async def fail(self):
            raise RuntimeError('synthetic commit failure')
        TestVoice.commit = fail
        try:
            ws = await self.client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
            await ws.receive_json()
            await ws.send_json({'type': 'input.start'})
            ready = await ws.receive_json()
            await ws.send_json({'type': 'input.audio', 'input_id': ready['input_id'], 'data': base64.b64encode(b'\0' * 640).decode()})
            await ws.send_json({'type': 'input.end', 'input_id': ready['input_id']})
            event = await asyncio.wait_for(ws.receive_json(), .5)
            self.assertEqual(event['code'], 'commit_failed')
            await ws.close()
        finally:
            TestVoice.commit = original

    async def test_cancel_retires_old_input_and_next_start_still_works(self):
        ws = await self.client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
        await ws.receive_json()
        await ws.send_json({'type': 'input.start'})
        first = (await ws.receive_json())['input_id']
        packet = {'type': 'input.audio', 'input_id': first, 'data': base64.b64encode(b'\0' * 640).decode()}
        await ws.send_json(packet)
        await ws.send_json({'type': 'cancel'})
        self.assertEqual((await ws.receive_json())['type'], 'cancelled')
        await ws.send_json(packet)
        self.assertEqual((await asyncio.wait_for(ws.receive_json(), .5))['code'], 'invalid_input')
        await ws.send_json({'type': 'input.end', 'input_id': first})
        self.assertEqual((await asyncio.wait_for(ws.receive_json(), .5))['code'], 'invalid_input')
        self.assertEqual(self.voices[0].commits, 0)
        await ws.send_json({'type': 'input.start'})
        second = (await ws.receive_json())['input_id']
        self.assertGreater(second, first)
        await ws.send_json({**packet, 'input_id': second})
        await ws.send_json({'type': 'input.end', 'input_id': second})
        self.assertEqual((await asyncio.wait_for(ws.receive_json(), .5))['type'], 'committed')
        await ws.close()

    async def test_session_deadline_has_explicit_reason_and_releases_lease(self):
        client = TestClient(TestServer(create_app(self.cfg, session_factory=TestVoice, session_seconds=.06)))
        await client.start_server()
        try:
            ws = await client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
            ready = await ws.receive_json()
            event = await asyncio.wait_for(ws.receive_json(), .5)
            self.assertEqual(event['code'], 'session_expired')
            self.assertEqual(ready['session_seconds'], .06)
            await ws.close()
            async with asyncio.timeout(1):
                while (await (await client.get('/healthz')).json())['active_sessions']:
                    await asyncio.sleep(.01)
            records = [json.loads(p.read_text()) for p in Path(self.temp.name).glob('*.json')]
            self.assertTrue(any(r['stage'] == 'client_session' and r['state'] == 'session_expired' for r in records))
        finally:
            await client.close()

    async def test_operation_timeout_is_not_mislabelled_as_session_expiry(self):
        class TimedOutVoice(TestVoice):
            async def begin_input(self):
                raise TimeoutError()
        client = TestClient(TestServer(create_app(self.cfg, session_factory=TimedOutVoice)))
        await client.start_server()
        try:
            ws = await client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
            await ws.receive_json()
            await ws.send_json({'type': 'input.start'})
            event = await asyncio.wait_for(ws.receive_json(), .5)
            self.assertEqual(event['code'], 'operation_timeout')
            self.assertEqual(event['stage'], 'input.start')
            self.assertEqual(event['error_type'], 'TimeoutError')
            await ws.close()
        finally:
            await client.close()

    async def test_continuous_audio_needs_no_manual_input_identity(self):
        voices = []
        class ListeningVoice(TestVoice):
            def __init__(self, cfg, send, *, automatic=False):
                super().__init__(cfg, send)
                self.automatic = automatic
                voices.append(self)
        client = TestClient(TestServer(create_app(self.cfg, session_factory=ListeningVoice)))
        await client.start_server()
        try:
            ws = await client.ws_connect('/v1/audio?mode=continuous', headers={'Authorization': 'Bearer synthetic-token'})
            ready = await ws.receive_json()
            self.assertEqual(ready['mode'], 'continuous')
            self.assertTrue(voices[0].automatic)
            for _ in range(3):
                await ws.send_json({'type': 'listen.audio', 'data': base64.b64encode(b'\0' * 640).decode()})
            await self.client_until(lambda: voices[0].frames == 960)
            self.assertEqual(voices[0].commits, 0)
            await ws.send_json({'type': 'input.end', 'input_id': 1})
            self.assertEqual((await ws.receive_json())['code'], 'invalid_input')
            await ws.close()
        finally:
            await client.close()

    async def test_drain_is_device_only_and_routes_playback_and_snapshot(self):
        voices = []
        class DrainVoice(TestVoice):
            def __init__(self, cfg, send):
                super().__init__(cfg, send)
                self.started, self.hard_stopped = [], False
                self.output = self
                voices.append(self)
            def mark_started(self, block_id, epoch):
                self.started.append((block_id, epoch))
            def content_snapshot(self):
                return {'main': {'next': 1, 'state': 'paused'}}
            async def hard_stop(self):
                self.hard_stopped = True
        client = TestClient(TestServer(create_app(self.cfg, drain_factory=DrainVoice)))
        await client.start_server()
        try:
            with self.assertRaises(WSServerHandshakeError) as denied:
                await client.ws_connect('/v1/browser/audio?mode=drain', headers={'Authorization': 'Bearer synthetic-token'})
            self.assertIn(denied.exception.status, (400, 401, 403))
            ws = await client.ws_connect('/v1/audio?mode=drain', headers={'Authorization': 'Bearer synthetic-token'})
            ready = await ws.receive_json()
            self.assertEqual(ready['mode'], 'drain')
            self.assertIn('staging', ready['capabilities'])
            await ws.send_json({'type': 'listen.audio', 'data': base64.b64encode(b'\0' * 640).decode()})
            await ws.send_json({'type': 'playback.started', 'block_id': 2, 'epoch': 1})
            await ws.send_json({'type': 'content.snapshot'})
            snapshot = await ws.receive_json()
            self.assertEqual(snapshot['type'], 'content.snapshot')
            self.assertEqual(voices[0].started, [(2, 1)])
            self.assertEqual(voices[0].frames, 320)
            await ws.send_json({'type': 'hard.stop'})
            self.assertEqual((await ws.receive_json())['type'], 'hard.stopped')
            self.assertTrue(voices[0].hard_stopped)
            await ws.close()
        finally:
            await client.close()

    async def test_ready_precedes_startup_events_for_device_handshake(self):
        class StartingVoice(TestVoice):
            async def start(self):
                await self.send({'type': 'agent.state', 'state': 'listening'})
        client = TestClient(TestServer(create_app(self.cfg, session_factory=StartingVoice)))
        await client.start_server()
        try:
            ws = await client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
            self.assertEqual((await ws.receive_json())['type'], 'ready')
            self.assertEqual((await ws.receive_json())['type'], 'agent.state')
            await ws.close()
        finally:
            await client.close()

    async def test_keep_open_drain_ignores_session_budget_until_manual_close(self):
        class KeepVoice(TestVoice):
            def content_snapshot(self): return {}
        cfg = replace(self.cfg, test_keep_open=True)
        client = TestClient(TestServer(create_app(cfg, drain_factory=KeepVoice, session_seconds=.02)))
        await client.start_server()
        try:
            ws = await client.ws_connect('/v1/audio?mode=drain', headers={'Authorization': 'Bearer synthetic-token'})
            ready = await ws.receive_json()
            self.assertTrue(ready['manual_close'])
            self.assertIsNone(ready['session_seconds'])
            self.assertIsNone(ready['max_turns'])
            await asyncio.sleep(.06)
            await ws.send_json({'type': 'content.snapshot'})
            self.assertEqual((await ws.receive_json())['type'], 'content.snapshot')
            await ws.close()
            async with asyncio.timeout(1):
                while (await (await client.get('/healthz')).json())['active_sessions']:
                    await asyncio.sleep(.01)
            legacy = await client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
            self.assertEqual((await legacy.receive_json())['session_seconds'], .02)
            self.assertEqual((await legacy.receive_json())['code'], 'session_expired')
            await legacy.close()
        finally:
            await client.close()

    async def client_until(self, predicate):
        async with asyncio.timeout(1):
            while not predicate():
                await asyncio.sleep(.01)

    async def test_continuous_sleep_closes_socket_and_releases_single_session(self):
        class SleepVoice(TestVoice):
            def __init__(self, cfg, send, *, automatic=False):
                super().__init__(cfg, send)
            def feed(self, pcm):
                asyncio.create_task(self.send({'type': 'sleep', 'code': 'idle'}))
        client = TestClient(TestServer(create_app(self.cfg, session_factory=SleepVoice)))
        await client.start_server()
        try:
            ws = await client.ws_connect('/v1/audio?mode=continuous', headers={'Authorization': 'Bearer synthetic-token'})
            await ws.receive_json()
            await ws.send_json({'type': 'listen.audio', 'data': base64.b64encode(b'\0' * 640).decode()})
            self.assertEqual((await asyncio.wait_for(ws.receive_json(), .5))['type'], 'sleep')
            await asyncio.wait_for(ws.receive(), .5)
            await ws.close()
            async with asyncio.timeout(1):
                while (await (await client.get('/healthz')).json())['active_sessions']:
                    await asyncio.sleep(.01)
        finally:
            await client.close()

    async def test_continuous_rejects_empty_odd_and_oversize_pcm(self):
        def factory(cfg, send, *, automatic=False):
            return TestVoice(cfg, send)
        client = TestClient(TestServer(create_app(self.cfg, session_factory=factory)))
        await client.start_server()
        try:
            for pcm in (b'', b'\0', b'\0' * 642):
                ws = await client.ws_connect('/v1/audio?mode=continuous', headers={'Authorization': 'Bearer synthetic-token'})
                await ws.receive_json()
                await ws.send_json({'type': 'listen.audio', 'data': base64.b64encode(pcm).decode()})
                self.assertEqual((await asyncio.wait_for(ws.receive_json(), .5))['code'], 'invalid_input')
                await ws.close()
                async with asyncio.timeout(1):
                    while (await (await client.get('/healthz')).json())['active_sessions']:
                        await asyncio.sleep(.01)
        finally:
            await client.close()


if __name__ == '__main__':
    unittest.main()
