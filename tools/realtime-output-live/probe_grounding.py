"""Explicit paid grounding probe. Run on the cloud with synthetic WAV only."""
import argparse
import asyncio
import json
import os
import time
from pathlib import Path

from replay_history import replay
from server import INSTRUCTIONS
from upstream import StreamingNative
from providers import settings, wav_pcm, private_json


async def probe(args):
    os.umask(0o077)
    _, snapshots = replay(args.events)
    snapshot = snapshots[args.turn]['after']
    directory = Path('/var/lib/family-robot-realtime-test/verification') / ('history-dialogue-' + time.strftime('%Y%m%dT%H%M%S') + '-' + os.urandom(3).hex())
    directory.mkdir(mode=0o700)
    cfg = settings()
    cfg['synthetic_only'] = True
    instructions = INSTRUCTIONS + json.dumps(snapshot, ensure_ascii=False)
    native = StreamingNative(cfg, directory)
    started, first, samples, text = time.monotonic(), None, 0, ''
    try:
        await native.open(instructions)
        async for kind, value in native.stream(wav_pcm(args.wav)):
            if kind == 'text':
                text = value
            else:
                if first is None:
                    first = round((time.monotonic() - started) * 1000)
                samples += len(value) // 2
        result = {'before_turn': args.turn, 'transcript': text, 'streaming': True,
            'output_samples': samples, 'first_released_pcm_ms': first}
        private_json(directory / 'result.json', result)
        output = {'passed': bool(samples), 'before_turn': args.turn,
            'first_released_pcm_ms': first, 'directory': str(directory)}
        if args.show_reply:
            output['reply'] = text
        print(json.dumps(output, ensure_ascii=False))
    except BaseException as error:
        private_json(directory / 'failure.json', {'error_type': type(error).__name__})
        raise
    finally:
        await native.close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--events', required=True, type=Path)
    parser.add_argument('--turn', required=True, type=int)
    parser.add_argument('--wav', required=True, type=Path)
    parser.add_argument('--run-provider', required=True, action='store_true')
    parser.add_argument('--show-reply', action='store_true')
    asyncio.run(probe(parser.parse_args()))
