"""One-shot cloud-only peer: burst revocations saturate the native write queue.

No microphone/providers/recording. The client must close once with failure,
instead of recursively enqueueing another stop message into the full queue.
"""
import asyncio
from aiohttp import web


async def main():
    finished = asyncio.Event()
    async def connect(request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        await ws.receive_json()
        sequence = 0
        async def send(kind, **values):
            nonlocal sequence
            sequence += 1
            await ws.send_json({'type': kind, 'session_id': 'teardown-probe', 'control_seq': sequence, **values})
        try:
            await send('hello', audio_params={'format': 'opus', 'sample_rate': 24000, 'channels': 1, 'frame_duration': 60},
                       features={'drain': True, 'staging': True, 'coreaudio_playback': True}, playback_basis='coreaudio_playback')
            await send('ready', mode='drain', input_rate=16000, output_rate=24000, manual_close=True)
            await ws.receive()
            for identity in range(1, 17):
                await send('output.handoff', block_id=identity, epoch=identity, input_epoch=0, after=[1] if identity > 1 else [])
                await send('audio.end', block_id=identity, epoch=identity, device_sent_samples=0, valid_samples=0, lookahead_samples=0)
            await send('output.revoke', blocks=list(range(2, 17)), input_epoch=1)
            async for _ in ws:
                pass
        finally:
            await ws.close()
            finished.set()
        return ws
    app = web.Application()
    app.router.add_get('/xiaozhi/v1/', connect)
    runner = web.AppRunner(app, access_log=None)
    await runner.setup()
    await web.TCPSite(runner, '127.0.0.1', 18122).start()
    print('teardown_probe_ready', flush=True)
    try:
        await asyncio.wait_for(finished.wait(), 30)
    finally:
        await runner.cleanup()


if __name__ == '__main__':
    asyncio.run(main())
