import asyncio
import base64
import hmac
import os

from aiohttp import web, WSMsgType

from .config import Settings
from .runtime import VoiceSession
from .logging_safe import configure_logging


STATE = web.AppKey('voice_state', dict)


def create_app(cfg, *, session_factory=VoiceSession):
    if not cfg.client_token:
        raise ValueError('VOICE_CLIENT_TOKEN is required for the test service')
    app = web.Application(client_max_size=16384)
    app[STATE] = {'active': False}

    async def health(request):
        return web.json_response({'status': 'ok', 'active_sessions': int(app[STATE]['active']), 'engine': 'livekit-chain', 'input_rate': 16000, 'output_rate': 24000, 'recording': False})

    async def audio(request):
        if not hmac.compare_digest(request.headers.get('Authorization', ''), 'Bearer ' + cfg.client_token):
            raise web.HTTPUnauthorized()
        if app[STATE]['active']:
            raise web.HTTPConflict()
        app[STATE]['active'] = True
        ws = web.WebSocketResponse(max_msg_size=16384, heartbeat=10, receive_timeout=30)
        voice, commit_job, cancel_job = None, None, None
        try:
            await ws.prepare(request)
            async def send(event):
                try:
                    await asyncio.wait_for(ws.send_json(event), .25)
                except Exception:
                    await ws.close(code=1011)
                    raise
            voice = session_factory(cfg, send)
            await voice.start()
            await send({'type': 'ready', 'input_rate': 16000, 'output_rate': 24000})
            input_id = 0
            accepting = False
            input_samples = 0
            async with asyncio.timeout(300):
                async for message in ws:
                    if message.type != WSMsgType.TEXT:
                        break
                    event = message.json()
                    kind = event.get('type')
                    if kind == 'input.start':
                        if input_id >= 8:
                            raise ValueError('turn limit')
                        if commit_job and not commit_job.done():
                            commit_job.cancel()
                            await asyncio.gather(commit_job, return_exceptions=True)
                        await voice.begin_input()
                        input_id += 1
                        accepting, input_samples = True, 0
                        await send({'type': 'input.ready', 'input_id': input_id})
                    elif kind in ('input.audio', 'input.end'):
                        if not accepting or type(event.get('input_id')) is not int or event['input_id'] != input_id:
                            await send({'type': 'error', 'code': 'invalid_input'})
                            continue
                        if kind == 'input.audio':
                            pcm = base64.b64decode(event['data'], validate=True)
                            input_samples += len(pcm) // 2
                            if input_samples > 16000 * 29:
                                raise ValueError('input limit')
                            voice.feed(pcm)
                        else:
                            accepting = False
                            commit_job = asyncio.create_task(voice.commit())
                            def committed(job):
                                if not job.cancelled() and job.exception():
                                    asyncio.create_task(send({'type': 'error', 'code': 'commit_failed', 'error_type': type(job.exception()).__name__}))
                            commit_job.add_done_callback(committed)
                    elif kind == 'cancel':
                        accepting = False
                        if cancel_job and not cancel_job.done():
                            continue
                        async def cancel():
                            await voice.interrupt()
                            await send({'type': 'cancelled'})
                        cancel_job = asyncio.create_task(cancel())
                        cancel_job.add_done_callback(lambda job: None if job.cancelled() else job.exception())
                    elif kind == 'playback':
                        voice.receipt(event)
                    else:
                        raise ValueError('invalid command')
        except (ValueError, KeyError, TypeError, BufferError):
            if not ws.closed:
                await ws.send_json({'type': 'error', 'code': 'invalid_input'})
        except Exception:
            if not ws.closed:
                await ws.send_json({'type': 'error', 'code': 'session_failed'})
        finally:
            try:
                for job in (commit_job, cancel_job):
                    if job and not job.done():
                        job.cancel()
                await asyncio.gather(*(job for job in (commit_job, cancel_job) if job), return_exceptions=True)
                if voice:
                    await voice.close()
            except Exception:
                pass
            finally:
                app[STATE]['active'] = False
                if ws.prepared:
                    await ws.close()
        return ws

    app.router.add_get('/healthz', health)
    app.router.add_get('/v1/audio', audio)
    return app


def main():
    configure_logging()
    cfg = Settings.from_env(os.environ)
    web.run_app(create_app(cfg), host='127.0.0.1', port=18010, access_log=None, print=None)


if __name__ == '__main__':
    main()
