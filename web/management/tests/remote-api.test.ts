import { test } from "node:test";
import assert from "node:assert/strict";
import { createRemoteApi } from "../src/remote-api.ts";
import { sessionGeneration } from "../src/sensitive-session.ts";

test("anonymous session checks do not invalidate the guest login page", async () => {
  for (const scope of ["admin", "family"] as const) {
    const api = createRemoteApi((async () => new Response(
      JSON.stringify({ code: "unauthenticated", message: "请先登录" }),
      { status: 401 },
    )) as typeof fetch);
    const before = sessionGeneration();
    assert.equal(await api.getAccount(scope), null);
    assert.equal(await api.getAccount(scope), null);
    assert.equal(sessionGeneration(), before, "guest checks must leave the login form active");
  }
});

test("a previously authenticated session becoming anonymous still clears sensitive reads", async () => {
  let expired = false;
  let resolve!: (response: Response) => void;
  const api = createRemoteApi((async (input: string | URL | Request) => {
    if (!String(input).endsWith("/session")) return new Promise<Response>(r => { resolve = r; });
    return expired
      ? new Response(JSON.stringify({ code: "unauthenticated" }), { status: 401 })
      : new Response(JSON.stringify({ email: "fixture", role: "家庭管理员", scope: "family" }));
  }) as typeof fetch);
  await api.getAccount("family");
  const read = api.listDevices("family");
  const before = sessionGeneration();
  expired = true;
  assert.equal(await api.getAccount("family"), null);
  assert.ok(sessionGeneration() > before);
  resolve(new Response("[]"));
  await assert.rejects(read, /页面已离开|账号已变化/);
});

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

test("live memory reads carry scope and selected device, with selected scope", async () => {
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

test('runtime and clear use exact scoped paths and shape, and reject mismatched clear receipts', async()=>{
 const seen:any[]=[];let result:any={deviceId:'dev/x',status:'confirmed'};
 const api=createRemoteApi((async(url:any,init:any)=>{seen.push({url,init});return new Response(JSON.stringify(String(url).includes('device-runtime')?{deviceId:'dev/x',connection:'unknown',seenAt:'',boot:'',volume:null,brightness:null,canControl:false,command:null}:result));}) as typeof fetch);
 await api.getDeviceRuntime('dev/x','admin');await api.controlDevice('dev/x',{field:'volume',value:60,boot:'boot'},'family');await api.clearMemories('dev/x','admin');
 assert.deepEqual(seen.map(x=>x.url),['/api/admin/device-runtime/dev%2Fx','/api/family/device-runtime/dev%2Fx','/api/admin/memory-clear/dev%2Fx']);
 assert.equal(seen[1].init.method,'PUT');assert.deepEqual(JSON.parse(seen[1].init.body),{field:'volume',value:60,boot:'boot'});assert.deepEqual(JSON.parse(seen[2].init.body),{confirm:true});
 for(const bad of [{deviceId:'other',status:'confirmed'},{deviceId:'dev/x',status:'unknown'},{},null]){result=bad;await assert.rejects(api.clearMemories('dev/x','admin'),/核实/);}
});

test('401 in another scope invalidates a delayed sensitive response even with malformed JSON', async()=>{
 let resolve!:(x:Response)=>void;
 const pending=new Promise<Response>(r=>resolve=r);
 const api=createRemoteApi((async()=>pending) as typeof fetch);
 const result=api.getMemory('m','family','d');
 const other=createRemoteApi((async()=>new Response('broken',{status:401})) as typeof fetch);
 await assert.rejects(other.listDevices('admin'));
 resolve(new Response(JSON.stringify({text:'sensitive'})));
 await assert.rejects(result,/页面已离开|账号已变化/);
});
test('logout invalidates pending reads across API instances before network completes',async()=>{
 let resolve!:(x:Response)=>void;const pending=new Promise<Response>(r=>resolve=r);
 const a=createRemoteApi((async()=>pending) as typeof fetch);
 const read=a.listMemories({deviceId:'d',scope:'family'});
 const b=createRemoteApi((async()=>new Response('{}')) as typeof fetch);await b.logout('admin');
 resolve(new Response('[]'));await assert.rejects(read,/页面已离开|账号已变化/);
});
test('changed account invalidates a read started under previous identity',async()=>{
 let identity='first',resolve!:(x:Response)=>void;
 const api=createRemoteApi((async(url:any)=>String(url).endsWith('/session')?new Response(JSON.stringify({email:identity,role:'member',scope:'admin'})):new Promise<Response>(r=>resolve=r)) as typeof fetch);
 await api.getAccount('admin');const read=api.listDevices('admin');identity='second';await api.getAccount('admin');resolve(new Response('[]'));await assert.rejects(read,/账号已变化/);
});


test('malformed runtime DTOs cannot become trusted status or control receipts',async()=>{
 const good={deviceId:'d',connection:'online',seenAt:'2026-11-11T00:00:00Z',boot:'b',volume:null,brightness:30,canControl:true,command:null};
 let result:any=good;
 const api=createRemoteApi((async()=>new Response(JSON.stringify(result))) as typeof fetch);
 assert.equal((await api.getDeviceRuntime('d','admin')).volume,null);
 for(const bad of [null,{}, {...good,deviceId:'other'},{...good,volume:101},{...good,volume:undefined},{...good,canControl:'yes'},{...good,command:{id:1,field:'volume',value:20,status:'confirmed',actual:20}}]){
  result=bad;await assert.rejects(api.getDeviceRuntime('d','admin'),/核实/);await assert.rejects(api.controlDevice('d',{field:'volume',value:20,boot:'b'},'admin'),/核实/);
 }
});

test('audit page uses only the admin endpoint and exact filter parameters with no mock fallback', async () => {
  const seen:Array<{url:string;init?:RequestInit}>=[];
  const api=createRemoteApi((async(input:string|URL|Request,init?:RequestInit)=>{seen.push({url:String(input),init});return new Response(JSON.stringify({items:[],hasMore:false,pending:true}));}) as typeof fetch);
  const page=await api.listAuditPage({device:'dev_'+'a'.repeat(32),from:'2026-11-01T00:00:00Z',to:'2026-11-12T00:00:00Z',action:'access',offset:50,limit:50});
  const url=new URL(seen[0]!.url,'http://fixture');assert.equal(url.pathname,'/api/admin/audit');
  assert.deepEqual([...url.searchParams.keys()].sort(),['action','device','from','limit','offset','to']);assert.equal(url.searchParams.get('offset'),'50');assert.equal(url.searchParams.get('action'),'access');assert.equal(page.pending,true);assert.equal(seen[0]!.init?.cache,'no-store');
});
