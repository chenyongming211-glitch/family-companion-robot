import test from 'node:test';
import assert from 'node:assert/strict';
import { createRemoteApi } from '../src/remote-api.ts';
import { invalidateSensitive } from '../src/sensitive-session.ts';

const id = 'dev_' + 'a'.repeat(32), boot = 'b'.repeat(32);
const release = { id: 'fw_' + 'c'.repeat(64), version: '2.5.3', sourceCommit: 'd'.repeat(40), size: 3530000, sha256: 'c'.repeat(64), elfSha256: 'e'.repeat(64) };
const job = { id: 'ota_' + 'f'.repeat(32), releaseId: release.id, version: release.version, status: 'queued', progress: null, error: '', requestedAt: '2026-11-14T01:00:00Z', expiresAt: '2026-11-14T01:15:00Z' };
const dto = { deviceId: id, canUpgrade: true, connection: 'online', boot, mode: 'online', current: { version: '2.5.2', elfSha256: '1'.repeat(64) }, releases: [release], job: null };

test('firmware API uses both real scopes, exact request body, credentials, no-store and cancellation', async () => {
  const seen: Array<{url: string; init?: RequestInit}> = [];
  const api = createRemoteApi((async (url: unknown, init?: RequestInit) => { seen.push({url: String(url), init}); return new Response(JSON.stringify(dto)); }) as typeof fetch);
  const abort = new AbortController();
  for (const scope of ['family', 'admin'] as const) {
    assert.equal((await api.getDeviceFirmware(id, scope, abort.signal)).current?.version, '2.5.2');
    await api.upgradeDeviceFirmware(id, {...{releaseId: release.id, boot}, url: 'https://ignored.invalid'} as any, scope, abort.signal);
  }
  assert.deepEqual(seen.map(x => x.url), [`/api/family/device-firmware/${id}`, `/api/family/device-firmware/${id}`, `/api/admin/device-firmware/${id}`, `/api/admin/device-firmware/${id}`]);
  for (const request of seen) { assert.equal(request.init?.credentials, 'same-origin'); assert.equal(request.init?.cache, 'no-store'); assert.equal(request.init?.signal, abort.signal); }
  assert.equal(seen[1]!.init?.method, 'POST');
  assert.deepEqual(JSON.parse(String(seen[1]!.init?.body)), {releaseId: release.id, boot});
});

test('firmware API accepts legacy unknown version and every persisted job outcome without inventing completion', async () => {
  let response: unknown = {...dto, mode: 'restart', current: null};
  const api = createRemoteApi((async () => new Response(JSON.stringify(response))) as typeof fetch);
  assert.equal((await api.getDeviceFirmware(id, 'family')).current, null);
  for (const status of ['queued', 'sent', 'downloading', 'restarting', 'confirmed', 'failed', 'unknown']) {
    response = {...dto, job: {...job, status, progress: status === 'downloading' ? 42 : null, error: status === 'failed' ? 'health_failed' : ''}};
    assert.equal((await api.getDeviceFirmware(id, 'admin')).job?.status, status);
  }
  response = {...dto, connection: 'unknown', canUpgrade: false, boot: '', mode: 'restart', current: null, releases: []};
  assert.equal((await api.getDeviceFirmware(id, 'family')).boot, '');
});

test('firmware API rejects malformed identities, lists, progress, status and unbounded or unexpected fields', async () => {
  let response: unknown = dto;
  const api = createRemoteApi((async () => new Response(JSON.stringify(response))) as typeof fetch);
  for (const bad of [null, {}, {...dto, deviceId: 'other'}, {...dto, canUpgrade: 'yes'}, {...dto, connection: 'ready'}, {...dto, boot: 'bad'}, {...dto, mode: 'auto'}, {...dto, current: null}, {...dto, current: {...dto.current, version: 'next'}}, {...dto, current: {...dto.current, elfSha256: 'A'.repeat(64)}}, {...dto, releases: Array(9).fill(release)}, {...dto, releases: [release, release]}, {...dto, releases: [{...release, id: 'fw_' + 'a'.repeat(64)}]}, {...dto, releases: [{...release, size: 3538945}]}, {...dto, releases: [{...release, sourceCommit: 'bad'}]}, {...dto, releases: [{...release, url: 'https://bad.invalid'}]}, {...dto, job: {...job, status: 'installed'}}, {...dto, job: {...job, progress: 101}}, {...dto, job: {...job, progress: 0.5}}, {...dto, job: {...job, error: 'https://private.invalid'}}, {...dto, job: {...job, expiresAt: 'tomorrow'}}, {...dto, job: {...job, expiresAt: '2026-11-14T01:16:00Z'}}, {...dto, ticket: 'private'}]) {
    response = bad;
    await assert.rejects(api.getDeviceFirmware(id, 'family'), /核实/);
    await assert.rejects(api.upgradeDeviceFirmware(id, {releaseId: release.id, boot}, 'admin'), /核实/);
  }
});

test('invalid local upgrade identity cannot issue POST and server errors retain code and status', async () => {
  let calls = 0;
  const api = createRemoteApi((async () => { calls++; return new Response(JSON.stringify(dto)); }) as typeof fetch);
  for (const input of [{releaseId: 'fw_bad', boot}, {releaseId: release.id, boot: ''}]) await assert.rejects(api.upgradeDeviceFirmware(id, input, 'family'), /核实/);
  assert.equal(calls, 0);
  for (const [status, code] of [[403, 'forbidden'], [409, 'firmware_conflict'], [503, 'firmware_unavailable']] as const) {
    const failed = createRemoteApi((async () => new Response(JSON.stringify({code, message: '升级服务反馈'}), {status})) as typeof fetch);
    await assert.rejects(failed.getDeviceFirmware(id, 'family'), (error: any) => error.code === code && error.status === status && error.message === '升级服务反馈');
  }
});

test('a pending upgrade receipt cannot survive session invalidation', async () => {
  let resolve!: (response: Response) => void;
  const api = createRemoteApi((async () => new Promise<Response>(r => resolve = r)) as typeof fetch);
  const saving = api.upgradeDeviceFirmware(id, {releaseId: release.id, boot}, 'family');
  invalidateSensitive(); resolve(new Response(JSON.stringify({...dto, job})));
  await assert.rejects(saving, /页面已离开|账号已变化/);
});

test('firmware audit category is parsed and sent as an admin filter; unexpected categories are rejected', async () => {
  let response: unknown = {items: [{id: 'audit_fixture', time: '2026-11-14T00:00:00Z', actor: '管理员', deviceId: id, target: '客厅', action: 'firmware', description: '固件升级', status: 'confirmed', result: '设备已确认', scope: 'admin', canOpenDevice: true}], hasMore: false, pending: false};
  let url = '';
  const api = createRemoteApi((async (input: unknown) => {url = String(input); return new Response(JSON.stringify(response));}) as typeof fetch);
  assert.equal((await api.listAuditPage({action: 'firmware'})).items[0]!.action, 'firmware');
  assert.equal(new URL(url, 'http://fixture').searchParams.get('action'), 'firmware');
  response = {items: [{action: 'arbitrary'}], hasMore: false, pending: false};
  await assert.rejects(api.listAuditPage(), /核实/);
});
