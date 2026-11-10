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
  "native artifact: independent App/page scene and cleanup bridge, routes, WXML handlers and phase-bound success visibility, BigInt-free initial bundle and deferred pure-JS Security2 module verified",
);
