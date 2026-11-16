import test from 'node:test';
import assert from 'node:assert/strict';
import { createRemoteApi } from '../src/remote-api.ts';
import { invalidateSensitive } from '../src/sensitive-session.ts';

const id = 'dev_' + 'a'.repeat(32), version = 'b'.repeat(64);
const dto = {deviceId: id, selectedRoute: 'C', activeRoute: null, connection: 'idle', version, canSwitch: true};
test('voice route API is admin-only, uses exact PUT, cancellation, same-origin and no-store', async () => {
  const calls: {url: string; init?: RequestInit}[] = [];
  const api = createRemoteApi((async (url: unknown, init?: RequestInit) => {
    calls.push({url: String(url), init});
    return new Response(JSON.stringify(init?.method === 'PUT' ? {...dto, selectedRoute: 'A', disconnectConfirmed: true} : dto));
  }) as typeof fetch);
  assert.equal(typeof api.getDeviceVoiceRoute, 'function');
  const abort = new AbortController();
  await api.getDeviceVoiceRoute(id, abort.signal);
  await api.setDeviceVoiceRoute(id, 'A', version, abort.signal);
  assert.deepEqual(calls.map(c => c.url), Array(2).fill(`/api/admin/device-voice-route/${id}`));
  assert.deepEqual(JSON.parse(String(calls[1]!.init?.body)), {route: 'A', version});
  assert.equal(calls[1]!.init?.method, 'PUT');
  for (const {init} of calls) { assert.equal(init?.cache, 'no-store'); assert.equal(init?.credentials, 'same-origin'); assert.equal(init?.signal, abort.signal); }
});

test('voice route parser accepts every actual route and unknown/idle but rejects malformed and contradictory wire state', async () => {
  let response: unknown = dto;
  const api = createRemoteApi((async () => new Response(JSON.stringify(response))) as typeof fetch);
  for (const route of ['A', 'B', 'C']) for (const connection of ['connecting', 'active']) {
    response = {...dto, activeRoute: route, connection};
    assert.equal((await api.getDeviceVoiceRoute(id)).activeRoute, route);
  }
  response = {...dto, connection: 'connecting'};
  assert.equal((await api.getDeviceVoiceRoute(id)).activeRoute, null);
  response = {...dto, connection: 'unknown'};
  assert.equal((await api.getDeviceVoiceRoute(id)).connection, 'unknown');
  for (const bad of [null, [], {}, {...dto, deviceId: 'other'}, {...dto, selectedRoute: 'a'}, {...dto, selectedRoute: 'D'}, {...dto, activeRoute: 'D'}, {...dto, activeRoute: 'C'}, {...dto, activeRoute: 'A', connection: 'unknown'}, {...dto, connection: 'active'}, {...dto, connection: 'online'}, {...dto, version: 'B'.repeat(64)}, {...dto, canSwitch: 1}, {...dto, extra: 'private'}, {...dto, disconnectConfirmed: true}]) {
    response = bad; await assert.rejects(api.getDeviceVoiceRoute(id), /核实/);
  }
  for (const bad of [dto, {...dto, disconnectConfirmed: 'true'}, {...dto, disconnectConfirmed: true, selectedRoute: 'B'}]) {
    response = bad; await assert.rejects(api.setDeviceVoiceRoute(id, 'A', version), /核实/);
  }
  response = {...dto, selectedRoute: 'A', connection: 'unknown', disconnectConfirmed: true};
  assert.equal((await api.setDeviceVoiceRoute(id, 'A', version)).connection, 'unknown');
  response = {...dto, selectedRoute: 'A', connection: 'unknown', disconnectConfirmed: false};
  assert.equal((await api.setDeviceVoiceRoute(id, 'A', version)).disconnectConfirmed, false);
});

test('invalid local route/version cannot write; server 409/403 and sensitive generations remain enforced', async () => {
  let calls = 0;
  const api = createRemoteApi((async () => {calls++; return new Response(JSON.stringify(dto));}) as typeof fetch);
  for (const [route, v] of [['D', version], ['A', 'invalid'], ['a', version]]) await assert.rejects(api.setDeviceVoiceRoute(id, route as any, v!), /核实/);
  assert.equal(calls, 0);
  for (const status of [403, 409]) {
    const failed = createRemoteApi((async () => new Response(JSON.stringify({code: 'conflict', message: '请重新读取'}), {status})) as typeof fetch);
    await assert.rejects(failed.getDeviceVoiceRoute(id), (e: any) => e.status === status);
  }
  let resolve!: (value: Response) => void;
  const pending = createRemoteApi((async () => new Promise<Response>(r => resolve = r)) as typeof fetch).getDeviceVoiceRoute(id);
  invalidateSensitive(); resolve(new Response(JSON.stringify(dto)));
  await assert.rejects(pending, /页面已离开|账号已变化/);
});
