import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import * as vue from "vue";
import { parse, compileScript } from "@vue/compiler-sfc";
import ts from "typescript";
import { createRemoteApi } from "../src/remote-api.ts";
import * as control from "../src/runtime-control.ts";
import * as sensitive from "../src/sensitive-session.ts";

async function mount(scope: "admin" | "family") {
  const keys = ["location", "document", "window"] as const;
  const prior = keys.map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const);
  const location = { hash: "#/login", href: "", pathname: scope === "admin" ? "/index.html" : "/family.html" };
  Object.defineProperty(globalThis, "location", { configurable: true, value: location });
  Object.defineProperty(globalThis, "document", { configurable: true, value: { visibilityState: "visible" } });
  Object.defineProperty(globalThis, "window", { configurable: true, value: new EventTarget() });
  let authenticated = false;
  let loginCalls = 0;
  const api = createRemoteApi((async (url: string | URL | Request, init?: RequestInit) => {
    if (String(url).endsWith("/login")) {
      loginCalls++;
      authenticated = JSON.parse(String(init?.body)).password === "fixture-correct";
      if (!authenticated) return new Response(JSON.stringify({ code: "invalid_login", message: "账号或密码不正确" }), { status: 401 });
    }
    return authenticated
      ? new Response(JSON.stringify({ email: "fixture", scope, role: "家庭管理员" }))
      : new Response(JSON.stringify({ code: "unauthenticated", message: "请先登录" }), { status: 401 });
  }) as typeof fetch);
  const source = readFileSync(new URL("../src/pages/DataPages.vue", import.meta.url), "utf8");
  const { descriptor } = parse(source);
  const script = compileScript(descriptor, { id: "guest-login-fixture" });
  const code = ts.transpileModule(script.content, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
  const require = createRequire(import.meta.url), module = { exports: {} as any };
  new Function("require", "module", "exports", code)((name: string) =>
    name === "vue" ? vue : name === "../api.ts" ? { api, liveMode: true } :
    name === "../runtime-control.ts" ? control : name === "../sensitive-session.ts" ? sensitive :
    name === "../navigation.ts" ? { linkTo: (_scope: string, page: string) => `#/${page}` } :
    name.endsWith(".css") ? {} : require(name), module, module.exports);
  const component = module.exports.default;
  component.render = () => null;
  const renderer = vue.createRenderer<any, any>({ createElement: () => ({}), insert() {}, remove() {}, createText: () => ({}), createComment: () => ({}), setText() {}, setElementText() {}, parentNode: () => null, nextSibling: () => null, patchProp() {} });
  const app = renderer.createApp(component, { scope, route: { page: "login", device: "", family: "", chat: "", memory: "" } });
  app.mount({});
  await new Promise(resolve => setImmediate(resolve));
  const state = (app as any)._instance.setupState;
  return { state, location, loginCalls: () => loginCalls, close() {
    app.unmount();
    for (const [key, old] of prior) { if (old) Object.defineProperty(globalThis, key, old); else Reflect.deleteProperty(globalThis, key); }
  } };
}

for (const scope of ["admin", "family"] as const) {
  test(`${scope} guest login finishes loading and retains a rejected-login message`, async () => {
    const page = await mount(scope);
    try {
      assert.equal(page.state.loading, false, "anonymous getAccount must not invalidate its own page load");
      assert.equal(page.state.error, "");
      assert.equal(page.state.email, "", "live login must not prefill an obsolete account name");
      page.state.email = "fixture";
      page.state.secret = "fixture-wrong";
      await page.state.login();
      assert.equal(page.loginCalls(), 1);
      assert.match(page.state.feedback, /账号或密码/);
      assert.equal(page.state.saving, false);
      page.state.secret = "fixture-correct";
      await page.state.login();
      assert.equal(page.loginCalls(), 2);
      assert.equal(page.location.href, scope === "admin" ? "#/overview" : "#/home");
    } finally { page.close(); }
  });
}
