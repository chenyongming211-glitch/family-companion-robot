import { PlaybackLedger } from './audio-core.mjs';
import { AsyncResource, AttemptGate, aborted, serviceErrorMessage } from './lifecycle.mjs';

const $ = id => document.getElementById(id);
let ctx, stream, capture, ws, player, ready = false, recording = false, inputId = null, paired = false;
let request = 0, inFrames = 0, outFrames = 0, waitInput, recordTimer, releaseTimer;
let sessionTimer, sessionUntil = 0, turnLimit = 8, usedTurns = 0;
const stops = new Map();
const microphoneResource = new AsyncResource(), connection = new AttemptGate(), inputOps = new AttemptGate();
function log(value) { $('log').textContent = `${new Date().toLocaleTimeString()} ${value}\n${$('log').textContent}`.slice(0, 6000); }
function status(value) { $('status').textContent = value; }
function stats() { $('stats').textContent = `输入 ${inFrames} 帧 · 输出 ${outFrames} 帧`; }
function send(event) {
  if (ws?.readyState !== WebSocket.OPEN || ws.bufferedAmount > 65536) throw new Error('连接中断或发送队列已满，请重新连接');
  ws.send(JSON.stringify(event));
}
function controls() {
  $('connect').disabled = !paired || !!ws || connection.busy;
  $('disconnect').disabled = !ws;
  $('start').disabled = !ready || recording || !!waitInput || inputOps.busy || usedTurns >= turnLimit;
  $('end').disabled = !recording || inputOps.busy;
  $('cancel').disabled = !ready;
}
function sessionBudget() {
  const seconds = Math.max(0, Math.ceil((sessionUntil - performance.now()) / 1000));
  $('budget').textContent = `本次会话剩余 ${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')} · 已用 ${usedTurns}/${turnLimit} 轮${usedTurns >= turnLimit ? '，请断开后重新连接' : ''}`;
}
function clock() {
  const timestamp = ctx?.getOutputTimestamp?.();
  return ctx?.state === 'running' && timestamp?.performanceTime > 0 ? timestamp.contextTime : NaN;
}

class Speaker {
  constructor() {
    this.book = new PlaybackLedger(); this.nodes = new Set(); this.cursor = 0; this.muted = false;
    this.timer = setInterval(() => this.settle(), 40);
  }
  frame(event) {
    if (this.muted || event.epoch < this.book.floor) return;
    const binary = atob(event.data);
    if (binary.length !== event.samples * 2) throw new Error('输出音频长度不匹配');
    const buffer = ctx.createBuffer(1, event.samples, 24000), values = buffer.getChannelData(0);
    for (let i = 0; i < values.length; i++) { let s = binary.charCodeAt(i * 2) | binary.charCodeAt(i * 2 + 1) << 8; if (s >= 32768) s -= 65536; values[i] = s / 32768; }
    const start = Math.max(this.cursor, ctx.currentTime + .04);
    if (!this.book.add(event.block_id, event.epoch, event.samples, start)) return;
    const node = ctx.createBufferSource(); node.buffer = buffer; node.connect(ctx.destination); node.epoch = event.epoch;
    node.onended = () => { this.nodes.delete(node); node.disconnect(); };
    this.nodes.add(node); node.start(start); this.cursor = start + values.length / 24000;
    outFrames++; stats(); status('正在播放回复');
  }
  seal(event) {
    if (this.muted || event.epoch < this.book.floor) return;
    this.book.seal(event.block_id, event.epoch, event.sent_samples);
    this.settle();
  }
  settle() {
    if (!ctx || !ws || ws.readyState !== WebSocket.OPEN) return;
    try {
      for (const [id, block] of this.book.blocks) {
        const receipt = this.book.receipt(id, clock());
        const end = block.chunks.at(-1).end;
        if (receipt.finished || (block.sealed && ctx.currentTime > end + 1)) {
          if (!receipt.finished) { receipt.valid = false; receipt.finished = true; }
          send(receipt); this.book.blocks.delete(id);
          $('playback').textContent = receipt.valid ? `浏览器输出时钟估算：${receipt.played_samples} 个采样已输出；物理听感待确认。` : '输出时钟不可用，本段播放位置 unknown。';
        }
      }
    } catch (error) { fail(error); }
  }
  stop(nextEpoch = null) {
    const t = performance.now(), outputTime = clock();
    for (const node of this.nodes) if (nextEpoch === null || node.epoch < nextEpoch) { node.stop(); node.disconnect(); this.nodes.delete(node); }
    for (const [id, block] of this.book.blocks) if (nextEpoch === null || block.epoch < nextEpoch) {
      const receipt = this.book.receipt(id, outputTime);
      receipt.finished = true;
      // Send interruption receipts after the server seals/retire signal. A
      // local click can precede server interruption, so retain the observation.
      this.retired ??= new Map(); this.retired.set(id, receipt);
      this.book.blocks.delete(id);
    }
    if (nextEpoch !== null) {
      this.book.retire(nextEpoch);
      for (const [id, receipt] of this.retired ?? []) if (receipt.epoch < nextEpoch) { send(receipt); this.retired.delete(id); }
    }
    this.cursor = ctx?.currentTime ?? 0;
    $('playback').textContent = `已执行本地停播（${(performance.now() - t).toFixed(1)}ms）；这不是扬声器实际停声测量。`;
  }
  close() { clearInterval(this.timer); this.stop(); this.retired?.clear(); }
}

async function ensureAudio(microphone = true) {
  if (!ctx || ctx.state === 'closed') ctx = new AudioContext({latencyHint: 'interactive'});
  const audioContext = ctx;
  try { await audioContext.resume(); }
  catch (error) { if (ctx !== audioContext) throw aborted(); throw error; }
  if (ctx !== audioContext || document.hidden) throw aborted();
  if (microphone && !stream) {
    try {
      const resource = await microphoneResource.acquire(async () => {
        const media = await navigator.mediaDevices.getUserMedia({audio: {channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true}, video: false});
        let node;
        try {
          if (ctx !== audioContext || document.hidden) throw aborted();
          await audioContext.audioWorklet.addModule('/client/input-worklet.mjs');
          if (ctx !== audioContext || document.hidden) throw aborted();
          node = new AudioWorkletNode(audioContext, 'microphone-capture', {channelCount: 1});
          node.port.onmessage = ({data}) => {
            if (capture !== node) return;
            if (data.type === 'level') $('level').value = Math.min(1, data.rms * 5);
            if (data.type === 'stopped') { stops.get(data.request)?.(); stops.delete(data.request); }
            if (data.type === 'pcm' && recording) {
              try {
                const bytes = new Uint8Array(data.buffer);
                send({type: 'input.audio', input_id: inputId, data: btoa(String.fromCharCode(...bytes))});
                inFrames++; stats();
              } catch (error) { fail(error); }
            }
          };
          const mute = audioContext.createGain(); mute.gain.value = 0;
          audioContext.createMediaStreamSource(media).connect(node).connect(mute).connect(audioContext.destination);
          return {stream: media, capture: node};
        } catch (error) { media.getTracks().forEach(track => track.stop()); node?.disconnect(); throw error; }
      }, resource => { resource.stream.getTracks().forEach(track => track.stop()); resource.capture.disconnect(); });
      if (ctx !== audioContext || document.hidden) throw aborted();
      stream = resource.stream; capture = resource.capture;
      const s = stream.getAudioTracks()[0].getSettings();
      $('device').textContent = `麦克风已就绪 · 音频时钟 ${ctx.sampleRate}Hz → 16kHz · 浏览器回声消除 ${s.echoCancellation === true ? '已开启' : '未确认'} · 电平随声音变化`;
      clearTimeout(releaseTimer);
      releaseTimer = setTimeout(() => { if (!ws) releaseMicrophone(); }, 60000);
    } catch (error) {
      if (ctx !== audioContext || document.hidden || error.name === 'AbortError') throw aborted();
      releaseMicrophone(); throw error;
    }
  }
}
function releaseMicrophone() {
  microphoneResource.clear(); stream = null; capture = null;
  for (const resolve of stops.values()) resolve();
  stops.clear();
  $('level').value = 0; $('device').textContent = '麦克风已释放';
}
async function stopCapture(kind) {
  clearTimeout(recordTimer);
  if (!capture) { recording = false; return; }
  const id = ++request;
  await Promise.race([new Promise(resolve => { stops.set(id, resolve); capture.port.postMessage({type: kind, request: id}); }), new Promise((_, reject) => setTimeout(() => reject(new Error('麦克风结束超时')), 1000))]);
  recording = false; stops.delete(id); controls();
}
function disconnect() {
  connection.cancel(); inputOps.cancel();
  recording = false; ready = false; waitInput?.reject(aborted()); waitInput = null;
  clearTimeout(recordTimer); clearTimeout(releaseTimer);
  clearInterval(sessionTimer);
  player?.close(); player = null;
  const socket = ws; ws = null; socket?.close();
  releaseMicrophone(); ctx?.close(); ctx = null;
  $('budget').textContent = '本次连接已结束，重新连接后重新计时';
  status('已断开，麦克风已释放'); controls();
}
function fail(error) { $('error').textContent = error.message || '操作失败'; log(`失败：${error.code || error.name || 'Error'}`); disconnect(); status(error.message || '操作失败'); }
const action = fn => async () => { $('error').textContent = ''; try { await fn(); } catch (error) { if (error.name !== 'AbortError') fail(error); } };
async function inputAction(fn, cancel = false) {
  if (cancel) { inputOps.cancel(); waitInput?.reject(aborted()); waitInput = null; }
  const token = inputOps.begin();
  if (!token) return;
  controls();
  try { await fn(() => inputOps.current(token)); }
  catch (error) { if (!inputOps.current(token)) throw aborted(); throw error; }
  finally { inputOps.finish(token); controls(); }
}

$('mic').onclick = action(() => ensureAudio());
$('tone').onclick = action(async () => {
  await ensureAudio(false);
  const tone = ctx.createOscillator(), gain = ctx.createGain(); gain.gain.value = .04;
  tone.frequency.value = 440; tone.connect(gain).connect(ctx.destination); tone.start(); tone.stop(ctx.currentTime + .3);
  log('已输出0.3秒提示音，请确认能听到');
});
$('release').onclick = disconnect;
$('connect').onclick = action(async () => {
  const token = connection.begin(); if (!token) return;
  controls();
  try { await ensureAudio(); }
  catch (error) { if (!connection.current(token)) throw aborted(); throw error; }
  if (!connection.current(token) || document.hidden) throw aborted();
  ws = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/v1/browser/audio`);
  const socket = ws;
  player = new Speaker(); controls(); status('正在连接服务器…');
  ws.onmessage = event => {
    if (ws !== socket) return;
    try {
      const e = JSON.parse(event.data);
      if (e.type === 'ready') {
        ready = true; usedTurns = 0; turnLimit = e.max_turns || 8;
        sessionUntil = performance.now() + (e.session_seconds || 900) * 1000;
        clearInterval(sessionTimer); sessionBudget(); sessionTimer = setInterval(sessionBudget, 1000);
        status('已连接，可以开始说话'); controls();
      }
      if (e.type === 'input.ready') { inputId = e.input_id; usedTurns = inputId; sessionBudget(); player.muted = false; waitInput?.resolve(); }
      if (e.type === 'transcript') $('transcript').textContent = e.text + (e.final ? '' : ' …');
      if (e.type === 'reply') { $('reply').textContent = e.text || '（回复已中断）'; if (!recording) status(e.interrupted ? '回复已中断' : '本轮结束，可以继续提问'); }
      if (e.type === 'audio') player.frame(e);
      if (e.type === 'audio.end') player.seal(e);
      if (e.type === 'audio.cancel') player.stop(e.next_epoch);
      if (e.type === 'cancelled') { status('已确认停播；下一问题请点“开始说话”，说完点“结束发送”'); log('服务器取消确认'); }
      if (e.type === 'error') { const error = new Error(serviceErrorMessage(e)); error.code = e.code; throw error; }
    } catch (error) { fail(error); }
  };
  ws.onerror = () => { if (ws === socket) fail(new Error('连接失败，请重新运行配对入口；检查SSH隧道与服务器')); };
  ws.onclose = () => { if (ws === socket) disconnect(); };
});
$('disconnect').onclick = disconnect;
$('start').onclick = action(() => inputAction(async current => {
  player.muted = true; player.stop();
  await stopCapture('cancel');
  if (!current()) throw aborted();
  let turnWait;
  const waiting = new Promise((resolve, reject) => { turnWait = waitInput = {resolve, reject}; });
  controls(); send({type: 'input.start'});
  try { await Promise.race([waiting, new Promise((_, reject) => setTimeout(() => reject(new Error('输入准备超时')), 5000))]); }
  finally { if (waitInput === turnWait) waitInput = null; }
  if (!current()) throw aborted();
  recording = true; capture.port.postMessage({type: 'start'}); controls(); status('正在收音，说完点击“结束发送”');
  $('transcript').textContent = '正在听…'; $('reply').textContent = '等待回答';
  recordTimer = setTimeout(() => $('end').click(), 25000);
}));
$('end').onclick = action(() => inputAction(async current => {
  await stopCapture('end'); if (!current()) throw aborted();
  send({type: 'input.end', input_id: inputId}); status('已发送，等待识别和回答');
}));
$('cancel').onclick = action(async () => {
  player.muted = true; player.stop();
  await inputAction(async current => {
    await stopCapture('cancel'); if (!current()) throw aborted();
    send({type: 'cancel'}); status('本地已停播，正在通知服务器取消');
  }, true);
});
document.addEventListener('visibilitychange', () => { if (document.hidden) disconnect(); });
window.addEventListener('pagehide', disconnect);

async function pair() {
  const ticket = new URLSearchParams(location.hash.slice(1)).get('pair');
  history.replaceState(null, '', '/client');
  if (!ticket) { $('pair').textContent = '请运行 tools/mac-voice/open.command，通过SSH取得一次性配对链接。'; return; }
  const response = await fetch('/v1/browser/pair', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({ticket})});
  if (!response.ok) throw new Error('配对链接已过期或已使用，请重新运行入口');
  const info = await response.json();
  paired = true; $('pair').textContent = `已配对：${Math.ceil(info.expires_in / 60)}分钟内有效。点击连接后允许麦克风。`; controls();
}
pair().catch(fail);
window.addEventListener('hashchange', () => pair().catch(fail));
