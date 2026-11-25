import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import * as vue from 'vue';
import { parse, compileScript, compileTemplate } from '@vue/compiler-sfc';
import ts from 'typescript';
import * as controller from '../src/voice-routes.ts';
import * as sensitive from '../src/sensitive-session.ts';
const id = 'dev_' + 'a'.repeat(32);
const dto = (deviceId = id, extra: object = {}) => ({deviceId, selectedRoute: 'C', activeRoute: null, connection: 'idle', version: 'a'.repeat(64), canSwitch: true, ...extra});
const tick = async () => {await new Promise(r => setImmediate(r)); await vue.nextTick();};
type El = {tag: string; props: Record<string, any>; children: El[]; text: string; parent?: El};
function mount(api: any) {
  const source = readFileSync(new URL('../src/components/VoiceRoutes.vue', import.meta.url), 'utf8');
  const {descriptor} = parse(source), script = compileScript(descriptor, {id: 'voice-route-test'});
  const template = compileTemplate({source: descriptor.template!.content, filename: 'VoiceRoutes.vue', id: 'voice-route-test', compilerOptions: {bindingMetadata: script.bindings}}); assert.deepEqual(template.errors, []);
  const load = (input: string) => {const module = {exports: {} as any}; const code = ts.transpileModule(input, {compilerOptions: {target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS}}).outputText; const realRequire = createRequire(import.meta.url); new Function('require', 'module', 'exports', code)((name: string) => name === 'vue' ? vue : name === '../api.ts' ? {api} : name === '../voice-routes.ts' ? controller : name === '../sensitive-session.ts' ? sensitive : realRequire(name), module, module.exports); return module.exports;};
  const component = load(script.content).default; component.render = load(template.code).render;
  const element = (tag: string, text = ''): El => ({tag, text, props: {}, children: []});
  const renderer = vue.createRenderer<El, El>({createElement: tag => element(tag), createText: text => element('#text', text), createComment: () => element('#comment'), insert: (el, parent, anchor) => {if (el.parent) {const i = el.parent.children.indexOf(el); if (i >= 0) el.parent.children.splice(i, 1);} el.parent = parent; const i = anchor ? parent.children.indexOf(anchor) : -1; if (i < 0) parent.children.push(el); else parent.children.splice(i, 0, el);}, remove: el => {const i = el.parent?.children.indexOf(el) ?? -1; if (i >= 0) el.parent!.children.splice(i, 1);}, setText: (el, text) => {el.text = text;}, setElementText: (el, text) => {el.text = text; el.children = [];}, parentNode: el => el.parent ?? null, nextSibling: el => el.parent?.children[el.parent.children.indexOf(el) + 1] ?? null, patchProp: (el, key, _old, value) => {el.props[key] = value;}});
  const props = vue.reactive({deviceId: id}), app = renderer.createApp({setup: () => () => vue.h(component, props)}), root = element('root'); app.mount(root);
  const nodes = () => {const all: El[] = []; const visit = (el: El) => {all.push(el); el.children.forEach(visit);}; visit(root); return all;};
  return {app, props, nodes, state: () => ((app as any)._instance.subTree.component.setupState.state as ReturnType<typeof controller.voiceRoutesState>), text: () => nodes().map(n => n.text).join(' '), find: (id: string) => {const found = nodes().find(n => n.props.id === id); assert.ok(found, `missing #${id}`); return found;}, button: (label: string) => {const found = nodes().find(n => n.tag === 'button' && n.text === label); assert.ok(found, `missing ${label}`); return found;}};
}
async function withDocument(action: (doc: EventTarget & {visibilityState: string}) => Promise<void>) {
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'document'), doc = Object.assign(new EventTarget(), {visibilityState: 'visible'}); Object.defineProperty(globalThis, 'document', {configurable: true, value: doc});
  try {await action(doc);} finally {if (previous) Object.defineProperty(globalThis, 'document', previous); else Reflect.deleteProperty(globalThis, 'document');}
}


test('compiled voice panel offers three buttons, disables retired route A and switches active routes', async () => withDocument(async () => {
  const calls: string[] = []; let saved = false;
  const m = mount({getDeviceVoiceRoute: async () => {calls.push('GET'); return dto(id, saved ? {selectedRoute: 'B', version: 'b'.repeat(64)} : {});}, setDeviceVoiceRoute: async () => {calls.push('PUT'); saved = true; return {...dto(id, {selectedRoute: 'B', version: 'b'.repeat(64)}), disconnectConfirmed: true};}});
  try {
    await tick();
    for (const text of [/Omni/, /CosyVoice/, /Fun-ASR/, /Qwen Plus/, /原生直接语音/, /完整合成后播放/, /当前对话将立即结束/, /已选择/, /当前连接实际链路/]) assert.match(m.text(), text);
    const buttons = ['A', 'B', 'C'].map(route => m.find('voice-route-' + route));
    assert.equal(buttons.length, 3); assert.equal(buttons[2]!.props['aria-pressed'], true);
    assert.equal(buttons[0]!.props.disabled, true);
    assert.equal(buttons[1]!.props.disabled, false);
    for (const button of buttons) {assert.equal(button.tag, 'button'); assert.equal(button.props.type, 'button');}
    buttons[1]!.props.onClick(); await tick(); assert.deepEqual(calls, ['GET', 'PUT', 'GET']); assert.match(m.text(), /已切换，请重新唤醒/);
    assert.equal(m.nodes().filter(n => n.props.role === 'dialog').length, 0);
  } finally {m.app.unmount();}
}));

test('compiled voice panel separates selected and actual route, forbids readonly writes and preserves unknown', async () => withDocument(async () => {
  for (const state of [{activeRoute: 'B', connection: 'active'}, {connection: 'unknown'}, {canSwitch: false}]) {
    let writes = 0;
    const m = mount({getDeviceVoiceRoute: async () => dto(id, state), setDeviceVoiceRoute: async () => {writes++;}});
    try {
      await tick(); assert.match(m.text(), /已选择：C/);
      if (state.connection === 'active') assert.match(m.text(), /当前连接实际链路：B/);
      if (state.connection === 'unknown') assert.match(m.text(), /当前连接实际链路：未知/);
      if (state.canSwitch === false) {assert.ok(m.find('voice-route-B').props.disabled); m.find('voice-route-B').props.onClick(); await tick(); assert.equal(writes, 0);}
      assert.doesNotMatch(m.text(), /已切换/);
    } finally {m.app.unmount();}
  }
}));

test('compiled panel aborts on hide/logout/device change and drops late results with no automatic writes', async () => withDocument(async doc => {
  let resolve!: (value: any) => void, signal!: AbortSignal; let writes = 0, reads = 0;
  const m = mount({getDeviceVoiceRoute: async (deviceId: string) => {reads++; return dto(deviceId);}, setDeviceVoiceRoute: async (_id: string, _route: string, _version: string, requestSignal: AbortSignal) => {writes++; signal = requestSignal; return new Promise(r => resolve = r);}});
  await tick(); m.find('voice-route-B').props.onClick(); await tick(); assert.ok(m.find('voice-route-C').props.disabled);
  doc.visibilityState = 'hidden'; doc.dispatchEvent(new Event('visibilitychange')); await tick(); assert.equal(signal.aborted, true); assert.equal(m.state().view, null);
  resolve({...dto(), disconnectConfirmed: true}); await tick(); assert.equal(m.state().view, null);
  doc.visibilityState = 'visible'; doc.dispatchEvent(new Event('visibilitychange')); await tick(); m.props.deviceId = 'next'; await tick(); assert.equal(m.state().view?.deviceId, 'next');
  sensitive.invalidateSensitive(); await tick(); assert.equal(m.state().view, null);
  m.app.unmount(); const before = reads; doc.dispatchEvent(new Event('visibilitychange')); sensitive.invalidateSensitive(); await tick(); assert.equal(reads, before); assert.equal(writes, 1);
}));

test('voice routes mount only in admin configuration, with no family scope input or entry', () => {
  const source = readFileSync(new URL('../src/pages/CorePages.vue', import.meta.url), 'utf8');
  assert.match(source, /import VoiceRoutes/);
  assert.match(source, /<VoiceRoutes[^>]*v-if="scope === 'admin' && route.page === 'configuration'"[^>]*:device-id="device.id"/);
});
