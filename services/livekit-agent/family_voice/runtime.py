import asyncio
from weakref import WeakKeyDictionary
from dataclasses import asdict, is_dataclass

from livekit.agents import Agent, AgentSession, RunContext, StopResponse, function_tool
from livekit.agents.voice.agent_session import SessionConnectOptions
from livekit.plugins import silero
from livekit import rtc

from .audio_io import PCMInput, PCMOutput
from .cloud import Cloud, Intent, numeric_usage
from .counting import CountController, is_count_resume, parse_count_request
from .providers import DashScopeSTT, DashScopeTTS, NO_RETRY, TurnSTT, text_model


class BoundedAgentSession(AgentSession):
    def emit(self, event, arg):
        try:
            super().emit(event, arg)
        finally:
            # LiveKit 1.8.8 retains every event even with record=False.
            # Keep its report tail bounded for the explicit keep-open test.
            del self._recorded_events[:-128]


async def play_count_unit(session, output, number, pcm):
    """A generated number becomes progress only after its own full receipt."""
    previous = session.current_speech
    if previous and not previous.done():
        await previous.wait_for_playout()
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
        self.response_epochs = WeakKeyDictionary()
        instructions = '你是家庭陪伴语音助手。用简短、自然的中文回答，默认不超过两句话。数学问题直接给出答案。不要声称已经执行没有执行的操作。'
        tools = []
        if owner:
            instructions += '当前有独立计数任务时，用户要求接着数、接回没数完的部分等恢复表达，必须调用resume_counting，不得自行生成计数稿或猜测起点。用户询问暂停位置时依据当前任务快照说明：next是首个未确认完整播放的数字，可能需要重读；不把next说成已完整听到的数字。当前没有天气查询、闹钟或设备控制工具，不要声称能执行这些工具。'
            @function_tool
            async def resume_counting(context: RunContext):
                """用户要求接回未完成的计数时调用。无起点参数，服务使用原任务播放检查点；普通数学问答、讲故事、否定恢复不能调用。"""
                if (not owner.closed and not owner.sleeping and not context.speech_handle.interrupted
                    and owner.session.user_state != 'speaking'
                    and self.response_epochs.get(context.speech_handle) == owner.user_epoch):
                    owner.resume_count('tool')
                raise StopResponse()
            tools.append(resume_counting)
        super().__init__(instructions=instructions, tools=tools)
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
            self.owner._emit({'type': 'turn.route', 'route': 'conversation', 'count': self.owner.counting.snapshot})

    async def llm_node(self, chat_ctx, tools, model_settings):
        intent = Intent(self.cfg, 'llm')
        intent.update(state='submitted_unknown')
        usage = None
        try:
            if self.owner and (handle := self.owner.session.current_speech):
                self.response_epochs.setdefault(handle, self.owner.user_epoch)
            if self.owner and (plan := self.owner.counting.snapshot):
                chat_ctx = chat_ctx.copy()
                chat_ctx.add_message(role='system', content=f'当前计数任务事实（仅本轮，只读）：start={plan["start"]}, end={plan["end"]}, next={plan["next"]}, state={plan["state"]}。数字小于next的已确认完整播放；next尚未确认播完。原范围不可擅自延长；恢复必须调用resume_counting，不能自己输出数字串。')
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
        self.user_epoch = 0
        self.input = PCMInput()
        self.output = self.make_output(send)
        vad = silero.VAD.load(max_buffered_speech=30, force_cpu=True, **({'min_speech_duration': .12, 'min_silence_duration': .65, 'prefix_padding_duration': .3, 'activation_threshold': .6} if automatic else {}))
        self.providers = [DashScopeSTT(cfg), text_model(cfg, max_tokens=512 if automatic else 128), DashScopeTTS(cfg), vad]
        self.recognizer = TurnSTT(self.providers[0], vad) if automatic else self.providers[0]
        turn_handling = {'turn_detection': 'manual', 'interruption': {'resume_false_interruption': False}, 'preemptive_generation': {'enabled': False}}
        if automatic:
            turn_handling.update(turn_detection='vad', endpointing={'min_delay': .35, 'max_delay': 1}, interruption={'mode': 'vad', 'enabled': True, 'min_duration': .2, 'min_words': 0, 'resume_false_interruption': False})
        self.session = self.make_session(
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
        self.counting = self.make_counting() if automatic else None

    def make_session(self, **options):
        return AgentSession(**options)

    def make_output(self, send):
        return PCMOutput(send)

    def make_counting(self):
        return CountController(self.prepare_count, self.play_count, self._emit)

    def make_agent(self):
        return AuditedAgent(self.cfg, self if self.automatic else None)

    def on_speech_started(self):
        self.user_epoch += 1
        if self.counting:
            self.counting.pause()

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
            self._emit({'type': 'turn.route', 'route': 'count.start', 'count': self.counting.snapshot})
            return True
        plan = self.counting.snapshot
        if plan and is_count_resume(text):
            self.resume_count('direct')
            return True
        return False

    def resume_count(self, source):
        plan = self.counting.snapshot
        if self.closed or self.sleeping:
            return
        self._emit({'type': 'turn.route', 'route': 'count.resume', 'source': source, 'count': plan})
        if not plan:
            self.session.say('当前没有保留的计数任务，请指定计数范围。')
        elif plan['state'] == 'completed':
            self.session.say(f'已经数到{plan["end"]}，这次数数已完成。')
        elif plan['state'] == 'failed':
            self.session.say('上次数数的语音准备失败，请重新指定计数范围。')
        else:
            self.counting.resume()

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
                    self.on_speech_started()
                    async def limit():
                        await asyncio.sleep(self.automatic_utterance_seconds)
                        self.on_utterance_limit()
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
        await self.session.start(self.make_agent(), record=False)

    def on_utterance_limit(self):
        self.sleep('utterance_limit')

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
