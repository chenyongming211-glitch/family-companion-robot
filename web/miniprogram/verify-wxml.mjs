import assert from "node:assert/strict";
import vm from "node:vm";

// Native WXML evaluates raw mustache source; HTML entities are not decoded
// into JavaScript operators. Inspect quoted attributes directly so comparisons
// containing > or >= cannot truncate an opening tag.
export function verifyBindings(wxml) {
  for (const attribute of wxml.matchAll(/([\w:-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')/g)) {
    const name = attribute[1];
    const value = attribute[2] ?? attribute[3];
    if (name === "wx:if" || name === "wx:elif") {
      assert.match(value, /^\{\{[\s\S]+\}\}$/, `${name} must be a native expression binding`);
    }
    const bindings = [...value.matchAll(/\{\{([\s\S]*?)\}\}/g)];
    const rest = value.replace(/\{\{[\s\S]*?\}\}/g, "");
    assert.ok(!rest.includes("{{") && !rest.includes("}}"), `${name}: incomplete native expression binding`);
    for (const binding of bindings) {
      assert.doesNotThrow(() => new vm.Script(`(${binding[1]})`), `${name}: invalid native binding expression ${binding[0]}`);
    }
  }
}

// This checks native expression delimiters and evaluates the actual success
// text's ancestor conditions. It models wx:if truthiness, not WeChat rendering.
export function verifyWxml(wxml, initialData) {
  verifyBindings(wxml);
  const successConditions = [];
  const stack = [];
  for (const match of wxml.matchAll(/<\/?[a-z](?:[^>"']|"[^"]*"|'[^']*')*>|[^<]+/gi)) {
    const token = match[0];
    if (token.startsWith("</")) {
      stack.pop();
      continue;
    }
    if (token.startsWith("<")) {
      const raw = /\bwx:if="([^"]*)"/.exec(token)?.[1];
      if (raw !== undefined) {
        assert.match(
          raw,
          /^\{\{[\s\S]+\}\}$/,
          "wx:if must be a native expression binding",
        );
      }
      if (!token.endsWith("/>")) stack.push(raw?.slice(2, -2) || "true");
    } else if (
      token.includes("设备添加成功") ||
      token.includes("云端已确认绑定")
    ) {
      successConditions.push([...stack]);
    }
  }
  assert.equal(
    successConditions.length,
    2,
    "both success claims must be checked",
  );
  const visible = (conditions, data) =>
    conditions.every(
      (expression) =>
        !!vm.runInNewContext(`(${expression})`, { ...data }, { timeout: 100 }),
    );
  for (const conditions of successConditions) {
    assert.equal(
      visible(conditions, initialData),
      false,
      "initial page must not claim success",
    );
    for (const phase of [
      "idle",
      "connecting",
      "wifi",
      "applying",
      "waiting_cloud",
      "error",
      "success",
    ]) {
      for (const loggedIn of [false, true]) {
        for (const error of ["", "synthetic feedback"]) {
          assert.equal(
            visible(conditions, { ...initialData, phase, loggedIn, error }),
            phase === "success",
            `success claim visibility in phase ${phase}`,
          );
        }
      }
    }
  }
}
