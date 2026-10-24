import asyncio
import uuid
import weakref
import time

from livekit import rtc
from livekit.agents import APIError, stt, tts
from livekit.agents.types import APIConnectOptions, NOT_GIVEN
from livekit.agents.vad import VADEventType
from livekit.plugins import openai

from .cloud import Cloud, CloudError


NO_RETRY = APIConnectOptions(max_retry=0, timeout=30)


class DashScopeSTT(stt.STT):
    def __init__(self, cfg):
        super().__init__(capabilities=stt.STTCapabilities(streaming=True, interim_results=True))
        self.cfg = cfg
        self.streams = weakref.WeakSet()
        self.turn_epoch = 0
        self.has_final = False
        self.turn_finished = asyncio.Event()
        self.input_started = asyncio.Event()

    def stream(self, *, language=NOT_GIVEN, conn_options=NO_RETRY):
        stream = Recognition(stt=self, conn_options=NO_RETRY, sample_rate=16000)
        self.streams.add(stream)
        return stream

    def finish_input(self, *, required=True):
        flushed = 0
        for stream in tuple(self.streams):
            try:
                stream.flush()
                flushed += 1
            except RuntimeError:
                self.streams.discard(stream)
        if required and not flushed:
            raise RuntimeError('no active recognition stream')

    def begin_turn(self):
        self.turn_epoch += 1
        self.has_final = False
        self.turn_finished.clear()
        self.input_started.clear()
        self.finish_input(required=False)

    async def _recognize_impl(self, buffer, *, language=NOT_GIVEN, conn_options=NO_RETRY):
        frame = rtc.combine_audio_frames(buffer)
        if frame.sample_rate != 16000 or frame.num_channels != 1 or frame.samples_per_channel > 16000 * 29:
            raise APIError('recognition requires at most 29 seconds of mono16 16k audio', retryable=False)
        async def frames():
            data = bytes(frame.data)
            for offset in range(0, len(data), 3200):
                yield data[offset:offset + 3200]
        events = [ev async for ev in Cloud(self.cfg).asr(frames())]
        text = ''.join(ev['text'] for ev in events if ev['final'])
        return stt.SpeechEvent(type=stt.SpeechEventType.FINAL_TRANSCRIPT, alternatives=[stt.SpeechData(language='zh', text=text)])


class Recognition(stt.SpeechStream):
    async def aclose(self):
        try:
            await super().aclose()
        finally:
            self._stt.streams.discard(self)

    async def _run(self):
        try:
            async for first in self._input_ch:
                if not isinstance(first, rtc.AudioFrame):
                    continue
                epoch = self._stt.turn_epoch
                self._stt.input_started.set()
                async def frames():
                    yield bytes(first.data)
                    async for frame in self._input_ch:
                        if not isinstance(frame, rtc.AudioFrame):
                            break
                        yield bytes(frame.data)
                speaking = False
                async for result in Cloud(self._stt.cfg).asr(frames()):
                    if epoch != self._stt.turn_epoch:
                        continue
                    if not speaking:
                        speaking = True
                        self._event_ch.send_nowait(stt.SpeechEvent(type=stt.SpeechEventType.START_OF_SPEECH))
                    self._event_ch.send_nowait(stt.SpeechEvent(
                        type=stt.SpeechEventType.FINAL_TRANSCRIPT if result['final'] else stt.SpeechEventType.INTERIM_TRANSCRIPT,
                        alternatives=[stt.SpeechData(language='zh', text=result['text'])],
                    ))
                    if result['final']:
                        self._stt.has_final = True
                        speaking = False
                        self._event_ch.send_nowait(stt.SpeechEvent(type=stt.SpeechEventType.END_OF_SPEECH))
                if epoch == self._stt.turn_epoch:
                    self._stt.turn_finished.set()
        except CloudError as error:
            raise APIError(str(error), retryable=False) from None


class TurnSTT(stt.STT):
    """VAD-separated recognition with explicit ownership of in-flight segments.

    A new speech onset retires the previous pending recognition. Unlike a
    serial adapter, VAD consumption never waits for the cloud terminal event.
    """
    def __init__(self, wrapped, vad):
        super().__init__(capabilities=stt.STTCapabilities(streaming=True, interim_results=False))
        self.wrapped, self.vad = wrapped, vad
        self.streams = weakref.WeakSet()

    def stream(self, *, language=NOT_GIVEN, conn_options=NO_RETRY):
        stream = TurnRecognition(stt=self, conn_options=NO_RETRY, sample_rate=16000)
        self.streams.add(stream)
        return stream

    async def _recognize_impl(self, buffer, *, language=NOT_GIVEN, conn_options=NO_RETRY):
        return await self.wrapped.recognize(buffer, language=language, conn_options=NO_RETRY)

    async def aclose(self):
        for stream in tuple(self.streams):
            await stream.aclose()

    def discard_current(self):
        for stream in tuple(self.streams):
            stream.discard_current()


class TurnRecognition(stt.SpeechStream):
    _discard_current = False

    def discard_current(self):
        self._discard_current = True

    async def aclose(self):
        try:
            await super().aclose()
        finally:
            self._stt.streams.discard(self)

    async def _run(self):
        detector = self._stt.vad.stream()
        generation, active = 0, None
        jobs = set()

        async def forward():
            async for value in self._input_ch:
                if isinstance(value, rtc.AudioFrame):
                    detector.push_frame(value)
                else:
                    detector.flush()
            detector.end_input()

        async def recognize(frames, epoch, ended):
            if epoch != generation:
                return
            try:
                result = await self._stt.wrapped.recognize(rtc.combine_audio_frames(frames), conn_options=NO_RETRY)
            except asyncio.CancelledError:
                raise
            except Exception:
                if epoch != generation:
                    return
                raise
            if epoch == generation and not self._discard_current and result.alternatives and result.alternatives[0].text:
                self._event_ch.send_nowait(stt.SpeechEvent(type=stt.SpeechEventType.FINAL_TRANSCRIPT, alternatives=result.alternatives, speech_end_time=ended))

        async def detect(group):
            nonlocal generation, active
            async for event in detector:
                if event.type == VADEventType.START_OF_SPEECH:
                    self._discard_current = False
                    generation += 1
                    if active and not active.done():
                        active.cancel()
                    self._event_ch.send_nowait(stt.SpeechEvent(type=stt.SpeechEventType.START_OF_SPEECH))
                elif event.type == VADEventType.END_OF_SPEECH:
                    self._event_ch.send_nowait(stt.SpeechEvent(type=stt.SpeechEventType.END_OF_SPEECH))
                    if self._discard_current:
                        self._discard_current = False
                        continue
                    if len(jobs) >= 4:
                        raise APIError('recognition backlog limit', retryable=False)
                    ended = time.time() - event.silence_duration - event.inference_duration
                    active = group.create_task(recognize(event.frames, generation, ended))
                    jobs.add(active)
                    active.add_done_callback(jobs.discard)

        try:
            try:
                async with asyncio.TaskGroup() as group:
                    group.create_task(forward())
                    group.create_task(detect(group))
            except ExceptionGroup:
                raise APIError('continuous recognition failed', retryable=False) from None
        finally:
            await detector.aclose()


class DashScopeTTS(tts.TTS):
    def __init__(self, cfg):
        super().__init__(capabilities=tts.TTSCapabilities(streaming=False), sample_rate=24000, num_channels=1)
        self.cfg = cfg

    def synthesize(self, text, *, conn_options=NO_RETRY):
        return Synthesis(tts=self, input_text=text, conn_options=NO_RETRY)


class Synthesis(tts.ChunkedStream):
    async def _run(self, output_emitter):
        output_emitter.initialize(request_id=uuid.uuid4().hex, sample_rate=24000, num_channels=1, mime_type='audio/pcm', frame_size_ms=20)
        try:
            async for pcm in Cloud(self._tts.cfg).tts(self._input_text):
                output_emitter.push(pcm)
        except CloudError as error:
            raise APIError(str(error), retryable=False) from None


def text_model(cfg, *, max_tokens=128):
    return openai.LLM(model=cfg.llm_model, api_key=cfg.api_key, base_url=cfg.llm_url, max_retries=0,
                      prompt_cache_breakpoints=False, extra_body={'enable_thinking': False, 'max_tokens': max_tokens})
