import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import * as vue from 'vue';
import { parse, compileScript, compileTemplate } from '@vue/compiler-sfc';
import ts from 'typescript';
import * as controller from '../src/firmware-upgrade.ts';
import * as sensitive from '../src/sensitive-session.ts';

const id = 'dev_' + 'a'.repeat(32), boot = 'b'.repeat(32);
const release = {id: 'fw_' + 'c'.repeat(64), version: '2.5.3', sourceCommit: 'd'.repeat(40), size: 3530000, sha256: 'c'.repeat(64), elfSha256: 'e'.repeat(64)};
const job = {id: 'ota_' + 'f'.repeat(32), releaseId: release.id, version: release.version, status: 'queued', progress: null, error: '', requestedAt: '2026-11-14T01:00:00Z', expiresAt: '2026-11-14T01:15:00Z'};
const dto = (deviceId = id) => ({deviceId, canUpgrade: true, connection: 'online', boot, mode: 'online', current: {version: '2.5.2', elfSha256: '1'.repeat(64)}, releases: [release], job: null});
const tick = async () => {await new Promise(r => setImmediate(r)); await vue.nextTick();};
type El = {tag: string; props: Record<string, any>; children: El[]; text: string; parent?: El};
function mount(api: any) {
  const source = readFileSync(new URL('../src/components/FirmwareUpgrade.vue', import.meta.url), 'utf8');
  const {descriptor} = parse(source), script = compileScript(descriptor, {id: 'firmware-test'});
  const template = compileTemplate({source: descriptor.template!.content, filename: 'FirmwareUpgrade.vue', id: 'firmware-test', compilerOptions: {bindingMetadata: script.bindings}}); assert.deepEqual(template.errors, []);
  const load = (input: string) => {const module = {exports: {} as any}; const code = ts.transpileModule(input, {compilerOptions: {target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS}}).outputText; const realRequire = createRequire(import.meta.url); new Function('require', 'module', 'exports', code)((name: string) => name === 'vue' ? vue : name === '../api.ts' ? {api} : name === '../firmware-upgrade.ts' ? controller : name === '../sensitive-session.ts' ? sensitive : realRequire(name), module, module.exports); return module.exports;};
  const component = load(script.content).default; component.render = load(template.code).render;
  const element = (tag: string, text = ''): El => ({tag, text, props: {}, children: []});
  const renderer = vue.createRenderer<El, El>({createElement: tag => element(tag), createText: text => element('#text', text), createComment: () => element('#comment'), insert: (el, parent, anchor) => {if (el.parent) {const i = el.parent.children.indexOf(el); if (i >= 0) el.parent.children.splice(i, 1);} el.parent = parent; const i = anchor ? parent.children.indexOf(anchor) : -1; if (i < 0) parent.children.push(el); else parent.children.splice(i, 0, el);}, remove: el => {const i = el.parent?.children.indexOf(el) ?? -1; if (i >= 0) el.parent!.children.splice(i, 1);}, setText: (el, text) => {el.text = text;}, setElementText: (el, text) => {el.text = text; el.children = [];}, parentNode: el => el.parent ?? null, nextSibling: el => el.parent?.children[el.parent.children.indexOf(el) + 1] ?? null, patchProp: (el, key, _old, value) => {el.props[key] = value;}});
  const props = vue.reactive({deviceId: id, scope: 'family'}), app = renderer.createApp({setup: () => () => vue.h(component, props)}), root = element('root'); app.mount(root);
  const nodes = () => {const all: El[] = []; const visit = (el: El) => {all.push(el); el.children.forEach(visit);}; visit(root); return all;};
  return {app, props, nodes, state: () => ((app as any)._instance.subTree.component.setupState.state as ReturnType<typeof controller.firmwareUpgradeState>), text: () => nodes().map(n => n.text).join(' '), find: (id: string) => {const found = nodes().find(n => n.props.id === id); assert.ok(found, `missing #${id}`); return found;}, button: (label: string) => {const found = nodes().find(n => n.tag === 'button' && n.text === label); assert.ok(found, `missing ${label}`); return found;}};
}
async function withDocument(action: (doc: EventTarget & {visibilityState: string}) => Promise<void>) {
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'document'), doc = Object.assign(new EventTarget(), {visibilityState: 'visible'}); Object.defineProperty(globalThis, 'document', {configurable: true, value: doc});
  try {await action(doc);} finally {if (previous) Object.defineProperty(globalThis, 'document', previous); else Reflect.deleteProperty(globalThis, 'document');}
}

test('compiled upgrade panel requires actual confirmation, offers cancellation, and displays real version/size/source', async () => withDocument(async () => {
  const calls: string[] = []; let input: unknown;
  const m = mount({getDeviceFirmware: async () => {calls.push('GET'); return dto();}, upgradeDeviceFirmware: async (_id: string, value: unknown) => {calls.push('POST'); input = value; return {...dto(), job};}});
  try {
    await tick(); assert.match(m.text(), /当前版本：2\.5\.2/); assert.match(m.text(), /3,530,000/); assert.match(m.text(), /ddddddd/); assert.deepEqual(calls, ['GET']);
    m.button('升级到 2.5.3').props.onClick(); await tick(); assert.match(m.text(), /暂时不能对话/); assert.match(m.text(), /保持供电与 Wi-Fi/); assert.equal(calls.length, 1);
    m.find('firmware-cancel').props.onClick(); await tick(); assert.equal(m.state().confirming, null); assert.equal(calls.length, 1);
    m.button('升级到 2.5.3').props.onClick(); await tick(); m.find('firmware-confirm').props.onClick(); await tick();
    assert.deepEqual(calls, ['GET', 'POST']); assert.deepEqual(input, {releaseId: release.id, boot}); assert.match(m.text(), /等待设备接收/); assert.doesNotMatch(m.text(), /升级成功/);
  } finally {m.app.unmount();}
}));

test('legacy, readonly/offline, empty and unavailable panel states do not invent installed version or success', async () => withDocument(async () => {
  const legacy = mount({getDeviceFirmware: async () => ({...dto(), mode: 'restart', current: null, job})});
  try {await tick(); assert.match(legacy.text(), /当前固件版本未知/); assert.match(legacy.text(), /关机再开机/); assert.doesNotMatch(legacy.text(), /升级成功/);} finally {legacy.app.unmount();}
  for (const change of [{canUpgrade: false}, {canUpgrade: false, connection: 'offline'}]) {
    let writes = 0; const m = mount({getDeviceFirmware: async () => ({...dto(), ...change}), upgradeDeviceFirmware: async () => {writes++;}});
    try {await tick(); assert.ok(m.button('升级到 2.5.3').props.disabled); m.button('升级到 2.5.3').props.onClick(); await tick(); assert.equal(m.state().confirming, null); assert.equal(writes, 0);} finally {m.app.unmount();}
  }
  const empty = mount({getDeviceFirmware: async () => ({...dto(), releases: []})}); try {await tick(); assert.match(empty.text(), /暂无可升级版本/);} finally {empty.app.unmount();}
  const failed = mount({getDeviceFirmware: async () => {throw new Error('HTTP 503 服务不可用');}}); try {await tick(); assert.match(failed.text(), /503 服务不可用/); assert.doesNotMatch(failed.text(), /暂无可升级版本|升级成功/);} finally {failed.app.unmount();}
}));

test('compiled receipts show progress and new-boot waiting, and only confirmed reports upgrade success', async () => withDocument(async () => {
  for (const [status, text] of [['downloading', /42%/], ['restarting', /等待新固件启动和音频自检确认/], ['confirmed', /升级成功/], ['failed', /升级失败/], ['unknown', /升级结果未知/]] as const) {
    const m = mount({getDeviceFirmware: async () => ({...dto(), job: {...job, status, progress: status === 'downloading' ? 42 : null, error: status === 'failed' ? 'health_failed' : ''}})});
    try {await tick(); assert.match(m.text(), text); if (status !== 'confirmed') assert.doesNotMatch(m.text(), /升级成功/); assert.equal(m.nodes().filter(n => n.props.role === 'status').length, 1);} finally {m.app.unmount();}
  }
}));

test('compiled panel aborts and clears confirmation on hide/logout, ignores late requests after device switch and unmount', async () => withDocument(async doc => {
  let resolve!: (value: any) => void, writeSignal!: AbortSignal, reads = 0; const scopes: string[] = [];
  const m = mount({getDeviceFirmware: async (deviceId: string, scope: string) => {reads++; scopes.push(scope); return dto(deviceId);}, upgradeDeviceFirmware: async (_id: string, _input: unknown, _scope: string, signal: AbortSignal) => {writeSignal = signal; return new Promise(r => resolve = r);}});
  await tick(); m.button('升级到 2.5.3').props.onClick(); await tick(); m.find('firmware-confirm').props.onClick(); await tick();
  doc.visibilityState = 'hidden'; doc.dispatchEvent(new Event('visibilitychange')); await tick(); assert.equal(writeSignal.aborted, true); assert.equal(m.state().view, null); assert.equal(m.state().confirming, null); resolve({...dto(), job}); await tick(); assert.equal(m.state().view, null); assert.equal(reads, 1);
  doc.visibilityState = 'visible'; doc.dispatchEvent(new Event('visibilitychange')); await tick(); m.props.scope = 'admin'; m.props.deviceId = 'other'; await tick(); assert.equal(m.state().view?.deviceId, 'other'); assert.ok(scopes.includes('admin'));
  m.button('升级到 2.5.3').props.onClick(); await tick(); sensitive.invalidateSensitive(); await tick(); assert.equal(m.state().view, null); assert.equal(m.state().confirming, null);
  m.app.unmount(); const before = reads; doc.dispatchEvent(new Event('visibilitychange')); sensitive.invalidateSensitive(); await tick(); assert.equal(reads, before);
}));

test('both family settings and admin configuration embed OTA for the current selected device', () => {
  const source = readFileSync(new URL('../src/pages/CorePages.vue', import.meta.url), 'utf8');
  assert.match(source, /import FirmwareUpgrade/); assert.match(source, /<FirmwareUpgrade[^>]*:device-id="device.id"[^>]*:scope="scope"/);
});
