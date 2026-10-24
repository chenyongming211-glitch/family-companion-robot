"""Loopback browser access. No long-lived server credential leaves this process."""
import hmac
import secrets
import time
from pathlib import Path
from urllib.parse import urlsplit

from aiohttp import web

GATE = web.AppKey('browser_gate', object)
ASSETS = {'client.mjs', 'audio-core.mjs', 'input-worklet.mjs', 'lifecycle.mjs', 'client.css'}
ROOT = Path(__file__).parent.parent / 'web'
SESSION_SECONDS = 900


def loopback_origin(origin):
    try:
        value = urlsplit(origin)
        return value.scheme == 'http' and value.hostname in ('localhost', '127.0.0.1', '::1') and not value.username and not value.password and value.path == '' and not value.query and not value.fragment and bool(value.port)
    except ValueError:
        return False


class BrowserGate:
    def __init__(self):
        self.clock = time.monotonic
        self.tickets, self.sessions = {}, {}

    def prune(self):
        now = self.clock()
        for store in (self.tickets, self.sessions):
            for key, (_, expires) in tuple(store.items()):
                if expires <= now:
                    del store[key]

    def origin(self, request):
        origin = request.headers.get('Origin', '')
        if not loopback_origin(origin) or origin != f'http://{request.host}':
            raise web.HTTPForbidden()
        return origin

    def authorize(self, request):
        origin = self.origin(request)
        self.prune()
        if self.sessions.get(request.cookies.get('voice_browser', ''), (None, 0))[0] != origin:
            raise web.HTTPUnauthorized()


def install_browser(app, cfg):
    gate = BrowserGate()
    app[GATE] = gate

    async def static(request):
        if not loopback_origin(f'http://{request.host}'):
            raise web.HTTPForbidden()
        asset = request.match_info.get('asset', 'index.html')
        if asset != 'index.html' and asset not in ASSETS:
            raise web.HTTPNotFound()
        response = web.FileResponse(ROOT / asset)
        response.headers.update({
            'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff',
            'Referrer-Policy': 'no-referrer', 'Permissions-Policy': 'microphone=(self), camera=()',
            'Content-Security-Policy': "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'",
        })
        if asset.endswith('.mjs'):
            response.content_type = 'text/javascript'
        return response

    async def ticket(request):
        if not hmac.compare_digest(request.headers.get('Authorization', ''), 'Bearer ' + cfg.client_token):
            raise web.HTTPUnauthorized()
        origin = (await request.json()).get('origin')
        if not isinstance(origin, str) or not loopback_origin(origin):
            raise web.HTTPBadRequest()
        gate.prune()
        if len(gate.tickets) >= 8 or len(gate.sessions) >= 8:
            raise web.HTTPTooManyRequests()
        value = secrets.token_urlsafe(32)
        gate.tickets[value] = (origin, gate.clock() + 120)
        return web.json_response({'ticket': value}, headers={'Cache-Control': 'no-store'})

    async def pair(request):
        origin = gate.origin(request)
        value = (await request.json()).get('ticket')
        if not isinstance(value, str) or len(value) > 128:
            raise web.HTTPUnauthorized()
        gate.prune()
        if gate.tickets.get(value, (None, 0))[0] != origin:
            raise web.HTTPUnauthorized()
        del gate.tickets[value]
        cookie = secrets.token_urlsafe(32)
        gate.sessions[cookie] = (origin, gate.clock() + SESSION_SECONDS)
        response = web.json_response({'paired': True, 'expires_in': SESSION_SECONDS}, headers={'Cache-Control': 'no-store'})
        response.set_cookie('voice_browser', cookie, httponly=True, samesite='Strict', max_age=SESSION_SECONDS, path='/v1/browser')
        return response

    app.router.add_get('/client', static)
    app.router.add_get('/client/{asset}', static)
    app.router.add_post('/v1/browser/ticket', ticket)
    app.router.add_post('/v1/browser/pair', pair)
    return gate.authorize
