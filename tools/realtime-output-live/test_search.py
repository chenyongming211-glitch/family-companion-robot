"""Search session contract without network access or billable calls."""
import asyncio
import base64
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from state import context_for
from providers import Native as BaseNative
from providers import ProbeError
import upstream
from upstream import StreamingNative
from server import Session, instructions_for
from test_identity import snapshot
from test_streaming import Socket


class ProviderSocket:
    def __init__(self):
        self.sent = []
        self.events = asyncio.Queue()
        self.events.put_nowait({'type': 'session.created', 'session': {}})
        self.response_events = []
        self.ignore_search = False

    async def send_json(self, event):
        self.sent.append(event)
        kind = event['type']
        if kind == 'session.update':
            session = dict(event['session'])
            if self.ignore_search:
                session['enable_search'] = False
            self.events.put_nowait({'type': 'session.updated', 'session': session})
        elif kind == 'input_audio_buffer.commit':
            self.events.put_nowait({'type': 'input_audio_buffer.committed'})
        elif kind == 'response.create':
            for value in self.response_events:
                self.events.put_nowait(value)

    async def receive_json(self, **_):
        return await self.events.get()

    async def close(self):
        pass


class ProviderClient:
    def __init__(self, socket):
        self.socket = socket

    async def ws_connect(self, *_args, **_kwargs):
        return self.socket

    async def close(self):
        pass


class SearchTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        self.cfg = {'QWEN_REALTIME_URL': 'wss://private.example/api?model=qwen3.8-omni-flash-realtime',
            'DASHSCOPE_API_KEY': 'synthetic-key', 'enable_search': True}

    async def asyncTearDown(self):
        self.directory.cleanup()

    async def test_generation_requests_search_and_sources_in_initial_session(self):
        socket = ProviderSocket()
        native = StreamingNative(self.cfg, self.root)
        with patch('providers.aiohttp.ClientSession', return_value=ProviderClient(socket)):
            await native.open('合成测试')
        try:
            session = socket.sent[0]['session']
            self.assertTrue(session.get('enable_search'), 'generation session must enable native search')
            self.assertTrue(session.get('search_options', {}).get('enable_source'), 'request provider sources')
            self.assertNotIn('tools', session)
        finally:
            await native.close()

    async def test_vad_listener_does_not_enable_search_or_mutate_generation_config(self):
        session = Session(Socket(), self.cfg, self.root)
        socket = ProviderSocket()
        try:
            with patch('providers.aiohttp.ClientSession', return_value=ProviderClient(socket)):
                await session.listener.listen()
            initial = socket.sent[0]['session']
            self.assertIs(initial.get('enable_search'), False)
            self.assertTrue(self.cfg['enable_search'])
            self.assertFalse(socket.sent[-1]['session']['turn_detection']['create_response'])
        finally:
            await session.listener.close()
            session.input_codec.close()

    async def test_legacy_probe_does_not_gain_search_by_default(self):
        socket = ProviderSocket()
        native = BaseNative({k: v for k, v in self.cfg.items() if k != 'enable_search'}, self.root)
        with patch('providers.aiohttp.ClientSession', return_value=ProviderClient(socket)):
            await native.open('旧探针')
        try:
            self.assertFalse(socket.sent[0]['session'].get('enable_search', False))
        finally:
            await native.close()

    def test_identity_tracks_search_switch_and_connection_state(self):
        data = snapshot(instructions_for(self.cfg, context_for([])))
        self.assertEqual(data['capabilities']['live_information']['status'], 'available')
        disabled = snapshot(instructions_for({**self.cfg, 'enable_search': False}, context_for([])))
        self.assertEqual(disabled['capabilities']['live_information']['status'], 'not_connected')
        disconnected = snapshot(instructions_for(self.cfg, context_for([]), connected=False))
        self.assertEqual(disconnected['capabilities']['live_information']['status'], 'temporarily_unavailable')

    async def test_search_meter_is_retained_in_private_audit_and_turn_callback(self):
        socket = ProviderSocket()
        socket.response_events = [
            {'type': 'response.created', 'response': {'id': 'r1'}},
            {'type': 'response.audio.delta', 'response_id': 'r1', 'delta': base64.b64encode(bytes(2880)).decode()},
            {'type': 'response.done', 'response': {'id': 'r1', 'status': 'completed',
                'usage': {'total_tokens': 8, 'plugins': {'search': {'count': 1, 'strategy': 'agent'}}},
                'search_info': {'search_results': [{'title': '气象来源', 'url': 'https://weather.example/forecast', 'index': 1}]}}},
        ]
        native, observed = StreamingNative(self.cfg, self.root), []
        native.on_search = observed.append
        with patch('providers.aiohttp.ClientSession', return_value=ProviderClient(socket)):
            await native.open('合成测试')
        try:
            events = [event async for event in native.stream(bytes(640))]
            self.assertEqual(events[0][0], 'pcm')
            completed = [e for e in observed if e.get('phase') == 'completed']
            self.assertEqual(len(completed), 1)
            self.assertEqual(completed[0]['count'], 1)
            self.assertEqual(completed[0].get('sources'), [{'title': '气象来源', 'url': 'https://weather.example/forecast', 'index': 1}])
            audit = json.loads(next(self.root.glob('live_native_turn-*.json')).read_text())
            self.assertEqual(audit['search']['count'], 1)
            self.assertEqual(audit['search']['sources'][0]['url'], 'https://weather.example/forecast')
        finally:
            await native.close()

    async def test_server_must_acknowledge_requested_search_before_audio_submission(self):
        socket = ProviderSocket()
        socket.ignore_search = True
        native = StreamingNative(self.cfg, self.root)
        with patch('providers.aiohttp.ClientSession', return_value=ProviderClient(socket)):
            with self.assertRaisesRegex(ProbeError, 'search_setting_not_confirmed'):
                await native.open('合成测试')
        await native.close()
        self.assertFalse(any(e['type'] == 'input_audio_buffer.commit' for e in socket.sent))

    async def test_bad_optional_search_metadata_does_not_break_audio(self):
        socket = ProviderSocket()
        socket.response_events = [
            {'type': 'response.created', 'response': {'id': 'r1'}},
            {'type': 'response.audio.delta', 'response_id': 'r1', 'delta': base64.b64encode(bytes(2880)).decode()},
            {'type': 'response.done', 'response': {'id': 'r1', 'status': 'completed',
                'usage': {'plugins': {'search': None}}, 'search_info': None}},
        ]
        native = StreamingNative(self.cfg, self.root)
        with patch('providers.aiohttp.ClientSession', return_value=ProviderClient(socket)):
            await native.open('合成测试')
        try:
            self.assertEqual([kind async for kind, _ in native.stream(bytes(640))], ['pcm'])
        finally:
            await native.close()

    async def test_search_and_tools_conflict_is_rejected_before_connecting(self):
        socket = ProviderSocket()
        native = BaseNative({**self.cfg, 'tools': [{'type': 'function'}]}, self.root)
        try:
            with patch('providers.aiohttp.ClientSession', return_value=ProviderClient(socket)) as factory:
                with self.assertRaisesRegex(ProbeError, 'search_tools_incompatible'):
                    await native.open('合成测试')
            factory.assert_not_called()
        finally:
            await native.close()

    def test_live_entry_defaults_on_and_can_explicitly_disable_search(self):
        load = getattr(upstream, 'settings', None)
        self.assertTrue(callable(load), 'live entry must own its search configuration')
        with patch('upstream.provider_settings', return_value={}), patch.dict(os.environ, {}, clear=True):
            self.assertTrue(load()['enable_search'])
            with patch.dict(os.environ, {'OUTPUT_LIVE_ENABLE_SEARCH': '0'}):
                self.assertFalse(load()['enable_search'])


if __name__ == '__main__':
    unittest.main()
