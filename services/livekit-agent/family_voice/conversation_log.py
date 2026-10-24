"""Private, append-only text archives. No audio, model context, or ordinary logs.

append() only validates and submits bounded text to a single writer thread. A
successful append is admission, while flush()/close() confirm file fsync. Files
are retained until an operator explicitly removes them; this is not LLM memory.
"""
from __future__ import annotations

import argparse
import asyncio
from datetime import datetime, timezone
import heapq
import json
import math
import os
from pathlib import Path
import queue
import re
import stat
import sys
import threading
from typing import Callable
import uuid
from zoneinfo import ZoneInfo


SCHEMA = 'family-voice.conversation.v1'
MAX_EVENT_BYTES = 128 * 1024
QUEUE_CAPACITY = 256
MAX_READ_LIMIT = 1000
_IDENTITY = re.compile(r'[0-9a-f]{32}')
_KIND = re.compile(r'[a-z][a-z0-9_.-]{0,63}')
_METADATA = frozenset({
    'turn_id', 'input_epoch', 'content_id', 'revision', 'unit_index', 'unit_id',
    'source', 'finish_reason', 'reason', 'delivery', 'content_kind',
})
_BASE_FIELDS = frozenset({'schema', 'session_id', 'seq', 'at', 'kind', 'role', 'text'})


class ConversationLogError(RuntimeError):
    """Archive failure with a fixed message that never embeds text or OS errors."""


def _call_failure_callback(callback):
    if callback is not None:
        try:
            callback()
        except Exception:
            pass  # Do not let callback errors disclose conversation data.


def _check_identity(identity):
    if type(identity) is not str or not _IDENTITY.fullmatch(identity):
        raise ConversationLogError('conversation id is invalid')


def _check_fields(kind, role, text, metadata):
    if type(kind) is not str or not _KIND.fullmatch(kind):
        raise ValueError
    if role is not None and (type(role) is not str or role not in ('user', 'assistant')):
        raise ValueError
    if text is not None and (type(text) is not str or len(text) > MAX_EVENT_BYTES):
        raise ValueError
    if not metadata.keys() <= _METADATA:
        raise ValueError
    characters = len(text) if text is not None else 0
    for value in metadata.values():
        if type(value) not in (str, int, float, bool, type(None)):
            raise ValueError
        if type(value) is float and not math.isfinite(value):
            raise ValueError
        if type(value) is str:
            characters += len(value)
    if characters > MAX_EVENT_BYTES:
        raise ValueError


def _utc_now():
    return datetime.now(timezone.utc).isoformat(timespec='milliseconds').replace('+00:00', 'Z')


def _open_directory(directory: Path, *, create=False):
    if create:
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    # Refuse a final directory symlink, and use its descriptor for every file
    # operation so replacement of its path cannot redirect the open afterward.
    fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        if create:
            os.fchmod(fd, 0o700)
        return fd
    except BaseException:
        os.close(fd)
        raise


def _unlink_created(directory_fd, identity, created):
    """Remove only the inode this open attempt exclusively created, never a replacement."""
    if created is None:
        return
    try:
        current = os.stat(identity + '.jsonl', dir_fd=directory_fd, follow_symlinks=False)
        if stat.S_ISREG(current.st_mode) and (current.st_dev, current.st_ino) == (created.st_dev, created.st_ino):
            os.unlink(identity + '.jsonl', dir_fd=directory_fd)
            os.fsync(directory_fd)
    except OSError:
        pass  # A crash or unavailable disk may leave an explicitly unreadable entry.


def _discard_creation(directory, identity, created, *, close_fd=False):
    fd, identity_stat, guard_fd = created
    try:
        directory_fd = _open_directory(directory)
        try:
            _unlink_created(directory_fd, identity, identity_stat)
        finally:
            os.close(directory_fd)
    except OSError:
        pass
    finally:
        # Keep an inode reference until after the name check/unlink, preventing
        # reuse of the old inode number after the writer has closed its fd.
        for owned_fd in ((fd, guard_fd) if close_fd else (guard_fd,)):
            try:
                os.close(owned_fd)
            except OSError:
                pass


async def _finish_cleanup(awaitable):
    """Finish an already-owned resource operation even after repeated cancellation."""
    task = asyncio.ensure_future(awaitable)
    while True:
        try:
            return await asyncio.shield(task)
        except asyncio.CancelledError:
            if task.cancelled():
                raise


def _create_file(directory: Path, identity: str):
    directory_fd = _open_directory(directory, create=True)
    file_fd = guard_fd = None
    created = None
    try:
        file_fd = os.open(identity + '.jsonl', os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW,
                          0o600, dir_fd=directory_fd)
        created = os.fstat(file_fd)
        guard_fd = os.dup(file_fd)
        os.fchmod(file_fd, 0o600)
        os.fsync(directory_fd)
        return file_fd, created, guard_fd
    except BaseException:
        if file_fd is not None:
            _unlink_created(directory_fd, identity, created)
            os.close(file_fd)
        if guard_fd is not None:
            os.close(guard_fd)
        raise
    finally:
        os.close(directory_fd)


class ConversationLog:
    @classmethod
    async def open(cls, directory: Path, *, source: str = 'livekit-drain',
                   on_failure: Callable[[], None] | None = None):
        """Create a private session and return only after session.start is durable."""
        try:
            if type(source) is not str or not source or len(source) > 256:
                raise ValueError
            directory = Path(directory)
            identity = uuid.uuid4().hex
            # Shield creation so cancellation cannot abandon an opened descriptor.
            creating = asyncio.create_task(asyncio.to_thread(_create_file, directory, identity))
            try:
                created = await asyncio.shield(creating)
            except asyncio.CancelledError:
                try:
                    created = await _finish_cleanup(creating)
                except Exception:
                    pass  # _create_file already cleaned up its own failed attempt.
                else:
                    await _finish_cleanup(asyncio.to_thread(_discard_creation, directory, identity, created, close_fd=True))
                raise
        except asyncio.CancelledError:
            raise
        except Exception:
            _call_failure_callback(on_failure)
            raise ConversationLogError('conversation log cannot be created') from None

        archive = None
        try:
            archive = cls(directory, identity, created[0], on_failure)
            archive.append('session.start', source=source)
            await archive.flush()
            os.close(created[2])
            return archive
        except BaseException:
            if archive is not None:
                try:
                    await _finish_cleanup(archive.close(reason='start_failed'))
                except ConversationLogError:
                    pass
            # open never returned, so this inode contains only startup metadata;
            # no caller has been able to append conversation body to it.
            await _finish_cleanup(asyncio.to_thread(_discard_creation, directory, identity,
                                                    created, close_fd=archive is None))
            raise

    def __init__(self, directory, identity, fd, on_failure):
        self.id = identity
        self.path = directory / (identity + '.jsonl')
        self.failed = False
        self._fd = fd
        self._loop = asyncio.get_running_loop()
        self._on_failure = on_failure
        self._queue = queue.Queue(maxsize=QUEUE_CAPACITY)
        self._lock = threading.Lock()
        self._changed = asyncio.Event()
        self._seq = self._durable = 0
        self._closing = self._stopping = self._worker_done = False
        self._close_task = None
        self._thread = threading.Thread(target=self._write_loop, name='conversation-writer', daemon=True)
        self._thread.start()

    def _signal(self):
        try:
            self._loop.call_soon_threadsafe(self._changed.set)
        except RuntimeError:
            pass  # The owning loop has already ended; never print archive data.

    def _notify_failure(self):
        _call_failure_callback(self._on_failure)

    def _fail(self):
        with self._lock:
            first = not self.failed
            self.failed = True
        if first:
            try:
                self._loop.call_soon_threadsafe(self._notify_failure)
            except RuntimeError:
                pass
        self._signal()

    def append(self, kind: str, *, role: str | None = None, text: str | None = None,
               **metadata) -> bool:
        """Admit one complete text event without waiting for disk or queue space."""
        return self._append(kind, role=role, text=text, metadata=metadata)

    def _append(self, kind, *, role=None, text=None, metadata, during_close=False):
        with self._lock:
            if self.failed or (self._closing and not during_close):
                return False
            try:
                _check_fields(kind, role, text, metadata)
                sequence = self._seq + 1
                event = {'schema': SCHEMA, 'session_id': self.id, 'seq': sequence,
                         'at': _utc_now(), 'kind': kind, 'role': role, 'text': text, **metadata}
                data = (json.dumps(event, ensure_ascii=False, allow_nan=False, separators=(',', ':')) + '\n').encode('utf-8')
                if len(data) > MAX_EVENT_BYTES:
                    raise ValueError
                self._queue.put_nowait((sequence, data))
                self._seq = sequence
                return True
            except (ValueError, TypeError, OverflowError, UnicodeError, queue.Full):
                pass
        self._fail()
        return False

    def _write_loop(self):
        try:
            while True:
                with self._lock:
                    if self._stopping and self._queue.empty():
                        break
                try:
                    sequence, data = self._queue.get(timeout=.05)
                except queue.Empty:
                    continue
                try:
                    remaining = memoryview(data)
                    while remaining:
                        written = os.write(self._fd, remaining)
                        if written <= 0:
                            raise OSError
                        remaining = remaining[written:]
                    os.fsync(self._fd)
                except Exception:
                    self._fail()
                    break
                with self._lock:
                    self._durable = sequence
                self._signal()
        except Exception:
            self._fail()
        finally:
            # Disk failures cannot make flush/close wait on discarded work. On
            # saturation, however, the healthy writer drains all admitted work.
            while True:
                try:
                    self._queue.get_nowait()
                except queue.Empty:
                    break
            try:
                os.close(self._fd)
            except Exception:
                self._fail()
            with self._lock:
                self._worker_done = True
            self._signal()

    async def flush(self):
        """Wait for all events admitted before this call, or report fixed failure."""
        with self._lock:
            target = self._seq
        while True:
            self._changed.clear()
            with self._lock:
                if self.failed:
                    raise ConversationLogError('conversation text recording failed')
                if self._durable >= target:
                    return
            await self._changed.wait()

    async def close(self, reason: str = 'disconnected'):
        """Stop admission, drain, persist one session.end, and join the writer."""
        if self._close_task is None:
            with self._lock:
                self._closing = True
            self._close_task = asyncio.create_task(self._close(reason))
        await asyncio.shield(self._close_task)

    async def _close(self, reason):
        try:
            await self.flush()
            self._append('session.end', metadata={'reason': reason}, during_close=True)
            await self.flush()
        finally:
            with self._lock:
                self._stopping = True
            await asyncio.to_thread(self._thread.join)
        if self.failed:
            raise ConversationLogError('conversation text recording failed')


def _open_read(directory_fd, identity):
    fd = os.open(identity + '.jsonl', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory_fd)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise ConversationLogError('conversation file is invalid')
        return os.fdopen(fd, 'rb')
    except BaseException:
        os.close(fd)
        raise


def _timestamp(value):
    if type(value) is not str or not value.endswith('Z'):
        raise ValueError
    parsed = datetime.fromisoformat(value[:-1] + '+00:00')
    if parsed.utcoffset() != timezone.utc.utcoffset(parsed):
        raise ValueError
    return parsed


def _parse_line(line, identity, expected_sequence):
    event = json.loads(line)
    if type(event) is not dict or not _BASE_FIELDS <= event.keys() or event.keys() - _BASE_FIELDS - _METADATA:
        raise ValueError
    if event['schema'] != SCHEMA or event['session_id'] != identity:
        raise ValueError
    if type(event['seq']) is not int or event['seq'] != expected_sequence:
        raise ValueError
    _timestamp(event['at'])
    _check_fields(event['kind'], event['role'], event['text'],
                  {key: value for key, value in event.items() if key in _METADATA})
    if expected_sequence == 1 and (event['kind'] != 'session.start' or
            type(event.get('source')) is not str or not event['source']):
        raise ValueError
    return event


def _check_pagination(after, limit):
    if type(after) is not int or after < 0 or type(limit) is not int or not 1 <= limit <= MAX_READ_LIMIT:
        raise ConversationLogError('conversation pagination is invalid')


def read_conversation(directory, id, *, after=0, limit=100) -> dict:
    """Stream and validate an archive with bounded page memory.

    Scan the whole file, including beyond the page, to report a partial tail and
    never hide corruption behind a pagination boundary. A final line without a
    newline is uncommitted and omitted, even if its JSON happens to parse.
    """
    _check_identity(id)
    _check_pagination(after, limit)
    events, has_more, truncated_tail = [], False, False
    try:
        directory_fd = _open_directory(Path(directory))
        try:
            with _open_read(directory_fd, id) as archive:
                expected_sequence = 1
                while line := archive.readline(MAX_EVENT_BYTES + 1):
                    if len(line) > MAX_EVENT_BYTES:
                        raise ValueError
                    if not line.endswith(b'\n'):
                        truncated_tail = True
                        break
                    event = _parse_line(line, id, expected_sequence)
                    expected_sequence += 1
                    if event['seq'] > after:
                        if len(events) < limit:
                            events.append(event)
                        else:
                            has_more = True
                if expected_sequence == 1:
                    raise ValueError  # Empty/fragment-only files are not sessions.
        finally:
            os.close(directory_fd)
    except Exception:
        raise ConversationLogError('conversation log cannot be read or contains an invalid record') from None
    return {'events': events, 'has_more': has_more,
            'next_after': events[-1]['seq'] if events else after, 'truncated_tail': truncated_tail}


def _session_summary(directory_fd, identity):
    at, error_code = _utc_now(), 'read_failed'
    try:
        metadata = os.stat(identity + '.jsonl', dir_fd=directory_fd, follow_symlinks=False)
        at = datetime.fromtimestamp(metadata.st_mtime, timezone.utc).isoformat(timespec='milliseconds').replace('+00:00', 'Z')
        if not stat.S_ISREG(metadata.st_mode):
            error_code = 'unsafe_file'
            raise ValueError
        with _open_read(directory_fd, identity) as archive:
            line = archive.readline(MAX_EVENT_BYTES + 1)
        error_code = 'missing_header'
        if not line or not line.endswith(b'\n'):
            raise ValueError
        error_code = 'invalid_header'
        if len(line) > MAX_EVENT_BYTES:
            raise ValueError
        event = _parse_line(line, identity, 1)
        return {'session_id': identity, 'at': event['at'], 'source': event['source']}
    except Exception:
        return {'session_id': identity, 'at': at, 'source': 'unreadable',
                'unreadable': True, 'error_code': error_code}


def list_conversations(directory, limit=10) -> list[dict]:
    """List session metadata and explicit unreadable entries, newest first."""
    _check_pagination(0, limit)
    try:
        directory_fd = _open_directory(Path(directory))
        try:
            def summaries():
                with os.scandir(directory_fd) as entries:
                    for entry in entries:
                        if not entry.name.endswith('.jsonl'):
                            continue
                        identity = entry.name[:-6]
                        if not _IDENTITY.fullmatch(identity):
                            continue
                        yield _session_summary(directory_fd, identity)
            return heapq.nlargest(limit, summaries(), key=lambda event: (_timestamp(event['at']), event['session_id']))
        finally:
            os.close(directory_fd)
    except Exception:
        raise ConversationLogError('conversation list cannot be read or contains an invalid record') from None


def _local_time(value):
    return _timestamp(value).astimezone(ZoneInfo('Asia/Shanghai')).strftime('%Y-%m-%d %H:%M:%S')


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description='Read private conversation text on the server.')
    parser.add_argument('--directory', type=Path, required=True)
    commands = parser.add_subparsers(dest='command', required=True)
    listing = commands.add_parser('list', help='List session metadata only.')
    listing.add_argument('--limit', type=int, default=10)
    showing = commands.add_parser('show', help='Read a page of session text.')
    showing.add_argument('id')
    showing.add_argument('--after', type=int, default=0)
    showing.add_argument('--limit', type=int, default=100)
    showing.add_argument('--json', action='store_true', help='Include every event with original UTC timestamps.')
    showing.add_argument('--include-generated', action='store_true', help='Label and include unconfirmed generated text.')
    args = parser.parse_args(argv)
    try:
        if args.command == 'list':
            for item in list_conversations(args.directory, args.limit):
                status = f"不可读：{item['error_code']}" if item.get('unreadable') else item['source']
                print(f"{_local_time(item['at'])}  {item['session_id']}  {status}")
            return 0
        result = read_conversation(args.directory, args.id, after=args.after, limit=args.limit)
        if args.json:
            print(json.dumps(result, ensure_ascii=False))
            return 0
        labels = {'user.transcript': '你', 'assistant.played': '助手（已确认播完）',
                  'assistant.delivery_unknown': '助手（本句未确认完整播完）'}
        for event in result['events']:
            label = labels.get(event['kind'])
            if label is None and args.include_generated and event['kind'].startswith('assistant.'):
                label = '助手（生成稿，未确认播放）'
            if label is not None and event['text'] is not None:
                print(f"[{event['seq']}] {_local_time(event['at'])} {label}：{event['text']}")
        if result['has_more']:
            print(f"后续分页：--after {result['next_after']}")
        if result['truncated_tail']:
            print('警告：末尾记录未完整写入，已忽略残片。')
        return 0
    except ConversationLogError as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
