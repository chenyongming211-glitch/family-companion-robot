"""One-shot controlled WS peer. Run on the cloud test host, never on the Mac.

No providers, keys or recordings. Native --fixtures is expected to fail its
unfinished scenario cleanly on sleep, without a websocket_receive error.
"""
import asyncio
from aiohttp import web


async def main():
    finished = asyncio.Event()
    async def connect(request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        await ws.receive_json()
        await ws.send_json({'type': 'hello', 'control_seq': 1, 'session_id': 'standby-probe',
                            'audio_params': {'format': 'opus', 'sample_rate': 24000, 'channels': 1, 'frame_duration': 60},
                            'features': {'drain': True, 'staging': True, 'coreaudio_playback': True},
                            'playback_basis': 'coreaudio_playback'})
        await ws.send_json({'type': 'ready', 'control_seq': 2, 'session_id': 'standby-probe',
                            'mode': 'drain', 'input_rate': 16000, 'output_rate': 24000})
        await ws.receive()  # Native starts its actual no-microphone input path.
        await ws.send_json({'type': 'sleep', 'code': 'idle', 'control_seq': 3, 'session_id': 'standby-probe'})
        await ws.close()
        finished.set()
        return ws
    app = web.Application()
    app.router.add_get('/xiaozhi/v1/', connect)
    runner = web.AppRunner(app, access_log=None)
    await runner.setup()
    await web.TCPSite(runner, '127.0.0.1', 18121).start()
    print('standby_probe_ready', flush=True)
    try:
        await asyncio.wait_for(finished.wait(), 30)
    finally:
        await runner.cleanup()


if __name__ == '__main__':
    asyncio.run(main())
