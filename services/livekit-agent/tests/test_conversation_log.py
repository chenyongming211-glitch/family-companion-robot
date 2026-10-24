"""Private text archives are durable, bounded, and honest about playback."""
import asyncio
import contextlib
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import threading
import unittest
from unittest.mock import patch
import uuid

try:
    from family_voice import conversation_log as logs
except ImportError:
    logs = None


class ConversationLogTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.assertIsNotNone(logs, 'private conversation text archive is not implemented')
        self.temporary = tempfile.TemporaryDirectory()
        self.directory = Path(self.temporary.name) / 'records'
        self.opened = []

    async def asyncTearDown(self):
        for archive in getattr(self, 'opened', []):
            with contextlib.suppress(logs.ConversationLogError):
                await archive.close()
        if hasattr(self, 'temporary'):
            self.temporary.cleanup()

    async def open(self, **kwargs):
        archive = await logs.ConversationLog.open(self.directory, **kwargs)
        self.opened.append(archive)
        return archive

    def events(self, archive, **kwargs):
        return logs.read_conversation(self.directory, archive.id, **kwargs)['events']

    async def wait_for_thread(self, event):
        self.assertTrue(await asyncio.to_thread(event.wait, 2), 'writer did not reach test gate')

    async def test_reopen_preserves_unicode_order_and_all_event_fields(self):
        archive = await self.open(source='fixture')
        self.assertRegex(archive.id, r'^[0-9a-f]{32}$')
        self.assertEqual(archive.path, self.directory / (archive.id + '.jsonl'))
        self.assertEqual([e['kind'] for e in self.events(archive)], ['session.start'])
        self.assertTrue(archive.append('user.transcript', role='user', text='测试输入。', turn_id=3))
        self.assertTrue(archive.append('assistant.played', role='assistant', text='测试回答。', unit_index=0))
        await archive.close(reason='fixture_done')
        events = self.events(archive)
        self.assertEqual([e['seq'] for e in events], [1, 2, 3, 4])
        self.assertEqual([e['text'] for e in events], [None, '测试输入。', '测试回答。', None])
        self.assertEqual(events[-1]['reason'], 'fixture_done')
        self.assertTrue(all(e['session_id'] == archive.id and e['schema'] == logs.SCHEMA for e in events))
        self.assertTrue(all(e['at'].endswith('Z') for e in events))
        self.assertTrue(all('role' in e and 'text' in e for e in events))
        self.assertEqual(events[1]['turn_id'], 3)

    async def test_more_than_sdk_32_items_survive_close_without_trimming(self):
        archive = await self.open()
        for index in range(70):
            self.assertTrue(archive.append('user.transcript', role='user', text=f'fixture-{index}'))
        await archive.close()
        events = self.events(archive, limit=100)
        self.assertEqual(len(events), 72)
        self.assertEqual([e['text'] for e in events[1:-1]], [f'fixture-{i}' for i in range(70)])

    async def test_new_and_existing_directory_and_file_are_private(self):
        self.directory.mkdir(mode=0o755)
        archive = await self.open()
        self.assertEqual(stat.S_IMODE(self.directory.stat().st_mode), 0o700)
        self.assertEqual(stat.S_IMODE(archive.path.stat().st_mode), 0o600)

    async def test_close_drains_and_concurrent_repeated_close_has_one_end(self):
        archive = await self.open()
        for index in range(40):
            self.assertTrue(archive.append('assistant.generated', text=str(index), role='assistant'))
        await asyncio.wait_for(asyncio.gather(archive.close(), archive.close()), 3)
        await archive.close()
        self.assertFalse(archive.append('user.transcript', text='late'))
        events = self.events(archive)
        self.assertEqual(len(events), 42)
        self.assertEqual(sum(e['kind'] == 'session.end' for e in events), 1)

    async def test_writes_handle_short_write_and_never_run_on_event_loop_thread(self):
        real_write = os.write
        threads = []
        def short_write(fd, data):
            threads.append(threading.get_ident())
            return real_write(fd, data[:13])
        with patch.object(logs.os, 'write', side_effect=short_write):
            archive = await self.open()
            archive.append('user.transcript', role='user', text='短写测试。' * 30)
            await archive.close()
        self.assertTrue(threads)
        self.assertNotIn(threading.get_ident(), threads)
        self.assertEqual(self.events(archive)[1]['text'], '短写测试。' * 30)

    async def test_flush_waits_until_fsync_finishes(self):
        archive = await self.open()
        entered, release = threading.Event(), threading.Event()
        real_fsync = os.fsync
        def gated_fsync(fd):
            entered.set()
            release.wait(3)
            return real_fsync(fd)
        try:
            with patch.object(logs.os, 'fsync', side_effect=gated_fsync):
                self.assertTrue(archive.append('user.transcript', text='durability fixture'))
                await self.wait_for_thread(entered)
                flushing = asyncio.create_task(archive.flush())
                await asyncio.sleep(.01)
                self.assertFalse(flushing.done())
                release.set()
                await asyncio.wait_for(flushing, 2)
        finally:
            release.set()

    async def test_disk_failure_reports_once_and_does_not_leak_body(self):
        callback_threads = []
        archive = await self.open(on_failure=lambda: callback_threads.append(threading.get_ident()))
        body = 'PRIVATE_FIXTURE_MUST_NOT_APPEAR_IN_ERRORS'
        with patch.object(logs.os, 'write', side_effect=OSError('disk failure ' + body)):
            self.assertTrue(archive.append('user.transcript', text=body))
            with self.assertRaises(logs.ConversationLogError) as error:
                await asyncio.wait_for(archive.flush(), 2)
            self.assertNotIn(body, str(error.exception))
            self.assertIsNone(error.exception.__cause__)
            self.assertTrue(archive.failed)
            self.assertFalse(archive.append('user.transcript', text=body))
            for _ in range(2):
                with self.assertRaises(logs.ConversationLogError):
                    await asyncio.wait_for(archive.close(), 2)
        await asyncio.sleep(0)
        self.assertEqual(callback_threads, [threading.get_ident()])

    async def test_queue_saturation_is_nonblocking_fails_once_and_close_cannot_deadlock(self):
        failures = []
        with patch.object(logs, 'QUEUE_CAPACITY', 2):
            archive = await self.open(on_failure=lambda: failures.append('failed'))
        entered, release = threading.Event(), threading.Event()
        real_fsync = os.fsync
        def gated_fsync(fd):
            entered.set()
            release.wait(3)
            return real_fsync(fd)
        try:
            with patch.object(logs.os, 'fsync', side_effect=gated_fsync):
                self.assertTrue(archive.append('user.transcript', text='in-flight fixture'))
                await self.wait_for_thread(entered)
                self.assertTrue(archive.append('user.transcript', text='queued fixture 1'))
                self.assertTrue(archive.append('user.transcript', text='queued fixture 2'))
                self.assertFalse(archive.append('user.transcript', text='overflow fixture'))
                self.assertTrue(archive.failed)
                self.assertFalse(archive.append('user.transcript', text='another overflow'))
                with self.assertRaises(logs.ConversationLogError):
                    await archive.flush()
                closing = asyncio.create_task(archive.close())
                await asyncio.sleep(.01)
                self.assertFalse(closing.done())
                release.set()
                with self.assertRaises(logs.ConversationLogError):
                    await asyncio.wait_for(closing, 2)
        finally:
            release.set()
        self.assertEqual(failures, ['failed'])
        self.assertEqual(len(self.events(archive)), 4)

    async def test_open_confirms_start_fsync_and_failure_has_no_body(self):
        failures = []
        with patch.object(logs.os, 'fsync', side_effect=OSError('PRIVATE_OPEN_ERROR')):
            with self.assertRaises(logs.ConversationLogError) as error:
                await logs.ConversationLog.open(self.directory, on_failure=lambda: failures.append(1))
        self.assertNotIn('PRIVATE_OPEN_ERROR', str(error.exception))
        self.assertEqual(failures, [1])

    async def test_audio_non_scalar_unknown_metadata_and_reserved_keys_are_rejected(self):
        for metadata in ({'audio': b'pcm'}, {'turn_id': b'pcm'}, {'turn_id': []},
                         {'api_key': 'private fixture'}, {'seq': 200}, {'revision': float('nan')}):
            with self.subTest(keys=list(metadata)):
                failures = []
                archive = await self.open(on_failure=lambda: failures.append(1))
                self.assertFalse(archive.append('user.transcript', text='fixture', **metadata))
                self.assertTrue(archive.failed)
                with self.assertRaises(logs.ConversationLogError):
                    await archive.close()
                await asyncio.sleep(0)
                self.assertEqual(failures, [1])
                self.assertEqual(len(self.events(archive)), 1)

    async def test_oversized_utf8_and_binary_text_are_rejected_without_truncation(self):
        for text in ('字' * (logs.MAX_EVENT_BYTES // 3), b'not text'):
            archive = await self.open()
            self.assertFalse(archive.append('user.transcript', role='user', text=text))
            self.assertTrue(archive.failed)
            with self.assertRaises(logs.ConversationLogError):
                await archive.close()
            self.assertEqual(len(self.events(archive)), 1)

    async def test_allowed_metadata_and_large_valid_text_roundtrip(self):
        archive = await self.open()
        text = '字' * 20000
        self.assertTrue(archive.append('assistant.content', role='assistant', text=text,
            turn_id=1, input_epoch=2, content_id='main:1', revision=1, unit_index=0,
            unit_id='main:1:0', source='fixture', finish_reason='stop', reason=None,
            delivery='pending', content_kind='story'))
        await archive.close()
        self.assertEqual(self.events(archive)[1]['text'], text)

    async def test_pagination_keeps_sequence_cursor_and_marks_remaining(self):
        archive = await self.open()
        for index in range(6):
            archive.append('user.transcript', text=str(index))
        await archive.close()
        first = logs.read_conversation(self.directory, archive.id, limit=3)
        second = logs.read_conversation(self.directory, archive.id, after=first['next_after'], limit=3)
        last = logs.read_conversation(self.directory, archive.id, after=second['next_after'], limit=3)
        empty = logs.read_conversation(self.directory, archive.id, after=last['next_after'])
        self.assertEqual([e['seq'] for e in first['events'] + second['events'] + last['events']], list(range(1, 9)))
        self.assertEqual([first['has_more'], second['has_more'], last['has_more']], [True, True, False])
        self.assertEqual(empty['events'], [])
        self.assertEqual(empty['next_after'], 8)
        self.assertFalse(empty['truncated_tail'])

    async def test_incomplete_tail_is_explicit_even_on_an_earlier_page(self):
        archive = await self.open()
        archive.append('user.transcript', text='complete fixture')
        await archive.close()
        with archive.path.open('ab') as output:
            output.write(b'{"text":"PRIVATE_INCOMPLETE')
        result = logs.read_conversation(self.directory, archive.id, limit=1)
        self.assertTrue(result['truncated_tail'])
        self.assertTrue(result['has_more'])
        self.assertEqual(len(result['events']), 1)
        self.assertEqual(len(self.events(archive)), 3)

    async def test_complete_json_without_newline_is_still_an_uncommitted_tail(self):
        archive = await self.open()
        await archive.close()
        archive.path.write_bytes(archive.path.read_bytes().rstrip(b'\n'))
        result = logs.read_conversation(self.directory, archive.id)
        self.assertTrue(result['truncated_tail'])
        self.assertEqual([e['kind'] for e in result['events']], ['session.start'])

    async def test_middle_corruption_is_not_silently_skipped_or_exposed(self):
        archive = await self.open()
        await archive.close()
        lines = archive.path.read_bytes().splitlines(keepends=True)
        archive.path.write_bytes(lines[0] + b'PRIVATE_BROKEN_LINE\n' + lines[1])
        with self.assertRaises(logs.ConversationLogError) as error:
            logs.read_conversation(self.directory, archive.id, limit=1)
        self.assertNotIn('PRIVATE_BROKEN_LINE', str(error.exception))

    async def test_mismatched_session_and_sequence_are_corruption(self):
        archive = await self.open()
        await archive.close()
        original = archive.path.read_text()
        for key, value in (('session_id', uuid.uuid4().hex), ('seq', 99)):
            records = [json.loads(line) for line in original.splitlines()]
            records[1][key] = value
            archive.path.write_text(''.join(json.dumps(event) + '\n' for event in records))
            with self.subTest(key=key), self.assertRaises(logs.ConversationLogError):
                self.events(archive)

    async def test_invalid_id_pagination_and_missing_file_are_safe_errors(self):
        archive = await self.open()
        for identifier in ('../private', '/', archive.id + '.jsonl', 'f' * 31, 'F' * 32):
            with self.subTest(identifier=identifier), self.assertRaises(logs.ConversationLogError):
                logs.read_conversation(self.directory, identifier)
        for options in ({'after': -1}, {'limit': 0}, {'after': True}, {'limit': 1000000}):
            with self.subTest(options=options), self.assertRaises(logs.ConversationLogError):
                logs.read_conversation(self.directory, archive.id, **options)
        with self.assertRaises(logs.ConversationLogError):
            logs.read_conversation(self.directory, uuid.uuid4().hex)

    async def test_symlink_file_and_directory_are_refused(self):
        archive = await self.open()
        await archive.close()
        alias = uuid.uuid4().hex
        (self.directory / (alias + '.jsonl')).symlink_to(archive.path)
        with self.assertRaises(logs.ConversationLogError):
            logs.read_conversation(self.directory, alias)
        listed = {item['session_id']: item for item in logs.list_conversations(self.directory)}
        self.assertTrue(listed[alias]['unreadable'])
        self.assertEqual(listed[alias]['source'], 'unreadable')
        self.assertEqual(listed[archive.id]['source'], 'livekit-drain')
        directory_alias = Path(self.temporary.name) / 'alias'
        directory_alias.symlink_to(self.directory, target_is_directory=True)
        with self.assertRaises(logs.ConversationLogError):
            await logs.ConversationLog.open(directory_alias)

    async def test_session_filename_collision_never_overwrites_existing_file(self):
        identity = uuid.uuid4()
        with patch.object(logs.uuid, 'uuid4', return_value=identity):
            archive = await self.open()
            before = archive.path.read_bytes()
            with self.assertRaises(logs.ConversationLogError):
                await logs.ConversationLog.open(self.directory)
        self.assertEqual(archive.path.read_bytes(), before)

    async def test_list_is_recent_first_limited_and_contains_no_body(self):
        identities = []
        for day in range(1, 4):
            archive = await self.open(source='fixture-source')
            archive.append('user.transcript', text='PRIVATE_LIST_BODY')
            await archive.close()
            records = [json.loads(line) for line in archive.path.read_text().splitlines()]
            records[0]['at'] = f'2026-10-{day:02d}T00:00:00.000Z'
            archive.path.write_text(''.join(json.dumps(event) + '\n' for event in records))
            identities.append(archive.id)
        result = logs.list_conversations(self.directory, limit=2)
        self.assertEqual([item['session_id'] for item in result], list(reversed(identities))[:2])
        self.assertEqual(set(result[0]), {'session_id', 'at', 'source'})
        self.assertEqual(result[0]['source'], 'fixture-source')
        self.assertNotIn('PRIVATE_LIST_BODY', json.dumps(result))

    async def test_cli_distinguishes_delivered_unknown_and_optional_generated(self):
        archive = await self.open()
        archive.append('user.transcript', role='user', text='USER_FIXTURE')
        archive.append('assistant.generated', role='assistant', text='GENERATED_FIXTURE')
        archive.append('assistant.content', role='assistant', text='CONTENT_FIXTURE')
        archive.append('assistant.played', role='assistant', text='PLAYED_FIXTURE')
        archive.append('assistant.delivery_unknown', role='assistant', text='UNKNOWN_FIXTURE')
        await archive.close()
        records = [json.loads(line) for line in archive.path.read_text().splitlines()]
        for event in records:
            event['at'] = '2026-10-24T00:00:00.000Z'
        archive.path.write_text(''.join(json.dumps(event) + '\n' for event in records))
        def run(*options):
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                self.assertEqual(logs.main(['--directory', str(self.directory), 'show', archive.id, *options]), 0)
            return output.getvalue()
        human = run()
        self.assertIn('你', human)
        self.assertIn('已确认播完', human)
        self.assertIn('本句未确认完整播完', human)
        self.assertIn('2026-10-24 08:00:00', human)
        for text in ('GENERATED_FIXTURE', 'CONTENT_FIXTURE'):
            self.assertNotIn(text, human)
            self.assertIn(text, run('--include-generated'))
        self.assertIn('生成稿', run('--include-generated'))
        self.assertEqual(len(json.loads(run('--json'))['events']), 7)

    async def test_cli_error_has_no_traceback_or_input_body(self):
        output = io.StringIO()
        with contextlib.redirect_stderr(output):
            code = logs.main(['--directory', str(self.directory), 'show', '../PRIVATE_ID'])
        self.assertEqual(code, 1)
        self.assertNotIn('PRIVATE_ID', output.getvalue())
        self.assertNotIn('Traceback', output.getvalue())

    async def test_list_keeps_valid_sessions_and_explicitly_marks_bad_headers(self):
        archive = await self.open(source='valid-fixture')
        await archive.close()
        unreadable = []
        for data in (b'', b'PRIVATE_BAD_HEADER\n', b'{"incomplete":'):
            identity = uuid.uuid4().hex
            (self.directory / (identity + '.jsonl')).write_bytes(data)
            unreadable.append(identity)
        listed = {item['session_id']: item for item in logs.list_conversations(self.directory)}
        self.assertEqual(set(listed), {archive.id, *unreadable})
        self.assertEqual(listed[archive.id]['source'], 'valid-fixture')
        for identity in unreadable:
            self.assertTrue(listed[identity]['unreadable'])
            self.assertEqual(listed[identity]['source'], 'unreadable')
            self.assertTrue(listed[identity]['at'].endswith('Z'))
            self.assertIn('error_code', listed[identity])
        self.assertNotIn('PRIVATE_BAD_HEADER', json.dumps(listed))
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.assertEqual(logs.main(['--directory', str(self.directory), 'list']), 0)
        self.assertIn('不可读', output.getvalue())
        for identity in (archive.id, *unreadable):
            self.assertIn(identity, output.getvalue())

    async def test_empty_or_fragment_only_archive_has_no_readable_session_header(self):
        self.directory.mkdir()
        for data in (b'', b'{"incomplete":'):
            identity = uuid.uuid4().hex
            (self.directory / (identity + '.jsonl')).write_bytes(data)
            with self.subTest(data=data), self.assertRaises(logs.ConversationLogError):
                logs.read_conversation(self.directory, identity)

    async def test_failed_directory_fsync_or_start_write_removes_only_its_new_file(self):
        archive = await self.open()
        await archive.close()
        original = archive.path.read_bytes()
        for syscall in ('fsync', 'write'):
            identity = uuid.uuid4()
            with self.subTest(syscall=syscall), patch.object(logs.uuid, 'uuid4', return_value=identity):
                with patch.object(logs.os, syscall, side_effect=OSError('PRIVATE_FAILURE')):
                    with self.assertRaises(logs.ConversationLogError):
                        await logs.ConversationLog.open(self.directory)
                self.assertFalse((self.directory / (identity.hex + '.jsonl')).exists())
                self.assertEqual(archive.path.read_bytes(), original)

    async def test_cancel_during_creation_or_start_fsync_removes_only_own_metadata(self):
        archive = await self.open()
        await archive.close()
        original = archive.path.read_bytes()
        for gate_file in (False, True):
            identity = uuid.uuid4()
            entered, release = threading.Event(), threading.Event()
            real_fsync = os.fsync
            def gated_fsync(fd):
                if stat.S_ISREG(os.fstat(fd).st_mode) == gate_file:
                    entered.set()
                    release.wait(3)
                return real_fsync(fd)
            try:
                with patch.object(logs.uuid, 'uuid4', return_value=identity), patch.object(logs.os, 'fsync', side_effect=gated_fsync):
                    opening = asyncio.create_task(logs.ConversationLog.open(self.directory))
                    await self.wait_for_thread(entered)
                    opening.cancel()
                    release.set()
                    with self.assertRaises(asyncio.CancelledError):
                        await asyncio.wait_for(opening, 2)
                self.assertFalse((self.directory / (identity.hex + '.jsonl')).exists())
                self.assertEqual(archive.path.read_bytes(), original)
            finally:
                release.set()

    async def test_cancel_cleanup_preserves_replaced_path_with_existing_body(self):
        archive = await self.open()
        archive.append('user.transcript', role='user', text='PRIVATE_REPLACEMENT_FIXTURE')
        await archive.close()
        original = archive.path.read_bytes()
        identity = uuid.uuid4()
        candidate = self.directory / (identity.hex + '.jsonl')
        entered, release = threading.Event(), threading.Event()
        real_fsync = os.fsync
        def gated_fsync(fd):
            entered.set()
            release.wait(3)
            return real_fsync(fd)
        try:
            with patch.object(logs.uuid, 'uuid4', return_value=identity), patch.object(logs.os, 'fsync', side_effect=gated_fsync):
                opening = asyncio.create_task(logs.ConversationLog.open(self.directory))
                await self.wait_for_thread(entered)
                candidate.rename(candidate.with_suffix('.abandoned'))
                candidate.write_bytes(original)
                opening.cancel()
                release.set()
                with self.assertRaises(asyncio.CancelledError):
                    await asyncio.wait_for(opening, 2)
            self.assertEqual(candidate.read_bytes(), original)
            self.assertEqual(archive.path.read_bytes(), original)
        finally:
            release.set()


if __name__ == '__main__':
    unittest.main()
