import test from 'node:test';
import assert from 'node:assert/strict';
import { createFirmwareUpgradeController, firmwareUpgradeState, firmwareJobMessage, firmwareCandidates } from '../src/firmware-upgrade.ts';
import type { DeviceFirmware } from '../src/types.ts';

const id = 'dev_' + 'a'.repeat(32), boot = 'b'.repeat(32);
const release = {id: 'fw_' + 'c'.repeat(64), version: '2.5.3', sourceCommit: 'd'.repeat(40), size: 3530000, sha256: 'c'.repeat(64), elfSha256: 'e'.repeat(64)};
const job = {id: 'ota_' + 'f'.repeat(32), releaseId: release.id, version: release.version, status: 'queued' as const, progress: null, error: '', requestedAt: '2026-11-14T01:00:00Z', expiresAt: '2026-11-14T01:15:00Z'};
const dto = (deviceId = id): DeviceFirmware => ({deviceId, canUpgrade: true, connection: 'online', boot, mode: 'online', current: {version: '2.5.2', elfSha256: '1'.repeat(64)}, releases: [release], job: null});
const tick = () => new Promise(r => setImmediate(r));
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => resolve = r); return {resolve, promise}; }
function setup() {
  let read = async (_id: string) => dto(_id), write = async () => ({...dto(), job}), now = 0;
  const calls: Array<{method: string; args: any[]}> = [];
  const timers = new Map<number, {fn: () => void; ms: number}>(); let next = 0;
  const state = firmwareUpgradeState();
  const control = createFirmwareUpgradeController({getDeviceFirmware: async (...args) => {calls.push({method: 'GET', args}); return read(args[0]);}, upgradeDeviceFirmware: async (...args) => {calls.push({method: 'POST', args}); return write();}}, state, {after: (fn, ms) => {timers.set(++next, {fn, ms}); return next;}, cancel: timer => {timers.delete(timer as number);}, now: () => now});
  return {state, control, calls, timers, setRead: (fn: typeof read) => {read = fn;}, setWrite: (fn: typeof write) => {write = fn;}, clock: (value: number) => {now = value;}, fire(ms: number) {const timer = [...timers].find(([, timer]) => timer.ms === ms); assert.ok(timer, `missing ${ms} timer`); timers.delete(timer[0]); timer[1].fn();}};
}

test('reading and cancelled confirmation never POST; explicit confirmation sends selected identity once', async () => {
  const p = setup(); await p.control.start(id, 'family');
  await p.control.confirmUpgrade(); assert.equal(p.calls.length, 1);
  p.control.askUpgrade(release.id); assert.equal(p.state.confirming, release.id); p.control.cancelUpgrade(); await p.control.confirmUpgrade(); assert.equal(p.calls.length, 1);
  p.control.askUpgrade(release.id); const wait = deferred<DeviceFirmware>(); p.setWrite(() => wait.promise); const writing = p.control.confirmUpgrade(); await tick();
  await p.control.confirmUpgrade(); p.control.askUpgrade(release.id); await p.control.refresh(); assert.equal(p.calls.length, 2); assert.equal(p.state.pending, true);
  assert.deepEqual(p.calls[1]!.args.slice(0, 3), [id, {releaseId: release.id, boot}, 'family']);
  wait.resolve({...dto(), job}); await writing; assert.equal(p.state.view?.job?.status, 'queued'); assert.equal(p.state.confirming, null); assert.doesNotMatch(firmwareJobMessage(p.state.view!), /升级成功/); p.control.stop();
});

test('persisted active job polls GET only through receipt stages; only confirmed says upgrade succeeded', async () => {
  const p = setup(); p.setRead(async () => ({...dto(), job})); await p.control.start(id, 'admin');
  for (const status of ['sent', 'downloading', 'restarting', 'confirmed'] as const) {
    p.setRead(async () => ({...dto(), job: {...job, status, progress: status === 'downloading' ? 37 : null}}));
    p.fire(3000); await tick();
    assert.equal(p.state.view?.job?.status, status);
    if (status === 'confirmed') assert.match(firmwareJobMessage(p.state.view!), /升级成功/); else assert.doesNotMatch(firmwareJobMessage(p.state.view!), /升级成功/);
  }
  assert.ok(p.calls.every(x => x.method === 'GET')); assert.equal(p.timers.size, 0); p.control.stop();
});

test('failed and unknown receipts stop polling, preserve outcome and require a new explicit manual action', async () => {
  for (const status of ['failed', 'unknown'] as const) {
    const p = setup(); p.setRead(async () => ({...dto(), job: {...job, status, error: status === 'failed' ? 'health_failed' : 'timeout'}})); await p.control.start(id, 'family');
    assert.equal(p.timers.size, 0); assert.match(firmwareJobMessage(p.state.view!), /失败|未知/); assert.match(firmwareJobMessage(p.state.view!), /不会自动重试/); assert.equal(p.calls.length, 1);
    await p.control.confirmUpgrade(); assert.equal(p.calls.length, 1); p.control.stop();
  }
});

test('readonly, offline, active task and noncandidate identities never arm or send an upgrade', async () => {
  for (const change of [{canUpgrade: false}, {connection: 'offline' as const}, {job}]) {
    const p = setup(); p.setRead(async () => ({...dto(), ...change})); await p.control.start(id, 'family'); p.control.askUpgrade(release.id); await p.control.confirmUpgrade(); assert.equal(p.calls.length, 1); assert.equal(p.state.confirming, null); p.control.stop();
  }
  const p = setup(); await p.control.start(id, 'family'); p.control.askUpgrade('fw_' + '0'.repeat(64)); await p.control.confirmUpgrade(); assert.equal(p.calls.length, 1); p.control.stop();
});

test('timeout aborts POST, becomes unconfirmed and ignores late receipt without automatic retry', async () => {
  const p = setup(); await p.control.start(id, 'family'); const wait = deferred<DeviceFirmware>(); p.setWrite(() => wait.promise); p.control.askUpgrade(release.id); const writing = p.control.confirmUpgrade(); await tick();
  const signal = p.calls[1]!.args[3] as AbortSignal; p.fire(30000); await writing;
  assert.equal(signal.aborted, true); assert.equal(p.state.needsRead, true); assert.equal(p.state.view, null); assert.equal(p.state.pending, false); assert.match(p.state.feedback, /结果未确认/); assert.match(p.state.feedback, /不会自动重试/);
  wait.resolve({...dto(), job: {...job, status: 'confirmed'}}); await tick(); await p.control.confirmUpgrade(); assert.equal(p.state.view, null); assert.equal(p.calls.length, 2); p.control.stop();
});

test('device or scope change aborts pending GET/POST and late callbacks cannot replace new device', async () => {
  const p = setup(); const old = deferred<DeviceFirmware>(); p.setRead(() => old.promise); const initial = p.control.start(id, 'family'); await tick(); const readSignal = p.calls[0]!.args[2] as AbortSignal;
  p.setRead(async selected => dto(selected)); await p.control.start('other', 'admin'); old.resolve(dto()); await initial; assert.equal(readSignal.aborted, true); assert.equal(p.state.view?.deviceId, 'other');
  const q = setup(); await q.control.start(id, 'family'); const late = deferred<DeviceFirmware>(); q.setWrite(() => late.promise); q.control.askUpgrade(release.id); const saving = q.control.confirmUpgrade(); await tick(); const writeSignal = q.calls[1]!.args[3] as AbortSignal;
  q.control.stop(); q.setRead(async selected => dto(selected)); await q.control.start('other', 'admin'); late.resolve({...dto(), job}); await saving; await tick(); assert.equal(writeSignal.aborted, true); assert.equal(q.state.view?.deviceId, 'other'); assert.equal(q.state.confirming, null); assert.equal(q.calls.length, 3); q.control.stop(); p.control.stop();
});

test('request failure and mismatched device never become task success or allow repeat POST without rereading', async () => {
  for (const reply of [async () => {throw new Error('HTTP 409 升级状态已变化');}, async () => ({...dto('other'), job})]) {
    const p = setup(); await p.control.start(id, 'family'); p.setWrite(reply); p.control.askUpgrade(release.id); await p.control.confirmUpgrade(); assert.equal(p.state.view, null); assert.equal(p.state.needsRead, true); assert.match(p.state.feedback, /重新读取/); await p.control.confirmUpgrade(); assert.equal(p.calls.length, 2); p.control.stop();
  }
});

test('legacy upgrade explicitly waits for restart while current version remains unknown', async () => {
  const legacy: DeviceFirmware = {...dto(), mode: 'restart', current: null, job};
  assert.equal(legacy.current, null); assert.match(firmwareJobMessage(legacy), /关机再开机/); assert.doesNotMatch(firmwareJobMessage(legacy), /升级成功/);
});
test('sent records command dispatch without claiming the device has received it', () => {
  const text = firmwareJobMessage({...dto(), job: {...job, status: 'sent'}});
  assert.match(text, /指令已下发/); assert.match(text, /等待设备下载回执/); assert.doesNotMatch(text, /设备已接收|升级成功/);
});

test('candidate list excludes same ELF, same/older versions and compares numeric version parts', () => {
  const view = dto(); view.current!.version = '2.5.9';
  view.releases = [release, {...release, id: 'fw_' + '2'.repeat(64), version: '2.5.10'}, {...release, id: 'fw_' + '3'.repeat(64), version: '2.6.0', elfSha256: view.current!.elfSha256}, {...release, id: 'fw_' + '4'.repeat(64), version: '2.5.9'}];
  assert.deepEqual(firmwareCandidates(view).map(r => r.version), ['2.5.10']); assert.equal(firmwareCandidates({...view, current: null, mode: 'restart'}).length, 4);
});
