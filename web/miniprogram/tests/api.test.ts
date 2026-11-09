import { test } from "node:test";
import assert from "node:assert/strict";
import { Api, ClientError, claimCode, ORIGIN } from "../src/api.ts";
test("scan admits only exact claim codes or scene wrapper and never URLs", () => {
  const c = "a".repeat(32);
  assert.equal(claimCode(c), c);
  assert.equal(claimCode("scene=" + c), c);
  for (const bad of [
    "https://evil.test/?scene=" + c,
    "A".repeat(32),
    c + "&x=1",
  ])
    assert.throws(() => claimCode(bad));
});
test("cookie is RAM only, fixed origin and HTTP errors stay errors", async () => {
  let seen: any[] = [];
  let status = 200;
  const api = new Api({
    request: (o: any) => {
      seen.push(o);
      o.success({
        statusCode: status,
        data: { ok: true },
        cookies: ["management_family=synthetic; Path=/; HttpOnly"],
      });
    },
  });
  await api.login("u", "p");
  await api.families();
  assert.equal(seen[1].url, ORIGIN + "/api/family/families");
  assert.equal(seen[1].header.Cookie, "management_family=synthetic");
  status = 401;
  await assert.rejects(api.families(), /重新登录/);
  status = 200;
  await api.families();
  assert.equal(seen[3].header.Cookie, undefined);
  await assert.rejects(api.request("https://evil.test"), /地址无效/);
});
test("network failures use sanitized feedback", async () => {
  const api = new Api({
    request: (o: any) => o.fail({ errMsg: "contains sensitive vendor text" }),
  });
  await assert.rejects(api.families(), /网络连接失败/);
});
test("creating an onboarding task reports an unregistered or invalid device code on 404", async () => {
  const api = new Api({
    request: (o: any) =>
      o.success({
        statusCode: 404,
        data: { code: "not_found", message: "sensitive upstream details" },
      }),
  });
  await assert.rejects(api.start("synthetic-family", "a".repeat(32)), {
    name: "Error",
    code: "not_found",
    message: "设备码尚未登记或已失效，请联系管理员",
  });
});
test("querying or cancelling an existing onboarding task reports its absence on 404", async () => {
  const api = new Api({
    request: (o: any) =>
      o.success({
        statusCode: 404,
        data: { code: "not_found", message: "sensitive upstream details" },
      }),
  });
  for (const request of [
    () => api.status("synthetic-task"),
    () => api.cancel("synthetic-task"),
  ]) {
    await assert.rejects(request(), {
      name: "Error",
      code: "not_found",
      message: "添加任务不存在，请重新扫码",
    });
  }
});
test("other 404 responses report a missing service entry without upstream details", async () => {
  const api = new Api({
    request: (o: any) =>
      o.success({
        statusCode: 404,
        data: { code: "not_found", message: "sensitive upstream details" },
      }),
  });
  for (const [path, method] of [
    ["/families", "GET"],
    ["/onboarding", "GET"],
    ["/onboarding/synthetic-task", "POST"],
  ]) {
    await assert.rejects(api.request(path, method), (error: unknown) => {
      assert.ok(error instanceof ClientError);
      assert.equal(error.code, "not_found");
      assert.equal(error.message, "服务入口不存在，请联系管理员");
      return true;
    });
  }
});
test("invalid login reports credentials while clearing the RAM session", async () => {
  const api: any = new Api({
    request: (o: any) =>
      o.success({ statusCode: 401, data: { code: "invalid_login" } }),
  });
  api.cookie = "management_family=synthetic";
  await assert.rejects(api.login("synthetic", "wrong"), /账号或密码不正确/);
  assert.equal(api.cookie, "");
});
test("cleared RAM session rejects late login cookie and cannot restore it", async () => {
  let finish: any;
  const api: any = new Api({
    request: (o: any) => {
      finish = () =>
        o.success({
          statusCode: 200,
          data: { ok: true },
          cookies: ["management_family=synthetic"],
        });
    },
  });
  const p = api.login("synthetic", "test-only");
  api.clear();
  finish();
  await assert.rejects(p, /取消/);
  assert.equal(api.cookie, "");
});

test("successful login isolates its cookie from an older concurrent DELETE401", async () => {
  const requests: any[] = [];
  const api: any = new Api({ request: (o: any) => requests.push(o) });
  const logging = api.login("synthetic", "synthetic");
  const cancellation = api.cancel("old-task");
  const rejected = assert.rejects(cancellation, /取消/);
  requests[0].success({
    statusCode: 200,
    data: {},
    cookies: ["management_family=new"],
  });
  await logging;
  requests[1].success({ statusCode: 401, data: {} });
  await rejected;
  assert.equal(api.cookie, "management_family=new");
});
