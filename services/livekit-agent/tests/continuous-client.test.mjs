import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import { PlaybackLedger } from '../web/audio-core.mjs';
import { AsyncResource, AttemptGate, aborted, serviceErrorMessage } from '../web/lifecycle.mjs';

const source = (await readFile(new URL('../web/client.mjs', import.meta.url), 'utf8')).replace(/^import .*;\n/gm, '');
const html = await readFile(new URL('../web/index.html', import.meta.url), 'utf8');
const ready = {type: 'ready', mode: 'continuous', input_rate: 16000, output_rate: 24000, session_seconds: 900, max_turns: 8};
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
function deferred() { let resolve; const promise = new Promise(done => { resolve = done; }); return {promise, resolve}; }

async function browser({permission, workletLoad} = {}) {
  const elements = new Map([...html.matchAll(/id="([^"]+)"/g)].map(([, id]) => [id, {textContent: '', value: 0, disabled: false}]));
  const documentEvents = new Map(), windowEvents = new Map(), sockets = [], contexts = [], captures = [], media = [], requests = [], timers = new Map();
  let nextTimer = 0;
  const document = {hidden: false, getElementById: id => elements.get(id) || null, addEventListener: (event, fn) => documentEvents.set(event, fn)};
  const connectable = () => ({connect(node) { return node; }, disconnect() { this.disconnected = true; }});
  class AudioContext {
    constructor() {
      this.state = 'suspended'; this.sampleRate = 48000; this.currentTime = 10; this.destination = {}; this.sources = [];
      this.audioWorklet = {addModule: async () => { if (workletLoad) await workletLoad.promise; }};
      contexts.push(this);
    }
    async resume() { this.state = 'running'; }
    close() { this.state = 'closed'; return Promise.resolve(); }
    getOutputTimestamp() { return {contextTime: this.currentTime, performanceTime: 100}; }
    createMediaStreamSource() { return connectable(); }
    createGain() { return {...connectable(), gain: {value: 1}}; }
    createBuffer(_channels, samples) { return {getChannelData: () => new Float32Array(samples)}; }
    createBufferSource() {
      const node = {...connectable(), start(time) { this.started = time; }, stop() { this.stopped = true; }};
      this.sources.push(node); return node;
    }
  }
  class AudioWorkletNode {
    constructor() { Object.assign(this, connectable()); this.messages = []; this.port = {postMessage: event => this.messages.push(event)}; captures.push(this); }
  }
  class WebSocket {
    static OPEN = 1;
    constructor(url) { this.url = url; this.readyState = 0; this.bufferedAmount = 0; this.sent = []; sockets.push(this); }
    send(value) { this.sent.push(JSON.parse(value)); }
    close() { this.readyState = 3; this.closed = true; this.onclose?.(); }
  }
  const makeMedia = () => {
    const track = {stop() { this.stopped = true; }, getSettings: () => ({echoCancellation: true})};
    const value = {track, getTracks: () => [track], getAudioTracks: () => [track]}; media.push(value); return value;
  };
  const globals = {
    document, window: {addEventListener: (event, fn) => windowEvents.set(event, fn)},
    location: {protocol: 'http:', host: '127.0.0.1:18010', hash: '#pair=test-ticket'}, history: {replaceState() {}},
    navigator: {mediaDevices: {getUserMedia: async options => { requests.push(options); if (permission) return permission.promise; return makeMedia(); }}},
    AudioContext, AudioWorkletNode, WebSocket, PlaybackLedger, AsyncResource, AttemptGate, aborted, serviceErrorMessage,
    fetch: async () => ({ok: true, json: async () => ({expires_in: 900})}),
    setInterval: fn => { timers.set(++nextTimer, fn); return nextTimer; }, clearInterval: id => timers.delete(id),
    setTimeout: fn => { timers.set(++nextTimer, fn); return nextTimer; }, clearTimeout: id => timers.delete(id),
    performance: {now: () => 100}, URLSearchParams, Uint8Array, DOMException, console,
    btoa: value => Buffer.from(value, 'binary').toString('base64'), atob: value => Buffer.from(value, 'base64').toString('binary'),
  };
  vm.runInNewContext(source, globals, {filename: 'client.mjs'});
  await flush();
  return {
    elements, document, sockets, contexts, captures, media, requests, makeMedia,
    click: id => elements.get(id).onclick(),
    async message(event, socket = sockets.at(-1)) { socket.readyState = WebSocket.OPEN; socket.onmessage?.({data: JSON.stringify(event)}); await flush(); },
    pcm(value = 1) { captures.at(-1).port.onmessage({data: {type: 'pcm', buffer: new Uint8Array([value, 0]).buffer}}); },
    async hide() { document.hidden = true; documentEvents.get('visibilitychange')(); await flush(); },
    async pagehide() { windowEvents.get('pagehide')(); await flush(); },
    async tick() { for (const fn of [...timers.values()]) fn(); await flush(); },
  };
}

async function start(browser) { await browser.click('connect'); await browser.message(ready); }
const audio = (epoch, block_id) => ({type: 'audio', epoch, block_id, samples: 2, data: Buffer.from([1, 0, 2, 0]).toString('base64')});

test('one paired click connects continuous mode and starts capture once after ready', async () => {
  const ui = await browser();
  await ui.click('connect');
  assert.match(ui.sockets[0].url, /\/v1\/browser\/audio\?mode=continuous$/);
  assert.equal(ui.captures[0].messages.length, 0, 'capture waits for server readiness');
  await ui.message(ready);
  await ui.message(ready);
  assert.deepEqual(ui.captures[0].messages.map(event => event.type), ['start']);
  assert.equal(ui.requests.length, 1);
  assert.equal(ui.requests[0].audio.echoCancellation, true);
  assert.equal(ui.requests[0].audio.noiseSuppression, true);
  assert.equal(ui.elements.get('connect').textContent, '结束对话');
});

test('capture keeps sending listen.audio during output, cancellation, and subsequent turns', async () => {
  const ui = await browser(); await start(ui);
  ui.pcm(1);
  await ui.message({type: 'agent.state', state: 'speaking'});
  await ui.message(audio(0, 1)); ui.pcm(2);
  await ui.message({type: 'audio.cancel', next_epoch: 1}); ui.pcm(3);
  await ui.message({type: 'user.state', state: 'speaking'});
  await ui.message({type: 'transcript', text: '二加三等于几', final: true});
  await ui.message({type: 'reply', text: '等于五'});
  await ui.message({type: 'turn', count: 2, max_turns: 8});
  ui.pcm(4);
  assert.deepEqual(ui.sockets[0].sent.filter(event => event.type !== 'playback').map(event => event.type), ['listen.audio', 'listen.audio', 'listen.audio', 'listen.audio']);
  assert.deepEqual(ui.captures[0].messages.map(event => event.type), ['start']);
  assert.match(ui.elements.get('budget').textContent, /2\/8/);
  assert.equal(ui.elements.get('transcript').textContent, '二加三等于几');
  assert.equal(ui.elements.get('reply').textContent, '等于五');
});

test('remote barge-in stops old output immediately and allows the next output epoch', async () => {
  const ui = await browser(); await start(ui);
  await ui.message(audio(0, 1));
  const old = ui.contexts[0].sources[0];
  await ui.message({type: 'audio.cancel', next_epoch: 1});
  assert.equal(old.stopped, true);
  await ui.message(audio(0, 1));
  assert.equal(ui.contexts[0].sources.length, 1, 'retired epoch stays retired');
  await ui.message(audio(1, 2));
  assert.equal(ui.contexts[0].sources.length, 2, 'future audio remains playable');
  assert.equal(ui.contexts[0].sources[1].stopped, undefined);
  await ui.message({type: 'audio.cancel', next_epoch: 1});
  await ui.message(audio(1, 2));
  assert.ok(ui.contexts[0].sources[2].started >= ui.contexts[0].sources[1].started + 2 / 24000, 'a repeated cancellation cannot overlap new audio');
});

for (const code of ['idle', 'turn_limit', 'utterance_limit']) test(`${code} sleep releases microphone and socket, returning the same button to standby`, async () => {
  const ui = await browser(); await start(ui);
  await ui.message({type: 'sleep', code});
  assert.equal(ui.media[0].track.stopped, true);
  assert.equal(ui.sockets[0].closed, true);
  assert.equal(ui.contexts[0].state, 'closed');
  assert.equal(ui.elements.get('connect').textContent, '开始说话');
  assert.equal(ui.elements.get('connect').disabled, false);
  assert.match(ui.elements.get('status').textContent, /待机/);
  assert.equal(ui.elements.get('error').textContent, '');
});

test('a second click ends the conversation and a fresh click creates only one new capture', async () => {
  const ui = await browser(); await start(ui);
  await ui.click('connect');
  assert.equal(ui.media[0].track.stopped, true);
  assert.equal(ui.sockets[0].closed, true);
  await ui.click('connect'); await ui.message(ready);
  assert.equal(ui.sockets.length, 2);
  assert.equal(ui.requests.length, 2);
  assert.deepEqual(ui.captures[1].messages.map(event => event.type), ['start']);
});

test('ending while permission is pending disposes late microphone access without opening a socket', async () => {
  const permission = deferred(), ui = await browser({permission});
  const starting = ui.click('connect'); await flush();
  await ui.click('connect');
  const late = ui.makeMedia(); permission.resolve(late); await starting; await flush();
  assert.equal(late.track.stopped, true);
  assert.equal(ui.sockets.length, 0);
  assert.equal(ui.elements.get('connect').textContent, '开始说话');
});

test('backgrounding during worklet loading invalidates resources and prevents a late connection', async () => {
  const workletLoad = deferred(), ui = await browser({workletLoad});
  const starting = ui.click('connect'); await flush(); await ui.hide();
  workletLoad.resolve(); await starting; await flush();
  assert.equal(ui.media[0].track.stopped, true);
  assert.equal(ui.sockets.length, 0);
  assert.equal(ui.contexts[0].state, 'closed');
});

test('page close stops output and capture and ignores late socket messages', async () => {
  const ui = await browser(); await start(ui); await ui.message(audio(0, 1));
  await ui.pagehide();
  assert.equal(ui.media[0].track.stopped, true);
  assert.equal(ui.contexts[0].sources[0].stopped, true);
  await ui.message(ready, ui.sockets[0]);
  assert.equal(ui.captures[0].messages.filter(event => event.type === 'start').length, 1);
  assert.equal(ui.elements.get('connect').textContent, '开始说话');
});

test('the page exposes one action with automatic speech end and voice barge-in guidance', () => {
  assert.equal([...html.matchAll(/<button\b/g)].length, 1);
  assert.match(html, /开始说话/);
  assert.match(html, /自动.*(?:回答|回复)/);
  assert.match(html, /插话/);
  assert.doesNotMatch(html, /结束发送|立即停播|试听提示音|点击分轮/);
});

test('count progress shows confirmed checkpoint and clears when the conversation ends', async () => {
  const ui = await browser(); await start(ui);
  await ui.message({type: 'count.state', start: 1, end: 50, next: 16, state: 'paused'});
  assert.match(ui.elements.get('count').textContent, /暂停/);
  assert.match(ui.elements.get('count').textContent, /16/);
  assert.match(ui.elements.get('count').textContent, /50/);
  await ui.message({type: 'count.state', start: 1, end: 50, next: 51, state: 'completed'});
  assert.match(ui.elements.get('count').textContent, /完成.*50/);
  assert.doesNotMatch(ui.elements.get('count').textContent, /51/);
  await ui.click('connect');
  assert.match(ui.elements.get('count').textContent, /本次对话已结束/);
  assert.match(ui.elements.get('count').textContent, /50/);
  assert.doesNotMatch(ui.elements.get('count').textContent, /继续.*51/);
});

test('bounded event log keeps numeric route and playback evidence without conversation text', async () => {
  const ui = await browser(); await start(ui);
  await ui.message({type: 'turn.route', route: 'count.resume', source: 'direct', count: {start: 1, end: 50, next: 16, state: 'paused'}});
  assert.match(ui.elements.get('log').textContent, /count.resume.*16.*50/);
  await ui.message({type: 'transcript', text: 'synthetic-private-transcript', final: true});
  await ui.message({type: 'reply', text: 'synthetic-private-reply'});
  assert.doesNotMatch(ui.elements.get('log').textContent, /synthetic-private/);
});
