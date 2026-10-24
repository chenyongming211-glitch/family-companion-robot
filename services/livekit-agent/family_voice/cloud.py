"""Bounded provider protocols. No automatic retries, transcripts or audio logs."""

import asyncio
import base64
import json
import os
import time
import uuid

import aiohttp


class CloudError(Exception):
    def __init__(self, stage, code):
        self.stage, self.code = stage, code
        super().__init__(f'{stage}: {code}')


class Intent:
    def __init__(self, cfg, stage):
        cfg.audit_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.path = cfg.audit_dir / (uuid.uuid4().hex + '.json')
        self.started = time.monotonic()
        self.data = {'stage': stage, 'state': 'intent_saved', 'created_unix': time.time(), 'usage': None}
        self.update()

    def update(self, **changes):
        self.data.update(changes)
        self.data['elapsed_ms'] = round((time.monotonic() - self.started) * 1000)
        temporary = self.path.with_suffix('.tmp')
        with os.fdopen(os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), 'w') as f:
            json.dump(self.data, f)
            f.flush()
            os.fsync(f.fileno())
        os.replace(temporary, self.path)


def numeric_usage(value):
    return {k: v for k, v in (value or {}).items() if k in ('characters', 'duration', 'input_tokens', 'output_tokens', 'total_tokens', 'prompt_tokens', 'completion_tokens') and type(v) in (int, float)}


class Cloud:
    def __init__(self, cfg, *, timeout=30):
        self.cfg, self.timeout = cfg, timeout
        self.last_usage = None

    async def _event(self, ws, stage):
        event = await ws.receive_json()
        if event.get('type') == 'error' or event.get('header', {}).get('event') == 'task-failed':
            raise CloudError(stage, 'provider_error')
        return event

    async def _expect(self, ws, name, stage):
        while True:
            event = await self._event(ws, stage)
            if event.get('type') == name or event.get('header', {}).get('event') == name:
                return event

    async def tts(self, text):
        if not text.strip() or len(text) > 512:
            raise ValueError('TTS text must contain 1..512 characters')
        intent = Intent(self.cfg, 'tts')
        received, response_id = 0, None
        try:
            async with asyncio.timeout(self.timeout), aiohttp.ClientSession() as client:
                async with client.ws_connect(self.cfg.tts_url, headers={'Authorization': 'Bearer ' + self.cfg.api_key}, max_msg_size=2**21) as ws:
                    await self._expect(ws, 'session.created', 'tts')
                    await ws.send_json({'event_id': uuid.uuid4().hex, 'type': 'session.update', 'session': {'voice': self.cfg.voice, 'mode': 'commit', 'language_type': 'Chinese', 'response_format': 'pcm', 'sample_rate': 24000}})
                    await self._expect(ws, 'session.updated', 'tts')
                    intent.update(state='submitted_unknown', input_characters=len(text))
                    await ws.send_json({'event_id': uuid.uuid4().hex, 'type': 'input_text_buffer.append', 'text': text})
                    await ws.send_json({'event_id': uuid.uuid4().hex, 'type': 'input_text_buffer.commit'})
                    while True:
                        event = await self._event(ws, 'tts')
                        if event.get('type') == 'response.audio.delta':
                            current = event.get('response_id')
                            if response_id is not None and current != response_id:
                                raise CloudError('tts', 'response_identity_mismatch')
                            response_id = current
                            try:
                                pcm = base64.b64decode(event['delta'], validate=True)
                            except (ValueError, KeyError):
                                raise CloudError('tts', 'invalid_audio') from None
                            if not pcm or len(pcm) % 2 or received + len(pcm) > 24000 * 2 * 120:
                                raise CloudError('tts', 'invalid_audio')
                            if not received:
                                intent.update(first_pcm_ms=round((time.monotonic() - intent.started) * 1000))
                            received += len(pcm)
                            yield pcm
                        elif event.get('type') == 'response.done':
                            response = event.get('response', {})
                            if not received or response.get('status') != 'completed' or (response_id and response.get('id') != response_id):
                                raise CloudError('tts', 'invalid_terminal')
                            self.last_usage = numeric_usage(response.get('usage'))
                            intent.update(state='completed', pcm_bytes=received, usage=self.last_usage)
                            return
        except asyncio.CancelledError:
            intent.update(state='cancelled_unknown', pcm_bytes=received)
            raise
        except Exception as error:
            intent.update(state='outcome_unknown', error_type=type(error).__name__, pcm_bytes=received)
            if isinstance(error, CloudError):
                raise
            raise CloudError('tts', type(error).__name__) from None

    async def asr(self, frames):
        intent = Intent(self.cfg, 'asr')
        task_id = uuid.uuid4().hex
        sender = reader = None
        try:
            async with asyncio.timeout(self.timeout), aiohttp.ClientSession() as client:
                async with client.ws_connect(self.cfg.asr_url, headers={'Authorization': 'Bearer ' + self.cfg.api_key}, max_msg_size=2**20, timeout=aiohttp.ClientWSTimeout(ws_close=.1)) as ws:
                    intent.update(state='submitted_unknown')
                    await ws.send_json({'header': {'action': 'run-task', 'task_id': task_id, 'streaming': 'duplex'}, 'payload': {'task_group': 'audio', 'task': 'asr', 'function': 'recognition', 'model': self.cfg.asr_model, 'parameters': {'format': 'pcm', 'sample_rate': 16000}, 'input': {}}})
                    started = await self._expect(ws, 'task-started', 'asr')
                    if started['header'].get('task_id') != task_id:
                        raise CloudError('asr', 'task_identity_mismatch')

                    async def send():
                        size = 0
                        async for pcm in frames:
                            if not pcm or len(pcm) % 2:
                                raise CloudError('asr', 'invalid_audio')
                            size += len(pcm)
                            if size > 16000 * 2 * 30:
                                raise CloudError('asr', 'input_limit')
                            await ws.send_bytes(pcm)
                        await ws.send_json({'header': {'action': 'finish-task', 'task_id': task_id, 'streaming': 'duplex'}, 'payload': {'input': {}}})
                        intent.update(input_samples=size // 2)

                    sender = asyncio.create_task(send())
                    while True:
                        reader = asyncio.create_task(self._event(ws, 'asr'))
                        if not sender.done():
                            done, _ = await asyncio.wait((reader, sender), return_when=asyncio.FIRST_COMPLETED)
                            if sender in done:
                                await sender
                        event = await reader
                        if event.get('header', {}).get('task_id') != task_id:
                            raise CloudError('asr', 'task_identity_mismatch')
                        kind = event.get('header', {}).get('event')
                        if kind == 'result-generated':
                            sentence = event.get('payload', {}).get('output', {}).get('sentence', {})
                            if sentence.get('heartbeat') or not sentence.get('text'):
                                continue
                            yield {'text': sentence['text'], 'final': bool(sentence.get('sentence_end'))}
                            usage = event.get('payload', {}).get('usage')
                            if usage:
                                self.last_usage = numeric_usage(usage)
                        elif kind == 'task-finished':
                            await sender
                            intent.update(state='completed', usage=self.last_usage)
                            return
        except asyncio.CancelledError:
            intent.update(state='cancelled_unknown')
            raise
        except Exception as error:
            intent.update(state='outcome_unknown', error_type=type(error).__name__)
            if isinstance(error, CloudError):
                raise
            raise CloudError('asr', type(error).__name__) from None
        finally:
            for job in (sender, reader):
                if job and not job.done():
                    job.cancel()
            await asyncio.gather(*(job for job in (sender, reader) if job), return_exceptions=True)
