import asyncio
import base64
from collections import deque
from dataclasses import dataclass

from livekit import rtc
from livekit.agents.voice import io


class PCMInput(io.AudioInput):
    def __init__(self, *, capacity=100):
        super().__init__(label='pcm-input')
        self.queue = asyncio.Queue(maxsize=capacity)
        self.closed = False
        self.previous_pending = False

    def push(self, pcm):
        if self.closed:
            raise RuntimeError('input closed')
        if not pcm or len(pcm) % 2 or len(pcm) > 640:
            raise ValueError('expected at most 20ms mono16 16k PCM')
        try:
            self.queue.put_nowait(rtc.AudioFrame(pcm, 16000, 1, len(pcm) // 2))
        except asyncio.QueueFull:
            raise BufferError('input queue full') from None

    async def __anext__(self):
        if self.previous_pending:
            self.queue.task_done()
            self.previous_pending = False
        if self.closed and self.queue.empty():
            raise StopAsyncIteration
        value = await self.queue.get()
        if value is None:
            self.queue.task_done()
            raise StopAsyncIteration
        self.previous_pending = True
        return value

    async def drain(self):
        await asyncio.wait_for(self.queue.join(), 1)

    def close(self):
        self.closed = True
        while not self.queue.empty():
            self.queue.get_nowait()
            self.queue.task_done()
        self.queue.put_nowait(None)

    def discard(self):
        while not self.queue.empty():
            self.queue.get_nowait()
            self.queue.task_done()


@dataclass
class Block:
    id: int
    epoch: int
    sent: int = 0
    played: int = 0
    valid: bool = True
    sealed: bool = False
    interrupted: bool = False
    finished: bool = False
    timer: asyncio.Task | None = None


class PCMOutput(io.AudioOutput):
    def __init__(self, send, *, settle_timeout=.25, normal_slack=2):
        super().__init__(label='pcm-output', capabilities=io.AudioOutputCapabilities(pause=False), sample_rate=24000)
        self.send = send
        self.settle_timeout, self.normal_slack = settle_timeout, normal_slack
        self.epoch = self.sequence = 0
        self.active = None
        self.pending = deque()
        self.jobs = set()
        self.last_observation = None
        self.closed = False

    @property
    def active_identity(self):
        return {'epoch': self.active.epoch, 'block_id': self.active.id} if self.active else None

    async def _send(self, event):
        await asyncio.wait_for(self.send(event), .25)

    def _spawn(self, coroutine):
        job = asyncio.create_task(coroutine)
        self.jobs.add(job)
        def done(future):
            self.jobs.discard(future)
            if not future.cancelled():
                future.exception()
        job.add_done_callback(done)
        return job

    async def capture_frame(self, frame):
        if self.closed:
            raise RuntimeError('output closed')
        if frame.sample_rate != 24000 or frame.num_channels != 1 or frame.samples_per_channel <= 0:
            raise ValueError('expected mono 24k PCM')
        if self.active is None:
            if len(self.pending) >= 32:
                raise BufferError('playback ledger full')
            self.sequence += 1
            self.active = Block(self.sequence, self.epoch)
            self.pending.append(self.active)
        block = self.active
        if sum(b.sent - b.played for b in self.pending) + frame.samples_per_channel > 24000 * 120:
            raise BufferError('playback limit')
        await super().capture_frame(frame)
        block.sent += frame.samples_per_channel
        await self._send({'type': 'audio', 'epoch': block.epoch, 'block_id': block.id, 'samples': frame.samples_per_channel, 'data': base64.b64encode(bytes(frame.data)).decode()})

    def flush(self):
        super().flush()
        block = self.active
        self.active = None
        if block and not block.finished:
            block.sealed = True
            self._spawn(self._send({'type': 'audio.end', 'epoch': block.epoch, 'block_id': block.id, 'sent_samples': block.sent}))
            if block.valid:
                self._deadline(block, block.sent / 24000 + self.normal_slack)
            else:
                block.finished = True
                self._drain()

    def _deadline(self, block, delay):
        if block.timer:
            block.timer.cancel()
        async def expire():
            await asyncio.sleep(delay)
            if not block.finished:
                block.valid = False
                block.finished = True
                self._drain()
        block.timer = self._spawn(expire())

    def clear_buffer(self):
        old_epoch = self.epoch
        self.epoch += 1
        super().flush()
        self.active = None
        for block in self.pending:
            if not block.finished:
                block.sealed = block.interrupted = True
                self._deadline(block, self.settle_timeout)
        self._spawn(self._send({'type': 'audio.cancel', 'epoch': old_epoch, 'next_epoch': self.epoch}))

    def receipt(self, block_id, epoch, played_samples, *, valid=True, finished=True):
        if type(block_id) is not int or type(epoch) is not int or type(played_samples) is not int or type(valid) is not bool or type(finished) is not bool:
            return False
        block = next((b for b in self.pending if b.id == block_id and b.epoch == epoch), None)
        if not block or block.finished:
            return False
        if not valid:
            block.valid = False
            if block.sealed:
                block.finished = True
                self._drain()
            return False
        if not block.valid or not block.played <= played_samples <= block.sent:
            return False
        if finished and (not block.sealed or (not block.interrupted and played_samples != block.sent)):
            return False
        block.played = played_samples
        if finished:
            block.finished = True
            self._drain()
        return True

    def _drain(self):
        while self.pending and self.pending[0].finished:
            block = self.pending.popleft()
            if block.timer and block.timer is not asyncio.current_task():
                block.timer.cancel()
            self.last_observation = {'block_id': block.id, 'epoch': block.epoch, 'sent_samples': block.sent, 'played_samples': block.played if block.valid else None, 'valid': block.valid}
            # An unknown lower-bound completion releases SDK lifecycle only.
            # It never becomes measured playback or synchronized transcript.
            self.on_playback_finished(playback_position=block.played / 24000 if block.valid else 0, interrupted=block.interrupted or not block.valid, synchronized_transcript=None)

    async def aclose(self):
        self.closed = True
        for block in self.pending:
            block.valid = False
            block.finished = True
        self._drain()
        jobs = tuple(self.jobs)
        for job in jobs:
            job.cancel()
        await asyncio.gather(*jobs, return_exceptions=True)
