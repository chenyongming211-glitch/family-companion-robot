import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import * as vue from 'vue';
import { parse, compileScript, compileTemplate } from '@vue/compiler-sfc';
import ts from 'typescript';
import { createRemoteApi } from '../src/remote-api.ts';
import * as sensitive from '../src/sensitive-session.ts';
import * as navigation from '../src/navigation.ts';
import * as runtime from '../src/runtime-control.ts';
import * as deviceSettings from '../src/device-settings.ts';
import * as audit from '../src/audit-control.ts';
import type { PageRoute, Scope } from '../src/types.ts';

const tick = async () => { await vue.nextTick(); await new Promise(r => setImmediate(r)); await vue.nextTick(); };

async function mount(scope: 'admin' | 'family', loggedIn = false, options: {
  core?: { scope: Scope; route: PageRoute };
  resource?: (url: string, init: RequestInit) => Promise<Response | undefined>;
} = {}) {
  const keys = ['location', 'document', 'window', 'Document'] as const;
  const prior = keys.map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const);
  const home = scope === 'admin' ? 'overview' : 'home';
  const location = { pathname: scope === 'admin' ? '/index.html' : '/family.html', hash: `#/${loggedIn ? 'login' : home}`, href: '' };
  class FixtureDocument extends EventTarget { visibilityState = 'visible'; activeElement = null; }
  const doc = new FixtureDocument();
  Object.defineProperty(globalThis, 'Document', { configurable: true, value: FixtureDocument });
  Object.defineProperty(globalThis, 'location', { configurable: true, value: location });
  Object.defineProperty(globalThis, 'document', { configurable: true, value: doc });
  Object.defineProperty(globalThis, 'window', { configurable: true, value: new EventTarget() });
  let authenticated = loggedIn;
  let releaseLogout!: () => void;
  let sessionReads = 0;
  const api = createRemoteApi((async (input: any, init: RequestInit = {}) => {
    const url = String(input);
    if (url.endsWith('/login')) authenticated = true;
    if (url.endsWith('/logout')) {
      await new Promise<void>(r => { releaseLogout = r; });
      authenticated = false;
      return new Response('{}');
    }
    if (url.endsWith('/session')) sessionReads++;
    if (url.endsWith('/session') || url.endsWith('/login'))
      return authenticated
        ? new Response(JSON.stringify({ email: 'isolated-owner', role: '家庭管理员', scope }))
        : new Response(JSON.stringify({ code: 'unauthenticated', message: '请先登录' }), { status: 401 });
    const resource = await options.resource?.(url, init);
    if (resource) return resource;
    if (url.includes('/chats?')) return new Response(JSON.stringify({ items: [], hasMore: false }));
    if (url.endsWith('/devices')) return new Response('[]');
    throw new Error(`unexpected isolated HTTP request ${url}`);
  }) as typeof fetch);
  const require = createRequire(import.meta.url);
  const components = new Map<string, any>();
  function component(path: string): any {
    if (components.has(path)) return components.get(path);
    const { descriptor } = parse(readFileSync(path, 'utf8'));
    const script = compileScript(descriptor, { id: path });
    const template = compileTemplate({ source: descriptor.template!.content, filename: path, id: path, compilerOptions: { bindingMetadata: script.bindings } });
    assert.deepEqual(template.errors, []);
    const module = { exports: {} as any }, rendered = { exports: {} as any };
    const load = (name: string): any => {
      if (name === 'vue') return vue;
      if (name.endsWith('/api.ts')) return { api };
      if (name.endsWith('/navigation.ts')) return navigation;
      if (name.endsWith('/sensitive-session.ts')) return sensitive;
      if (name.endsWith('/runtime-control.ts')) return runtime;
      if (name.endsWith('/device-settings.ts')) return deviceSettings;
      if (name.endsWith('/audit-control.ts')) return audit;
      if (name.endsWith('.css')) return {};
      if (name.endsWith('.vue')) return { default: component(resolve(dirname(path), name)) };
      return require(name);
    };
    for (const [code, target] of [[script.content, module], [template.code, rendered]] as const) {
      const js = ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
      new Function('require', 'module', 'exports', js)(load, target, target.exports);
    }
    module.exports.default.render = rendered.exports.render;
    module.exports.default.fixturePath = path;
    components.set(path, module.exports.default);
    return module.exports.default;
  }
  const node = (tag: string, text = ''): any => ({ tag, tagName: tag.toUpperCase(), text, props: {}, children: [], parent: null, getRootNode: () => doc, addEventListener() {}, removeEventListener() {} });
  const renderer = vue.createRenderer<any, any>({
    createElement: node, createText: text => node('#text', text), createComment: text => node('#comment', text),
    setText: (n, text) => n.text = text, setElementText: (n, text) => { n.text = text; n.children = []; },
    insert: (n, parent, anchor) => { if (n.parent) n.parent.children.splice(n.parent.children.indexOf(n), 1); n.parent = parent; const i = anchor ? parent.children.indexOf(anchor) : -1; if (i < 0) parent.children.push(n); else parent.children.splice(i, 0, n); },
    remove: n => { n.parent?.children.splice(n.parent.children.indexOf(n), 1); }, parentNode: n => n.parent,
    nextSibling: n => n.parent?.children[n.parent.children.indexOf(n) + 1], patchProp: (n, key, _old, value) => n.props[key] = value,
  });
  const app = renderer.createApp(options.core
    ? { render: () => vue.h(component(fileURLToPath(new URL('../src/pages/CorePages.vue', import.meta.url))), options.core) }
    : component(fileURLToPath(new URL('../src/App.vue', import.meta.url))));
  const root = node('root');
  app.mount(root); await tick();
  const instance = (app as any)._instance;
  const all = (): any[] => { const result: any[] = []; function walk(n: any) { result.push(n); for (const c of n.children) walk(c); } walk(root); return result; };
  function componentInstance(suffix: string): any {
    function walk(v: any): any {
      if (!v) return;
      if (v.component) {
        if (v.component.type.fixturePath?.endsWith(suffix)) return v.component;
        const result = walk(v.component.subTree); if (result) return result;
      }
      if (Array.isArray(v.children)) for (const child of v.children) { const result = walk(child); if (result) return result; }
    }
    const result = walk(instance.subTree); assert.ok(result, `actual ${suffix} must be mounted`); return result;
  }
  return { state: instance.setupState, dataInstance: () => componentInstance('/DataPages.vue'), componentInstance, all, document: doc, location, sessionReads: () => sessionReads,
    text: () => all().map(n => n.text).join(' '),
    form: () => { const form = all().find(n => n.tag === 'form'); assert.ok(form); return form; },
    logoutButton: () => { const button = all().find(n => n.tag === 'button' && n.text.trim() === '退出'); assert.ok(button); return button; },
    releaseLogout: () => releaseLogout(),
    close() { app.unmount(); for (const [key, old] of prior) { if (old) Object.defineProperty(globalThis, key, old); else Reflect.deleteProperty(globalThis, key); } },
  };
}

for (const scope of ['admin', 'family'] as const) {
  test(`${scope} login recovers the actual App after invalidation unmounts DataPages on an unchanged home hash`, async () => {
    const view = await mount(scope);
    try {
      const child = view.dataInstance();
      child.setupState.email = 'isolated-owner'; child.setupState.secret = 'isolated-password';
      const originalHash = view.location.hash;
      await view.form().props.onSubmit({ preventDefault() {} }); await tick();
      assert.equal(child.isUnmounted, true, 'real privacy invalidation must unmount the old form');
      assert.equal(view.location.hash, originalHash, 'no hashchange or foreground event rescues the parent');
      assert.equal(view.state.account?.email, 'isolated-owner');
      assert.equal(view.state.pageVisible, true);
      assert.equal(view.state.needsLogin, false);
      assert.match(view.text(), scope === 'admin' ? /管理总览/ : /家庭设备/);
      assert.doesNotMatch(view.text(), /页面内容已清理/);
    } finally { view.close(); }
  });
  test(`${scope} login-page logout recovers a usable guest form after its child unmounts`, async () => {
    const view = await mount(scope, true);
    try {
      const child = view.dataInstance();
      assert.equal(view.state.account.email, 'isolated-owner');
      const pending = view.logoutButton().props.onClick(); await tick();
      assert.equal(child.isUnmounted, true, 'logout must immediately discard private content before HTTP completion');
      view.releaseLogout(); await pending; await tick();
      assert.equal(view.state.account, null);
      assert.equal(view.state.pageVisible, true);
      assert.equal(view.state.needsLogin, true);
      assert.equal(view.dataInstance().setupState.loading, false);
      assert.ok(view.form());
      assert.doesNotMatch(view.text(), /页面内容已清理|当前账号：/);
      assert.ok(view.sessionReads() >= 3, 'completion rereads the actual session');
    } finally { view.close(); }
  });
}


function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}

// Isolated HTTP fixtures drive the real compiled CorePages and settings form.
async function settingsView(scope: Scope = 'family', canEdit = true) {
  const props = vue.reactive({ scope, route: { page: scope === 'family' ? 'settings' : 'configuration', device: 'device-one', chat: '', memory: '', family: '' } });
  let name = '初始昵称', location = '初始位置', version = 0;
  const reads: Array<{ scope: string; result: ReturnType<typeof deferred<Response>> }> = [];
  let deferLists = false, deferSettings = false, failSettings = false;
  const settingsReads: Array<{ result: ReturnType<typeof deferred<Response>>; response: Response }> = [];
  const detailSnapshots: Array<{ name: string; location: string }> = [];
  const response = (value: unknown) => new Response(JSON.stringify(value));
  const device = (id: string, deviceName = name, place = location) => ({ id, name: deviceName, location: place, model: 'isolated-board' });
  const view = await mount(scope, false, { core: props, resource: async (url, init) => {
    const selectedScope = url.split('/')[2];
    if (url.endsWith('/devices')) {
      if (!deferLists) return response([device(props.route.device)]);
      const result = deferred<Response>(); reads.push({ scope: selectedScope, result }); return result.promise;
    }
    const id = decodeURIComponent(url.split('/').at(-1)!);
    if (url.includes('/device-settings/')) {
      if (init.method === 'PUT') {
        const input = JSON.parse(String(init.body));
        if (input.version !== version.toString(16).padStart(64, '0'))
          return new Response(JSON.stringify({ message: '设备设置已变化' }), { status: 409 });
        name = input.name; location = input.location; version++;
      }
      const snapshot = response({ deviceId: id, name, location, version: version.toString(16).padStart(64, '0'), canEdit });
      if (init.method !== 'PUT' && failSettings) { failSettings = false; return new Response('{}', { status: 500 }); }
      if (init.method !== 'PUT' && deferSettings) {
        const result = deferred<Response>(); settingsReads.push({ result, response: snapshot }); return result.promise;
      }
      return snapshot;
    }
    if (url.includes('/device-runtime/')) return response({ deviceId: id, connection: 'unknown', seenAt: '', boot: '', canControl: false, volume: null, brightness: null, command: null });
    if (url.includes('/devices/')) {
      const snapshot = detailSnapshots.shift();
      return response(device(id, snapshot?.name, snapshot?.location));
    }
  } });
  const labels = () => ({
    heading: view.all().find(n => n.tag === 'h1')?.text,
    options: view.all().filter(n => n.tag === 'option').map(n => n.text),
  });
  const form = () => view.componentInstance('/DeviceSettings.vue');
  async function save(nextName: string, nextLocation: string) {
    const settings = form(); settings.setupState.state.name = nextName; settings.setupState.state.location = nextLocation;
    await settings.setupState.control.submit(); await tick();
    assert.match(view.text(), /设备昵称和放置位置已保存/);
    return settings;
  }
  return { ...view, props, reads, settingsReads, labels, form, save,
    deferSettings: () => { deferSettings = true; },
    failSettings: () => { failSettings = true; },
    deferLists: () => { deferLists = true; },
    current: (nextName: string, nextLocation: string) => { name = nextName; location = nextLocation; version++; },
    release: (index: number, deviceName: string, place: string, id = 'device-one') => {
      detailSnapshots.push({ name: deviceName, location: place });
      reads[index].result.resolve(response([device(id, deviceName, place)]));
    },
  };
}

for (const order of ['older-first', 'newer-first'] as const) {
  test(`confirmed settings save supersedes a pending parent refresh (${order})`, async () => {
    const view = await settingsView();
    try {
      const form = view.form(); view.deferLists();
      await view.save('保存甲', '位置甲');
      assert.equal(view.reads.length, 1);
      await view.save('保存乙', '位置乙');
      assert.equal(view.form(), form, 'silent refresh keeps the real form and verified feedback mounted');
      if (order === 'older-first') { view.release(0, '保存甲', '位置甲'); await tick(); }
      assert.equal(view.reads.length, 2, 'each confirmed save must start a mandatory fresh parent read');
      view.release(1, '保存乙', '位置乙'); await tick();
      if (order === 'newer-first') { view.release(0, '保存甲', '位置甲'); await tick(); }
      assert.deepEqual(view.labels(), { heading: '保存乙的设置', options: ['保存乙 · 位置乙'] });
      assert.equal(view.form(), form);
      assert.match(view.text(), /设备昵称和放置位置已保存/);
    } finally { view.close(); }
  });
}

for (const change of ['device', 'scope', 'route', 'account'] as const) {
  test(`pending saved-settings parent refresh cannot restore private labels after ${change} invalidation`, async () => {
    const view = await settingsView();
    try {
      view.deferLists();
      const previousForm = await view.save('旧账号昵称', '旧账号位置');
      if (change === 'account') sensitive.invalidateSensitive();
      else {
        view.current('当前昵称', '当前位置');
        if (change === 'device') view.props.route = { ...view.props.route, device: 'device-two' };
        if (change === 'scope') view.props.scope = 'admin';
        if (change === 'route') view.props.route = { ...view.props.route, page: 'devices' };
      }
      await tick();
      assert.equal(previousForm.isUnmounted, true, 'leaving the context destroys its form state');
      assert.equal(previousForm.setupState.state.name, '');
      assert.equal(previousForm.setupState.state.feedback, '');
      view.release(0, '旧账号昵称', '旧账号位置'); await tick();
      assert.doesNotMatch(view.text(), /旧账号昵称|旧账号位置|设备昵称和放置位置已保存/);
      if (change !== 'account') {
        assert.equal(view.reads.length, 2);
        assert.equal(view.reads[1].scope, change === 'scope' ? 'admin' : 'family');
        view.release(1, '当前昵称', '当前位置', change === 'device' ? 'device-two' : 'device-one'); await tick();
        assert.doesNotMatch(view.text(), /旧账号昵称|旧账号位置/);
      }
    } finally { view.close(); }
  });
}

for (const scope of ['family', 'admin'] as const) {
  for (const canEdit of [true, false]) {
    test(`${scope} manual settings reread updates the header and selector (canEdit=${canEdit})`, async () => {
      const view = await settingsView(scope, canEdit);
      try {
        const form = view.form();
        view.current('外部新昵称', '外部新位置');
        if (canEdit) {
          form.setupState.state.name = '旧草稿';
          await form.setupState.control.submit(); await tick();
          assert.equal(form.setupState.state.needsRead, true, '409 requires an explicit read');
        }
        view.deferLists();
        await form.setupState.control.refresh(); await tick();
        assert.equal(form.setupState.state.name, '外部新昵称');
        assert.equal(view.reads.length, 1, 'a verified manual read starts a mandatory parent refresh');
        assert.deepEqual(view.labels(), { heading: scope === 'family' ? '外部新昵称的设置' : '设备配置', options: ['外部新昵称 · 外部新位置'] });
        view.release(0, '外部新昵称', '外部新位置'); await tick();
        assert.equal(view.form(), form, 'the read keeps the same form mounted');
        assert.equal(form.setupState.state.settings.canEdit, canEdit);
        assert.doesNotMatch(view.text(), /设备昵称和放置位置已保存/);
      } finally { view.close(); }
    });
  }
}

for (const order of ['older-first', 'newer-first'] as const) {
  test(`manual settings read supersedes a pending saved-settings parent refresh (${order})`, async () => {
    const view = await settingsView();
    try {
      view.deferLists();
      const form = await view.save('先前保存', '先前位置');
      view.current('外部新昵称', '外部新位置');
      await form.setupState.control.refresh(); await tick();
      if (order === 'older-first') { view.release(0, '先前保存', '先前位置'); await tick(); }
      assert.equal(view.reads.length, 2, 'a manual read must supersede the older parent read');
      view.release(1, '外部新昵称', '外部新位置'); await tick();
      if (order === 'newer-first') { view.release(0, '先前保存', '先前位置'); await tick(); }
      assert.deepEqual(view.labels(), { heading: '外部新昵称的设置', options: ['外部新昵称 · 外部新位置'] });
      assert.equal(view.form(), form);
      assert.doesNotMatch(view.text(), /设备昵称和放置位置已保存/);
    } finally { view.close(); }
  });
}

test('a failed manual settings read cannot refresh parent metadata', async () => {
  const view = await settingsView();
  try {
    const before = view.labels(); view.deferLists(); view.failSettings();
    view.current('未经核实昵称', '未经核实位置');
    await view.form().setupState.control.refresh(); await tick();
    assert.equal(view.reads.length, 0);
    assert.deepEqual(view.labels(), before);
    assert.equal(view.form().setupState.state.needsRead, true);
    assert.doesNotMatch(view.text(), /未经核实昵称|未经核实位置/);
  } finally { view.close(); }
});

for (const change of ['device', 'scope', 'route', 'account', 'hide'] as const) {
  test(`a late manual settings read cannot restore metadata after ${change} invalidation`, async () => {
    const view = await settingsView();
    try {
      view.deferSettings(); view.current('迟到私有昵称', '迟到私有位置');
      const form = view.form(), pending = form.setupState.control.refresh(); await tick();
      assert.equal(view.settingsReads.length, 1);
      view.deferLists();
      if (change === 'account') sensitive.invalidateSensitive();
      else if (change === 'hide') { view.document.visibilityState = 'hidden'; view.document.dispatchEvent(new Event('visibilitychange')); }
      else {
        view.current('当前昵称', '当前位置');
        if (change === 'device') view.props.route = { ...view.props.route, device: 'device-two' };
        if (change === 'scope') view.props.scope = 'admin';
        if (change === 'route') view.props.route = { ...view.props.route, page: 'devices' };
      }
      await tick();
      assert.equal(form.setupState.state.name, '');
      assert.equal(form.setupState.state.feedback, '');
      const count = view.reads.length;
      view.settingsReads[0].result.resolve(view.settingsReads[0].response); await pending; await tick();
      assert.equal(view.reads.length, count, 'a discarded read cannot emit a parent refresh');
      assert.doesNotMatch(view.text(), /迟到私有昵称|迟到私有位置/);
    } finally { view.close(); }
  });
}
