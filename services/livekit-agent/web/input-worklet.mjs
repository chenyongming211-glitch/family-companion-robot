import { CapturePCM } from './audio-core.mjs';

class MicrophoneCapture extends AudioWorkletProcessor {
  constructor() {
    super();
    this.pcm = new CapturePCM(sampleRate);
    this.active = false; this.meterSamples = 0;
    this.port.onmessage = ({data}) => {
      if (data.type === 'start') { this.pcm.clear(); this.active = true; }
      if (data.type === 'end' || data.type === 'cancel') {
        this.active = false;
        if (data.type === 'end') for (const frame of this.pcm.finish()) this.send(frame);
        this.pcm.clear();
        this.port.postMessage({type: 'stopped', request: data.request});
      }
    };
  }
  send(frame) { this.port.postMessage({type: 'pcm', buffer: frame.buffer}, [frame.buffer]); }
  process(inputs) {
    const input = inputs[0]?.[0];
    if (input) {
      this.meterSamples += input.length;
      if (this.meterSamples >= sampleRate / 10) {
        this.meterSamples = 0;
        this.port.postMessage({type: 'level', rms: Math.sqrt(input.reduce((n, s) => n + s * s, 0) / input.length)});
      }
      if (this.active) for (const frame of this.pcm.push(input)) this.send(frame);
    }
    return true;
  }
}
registerProcessor('microphone-capture', MicrophoneCapture);
