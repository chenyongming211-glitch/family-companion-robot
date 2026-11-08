import { test } from "node:test";
import assert from "node:assert/strict";
import { createMockApi } from "../src/api.ts";

test("audit queries expose demo mutations and return detached values", async () => {
  const api = createMockApi();
  await api.updateDevice("dev-living", {
    name: "暖暖",
    volume: 50,
    brightness: 60,
  });
  const events = await api.listAudit("暖暖");
  assert.equal(events.length, 1);
  assert.equal(events[0]!.action, "配置变更");
  events[0]!.target = "外部修改";
  assert.equal((await api.listAudit("暖暖"))[0]!.target, "暖暖");
  assert.equal((await api.listAudit("不存在的事件")).length, 0);
});

test("expression preview offers 15 canonical emotions with thinking kept as a system state", async () => {
  const api = createMockApi();
  const expressions = await api.listExpressions();
  assert.equal(expressions.length, 15);
  assert.equal(new Set(expressions.map((e) => e.id)).size, 15);
  assert.equal(
    expressions.some((e) => e.id === "thinking"),
    false,
  );
  assert.ok(expressions.some((e) => e.id === "confused"));
});
