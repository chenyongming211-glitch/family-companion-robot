import test from "node:test";
import assert from "node:assert/strict";
import { familySession, deviceID, chatID } from "../src/family.ts";
test("family session lives on native app instance across separate callers", () => {
  const app: any = {};
  const a = familySession({}, app);
  assert.equal(a, familySession({}, app));
  assert.notEqual(a, familySession({}, {}));
});
test("route identity rejects cross-device carriers and malformed chat IDs", () => {
  assert.equal(deviceID("dev_" + "a".repeat(32)), "dev_" + "a".repeat(32));
  for (const bad of [
    "x",
    "dev_" + "A".repeat(32),
    "dev_" + "a".repeat(32) + "?x",
  ])
    assert.throws(() => deviceID(bad));
  assert.throws(() => chatID("chat_bad"));
});
test("family API strictly admits only scoped device and chat routes", async () => {
  const calls: any[] = [];
  const api = familySession(
    {
      request: (o: any) => {
        calls.push(o);
        o.success({ statusCode: 200, data: {} });
      },
    },
    {},
  );
  const d = "dev_" + "a".repeat(32),
    c = "chat_" + "b".repeat(64);
  await api.request("/devices/" + d);
  await api.request("/chats/" + c + "?device=" + d);
  await api.request("/chats?device=" + d + "&offset=0&limit=20");
  for (const path of [
    "/devices/other",
    "/chats",
    "/chats?device=" + d + "&offset=-1&limit=20",
    "/chats/" + c + "?device=" + d + "&extra=1",
    "/device-settings/" + d + "/x",
  ])
    await assert.rejects(api.request(path), /地址/);
  assert.equal(calls.length, 3);
});
