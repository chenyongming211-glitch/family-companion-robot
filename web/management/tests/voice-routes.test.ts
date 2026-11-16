import test from 'node:test';
import assert from 'node:assert/strict';
import { createVoiceRoutesController, voiceRoutesState } from '../src/voice-routes.ts';
import type { DeviceVoiceRoute, VoiceRouteReceipt } from '../src/types.ts';

const dto = (extra: Partial<DeviceVoiceRoute> = {}): DeviceVoiceRoute => ({deviceId: 'd', selectedRoute: 'C', activeRoute: null, connection: 'idle', version: 'a'.repeat(64), canSwitch: true, ...extra});
const receipt = (extra: Partial<VoiceRouteReceipt> = {}): VoiceRouteReceipt => ({...dto({selectedRoute: 'A', version: 'b'.repeat(64)}), disconnectConfirmed: true, ...extra});
const deferred = <T>() => {let resolve!: (value: T) => void; const promise = new Promise<T>(r => resolve = r); return {resolve, promise};};
const tick = () => new Promise(r => setImmediate(r));
function setup() {
  let read = async (_id: string, _signal?: AbortSignal): Promise<DeviceVoiceRoute> => dto();
  let write = async (): Promise<VoiceRouteReceipt> => receipt();
  let expire = () => {}, now = 0;
  const calls: {method: string; args: unknown[]}[] = [], state = voiceRoutesState();
  const c = createVoiceRoutesController({
    getDeviceVoiceRoute: async (...args) => {calls.push({method: 'GET', args}); return read(...args);},
    setDeviceVoiceRoute: async (...args) => {calls.push({method: 'PUT', args}); return write();},
  }, state, {after: fn => {expire = fn; return 1;}, cancel: () => {}, now: () => now});
  return {c, state, calls, setRead: (fn: typeof read) => read = fn, setWrite: (fn: typeof write) => write = fn, expire: () => expire(), clock: (time: number) => now = time};
}
test('one explicit click issues PUT then GET and only matching confirmed idle readback reports success', async () => {
  const p = setup(); await p.c.start('d');
  const wait = deferred<DeviceVoiceRoute>(); p.setRead(() => wait.promise);
  const pending = p.c.select('A'); await tick(); await p.c.select('B'); await p.c.refresh();
  assert.deepEqual(p.calls.map(c => c.method), ['GET', 'PUT', 'GET']);
  assert.deepEqual(p.calls[1]!.args.slice(0, 3), ['d', 'A', 'a'.repeat(64)]);
  assert.equal(p.state.pending, true); assert.doesNotMatch(p.state.feedback, /已切换/);
  wait.resolve(dto({selectedRoute: 'A', version: 'b'.repeat(64)})); await pending;
  assert.match(p.state.feedback, /已切换，请重新唤醒/); assert.equal(p.state.needsRead, false);
});
test('disconnect not confirmed, unknown or old actual connection never reports success', async () => {
  for (const failure of ['unconfirmed', 'unknown', 'put-unknown', 'old-active', 'old-connecting']) {
    const p = setup(); await p.c.start('d');
    p.setWrite(async () => receipt({disconnectConfirmed: failure !== 'unconfirmed', connection: failure === 'put-unknown' ? 'unknown' : 'idle'}));
    p.setRead(async () => dto({selectedRoute: 'A', version: 'b'.repeat(64), connection: failure === 'unknown' ? 'unknown' : failure === 'old-active' ? 'active' : failure === 'old-connecting' ? 'connecting' : 'idle', activeRoute: failure.startsWith('old-') ? 'C' : null}));
    await p.c.select('A'); assert.doesNotMatch(p.state.feedback, /已切换/); assert.match(p.state.feedback, /尚未确认|未知/);
    assert.equal(p.state.view?.selectedRoute, 'A');
  }
});
test('current route may be clicked explicitly to end an old connection; readonly and unread states block writes', async () => {
  const p = setup(); p.setRead(async () => dto({canSwitch: false})); await p.c.start('d'); await p.c.select('A');
  assert.equal(p.calls.length, 1); p.setRead(async () => dto({activeRoute: 'B', connection: 'active'})); await p.c.refresh();
  p.setWrite(async () => receipt({selectedRoute: 'C'})); p.setRead(async () => dto({version: 'b'.repeat(64)}));
  await p.c.select('C'); assert.equal(p.calls.filter(c => c.method === 'PUT').length, 1);
});
test('write/read failure, identity/version/selected mismatch and revoked permission require manual refresh', async () => {
  for (const failure of ['write', 'read', 'put-id', 'put-route', 'get-id', 'get-version', 'get-route', 'revoked']) {
    const p = setup(); await p.c.start('d');
    p.setWrite(async () => {if (failure === 'write') throw new Error('HTTP 409'); return receipt({deviceId: failure === 'put-id' ? 'other' : 'd', selectedRoute: failure === 'put-route' ? 'B' : 'A'});});
    p.setRead(async () => {if (failure === 'read') throw new Error('断线'); return dto({deviceId: failure === 'get-id' ? 'other' : 'd', version: (failure === 'get-version' ? 'c' : 'b').repeat(64), selectedRoute: failure === 'get-route' ? 'B' : 'A', canSwitch: failure !== 'revoked'});});
    await p.c.select('A'); await p.c.select('A');
    assert.equal(p.state.needsRead, true, failure); assert.equal(p.state.view, null, failure); assert.doesNotMatch(p.state.feedback, /已切换/, failure);
    assert.equal(p.calls.filter(c => c.method === 'PUT').length, 1, failure);
    p.setRead(async () => dto()); await p.c.refresh(); assert.equal(p.state.needsRead, false);
  }
});
test('timeouts abort pending requests and reject late callbacks even before timer runs', async () => {
  for (const phase of ['GET', 'PUT', 'readback', 'late-clock']) {
    const p = setup(), wait = deferred<any>();
    if (phase === 'GET') p.setRead(() => wait.promise); else {await p.c.start('d'); if (phase === 'readback') p.setRead(() => wait.promise); else p.setWrite(() => wait.promise);}
    const pending = phase === 'GET' ? p.c.start('d') : p.c.select('A'); await tick();
    const signal = p.calls.at(-1)!.args.at(-1) as AbortSignal;
    if (phase === 'late-clock') {p.clock(30000); wait.resolve(receipt());} else p.expire();
    await pending; assert.equal(signal.aborted, true); assert.equal(p.state.needsRead, true); assert.equal(p.state.pending, false);
    wait.resolve(phase === 'GET' ? dto() : receipt()); await tick(); assert.equal(p.state.view, null); assert.match(p.state.feedback, /未确认/);
  }
});
test('stop clears sensitive data, aborts operations and ignores old device completions', async () => {
  const p = setup(); await p.c.start('d'); const wait = deferred<VoiceRouteReceipt>(); p.setWrite(() => wait.promise);
  const pending = p.c.select('A'); const signal = p.calls.at(-1)!.args.at(-1) as AbortSignal;
  p.c.stop(); assert.equal(signal.aborted, true); assert.deepEqual(p.state, voiceRoutesState());
  p.setRead(async id => dto({deviceId: id, selectedRoute: 'B'})); await p.c.start('next'); wait.resolve(receipt()); await pending;
  assert.equal(p.state.view?.deviceId, 'next'); assert.equal(p.state.view?.selectedRoute, 'B'); assert.equal(p.state.feedback, '');
});
