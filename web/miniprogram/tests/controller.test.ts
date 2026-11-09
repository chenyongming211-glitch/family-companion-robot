import { test } from "node:test";
import assert from "node:assert/strict";
import { Onboarding } from "../src/controller.ts";
test("background cleanup clears RAM secrets, session, timer and BLE connection", async () => {
  let destroyed = false,
    closed = false;
  const c: any = new Onboarding({}, () => {});
  c.code = "a".repeat(32);
  c.password = "synthetic-only";
  c.security = { destroy: () => (destroyed = true) };
  c.link = {
    close: async () => {
      closed = true;
    },
  };
  c.api.cookie = "management_family=synthetic";
  c.view.families = [{ id: "family", name: "synthetic", canAddDevices: true }];
  await c.leave();
  assert.equal(c.code, "");
  assert.equal(c.password, "");
  assert.equal(c.api.cookie, "");
  assert.equal(c.security, undefined);
  assert.equal(c.link, undefined);
  assert.equal(c.view.families.length, 0);
  assert.ok(destroyed && closed);
});
test("cancel invalidates pending task creation before stale response returns", async () => {
  let finish: any;
  const views: any[] = [];
  const platform = {
    request: (o: any) => {
      finish = () =>
        o.success({
          statusCode: 200,
          data: {
            id: "task",
            status: "waiting",
            serial: "serial",
            challenge: "test",
            expires_at: new Date(Date.now() + 60000).toISOString(),
          },
        });
    },
  };
  const c: any = new Onboarding(platform, (v) => views.push(v));
  c.setCode("a".repeat(32));
  c.view.families = [{ id: "family", name: "test", canAddDevices: true }];
  const creating = c.start("family", "");
  await new Promise((resolve) => setTimeout(resolve, 0));
  await c.cancel();
  finish();
  await creating;
  assert.equal(c.view.state.phase, "idle");
  assert.equal(c.task, undefined);
  assert.equal(c.link, undefined);
});
test("creator login 401 is visible and clears logged-in family state", async () => {
  const c: any = new Onboarding(
    {
      request: (o: any) =>
        o.success({ statusCode: 401, data: { code: "unauthorized" } }),
    },
    () => {},
  );
  c.setCode("a".repeat(32));
  c.view.families = [{ id: "family", name: "test", canAddDevices: true }];
  await c.start("family", "");
  assert.equal(c.view.state.phase, "error");
  assert.match(c.view.state.error, /重新登录/);
  assert.equal(c.view.families.length, 0);
});

for (const devices of [[{deviceId:"other",name:"FAMILY_111111111111"}], [{deviceId:"one",name:"FAMILY_020000000001"},{deviceId:"two",name:"FAMILY_020000000001"}]]) {
 test(`auto connection rejects nonunique exact target (${devices.length})`, async () => {
  const c:any = new Onboarding({},()=>{});
  c.code="a".repeat(32); c.view.families=[{id:"family"}];
  c.api.start=async()=>({id:"task",status:"waiting",serial:"UNIT001",ble_name:"FAMILY_020000000001",challenge:"test",expires_at:new Date(Date.now()+60000).toISOString()});
  c.platform.openBluetoothAdapter=(o:any)=>o.success({});
  // Discovery is stubbed at the transport boundary; no handshake may occur.
  const {BleLink}=await import("../src/provisioning/ble.ts");
  const original=BleLink.prototype.discover; const close=BleLink.prototype.close; BleLink.prototype.close=async()=>{};
  BleLink.prototype.discover=async()=>devices;
  try { await c.start("family",""); assert.equal(c.view.state.phase,"error"); assert.match(c.view.state.error,/重新扫码/); }
  finally { await c.cancel(); BleLink.prototype.discover=original; BleLink.prototype.close=close; }
 });
}
test("start automatically connects only the exact target once",async()=>{
 const {BleLink}=await import("../src/provisioning/ble.ts"); const original=BleLink.prototype.discover; const close=BleLink.prototype.close; BleLink.prototype.close=async()=>{};
 BleLink.prototype.discover=async()=>[{deviceId:"wrong",name:"FAMILY_111111111111"},{deviceId:"right",name:"FAMILY_020000000001"}];
 const c:any=new Onboarding({},()=>{}); c.code="a".repeat(32); c.view.families=[{id:"family"}];
 let creates=0; const connected:string[]=[];
 c.api.start=async()=>{creates++; return {id:"task",status:"waiting",serial:"UNIT001",ble_name:"FAMILY_020000000001",challenge:"test",expires_at:new Date(Date.now()+60000).toISOString()};};
 c.connect=async(id:string)=>connected.push(id);
 try { await Promise.all([c.start("family",""),c.start("family","")]); assert.equal(creates,1); assert.deepEqual(connected,["right"]); }
 finally {await c.cancel(); BleLink.prototype.discover=original; BleLink.prototype.close=close;}
});
test("cancel pending connect allows replacement and late finally cannot clear replacement busy",async()=>{
 const c:any=new Onboarding({},()=>{});let finish:any;let attempts=0;
 const setup=()=>{c.code="a".repeat(32);c.task={serial:"synthetic"};c.view.devices=[{deviceId:"right"}];c.view.state={phase:"connecting",generation:c.view.state.generation};c.link={connect:()=>{attempts++;return new Promise(resolve=>finish=resolve);},close:async()=>{}};};
 setup();const old=c.connect("right");const finishOld=finish;await c.cancel();setup();const replacement=c.connect("right");const finishReplacement=finish;
 assert.equal(attempts,2); // Replacement must own a real connect, never silently stall.
 // Canceling replacement also releases both unresolved synthetic transports.
 finishOld();await old;assert.equal(c.connecting,true);
 await c.cancel();finishReplacement();await replacement;
 // Original promise remains transport-owned; use its captured resolver separately.
 void old;void replacement;
});
