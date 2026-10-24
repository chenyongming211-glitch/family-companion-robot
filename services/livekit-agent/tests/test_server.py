import unittest
import asyncio
import base64

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
        cfg = Settings.from_env({'DASHSCOPE_API_KEY': 'synthetic-key', 'VOICE_CLIENT_TOKEN': 'synthetic-token'})
        self.voices = []
        def factory(cfg, send):
            voice = TestVoice(cfg, send)
            self.voices.append(voice)
            return voice
        self.client = TestClient(TestServer(create_app(cfg, session_factory=factory)))
        await self.client.start_server()

    async def asyncTearDown(self):
        await self.client.close()

    async def test_health_and_auth_and_single_session(self):
        response = await self.client.get('/healthz')
        self.assertEqual((await response.json())['active_sessions'], 0)
        with self.assertRaises(WSServerHandshakeError) as denied:
            await self.client.ws_connect('/v1/audio')
        self.assertEqual(denied.exception.status, 401)
        ws = await self.client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
        self.assertEqual((await ws.receive_json())['type'], 'ready')
        with self.assertRaises(WSServerHandshakeError) as busy:
            await self.client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
        self.assertEqual(busy.exception.status, 409)
        await ws.close()

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


if __name__ == '__main__':
    unittest.main()
