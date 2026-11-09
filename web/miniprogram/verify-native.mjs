import assert from "node:assert/strict";
import { readFile, access } from "node:fs/promises";
import vm from "node:vm";
import { verifyWxml } from "./verify-wxml.mjs";
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
vm.runInNewContext(await readFile("dist/app.js", "utf8"), context);
assert.equal(typeof app.onShow, "function");
const main = await readFile("dist/pages/index/index.js", "utf8");
assert.ok(
  !/\b\d+n\b/.test(main),
  "entry bundle must parse on engines without BigInt literals",
);
vm.runInNewContext(main, context);
assert.equal(typeof page.onLoad, "function");
for (const route of config.pages) {
  for (const ext of ["js", "json", "wxml"])
    await access("dist/" + route + "." + ext);
  const wxml = await readFile("dist/" + route + ".wxml", "utf8");
  for (const match of wxml.matchAll(
    /bind(?:tap|input|change)="([a-zA-Z0-9_]+)"/g,
  ))
    assert.equal(typeof page[match[1]], "function", match[1]);
}
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
console.log(
  "native artifact: independent App/page scene and cleanup bridge, routes, WXML handlers and phase-bound success visibility, BigInt-free initial bundle and deferred pure-JS Security2 module verified",
);
