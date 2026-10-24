export class CapturePCM {
  constructor(rate) {
    if (!Number.isFinite(rate) || rate < 16000) throw new Error('unsupported capture rate');
    this.ratio = rate / 16000;
    this.clear();
  }
  clear() { this.left = this.ratio; this.sum = 0; this.samples = []; }
  packet() {
    const data = new Uint8Array(this.samples.length * 2);
    const view = new DataView(data.buffer);
    this.samples.forEach((x, i) => view.setInt16(i * 2, Math.round(Math.max(-1, Math.min(1, x)) * (x < 0 ? 32768 : 32767)), true));
    this.samples = [];
    return data;
  }
  push(input) {
    const packets = [];
    for (const value of input) {
      let remaining = 1;
      while (remaining > 1e-9) {
        const take = Math.min(remaining, this.left);
        this.sum += value * take;
        this.left -= take;
        remaining -= take;
        if (this.left < 1e-9) {
          this.samples.push(this.sum / this.ratio);
          this.left = this.ratio; this.sum = 0;
          if (this.samples.length === 320) packets.push(this.packet());
        }
      }
    }
    return packets;
  }
  finish() {
    const result = this.samples.length ? [this.packet()] : [];
    this.clear();
    return result;
  }
}

export class PlaybackLedger {
  constructor({maxPendingSamples = 24000 * 120} = {}) { this.blocks = new Map(); this.floor = 0; this.maxPendingSamples = maxPendingSamples; }
  add(id, epoch, samples, start) {
    if (![id, epoch, samples].every(Number.isSafeInteger) || id < 1 || epoch < 0 || samples < 1 || samples > 480 || !Number.isFinite(start) || start < 0) throw new Error('invalid output frame');
    if (epoch < this.floor) return false;
    let block = this.blocks.get(id);
    if (!block) {
      if (this.blocks.size >= 32) throw new Error('playback segment limit');
      block = {id, epoch, samples: 0, played: 0, chunks: [], sealed: false};
      this.blocks.set(id, block);
    }
    if (block.epoch !== epoch || block.sealed) throw new Error('output identity mismatch');
    const pending = [...this.blocks.values()].reduce((n, b) => n + b.samples - b.played, 0);
    if (pending + samples > this.maxPendingSamples) throw new Error('播放缓冲已满，本轮已停止；请使用更短的问题');
    block.chunks.push({start, end: start + samples / 24000, samples});
    block.samples += samples;
    return true;
  }
  seal(id, epoch, samples) {
    const block = this.blocks.get(id);
    if (!block || block.epoch !== epoch || samples !== block.samples) throw new Error('output seal mismatch');
    block.sealed = true;
  }
  receipt(id, outputTime) {
    const b = this.blocks.get(id);
    if (!b) throw new Error('unknown output segment');
    const valid = Number.isFinite(outputTime) && outputTime > 0;
    const played = valid ? b.chunks.reduce((n, c) => n + Math.min(c.samples, Math.max(0, Math.floor((outputTime - c.start) * 24000 + 1e-6))), 0) : 0;
    if (valid) b.played = Math.max(b.played, played);
    return {type: 'playback', block_id: b.id, epoch: b.epoch, played_samples: played, valid, finished: valid && b.sealed && played === b.samples, evidence: 'browser_output_clock_estimate'};
  }
  retire(nextEpoch) { this.floor = Math.max(this.floor, nextEpoch); }
}
