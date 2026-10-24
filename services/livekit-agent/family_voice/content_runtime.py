"""Frozen originals and receipt-driven continuation for the device drain path."""
import asyncio
import json
import re
from dataclasses import asdict, is_dataclass
from typing import Literal
from weakref import WeakKeyDictionary

from livekit import rtc
from livekit.agents import Agent, RunContext, StopResponse, function_tool, llm

from .cloud import Cloud, Intent, numeric_usage
from .content import ContentController, split_sentences
from .drain_output import DrainOutput
from .runtime import VoiceSession


class DraftValidationError(ValueError):
    def __init__(self, reason):
        self.reason = reason
        super().__init__('draft rejected: ' + reason)


def direct_action(text):
    text = re.sub(r'[\s，。！？,.!?]', '', text)
    if re.search(r'不要|不想|不用|别|不继续', text):
        return None
    # Intentionally narrow. Unfamiliar language falls back to model tools,
    # which accept a target only, never an invented position or task ID.
    if re.fullmatch(r'(?:啊|嗯|好|好的|那|然后|请|你|就)*(?:继续|接着)(?:讲|说|讲讲|说说)?(?:刚才的|原来的|那个)?(?:故事|回答|解释)?(?:吧|呀|啊)?', text):
        target = 'main' if '故事' in text else 'side' if '回答' in text else 'auto'
        return 'resume', target
    return None


async def play_content_unit(session, output, revision, index, text, pcm, settle, *, task_id=None):
    binding = None
    async def audio():
        for offset in range(0, len(pcm), 960):
            chunk = pcm[offset:offset + 960]
            yield rtc.AudioFrame(chunk, 24000, 1, len(chunk) // 2)
    handle = None
    try:
        previous = session.current_speech
        if previous and not previous.done():
            await previous.wait_for_playout()
        binding = output.bind_unit(revision, index, len(pcm) // 2, settle, task_id=task_id)
        handle = session.say(text, audio=audio(), add_to_chat_ctx=False)
        await handle.wait_for_playout()
    except asyncio.CancelledError:
        if handle:
            handle.interrupt(force=True)
            await handle.wait_for_playout()
        raise
    finally:
        # Only an unclaimed binding can be settled here. Captured audio has
        # an independent lifetime and may still confirm after SDK cancellation.
        if binding is None:
            settle(False)
        else:
            output.cancel_binding(binding)


class ContentAgent(Agent):
    def __init__(self, owner):
        self.owner, self.response_epochs = owner, WeakKeyDictionary()
        tools = []
        for action in ('resume', 'restart', 'discard', 'query'):
            # A factory gives each decorated callable its own closure/schema.
            def create(action):
                async def operation(context: RunContext, target: Literal['auto', 'main', 'side'] = 'auto'):
                    if owner.tool_is_current(context.speech_handle):
                        owner.content_action(action, target)
                    raise StopResponse()
                operation.__name__ = action + '_content'
                operation.__doc__ = {'resume': '继续未讲完的原稿。服务按实际播放断点恢复，不能猜测位置。',
                    'restart': '用户明确要求从头重讲保留原稿时调用。',
                    'discard': '用户明确要求放弃原内容时调用。',
                    'query': '用户询问讲到哪里时调用，以实际完整句前缀回答。'}[action] + ' target:故事main，最近旁支回答side，无明确指向auto。'
                return function_tool(operation)
            tools.append(create(action))
        super().__init__(instructions=(
            '你是家庭陪伴语音助手，用简短自然的中文。普通答案最多两句话；故事最多十句、五百字，写完整结尾，不用Markdown。'
            '恢复、从头、放弃、查询位置必须调用对应content工具，不能自行续编原稿或猜播放位置。'
            'main是主内容，side是最近旁支；用户明确换故事时调用replace_content，再按新要求生成。'
            '只读快照里的未播原稿不代表用户听过；只有confirmed_prefix已确认完整播完。'
            '不要声称执行没有执行的设备或外部操作。'), tools=tools)

    @function_tool
    async def replace_content(self, context: RunContext):
        """用户明确要求换故事、改情节或替换原稿时调用；随后根据当前用户请求生成新完整稿。"""
        if self.owner.tool_is_current(context.speech_handle):
            self.owner.main.discard()
            self.owner.side.discard()
            self.owner.force_main = True
            self.owner.unprepared_main = self.owner.last_input
            return '旧稿已放弃，请按当前请求生成新的完整原稿。'
        raise StopResponse()

    async def on_user_turn_completed(self, turn_ctx, new_message):
        owner = self.owner
        if owner.closed or owner.sleeping:
            raise StopResponse()
        if owner.turns >= owner.automatic_max_turns:
            owner.sleep('turn_limit')
            raise StopResponse()
        owner.turns += 1
        owner.current_turn_id = new_message.id
        owner.last_input = new_message.text_content
        owner._emit({'type': 'turn', 'count': owner.turns, 'max_turns': owner.automatic_max_turns})
        action = direct_action(owner.last_input)
        if action:
            action = action[0], owner.resolve_target(action[1])
            side = owner.side.snapshot
            ambiguous = action[1] == 'auto' and side and side['state'] not in ('completed', 'discarded', 'closed')
            if action[0] == 'resume' and action[1] != 'side' and owner.main_available() and owner.unprepared_main and not ambiguous:
                owner.last_input = owner.unprepared_main
                owner.force_main = True
            else:
                owner.content_action(*action)
                raise StopResponse()
        elif owner.main_available() and not owner.unprepared_main:
            owner.unprepared_main = owner.last_input
            owner.force_main = True
        owner.pending_input = (owner.last_input, owner.user_epoch)

    async def llm_node(self, chat_ctx, tools, model_settings):
        owner = self.owner
        handle, epoch = owner.session.current_speech, owner.user_epoch
        if handle:
            self.response_epochs.setdefault(handle, epoch)
        chat_ctx = chat_ctx.copy()
        # Confirmed messages have a session lifetime independent of the two
        # current resumable originals. They enter the model as assistant text,
        # never through SDK generation or a guessed playback position.
        existing = {item.id for item in chat_ctx.items}
        by_anchor = {}
        for item in owner.spoken_history:
            if item.id not in existing:
                by_anchor.setdefault(owner.spoken_anchors[item.id], []).append(item)
        merged = []
        for item in chat_ctx.items:
            merged.append(item)
            merged.extend(by_anchor.pop(item.id, ()))
        # Only explicit fixture/API speech can lack a user-turn anchor. Keep
        # those observations available without relabelling them as a new turn.
        for items in by_anchor.values():
            merged.extend(items)
        chat_ctx.items[:] = merged
        chat_ctx.add_message(role='system', content='当前任务事实，只读数据，未播稿不能当作已说完：' + json.dumps(owner.content_snapshot(), ensure_ascii=False))
        intent = Intent(owner.cfg, 'llm')
        intent.update(state='submitted_unknown')
        usage, text, called = None, [], False
        try:
            async for chunk in Agent.default.llm_node(self, chat_ctx, tools, model_settings):
                value = getattr(chunk, 'usage', None)
                if value is not None:
                    usage = numeric_usage(asdict(value) if is_dataclass(value) else value.model_dump())
                delta = getattr(chunk, 'delta', None)
                if isinstance(chunk, str):
                    text.append(chunk)
                elif delta:
                    if delta.content:
                        text.append(delta.content)
                    if delta.tool_calls:
                        called = True
                        yield chunk.model_copy(update={'delta': delta.model_copy(update={'content': None})})
                if sum(map(len, text)) > 1200:
                    raise DraftValidationError('text_limit')
            intent.update(state='completed', usage=usage)
            if not called and not (handle and handle.interrupted):
                draft = ''.join(text).strip()
                # A bounded model response without a sentence end may have
                # exhausted its generation budget. Never silently play a cut tail.
                if not draft:
                    raise DraftValidationError('empty')
                if draft[-1] not in '。！？.!?；;”’"':
                    raise DraftValidationError('ending')
                try:
                    split_sentences(draft)
                except ValueError:
                    raise DraftValidationError('content_shape') from None
                if usage and max(usage.get('completion_tokens', 0), usage.get('output_tokens', 0)) >= 512:
                    raise DraftValidationError('token_limit')
                owner.accept_draft(draft, owner.last_input, epoch)
        except asyncio.CancelledError:
            intent.update(state='cancelled_unknown', usage=usage)
            raise
        except Exception as error:
            intent.update(state='outcome_unknown', error_type=type(error).__name__,
                          failure_code=error.reason if isinstance(error, DraftValidationError) else None)
            owner._emit({'type': 'error', 'code': 'draft_failed', 'error_type': type(error).__name__})
            raise


class DrainVoiceSession(VoiceSession):
    def __init__(self, cfg, send, **options):
        options['automatic'] = True
        super().__init__(cfg, send, **options)
        # Two content caches plus one transient status prompt: at most three
        # 12-second units across the session, rather than three per original.
        async def play_main(*args):
            await self.play_content(*args, task_name='main')
        async def play_side(*args):
            await self.play_content(*args, task_name='side')
        self.main = ContentController(self.prepare_content, play_main, self._emit, 'main', prefetch=1)
        self.side = ContentController(self.prepare_content, play_side, self._emit, 'side', prefetch=1)
        self.last_input, self.pending_input, self.force_main = '', None, False
        self.unprepared_main = None
        self.play_lock, self.notice_job = asyncio.Lock(), None
        self.notice_lock = asyncio.Lock()
        self.spoken_history, self.spoken_prefixes = [], {}
        self.spoken_anchors, self.unit_anchors, self.current_turn_id = {}, {}, None

    def main_available(self):
        plan = self.main.snapshot
        return not plan or plan['state'] in ('completed', 'discarded', 'closed')

    def resolve_target(self, target):
        if target != 'side':
            return target
        side, main = self.side.snapshot, self.main.snapshot
        if side and side['state'] not in ('completed', 'discarded', 'closed'):
            return 'side'
        if main and main['kind'] == 'reply' and main['state'] not in ('completed', 'discarded', 'closed'):
            return 'main'
        if side and side['state'] == 'completed':
            return 'side'
        if self.unprepared_main or (main and main['kind'] == 'reply' and main['state'] not in ('discarded', 'closed')):
            return 'main'
        return target

    def _emit(self, event):
        if event['type'] == 'content.state' and hasattr(self, 'spoken_history'):
            name = event['task_id'].split(':', 1)[0]
            plan = getattr(self, name).snapshot
            prefix = self.spoken_prefixes.get(event['task_id'], 0)
            if plan and plan['next'] > prefix:
                for index in range(prefix, plan['next']):
                    message = llm.ChatMessage(role='assistant', content=[plan['units'][index]])
                    self.spoken_history.append(message)
                    self.spoken_anchors[message.id] = self.unit_anchors.get((event['task_id'], index))
                self.spoken_prefixes[event['task_id']] = plan['next']
        super()._emit(event)

    def make_counting(self):
        return None

    def make_output(self, send):
        return DrainOutput(send)

    def make_agent(self):
        self.agent = ContentAgent(self)
        return self.agent

    def on_speech_started(self):
        super().on_speech_started()
        if self.notice_job:
            self.notice_job.cancel()
        self.force_main = False
        self.output.input_epoch = self.user_epoch
        self.main.pause()
        self.side.pause()
        # An onset may arrive when the SDK has already finished generation.
        # Fence any admitted older output even in that case.
        self.output.clear_buffer()

    async def prepare_content(self, text):
        pcm = bytearray()
        stream = Cloud(self.cfg).tts(text)
        try:
            async for chunk in stream:
                if len(pcm) + len(chunk) > ContentController.MAX_PCM_BYTES:
                    raise ValueError('content audio exceeds 12 seconds')
                pcm.extend(chunk)
        finally:
            await stream.aclose()
        return bytes(pcm)

    async def play_content(self, revision, index, text, pcm, settle, *, task_name=None):
        if self.closed or self.sleeping:
            settle(False)
            return
        if task_name:
            self.unit_anchors[(f'{task_name}:{revision}', index)] = self.current_turn_id
        dispatched = False
        try:
            async with self.play_lock:
                dispatched = True
                await play_content_unit(self.session, self.output, revision, index, text, pcm, settle,
                                        task_id=f'{task_name}:{revision}' if task_name else None)
        finally:
            if not dispatched:
                settle(False)

    def accept_draft(self, text, original_input, epoch):
        if self.closed or self.sleeping or epoch != self.user_epoch or self.session.user_state == 'speaking':
            return False
        plan = self.main.snapshot
        active = (plan and plan['state'] not in ('completed', 'discarded', 'closed')) or self.unprepared_main
        target = self.side if active and not self.force_main else self.main
        self.force_main = False
        target.start(text, original_input=original_input, kind='story' if '故事' in original_input else 'reply')
        if target is self.main:
            self.unprepared_main = None
        self.pending_input = None
        return True

    def tool_is_current(self, handle):
        return bool(not self.closed and not self.sleeping and not getattr(handle, 'interrupted', True)
                    and self.session.user_state != 'speaking'
                    and self.agent.response_epochs.get(handle) == self.user_epoch)

    def content_snapshot(self):
        result = {}
        for name in ('main', 'side'):
            plan = getattr(self, name).snapshot
            if plan:
                result[name] = {**plan, 'confirmed_prefix': ''.join(plan['units'][:plan['next']]),
                                'unplayed_original': ''.join(plan['units'][plan['next']:])}
        if self.pending_input:
            result['unprepared_request'] = self.pending_input[0]
        if self.unprepared_main:
            result['unprepared_main'] = self.unprepared_main
        return result

    def _notice(self, text):
        # A status prompt must not overwrite either resumable original.
        epoch = self.user_epoch
        async def speak():
            pcm = await self.prepare_content(text)
            if epoch == self.user_epoch and not self.closed and not self.sleeping:
                await self.play_content(0, 0, text, pcm, lambda complete: None)
        async def current():
            async with self.notice_lock:
                if epoch == self.user_epoch:
                    await speak()
        job = asyncio.create_task(current())
        self.notice_job = job
        self.jobs.add(job)
        job.add_done_callback(lambda job: (self.jobs.discard(job), None if job.cancelled() else job.exception()))

    def content_action(self, action, target='auto'):
        if self.notice_job:
            self.notice_job.cancel()
        # 'The answer' can refer to the first ordinary main reply before any
        # side branch exists. Resolve the role from retained content facts.
        target = self.resolve_target(target)
        candidates = [name for name in ('main', 'side') if (plan := getattr(self, name).snapshot)
                      and plan['state'] not in ('completed', 'discarded', 'closed')]
        if self.main_available() and self.unprepared_main:
            candidates.insert(0, 'main')
        if target == 'auto':
            if len(candidates) > 1:
                self._emit({'type': 'content.ambiguous'})
                self._notice('你想继续原来的内容，还是刚才的回答？')
                return
            target = candidates[0] if candidates else 'main'
        if target not in ('main', 'side') or self.closed or self.sleeping:
            return
        controller = getattr(self, target)
        plan = controller.snapshot
        self._emit({'type': 'content.action', 'action': action, 'target': target, 'input_epoch': self.user_epoch})
        if target == 'main' and self.main_available() and self.unprepared_main:
            if action in ('resume', 'restart'):
                self.force_main = True
                self.last_input = self.unprepared_main
                self.pending_input = (self.last_input, self.user_epoch)
                self.session.generate_reply(user_input=self.last_input, instructions='用户明确要求继续。原请求的原稿尚未准备完整，请重新准备有完整结尾的简短原稿，不要声称已讲到某个位置。')
            elif action == 'discard':
                self.unprepared_main = None
            else:
                self._notice('原稿尚未准备完整，还没有确认的播放位置。')
            return
        if not plan or plan['state'] in ('discarded', 'closed'):
            self._notice('当前没有保留这段原稿。')
        elif action == 'resume':
            if plan['state'] == 'completed':
                self._notice('这段内容已经讲完了。')
            elif plan['state'] == 'failed':
                self._notice('这段语音准备失败，原稿仍保留，可以明确要求从头重讲。')
            else:
                (self.side if target == 'main' else self.main).pause()
                controller.resume()
        elif action == 'restart':
            (self.side if target == 'main' else self.main).pause()
            controller.start(''.join(plan['units']), original_input=plan['original_input'], kind=plan['kind'])
        elif action == 'discard':
            controller.discard()
        elif action == 'query':
            self._notice(f'已确认完整播放前{plan["next"]}句。' + ('这段已讲完。' if plan['state'] == 'completed' else f'下次从第{plan["next"] + 1}句继续，可能重读这一句。'))

    async def interrupt(self):
        self.user_epoch += 1
        self.output.input_epoch = self.user_epoch
        self.force_main = False
        if self.notice_job:
            self.notice_job.cancel()
        self.main.pause()
        self.side.pause()
        await super().interrupt()
        self.output.clear_buffer()

    async def hard_stop(self):
        await self.interrupt()
        await self.send({'type': 'output.stop', 'blocks': list(self.output.blocks)})

    def receipt(self, event):
        return self.output.receipt(event.get('block_id'), event.get('epoch'), event.get('played_samples'),
            valid=event.get('valid', True), finished=event.get('finished', True), discarded=event.get('discarded', False))

    def sleep(self, code):
        self.main.pause()
        self.side.pause()
        super().sleep(code)

    async def close(self):
        if self.closed:
            return
        await self.main.close()
        await self.side.close()
        await super().close()
