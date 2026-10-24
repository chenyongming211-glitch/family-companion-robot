import uuid

from livekit import rtc
from livekit.agents import APIError, stt, tts
from livekit.agents.types import APIConnectOptions, NOT_GIVEN
from livekit.plugins import openai

from .cloud import Cloud, CloudError


NO_RETRY = APIConnectOptions(max_retry=0, timeout=30)


class DashScopeSTT(stt.STT):
    def __init__(self, cfg):
        super().__init__(capabilities=stt.STTCapabilities(streaming=True, interim_results=True))
        self.cfg = cfg

    def stream(self, *, language=NOT_GIVEN, conn_options=NO_RETRY):
        return Recognition(stt=self, conn_options=NO_RETRY, sample_rate=16000)

    async def _recognize_impl(self, buffer, *, language=NOT_GIVEN, conn_options=NO_RETRY):
        frame = rtc.combine_audio_frames(buffer)
        async def frames():
            yield bytes(frame.data)
        events = [ev async for ev in Cloud(self.cfg).asr(frames())]
        text = ''.join(ev['text'] for ev in events if ev['final'])
        return stt.SpeechEvent(type=stt.SpeechEventType.FINAL_TRANSCRIPT, alternatives=[stt.SpeechData(language='zh', text=text)])


class Recognition(stt.SpeechStream):
    async def _run(self):
        try:
            async for first in self._input_ch:
                if not isinstance(first, rtc.AudioFrame):
                    continue
                async def frames():
                    yield bytes(first.data)
                    async for frame in self._input_ch:
                        if not isinstance(frame, rtc.AudioFrame):
                            break
                        yield bytes(frame.data)
                speaking = False
                async for result in Cloud(self._stt.cfg).asr(frames()):
                    if not speaking:
                        speaking = True
                        self._event_ch.send_nowait(stt.SpeechEvent(type=stt.SpeechEventType.START_OF_SPEECH))
                    self._event_ch.send_nowait(stt.SpeechEvent(
                        type=stt.SpeechEventType.FINAL_TRANSCRIPT if result['final'] else stt.SpeechEventType.INTERIM_TRANSCRIPT,
                        alternatives=[stt.SpeechData(language='zh', text=result['text'])],
                    ))
                    if result['final']:
                        speaking = False
                        self._event_ch.send_nowait(stt.SpeechEvent(type=stt.SpeechEventType.END_OF_SPEECH))
        except CloudError as error:
            raise APIError(str(error), retryable=False) from None


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


def text_model(cfg):
    return openai.LLM(model=cfg.llm_model, api_key=cfg.api_key, base_url=cfg.llm_url, max_retries=0,
                      prompt_cache_breakpoints=False, extra_body={'enable_thinking': False, 'max_tokens': 128})
