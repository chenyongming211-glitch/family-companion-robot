import test from 'node:test';
import assert from 'node:assert/strict';

test('default API uses same-origin remote reads and never falls back on failure', async () => {
  const previous = globalThis.fetch;
  const storage = Object.getOwnPropertyDescriptor(globalThis, 'sessionStorage');
  let storageReads = 0;
  Object.defineProperty(globalThis, 'sessionStorage', {configurable: true, get() {storageReads++; throw new Error('storage unavailable');}});
  const seen: Array<{url: string; init?: RequestInit}> = [];
  let failed = false;
  globalThis.fetch = (async (url: any, init?: RequestInit) => {
    seen.push({url: String(url), init});
    if (failed) throw new Error('isolated network failure');
    return new Response('[]');
  }) as typeof fetch;
  try {
    const {api} = await import('../src/api.ts');
    assert.deepEqual(await api.listDevices('family'), []);
    assert.equal(seen[0]?.url, '/api/family/devices');
    assert.equal(seen[0]?.init?.credentials, 'same-origin');
    assert.equal(seen[0]?.init?.cache, 'no-store');
    failed = true;
    await assert.rejects(api.listDevices('admin'), /管理服务连接失败/);
    assert.equal(storageReads, 0, 'real API must never consult browser demo storage');
  } finally { globalThis.fetch = previous; if (storage) Object.defineProperty(globalThis, 'sessionStorage', storage); else Reflect.deleteProperty(globalThis, 'sessionStorage'); }
});

test('route defaults contain no invented identity and directory exposes implemented routes only', async () => {
  const prior = Object.getOwnPropertyDescriptor(globalThis, 'location');
  Object.defineProperty(globalThis, 'location', { configurable: true, value: { hash: '#/home' } });
  try {
    const {readRoute, adminRoutes, familyRoutes} = await import('../src/navigation.ts');
    assert.deepEqual(readRoute('family'), {page: 'home', device: '', chat: '', memory: '', family: ''});
    const removed = ['daily','capabilities','expressions','voiceprints','tasks','firmware','operations','policies','invite','bind','privacy'];
    for (const [route] of [...adminRoutes, ...familyRoutes]) assert.ok(!removed.includes(route), route);
  } finally { if (prior) Object.defineProperty(globalThis, 'location', prior); else Reflect.deleteProperty(globalThis, 'location'); }
});
