import test from "node:test";
import assert from "node:assert/strict";
import { familyPage } from "../src/family-page.ts";
test("hidden chat page clears all body and rejects a late result", async () => {
  let finish: any;
  const app: any = {};
  (globalThis as any).getApp = () => app;
  (globalThis as any).wx = {
    request: (o: any) => {
      finish = o.success;
    },
  };
  const p: any = familyPage("chat");
  p.data = { ...p.data };
  p.setData = (v: any) => Object.assign(p.data, v);
  p.onLoad({ device: "dev_" + "a".repeat(32), chat: "chat_" + "b".repeat(64) });
  const pending = p.onShow();
  p.onHide();
  finish({
    statusCode: 200,
    data: {
      id: "chat_" + "b".repeat(64),
      deviceId: "dev_" + "a".repeat(32),
      turns: [{ generated: "private" }],
    },
  });
  await pending;
  assert.equal(p.data.chat, null);
  assert.deepEqual(p.data.items, []);
});
function native(kind: string, responder: (o: any) => void, query: any = {}) {
  const app: any = {};
  (globalThis as any).getApp = () => app;
  (globalThis as any).wx = { request: (o:any) => {
    if(o.url.includes('/device-runtime/')) return o.success({statusCode:200,data:{deviceId:o.url.split('/').pop(),connection:'unknown',seenAt:'',boot:'',volume:null,brightness:null,canControl:false,command:null}});
    return responder(o);
  } };
  const p: any = familyPage(kind as any);
  p.data = { ...p.data };
  p.setData = (v: any) => Object.assign(p.data, v);
  p.onLoad(query);
  return p;
}
const device = "dev_" + "a".repeat(32),
  other = "dev_" + "c".repeat(32),
  chat = "chat_" + "b".repeat(64);
test("chat refuses response from another device", async () => {
  const p = native(
    "chat",
    (o) =>
      o.success({
        statusCode: 200,
        data: {
          id: chat,
          deviceId: other,
          bodyAvailable: true,
          turns: [{ generated: "private" }],
        },
      }),
    { device, chat },
  );
  await p.onShow();
  assert.equal(p.data.chat, null);
  assert.match(p.data.error, /匹配/);
});
test("settings PUT saves string version then re-reads cloud DTO", async () => {
  const calls: any[] = [];
  const dto = {
    deviceId: device,
    name: "机器人",
    location: "客厅",
    version: "b".repeat(64),
    canEdit: true,
  };
  const p = native(
    "settings",
    (o) => {
      calls.push(o);
      o.success({
        statusCode: 200,
        data:
          o.method === "PUT" || calls.length === 3
            ? { ...dto, name: "新名字" }
            : dto,
      });
    },
    { device },
  );
  await p.onShow();
  p.setData({ name: "新名字" });
  await p.save();
  assert.deepEqual(
    calls.map((o) => o.method),
    ["GET", "PUT", "GET"],
  );
  assert.equal(calls[1].data.version, dto.version);
  assert.match(p.data.notice, /重新读取/);
  assert.equal(p.data.name, "新名字");
});
test("settings conflict reloads current data without claiming saved", async () => {
  let n = 0;
  const dto = {
    deviceId: device,
    name: "新版本",
    location: "",
    version: "b".repeat(64),
    canEdit: true,
  };
  const p = native(
    "settings",
    (o) => {
      n++;
      o.success({
        statusCode: o.method === "PUT" ? 409 : 200,
        data: o.method === "PUT" ? { code: "settings_conflict" } : dto,
      });
    },
    { device },
  );
  await p.onShow();
  await p.save();
  assert.equal(n, 3);
  assert.equal(p.data.notice, "");
  assert.match(p.data.error, /刷新/);
});
test("pagination sends explicit device and offsets, serializes duplicate load", async () => {
  const requests: any[] = [];
  const p = native("chats", (o) => requests.push(o), { device });
  const first = p.onShow();
  requests[0].success({
    statusCode: 200,
    data: { items: [{ id: chat, deviceId: device }], hasMore: true },
  });
  await first;
  const more = p.more();
  void p.more();
  assert.equal(requests.length, 2);
  assert.match(requests[1].url, /offset=1&limit=20$/);
  requests[1].success({ statusCode: 200, data: { items: [], hasMore: false } });
  await more;
  assert.equal(p.data.items.length, 1);
});
test("ordinary read-only family is available on home without canAddDevices", async () => {
  const p = native("home", (o) =>
    o.success({
      statusCode: 200,
      data: o.url.endsWith("/families")
        ? [{ id: "home", name: "家庭", canAddDevices: false }]
        : [{ id: device, familyId: "home", name: "机器人" }],
    }),
  );
  await p.onShow();
  assert.equal(p.data.loggedIn, true);
  assert.equal(p.data.devices.length, 1);
});
test("401 in any page erases body in every shared native page", async () => {
  const app: any = {};
  (globalThis as any).getApp = () => app;
  const requests: any[] = [];
  (globalThis as any).wx = { request: (o: any) => requests.push(o) };
  const pages = ["chat", "device"].map((kind) => {
    const p: any = familyPage(kind as any);
    p.data = { ...p.data };
    p.setData = (v: any) => Object.assign(p.data, v);
    p.onLoad({ device, chat });
    return p;
  });
  const first = pages[0].onShow();
  requests[0].success({
    statusCode: 200,
    data: {
      id: chat,
      deviceId: device,
      bodyAvailable: true,
      turns: [{ generated: "synthetic" }],
    },
  });
  await first;
  const second = pages[1].onShow();
  requests[1].success({ statusCode: 401, data: {} });
  await second;
  assert.equal(pages[0].data.chat, null);
  assert.equal(pages[0].data.loggedIn, false);
});
test("family login rejects duplicate submissions and cannot set cookie after hide", async () => {
  const calls: any[] = [];
  const p = native("home", (o) => calls.push(o));
  p.active = true;
  p.setData({ username: "synthetic", password: "synthetic" });
  const login = p.login();
  void p.login();
  assert.equal(calls.length, 1);
  p.onHide();
  calls[0].success({
    statusCode: 200,
    data: { ok: true },
    cookies: ["management_family=synthetic"],
  });
  await login;
  assert.equal(p.api.authenticated, false);
  assert.equal(p.data.loggedIn, false);
});
test("logout sends RAM cookie before clearing every local view", async () => {
  const calls: any[] = [];
  const p = native("home", (o) => {
    calls.push(o);
    o.success({
      statusCode: 200,
      data: { ok: true },
      cookies: o.url.endsWith("/login") ? ["management_family=synthetic"] : [],
    });
  });
  await p.api.login("synthetic", "synthetic");
  await p.logout();
  assert.equal(calls[1].header.Cookie, "management_family=synthetic");
  assert.equal(p.api.authenticated, false);
});
test("stale conflict refresh failure cannot erase newer visible settings", async () => {
  let conflictRead: any;
  let calls = 0;
  const dto = {
    deviceId: device,
    name: "最新",
    location: "",
    version: "c".repeat(64),
    canEdit: true,
  };
  const p = native(
    "settings",
    (o) => {
      calls++;
      if (calls === 3) {
        conflictRead = o;
        return;
      }
      o.success({
        statusCode: o.method === "PUT" ? 409 : 200,
        data: o.method === "PUT" ? {} : dto,
      });
    },
    { device },
  );
  await p.onShow();
  const saving = p.save();
  for (let i = 0; i < 20 && !conflictRead; i++) await Promise.resolve();
  assert.ok(conflictRead);
  p.onHide();
  await p.onShow();
  conflictRead.fail();
  await saving;
  assert.equal(p.data.settings?.name, "最新");
});
test("save cannot claim submitted values confirmed when readback differs", async () => {
  const dto = {
    deviceId: device,
    name: "旧名字",
    location: "客厅",
    version: "b".repeat(64),
    canEdit: true,
  };
  const p = native(
    "settings",
    (o) => o.success({ statusCode: 200, data: dto }),
    { device },
  );
  await p.onShow();
  p.setData({ name: "新名字" });
  await p.save();
  assert.equal(p.data.notice, "");
  assert.match(p.data.error, /确认|变化|冲突/);
  assert.equal(p.data.name, "旧名字");
});
test("readback of another writer version shows latest values without success", async () => {
  let n = 0;
  const old = {
    deviceId: device,
    name: "旧名字",
    location: "",
    version: "b".repeat(64),
    canEdit: true,
  };
  const put = { ...old, name: "新名字", version: "c".repeat(64) };
  const latest = { ...put, version: "d".repeat(64) };
  const p = native(
    "settings",
    (o) => {
      n++;
      o.success({
        statusCode: 200,
        data: n === 1 ? old : o.method === "PUT" ? put : latest,
      });
    },
    { device },
  );
  await p.onShow();
  p.setData({ name: "新名字" });
  await p.save();
  assert.equal(p.data.notice, "");
  assert.match(p.data.error, /变化|确认/);
  assert.equal(p.data.settings.version, latest.version);
});
test("chat labels translate playback facts and keep generated text separate", async () => {
  const p = native(
    "chat",
    (o) =>
      o.success({
        statusCode: 200,
        data: {
          id: chat,
          deviceId: device,
          status: "archived",
          bodyAvailable: true,
          turns: [
            {
              id: "1",
              user: "转写",
              played: "已播内容",
              generated: "完整生成稿",
              playback: "interrupted",
              boundary: "unknown",
              exactStopKnown: false,
            },
          ],
        },
      }),
    { device, chat },
  );
  await p.onShow();
  assert.equal(p.data.chat.status, "已归档");
  assert.equal(p.data.chat.turns[0].playback, "播放被打断");
  assert.match(p.data.chat.turns[0].boundary, /不确定/);
  assert.equal(p.data.chat.turns[0].played, "已播内容");
});
test("actual backend boundary enums distinguish full playback from conservative evidence", async () => {
  const expected: Record<string, string> = {
    complete_response: "完整回复已播放，停止位置已确认",
    no_recognition: "没有语音识别证据，停止位置不确定",
    recognition_gap: "语音识别记录有缺口，停止位置不确定",
    invalid_timing: "识别时间信息无效，停止位置不确定",
    playback_guard: "已保守保留播放尾部，停止位置不确定",
    context_limit: "识别证据达到长度限制，停止位置不确定",
    conservative_end: "依据识别证据保守截取，停止位置不确定",
    unknown_playback: "缺少可信播放证据，停止位置不确定",
  };
  const p = native(
    "chat",
    (o) =>
      o.success({
        statusCode: 200,
        data: {
          id: chat,
          deviceId: device,
          status: "archived",
          bodyAvailable: true,
          turns: Object.keys(expected).map((boundary) => ({
            id: boundary,
            boundary,
            playback: boundary === "complete_response" ? "played" : "unknown",
            exactStopKnown: boundary === "complete_response",
            played: boundary === "complete_response" ? "完整回复" : "",
            generated: "完整回复",
          })),
        },
      }),
    { device, chat },
  );
  await p.onShow();
  for (const turn of p.data.chat.turns) {
    assert.equal(turn.boundary, expected[turn.id], turn.id);
    assert.equal(turn.exactStopKnown, turn.id === "complete_response");
  }
});
