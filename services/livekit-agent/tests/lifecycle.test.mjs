import test from 'node:test';
import assert from 'node:assert/strict';
import { AsyncResource, AttemptGate } from '../web/lifecycle.mjs';

test('concurrent microphone requests share one acquisition', async () => {
  const gate = new AsyncResource(); let calls = 0, release;
  const acquire = () => { calls++; return new Promise(resolve => { release = resolve; }); };
  const first = gate.acquire(acquire, () => {}), second = gate.acquire(acquire, () => {});
  await Promise.resolve();
  release('one microphone');
  assert.equal(await first, 'one microphone');
  assert.equal(await second, 'one microphone');
  assert.equal(calls, 1);
});

test('release during permission wait disposes a late result and permits a fresh capture', async () => {
  const gate = new AsyncResource(); let release; const disposed = [];
  const late = gate.acquire(() => new Promise(resolve => { release = resolve; }), r => disposed.push(r));
  await Promise.resolve(); gate.clear();
  assert.equal(await gate.acquire(async () => 'new', r => disposed.push(r)), 'new');
  release('old');
  await assert.rejects(late, {name: 'AbortError'});
  assert.deepEqual(disposed, ['old']);
  gate.clear(); assert.deepEqual(disposed, ['old', 'new']);
});

test('a pending connection is exclusive and cancellation invalidates its continuation', () => {
  const gate = new AttemptGate(), first = gate.begin();
  assert.ok(first); assert.equal(gate.begin(), null);
  gate.cancel(); const next = gate.begin();
  assert.equal(gate.current(first), false); assert.equal(gate.current(next), true);
  gate.finish(first); assert.equal(gate.current(next), true);
  gate.finish(next); assert.equal(gate.busy, false);
});
