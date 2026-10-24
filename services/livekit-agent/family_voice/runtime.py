import asyncio
from dataclasses import asdict, is_dataclass

from livekit.agents import Agent, AgentSession
from livekit.agents.voice.agent_session import SessionConnectOptions
from livekit.plugins import silero

from .audio_io import PCMInput, PCMOutput
from .cloud import Intent, numeric_usage
from .providers import DashScopeSTT, DashScopeTTS, NO_RETRY, text_model


class AuditedAgent(Agent):
    def __init__(self, cfg):
        super().__init__(instructions='你是家庭陪伴语音助手。用简短、自然的中文回答，默认不超过两句话。数学问题直接给出答案。不要声称已经执行没有执行的操作。')
        self.cfg = cfg

    async def llm_node(self, chat_ctx, tools, model_settings):
        intent = Intent(self.cfg, 'llm')
        intent.update(state='submitted_unknown')
        usage = None
        try:
            async for chunk in Agent.default.llm_node(self, chat_ctx, tools, model_settings):
                value = getattr(chunk, 'usage', None)
                if value is not None:
                    usage = numeric_usage(asdict(value) if is_dataclass(value) else value.model_dump())
                yield chunk
            intent.update(state='completed', usage=usage)
        except asyncio.CancelledError:
            intent.update(state='cancelled_unknown', usage=usage)
            raise
        except Exception as error:
            intent.update(state='outcome_unknown', error_type=type(error).__name__)
            raise


class VoiceSession:
    def __init__(self, cfg, send):
        self.cfg, self.send = cfg, send
        self.input = PCMInput()
        self.output = PCMOutput(send)
        self.providers = [DashScopeSTT(cfg), text_model(cfg), DashScopeTTS(cfg), silero.VAD.load(max_buffered_speech=30, force_cpu=True)]
        self.session = AgentSession(
            stt=self.providers[0], llm=self.providers[1], tts=self.providers[2], vad=self.providers[3],
            turn_handling={'turn_detection': 'manual', 'interruption': {'resume_false_interruption': False}, 'preemptive_generation': {'enabled': False}},
            use_tts_aligned_transcript=False, user_away_timeout=None,
            conn_options=SessionConnectOptions(stt_conn_options=NO_RETRY, llm_conn_options=NO_RETRY, tts_conn_options=NO_RETRY),
        )
        self.session.input.audio = self.input
        self.session.output.audio = self.output
        self.jobs = set()
        self.closed = False

    def _emit(self, event):
        if self.closed:
            return
        if len(self.jobs) >= 32:
            self.input.close()
            return
        job = asyncio.create_task(asyncio.wait_for(self.send(event), .25))
        self.jobs.add(job)
        def done(future):
            self.jobs.discard(future)
            if not future.cancelled():
                future.exception()
        job.add_done_callback(done)

    async def start(self):
        @self.session.on('user_input_transcribed')
        def transcribed(event):
            self._emit({'type': 'transcript', 'text': event.transcript, 'final': event.is_final})
        @self.session.on('conversation_item_added')
        def item_added(event):
            if getattr(event.item, 'role', None) == 'assistant':
                self._emit({'type': 'reply', 'text': event.item.text_content, 'interrupted': getattr(event.item, 'interrupted', False)})
        @self.session.on('error')
        def error(event):
            self._emit({'type': 'error', 'stage': 'runtime', 'code': type(event.error).__name__})
        await self.session.start(AuditedAgent(self.cfg), record=False)

    async def begin_input(self):
        await self.interrupt()
        self.input.discard()
        self.session.clear_user_turn()

    def feed(self, pcm):
        self.input.push(pcm)

    async def commit(self):
        await self.input.drain()
        committed = self.session.commit_user_turn(transcript_timeout=5, stt_flush_duration=0)
        self.providers[0].finish_input()
        return await committed

    async def interrupt(self):
        await asyncio.wait_for(self.session.interrupt(), 1)

    def receipt(self, event):
        return self.output.receipt(event.get('block_id'), event.get('epoch'), event.get('played_samples'), valid=event.get('valid', True), finished=event.get('finished', True))

    async def close(self):
        if self.closed:
            return
        self.closed = True
        self.input.close()
        await self.output.aclose()
        try:
            await asyncio.wait_for(self.session.aclose(), 3)
        finally:
            for job in tuple(self.jobs):
                job.cancel()
            await asyncio.gather(*self.jobs, return_exceptions=True)
            for provider in self.providers[:3]:
                await provider.aclose()
