import { test } from "node:test";
import assert from "node:assert/strict";
let app: any, definition: any, scanner: any;
(globalThis as any).App = (value: any) => {
  app = value;
};
(globalThis as any).getApp = () => app;
(globalThis as any).Page = (value: any) => {
  definition = value;
};
(globalThis as any).wx = {
  scanCode: (value: any) => {
    scanner = value;
  },
};
await import("../src/app.ts");
await import("../src/pages/index/index.ts");
function page(options: any = {}) {
  const p: any = {
    ...definition,
    data: { ...definition.data },
    setData(value: any) {
      Object.assign(this.data, value);
    },
  };
  p.onLoad(options);
  return p;
}
const code = "a".repeat(32),
  next = "b".repeat(32);
test("App callbacks use their native receiver before getApp registration", () => {
  const previous = (globalThis as any).getApp;
  const events: unknown[] = [];
  const receiver = {
    ...app,
    onboardingLaunch: (options: any) => events.push(options),
    onboardingCleanup: () => events.push("hidden"),
  };
  const options = { path: "pages/index/index", query: { scene: code } };
  (globalThis as any).getApp = () => undefined;
  try {
    assert.doesNotThrow(() => receiver.onShow(options));
    receiver.onHide();
    assert.deepEqual(events, [options, "hidden"]);
    receiver.onboardingLaunch = undefined;
    receiver.onboardingCleanup = undefined;
    assert.doesNotThrow(() => receiver.onShow(options));
    assert.doesNotThrow(() => receiver.onHide());
  } finally {
    (globalThis as any).getApp = previous;
  }
});
for (const prefix of ["", "/"]) {
  test(`WX_CODE callback accepts exact owned ${prefix}page scene path`, () => {
    const p = page();
    p.scan();
    scanner.success({
      scanType: "WX_CODE",
      path: `${prefix}pages/index/index?scene=${code}`,
      result: "",
    });
    assert.equal(p.controller.code, code);
    assert.equal(p.data.scanned, true);
    p.onUnload();
  });
}
test("camera scan requests wxCode in addition to plain qrCode", () => {
  const p = page();
  p.scan();
  assert.deepEqual(scanner.scanType, ["qrCode", "wxCode"]);
  p.onUnload();
});
test("untrusted or malformed mini-code paths cannot fall back to valid raw content", () => {
  for (const path of [
    `https://evil.test/pages/index/index?scene=${code}`,
    `pages/other/index?scene=${code}`,
    `pages/index/index?scene=${code}&x=1`,
    `pages/index/index?x=1&scene=${code}`,
    `pages/index/index?scene=${code}#fragment`,
    `pages/index/index?scene=${code}&scene=${next}`,
    `pages/index/index?scene=${code.toUpperCase()}`,
    `pages/index/index?scene=%ZZ`,
    `pages/index/index?scene=${"a".repeat(10000)}`,
  ]) {
    const p = page();
    p.scan();
    scanner.success({ scanType: "WX_CODE", path, result: code });
    assert.equal(p.controller.code, "", path.slice(0, 80));
    assert.equal(p.data.scanned, false);
    assert.match(p.data.error, /设备码|格式/);
    p.onUnload();
  }
});
test("warm App onShow supersedes old scanner and queues only until page visible", async () => {
  const p = page();
  const starts:any[]=[]; p.controller.start=async(...args:any[])=>starts.push(args);
  p.controller.api.cookie = "management_family=synthetic";
  p.controller.view.families = [
    { id: "family", name: "test", canAddDevices: true },
  ];
  p.scan();
  const old = scanner;
  p.onHide();
  old.success({ result: code });
  const queued = p.scanOperation;
  app.onShow?.({
    path: "pages/index/index",
    query: { scene: next },
    scene: 1047,
  });
  assert.equal(p.controller.code, "");
  assert.equal(queued.code, undefined);
  assert.equal(p.data.scanned, false);
  p.onShow();
  assert.equal(p.controller.code, next);
  assert.equal(p.data.scanned, false);
  assert.deepEqual(starts, [["family", ""]]);
  old.success({ result: code });
  assert.equal(p.controller.code, next);
  assert.equal(p.controller.api.cookie, "management_family=synthetic");
  await p.cancel();
  p.onShow();
  assert.equal(p.controller.code, "");
  p.onUnload();
});
test("warm launch consumes once and cancellation/unload/session expiry erase hidden scene", async () => {
  for (const cleanup of ["cancel", "onUnload", "session"]) {
    const p = page();
    p.onHide();
    app.onShow?.({
      path: "/pages/index/index",
      query: { scene: code },
      scene: 1047,
    });
    const queued = p.scanOperation;
    assert.ok(queued, "warm launch must create a bounded page operation");
    if (cleanup === "session") {
      (globalThis as any).wx.request = (o: any) =>
        o.success({ statusCode: 401, data: {} });
      await assert.rejects(p.controller.api.families(), /重新登录/);
    } else await p[cleanup]();
    assert.equal(queued.code, undefined);
    p.onShow();
    assert.equal(p.controller.code, "");
    p.onUnload();
  }
  const p = page();
  p.onUnload();
  app.onShow?.({
    path: "pages/index/index",
    query: { scene: next },
    scene: 1047,
  });
  const replacement = page();
  replacement.onShow();
  assert.equal(replacement.controller.code, "");
  replacement.onUnload();
});
test("warm launch rejects arbitrary page/query and ordinary show does not restore a scene", () => {
  const p = page();
  for (const options of [
    { path: "https://evil.test", query: { scene: code } },
    { path: "pages/other/index", query: { scene: code } },
    { path: "pages/index/index", query: { scene: code, extra: "x" } },
    { path: "pages/index/index", query: { scene: [code] } },
    { path: "pages/index/index", query: { scene: "%ZZ" } },
    { path: "pages/index/index", query: {} },
  ]) {
    app.onShow?.(options);
    p.onShow();
    assert.equal(p.controller.code, "");
  }
  p.onUnload();
});
test("App hide reaches page cleanup across app/page modules", () => {
  const p = page({ scene: code });
  p.controller.password = "synthetic";
  p.setData({ password: "synthetic", loginPassword: "synthetic" });
  app.onHide();
  assert.equal(p.active, false);
  assert.equal(p.controller.code, "");
  assert.equal(p.controller.password, "");
  assert.equal(p.data.password, "");
  assert.equal(p.data.loginPassword, "");
  p.onUnload();
});
test("cold load scene is accepted without retaining an App-level code", () => {
  app.onShow?.({
    path: "pages/index/index",
    query: { scene: code },
    scene: 1047,
  });
  const p = page({ scene: code });
  assert.equal(p.controller.code, code);
  p.onUnload();
});

test("foreground scan automatically starts once; multiple families wait for selection", async()=>{
 for(const count of [1,2]) {
 const p=page(); const calls:any[]=[];
 p.controller.view.families=Array.from({length:count},(_,i)=>({id:`family-${i}`,name:`家${i}`,canAddDevices:true}));
 p.data.families=p.controller.view.families; p.data.loggedIn=true;
 p.controller.start=async(...args:any[])=>calls.push(args);
 p.scan(); scanner.success({result:code}); p.onShow();
 assert.equal(calls.length,count===1?1:0);
 if(count===2){p.family({detail:{value:"1"}}); assert.deepEqual(calls,[["family-1",""]]);}
 p.onUnload();
 }
});
test("scan before login continues after successful login",async()=>{
 const p=page();const calls:any[]=[];
 p.scan();scanner.success({result:code});
 p.controller.login=async()=>{p.controller.view.families=[{id:"home",name:"家",canAddDevices:true}];p.data.families=p.controller.view.families;p.data.loggedIn=true;};
 p.controller.start=async(...args:any[])=>calls.push(args);
 await p.login();assert.deepEqual(calls,[["home",""]]);p.onUnload();
});
test("prelogin code survives initial username input but cancel during login cannot revive it",async()=>{
 const p=page({scene:code});const calls:any[]=[];let finish:any;
 p.input({currentTarget:{dataset:{key:"username"}},detail:{value:"f"}});
 p.input({currentTarget:{dataset:{key:"username"}},detail:{value:"first-user"}});
 assert.equal(p.readyCode,code);
 p.controller.login=()=>new Promise(resolve=>{finish=()=>{p.data.families=[{id:"home"}];resolve(undefined);};});
 p.controller.start=async(...args:any[])=>calls.push(args);
 const login=p.login();await p.cancel();finish();await login;
 assert.deepEqual(calls,[]);assert.equal(p.readyCode,"");p.onUnload();
});
test("confirmed task bypasses Bluetooth and preserves server identity",async()=>{
 const p=page();p.controller.view.families=[{id:"home"}];p.controller.setCode(code);
 p.controller.api.start=async()=>({status:"confirmed",device_id:"dev_"+"1".repeat(32)});
 await p.controller.start("home","");assert.equal(p.controller.view.state.phase,"success");assert.equal(p.controller.link,undefined);p.onUnload();
});
test("prelogin scanned code has an active absolute expiry timer",()=>{
 const p=page();p.scan();scanner.success({result:code});
 assert.ok(p.scanTimer,"retained code must keep its bounded erase timer");p.onUnload();
});
test("session invalidation erases a foreground code waiting for family selection",async()=>{
 const p=page();p.scan();scanner.success({result:code});
 (globalThis as any).wx.request=(o:any)=>o.success({statusCode:401,data:{}});
 await assert.rejects(p.controller.api.families(),/重新登录/);
 const retained=p.readyCode;p.onUnload();assert.equal(retained,"");
});

for (const changeAccount of [false, true]) {
  test(`pending login serializes native auth and ${changeAccount ? "invalidates changed account" : "continues retained scan once"}`, async () => {
    const native = (globalThis as any).wx;
    const previous = native.request;
    const requests: any[] = [];
    native.request = (request: any) => requests.push(request);
    const p = page({ scene: code });
    const settle = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
    try {
      p.input({ currentTarget: { dataset: { key: "username" } }, detail: { value: "synthetic-account" } });
      p.setData({ loginPassword: "synthetic-password" });
      const first = p.login();
      p.setData({ loginPassword: "synthetic-password-again" });
      const duplicate = p.login();
      assert.equal(requests.length, 1, "duplicate submit must not create a second auth request");
      assert.equal(p.data.loginPending, true);
      assert.equal(p.readyCode, code, "duplicate submit retains prelogin code");
      if (changeAccount) p.input({ currentTarget: { dataset: { key: "username" } }, detail: { value: "different-synthetic-account" } });
      requests[0].success({ statusCode: 200, data: { ok: true }, cookies: ["management_family=synthetic"] });
      await settle();
      assert.equal(requests.length, 2);
      assert.match(requests[1].url, /\/families$/);
      await duplicate;
      assert.equal(p.data.loginPending, true, "duplicate completion cannot clear pending feedback");
      requests[1].success({ statusCode: 200, data: [{ id: "home", name: "家", canAddDevices: true }] });
      await first;
      await settle();
      assert.equal(p.data.loginPending, false);
      const starts = requests.filter(r => r.url.endsWith("/onboarding"));
      assert.equal(starts.length, changeAccount ? 0 : 1);
      assert.equal(p.readyCode, "");
      if (!changeAccount) {
        assert.deepEqual(starts[0].data, { family_id: "home", claim_code: code });
        starts[0].success({ statusCode: 200, data: { id: "synthetic-task", status: "confirmed", device_id: "dev_" + "a".repeat(32) } });
        await settle();
        assert.equal(p.data.phase, "success");
      }
    } finally {
      p.onUnload();
      native.request = previous;
    }
  });
}
