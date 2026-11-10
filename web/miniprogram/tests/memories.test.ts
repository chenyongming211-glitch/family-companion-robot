import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import { familyPage } from "../src/family-page.ts";
import { verifyBindings } from "../verify-wxml.mjs";
const device = "dev_" + "a".repeat(32), other = "dev_" + "c".repeat(32), chat = "chat_" + "b".repeat(64);
const row = { id: "mem-" + "d".repeat(32) + "-1", deviceId: device, conversationId: "legacy-source", text: "转写", date: "2026-11-11T01:02:03Z", user: "用户转写", assistant: "实际已播摘录", chatId: chat, turnId: "d".repeat(32) + ":1" };
const settings = { deviceId: device, name: "机器人", location: "", version: "e".repeat(64), canEdit: true };
function native(options: any = {}) {
  const app: any = {}, calls: any[] = [], modals: any[] = [], urls: string[] = [];
  (globalThis as any).getApp = () => app;
  const wx: any = {
    request(o: any) {
      calls.push(o);
      const path = o.url.split("/api/family")[1];
      if (path === "/login") return o.success({ statusCode: 200, data: {}, cookies: ["management_family=synthetic"] });
      if (options.respond?.(o, path)) return;
      let data: any = path.startsWith("/devices/") ? { id: device, familyId: "family-a" } : path.startsWith("/device-settings/") ? { ...settings, canEdit: options.canEdit ?? true } : path.startsWith("/memory-clear/") ? { deviceId: device, status: "confirmed" } : options.items ?? [row];
      o.success({ statusCode: 200, data });
    },
    showModal(o: any) { modals.push(o); if (options.confirm !== undefined) o.success({ confirm: options.confirm }); },
    navigateTo(o: any) { urls.push(o.url); },
  };
  (globalThis as any).wx = wx;
  const p: any = familyPage("memories" as any);
  p.data = { ...p.data };
  p.setData = (v: any) => Object.assign(p.data, v);
  p.onLoad({ device });
  return { p, calls, modals, urls, app, wx, posts: () => calls.filter(o => o.method === "POST" && o.url.includes("/memory-clear/")) };
}
async function shown(options: any = {}) {
  const n = native(options);
  await n.p.api.login("synthetic", "synthetic");
  await n.p.onShow();
  return n;
}
function sourceEvent(p: any, index = 0) { return { currentTarget: { dataset: { index, id: p.data.items[index]?.id, epoch: p.data.items[index]?.epoch } } }; }
async function tick() { for (let i = 0; i < 20; i++) await Promise.resolve(); }
test("memory page reads authorized device, actual spoken excerpt and settings", async () => {
  const { p, calls } = await shown();
  assert.equal(p.data.items.length, 1);
  assert.equal(p.data.items[0].assistant, row.assistant);
  assert.equal(p.data.items[0].user, row.user);
  assert.match(p.data.items[0].timeLabel, /2026-11-11/);
  assert.equal(p.data.canClear, true);
  assert.deepEqual(calls.slice(1).map(o => o.url.split("/api/family")[1]), ["/devices/" + device, "/memories?device=" + device, "/device-settings/" + device]);
});
test("ordinary member reads memory without clear control or mutation", async () => {
  const n = await shown({ canEdit: false });
  assert.equal(n.p.data.items.length, 1);
  assert.equal(n.p.data.canClear, false);
  await n.p.clearMemories();
  assert.equal(n.modals.length, 0);
  assert.equal(n.posts().length, 0);
});
test("settings read failure keeps authorized memory but disables clearing", async () => {
  const n = await shown({ respond(o: any, path: string) { if (!path.startsWith("/device-settings/")) return false; o.fail(); return true; } });
  assert.equal(n.p.data.items.length, 1);
  assert.equal(n.p.data.canClear, false);
  await n.p.clearMemories();
  assert.equal(n.modals.length, 0);
});
test("empty memory remains a successful authorized read", async () => {
  const n = await shown({ items: [] });
  assert.deepEqual(n.p.data.items, []);
  assert.equal(n.p.data.error, "");
  assert.equal(n.p.data.canClear, true);
});
test("body denial and another family's device cannot display or clear memory", async () => {
  for (const mismatch of [false, true]) {
    const n = await shown({ respond(o: any, path: string) {
      if (mismatch && path.startsWith("/devices/")) { o.success({ statusCode: 200, data: { id: other } }); return true; }
      if (!mismatch && path.startsWith("/memories?")) { o.success({ statusCode: 403, data: {} }); return true; }
      return false;
    } });
    assert.deepEqual(n.p.data.items, []);
    assert.equal(n.p.data.canClear, false);
    assert.ok(n.p.data.error);
    await n.p.clearMemories();
    assert.equal(n.posts().length, 0);
  }
});
test("memory payload from another device or malformed row is rejected", async () => {
  for (const items of [[{ ...row, deviceId: other }], [{ ...row, assistant: 4 }], {}, Array(25).fill(row)]) {
    const n = await shown({ items });
    assert.deepEqual(n.p.data.items, []);
    assert.equal(n.p.data.canClear, false);
    assert.ok(n.p.data.error);
  }
});
test("source handler uses only the selected current row's valid authorized chat ID", async () => {
  const n = await shown();
  n.p.openMemorySource(sourceEvent(n.p));
  assert.deepEqual(n.urls, ["/pages/chat/chat?device=" + device + "&chat=" + chat]);
  assert.equal(n.calls.filter(o => o.url.includes("/chats")).length, 0);
});
test("missing or malformed source never guesses from legacy conversation ID", async () => {
  for (const chatId of [undefined, "chat_bad"]) {
    const n = await shown({ items: [{ ...row, chatId, conversationId: chat }] });
    n.p.openMemorySource(sourceEvent(n.p));
    assert.deepEqual(n.urls, []);
    assert.match(n.p.data.notice, /来源暂不可用/);
  }
});
test("source handler rejects stale row epoch, forged index and hidden page", async () => {
  const n = await shown();
  const stale = sourceEvent(n.p);
  await n.p.refresh();
  n.p.openMemorySource(stale);
  n.p.openMemorySource({ currentTarget: { dataset: { index: 0, id: "forged", epoch: n.p.epoch } } });
  const fresh = sourceEvent(n.p);
  n.p.onHide();
  n.p.openMemorySource(fresh);
  assert.deepEqual(n.urls, []);
  assert.equal(n.p.data.notice, "");
});
test("cancelled system confirmation issues no POST and duplicate taps show one modal", async () => {
  const n = await shown();
  const clearing = n.p.clearMemories();
  void n.p.clearMemories();
  assert.equal(n.modals.length, 1);
  assert.match(n.modals[0].content, /聊天记录.*保留/);
  n.modals[0].success({ confirm: false });
  await clearing;
  assert.equal(n.posts().length, 0);
  assert.equal(n.p.data.clearing, false);
});
test("confirmed clear sends exact body once and refreshes actual new memory", async () => {
  let reads = 0;
  const n = await shown({ confirm: true, respond(o: any, path: string) {
    if (!path.startsWith("/memories?")) return false;
    reads++;
    o.success({ statusCode: 200, data: [{ ...row, user: reads === 1 ? "旧记忆" : "新对话记忆" }] }); return true;
  } });
  await n.p.clearMemories();
  assert.equal(n.posts().length, 1);
  assert.deepEqual(n.posts()[0].data, { confirm: true });
  assert.equal(reads, 2);
  assert.match(n.p.data.notice, /已清除近期记忆/);
  assert.equal(n.p.data.items[0].user, "新对话记忆");
});
test("duplicate tap during pending POST never reopens confirmation or retries", async () => {
  let pending: any;
  const n = await shown({ confirm: true, respond(o: any, path: string) { if (!path.startsWith("/memory-clear/")) return false; pending = o; return true; } });
  const clearing = n.p.clearMemories(); await tick();
  void n.p.clearMemories();
  assert.equal(n.modals.length, 1); assert.equal(n.posts().length, 1);
  pending.success({ statusCode: 200, data: { deviceId: device, status: "confirmed" } });
  await clearing;
  assert.equal(n.p.data.clearing, false);
});
test("busy, unavailable, network failure and invalid receipts never claim clear success or auto retry", async () => {
  const cases = [
    { statusCode: 409, data: { code: "memory_busy" }, text: /机器人正在对话，请结束后再清除/ },
    { statusCode: 503, data: { code: "memory_clear_unconfirmed" }, text: /结果未确认，请刷新查看/ },
    { fail: true, text: /结果未确认，请刷新查看/ },
    { statusCode: 200, data: { deviceId: other, status: "confirmed" }, text: /结果未确认，请刷新查看/ },
    { statusCode: 200, data: { deviceId: device, status: "queued" }, text: /结果未确认，请刷新查看/ },
    { statusCode: 200, data: null, text: /结果未确认，请刷新查看/ },
    { statusCode: 403, data: {}, text: /权限/ },
  ];
  for (const response of cases) {
    const n = await shown({ confirm: true, respond(o: any, path: string) { if (!path.startsWith("/memory-clear/")) return false; response.fail ? o.fail() : o.success(response); return true; } });
    await n.p.clearMemories();
    assert.match(n.p.data.error, response.text);
    assert.equal(n.p.data.notice, ""); assert.equal(n.posts().length, 1);
    if (response.statusCode === 403) assert.equal(n.p.data.canClear, false);
  }
});
test("hide, logout, account switch and permission loss invalidate an open confirmation", async () => {
  for (const action of ["hide", "logout", "account", "permission", "refresh"]) {
    const n = await shown();
    const clearing = n.p.clearMemories();
    if (action === "hide") n.p.onHide();
    if (action === "logout") await n.p.logout();
    if (action === "account") await n.p.api.login("other-synthetic", "synthetic");
    if (action === "permission") n.p.setData({ canClear: false });
    if (action === "refresh") await n.p.refresh();
    n.modals[0].success({ confirm: true }); await clearing;
    assert.equal(n.posts().length, 0, action);
    if (["hide", "logout", "account"].includes(action)) {
      assert.deepEqual(n.p.data.items, [], action); assert.equal(n.p.data.canClear, false, action); assert.equal(n.p.data.notice, "", action);
    }
  }
});
test("401 on another page invalidates memory confirmation and clears all private state", async () => {
  const n = await shown();
  const clearing = n.p.clearMemories();
  n.wx.request = (o: any) => o.success({ statusCode: 401, data: {} });
  await assert.rejects(n.p.api.request("/families"));
  n.modals[0].success({ confirm: true }); await clearing;
  assert.equal(n.posts().length, 0);
  assert.deepEqual(n.p.data.items, []); assert.equal(n.p.data.canClear, false); assert.equal(n.p.data.notice, "");
});
test("late clear receipt after hide and reshow cannot overwrite a newer page", async () => {
  let pending: any;
  const n = await shown({ confirm: true, respond(o: any, path: string) { if (!path.startsWith("/memory-clear/")) return false; pending = o; return true; } });
  const clearing = n.p.clearMemories(); await tick();
  n.p.onHide(); await n.p.onShow();
  pending.success({ statusCode: 200, data: { deviceId: device, status: "confirmed" } }); await clearing;
  assert.equal(n.p.data.items.length, 1); assert.equal(n.p.data.notice, ""); assert.equal(n.p.data.error, "");
});
test("late list after account change never restores body or clear permission", async () => {
  let pending: any;
  const n = native({ respond(o: any, path: string) { if (!path.startsWith("/memories?")) return false; pending = o; return true; } });
  await n.p.api.login("synthetic", "synthetic");
  const loading = n.p.onShow(); await tick();
  await n.p.api.login("other-synthetic", "synthetic");
  pending.success({ statusCode: 200, data: [row] }); await loading;
  assert.deepEqual(n.p.data.items, []); assert.equal(n.p.data.canClear, false);
});
test("memory API allows only exact new paths and method pairs", async () => {
  const n = native();
  await n.p.api.request("/memories?device=" + device);
  await n.p.api.request("/memory-clear/" + device, "POST", { confirm: true });
  const count = n.calls.length;
  for (const [path, method] of [["/memories", "GET"], ["/memories?device=" + device + "&x=1", "GET"], ["/memories?device=" + device, "POST"], ["/memory-clear/" + device, "GET"], ["/memory-clear/" + device + "/x", "POST"]]) await assert.rejects(n.p.api.request(path, method), /地址/);
  assert.equal(n.calls.length, count);
});
test("native memory source and clear bindings enforce actual conditional visibility", () => {
  const markup = readFileSync(new URL("../src/pages/memories/memories.wxml", import.meta.url), "utf8");
  verifyBindings(markup);
  assert.match(markup, /最近7天，最多24轮对话摘录/);
  assert.match(markup, /清除记忆后，聊天记录仍会保留/);
  assert.match(markup, /item\.assistant/);
  assert.doesNotMatch(markup, /item\.generated/);
  const clear = /<button\b([^>]*bindtap="clearMemories"[^>]*)>/.exec(markup);
  assert.ok(clear);
  const condition = /wx:if="{{([^}]+)}}"/.exec(clear[1]);
  assert.ok(condition);
  for (const canClear of [false, true]) assert.equal(!!vm.runInNewContext(condition[1], { canClear }), canClear);
  assert.match(clear[1], /disabled="{{[^}]*clearing[^}]*}}"/);
  assert.match(markup, /data-epoch="{{item.epoch}}"/);
  const deviceMarkup = readFileSync(new URL("../src/pages/device/device.wxml", import.meta.url), "utf8");
  assert.match(deviceMarkup, /bindtap="memories"/);
  assert.match(deviceMarkup, /近期记忆/);
});
test("changed RAM cookie during confirmation prevents mutation even while authenticated", async () => {
  const n = await shown();
  const clearing = n.p.clearMemories();
  n.wx.request = (o: any) => { n.calls.push(o); o.success({ statusCode: 200, data: [], cookies: ["management_family=synthetic-replacement"] }); };
  await n.p.api.families();
  n.modals[0].success({ confirm: true });
  await clearing;
  assert.equal(n.posts().length, 0);
  assert.deepEqual(n.p.data.items, []);
  assert.equal(n.p.data.canClear, false);
});
test("unknown HTTP clear outcomes ask for refresh without a retry instruction", async () => {
  for (const statusCode of [500, 502, 504, 409]) {
    const n = await shown({ confirm: true, respond(o: any, path: string) { if (!path.startsWith("/memory-clear/")) return false; o.success({ statusCode, data: { code: "unrecognized" } }); return true; } });
    await n.p.clearMemories();
    assert.equal(n.posts().length, 1);
    assert.equal(n.p.data.notice, "");
    assert.equal(n.p.data.error, "结果未确认，请刷新查看");
  }
});
test("confirmed receipt remains confirmed if subsequent memory refresh fails", async () => {
  let reads = 0;
  const n = await shown({ confirm: true, respond(o: any, path: string) { if (!path.startsWith("/memories?")) return false; if (++reads === 1) return false; o.fail(); return true; } });
  await n.p.clearMemories();
  assert.match(n.p.data.notice, /已清除近期记忆/);
  assert.match(n.p.data.error, /列表读取失败/);
  assert.deepEqual(n.p.data.items, []);
  assert.equal(n.p.data.canClear, false);
});
