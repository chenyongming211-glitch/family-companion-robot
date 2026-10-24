"""Receipt-driven audio with bounded credits and drain rather than hard cancel.

SDK generation lifetime and device playback lifetime are deliberately separate:
interrupting a SpeechHandle releases its SDK segment as unknown, while its real
device ledger remains until a final valid observation (or explicit failure).
"""
import asyncio
import base64
import time
from collections import OrderedDict
from dataclasses import dataclass

from livekit.agents.voice import io


@dataclass
class Binding:
    revision: int
    index: int
    expected: int
    settle: object
    task_id: str | None = None
    block_id: int | None = None
    done: bool = False


@dataclass
class DrainBlock:
    id: int
    epoch: int
    after: tuple
    input_epoch: int
    binding: Binding | None
    sent: int = 0
    played: int = 0
    sealed: bool = False
    sdk_done: bool = False
    cancelled: bool = False
    started: bool = False
    finished: bool = False
    valid: bool = True
    discarded: bool = False
    touched: float = 0
    seal_job: asyncio.Task | None = None


class DrainOutput(io.AudioOutput):
    def __init__(self, send, *, window_samples=7200, staged_samples=2880,
                 hard_samples=10080, receipt_timeout=2.5):
        super().__init__(label='drain-output', capabilities=io.AudioOutputCapabilities(pause=False), sample_rate=24000)
        if not 0 < window_samples <= hard_samples or not 0 < staged_samples <= hard_samples:
            raise ValueError('invalid playback credits')
        self.send = send
        self.window_samples, self.staged_samples, self.hard_samples = window_samples, staged_samples, hard_samples
        self.receipt_timeout = receipt_timeout
        self.epoch = self.sequence = self.input_epoch = 0
        self.blocks, self.history = OrderedDict(), OrderedDict()
        self.active = self.last_observation = self._binding = None
        self.closed = self.failed = False
        self.jobs = set()
        self._lock = asyncio.Lock()
        self._credit = asyncio.Event()
        self._credit.set()
        self._barrier = None
        self._watchdog = self._spawn(self._watch())

    @property
    def active_identity(self):
        return {'block_id': self.active.id, 'epoch': self.active.epoch} if self.active else None

    @property
    def outstanding_samples(self):
        return sum(b.sent - b.played for b in self.blocks.values() if not b.finished)

    def bind_unit(self, revision, index, expected_samples, settle, *, task_id=None):
        if self._binding is not None:
            raise RuntimeError('unclaimed content binding')
        if type(expected_samples) is not int or not 0 < expected_samples <= 24000 * 12:
            raise ValueError('invalid content sample limit')
        binding = Binding(revision, index, expected_samples, settle, task_id=task_id)
        self._binding = binding
        return binding

    def cancel_binding(self, binding):
        if self._binding is binding:
            self._binding = None
            self._settle_binding(binding, False)

    def _settle_binding(self, binding, complete):
        if binding and not binding.done:
            binding.done = True
            binding.settle(complete)

    def _spawn(self, coroutine):
        if len(self.jobs) >= 64:
            coroutine.close()
            raise BufferError('output control queue limit')
        job = asyncio.create_task(coroutine)
        self.jobs.add(job)
        def done(future):
            self.jobs.discard(future)
            if not future.cancelled():
                future.exception()
        job.add_done_callback(done)
        return job

    async def _send(self, event):
        await asyncio.wait_for(self.send(event), .25)

    async def _control(self, event):
        async with self._lock:
            await self._send(event)

    def _can_send(self, epoch, samples):
        total = self.outstanding_samples
        old = any(b.epoch < epoch and not b.finished for b in self.blocks.values())
        if old:
            staged = sum(b.sent - b.played for b in self.blocks.values() if b.epoch == epoch and not b.finished)
            return total + samples <= self.hard_samples and staged + samples <= self.staged_samples
        return total + samples <= self.window_samples

    async def capture_frame(self, frame):
        if frame.sample_rate != 24000 or frame.num_channels != 1 or not 0 < frame.samples_per_channel <= 480:
            raise ValueError('expected at most 20ms mono 24k audio')
        epoch = self.epoch
        if self._barrier:
            await asyncio.shield(self._barrier)
        while True:
            async with self._lock:
                if self.closed or self.failed:
                    raise RuntimeError('output unavailable')
                if epoch != self.epoch:
                    raise asyncio.CancelledError
                if self._can_send(epoch, frame.samples_per_channel):
                    if self.active is None:
                        if len(self.blocks) >= 16:
                            raise BufferError('device playback ledger full')
                        self.sequence += 1
                        after = tuple(b.id for b in self.blocks.values() if b.epoch < epoch and not b.finished)
                        binding, self._binding = self._binding, None
                        self.active = DrainBlock(self.sequence, epoch, after, self.input_epoch, binding, touched=time.monotonic())
                        block = self.active
                        if binding:
                            binding.block_id = self.sequence
                        self.blocks[self.sequence] = self.active
                        await super().capture_frame(frame)
                        await self._send({'type': 'output.handoff', 'block_id': block.id, 'epoch': epoch, 'after': list(after), 'input_epoch': self.input_epoch,
                                          **({'content_id': binding.task_id, 'content_revision': binding.revision, 'unit_index': binding.index} if binding and binding.task_id else {})})
                    else:
                        block = self.active
                        await super().capture_frame(frame)
                    if epoch != self.epoch:
                        raise asyncio.CancelledError
                    block.sent += frame.samples_per_channel
                    block.touched = time.monotonic()
                    await self._send({'type': 'audio', 'block_id': block.id, 'epoch': block.epoch,
                                      'samples': frame.samples_per_channel, 'data': base64.b64encode(bytes(frame.data)).decode()})
                    return
                self._credit.clear()
            await self._credit.wait()

    def flush(self):
        super().flush()
        block, self.active = self.active, None
        if block and block.seal_job is None:
            block.seal_job = self._spawn(self._seal(block))

    async def _seal(self, block):
        async with self._lock:
            if not block.sealed and not block.finished:
                block.sealed = True
                block.touched = time.monotonic()
                await self._send({'type': 'audio.end', 'block_id': block.id, 'epoch': block.epoch,
                                  'sent_samples': block.sent, 'policy': 'drain' if block.cancelled else 'natural'})

    def clear_buffer(self):
        # Cancelling generation fences its sender, but never clears admitted PCM.
        old_epoch = self.epoch
        self.epoch += 1
        affected = tuple(b for b in self.blocks.values() if not b.finished)
        for block in affected:
            if block.epoch == old_epoch:
                block.cancelled = True
        self.flush()
        self._credit.set()
        self._barrier = self._spawn(self._drain(affected))

    async def _drain(self, blocks):
        for block in blocks:
            if block.seal_job:
                await asyncio.shield(block.seal_job)
        async with self._lock:
            revoked = [b.id for b in blocks if b.after and not b.started and not b.finished]
            if revoked:
                await self._send({'type': 'output.revoke', 'blocks': revoked, 'input_epoch': self.input_epoch})
            await self._send({'type': 'output.drain', 'blocks': [b.id for b in blocks if not b.finished], 'next_epoch': self.epoch})
            for block in blocks:
                if not block.sdk_done:
                    block.sdk_done = True
                    # This is SDK lifecycle-only, not a measured completion.
                    self.on_playback_finished(playback_position=0, interrupted=True, synchronized_transcript=None)

    def mark_started(self, block_id, epoch):
        block = self.blocks.get(block_id)
        if not block or block.epoch != epoch or block.finished:
            return False
        if any(i in self.blocks for i in block.after):
            return False
        block.started = True
        return True

    def receipt(self, block_id, epoch, played_samples, *, valid=True, finished=True, discarded=False):
        if (type(block_id) is not int or type(epoch) is not int or type(played_samples) is not int
            or type(valid) is not bool or type(finished) is not bool or type(discarded) is not bool):
            return False
        block = self.blocks.get(block_id) or self.history.get(block_id)
        if not block or block.epoch != epoch:
            return False
        if block.finished:
            accepted = finished and valid == block.valid and played_samples == block.played and discarded == block.discarded
            if accepted:
                self._ack(block)
            return accepted
        if discarded and (not block.after or block.started or played_samples != 0 or not finished):
            return False
        if not block.played <= played_samples <= block.sent:
            return False
        if not discarded and played_samples > 0 and any(i in self.blocks for i in block.after):
            return False
        if finished and (not block.sealed or (not block.cancelled and not discarded and played_samples != block.sent)):
            return False
        if not valid:
            self._fail('playback_unknown')
            return False
        if played_samples > block.played:
            block.touched = time.monotonic()
        block.played = played_samples
        if played_samples:
            block.started = True
        if finished:
            block.discarded = discarded
            self._finish(block)
            self._ack(block)
        self._credit.set()
        return True

    def _ack(self, block):
        self._spawn(self._control({'type': 'playback.ack', 'block_id': block.id, 'epoch': block.epoch, 'played_samples': block.played}))

    def _finish(self, block):
        if block.finished:
            return
        block.finished = True
        self.blocks.pop(block.id, None)
        self.history[block.id] = block
        while len(self.history) > 64:
            self.history.popitem(last=False)
        self.last_observation = {'block_id': block.id, 'epoch': block.epoch, 'sent_samples': block.sent,
                                 'played_samples': block.played if block.valid else None, 'valid': block.valid}
        complete = (block.valid and not block.discarded and block.binding is not None
                    and block.played == block.sent == block.binding.expected)
        self._settle_binding(block.binding, complete)
        if not block.sdk_done:
            block.sdk_done = True
            self.on_playback_finished(playback_position=block.played / 24000 if block.valid else 0,
                                      interrupted=not block.valid or block.cancelled or block.discarded,
                                      synchronized_transcript=None)
        self._credit.set()

    def _fail(self, code):
        if self.failed or self.closed:
            return
        self.failed = True
        for block in tuple(self.blocks.values()):
            block.valid = False
            self._finish(block)
        self._spawn(self._control({'type': 'error', 'code': code}))
        self._credit.set()

    async def _watch(self):
        while not self.closed:
            await asyncio.sleep(.01)
            if any(time.monotonic() - b.touched > self.receipt_timeout for b in self.blocks.values()):
                self._fail('playback_unknown')

    async def aclose(self):
        if self.closed:
            return
        self.closed = True
        for block in tuple(self.blocks.values()):
            block.valid = False
            self._finish(block)
        if self._binding:
            self._settle_binding(self._binding, False)
            self._binding = None
        self._credit.set()
        jobs = tuple(self.jobs)
        for job in jobs:
            job.cancel()
        await asyncio.gather(*jobs, return_exceptions=True)
