"""Identity delivered to each native turn; public synthetic context only."""
import asyncio
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import server
from server import Session
from state import Turn, context_for
from test_streaming import Native, ASR, Socket


def snapshot(instructions):
    _, marker, remaining = instructions.partition('身份与当前能力：\n')
    if not marker:
        raise AssertionError('native turn has no product/capability snapshot')
    return json.JSONDecoder().raw_decode(remaining)[0]


class IdentityTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.cfg = {'model': 'qwen3.8-omni-flash-realtime', 'DASHSCOPE_API_KEY': 'private-test-key'}
        self.sessions = []
        self.session = self.new_session()

    def new_session(self):
        session = Session(Socket(), self.cfg, Path(self.directory.name))
        self.sessions.append(session)
        return session

    async def asyncTearDown(self):
        for session in self.sessions:
            for task in list(session.tasks):
                task.cancel()
            await asyncio.gather(*session.tasks, return_exceptions=True)
            session.input_codec.close()
        self.directory.cleanup()

    async def ask(self, session, question='你是谁？'):
        turn = Turn(len(session.history) + 1)
        turn.user, turn.stopped_at = question, asyncio.get_running_loop().time()
        session.history.append(turn)
        session.output = turn
        native = Native('我是小满。')
        with patch('server.StreamingNative', return_value=native) as factory, patch('server.OutputASR', ASR):
            await session.reply(turn, bytes(640))
        self.assertTrue(turn.ended)
        self.assertEqual(factory.call_count, 1, 'identity must not add another model generation')
        return native.instructions

    async def test_actual_turn_receives_product_identity_and_known_model_source(self):
        instructions = await self.ask(self.session)
        self.assertIn('小满', instructions)
        data = snapshot(instructions)
        self.assertEqual(data['product']['name'], '小满')
        self.assertIsNone(data['product']['developer'])
        self.assertIsNone(data['product']['owner'])
        self.assertEqual(data['model']['id'], self.cfg['model'])
        self.assertEqual(data['model']['provider'], '阿里云通义千问')
        self.assertNotIn('private-test-key', instructions)

    async def test_repeated_turn_and_new_session_keep_identity_with_fresh_history_scope(self):
        first = snapshot(await self.ask(self.session))
        second = snapshot(await self.ask(self.session, '那你叫什么名字？'))
        reconnected = snapshot(await self.ask(self.new_session()))
        self.assertEqual([x['product']['name'] for x in (first, second, reconnected)], ['小满'] * 3)
        self.assertEqual([x['memory']['included_turns'] for x in (first, second, reconnected)], [0, 1, 0])

    async def test_past_wrong_name_remains_history_without_overriding_current_identity(self):
        old = Turn(1)
        old.user, old.generated = '你是谁？', '我叫小伴。'
        old.sent = old.completed = 1440
        old.ended = True
        self.session.history.append(old)
        instructions = await self.ask(self.session, '你到底是谁？')
        self.assertEqual(snapshot(instructions)['product']['name'], '小满')
        self.assertNotIn('小伴', instructions.split('背景：', 1)[0])
        history = json.loads(instructions.split('背景：', 1)[1])
        self.assertEqual(history['recent_turns'][0]['assistant_spoken_prefix'], '我叫小伴。')

    async def test_capability_snapshot_does_not_inherit_other_entry_tools(self):
        data = snapshot(await self.ask(self.session, '你能做什么？'))
        caps = data['capabilities']
        self.assertEqual(caps['voice_chat']['status'], 'available')
        self.assertEqual(caps['recent_conversation']['status'], 'testing')
        self.assertEqual(caps['story_resume']['status'], 'testing')
        for key in ('full_history_search', 'cross_session_memory', 'reminders', 'image_analysis', 'live_information', 'device_control', 'physical_actions'):
            self.assertEqual(caps[key]['status'], 'not_connected')

    def test_unknown_model_does_not_invent_provider_or_leak_endpoint(self):
        build = getattr(server, 'instructions_for', None)
        self.assertTrue(callable(build), 'server must compose current identity for each turn')
        cfg = {'QWEN_REALTIME_URL': 'wss://private.example/api?model=other-model', 'DASHSCOPE_API_KEY': 'private-test-key'}
        instructions = build(cfg, context_for([]))
        data = snapshot(instructions)
        self.assertEqual(data['model']['id'], 'other-model')
        self.assertIsNone(data['model']['provider'])
        self.assertNotIn('private.example', instructions)
        self.assertNotIn('private-test-key', instructions)

    def test_disconnected_snapshot_downgrades_available_voice_capabilities(self):
        build = getattr(server, 'instructions_for', None)
        self.assertTrue(callable(build), 'server must compose current identity for each turn')
        data = snapshot(build(self.cfg, context_for([]), connected=False))
        self.assertEqual(data['capabilities']['voice_chat']['status'], 'temporarily_unavailable')
        self.assertEqual(data['capabilities']['reminders']['status'], 'not_connected')

    def test_memory_scope_matches_actual_context_window(self):
        build = getattr(server, 'instructions_for', None)
        self.assertTrue(callable(build), 'server must compose current identity for each turn')
        context = context_for([Turn(i) for i in range(20)])
        data = snapshot(build(self.cfg, context))
        self.assertEqual(data['memory']['included_turns'], len(json.loads(context)['recent_turns']))
        self.assertEqual(data['memory']['max_recent_turns'], 16)
        self.assertTrue(data['memory']['older_turns_omitted'])

    def test_audio_entry_does_not_advertise_an_image_input_channel(self):
        build = getattr(server, 'instructions_for', None)
        self.assertTrue(callable(build))
        data = snapshot(build(self.cfg, context_for([])))
        self.assertEqual(data.get('input_channels'), ['audio'])

    def test_server_text_retention_is_distinct_from_model_archive_access(self):
        build = getattr(server, 'instructions_for', None)
        self.assertTrue(callable(build))
        memory = snapshot(build(self.cfg, context_for([])))['memory']
        self.assertTrue(memory.get('server_persists_text', False))
        self.assertFalse(memory.get('can_query_prior_sessions', True))
        self.assertFalse(memory.get('server_saves_raw_audio', True))


if __name__ == '__main__':
    unittest.main()
