import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";
import { verifyWxml } from "../verify-wxml.mjs";
import { Onboarding } from "../src/controller.ts";
import { BleLink } from "../src/provisioning/ble.ts";
import { initial, reduce } from "../src/state.ts";
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
function closingPlatform() {
  let connected = false,
    opened = false,
    connect: any,
    open: any;
  const wx: any = {
    createBLEConnection: (o: any) => {
      connect = o;
    },
    openBluetoothAdapter: (o: any) => {
      open = o;
    },
    closeBLEConnection: (o: any) => {
      connected = false;
      o.success({});
    },
    closeBluetoothAdapter: (o: any) => {
      opened = false;
      o.success({});
    },
    stopBluetoothDevicesDiscovery: (o: any) => o.success({}),
    offBLECharacteristicValueChange: () => {},
    offBLEConnectionStateChange: () => {},
  };
  return {
    wx,
    get connected() {
      return connected;
    },
    get opened() {
      return opened;
    },
    connectedLate() {
      connected = true;
      connect.success({});
    },
    openedLate() {
      opened = true;
      open.success({});
    },
  };
}
test("cancel while start releases old resources makes zero cloud requests", async () => {
  let release!: () => void,
    requests = 0;
  const c: any = new Onboarding(
    {
      request: (o: any) => {
        requests++;
        o.fail();
      },
    },
    () => {},
  );
  c.setCode("a".repeat(32));
  c.view.families = [{ id: "family", name: "test", canAddDevices: true }];
  c.link = {
    close: () =>
      new Promise<void>((resolve) => {
        release = resolve;
      }),
  };
  const starting = c.start("family", "old name");
  await c.cancel();
  c.setCode("b".repeat(32));
  release();
  await starting;
  assert.equal(requests, 0);
  assert.equal(c.view.state.phase, "idle");
});
test("start snapshots claim and nickname before resource release", async () => {
  let release!: () => void, requested: any;
  const c: any = new Onboarding(
    {
      request: (o: any) => {
        requested = o.data;
        o.fail();
      },
    },
    () => {},
  );
  c.setCode("a".repeat(32));
  c.view.families = [{ id: "family", name: "test", canAddDevices: true }];
  c.link = {
    close: () =>
      new Promise<void>((resolve) => {
        release = resolve;
      }),
  };
  const starting = c.start("family", " old name ");
  c.setCode("b".repeat(32));
  release();
  await starting;
  assert.equal(requested.claim_code, "a".repeat(32));
  assert.equal(requested.name, "old name");
});
for (const cancelled of [true, false]) {
  test(`late BLE connect success is closed after ${cancelled ? "cancel" : "timeout"}`, async () => {
    const p = closingPlatform();
    const link = new BleLink(p.wx, 10);
    const connecting = link.connect("synthetic");
    const rejected = assert.rejects(connecting, cancelled ? /取消/ : /超时/);
    if (cancelled) await link.close();
    else await new Promise((resolve) => setTimeout(resolve, 20));
    p.connectedLate();
    await rejected;
    await tick();
    assert.equal(p.connected, false);
  });
  test(`late adapter open success is closed after ${cancelled ? "cancel" : "timeout"}`, async () => {
    const p = closingPlatform();
    const link = new BleLink(p.wx, 10);
    const discovering = link.discover();
    const rejected = assert.rejects(discovering, cancelled ? /取消/ : /超时/);
    if (cancelled) await link.close();
    else await new Promise((resolve) => setTimeout(resolve, 20));
    p.openedLate();
    await rejected;
    await tick();
    assert.equal(p.opened, false);
  });
}
let definition: any, scan: any;
(globalThis as any).Page = (p: any) => {
  definition = p;
};
(globalThis as any).wx = {
  scanCode: (options: any) => {
    scan = options;
  },
};
await import("../src/pages/index/index.ts");
function page() {
  const p: any = {
    ...definition,
    data: { ...definition.data },
    setData(values: any) {
      Object.assign(this.data, values);
    },
  };
  p.onLoad({});
  return p;
}
for (const cleanup of ["onHide", "onUnload", "cancel"]) {
  test(`late scanner success/failure cannot restore code or feedback after ${cleanup}`, async () => {
    const p = page();
    p.scan();
    const old = scan;
    await p[cleanup]();
    p.setData({ error: "new feedback" });
    old.success({ result: "a".repeat(32) });
    old.fail();
    assert.equal(p.data.scanned, false);
    assert.equal(p.controller.code, "");
    assert.equal(p.data.error, "new feedback");
    p.onUnload();
  });
}
test("superseded scanner cannot replace a newer scan result", () => {
  const p = page();
  p.scan();
  const old = scan;
  p.scan();
  scan.success({ result: "b".repeat(32) });
  old.success({ result: "a".repeat(32) });
  old.fail();
  assert.equal(p.controller.code, "b".repeat(32));
  assert.equal(p.data.error, "");
});
test("scanner result after onShow completes the same suspended operation", () => {
  const p = page();
  p.scan();
  const operation = scan;
  p.onHide();
  p.onShow();
  operation.success({ result: "a".repeat(32) });
  assert.equal(p.controller.code, "a".repeat(32));
  assert.equal(p.data.scanned, true);
});
test("scanner queues a validated hidden result until onShow and preserves RAM family session", async () => {
  const p = page();
  p.controller.api.cookie = "management_family=synthetic";
  p.controller.view.families = [
    { id: "family", name: "test", canAddDevices: true },
  ];
  p.setData({
    loggedIn: true,
    password: "synthetic",
    loginPassword: "synthetic",
  });
  p.scan();
  const operation = scan;
  p.onHide();
  operation.success({ result: "a".repeat(32) });
  assert.equal(p.controller.code, "");
  assert.equal(p.data.scanned, false);
  assert.equal(p.data.password, "");
  assert.equal(p.data.loginPassword, "");
  assert.equal(p.controller.api.cookie, "management_family=synthetic");
  assert.equal(p.controller.view.families.length, 1);
  p.onShow();
  assert.equal(p.controller.code, "a".repeat(32));
  assert.equal(p.data.scanned, true);
  await p.onUnload();
});
for (const invalidate of ["cancel", "onUnload"]) {
  test(`queued scanner result is erased by ${invalidate}`, async () => {
    const p = page();
    p.scan();
    const operation = scan;
    p.onHide();
    operation.success({ result: "a".repeat(32) });
    const queued = p.scanOperation;
    await p[invalidate]();
    assert.equal(queued.code, undefined);
    p.onShow();
    assert.equal(p.controller.code, "");
    assert.equal(p.data.scanned, false);
    assert.equal(p.scanOperation, undefined);
  });
}
test("changing account input invalidates a queued scanner result", () => {
  const p = page();
  p.scan();
  p.onHide();
  scan.success({ result: "a".repeat(32) });
  p.input({
    currentTarget: { dataset: { key: "username" } },
    detail: { value: "new account" },
  });
  p.onShow();
  assert.equal(p.controller.code, "");
  assert.equal(p.scanOperation, undefined);
});
test("terminal session leave invalidates a queued scanner result", async () => {
  const p = page();
  p.scan();
  p.onHide();
  scan.success({ result: "a".repeat(32) });
  await p.controller.leave();
  p.onShow();
  assert.equal(p.controller.code, "");
  assert.equal(p.scanOperation, undefined);
});
test("scan deadline clears hidden queued code and rejects late success", (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const p = page();
  p.scan();
  const operation = scan;
  p.onHide();
  operation.success({ result: "a".repeat(32) });
  t.mock.timers.tick(60001);
  p.onShow();
  operation.success({ result: "a".repeat(32) });
  assert.equal(p.controller.code, "");
  assert.equal(p.scanOperation, undefined);
  assert.match(p.data.error, /超时/);
});
test("actual WXML success claims are visible only after cloud confirmed state", async () => {
  const wxml = await readFile(
    new URL("../src/pages/index/index.wxml", import.meta.url),
    "utf8",
  );
  const conditions: string[][] = [];
  const stack: string[] = [];
  for (const part of wxml.matchAll(/<\/?[a-z][^>]*>|[^<]+/gi)) {
    const token = part[0];
    if (token.startsWith("</")) {
      stack.pop();
      continue;
    }
    if (token.startsWith("<")) {
      const raw = /\bwx:if="([^"]*)"/.exec(token)?.[1];
      if (raw !== undefined)
        assert.match(
          raw,
          /^\{\{[\s\S]+\}\}$/,
          "wx:if must be a native expression binding",
        );
      if (!token.endsWith("/>")) stack.push(raw?.slice(2, -2) || "true");
    } else if (
      token.includes("设备添加成功") ||
      token.includes("云端已确认绑定")
    )
      conditions.push([...stack]);
  }
  assert.equal(conditions.length, 2);
  let state = reduce(initial, { type: "start" });
  const states = [initial, state];
  state = reduce(state, { type: "secure", generation: state.generation });
  states.push(state);
  state = reduce(state, { type: "apply", generation: state.generation });
  states.push(state);
  state = reduce(state, { type: "ble_applied", generation: state.generation });
  states.push(state);
  state = reduce(state, {
    type: "cloud_status",
    generation: state.generation,
    status: "waiting",
  });
  states.push(state);
  state = reduce(state, {
    type: "cloud_status",
    generation: state.generation,
    status: "confirmed",
    deviceId: "dev_" + "a".repeat(32),
  });
  states.push(state);
  for (const value of states) {
    const data = { ...definition.data, phase: value.phase };
    for (const ancestors of conditions) {
      const visible = ancestors.every(
        (expr) => !!vm.runInNewContext(`(${expr})`, data),
      );
      assert.equal(
        visible,
        value.phase === "success",
        `success claim in phase ${value.phase}`,
      );
    }
  }
});
test("idle BLE disconnect immediately reports failure while entering Wi-Fi", async () => {
  let connection: any;
  const p = closingPlatform();
  Object.assign(p.wx, {
    createBLEConnection: (o: any) => o.success({}),
    onBLEConnectionStateChange: (callback: any) => {
      connection = callback;
    },
    onBLECharacteristicValueChange: () => {},
    getBLEDeviceServices: (o: any) =>
      o.success({
        services: [{ uuid: "1775244D-6B43-439B-877C-060F2D9BED07" }],
      }),
    getBLEDeviceCharacteristics: (o: any) =>
      o.success({
        characteristics: ["FF51", "FF52", "FF53", "FF50", "FF54"].map((id) => ({
          uuid: `1775${id}-6B43-439B-877C-060F2D9BED07`,
          properties: { read: true, write: true },
        })),
      }),
  });
  const original = BleLink.prototype.discover;
  BleLink.prototype.discover = async () => [
    { deviceId: "synthetic", name: "test" },
  ];
  try {
    const c: any = new Onboarding(p.wx, () => {});
    c.api.start = async () => ({
      id: "task",
      status: "waiting",
      serial: "synthetic",
      model: "test",
      challenge: "synthetic",
      expires_at: new Date(Date.now() + 60000).toISOString(),
    });
    c.setCode("a".repeat(32));
    c.view.families = [{ id: "family", name: "test", canAddDevices: true }];
    await c.start("family", "");
    await c.link.connect("synthetic");
    c.view.state = { phase: "wifi", generation: c.view.state.generation };
    connection({ deviceId: "synthetic", connected: false });
    assert.equal(c.view.state.phase, "error");
    assert.match(c.view.state.error, /断开/);
    await c.leave();
  } finally {
    BleLink.prototype.discover = original;
  }
});
test("artifact verifier rejects literal and always-visible success conditions", async () => {
  const wxml = await readFile(
    new URL("../src/pages/index/index.wxml", import.meta.url),
    "utf8",
  );
  verifyWxml(wxml, definition.data);
  assert.throws(
    () =>
      verifyWxml(
        wxml.replace('wx:if="{{error}}"', 'wx:if="error"'),
        definition.data,
      ),
    /native expression/,
  );
  assert.throws(
    () =>
      verifyWxml(
        wxml.replace("wx:if=\"{{phase==='success'}}\"", 'wx:if="{{true}}"'),
        definition.data,
      ),
    /must not claim success/,
  );
});

test("hidden pending login cannot restore cookie, families or feedback", async () => {
  let finish: any;
  (globalThis as any).wx.request = (options: any) => {
    finish = options;
  };
  const p = page();
  p.setData({ username: "synthetic", loginPassword: "synthetic" });
  const logging = p.login();
  p.onHide();
  p.setData({ error: "new feedback" });
  finish.success({
    statusCode: 200,
    data: { ok: true },
    cookies: ["management_family=synthetic"],
  });
  await logging;
  assert.equal(p.controller.api.cookie, "");
  assert.equal(p.data.loggedIn, false);
  assert.equal(p.controller.view.families.length, 0);
  assert.equal(p.data.error, "new feedback");
  p.onUnload();
});
test("expired family session invalidates a hidden queued scanner result", async () => {
  (globalThis as any).wx.request = (o: any) =>
    o.success({ statusCode: 401, data: {} });
  const p = page();
  p.scan();
  p.onHide();
  scan.success({ result: "a".repeat(32) });
  await assert.rejects(p.controller.api.families(), /重新登录/);
  p.onShow();
  assert.equal(p.controller.code, "");
  assert.equal(p.scanOperation, undefined);
  p.onUnload();
});

test("onShow enforces scan wall-clock expiry even if background timers paused", (t) => {
  const p = page();
  p.scan();
  p.onHide();
  scan.success({ result: "a".repeat(32) });
  const future = Date.now() + 60001;
  t.mock.method(Date, "now", () => future);
  p.onShow();
  assert.equal(p.controller.code, "");
  assert.equal(p.scanOperation, undefined);
  assert.match(p.data.error, /超时/);
  p.onUnload();
});

test("hide between login acknowledgement and family lookup stops the continuation", async () => {
  let requests = 0;
  (globalThis as any).wx.request = (options: any) => {
    requests++;
    options.success({
      statusCode: 200,
      data:
        requests === 1
          ? { ok: true }
          : [{ id: "family", name: "test", canAddDevices: true }],
    });
  };
  const p = page();
  p.setData({ username: "synthetic", loginPassword: "synthetic" });
  const logging = p.login();
  p.onHide();
  await logging;
  assert.equal(requests, 1);
  assert.equal(p.controller.view.families.length, 0);
  p.onUnload();
});

function sharedBluetoothPlatform() {
  const p = closingPlatform();
  const opens: any[] = [],
    connects: any[] = [];
  let opened = false,
    discovering = false,
    connected = false;
  Object.assign(p.wx, {
    openBluetoothAdapter: (o: any) => opens.push(o),
    createBLEConnection: (o: any) => connects.push(o),
    closeBluetoothAdapter: (o: any) => {
      opened = false;
      o.success({});
    },
    closeBLEConnection: (o: any) => {
      connected = false;
      o.success({});
    },
    onBluetoothDeviceFound: () => {},
    offBluetoothDeviceFound: () => {},
    onBLEConnectionStateChange: () => {},
    onBLECharacteristicValueChange: () => {},
    startBluetoothDevicesDiscovery: (o: any) => {
      discovering = true;
      o.success({});
    },
    stopBluetoothDevicesDiscovery: (o: any) => {
      discovering = false;
      o.success({});
    },
    getBLEDeviceServices: (o: any) =>
      o.success({
        services: [{ uuid: "1775244D-6B43-439B-877C-060F2D9BED07" }],
      }),
    getBLEDeviceCharacteristics: (o: any) =>
      o.success({
        characteristics: ["FF51", "FF52", "FF53", "FF50", "FF54"].map((id) => ({
          uuid: `1775${id}-6B43-439B-877C-060F2D9BED07`,
          properties: { read: true, write: true },
        })),
      }),
  });
  return {
    wx: p.wx,
    open(index: number) {
      opened = true;
      opens[index].success({});
    },
    connect(index: number) {
      connected = true;
      connects[index].success({});
    },
    get opened() {
      return opened;
    },
    get discovering() {
      return discovering;
    },
    get connected() {
      return connected;
    },
  };
}
for (const timedOut of [false, true]) {
  test(`old adapter late success after ${timedOut ? "timeout" : "cancel"} preserves replacement discovery`, async () => {
    const p = sharedBluetoothPlatform();
    const old = new BleLink(p.wx, 15),
      next = new BleLink(p.wx, 100);
    const oldDiscovery = old.discover();
    const oldRejected = assert.rejects(
      oldDiscovery,
      timedOut ? /超时/ : /取消/,
    );
    if (timedOut) await new Promise((resolve) => setTimeout(resolve, 25));
    else await old.close();
    const discovery = next.discover();
    const rejected = assert.rejects(discovery, /取消/);
    p.open(1);
    await tick();
    assert.equal(p.discovering, true);
    p.open(0);
    await oldRejected;
    await tick();
    assert.equal(p.opened, true);
    assert.equal(p.discovering, true);
    await old.close();
    assert.equal(p.opened, true);
    assert.equal(p.discovering, true);
    await next.close();
    await rejected;
    assert.equal(p.opened, false);
    assert.equal(p.discovering, false);
  });
}
test("old same-device connection late success and cleanup preserve replacement connection", async () => {
  const p = sharedBluetoothPlatform();
  const old = new BleLink(p.wx, 100),
    next = new BleLink(p.wx, 100);
  const connecting = old.connect("synthetic");
  const rejected = assert.rejects(connecting, /取消/);
  await old.close();
  const replacement = next.connect("synthetic");
  p.connect(1);
  await replacement;
  p.connect(0);
  await rejected;
  await tick();
  assert.equal(p.connected, true);
  await old.close();
  assert.equal(p.connected, true);
  await next.close();
  assert.equal(p.connected, false);
});
test("DELETE401 from hidden cancellation invalidates session and erases queued scan", async () => {
  let cancellation: any;
  (globalThis as any).wx.request = (o: any) => {
    cancellation = o;
  };
  const p = page();
  p.controller.api.cookie = "management_family=synthetic";
  p.controller.view.families = [
    { id: "family", name: "test", canAddDevices: true },
  ];
  p.controller.task = { id: "old-task", status: "waiting" };
  p.controller.password = "synthetic";
  p.setData({ loggedIn: true, password: "synthetic" });
  const session = p.controller.sessionEpoch;
  p.scan();
  const scanner = scan;
  p.onHide();
  scanner.success({ result: "a".repeat(32) });
  const queued = p.scanOperation;
  await tick();
  assert.equal(cancellation.method, "DELETE");
  cancellation.success({ statusCode: 401, data: {} });
  await tick();
  assert.ok(p.controller.sessionEpoch > session);
  assert.equal(p.controller.api.cookie, "");
  assert.deepEqual(p.controller.view.families, []);
  assert.equal(queued.code, undefined);
  p.onShow();
  assert.equal(p.controller.code, "");
  assert.equal(p.controller.password, "");
  assert.equal(p.data.password, "");
  assert.equal(p.data.scanned, false);
  assert.equal(p.data.loggedIn, false);
  assert.match(p.data.error, /重新登录/);
  p.onUnload();
});
test("cancel awaiting resource release cannot send old DELETE using a newer login", async () => {
  let release!: () => void;
  const requests: any[] = [];
  const c: any = new Onboarding(
    {
      request: (o: any) => {
        requests.push(o);
      },
    },
    () => {},
  );
  c.task = { id: "old-task", status: "waiting" };
  c.link = {
    close: () =>
      new Promise<void>((resolve) => {
        release = resolve;
      }),
  };
  const cancellation = c.cancel();
  const logging = c.login("synthetic", "synthetic");
  requests[0].success({
    statusCode: 200,
    data: {},
    cookies: ["management_family=new"],
  });
  await tick();
  requests[1].success({
    statusCode: 200,
    data: [{ id: "new-family", name: "new", canAddDevices: true }],
  });
  await logging;
  release();
  await tick();
  assert.equal(requests.filter((o) => o.method === "DELETE").length, 0);
  await cancellation;
  assert.equal(c.api.cookie, "management_family=new");
  assert.equal(c.view.families[0].id, "new-family");
});

test("older discovery finally and cleanup resumed after connection close preserve replacement adapter", async () => {
  const p = sharedBluetoothPlatform();
  const old = new BleLink(p.wx, 100),
    next = new BleLink(p.wx, 100);
  const oldDiscovery = old.discover();
  const oldRejected = assert.rejects(oldDiscovery, /取消/);
  p.open(0);
  await tick();
  const connecting = old.connect("synthetic");
  p.connect(0);
  await connecting;
  let finishClose!: () => void;
  p.wx.closeBLEConnection = (o: any) => {
    finishClose = () => o.success({});
  };
  const closing = old.close();
  const discovery = next.discover();
  const rejected = assert.rejects(discovery, /取消/);
  p.open(1);
  await tick();
  await oldRejected;
  assert.equal(p.discovering, true);
  finishClose();
  await closing;
  assert.equal(p.opened, true);
  assert.equal(p.discovering, true);
  await next.close();
  await rejected;
});
