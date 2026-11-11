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
import * as audit from '../src/audit-control.ts';

const tick = async () => { await vue.nextTick(); await new Promise(r => setImmediate(r)); await vue.nextTick(); };

async function mount(scope: 'admin' | 'family', loggedIn = false) {
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
  const api = createRemoteApi((async (input: any) => {
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
  const app = renderer.createApp(component(fileURLToPath(new URL('../src/App.vue', import.meta.url))));
  const root = node('root');
  app.mount(root); await tick();
  const instance = (app as any)._instance;
  const all = (): any[] => { const result: any[] = []; function walk(n: any) { result.push(n); for (const c of n.children) walk(c); } walk(root); return result; };
  function dataInstance(): any {
    function walk(v: any): any {
      if (!v) return;
      if (v.component) {
        if (v.component.type.fixturePath?.endsWith('/DataPages.vue')) return v.component;
        const result = walk(v.component.subTree); if (result) return result;
      }
      if (Array.isArray(v.children)) for (const child of v.children) { const result = walk(child); if (result) return result; }
    }
    const result = walk(instance.subTree); assert.ok(result, 'actual DataPages must be mounted'); return result;
  }
  return { state: instance.setupState, dataInstance, location, sessionReads: () => sessionReads,
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
      assert.match(view.text(), scope === 'admin' ? /管理总览/ : /和家人一起/);
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
