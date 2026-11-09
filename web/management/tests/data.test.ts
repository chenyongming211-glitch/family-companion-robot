import { test } from "node:test";
import assert from "node:assert/strict";
test("memory details reject an explicitly mismatched device", async () => {
  const api = createMockApi();
  await assert.rejects(api.getMemory("memory-story", "family", "dev-desk"));
});
import { createMockApi } from "../src/api.ts";

test("clearing one device memory preserves chats and other device memories", async () => {
  const api = createMockApi();
  await api.clearMemories("dev-living", "family");
  assert.equal((await api.listMemories({ deviceId: "dev-living" })).length, 0);
  assert.equal((await api.listMemories({ deviceId: "dev-desk" })).length, 1);
  assert.equal((await api.getConversation("chat-run")).turns.length, 2);
});

test("chat deletion removes only its derived memory and daily entry", async () => {
  const api = createMockApi();
  await assert.rejects(
    api.deleteConversation("chat-run", "family", "dev-desk"),
    /设备/,
  );
  await api.deleteConversation("chat-run", "family", "dev-living");
  await assert.rejects(api.getConversation("chat-run"), /未找到/);
  assert.equal(
    (await api.listMemories()).some((m) => m.conversationId === "chat-run"),
    false,
  );
  assert.equal(
    (await api.getDailySummaries()).some((day) =>
      day.conversations.some((c) => c.id === "chat-run"),
    ),
    false,
  );
  assert.equal((await api.getConversation("chat-story")).id, "chat-story");
});

test("family-only views reject a different family and unknown memory", async () => {
  const api = createMockApi();
  assert.deepEqual(
    (await api.listFamilies("family")).map((f) => f.id),
    ["family-lin"],
  );
  await assert.rejects(api.getFamily("family-zhou", "family"), /未授权/);
  await assert.rejects(
    api.listMemories({ deviceId: "dev-zhou", scope: "family" }),
    /未授权/,
  );
  await assert.rejects(api.getMemory("missing"), /未找到/);
});

test("invitations validate fictional addresses and produce one pending member", async () => {
  const api = createMockApi();
  const before = (await api.getFamily("family-lin")).members.length;
  await assert.rejects(
    api.inviteMember("family-lin", {
      email: "not-an-email",
      role: "家庭成员",
      allowHistory: false,
    }),
    /邮箱/,
  );
  const pending = await api.inviteMember("family-lin", {
    email: "new@example.test",
    role: "家庭成员",
    allowHistory: false,
  });
  assert.equal(pending.status, "待加入示例");
  assert.equal((await api.getFamily("family-lin")).members.length, before + 1);
  await assert.rejects(
    api.inviteMember("family-lin", {
      email: "new@example.test",
      role: "家庭成员",
      allowHistory: false,
    }),
    /已存在/,
  );
});

test("binding validates a fake code and creates one independently configurable device", async () => {
  const api = createMockApi();
  await assert.rejects(
    api.bindDevice(
      {
        familyId: "family-lin",
        code: "abc",
        name: "新朋友",
        model: "微雪 ESP32-S3",
      },
      "family",
    ),
    /6位/,
  );
  const device = await api.bindDevice(
    {
      familyId: "family-lin",
      code: "246810",
      name: "新朋友",
      model: "M5 ESP32-S3",
    },
    "family",
  );
  assert.equal((await api.listDevices("family")).length, 3);
  assert.equal((await api.getDevice(device.id, "family")).name, "新朋友");
  await assert.rejects(
    api.bindDevice(
      {
        familyId: "family-lin",
        code: "246810",
        name: "重复",
        model: "M5 ESP32-S3",
      },
      "family",
    ),
    /已绑定/,
  );
});

test("demo login stores account identity but never its submitted secret", async () => {
  const values = new Map<string, string>();
  const api = createMockApi({
    getItem: (k) => values.get(k) ?? null,
    setItem: (k, v) => {
      values.set(k, v);
    },
    removeItem: (k) => {
      values.delete(k);
    },
  });
  await assert.rejects(
    api.login("family", "parent@example.test", "bad"),
    /6位/,
  );
  const account = await api.login(
    "admin",
    "admin@example.test",
    "unique-demo-secret-42",
  );
  assert.equal(account.scope, "admin");
  assert.equal(
    [...values.values()].join("").includes("unique-demo-secret-42"),
    false,
  );
  await api.logout("admin");
  assert.equal(await api.getAccount("admin"), null);
});

test("privacy changes are device-scoped and disabling storage also disables recoverable memory", async () => {
  const api = createMockApi();
  const result = await api.updatePrivacy(
    "dev-living",
    { keepRecords: false, useMemory: true, retention: "7" },
    "family",
  );
  assert.equal(result.useMemory, false);
  assert.equal((await api.getPrivacy("dev-desk")).keepRecords, true);
  assert.equal((await api.getConversation("chat-run")).id, "chat-run");
});

test("living room access requires a living room device in the same family", async () => {
  const api = createMockApi();
  await assert.rejects(
    api.updateMemberAccess("family-zhou", "member-zhou", "客厅机器人"),
    /客厅/,
  );
  assert.equal(
    (
      await api.updateMemberAccess("family-lin", "member-child", "客厅机器人")
    ).members.find((m) => m.id === "member-child")?.access,
    "客厅机器人",
  );
});
