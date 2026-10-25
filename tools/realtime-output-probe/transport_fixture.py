# LEGACY_VALIDATION_REFERENCE (2026-10-25): Python baseline/probe, not Go runtime.
# Some modules remain active Go differential-test or legacy rollback dependencies.
# Keep reference behavior stable; see docs/CODE_LIFECYCLE.md.
"""Cloud-only no-provider regression: a packet arrives after the cut receipt."""
import asyncio
from aiohttp import web
from codec import opus_roundtrip


async def main():
    finished = asyncio.Event()
    packets, _ = opus_roundtrip(b'\0' * 24000)

    async def ready(request):
        return web.json_response({'ready': True})

    async def voice(request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        try:
            await ws.receive_json()
            await ws.send_json({'type': 'begin', 'id': 'original', 'cut_samples': 2880})
            for packet in packets[:6]:
                await ws.send_bytes(packet)
            receipt = await ws.receive_json(timeout=5)
            assert receipt['type'] == 'cut' and receipt['completed_samples'] >= 2880
            # This packet was already in flight when the playback stopped.
            await ws.send_bytes(packets[0])
            await asyncio.sleep(.05)
            await ws.send_json({'type': 'result', 'success': True, 'code': 'late_packet_regression'})
            async for _ in ws:
                pass
        except (ConnectionResetError, RuntimeError):
            pass
        finally:
            await ws.close()
            finished.set()
        return ws

    app = web.Application()
    app.router.add_get('/readyz', ready)
    app.router.add_get('/probe', voice)
    runner = web.AppRunner(app, access_log=None)
    await runner.setup()
    await web.TCPSite(runner, '127.0.0.1', 18031).start()
    print('transport_fixture_ready', flush=True)
    try:
        await asyncio.wait_for(finished.wait(), 90)
    finally:
        await runner.cleanup()


if __name__ == '__main__':
    asyncio.run(main())
