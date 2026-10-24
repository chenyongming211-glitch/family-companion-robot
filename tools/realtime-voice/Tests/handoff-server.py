"""Cloud-only controlled Xiaozhi fixture; no provider, credentials or microphone.

Sends two valid normal TTS blocks without waiting for the Mac's output tail.
Run with the cloud's existing aiohttp Python; loopback18031, terminate after test.
"""
import asyncio
import ctypes

from aiohttp import web


def packet():
    opus = ctypes.CDLL('libopus.so.0')
    opus.opus_encoder_create.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_int, ctypes.POINTER(ctypes.c_int)]
    opus.opus_encoder_create.restype = ctypes.c_void_p
    opus.opus_encode.argtypes = [ctypes.c_void_p, ctypes.POINTER(ctypes.c_int16), ctypes.c_int, ctypes.POINTER(ctypes.c_ubyte), ctypes.c_int]
    opus.opus_encoder_destroy.argtypes = [ctypes.c_void_p]
    error = ctypes.c_int()
    encoder = opus.opus_encoder_create(24000, 1, 2049, ctypes.byref(error))
    if not encoder or error.value:
        raise RuntimeError('fixture encoder')
    try:
        source = (ctypes.c_int16 * 1440)()
        output = (ctypes.c_ubyte * 4000)()
        count = opus.opus_encode(encoder, source, 1440, output, 4000)
        if count <= 0:
            raise RuntimeError('fixture packet')
        return bytes(output[:count])
    finally:
        opus.opus_encoder_destroy(encoder)


async def pair(request):
    return web.json_response({'device_id': 'native-realtime', 'backend': 'qwen-native-existing',
                             'transport': 'xiaozhi-opus-v1', 'ticket': 'x' * 43,
                             'model': 'controlled-audio-handoff', 'voice': 'fixture'})


async def voice(request):
    ws = web.WebSocketResponse()
    await ws.prepare(request)
    await ws.receive_json()
    await ws.send_json({'type': 'hello', 'session_id': 'handoff-fixture', 'version': 1,
                        'transport': 'websocket', 'audio_params': {'format': 'opus',
                        'sample_rate': 24000, 'channels': 1, 'frame_duration': 60}})

    async def produce():
        for _ in range(2):
            await ws.send_json({'type': 'tts', 'state': 'start', 'session_id': 'handoff-fixture'})
            for _ in range(3):
                await ws.send_bytes(packet())
                await asyncio.sleep(.06)
            await ws.send_json({'type': 'tts', 'state': 'stop', 'session_id': 'handoff-fixture'})

    task = asyncio.create_task(produce())
    try:
        async for _ in ws:
            pass
    finally:
        task.cancel()
        await asyncio.gather(task, return_exceptions=True)
    return ws


app = web.Application()
app.router.add_post('/v1/pair', pair)
app.router.add_get('/xiaozhi/v1/', voice)
web.run_app(app, host='127.0.0.1', port=18031, access_log=None)
