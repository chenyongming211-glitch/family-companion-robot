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
test("returning to page does not reactivate a scanner from before hide", () => {
  const p = page();
  p.scan();
  const old = scan;
  p.onHide();
  p.onShow?.();
  old.success({ result: "a".repeat(32) });
  assert.equal(p.controller.code, "");
  p.scan();
  scan.success({ result: "b".repeat(32) });
  assert.equal(p.controller.code, "b".repeat(32));
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
