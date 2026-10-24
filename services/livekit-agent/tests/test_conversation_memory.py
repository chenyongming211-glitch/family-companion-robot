import asyncio
import json
from pathlib import Path
import tempfile
import unittest

from family_voice.conversation_log import ConversationLog

try:
    from family_voice.conversation_memory import ConversationMemory
except ImportError:
    ConversationMemory = None


class ConversationMemoryTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.directory = Path(self.temp.name) / 'conversations'
        self.log = await ConversationLog.open(self.directory)

    async def asyncTearDown(self):
        await self.log.close()
        self.temp.cleanup()

    def memory(self):
        self.assertIsNotNone(ConversationMemory, 'archive memory is not implemented')
        return ConversationMemory(self.directory, self.log.id)

    def add(self, kind, text, **fields):
        self.assertTrue(self.log.append(kind, text=text, **fields))

    async def test_only_confirmed_speech_and_user_words_enter_recall(self):
        self.add('user.transcript', '我喜欢蓝色。', role='user', input_epoch=1)
        self.add('assistant.generated', '未播秘密。', role='assistant')
        self.add('assistant.content', '未播秘密。', role='assistant')
        self.add('assistant.delivery_unknown', '部分句子。', role='assistant', unit_id='partial')
        self.add('assistant.played', '记住蓝色了。', role='assistant', input_epoch=1, unit_id='one')
        await self.log.flush()
        result = self.memory().recall('read', 'current')
        text = json.dumps(result, ensure_ascii=False)
        self.assertIn('我喜欢蓝色', text)
        self.assertIn('记住蓝色了', text)
        self.assertNotIn('未播秘密', text)
        self.assertIn('not_fully_confirmed', text)

    async def test_late_confirmation_replaces_unknown_for_same_unit(self):
        self.add('assistant.delivery_unknown', '这句后来完整播完。', role='assistant', unit_id='same')
        self.add('assistant.played', '这句后来完整播完。', role='assistant', unit_id='same')
        await self.log.flush()
        rows = self.memory().recall('read', 'current')['records']
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]['delivery'], 'confirmed')

    async def test_long_context_has_bounded_extractive_summary_and_old_original_is_readable(self):
        self.add('user.transcript', '早期事实：小猫叫松果。', role='user', input_epoch=1)
        self.add('assistant.played', '松果喜欢森林。', role='assistant', unit_id='old', input_epoch=1)
        for index in range(180):
            self.add('user.transcript', f'第{index}轮。' + '其他内容。' * 28, role='user', input_epoch=index + 2)
            await self.log.flush()
        await self.log.flush()
        memory = self.memory()
        context = memory.context('小猫叫什么')
        encoded = json.dumps(context, ensure_ascii=False)
        self.assertLessEqual(len(encoded), 18000)
        self.assertEqual(context['current']['summary']['kind'], 'extractive')
        self.assertIn('松果', encoded)
        self.assertIn('松果', json.dumps(memory.recall('read', 'current'), ensure_ascii=False))

    async def test_previous_session_is_loaded_after_reconnect_without_restoring_playback(self):
        old = await ConversationLog.open(self.directory)
        old.append('user.transcript', text='上次我们谈了露营。', role='user')
        old.append('assistant.played', text='可以去湖边。', role='assistant', unit_id='old')
        await old.close()
        result = self.memory().context('上次聊了什么')
        self.assertIn('露营', json.dumps(result, ensure_ascii=False))
        self.assertNotIn('resume_from', json.dumps(result))

    async def test_paging_search_and_unreadable_files_are_explicit(self):
        for index in range(25):
            self.add('user.transcript', f'保存的第{index}条，提到松果。', role='user')
        await self.log.flush()
        memory = self.memory()
        page = memory.recall('read', 'current')
        self.assertLessEqual(len(page['records']), 20)
        self.assertTrue(page['has_more'])
        second = memory.recall('read', 'current', after=page['next_after'])
        self.assertEqual(len(second['records']), 5)
        result = memory.recall('search', 'current', query='松果')
        self.assertTrue(result['records'])
        self.assertLessEqual(len(json.dumps(result, ensure_ascii=False)), 12000)
        unsafe = memory.recall('read', '../../secret')
        self.assertEqual(unsafe['status'], 'unavailable')
        broken = 'a' * 32
        (self.directory / (broken + '.jsonl')).write_text('invalid\n')
        self.assertEqual(memory.recall('read', broken)['status'], 'unavailable')

    async def test_large_escaped_record_can_be_read_in_bounded_lossless_pages(self):
        text = '中"\\\n' * 7000
        self.add('user.transcript', text, role='user')
        await self.log.flush()
        memory, after, offset, pieces = self.memory(), 0, 0, []
        for _ in range(12):
            page = memory.recall('read', 'current', after=after, offset=offset)
            self.assertLessEqual(len(json.dumps(page, ensure_ascii=False)), 12000)
            pieces.extend(r['text'] for r in page['records'])
            if not page['has_more']:
                break
            after, offset = page['next_after'], page['next_offset']
        self.assertEqual(''.join(pieces), text)
        search = memory.recall('search', 'all')
        self.assertLessEqual(len(json.dumps(search, ensure_ascii=False)), 12000)

    async def test_archive_reader_uses_a_fixed_file_snapshot_during_append(self):
        from unittest.mock import patch
        import family_voice.conversation_memory as module
        await self.log.flush()
        original_parse, appended = module._parse_line, False
        def append_after_stat(line, identity, sequence):
            nonlocal appended
            event = original_parse(line, identity, sequence)
            if not appended:
                appended = True
                other = {**event, 'seq': 2, 'kind': 'user.transcript', 'role': 'user', 'text': '稍后追加'}
                with (self.directory / (identity + '.jsonl')).open('ab') as file:
                    file.write((json.dumps(other) + '\n').encode())
            return event
        with patch.object(module, '_parse_line', append_after_stat):
            rows = self.memory().recall('read', 'current')['records']
        self.assertEqual(rows, [], 'read must not chase records appended after its snapshot')

    async def test_symlink_and_oversize_archive_are_unavailable(self):
        from family_voice.conversation_memory import MAX_FILE_BYTES
        external = Path(self.temp.name) / 'external'
        external.write_text('private')
        (self.directory / ('b' * 32 + '.jsonl')).symlink_to(external)
        self.assertEqual(self.memory().recall('read', 'b' * 32)['status'], 'unavailable')
        huge = self.directory / ('c' * 32 + '.jsonl')
        with huge.open('wb') as file:
            file.truncate(MAX_FILE_BYTES + 1)
        self.assertEqual(self.memory().recall('read', 'c' * 32)['status'], 'unavailable')

    async def test_session_listing_pages_back_beyond_twenty_sessions(self):
        for index in range(23):
            older = await ConversationLog.open(self.directory)
            await older.close()
        first = self.memory().recall('list')
        self.assertTrue(first.get('has_more'), 'older sessions must remain discoverable')
        second = self.memory().recall('list', first['next_session_id'])
        self.assertTrue(second['sessions'])
        self.assertFalse({r['session_id'] for r in first['sessions']} &
                         {r['session_id'] for r in second['sessions']})

    async def test_global_search_returns_scoped_continuations_instead_of_global_sequence(self):
        self.add('user.transcript', '旧会话松果。', role='user')
        await self.log.flush()
        newer = await ConversationLog.open(self.directory)
        for index in range(20):
            newer.append('user.transcript', role='user', text=f'新会话松果{index}')
        await newer.close()
        memory = self.memory()
        page = memory.recall('search', 'all', query='松果')
        self.assertTrue(page['has_more'])
        continuations = page.get('continuations', [])
        self.assertTrue(any(c['session_id'] == self.log.id and c['after'] == 0 for c in continuations))
        invalid = memory.recall('search', 'all', query='松果', after=page['next_after'])
        self.assertNotEqual(invalid['status'], 'ok', 'per-file seq must never silently filter other sessions')
        old = memory.recall('search', self.log.id, query='松果')
        self.assertIn('旧会话松果', json.dumps(old, ensure_ascii=False))
