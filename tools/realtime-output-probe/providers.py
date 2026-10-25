# LEGACY_VALIDATION_REFERENCE (2026-10-25): Python baseline/probe, not Go runtime.
# Some modules remain active Go differential-test or legacy rollback dependencies.
# Keep reference behavior stable; see docs/CODE_LIFECYCLE.md.
"""Isolated, bounded paid calls. Machine fixtures only. No automatic retries."""
import asyncio
import base64
import json
import os
import shlex
import time
import uuid
import wave
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit

import aiohttp


class ProbeError(Exception):
    pass


def private_json(path, value):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), 'w') as file:
        json.dump(value, file, ensure_ascii=False)


def settings():
    values = {}
    for line in Path('/etc/family-robot/realtime-test.env').read_text().splitlines():
        key, sep, value = line.partition('=')
        if sep and key in ('DASHSCOPE_API_KEY', 'QWEN_REALTIME_URL', 'QWEN_VOICE'):
            parsed = shlex.split(value)
            values[key] = parsed[0] if parsed else ''
    u = urlsplit(values['QWEN_REALTIME_URL'])
    if u.scheme != 'wss' or not u.hostname or not values['DASHSCOPE_API_KEY']:
        raise ProbeError('protected_config_invalid')
    values['asr_url'] = urlunsplit((u.scheme, u.netloc, '/api-ws/v1/inference', '', ''))
    return values


def wav_pcm(path):
    with wave.open(str(path), 'rb') as file:
        if (file.getnchannels(), file.getsampwidth(), file.getframerate()) != (1, 2, 16000):
            raise ProbeError('synthetic_wav_format')
        if not 0 < file.getnframes() <= 16000 * 30:
            raise ProbeError('synthetic_wav_size')
        return file.readframes(file.getnframes())


class Intent:
    def __init__(self, directory, stage, *, synthetic_only=True):
        self.path = directory / (stage + '-' + uuid.uuid4().hex + '.json')
        self.data = {'stage': stage, 'state': 'intent_saved', 'synthetic_only': synthetic_only}
        self.started = time.monotonic()
        self.update()

    def update(self, **fields):
        self.data.update(fields, elapsed_ms=round((time.monotonic() - self.started) * 1000))
        private_json(self.path, self.data)


class Native:
    def __init__(self, cfg, directory):
        self.cfg, self.directory = cfg, directory
        self.client = self.ws = None

    async def open(self, context):
        if 'enable_search' in self.cfg and type(self.cfg['enable_search']) is not bool:
            raise ProbeError('invalid_search_setting')
        if self.cfg.get('enable_search') is True and self.cfg.get('tools'):
            raise ProbeError('search_tools_incompatible')
        self.intent = Intent(self.directory, 'native_session', synthetic_only=self.cfg.get('synthetic_only', True))
        self.client = aiohttp.ClientSession(timeout=aiohttp.ClientTimeout(total=70))
        try:
            async with asyncio.timeout(12):
                self.ws = await self.client.ws_connect(self.cfg['QWEN_REALTIME_URL'],
                    headers={'Authorization': 'Bearer ' + self.cfg['DASHSCOPE_API_KEY']}, max_msg_size=2**21)
                await self.expect('session.created')
                session = {
                    'modalities': ['text', 'audio'], 'instructions': context,
                    'turn_detection': None, 'input_audio_transcription': None,
                    'audio': {'input': {'format': {'type': 'pcm', 'sample_rate': 16000,
                         'sample_format': 's16le', 'channels': 1, 'packing': 'interleaved', 'channel_layout': 'mono'}},
                         'output': {'voice': self.cfg.get('QWEN_VOICE', 'Tina'),
                                    'format': {'type': 'pcm', 'sample_rate': 24000}}}}
                if 'enable_search' in self.cfg:
                    session['enable_search'] = self.cfg['enable_search']
                    if self.cfg['enable_search']:
                        session['search_options'] = {'enable_source': True}
                await self.ws.send_json({'type': 'session.update', 'session': session})
                await self.expect('session.updated')
                self.intent.update(state='connected')
            return self
        except BaseException:
            self.intent.update(state='connection_outcome_unknown')
            await self.close()
            raise

    async def event(self):
        e = await self.ws.receive_json(timeout=35)
        if e.get('type') == 'error':
            raise ProbeError('native_provider_error')
        return e

    async def expect(self, kind):
        for _ in range(64):
            e = await self.event()
            if e.get('type') == kind:
                return e
        raise ProbeError('native_expected_event_missing')

    async def ask(self, pcm):
        audit = Intent(self.directory, 'native_turn')
        audit.update(state='submission_outcome_unknown', input_samples=len(pcm) // 2)
        audio, transcript, identity, first_ms = bytearray(), '', None, None
        try:
            async with asyncio.timeout(55):
                for offset in range(0, len(pcm), 640):
                    await self.ws.send_json({'type': 'input_audio_buffer.append',
                        'audio': base64.b64encode(pcm[offset:offset + 640]).decode()})
                await self.ws.send_json({'type': 'input_audio_buffer.commit'})
                await self.expect('input_audio_buffer.committed')
                await self.ws.send_json({'type': 'response.create'})
                for _ in range(4096):
                    e = await self.event()
                    kind = e.get('type')
                    if kind == 'response.created':
                        if identity:
                            raise ProbeError('native_overlapping_response')
                        identity = e.get('response', {}).get('id')
                    elif kind == 'response.audio.delta':
                        if not identity or e.get('response_id') != identity:
                            raise ProbeError('native_response_mismatch')
                        frame = base64.b64decode(e['delta'], validate=True)
                        if not frame or len(frame) % 2 or len(audio) + len(frame) > 24000 * 2 * 60:
                            raise ProbeError('native_audio_limit')
                        if first_ms is None:
                            first_ms = round((time.monotonic() - audit.started) * 1000)
                        audio.extend(frame)
                    elif kind == 'response.audio_transcript.done' and e.get('response_id') == identity:
                        transcript = e.get('transcript', '')
                        if len(transcript) > 4096:
                            raise ProbeError('native_transcript_limit')
                    elif kind == 'response.done':
                        response = e.get('response', {})
                        if response.get('id') != identity or response.get('status') != 'completed' or not audio or not transcript:
                            raise ProbeError('native_terminal_invalid')
                        usage = {k: v for k, v in response.get('usage', {}).items() if type(v) in (int, float)}
                        audit.update(state='completed', first_pcm_ms=first_ms, output_samples=len(audio) // 2, usage=usage)
                        return bytes(audio), transcript, {'first_pcm_ms': first_ms, 'usage': usage}
                raise ProbeError('native_event_limit')
        except BaseException:
            audit.update(state='outcome_unknown', received_samples=len(audio) // 2)
            raise

    async def close(self):
        if self.ws:
            try:
                await asyncio.wait_for(self.ws.close(), 1)
            except Exception:
                pass
        if self.client:
            await self.client.close()


async def recognize(cfg, directory, pcm, on_final=None):
    audit = Intent(directory, 'output_asr')
    task = uuid.uuid4().hex
    sentences, events, usage = {}, [], {}
    sender = None
    try:
        async with asyncio.timeout(45), aiohttp.ClientSession() as client:
            async with client.ws_connect(cfg['asr_url'], headers={'Authorization': 'Bearer ' + cfg['DASHSCOPE_API_KEY']},
                                         max_msg_size=2**21) as ws:
                audit.update(state='submission_outcome_unknown', input_samples=len(pcm) // 2)
                await ws.send_json({'header': {'action': 'run-task', 'task_id': task, 'streaming': 'duplex'},
                    'payload': {'task_group': 'audio', 'task': 'asr', 'function': 'recognition',
                        'model': 'fun-asr-realtime', 'parameters': {'format': 'pcm', 'sample_rate': 16000}, 'input': {}}})
                started = await ws.receive_json(timeout=12)
                if started.get('header', {}).get('event') != 'task-started' or started['header'].get('task_id') != task:
                    raise ProbeError('asr_start_failed')

                async def send():
                    for offset in range(0, len(pcm), 3200):
                        await ws.send_bytes(pcm[offset:offset + 3200])
                    await ws.send_json({'header': {'action': 'finish-task', 'task_id': task, 'streaming': 'duplex'},
                                        'payload': {'input': {}}})

                sender = asyncio.create_task(send())
                for _ in range(2048):
                    e = await ws.receive_json(timeout=30)
                    header = e.get('header', {})
                    if header.get('task_id') != task or header.get('event') == 'task-failed':
                        raise ProbeError('asr_provider_error')
                    kind = header.get('event')
                    elapsed = round((time.monotonic() - audit.started) * 1000)
                    if kind == 'result-generated':
                        reported = e.get('payload', {}).get('usage') or {}
                        if type(reported.get('duration')) in (int, float):
                            usage['duration'] = reported['duration']
                        value = e.get('payload', {}).get('output', {}).get('sentence', {})
                        if value.get('heartbeat') or not value.get('text'):
                            continue
                        events.append({'elapsed_ms': elapsed, 'final': value.get('sentence_end') is True,
                                       'word_count': len(value.get('words', []))})
                        if value.get('sentence_end') is True:
                            sentences[value['sentence_id']] = value
                            if on_final:
                                on_final(value, elapsed)
                    elif kind == 'task-finished':
                        await sender
                        audit.update(state='completed', total_ms=elapsed, stable_sentences=len(sentences), usage=usage)
                        return {'sentences': list(sentences.values()), 'events': events, 'total_ms': elapsed, 'usage': usage}
                raise ProbeError('asr_event_limit')
    except BaseException:
        audit.update(state='outcome_unknown')
        raise
    finally:
        if sender:
            if not sender.done():
                sender.cancel()
            await asyncio.gather(sender, return_exceptions=True)
