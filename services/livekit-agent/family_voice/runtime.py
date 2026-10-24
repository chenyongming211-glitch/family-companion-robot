import asyncio
from dataclasses import asdict, is_dataclass

from livekit.agents import Agent, AgentSession, StopResponse
from livekit.agents.voice.agent_session import SessionConnectOptions
from livekit.plugins import silero
from livekit import rtc

from .audio_io import PCMInput, PCMOutput
from .cloud import Cloud, Intent, numeric_usage
from .counting import CountController, is_count_resume, parse_count_request
from .providers import DashScopeSTT, DashScopeTTS, NO_RETRY, TurnSTT, text_model


async def play_count_unit(session, output, number, pcm):
    """A generated number becomes progress only after its own full receipt."""
    first, epoch = output.sequence, output.epoch
    async def audio():
        for i in range(0, len(pcm), 960):
            chunk = pcm[i:i + 960]
            yield rtc.AudioFrame(chunk, 24000, 1, len(chunk) // 2)
    handle = session.say(f'{number}。', audio=audio(), add_to_chat_ctx=False)
    try:
        await handle.wait_for_playout()
    except asyncio.CancelledError:
        handle.interrupt(force=True)
        # The controller joins this worker before starting a replacement.
        # Retire SDK audio/receipt bookkeeping before releasing that barrier.
        await handle.wait_for_playout()
        raise
    observed = output.last_observation
    return bool(not handle.interrupted and observed and observed['valid']
                and observed['block_id'] == first + 1 and observed['epoch'] == epoch
                and observed['played_samples'] == observed['sent_samples'] == len(pcm) // 2)


class AuditedAgent(Agent):
    def __init__(self, cfg, owner=None):
        instructions = '你是家庭陪伴语音助手。用简短、自然的中文回答，默认不超过两句话。数学问题直接给出答案。不要声称已经执行没有执行的操作。'
        if owner:
            instructions += '用户明确要求逐个数数或逐项列举时，必须完整逐项输出，不得用省略号、范围、跳号代替。当前仅接通聊天、讲故事和数学问答，没有天气查询、闹钟或设备控制工具，不要声称能执行这些工具。'
        super().__init__(instructions=instructions)
        self.cfg = cfg
        self.owner = owner

    async def on_user_turn_completed(self, turn_ctx, new_message):
        if self.owner:
            if self.owner.sleeping or self.owner.closed:
                raise StopResponse()
            if self.owner.turns >= self.owner.automatic_max_turns:
                self.owner.sleep('turn_limit')
                raise StopResponse()
            self.owner.turns += 1
            self.owner._emit({'type': 'turn', 'count': self.owner.turns, 'max_turns': self.owner.automatic_max_turns})
            if self.owner.handle_count_turn(new_message.text_content):
                raise StopResponse()

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
    def __init__(self, cfg, send, *, automatic=False, automatic_idle_seconds=60, automatic_max_turns=8, automatic_utterance_seconds=25):
        self.cfg, self.send = cfg, send
        self.automatic, self.automatic_max_turns = automatic, automatic_max_turns
        self.automatic_utterance_seconds = automatic_utterance_seconds
        self.turns, self.sleeping, self.speech_timer = 0, False, None
        self.input = PCMInput()
        self.output = PCMOutput(send)
        vad = silero.VAD.load(max_buffered_speech=30, force_cpu=True, **({'min_speech_duration': .12, 'min_silence_duration': .65, 'prefix_padding_duration': .3, 'activation_threshold': .6} if automatic else {}))
        self.providers = [DashScopeSTT(cfg), text_model(cfg, max_tokens=512 if automatic else 128), DashScopeTTS(cfg), vad]
        self.recognizer = TurnSTT(self.providers[0], vad) if automatic else self.providers[0]
        turn_handling = {'turn_detection': 'manual', 'interruption': {'resume_false_interruption': False}, 'preemptive_generation': {'enabled': False}}
        if automatic:
            turn_handling.update(turn_detection='vad', endpointing={'min_delay': .35, 'max_delay': 1}, interruption={'mode': 'vad', 'enabled': True, 'min_duration': .2, 'min_words': 0, 'resume_false_interruption': False})
        self.session = AgentSession(
            stt=self.recognizer, llm=self.providers[1], tts=self.providers[2], vad=vad,
            turn_handling=turn_handling,
            use_tts_aligned_transcript=False, user_away_timeout=automatic_idle_seconds if automatic else None,
            conn_options=SessionConnectOptions(stt_conn_options=NO_RETRY, llm_conn_options=NO_RETRY, tts_conn_options=NO_RETRY),
        )
        self.session.input.audio = self.input
        self.session.output.audio = self.output
        self.jobs = set()
        self.closed = False
        self.commit_job = None
        self.counting = CountController(self.prepare_count, self.play_count, self._emit) if automatic else None

    async def prepare_count(self, number):
        pcm = bytearray()
        stream = Cloud(self.cfg).tts(f'{number}。')
        try:
            async for chunk in stream:
                if len(pcm) + len(chunk) > 24000 * 2 * 5:
                    raise ValueError('count unit audio limit')
                pcm.extend(chunk)
        finally:
            await stream.aclose()
        return bytes(pcm)

    async def play_count(self, number, pcm):
        if self.closed or self.sleeping:
            return False
        return await play_count_unit(self.session, self.output, number, pcm)

    def handle_count_turn(self, text):
        request = parse_count_request(text)
        if request:
            try:
                self.counting.start(*request)
            except ValueError:
                self.session.say('请指定零到一千以内的递增范围，一次最多数一百个数字。')
            return True
        plan = self.counting.snapshot
        if plan and is_count_resume(text):
            if plan['state'] == 'completed':
                self.session.say(f'已经数到{plan["end"]}，这次数数已完成。')
            elif plan['state'] == 'failed':
                self.session.say('上次数数的语音准备失败，请重新指定计数范围。')
            else:
                self.counting.resume()
            return True
        return False

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
        if self.automatic:
            @self.session.on('user_state_changed')
            def user_state(event):
                if self.closed:
                    return
                self._emit({'type': 'user.state', 'state': event.new_state})
                if self.speech_timer:
                    self.speech_timer.cancel()
                    self.speech_timer = None
                if event.new_state == 'away':
                    self.sleep('idle')
                elif event.new_state == 'speaking':
                    self.counting.pause()
                    async def limit():
                        await asyncio.sleep(self.automatic_utterance_seconds)
                        self.sleep('utterance_limit')
                    self.speech_timer = asyncio.create_task(limit())
            @self.session.on('agent_state_changed')
            def agent_state(event):
                self._emit({'type': 'agent.state', 'state': event.new_state})
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
        await self.session.start(AuditedAgent(self.cfg, self if self.automatic else None), record=False)

    def sleep(self, code):
        if not self.closed and not self.sleeping:
            self.sleeping = True
            if self.counting:
                self.counting.pause()
            self._emit({'type': 'sleep', 'code': code})

    async def begin_input(self):
        await self.interrupt()
        self.input.discard()
        self.session.clear_user_turn()

    def feed(self, pcm):
        self.input.push(pcm)

    async def commit(self):
        audit = Intent(self.cfg, 'input_commit')
        committed = None
        self.commit_job = asyncio.current_task()
        try:
            await self.input.drain()
            # An explicit PCM end must also terminate the audio front end.
            # Otherwise VAD remains speaking and small clips can remain in the
            # SDK's frame assembler instead of reaching the STT adapter.
            for _ in range(35):
                self.input.push(b'\0' * 640)
            await self.input.drain()
            await asyncio.wait_for(self.providers[0].input_started.wait(), 1)
            audit.update(state='input_drained', recognizers=len(self.providers[0].streams), user_state=self.session.user_state, agent_state=self.session.agent_state)
            self.providers[0].finish_input()
            audit.update(state='provider_flushed')
            await asyncio.wait_for(self.providers[0].turn_finished.wait(), 10)
            if not self.providers[0].has_final:
                raise RuntimeError('no provider final transcript')
            committed = self.session.commit_user_turn(transcript_timeout=5, stt_flush_duration=0)
            text = await committed
            audit.update(state='committed', transcript_characters=len(text), user_state=self.session.user_state, agent_state=self.session.agent_state)
            return text
        except BaseException as error:
            if committed and not committed.done():
                committed.cancel()
                self.session.clear_user_turn()
            audit.update(state='failed', error_type=type(error).__name__, user_state=self.session.user_state, agent_state=self.session.agent_state)
            raise
        finally:
            if self.commit_job is asyncio.current_task():
                self.commit_job = None

    async def interrupt(self):
        if self.counting:
            self.counting.pause()
        self.input.discard()
        pending = self.commit_job
        if pending and pending is not asyncio.current_task() and not pending.done():
            pending.cancel()
            await asyncio.gather(pending, return_exceptions=True)
        self.session.clear_user_turn()
        self.providers[0].begin_turn()
        await asyncio.wait_for(self.session.interrupt(), 1)

    def receipt(self, event):
        return self.output.receipt(event.get('block_id'), event.get('epoch'), event.get('played_samples'), valid=event.get('valid', True), finished=event.get('finished', True))

    async def close(self):
        if self.closed:
            return
        self.closed = True
        if self.counting:
            await self.counting.close()
        if self.speech_timer:
            self.speech_timer.cancel()
            await asyncio.gather(self.speech_timer, return_exceptions=True)
        self.input.close()
        await self.output.aclose()
        try:
            await asyncio.wait_for(self.session.aclose(), 3)
        finally:
            for job in tuple(self.jobs):
                job.cancel()
            await asyncio.gather(*self.jobs, return_exceptions=True)
            if self.automatic:
                await self.recognizer.aclose()
            for provider in self.providers[:3]:
                await provider.aclose()
