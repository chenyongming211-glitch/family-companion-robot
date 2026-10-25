# DEPRECATED_PRODUCT_PATH (2026-10-25): historical ASR -> LLM -> TTS cascade.
# Current product voice core is Go internal/realtimelive; do not extend this path.
# Reference/legacy service only; this marker does not stop a running instance.
# See docs/CODE_LIFECYCLE.md before reusing any business logic.
import asyncio
import base64
import hmac
import logging
import os

from aiohttp import web, WSMsgType

from .config import Settings
from .runtime import VoiceSession
from .logging_safe import configure_logging
from .browser import SESSION_SECONDS, install_browser
from .cloud import Intent
from .conversation_log import ConversationLogError


STATE = web.AppKey('voice_state', dict)


def create_app(cfg, *, session_factory=VoiceSession, drain_factory=None, session_seconds=SESSION_SECONDS):
    if not cfg.client_token:
        raise ValueError('VOICE_CLIENT_TOKEN is required for the test service')
    app = web.Application(client_max_size=16384)
    app[STATE] = {'active': False}
    browser_authorize = install_browser(app, cfg)

    async def health(request):
        return web.json_response({'status': 'ok', 'active_sessions': int(app[STATE]['active']), 'engine': 'livekit-chain', 'input_rate': 16000, 'output_rate': 24000, 'recording': False, 'text_recording': cfg.text_record_dir is not None})

    async def audio(request):
        if request.path == '/v1/browser/audio':
            browser_authorize(request)
        elif not hmac.compare_digest(request.headers.get('Authorization', ''), 'Bearer ' + cfg.client_token):
            raise web.HTTPUnauthorized()
        mode = request.query.get('mode', 'manual')
        if mode not in ('manual', 'continuous', 'drain') or (mode == 'drain' and request.path != '/v1/audio'):
            raise web.HTTPBadRequest()
        automatic = mode in ('continuous', 'drain')
        keep_open = mode == 'drain' and cfg.test_keep_open
        lifetime = None if keep_open else session_seconds
        if app[STATE]['active']:
            raise web.HTTPConflict()
        app[STATE]['active'] = True
        ws = web.WebSocketResponse(max_msg_size=16384, heartbeat=10, receive_timeout=30)
        voice, commit_job, cancel_job = None, None, None
        budget, audit = None, None
        input_id, phase, reason, error_type = 0, 'opening', 'disconnected', None
        try:
            await ws.prepare(request)
            opening, startup_events = True, []
            async def terminate_recording(event):
                nonlocal reason
                reason = 'text_recording_failed'
                try:
                    await asyncio.wait_for(ws.send_json(event), .2)
                except Exception:
                    pass
                finally:
                    try:
                        await asyncio.wait_for(ws.close(code=1011), .25)
                    except Exception:
                        pass
                    finally:
                        # Even cancellation of a blocked error write must end
                        # this failed recording connection and its audio input.
                        if request.transport:
                            request.transport.close()
            async def send(event):
                nonlocal input_id, reason
                if opening and event['type'] != 'ready':
                    if len(startup_events) >= 32:
                        raise BufferError('startup event limit')
                    startup_events.append(event)
                    return
                if event['type'] == 'error' and event.get('code') == 'text_recording_failed':
                    await terminate_recording(event)
                    return
                try:
                    await asyncio.wait_for(ws.send_json(event), .25)
                    if automatic and event['type'] == 'turn':
                        input_id = event['count']
                    if automatic and event['type'] == 'sleep':
                        reason = event['code']
                        await ws.close()
                except Exception:
                    await ws.close(code=1011)
                    raise
            if mode == 'drain':
                from .content_runtime import DrainVoiceSession
                voice = (drain_factory or DrainVoiceSession)(cfg, send)
            else:
                voice = session_factory(cfg, send, automatic=True) if automatic else session_factory(cfg, send)
            await voice.start()
            conversation = getattr(voice, 'conversation', None)
            audit = Intent(cfg, 'client_session')
            await send({'type': 'ready', 'input_rate': 16000, 'output_rate': 24000, 'session_seconds': lifetime, 'max_turns': None if keep_open else 8, 'mode': mode,
                        'manual_close': keep_open,
                        'text_recording': conversation is not None,
                        'conversation_id': conversation.id if conversation else None,
                        **({'capabilities': ['drain', 'staging', 'content_checkpoint']} if mode == 'drain' else {})})
            opening = False
            for event in startup_events:
                await send(event)
            startup_events.clear()
            accepting = False
            input_samples = 0
            budget = asyncio.timeout(lifetime)
            async with budget:
                async for message in ws:
                    if message.type != WSMsgType.TEXT:
                        break
                    event = message.json()
                    kind = event.get('type')
                    phase = kind if kind in ('input.start', 'input.audio', 'input.end', 'cancel', 'playback', 'listen.audio') else 'invalid_command'
                    if mode == 'drain' and kind in ('playback.started', 'content.snapshot', 'hard.stop'):
                        if kind == 'playback.started':
                            voice.output.mark_started(event.get('block_id'), event.get('epoch'))
                        elif kind == 'content.snapshot':
                            await send({'type': 'content.snapshot', 'contents': voice.content_snapshot()})
                        else:
                            await voice.hard_stop()
                            await send({'type': 'hard.stopped'})
                        continue
                    if automatic and kind != 'playback':
                        if kind != 'listen.audio':
                            raise ValueError('continuous mode requires continuous audio')
                        pcm = base64.b64decode(event['data'], validate=True)
                        if not pcm or len(pcm) % 2 or len(pcm) > 640:
                            raise ValueError('invalid continuous PCM frame')
                        voice.feed(pcm)
                        continue
                    if kind == 'input.start':
                        if input_id >= 8:
                            reason = 'turn_limit'
                            await send({'type': 'error', 'code': reason, 'max_turns': 8})
                            break
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
        except ConversationLogError as error:
            reason, error_type = 'text_recording_failed', type(error).__name__
            if not ws.closed:
                await terminate_recording({'type': 'error', 'code': reason, 'stage': phase, 'error_type': error_type})
        except (ValueError, KeyError, TypeError, BufferError) as error:
            reason, error_type = 'invalid_input', type(error).__name__
            if not ws.closed:
                await ws.send_json({'type': 'error', 'code': reason, 'stage': phase, 'error_type': error_type})
        except TimeoutError:
            reason = 'session_expired' if budget and budget.expired() else 'operation_timeout'
            error_type = 'TimeoutError'
            if not ws.closed:
                await ws.send_json({'type': 'error', 'code': reason, 'stage': phase, 'error_type': error_type})
        except Exception as error:
            reason, error_type = 'session_failed', type(error).__name__
            if not ws.closed:
                await ws.send_json({'type': 'error', 'code': reason, 'stage': phase, 'error_type': error_type})
        finally:
            if audit:
                try:
                    audit.update(state=reason, last_command=phase, turns=input_id, limit_seconds=lifetime, error_type=error_type)
                except OSError:
                    pass
            try:
                for job in (commit_job, cancel_job):
                    if job and not job.done():
                        job.cancel()
                await asyncio.gather(*(job for job in (commit_job, cancel_job) if job), return_exceptions=True)
                if voice:
                    await voice.close()
            except ConversationLogError:
                reason, error_type = 'text_recording_failed', 'ConversationLogError'
                reporter = getattr(voice, '_recording_failed', None)
                if reporter:
                    reporter()
                else:
                    logging.getLogger('family_voice.text_recording_failed').error('text_recording_failed')
                if audit:
                    try:
                        audit.update(state=reason, error_type=error_type)
                    except OSError:
                        pass
                if not ws.closed:
                    await terminate_recording({'type': 'error', 'code': reason, 'stage': 'closing'})
            except Exception:
                pass
            finally:
                app[STATE]['active'] = False
                if ws.prepared:
                    await ws.close(code=1011 if reason == 'text_recording_failed' else 1000)
        return ws

    app.router.add_get('/healthz', health)
    app.router.add_get('/v1/audio', audio)
    app.router.add_get('/v1/browser/audio', audio)
    return app


def main():
    configure_logging()
    cfg = Settings.from_env(os.environ)
    web.run_app(create_app(cfg), host='127.0.0.1', port=18010, access_log=None, print=None)


if __name__ == '__main__':
    main()
