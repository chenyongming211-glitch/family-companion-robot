import { PlaybackLedger } from './audio-core.mjs';
import { AsyncResource, AttemptGate, aborted, serviceErrorMessage } from './lifecycle.mjs';

const $ = id => document.getElementById(id);
let ctx, stream, capture, ws, player, ready = false, recording = false, paired = false;
let inFrames = 0, outFrames = 0;
let sessionTimer, sessionUntil = 0, turnLimit = 8, usedTurns = 0;
let userState = 'away', agentState = 'idle';
const microphoneResource = new AsyncResource(), connection = new AttemptGate();
function log(value) { $('log').textContent = `${new Date().toLocaleTimeString()} ${value}\n${$('log').textContent}`.slice(0, 6000); }
function status(value) { $('status').textContent = value; }
function stats() { $('stats').textContent = `输入 ${inFrames} 帧 · 输出 ${outFrames} 帧`; }
function send(event) {
  if (ws?.readyState !== WebSocket.OPEN || ws.bufferedAmount > 65536) throw new Error('连接中断或发送队列已满，请重新连接');
  ws.send(JSON.stringify(event));
}
function controls() {
  const active = !!ws || connection.busy;
  $('connect').disabled = !paired;
  $('connect').textContent = active ? '结束对话' : '开始说话';
  $('connect').setAttribute?.('aria-pressed', String(active));
}
function voiceState() {
  const user = {speaking: '正在听你说话', listening: '麦克风持续收音', away: '麦克风已关闭'};
  const agent = {initializing: '助手准备中', idle: '助手待机', listening: '助手在听', thinking: '助手正在思考', speaking: '助手正在说话'};
  $('voice').textContent = `${user[userState] || '麦克风持续收音'} · ${agent[agentState] || '助手已连接'}`;
}
function sessionBudget() {
  const seconds = Math.max(0, Math.ceil((sessionUntil - performance.now()) / 1000));
  $('budget').textContent = `本次会话剩余 ${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')} · 已用 ${usedTurns}/${turnLimit} 轮`;
}
function clock() {
  const timestamp = ctx?.getOutputTimestamp?.();
  return ctx?.state === 'running' && timestamp?.performanceTime > 0 ? timestamp.contextTime : NaN;
}

class Speaker {
  constructor() {
    this.book = new PlaybackLedger(); this.nodes = new Set(); this.cursor = 0;
    this.timer = setInterval(() => this.settle(), 40);
  }
  frame(event) {
    if (event.epoch < this.book.floor) return;
    const binary = atob(event.data);
    if (binary.length !== event.samples * 2) throw new Error('输出音频长度不匹配');
    const buffer = ctx.createBuffer(1, event.samples, 24000), values = buffer.getChannelData(0);
    for (let i = 0; i < values.length; i++) { let s = binary.charCodeAt(i * 2) | binary.charCodeAt(i * 2 + 1) << 8; if (s >= 32768) s -= 65536; values[i] = s / 32768; }
    const start = Math.max(this.cursor, ctx.currentTime + .04);
    if (!this.book.add(event.block_id, event.epoch, event.samples, start)) return;
    const node = ctx.createBufferSource(); node.buffer = buffer; node.connect(ctx.destination); node.epoch = event.epoch;
    node.onended = () => { this.nodes.delete(node); node.disconnect(); };
    this.nodes.add(node); node.start(start); this.cursor = start + values.length / 24000;
    outFrames++; stats();
  }
  seal(event) {
    if (event.epoch < this.book.floor) return;
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
      // Report interruption positions only after the server retires the epoch.
      this.retired ??= new Map(); this.retired.set(id, receipt);
      this.book.blocks.delete(id);
    }
    if (nextEpoch !== null) {
      this.book.retire(nextEpoch);
      for (const [id, receipt] of this.retired ?? []) if (receipt.epoch < nextEpoch) { send(receipt); this.retired.delete(id); }
    }
    this.cursor = Math.max(ctx?.currentTime ?? 0, ...[...this.book.blocks.values()].map(block => block.chunks.at(-1).end));
    $('playback').textContent = `已执行本地停播（${(performance.now() - t).toFixed(1)}ms）；这不是扬声器实际停声测量。`;
  }
  close() { clearInterval(this.timer); this.stop(); this.retired?.clear(); }
}

async function ensureAudio() {
  if (!ctx || ctx.state === 'closed') ctx = new AudioContext({latencyHint: 'interactive'});
  const audioContext = ctx;
  try { await audioContext.resume(); }
  catch (error) { if (ctx !== audioContext) throw aborted(); throw error; }
  if (ctx !== audioContext || document.hidden) throw aborted();
  if (!stream) {
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
            if (data.type === 'pcm' && ready && recording) {
              try {
                const bytes = new Uint8Array(data.buffer);
                send({type: 'listen.audio', data: btoa(String.fromCharCode(...bytes))});
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
    } catch (error) {
      if (ctx !== audioContext || document.hidden || error.name === 'AbortError') throw aborted();
      releaseMicrophone(); throw error;
    }
  }
}
function releaseMicrophone() {
  microphoneResource.clear(); stream = null; capture = null;
  $('level').value = 0; $('device').textContent = '麦克风已释放';
}
function disconnect(message = '已待机，麦克风已释放') {
  connection.cancel();
  recording = false; ready = false;
  clearInterval(sessionTimer);
  player?.close(); player = null;
  const socket = ws; ws = null; socket?.close();
  releaseMicrophone(); ctx?.close(); ctx = null;
  userState = 'away'; agentState = 'idle'; voiceState();
  $('budget').textContent = '开始下一次对话后重新计时';
  status(message); controls();
}
function fail(error) { $('error').textContent = error.message || '操作失败'; log(`失败：${error.code || error.name || 'Error'}`); disconnect(); status(error.message || '操作失败'); }
const action = fn => async () => { $('error').textContent = ''; try { await fn(); } catch (error) { if (error.name !== 'AbortError') fail(error); } };
$('connect').onclick = action(async () => {
  if (ws || connection.busy) { disconnect(); return; }
  if (!paired) return;
  const token = connection.begin(); if (!token) return;
  controls(); status('正在准备麦克风，请允许使用…');
  try {
    await ensureAudio();
    if (!connection.current(token) || document.hidden) throw aborted();
    ws = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/v1/browser/audio?mode=continuous`);
    const socket = ws;
    player = new Speaker(); controls(); status('正在连接服务器…');
    ws.onmessage = event => {
      if (ws !== socket) return;
      try {
        const e = JSON.parse(event.data);
        if (e.type === 'ready') {
          if (ready) return;
          if (e.mode !== 'continuous' || e.input_rate !== 16000 || e.output_rate !== 24000) throw new Error('服务器连续语音协议不匹配，请更新服务后重试');
          ready = true; usedTurns = 0; turnLimit = e.max_turns || 8;
          sessionUntil = performance.now() + (e.session_seconds || 900) * 1000;
          clearInterval(sessionTimer); sessionBudget(); sessionTimer = setInterval(sessionBudget, 1000);
          recording = true; capture.port.postMessage({type: 'start'});
          userState = 'listening'; agentState = 'listening'; voiceState();
          status('对话已开始 · 说完会自动回复，可随时开口插话'); controls();
        }
        if (e.type === 'transcript') $('transcript').textContent = e.text + (e.final ? '' : ' …');
        if (e.type === 'reply') $('reply').textContent = e.text || '（回复已中断）';
        if (e.type === 'user.state') { userState = e.state; voiceState(); }
        if (e.type === 'agent.state') { agentState = e.state; voiceState(); }
        if (e.type === 'turn') { usedTurns = e.count; turnLimit = e.max_turns || turnLimit; sessionBudget(); }
        if (e.type === 'audio') player.frame(e);
        if (e.type === 'audio.end') player.seal(e);
        if (e.type === 'audio.cancel') player.stop(e.next_epoch);
        if (e.type === 'sleep') {
          const reason = {idle: '一段时间没有说话', turn_limit: '本次对话轮数已用完', utterance_limit: '本次发言时长已达上限'};
          disconnect(`${reason[e.code] || '本次对话已结束'}，已待机。点击“开始说话”可再次唤醒。`);
        }
        if (e.type === 'error') { const error = new Error(serviceErrorMessage(e)); error.code = e.code; throw error; }
      } catch (error) { fail(error); }
    };
    ws.onerror = () => { if (ws === socket) fail(new Error('连接失败，请重新运行配对入口；检查SSH隧道与服务器')); };
    ws.onclose = () => { if (ws === socket) disconnect(); };
  } catch (error) { if (!connection.current(token)) throw aborted(); throw error; }
  finally { connection.finish(token); controls(); }
});
document.addEventListener('visibilitychange', () => { if (document.hidden) disconnect(); });
window.addEventListener('pagehide', () => disconnect());

async function pair() {
  const ticket = new URLSearchParams(location.hash.slice(1)).get('pair');
  history.replaceState(null, '', '/client');
  if (!ticket) { $('pair').textContent = '请运行 tools/mac-voice/open.command，通过SSH取得一次性配对链接。'; return; }
  const response = await fetch('/v1/browser/pair', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({ticket})});
  if (!response.ok) throw new Error('配对链接已过期或已使用，请重新运行入口');
  const info = await response.json();
  paired = true; $('pair').textContent = `已配对：${Math.ceil(info.expires_in / 60)}分钟内有效。点击“开始说话”并允许麦克风。`; controls();
}
pair().catch(fail);
window.addEventListener('hashchange', () => pair().catch(fail));
