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
  assert.equal(p.data.scanned, true);
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
