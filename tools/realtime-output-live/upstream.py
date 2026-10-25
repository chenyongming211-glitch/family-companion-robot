# DEPRECATED_PRODUCT_PATH (2026-10-25): replaced in the default voice path by Go.
# Go implementation: internal/realtimelive/native.go and asr.go
# Retained only for temporary Python rollback and differential-test reference.
# Do not add product features here; see docs/CODE_LIFECYCLE.md.
"""Native voice input/output plus a separate output-only streaming ASR."""
import asyncio
import base64
import json
import os
import time
import uuid
from urllib.parse import urlsplit

import aiohttp

from state import Turn  # establishes the existing isolated provider module path
from providers import Native, Intent, ProbeError, settings as provider_settings


def settings():
    cfg = provider_settings()
    search = os.environ.get('OUTPUT_LIVE_ENABLE_SEARCH', '1')
    if search not in ('0', '1'):
        raise ValueError('invalid_search_setting')
    cfg['enable_search'] = search == '1'
    return cfg


class StreamingNative(Native):
    def __init__(self, cfg, directory):
        super().__init__(cfg, directory)
        self.search_config = None
        self.on_search = None

    async def expect(self, kind):
        event = await super().expect(kind)
        if kind == 'session.updated' and 'enable_search' in self.cfg:
            session = event.get('session', {})
            if 'enable_search' not in session and self.search_config is not None:
                return event
            enabled = session.get('enable_search')
            if enabled is not self.cfg['enable_search']:
                raise ProbeError('search_setting_not_confirmed')
            self.search_config = {'enabled': enabled,
                'source_requested': session.get('search_options', {}).get('enable_source') is True}
            if self.on_search:
                self.on_search({'phase': 'configured', **self.search_config})
        return event

    async def listen(self):
        await self.open('仅检测语音轮次，不自动生成回复。')
        await self.ws.send_json({'type': 'session.update', 'session': {
            'input_audio_transcription': {'model': 'qwen3-asr-flash-realtime'},
            'turn_detection': {'type': 'server_vad', 'threshold': .5,
                'silence_duration_ms': 600, 'create_response': False, 'interrupt_response': False}}})
        await self.expect('session.updated')
        return self

    async def stream(self, pcm):
        audit = Intent(self.directory, 'live_native_turn', synthetic_only=self.cfg.get('synthetic_only', False))
        audit.update(state='submission_outcome_unknown', input_samples=len(pcm) // 2)
        identity, size, transcript, completed = None, 0, '', False
        try:
            async with asyncio.timeout(75):
                for offset in range(0, len(pcm), 640):
                    await self.ws.send_json({'type': 'input_audio_buffer.append',
                        'audio': base64.b64encode(pcm[offset:offset + 640]).decode()})
                await self.ws.send_json({'type': 'input_audio_buffer.commit'})
                await self.expect('input_audio_buffer.committed')
                await self.ws.send_json({'type': 'response.create'})
                for _ in range(8192):
                    e = await self.event()
                    kind = e.get('type')
                    if kind == 'response.created':
                        if identity:
                            raise ProbeError('overlapping_response')
                        identity = e.get('response', {}).get('id')
                    elif kind == 'response.audio.delta':
                        if not identity or e.get('response_id') != identity:
                            raise ProbeError('response_identity')
                        frame = base64.b64decode(e['delta'], validate=True)
                        size += len(frame)
                        if len(frame) % 2 or size > 24000 * 2 * 60:
                            raise ProbeError('output_limit')
                        if size == len(frame):
                            audit.update(first_pcm_ms=round((time.monotonic() - audit.started) * 1000))
                        yield 'pcm', frame
                    elif kind == 'response.audio_transcript.delta' and e.get('response_id') == identity:
                        transcript += e.get('delta', '')
                        if len(transcript) > 4096:
                            raise ProbeError('transcript_limit')
                        yield 'text', transcript
                    elif kind == 'response.audio_transcript.done' and e.get('response_id') == identity:
                        transcript = e.get('transcript', '')
                        if len(transcript) > 4096:
                            raise ProbeError('transcript_limit')
                        yield 'text', transcript
                    elif kind == 'response.done':
                        response = e.get('response', {})
                        if response.get('id') != identity or response.get('status') != 'completed' or not size:
                            raise ProbeError('response_terminal')
                        completed = True
                        usage = response.get('usage')
                        usage = usage if isinstance(usage, dict) else {}
                        plugins = usage.get('plugins')
                        plugins = plugins if isinstance(plugins, dict) else {}
                        search = plugins.get('search')
                        search = search if isinstance(search, dict) else {}
                        info = response.get('search_info')
                        info = info if isinstance(info, dict) else {}
                        results = info.get('search_results')
                        results = results if isinstance(results, list) else []
                        sources = []
                        for source in results[:32]:
                            if not isinstance(source, dict):
                                continue
                            title, url = source.get('title'), source.get('url')
                            if not isinstance(title, str) or not isinstance(url, str) or len(url) > 4096:
                                continue
                            try:
                                parsed = urlsplit(url)
                            except ValueError:
                                continue
                            if parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username or parsed.password:
                                continue
                            item = {'title': title[:512], 'url': url}
                            if type(source.get('index')) is int:
                                item['index'] = source['index']
                            sources.append(item)
                        search_evidence = {}
                        if type(search.get('count')) is int and search['count'] >= 0:
                            search_evidence['count'] = search['count']
                        if isinstance(search.get('strategy'), str):
                            search_evidence['strategy'] = search['strategy'][:64]
                        if sources:
                            search_evidence['sources'] = sources
                        if self.on_search and search_evidence:
                            self.on_search({'phase': 'completed', 'response_id': identity,
                                **search_evidence})
                        audit.update(state='completed', output_samples=size // 2,
                            usage={k: v for k, v in usage.items() if type(v) in (int, float)},
                            search=search_evidence)
                        return
                raise ProbeError('event_limit')
        finally:
            if not completed:
                audit.update(state='cancelled_or_outcome_unknown', received_samples=size // 2)
                if identity:
                    try:
                        await asyncio.wait_for(self.ws.send_json({'type': 'response.cancel'}), .5)
                    except Exception:
                        pass


class OutputASR:
    def __init__(self, cfg, directory, on_final):
        self.cfg, self.directory, self.on_final = cfg, directory, on_final
        self.queue = asyncio.Queue(maxsize=1024)
        self.done = asyncio.Event()
        self.started = time.monotonic()
        self.input_samples = 0
        self.failed = self.finished = False
        self.task = asyncio.create_task(self.run())

    def feed(self, pcm):
        if self.finished or self.failed:
            return
        try:
            self.queue.put_nowait(pcm)
            self.input_samples += len(pcm) // 2
        except asyncio.QueueFull:
            self.failed = True
            self.task.cancel()

    def finish(self):
        if not self.finished:
            self.finished = True
            if not self.task.done():
                try:
                    self.queue.put_nowait(None)
                except asyncio.QueueFull:
                    self.failed = True
                    self.task.cancel()

    async def run(self):
        audit = Intent(self.directory, 'live_output_asr', synthetic_only=self.cfg.get('synthetic_only', False))
        identity, sender = uuid.uuid4().hex, None
        samples = 0
        try:
            async with asyncio.timeout(100), aiohttp.ClientSession() as client:
                async with client.ws_connect(self.cfg['asr_url'],
                        headers={'Authorization': 'Bearer ' + self.cfg['DASHSCOPE_API_KEY']},
                        max_msg_size=2**21) as ws:
                    audit.update(state='submission_outcome_unknown')
                    await ws.send_json({'header': {'action': 'run-task', 'task_id': identity, 'streaming': 'duplex'},
                        'payload': {'task_group': 'audio', 'task': 'asr', 'function': 'recognition',
                            'model': 'fun-asr-realtime', 'parameters': {'format': 'pcm', 'sample_rate': 16000}, 'input': {}}})
                    e = await ws.receive_json(timeout=12)
                    if e.get('header', {}).get('event') != 'task-started':
                        raise ProbeError('asr_start')

                    async def send():
                        nonlocal samples
                        while True:
                            frame = await self.queue.get()
                            if frame is None:
                                break
                            samples += len(frame) // 2
                            await ws.send_bytes(frame)
                        await ws.send_json({'header': {'action': 'finish-task', 'task_id': identity,
                            'streaming': 'duplex'}, 'payload': {'input': {}}})

                    sender = asyncio.create_task(send())
                    for _ in range(4096):
                        e = await ws.receive_json(timeout=35)
                        header = e.get('header', {})
                        if header.get('task_id') != identity or header.get('event') == 'task-failed':
                            raise ProbeError('asr_event')
                        if header.get('event') == 'result-generated':
                            value = e.get('payload', {}).get('output', {}).get('sentence', {})
                            if value.get('sentence_end') is True:
                                self.on_final(value, round((time.monotonic() - self.started) * 1000))
                        elif header.get('event') == 'task-finished':
                            await sender
                            audit.update(state='completed', input_samples=samples)
                            return
                    raise ProbeError('asr_event_limit')
        except BaseException:
            self.failed = True
            audit.update(state='failed_or_outcome_unknown', input_samples=samples)
        finally:
            if sender:
                sender.cancel()
                await asyncio.gather(sender, return_exceptions=True)
            self.done.set()
