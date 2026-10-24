"""Server-side synthetic integration client. Never a microphone/physical playback test."""

import argparse
import asyncio
import base64
import json
import re
import shlex
import time
import wave
from pathlib import Path

import aiohttp
from livekit import rtc

from .cloud import Cloud
from .config import Settings
from .logging_safe import configure_logging


def answer_matches(text, expected):
    text = re.sub(r'[\s，。！？,.!?：:]', '', text or '')
    allowed = {2: ('2', '二', '一加一等于二', '一加一等于2', '1+1=2', '1+1等于2'), 5: ('5', '五', '二加三等于五', '二加三等于5', '2+3=5', '2+3等于5')}
    return text in allowed[expected]


def read_config(*paths):
    values = {}
    for path in paths:
        for line in Path(path).read_text().splitlines():
            if line.strip() and not line.lstrip().startswith('#') and '=' in line:
                name, value = line.split('=', 1)
                values[name.strip()] = shlex.split(value)[0] if value.strip() else ''
    return values


class SimulatedPlayer:
    def __init__(self, send, out=None):
        self.send, self.out = send, out
        self.blocks = {}
        self.epoch = 0
        self.received_frames = self.played_samples = self.dropped_old_frames = 0

    async def _play(self, block):
        played = bytearray()
        try:
            while True:
                pcm = await block['queue'].get()
                if pcm is None:
                    break
                await asyncio.sleep(len(pcm) / 48000)
                played.extend(pcm)
                self.played_samples += len(pcm) // 2
        except asyncio.CancelledError:
            pass
        finally:
            block['played'] = len(played) // 2
            if self.out and played:
                with wave.open(str(self.out / f"played-{block['id']}.wav"), 'wb') as f:
                    f.setnchannels(1); f.setsampwidth(2); f.setframerate(24000); f.writeframes(played)
            await self.send({'type': 'playback', 'epoch': block['epoch'], 'block_id': block['id'], 'played_samples': block['played'], 'valid': True, 'finished': True})

    async def event(self, event):
        kind = event.get('type')
        if kind == 'audio.cancel':
            self.epoch = max(self.epoch, event['next_epoch'])
            jobs = [b['job'] for b in self.blocks.values() if b['epoch'] < self.epoch and not b['job'].done()]
            for job in jobs:
                job.cancel()
            await asyncio.gather(*jobs, return_exceptions=True)
        elif kind == 'audio':
            if event['epoch'] < self.epoch:
                self.dropped_old_frames += 1
                return
            pcm = base64.b64decode(event['data'], validate=True)
            if len(pcm) != event['samples'] * 2:
                raise ValueError('output PCM size mismatch')
            self.received_frames += 1
            ident = event['block_id']
            if ident not in self.blocks:
                block = {'id': ident, 'epoch': event['epoch'], 'queue': asyncio.Queue(maxsize=6000), 'played': 0}
                block['job'] = asyncio.create_task(self._play(block))
                self.blocks[ident] = block
            self.blocks[ident]['queue'].put_nowait(pcm)
        elif kind == 'audio.end':
            block = self.blocks.get(event['block_id'])
            if block and event['epoch'] >= self.epoch:
                block['queue'].put_nowait(None)

    async def drain(self):
        results = await asyncio.gather(*(b['job'] for b in self.blocks.values()), return_exceptions=True)
        for result in results:
            if isinstance(result, Exception):
                raise result

    async def close(self):
        jobs = [b['job'] for b in self.blocks.values() if not b['job'].done()]
        for job in jobs:
            job.cancel()
        await asyncio.gather(*jobs, return_exceptions=True)


class Client:
    def __init__(self, ws, out):
        self.ws, self.out = ws, out
        self.events = []
        self.input_id = 0
        self.player = SimulatedPlayer(ws.send_json, out)
        self.reader = asyncio.create_task(self.receive())

    async def receive(self):
        async for message in self.ws:
            if message.type != aiohttp.WSMsgType.TEXT:
                break
            event = message.json()
            await self.player.event(event)
            if event.get('type') != 'audio':
                self.events.append({**event, 'received_at': time.monotonic()})

    async def wait(self, predicate, timeout=30):
        async with asyncio.timeout(timeout):
            while not predicate():
                if self.reader.done():
                    await self.reader
                    raise RuntimeError('connection ended')
                if any(e['type'] == 'error' for e in self.events):
                    raise RuntimeError('server reported stage failure')
                await asyncio.sleep(.01)

    async def input(self, pcm):
        self.input_id += 1
        await self.ws.send_json({'type': 'input.start'})
        await self.wait(lambda: any(e['type'] == 'input.ready' and e['input_id'] == self.input_id for e in self.events))
        for i in range(0, len(pcm), 640):
            part = pcm[i:i+640]
            await self.ws.send_json({'type': 'input.audio', 'input_id': self.input_id, 'data': base64.b64encode(part).decode()})
            await asyncio.sleep(len(part) / 32000)
        finished = time.monotonic()
        await self.ws.send_json({'type': 'input.end', 'input_id': self.input_id})
        return finished

    async def question(self, pcm, expected):
        start = len(self.events)
        end = await self.input(pcm)
        await self.wait(lambda: any(e['type'] == 'reply' and not e.get('interrupted') for e in self.events[start:]))
        reply = next(e for e in self.events[start:] if e['type'] == 'reply' and not e.get('interrupted'))
        if not answer_matches(reply['text'], expected):
            raise RuntimeError('answer mismatch')
        await asyncio.wait_for(self.player.drain(), 15)
        return {'answer': reply['text'], 'input_end_to_reply_ms': round((reply['received_at'] - end) * 1000)}

    async def close(self):
        await self.player.close()
        self.reader.cancel()
        await asyncio.gather(self.reader, return_exceptions=True)


async def fixture(cfg, text, destination):
    pcm = b''.join([data async for data in Cloud(cfg).tts(text)])
    resampler = rtc.AudioResampler(24000, 16000)
    frame = rtc.AudioFrame(pcm, 24000, 1, len(pcm) // 2)
    pcm16 = b''.join(bytes(f.data) for f in [*resampler.push(frame), *resampler.flush()])
    with wave.open(str(destination), 'wb') as f:
        f.setnchannels(1); f.setsampwidth(2); f.setframerate(16000); f.writeframes(pcm16)
    return pcm16


async def run(args):
    out = Path(args.out)
    out.mkdir(mode=0o700, parents=True, exist_ok=False)
    values = read_config(args.config, args.client_config)
    values['VOICE_AUDIT_DIR'] = str(out / 'fixture-calls')
    cfg = Settings.from_env(values)
    result = {'playback_evidence': 'simulated_timed_client', 'physical_audio_tested': False, 'ok': False}
    client = None
    try:
        if args.fixtures:
            def read(name):
                with wave.open(str(Path(args.fixtures) / name), 'rb') as f:
                    if (f.getnchannels(), f.getsampwidth(), f.getframerate(), f.getcomptype()) != (1, 2, 16000, 'NONE') or f.getnframes() > 16000 * 29:
                        raise ValueError('fixture requires mono16 16k WAV up to 29 seconds')
                    return f.readframes(f.getnframes())
            first, second, long = (read(name) for name in ('first.wav', 'second.wav', 'long.wav'))
        else:
            first = await fixture(cfg, '请问一加一等于几？', out / 'first.wav')
            second = await fixture(cfg, '请问二加三等于几？', out / 'second.wav')
            long = await fixture(cfg, '请从一一直数到五十。', out / 'long.wav')
        async with aiohttp.ClientSession() as http:
            async with http.ws_connect(args.url, headers={'Authorization': 'Bearer ' + cfg.client_token}) as ws:
                client = Client(ws, out)
                await client.wait(lambda: any(e['type'] == 'ready' for e in client.events))
                result['round1'] = await client.question(first, 2)
                result['round2'] = await client.question(second, 5)
                before = client.player.received_frames
                await client.input(long)
                await client.wait(lambda: client.player.received_frames > before)
                await asyncio.sleep(.1)
                t = time.monotonic()
                await ws.send_json({'type': 'cancel'})
                await client.wait(lambda: any(e['type'] == 'cancelled' and e['received_at'] > t for e in client.events))
                result['cancel_control_ack_ms'] = round((time.monotonic() - t) * 1000)
                result['after_cancel'] = await client.question(second, 5)
                result['received_frames'] = client.player.received_frames
                result['discarded_stale_frames'] = client.player.dropped_old_frames
                result['ok'] = True
                await client.close()
    except Exception as error:
        result['error_type'] = type(error).__name__
        raise
    finally:
        if client:
            await client.close()
            (out / 'events.json').write_text(json.dumps(client.events, ensure_ascii=False, indent=2))
        (out / 'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2))
        print(json.dumps(result, ensure_ascii=False))


def main():
    configure_logging()
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', default='/etc/family-robot/server.env')
    parser.add_argument('--client-config', default='/etc/family-robot/livekit-dev.env')
    parser.add_argument('--url', default='http://127.0.0.1:18010/v1/audio')
    parser.add_argument('--out', required=True)
    parser.add_argument('--fixtures', help='reuse an existing validated synthetic fixture directory')
    args = parser.parse_args()
    asyncio.run(run(args))


if __name__ == '__main__':
    main()
