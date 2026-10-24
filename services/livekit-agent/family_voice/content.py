"""Immutable content with checkpoints owned by actual playback settlement.

Cancelling the dispatch worker stops future output, but does not settle audio
already accepted by the output. Its separate receipt can still confirm a full
unit after cancellation. The output adapter must eventually settle that receipt
(including a bounded unknown outcome on disconnect or timeout).
"""
import asyncio
import re
from collections.abc import Awaitable, Callable
from dataclasses import dataclass


_MAX_TEXT = 1200
_MAX_UNITS = 40
_MAX_UNIT = 80
_ENDINGS = frozenset('。！？!?；;.…')
_CLOSERS = frozenset('”’\"\'）)]』」')


def split_sentences(text: str) -> list[str]:
    """Normalize whitespace and retain every character, or reject the text.

    Punctuation closes a sentence, including its closing quotes and following
    space. Long sentences are split into bounded units; limits never truncate
    a tail or silently drop a sentence.
    """
    if not isinstance(text, str):
        raise ValueError('content must be text')
    text = re.sub(r'\s+', ' ', text).strip()
    if len(text) > _MAX_TEXT:
        raise ValueError('content must contain at most 1200 normalized characters')
    sentences = []
    start = index = 0
    while index < len(text):
        if text[index] not in _ENDINGS:
            index += 1
            continue
        if text[index] == '.':
            boundary = index + 1
            while boundary < len(text) and text[boundary] == '.':
                boundary += 1
            while boundary < len(text) and text[boundary] in _CLOSERS:
                boundary += 1
            # Decimal, version and URL punctuation is part of the original
            # phrase. English full stops close at whitespace or text end.
            if boundary < len(text) and text[boundary] != ' ':
                index += 1
                continue
        index += 1
        while index < len(text) and text[index] in _ENDINGS:
            index += 1
        while index < len(text) and text[index] in _CLOSERS:
            index += 1
        if index < len(text) and text[index] == ' ':
            index += 1
        sentences.append(text[start:index])
        start = index
    if start < len(text):
        sentences.append(text[start:])

    units = []
    for sentence in sentences:
        while len(sentence) > _MAX_UNIT:
            # Keep whole words where a useful boundary exists, while retaining
            # its space so joining the units exactly reproduces the original.
            cut = sentence.rfind(' ', 0, _MAX_UNIT + 1) + 1
            if cut <= 0 or cut > _MAX_UNIT:
                cut = _MAX_UNIT
            units.append(sentence[:cut])
            sentence = sentence[cut:]
        if sentence:
            units.append(sentence)
    if len(units) > _MAX_UNITS:
        raise ValueError('content must contain at most 40 playback units')
    return units


@dataclass
class _Receipt:
    revision: int
    index: int
    future: asyncio.Future
    interrupted: bool = False


class ContentController:
    """One immutable original, at most three PCM units, and a full-unit prefix."""

    MAX_PCM_BYTES = 24000 * 2 * 12

    def __init__(
        self,
        prepare: Callable[[str], Awaitable[bytes]],
        play: Callable[[int, int, str, bytes, Callable[[bool], None]], Awaitable[None]],
        emit: Callable[[dict], None],
        name: str = 'main',
        prefetch: int = 3,
    ):
        if type(prefetch) is not int or not 1 <= prefetch <= 3:
            raise ValueError('prefetch must be between 1 and 3')
        if not isinstance(name, str) or not name:
            raise ValueError('content name must be nonempty text')
        self._prepare, self._play, self._emit = prepare, play, emit
        self._name, self._prefetch = name, prefetch
        self._plan: dict | None = None
        self._revision = self._generation = 0
        self._active_revision: int | None = None
        self._closed = self._desired_playing = False
        self._playing = asyncio.Event()
        self._prepared: dict[int, asyncio.Task] = {}
        self._worker: asyncio.Task | None = None
        self._pending: _Receipt | None = None
        self._tasks: set[asyncio.Task] = set()
        self._retiring: set[asyncio.Task] = set()

    @property
    def snapshot(self) -> dict | None:
        if self._plan is None:
            return None
        return {**self._plan, 'units': list(self._plan['units'])}

    def start(self, text: str, original_input: str = '', kind: str = 'reply') -> None:
        if self._closed:
            raise RuntimeError('content controller is closed')
        units = split_sentences(text)
        if not units:
            raise ValueError('content must not be empty')
        self._invalidate()
        self._revision += 1
        self._active_revision = self._revision
        self._plan = {
            'task_id': f'{self._name}:{self._revision}',
            'revision': self._revision,
            'original_input': original_input,
            'kind': kind,
            'units': tuple(units),
            'confirmed_count': 0,
            'next': 0,
            'state': 'playing',
        }
        self._desired_playing = True
        self._playing.set()
        self._publish()
        self._spawn_worker()

    def pause(self) -> None:
        if self._closed or self._plan is None or self._plan['state'] not in ('playing', 'draining'):
            return
        self._desired_playing = False
        self._generation += 1
        self._playing.clear()
        if self._pending is not None:
            self._pending.interrupted = True
        self._retire_worker()
        self._plan['state'] = 'draining' if self._pending is not None else 'paused'
        self._publish()

    def resume(self) -> None:
        if self._closed or self._plan is None or self._plan['state'] not in ('paused', 'draining'):
            return
        if self._desired_playing:
            return
        # A final receipt may precede the play coroutine's cleanup/return.
        # Retire and join that coroutine before using the output again.
        self._retire_worker()
        self._desired_playing = True
        self._generation += 1
        if self._pending is None:
            self._plan['state'] = 'playing'
            self._playing.set()
        else:
            self._plan['state'] = 'draining'
        self._publish()
        self._spawn_worker()

    def discard(self) -> None:
        if self._closed or self._plan is None or self._plan['state'] == 'discarded':
            return
        self._invalidate()
        self._plan['state'] = 'discarded'
        self._publish()

    def retry(self) -> None:
        """An explicit user retry keeps the original and confirmed prefix."""
        if self._closed or self._plan is None or self._plan['state'] != 'failed':
            return
        self._plan['state'] = 'draining' if self._pending is not None else 'paused'
        self.resume()

    async def close(self) -> None:
        if not self._closed:
            self._closed = True
            self._invalidate()
            if self._plan is not None:
                self._plan['state'] = 'closed'
                self._publish()
        if self._tasks:
            await asyncio.gather(*tuple(self._tasks), return_exceptions=True)

    def _publish(self) -> None:
        plan = self._plan
        self._emit({
            'type': 'content.state',
            'task_id': plan['task_id'],
            'revision': plan['revision'],
            'kind': plan['kind'],
            'total': len(plan['units']),
            'confirmed_count': plan['confirmed_count'],
            'next': plan['next'],
            'state': plan['state'],
        })

    def _track(self, coroutine) -> asyncio.Task:
        task = asyncio.create_task(coroutine)
        self._tasks.add(task)
        task.add_done_callback(self._done)
        return task

    def _done(self, task: asyncio.Task) -> None:
        self._tasks.discard(task)
        self._retiring.discard(task)
        if not task.cancelled():
            task.exception()

    def _retire(self, task: asyncio.Task) -> None:
        if not task.done() and task not in self._retiring:
            self._retiring.add(task)
            task.cancel()

    def _retire_worker(self) -> None:
        if self._worker is not None:
            self._retire(self._worker)
            self._worker = None

    def _discard_prepares(self) -> None:
        for task in self._prepared.values():
            self._retire(task)
        self._prepared.clear()

    def _invalidate(self) -> None:
        self._generation += 1
        self._active_revision = None
        self._desired_playing = False
        self._playing.clear()
        self._retire_worker()
        self._discard_prepares()
        if self._pending is not None:
            if not self._pending.future.done():
                self._pending.future.set_result(False)
            self._pending = None

    def _spawn_worker(self) -> None:
        self._worker = self._track(self._run(self._generation, tuple(self._retiring)))

    def _valid(self, revision: int) -> bool:
        return not self._closed and self._active_revision == revision

    def _current(self, generation: int) -> bool:
        return (
            not self._closed and self._plan is not None
            and generation == self._generation and self._desired_playing
            and self._plan['state'] == 'playing'
        )

    async def _synthesize(self, text: str, revision: int) -> bytes:
        while True:
            await self._playing.wait()
            if not self._valid(revision):
                raise asyncio.CancelledError
            # A synchronous pause may occur after Event.set wakes this task.
            if self._desired_playing and self._plan['state'] == 'playing':
                break
        pcm = await self._prepare(text)
        if not isinstance(pcm, bytes) or not pcm or len(pcm) % 2 or len(pcm) > self.MAX_PCM_BYTES:
            raise ValueError('content audio must be nonempty mono 24k s16 PCM of at most 12 seconds')
        return pcm

    def _preparation_done(self, task: asyncio.Task, revision: int, index: int) -> None:
        if task.cancelled() or not self._valid(revision) or self._prepared.get(index) is not task:
            return
        if task.exception() is not None:
            self._fail()

    def _fail(self) -> None:
        if self._plan['state'] == 'failed':
            return
        self._desired_playing = False
        self._playing.clear()
        self._generation += 1
        self._retire_worker()
        self._discard_prepares()
        self._plan['state'] = 'failed'
        self._publish()

    def _settle(self, receipt: _Receipt, complete: bool) -> None:
        if not self._valid(receipt.revision) or self._pending is not receipt or receipt.future.done():
            return
        complete = complete is True
        self._pending = None
        if complete and receipt.index == self._plan['next']:
            self._prepared.pop(receipt.index, None)
            self._plan['next'] += 1
            self._plan['confirmed_count'] = self._plan['next']
        if self._plan['state'] != 'failed':
            if self._plan['next'] == len(self._plan['units']):
                self._desired_playing = False
                self._playing.clear()
                self._plan['state'] = 'completed'
            elif not complete and not receipt.interrupted:
                self._desired_playing = False
                self._playing.clear()
                self._plan['state'] = 'paused'
            elif self._desired_playing:
                self._plan['state'] = 'playing'
                self._playing.set()
            else:
                self._plan['state'] = 'paused'
        receipt.future.set_result(complete)
        self._publish()

    async def _run(self, generation: int, retired: tuple[asyncio.Task, ...]) -> None:
        if retired:
            # Re-pausing a queued resume must not cancel old SDK cleanup twice.
            await asyncio.shield(asyncio.gather(*retired, return_exceptions=True))
        receipt = self._pending
        if receipt is not None:
            # This future belongs to the playback identity, not this worker.
            await asyncio.shield(receipt.future)
        while self._current(generation):
            index = self._plan['next']
            revision = self._plan['revision']
            units = self._plan['units']
            for upcoming in range(index, min(len(units), index + self._prefetch)):
                if upcoming not in self._prepared:
                    task = self._track(self._synthesize(units[upcoming], revision))
                    self._prepared[upcoming] = task
                    task.add_done_callback(lambda task, r=revision, i=upcoming: self._preparation_done(task, r, i))
            try:
                pcm = await asyncio.shield(self._prepared[index])
            except asyncio.CancelledError:
                raise
            except Exception:
                if self._current(generation):
                    self._fail()
                return
            if not self._current(generation):
                return
            receipt = _Receipt(revision, index, asyncio.get_running_loop().create_future())
            self._pending = receipt
            settle = lambda complete, receipt=receipt: self._settle(receipt, complete)
            try:
                await self._play(revision, index, units[index], pcm, settle)
            except asyncio.CancelledError:
                # Cancellation is SDK lifecycle evidence, never playback proof.
                raise
            except Exception:
                if self._current(generation) and not receipt.future.done():
                    settle(False)
            else:
                # Only a normal, still-current return without a callback means
                # unknown. A cancelled/superseded dispatch retains its receipt.
                if self._current(generation) and not receipt.future.done():
                    settle(False)
            # Do not retain the previous PCM while filling the next window.
            del pcm
            if not self._current(generation):
                return
