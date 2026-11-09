import { test } from "node:test";
import assert from "node:assert/strict";
import { createRemoteApi } from "../src/remote-api.ts";

test("live lists use business chats while source conversation links remain compatible", async () => {
  const seen: string[] = [];
  const api = createRemoteApi((async (input: string | URL | Request) => {
    seen.push(String(input));
    return new Response(
      JSON.stringify({ items: [], hasMore: false, pendingSources: true }),
    );
  }) as typeof fetch);
  const id = "chat_" + "a".repeat(64);
  const page = await api.listConversationPage({
    scope: "family",
    deviceId: "dev_fixture",
    offset: 0,
    limit: 50,
  });
  await api.getConversation(id, "family", "dev_fixture");
  await api.getConversation("source-record", "family", "dev_fixture");
  assert.deepEqual(seen, [
    "/api/family/chats?offset=0&limit=50&device=dev_fixture",
    `/api/family/chats/${id}?device=dev_fixture`,
    "/api/family/conversations/source-record?device=dev_fixture",
  ]);
  assert.equal(page.pendingSources, true);
});

test("live memory reads carry scope and selected device, while clear remains unsupported", async () => {
  const seen: string[] = [];
  const api = createRemoteApi((async (input: string | URL | Request) => {
    seen.push(String(input));
    return new Response(JSON.stringify([]));
  }) as typeof fetch);
  await api.listMemories({ scope: "family", deviceId: "dev_fixture" });
  await api.getMemory("mem-fixture-1", "admin", "dev_fixture");
  assert.deepEqual(seen, [
    "/api/family/memories?device=dev_fixture",
    "/api/admin/memories/mem-fixture-1?device=dev_fixture",
  ]);
  await assert.rejects(api.clearMemories("dev_fixture", "family"), /尚未接入/);
  assert.equal(seen.length, 2);
});

test("live login and reads use scoped same-origin endpoints with cookies", async () => {
  const requests: Array<{ url: string; init?: RequestInit }> = [];
  const fakeFetch = async (
    input: string | URL | Request,
    init?: RequestInit,
  ) => {
    requests.push({ url: String(input), init });
    return new Response(
      JSON.stringify({ email: "owner", role: "家庭管理员", scope: "family" }),
      { status: 200, headers: { "content-type": "application/json" } },
    );
  };
  const api = createRemoteApi(fakeFetch as typeof fetch);
  await api.login("family", "owner", "fixture-password");
  assert.equal(requests[0]!.url, "/api/family/login");
  assert.equal(requests[0]!.init?.credentials, "same-origin");
  assert.equal(requests[0]!.init?.method, "POST");
  assert.deepEqual(JSON.parse(String(requests[0]!.init?.body)), {
    username: "owner",
    password: "fixture-password",
  });
});

test("expired session is anonymous, while real read failures never become mock data", async () => {
  const api = createRemoteApi(
    (async () =>
      new Response(
        JSON.stringify({ code: "unauthenticated", message: "请先登录" }),
        { status: 401 },
      )) as typeof fetch,
  );
  assert.equal(await api.getAccount("family"), null);
  await assert.rejects(api.listDevices("family"), /请先登录/);
  const failed = createRemoteApi(
    (async () =>
      new Response("upstream unavailable", { status: 502 })) as typeof fetch,
  );
  await assert.rejects(failed.listDevices("family"), /502/);
});

test("live pagination and exact device ownership remain in the request", async () => {
  const seen: string[] = [];
  const api = createRemoteApi((async (input: string | URL | Request) => {
    seen.push(String(input));
    return new Response(JSON.stringify({ items: [], hasMore: true }), {
      status: 200,
    });
  }) as typeof fetch);
  const page = await api.listConversationPage({
    scope: "family",
    deviceId: "dev_live",
    offset: 50,
    limit: 50,
  });
  assert.equal(page.hasMore, true);
  const url = new URL(seen[0]!, "http://example.test");
  assert.equal(url.searchParams.get("device"), "dev_live");
  assert.equal(url.searchParams.get("offset"), "50");
  await api.getConversation("record-live", "family", "dev_live");
  assert.equal(
    new URL(seen[1]!, "http://example.test").searchParams.get("device"),
    "dev_live",
  );
});

test("unsupported real writes reject without touching fetch or mock state", async () => {
  let calls = 0;
  const api = createRemoteApi((async () => {
    calls++;
    return new Response("{}");
  }) as typeof fetch);
  await assert.rejects(
    api.updateDevice(
      "dev_live",
      { name: "名称", volume: 50, brightness: 50 },
      "family",
    ),
    /尚未接入/,
  );
  await assert.rejects(
    api.deleteConversation("record-live", "family"),
    /尚未接入/,
  );
  assert.equal(calls, 0);
});

test("a rejected login stays a form error; expired protected reads notify the page", async () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "window");
  let notices = 0;
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: {
      dispatchEvent() {
        notices++;
      },
    },
  });
  try {
    const api = createRemoteApi(
      (async () =>
        new Response(
          JSON.stringify({
            code: "unauthenticated",
            message: "账号或密码错误",
          }),
          { status: 401 },
        )) as typeof fetch,
    );
    await assert.rejects(
      api.login("family", "owner", "fixture-password"),
      /账号或密码/,
    );
    assert.equal(notices, 0);
    await assert.rejects(api.listDevices("family"));
    assert.equal(notices, 1);
  } finally {
    if (previous) Object.defineProperty(globalThis, "window", previous);
    else Reflect.deleteProperty(globalThis, "window");
  }
});
