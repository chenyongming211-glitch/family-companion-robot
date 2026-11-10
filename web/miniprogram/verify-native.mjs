import assert from "node:assert/strict";
import { readFile, access } from "node:fs/promises";
import vm from "node:vm";
import { verifyWxml, verifyBindings } from "./verify-wxml.mjs";
const config = JSON.parse(await readFile("dist/app.json", "utf8"));
let page, app;
const context = {
  Page: (p) => {
    page = p;
  },
  App: (value) => {
    // Native App construction can dispatch the first onShow before getApp()
    // exposes the instance. The callback must use its actual receiver.
    assert.doesNotThrow(() =>
      value.onShow({ path: "pages/index/index", query: {} }),
    );
    app = value;
  },
  getApp: () => app,
  BigInt: undefined,
  wx: {},
  setTimeout,
  clearTimeout,
  require: () => assert.fail("Security2 must not load before capability check"),
};
function runNativeEntry(source) {
  const module = { exports: {} };
  const entry = vm.runInNewContext(
    "(function(require, module, exports) {\n" + source + "\n})",
    context,
  );
  entry(context.require, module, module.exports);
}
runNativeEntry(await readFile("dist/app.js", "utf8"));
assert.equal(typeof app.onShow, "function");
const main = await readFile("dist/pages/index/index.js", "utf8");
assert.match(
  main,
  /\brequire\(["']\.\.\/\.\.\/provisioning\/security2\.js["']\)/,
  "WeChat's static dependency scanner must see the deferred crypto require",
);
assert.ok(
  !/\b\d+n\b/.test(main),
  "entry bundle must parse on engines without BigInt literals",
);
runNativeEntry(main);
assert.equal(typeof page.onLoad, "function");
const onboardingPage = page;
let sharedApi;
const bindingFailures = [];
for (const route of config.pages) {
  runNativeEntry(await readFile("dist/" + route + ".js", "utf8"));
  for (const ext of ["js", "json", "wxml"])
    await access("dist/" + route + "." + ext);
  if (route !== "pages/index/index") {
    const instance = {...page, data:{...page.data}, setData(value) { Object.assign(this.data,value); }};
    instance.onLoad({device:"dev_"+"a".repeat(32),chat:"chat_"+"b".repeat(64)});
    if (sharedApi) assert.equal(instance.api, sharedApi, "separate native page bundles must share App Api");
    sharedApi = instance.api;
    instance.onUnload();
  }
  const wxml = await readFile("dist/" + route + ".wxml", "utf8");
  try { verifyBindings(wxml); } catch (error) { bindingFailures.push(`${route}: ${error.message}`); }
  for (const match of wxml.matchAll(
    /bind(?:tap|input|change)="([a-zA-Z0-9_]+)"/g,
  ))
    assert.equal(typeof page[match[1]], "function", match[1]);
}
assert.deepEqual(bindingFailures, [], "every native page must have valid WXML binding expressions");
// Exercise the memory page through its independent native bundle with synthetic
// fixtures. This checks App RAM sharing, actual handlers, bindings and cleanup.
assert.ok(config.pages.includes("pages/memories/memories"));
runNativeEntry(await readFile("dist/pages/memories/memories.js", "utf8"));
const memoryPage = { ...page, data: { ...page.data }, setData(value) { Object.assign(this.data, value); } };
const memoryDevice = "dev_" + "a".repeat(32), memoryChat = "chat_" + "b".repeat(64);
const memoryCalls = [], memoryModals = [], memoryUrls = [];
context.wx.request = (o) => {
  memoryCalls.push(o);
  const path = o.url.split("/api/family")[1];
  const data = path === "/login" ? {} : path.startsWith("/devices/") ? { id: memoryDevice } :
    path.startsWith("/device-settings/") ? { deviceId: memoryDevice, name: "fixture", location: "", version: "c".repeat(64), canEdit: true } :
    path.startsWith("/memory-clear/") ? { deviceId: memoryDevice, status: "confirmed" } :
    [{ id: "mem-" + "d".repeat(32) + "-1", deviceId: memoryDevice, date: "2026-11-11T01:02:03Z", text: "fixture", user: "fixture user", assistant: "fixture spoken", chatId: memoryChat }];
  o.success({ statusCode: 200, data, cookies: path === "/login" ? ["management_family=synthetic-native"] : [] });
};
context.wx.showModal = (o) => memoryModals.push(o);
context.wx.navigateTo = (o) => memoryUrls.push(o.url);
memoryPage.onLoad({ device: memoryDevice });
assert.equal(memoryPage.api, sharedApi);
await memoryPage.api.login("synthetic", "synthetic");
await memoryPage.onShow();
assert.equal(memoryPage.data.items[0].assistant, "fixture spoken");
assert.equal(memoryPage.data.canClear, true);
const item = memoryPage.data.items[0];
memoryPage.openMemorySource({ currentTarget: { dataset: { index: 0, id: item.id, epoch: item.epoch } } });
assert.equal(memoryUrls[0], "/pages/chat/chat?device=" + memoryDevice + "&chat=" + memoryChat);
const hiddenClear = memoryPage.clearMemories();
void memoryPage.clearMemories();
assert.equal(memoryModals.length, 1);
memoryPage.onHide();
assert.equal(memoryPage.data.items.length, 0);
assert.equal(memoryPage.data.canClear, false);
assert.equal(memoryPage.data.notice, "");
memoryModals[0].success({ confirm: true });
await hiddenClear;
assert.equal(memoryCalls.filter(o => o.method === "POST" && o.url.includes("/memory-clear/")).length, 0);
await memoryPage.onShow();
const confirmedClear = memoryPage.clearMemories();
memoryModals[1].success({ confirm: true });
await confirmedClear;
const nativePosts = memoryCalls.filter(o => o.method === "POST" && o.url.includes("/memory-clear/"));
assert.equal(nativePosts.length, 1);
assert.equal(JSON.stringify(nativePosts[0].data), '{"confirm":true}');
assert.match(memoryPage.data.notice, /已清除近期记忆/);
assert.equal(memoryPage.data.items.length, 1, "a fresh conversation may add memory after a confirmed clear");
const memoryWxml = await readFile("dist/pages/memories/memories.wxml", "utf8");
const clearButton = /<button\b([^>]*bindtap="clearMemories"[^>]*)>/.exec(memoryWxml);
assert.ok(clearButton);
const clearCondition = /wx:if="{{([^}]+)}}"/.exec(clearButton[1]);
assert.ok(clearCondition);
for (const canClear of [false, true]) assert.equal(!!vm.runInNewContext(clearCondition[1], { canClear }), canClear);
assert.match(clearButton[1], /disabled="{{[^}]*clearing[^}]*}}"/);
const nativeStyle = await readFile("dist/app.wxss", "utf8");
for (const name of ["memory-source", "memory-clear", "family-notice"]) assert.ok(nativeStyle.includes("." + name));
memoryPage.onUnload();
page = onboardingPage;
verifyWxml(await readFile("dist/pages/index/index.wxml", "utf8"), page.data);
// The entry points are independent native bundles. Verify their bridge through
// the native App instance, rather than a shared source-module test cache.
const instance = {
  ...page,
  data: { ...page.data },
  setData(value) {
    Object.assign(this.data, value);
  },
};
instance.onLoad({});
instance.onHide();
app.onShow({
  path: "pages/index/index",
  query: { scene: "a".repeat(32) },
  scene: 1047,
});
assert.equal(instance.controller.code, "");
instance.onShow();
assert.equal(instance.controller.code, "a".repeat(32));
instance.setData({ password: "synthetic" });
app.onHide();
assert.equal(instance.controller.code, "");
assert.equal(instance.data.password, "");
instance.onUnload();
assert.equal(app.onboardingLaunch, undefined);
assert.equal(app.onboardingCleanup, undefined);
const crypto = await readFile("dist/provisioning/security2.js", "utf8");
assert.ok(
  !/\?\.|\?\?|\b\d+n\b/.test(crypto),
  "WeChat's packager requires ES2018 syntax even for deferred BigInt code",
);
const module = { exports: {} };
vm.runInNewContext(crypto, {
  module,
  exports: module.exports,
  Uint8Array,
  Uint32Array,
  BigInt,
  DataView,
  TextEncoder,
  console,
});
assert.equal(typeof module.exports.Security2Session, "function");
const vector = JSON.parse(await readFile("tests/security2-vectors/official.json", "utf8"));
const bytes = (hex) => Uint8Array.from(Buffer.from(hex, "hex"));
const secure = new module.exports.Security2Session(vector.username, vector.password, async () => bytes(vector.random));
let step = 0;
await secure.establish(async (request) => {
  assert.deepEqual(request, bytes(step === 0 ? vector.cmd0 : vector.cmd1));
  return bytes(step++ === 0 ? vector.resp0 : vector.resp1);
});
assert.deepEqual(secure.encrypt(bytes(vector.plain)), bytes(vector.encrypted));
assert.deepEqual(secure.decrypt(bytes(vector.cipher_reply)), bytes(vector.reply));
secure.destroy();
console.log(
  "native artifact: independent App/page scene and cleanup bridge, routes, memory source/confirmed clear/privacy lifecycle, WXML handlers and phase-bound success visibility, BigInt-free initial bundle and deferred pure-JS Security2 module verified",
);
