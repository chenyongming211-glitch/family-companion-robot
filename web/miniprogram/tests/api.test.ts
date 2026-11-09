import { test } from "node:test";
import assert from "node:assert/strict";
import { Api, claimCode, ORIGIN } from "../src/api.ts";
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
