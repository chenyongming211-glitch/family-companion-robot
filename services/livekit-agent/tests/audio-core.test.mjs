import test from 'node:test';
import assert from 'node:assert/strict';
import { CapturePCM, PlaybackLedger } from '../web/audio-core.mjs';

test('48k and 44.1k input preserve sample count across arbitrary chunk boundaries', () => {
  for (const rate of [48000, 44100, 16000]) {
    const capture = new CapturePCM(rate);
    const frames = [];
    const input = new Float32Array(rate).fill(.25);
    for (let i = 0; i < input.length; i += 127) frames.push(...capture.push(input.subarray(i, i + 127)));
    frames.push(...capture.finish());
    assert.equal(frames.reduce((n, f) => n + f.byteLength / 2, 0), 16000);
    assert.ok(frames.every(f => f.byteLength <= 640 && f.byteLength % 2 === 0));
    assert.equal(new DataView(frames[0].buffer).getInt16(0, true), 8192);
  }
});

test('cancellation drops pending input; next capture starts without old samples', () => {
  const c = new CapturePCM(48000);
  c.push(new Float32Array(200).fill(1));
  c.clear();
  assert.equal(c.finish().length, 0);
  const parts = c.push(new Float32Array(960).fill(-1));
  assert.equal(parts.length, 1);
  assert.equal(new DataView(parts[0].buffer).getInt16(0, true), -32768);
});

test('output clock estimates only submitted audio, not gaps or queued future data', () => {
  const book = new PlaybackLedger();
  book.add(1, 0, 480, 1);
  book.add(1, 0, 480, 2);
  book.seal(1, 0, 960);
  assert.equal(book.receipt(1, 1.5).played_samples, 480);
  assert.equal(book.receipt(1, 2.01).played_samples, 720);
  assert.equal(book.receipt(1, 2.01).finished, false);
  assert.equal(book.receipt(1, 2.03).finished, true);
});

test('retired output cannot reappear; output size and identities are bounded', () => {
  const book = new PlaybackLedger();
  book.add(1, 3, 480, 1);
  book.retire(4);
  assert.equal(book.add(1, 3, 480, 2), false);
  assert.equal(book.add(2, 4, 480, 2), true);
  assert.throws(() => book.add(2, 4, -1, 3));
  assert.throws(() => book.add(3, 4, 24000 * 13, 3));
  assert.equal(book.receipt(2, NaN).valid, false);
});

test('output seal requires the actual received sample count', () => {
  const book = new PlaybackLedger();
  book.add(1, 0, 480, 1);
  assert.throws(() => book.seal(1, 0, 960));
  book.seal(1, 0, 480);
  assert.equal(book.receipt(1, .9).finished, false);
  assert.equal(book.receipt(1, 2).played_samples, 480);
});

test('a long segment playing in real time does not consume the backlog limit', () => {
  const book = new PlaybackLedger();
  for (let i = 0; i < 800; i++) {
    book.add(1, 0, 480, 1 + i * .02);
    book.receipt(1, 1 + (i + 1) * .02);
  }
  book.seal(1, 0, 800 * 480);
  assert.equal(book.receipt(1, 18).finished, true);
});

test('twelve seconds of actual pending audio is still bounded', () => {
  const book = new PlaybackLedger({maxPendingSamples: 24000 * 12});
  for (let i = 0; i < 600; i++) book.add(1, 0, 480, 1 + i * .02);
  assert.throws(() => book.add(1, 0, 480, 13));
  book.receipt(1, 1.02);
  assert.equal(book.add(1, 0, 480, 13), true);
});

test('the default queue accepts a fast twenty-second TTS burst within the server limit', () => {
  const book = new PlaybackLedger();
  for (let i = 0; i < 1000; i++) book.add(1, 0, 480, 1 + i * .02);
  book.seal(1, 0, 480000);
  assert.equal(book.receipt(1, 22).finished, true);
});
