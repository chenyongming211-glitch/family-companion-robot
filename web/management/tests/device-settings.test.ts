import test from 'node:test';
import assert from 'node:assert/strict';
import { createDeviceSettingsController, deviceSettingsState } from '../src/device-settings.ts';
import type { DeviceSettings } from '../src/types.ts';

const dto = (extra: Partial<DeviceSettings> = {}): DeviceSettings => ({ deviceId: 'd', name: '伴伴', location: '客厅', version: 'a'.repeat(64), canEdit: true, ...extra });
const deferred = () => { let resolve!: (x: DeviceSettings) => void; const promise = new Promise<DeviceSettings>(r => { resolve = r; }); return { promise, resolve }; };
function setup() {
  let read: () => Promise<DeviceSettings> = async () => dto();
  let write: () => Promise<DeviceSettings> = async () => dto({ name: '新名', version: 'b'.repeat(64) });
  let expire = () => {}, delay = 0, now = 0;
  const calls: Array<{ method: string; args: unknown[] }> = [];
  const saved: DeviceSettings[] = [];
  const state = deviceSettingsState();
  const c = createDeviceSettingsController({
    getDeviceSettings: async (...args) => { calls.push({ method: 'GET', args }); return read(); },
    updateDeviceSettings: async (...args) => { calls.push({ method: 'PUT', args }); return write(); },
  }, state, value => { saved.push(value); }, {
    after: (fn, ms) => { expire = fn; delay = ms; return 1; }, cancel: () => {}, now: () => now,
  });
  return { c, state, calls, saved, setRead: (fn: typeof read) => { read = fn; }, setWrite: (fn: typeof write) => { write = fn; }, expire: () => expire(), delay: () => delay, clock: (value: number) => { now = value; } };
}
test('settings save uses exactly one PUT then matching GET and emits only after confirmation', async () => {
  const p = setup(); await p.c.start('d', 'family'); p.state.name = ' 新名 ';
  const wait = deferred(); p.setRead(() => wait.promise);
  const saving = p.c.submit(); await new Promise(r => setImmediate(r));
  await p.c.submit(); await p.c.refresh();
  assert.deepEqual(p.calls.map(x => x.method), ['GET', 'PUT', 'GET']);
  assert.deepEqual(p.calls[1]?.args, ['d', { name: '新名', location: '客厅', version: 'a'.repeat(64) }, 'family']);
  assert.equal(p.saved.length, 0); assert.equal(p.state.pending, true);
  wait.resolve(dto({ name: '新名', version: 'b'.repeat(64) })); await saving;
  assert.equal(p.saved.length, 1); assert.match(p.state.feedback, /已保存/);
});
test('read-only, revoked, failed reads and invalid code-point lengths block writes', async () => {
  const p = setup(); p.setRead(async () => dto({ canEdit: false })); await p.c.start('d', 'admin'); await p.c.submit();
  p.setRead(async () => { throw new Error('读取失败'); }); await p.c.refresh(); await p.c.submit();
  assert.equal(p.calls.filter(x => x.method === 'PUT').length, 0);
  p.setRead(async () => dto()); await p.c.refresh();
  for (const [name, location] of [['', ''], ['😀'.repeat(13), ''], ['正常', '😀'.repeat(25)]]) {
    p.state.name = name!; p.state.location = location!; await p.c.submit();
  }
  assert.equal(p.calls.filter(x => x.method === 'PUT').length, 0);
  p.state.name = '😀'.repeat(12); p.state.location = '😀'.repeat(24);
  p.setWrite(async () => dto({ name: p.state.name, location: p.state.location, version: 'b'.repeat(64) }));
  p.setRead(async () => dto({ name: p.state.name, location: p.state.location, version: 'b'.repeat(64) }));
  await p.c.submit(); assert.equal(p.saved.length, 1);
});
test('mismatched PUT or final GET, version conflicts and failed writes require fresh read', async () => {
  for (const failure of ['put-id', 'put-value', 'get-version', 'get-value', 'get-revoked', 'conflict', 'network', 'read-failure']) {
    const p = setup(); await p.c.start('d', 'admin'); p.state.name = '新名';
    p.setWrite(async () => {
      if (failure === 'conflict' || failure === 'network') throw new Error(failure);
      return dto({ deviceId: failure === 'put-id' ? 'other' : 'd', name: failure === 'put-value' ? '别名' : '新名', version: 'b'.repeat(64) });
    });
    p.setRead(async () => {
      if (failure === 'read-failure') throw new Error('读取失败');
      return dto({ name: failure === 'get-value' ? '旧名' : '新名', version: failure === 'get-version' ? 'c'.repeat(64) : 'b'.repeat(64), canEdit: failure !== 'get-revoked' });
    });
    await p.c.submit(); await p.c.submit();
    assert.equal(p.saved.length, 0, failure); assert.equal(p.state.needsRead, true, failure);
    assert.equal(p.calls.filter(x => x.method === 'PUT').length, 1, failure);
    p.setRead(async () => dto()); await p.c.refresh(); assert.equal(p.state.needsRead, false, failure);
  }
});
test('30s PUT and confirmation GET timeouts ignore late results and require explicit fresh read', async () => {
  for (const stage of ['PUT', 'GET']) {
    const p = setup(); await p.c.start('d', 'family'); p.state.name = '新名'; const wait = deferred();
    if (stage === 'PUT') p.setWrite(() => wait.promise); else p.setRead(() => wait.promise);
    const saving = p.c.submit(); await new Promise(r => setImmediate(r));
    assert.equal(p.delay(), 30000); p.expire(); await saving;
    assert.equal(p.state.pending, false); assert.equal(p.state.needsRead, true); assert.match(p.state.feedback, /未确认/);
    await p.c.submit(); wait.resolve(dto({ name: '新名', version: 'b'.repeat(64) })); await new Promise(r => setImmediate(r));
    assert.equal(p.saved.length, 0); assert.doesNotMatch(p.state.feedback, /已保存/);
    assert.equal(p.calls.filter(x => x.method === 'PUT').length, 1);
    p.setRead(async () => dto({ name: '新名', version: 'b'.repeat(64) })); await p.c.refresh();
    assert.equal(p.state.needsRead, false); assert.equal(p.state.name, '新名');
  }
});
test('hanging initial read times out, then ignores its late response', async () => {
  const p = setup(), wait = deferred(); p.setRead(() => wait.promise); const reading = p.c.start('d', 'admin'); p.expire(); await reading;
  wait.resolve(dto()); await new Promise(r => setImmediate(r)); assert.equal(p.state.settings, null); assert.equal(p.state.name, '');
});
test('hide/unmount/account invalidation clears drafts and ignores pending read or save callbacks', async () => {
  for (const operation of ['read', 'save']) {
    const p = setup(); await p.c.start('d', 'admin'); const wait = deferred();
    if (operation === 'read') p.setRead(() => wait.promise); else { p.state.name = '新名'; p.setWrite(() => wait.promise); }
    const pending = operation === 'read' ? p.c.refresh() : p.c.submit(); p.c.stop();
    assert.equal(p.state.name, ''); assert.equal(p.state.location, ''); assert.equal(p.state.feedback, ''); assert.equal(p.state.settings, null);
    wait.resolve(dto({ name: '新名', version: 'b'.repeat(64) })); await pending; assert.equal(p.saved.length, 0); assert.equal(p.state.settings, null);
  }
});
test('device and scope changes invalidate an older save before new read', async () => {
  const p = setup(); await p.c.start('d', 'family'); p.state.name = '新名'; const wait = deferred(); p.setWrite(() => wait.promise);
  const saving = p.c.submit(); p.setRead(async () => dto({ deviceId: 'next', name: '下一台' })); await p.c.start('next', 'admin');
  wait.resolve(dto({ name: '新名', version: 'b'.repeat(64) })); await saving;
  assert.equal(p.state.name, '下一台'); assert.equal(p.saved.length, 0);
  assert.deepEqual(p.calls.at(-1)?.args, ['next', 'admin']);
});
test('deadline checks reject late completion even before the timer callback runs', async () => {
  const p = setup(); await p.c.start('d', 'family'); p.state.name = '新名'; const wait = deferred(); p.setWrite(() => wait.promise);
  const saving = p.c.submit(); p.clock(30000); wait.resolve(dto({ name: '新名', version: 'b'.repeat(64) })); await saving;
  assert.equal(p.saved.length, 0); assert.equal(p.state.needsRead, true); assert.match(p.state.feedback, /未确认/);
  assert.equal(p.calls.filter(x => x.method === 'GET').length, 1);
});
test('fresh read after timeout stays authoritative when the original response arrives later', async () => {
  const p = setup(), wait = deferred(); p.setRead(() => wait.promise); const old = p.c.start('d', 'family'); p.expire(); await old;
  p.setRead(async () => dto({ name: '最新' })); await p.c.refresh();
  wait.resolve(dto({ name: '迟到' })); await new Promise(r => setImmediate(r));
  assert.equal(p.state.name, '最新'); assert.equal(p.state.needsRead, false);
});
test('scope-only switch invalidates a pending read for the same device', async () => {
  const p = setup(), wait = deferred(); p.setRead(() => wait.promise); const old = p.c.start('d', 'family');
  p.setRead(async () => dto({ name: '后台' })); await p.c.start('d', 'admin'); wait.resolve(dto({ name: '家庭' })); await old;
  assert.equal(p.state.name, '后台'); assert.deepEqual(p.calls.at(-1)?.args, ['d', 'admin']);
});
