import asyncio
import unittest

from aiohttp import WSServerHandshakeError
from aiohttp.test_utils import TestClient, TestServer

from family_voice.config import Settings
from family_voice.server import create_app
from test_server import TestVoice


class BrowserTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.cfg = Settings.from_env({'DASHSCOPE_API_KEY': 'synthetic-key', 'VOICE_CLIENT_TOKEN': 'synthetic-token'})
        self.client = TestClient(TestServer(create_app(self.cfg, session_factory=TestVoice)))
        await self.client.start_server()
        self.origin = str(self.client.make_url('')).rstrip('/')

    async def asyncTearDown(self):
        await self.client.close()

    async def ticket(self):
        response = await self.client.post('/v1/browser/ticket', headers={'Authorization': 'Bearer synthetic-token'}, json={'origin': self.origin})
        self.assertEqual(response.status, 200)
        return (await response.json())['ticket']

    async def pair(self):
        ticket = await self.ticket()
        response = await self.client.post('/v1/browser/pair', headers={'Origin': self.origin}, json={'ticket': ticket})
        self.assertEqual(response.status, 200)
        cookie = response.cookies['voice_browser']
        self.assertTrue(cookie['httponly'])
        self.assertEqual(cookie['samesite'], 'Strict')
        return ticket, {'Cookie': f'voice_browser={cookie.value}', 'Origin': self.origin}

    async def test_static_client_and_headers_do_not_expose_server_key(self):
        response = await self.client.get('/client')
        self.assertEqual(response.status, 200)
        body = await response.text()
        self.assertIn('麦克风', body)
        self.assertNotIn('synthetic-token', body)
        self.assertIn("frame-ancestors 'none'", response.headers['Content-Security-Policy'])
        self.assertEqual(response.headers['Cache-Control'], 'no-store')
        response = await self.client.get('/client/not-a-file')
        self.assertEqual(response.status, 404)

    async def test_ticket_needs_bearer_and_is_one_use_and_origin_bound(self):
        response = await self.client.post('/v1/browser/ticket', json={'origin': self.origin})
        self.assertEqual(response.status, 401)
        ticket = await self.ticket()
        response = await self.client.post('/v1/browser/pair', headers={'Origin': 'https://example.org'}, json={'ticket': ticket})
        self.assertEqual(response.status, 403)
        response = await self.client.post('/v1/browser/pair', headers={'Origin': self.origin}, json={'ticket': ticket})
        self.assertEqual(response.status, 200)
        response = await self.client.post('/v1/browser/pair', headers={'Origin': self.origin}, json={'ticket': ticket})
        self.assertEqual(response.status, 401)

    async def test_browser_ws_requires_cookie_origin_and_shares_session_lease(self):
        _, headers = await self.pair()
        self.client.session.cookie_jar.clear()
        with self.assertRaises(WSServerHandshakeError):
            await self.client.ws_connect('/v1/browser/audio', headers={'Origin': self.origin})
        with self.assertRaises(WSServerHandshakeError):
            await self.client.ws_connect('/v1/browser/audio', headers={**headers, 'Origin': 'https://example.org'})
        ws = await self.client.ws_connect('/v1/browser/audio', headers=headers)
        self.assertEqual((await ws.receive_json())['type'], 'ready')
        with self.assertRaises(WSServerHandshakeError) as busy:
            await self.client.ws_connect('/v1/audio', headers={'Authorization': 'Bearer synthetic-token'})
        self.assertEqual(busy.exception.status, 409)
        await ws.close()

    async def test_expired_ticket_cookie_and_dns_rebinding_are_rejected(self):
        from family_voice.browser import GATE
        gate = self.client.server.app[GATE]
        ticket = await self.ticket()
        real_clock = gate.clock
        gate.clock = lambda: real_clock() + 121
        response = await self.client.post('/v1/browser/pair', headers={'Origin': self.origin}, json={'ticket': ticket})
        self.assertEqual(response.status, 401)
        gate.clock = real_clock
        _, headers = await self.pair()
        gate.clock = lambda: real_clock() + 301
        with self.assertRaises(WSServerHandshakeError):
            await self.client.ws_connect('/v1/browser/audio', headers=headers)
        response = await self.client.get('/client', headers={'Host': 'evil.example'})
        self.assertEqual(response.status, 403)


if __name__ == '__main__':
    unittest.main()
