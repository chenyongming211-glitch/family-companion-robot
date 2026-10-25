# DEPRECATED_PRODUCT_PATH (2026-10-25): historical ASR -> LLM -> TTS cascade.
# Current product voice core is Go internal/realtimelive; do not extend this path.
# Reference/legacy service only; this marker does not stop a running instance.
# See docs/CODE_LIFECYCLE.md before reusing any business logic.
"""Session-local counting checkpoints based only on confirmed full playback.

Callbacks own SDK/provider details and must finish cancellation cleanup when
cancelled. Pausing retains synthesis requests and results; replacing/closing a
plan cancels and joins them. No synthesis failure is retried automatically.
"""
import asyncio
import re
from collections.abc import Awaitable, Callable


_DIGITS = {char: value for value, chars in enumerate(('零〇', '一', '二两', '三', '四', '五', '六', '七', '八', '九')) for char in chars}
_UNITS = {'十': 10, '百': 100, '千': 1000}
_NUMBER = r'(?:-?[0-9]+|[零〇一二两三四五六七八九十百千]+)'
_PREFIX = r'(?:(?:请|麻烦)(?:你)?|帮我|给我|你)?'
_COUNT_REQUEST = re.compile(
    _PREFIX + r'(?:从(?P<from>' + _NUMBER + r')(?:开始)?(?:一直|连续|持续)?数(?:数)?到(?P<to>' + _NUMBER + r')'
    r'|(?:一直|连续)?数(?:数)?(?:从)?(?P<start>' + _NUMBER + r')(?:到|至)(?P<end>' + _NUMBER + r'))'
    r'(?:吧|好吗|可以吗)?(?:[,，](?:中间)?(?:不要停|别停|不停|一直数))?[。.!！?？]*$'
)
_RESUME = re.compile(
    r'(?:继续(?:数(?:数)?|计数)?|接着数(?:数)?'
    r'|继续(?:刚才|刚刚|上次|之前)的?(?:数数|计数)'
    r'|从(?:刚才|刚刚|上次)(?:停|停下|暂停|中断)的?(?:地方|位置|那儿|那里)(?:继续|接着)数(?:数)?)'
    r'(?:吧|啊|呀|好吗|可以吗)?[。.!！?？]*$'
)
_SPOKEN_PREFIX = re.compile(r'^(?:(?:啊|呃|额|嗯|哦|噢|好(?:的)?|那(?:么)?|请(?:你)?|麻烦(?:你)?|帮我|给我|你)[,，。]*)+')


def _spoken_command(text: str) -> str:
    # Normalize only leading discourse/politeness markers. Negation and the
    # rest of the utterance stay intact, and command matching remains anchored.
    return _SPOKEN_PREFIX.sub('', re.sub(r'\s+', '', text))


def _number(text: str) -> int:
    if text.isascii() and text.lstrip('-').isdigit():
        return int(text)
    if not any(char in _UNITS for char in text):
        return int(''.join(str(_DIGITS[char]) for char in text))
    total, digit, last_unit = 0, None, 10000
    for char in text:
        if char in _DIGITS:
            value = _DIGITS[char]
            if digit not in (None, 0):
                raise ValueError('invalid Chinese numeral')
            digit = value
        else:
            unit = _UNITS[char]
            if unit >= last_unit or (digit is None and unit != 10):
                raise ValueError('invalid Chinese numeral')
            total += (1 if digit is None else digit) * unit
            digit, last_unit = None, unit
    return total + (digit or 0)


def parse_count_request(text: str) -> tuple[int, int] | None:
    """Recognize explicit ranges; the controller validates size and direction."""
    match = _COUNT_REQUEST.fullmatch(_spoken_command(text))
    if not match:
        return None
    try:
        return (_number(match['from'] or match['start']), _number(match['to'] or match['end']))
    except ValueError:
        return None


def is_count_resume(text: str) -> bool:
    """Bare 'continue' needs an existing count plan, checked by the caller."""
    return _RESUME.fullmatch(_spoken_command(text)) is not None


class CountController:
    MAX_PCM_BYTES = 24000 * 2 * 5

    def __init__(
        self,
        prepare: Callable[[int], Awaitable[bytes]],
        play: Callable[[int, bytes], Awaitable[bool]],
        emit: Callable[[dict], None],
        prefetch: int = 3,
    ):
        if type(prefetch) is not int or not 1 <= prefetch <= 3:
            raise ValueError('prefetch must be between 1 and 3')
        self._prepare, self._play, self._emit = prepare, play, emit
        self._prefetch = prefetch
        self._plan: dict | None = None
        self._generation = self._plan_id = 0
        self._closed = False
        self._playing = asyncio.Event()
        self._prepared: dict[int, asyncio.Task] = {}
        self._worker: asyncio.Task | None = None
        self._tasks: set[asyncio.Task] = set()
        self._retiring: set[asyncio.Task] = set()

    @property
    def snapshot(self) -> dict | None:
        return dict(self._plan) if self._plan is not None else None

    def start(self, start: int, end: int) -> None:
        if self._closed:
            raise RuntimeError('count controller is closed')
        if type(start) is not int or type(end) is not int or not 0 <= start <= end <= 1000 or end - start + 1 > 100:
            raise ValueError('count range must be ascending, within 0..1000, and at most 100 numbers')
        self._retire_worker()
        self._discard_prepares()
        self._generation += 1
        self._plan_id += 1
        self._plan = {'start': start, 'end': end, 'next': start, 'state': 'playing'}
        self._playing.set()
        self._publish()
        self._spawn_worker()

    def pause(self) -> None:
        if self._closed or self._plan is None or self._plan['state'] != 'playing':
            return
        self._generation += 1
        self._plan['state'] = 'paused'
        self._playing.clear()
        self._retire_worker()
        self._publish()

    def resume(self) -> None:
        if self._closed or self._plan is None or self._plan['state'] != 'paused':
            return
        self._generation += 1
        self._plan['state'] = 'playing'
        self._playing.set()
        self._publish()
        self._spawn_worker()

    async def close(self) -> None:
        if not self._closed:
            self.pause()
            self._closed = True
            self._generation += 1
            self._playing.clear()
            self._retire_worker()
            self._discard_prepares()
        if self._tasks:
            await asyncio.gather(*tuple(self._tasks), return_exceptions=True)

    def _publish(self) -> None:
        self._emit({'type': 'count.state', **self.snapshot})

    def _track(self, coroutine) -> asyncio.Task:
        task = asyncio.create_task(coroutine)
        self._tasks.add(task)
        task.add_done_callback(self._done)
        return task

    def _done(self, task: asyncio.Task) -> None:
        self._tasks.discard(task)
        self._retiring.discard(task)
        # A future prefetched unit can fail before the current one is played.
        # Its exception remains on the task for the worker, but is observed now.
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

    def _spawn_worker(self) -> None:
        # Await old playback cleanup before a replacement can use the output.
        self._worker = self._track(self._run(self._generation, tuple(self._retiring)))

    def _current(self, generation: int) -> bool:
        return not self._closed and generation == self._generation and self._plan['state'] == 'playing'

    async def _synthesize(self, number: int, plan_id: int) -> bytes:
        # A prefetch task scheduled just before pause must not begin a new call
        # once paused. Already-started calls continue and are reused on resume.
        while True:
            await self._playing.wait()
            if self._closed or plan_id != self._plan_id:
                raise asyncio.CancelledError
            # Event.set() may have awakened us immediately before another
            # synchronous pause. Recheck state before issuing a cloud call.
            if self._plan['state'] == 'playing':
                break
        pcm = await self._prepare(number)
        if not isinstance(pcm, bytes) or not pcm or len(pcm) % 2 or len(pcm) > self.MAX_PCM_BYTES:
            raise ValueError('count audio must be nonempty mono 24k s16 PCM of at most 5 seconds')
        return pcm

    async def _run(self, generation: int, retired: tuple[asyncio.Task, ...]) -> None:
        if retired:
            # Cancelling this waiter must not cancel old cleanup a second time.
            await asyncio.shield(asyncio.gather(*retired, return_exceptions=True))
        while self._current(generation):
            number = self._plan['next']
            for upcoming in range(number, min(self._plan['end'] + 1, number + self._prefetch)):
                if upcoming not in self._prepared:
                    self._prepared[upcoming] = self._track(self._synthesize(upcoming, self._plan_id))
            try:
                pcm = await asyncio.shield(self._prepared[number])
            except asyncio.CancelledError:
                raise
            except Exception:
                if self._current(generation):
                    self._plan['state'] = 'failed'
                    self._playing.clear()
                    self._discard_prepares()
                    self._publish()
                return
            if not self._current(generation):
                return
            try:
                complete = await self._play(number, pcm)
            except asyncio.CancelledError:
                raise
            except Exception:
                complete = False
            if not self._current(generation):
                return
            if complete is not True:
                self._plan['state'] = 'paused'
                self._playing.clear()
                self._publish()
                return
            del self._prepared[number]
            self._plan['next'] = number + 1
            if number == self._plan['end']:
                self._plan['state'] = 'completed'
                self._playing.clear()
            self._publish()
