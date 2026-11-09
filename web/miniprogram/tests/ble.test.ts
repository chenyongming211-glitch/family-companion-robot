import { test } from "node:test";
import assert from "node:assert/strict";
import { BleLink, SERVICE } from "../src/provisioning/ble.ts";
function fixture() {
  let value: any;
  let connection: any;
  const writes: Uint8Array[] = [];
  let closed = false;
  let respond = true;
  let corrupt = false;
  let last = new Uint8Array();
  const body = new Uint8Array(Array.from({ length: 37 }, (_, i) => i));
  const wx: any = {
    onBLEConnectionStateChange: (f: any) => (connection = f),
    offBLEConnectionStateChange: () => {},
    onBLECharacteristicValueChange: (f: any) => (value = f),
    offBLECharacteristicValueChange: () => {},
    createBLEConnection: (o: any) => o.success({}),
    setBLEMTU: (o: any) => o.success({}),
    getBLEMTU: (o: any) => o.success({ mtu: 23 }),
    getBLEDeviceServices: (o: any) =>
      o.success({ services: [{ uuid: SERVICE }] }),
    getBLEDeviceCharacteristics: (o: any) =>
      o.success({
        characteristics: ["ff51", "ff52", "ff53", "ff50", "ff54"].map((s) => ({
          uuid: SERVICE.slice(0, 4) + s + SERVICE.slice(8),
          properties: { read: true, write: true },
        })),
      }),
    writeBLECharacteristicValue: (o: any) => {
      last = new Uint8Array(o.value);
      writes.push(last);
      o.success({});
    },
    readBLECharacteristicValue: (o: any) => {
      o.success({});
      if (respond) {
        const offset = last[6] + 256 * last[7],
          part = body.slice(offset, offset + 10);
        const frame = new Uint8Array(10 + part.length);
        frame.set(last.slice(0, 10));
        frame[3] = 3;
        frame[8] = body.length;
        frame[9] = 0;
        frame.set(part, 10);
        if (corrupt) frame[4] ^= 1;
        value({
          deviceId: "phone",
          characteristicId: o.characteristicId,
          value: frame.buffer,
        });
      }
    },
    closeBLEConnection: (o: any) => {
      closed = true;
      o.success({});
    },
    stopBluetoothDevicesDiscovery: (o: any) => o.success({}),
    closeBluetoothAdapter: (o: any) => o.success({}),
  };
  return {
    wx,
    writes,
    body,
    get closed() {
      return closed;
    },
    noResponse() {
      respond = false;
    },
    corrupt() {
      corrupt = true;
    },
    disconnect() {
      connection({ deviceId: "phone", connected: false });
    },
  };
}
test("20-byte baseline frames reassemble full encrypted payload and response", async () => {
  const f = fixture();
  const link = new BleLink(f.wx, 25);
  await link.connect("phone");
  const payload = new Uint8Array(430).fill(3);
  assert.deepEqual(await link.exchange("prov-session", payload), f.body);
  const fragments = f.writes.filter((p) => p[3] === 1);
  assert.equal(fragments.length, 43);
  assert.ok(f.writes.every((p) => p.length <= 20));
  assert.deepEqual(
    new Uint8Array(fragments.flatMap((p) => [...p.subarray(10)])),
    payload,
  );
  await link.close();
  assert.equal(f.closed, true);
});
test("missing response times out and disconnect rejects pending request", async () => {
  const f = fixture();
  let link = new BleLink(f.wx, 25);
  await link.connect("phone");
  f.noResponse();
  await assert.rejects(
    link.exchange("prov-session", new Uint8Array([3])),
    /超时/,
  );
  assert.equal(f.closed, true);
  link = new BleLink(f.wx, 25);
  await link.connect("phone");
  const p = link.exchange("prov-session", new Uint8Array([3]));
  await new Promise((resolve) => setTimeout(resolve, 0));
  f.disconnect();
  await assert.rejects(p, /断开/);
  await link.close();
});
test("cancel rejects pending BLE and removes connection", async () => {
  const f = fixture();
  const link = new BleLink(f.wx, 25);
  await link.connect("phone");
  f.noResponse();
  const p = link.exchange("prov-session", new Uint8Array([3]));
  const rejection = assert.rejects(p, /取消/);
  await link.close();
  await rejection;
  assert.equal(f.closed, true);
});
test("mismatched frame identity closes transport without plaintext fallback", async () => {
  const f = fixture();
  f.corrupt();
  const link = new BleLink(f.wx, 25);
  await link.connect("phone");
  await assert.rejects(
    link.exchange("prov-session", new Uint8Array([3])),
    /不匹配/,
  );
  assert.equal(f.closed, true);
  assert.ok(f.writes.every((p) => p[0] === 0x46 && p[1] === 0x52));
});
test("leaving during discovery removes listener immediately and rejects stale results", async () => {
  let listener: any;
  let removed = false;
  const f = fixture();
  Object.assign(f.wx, {
    openBluetoothAdapter: (o: any) => o.success({}),
    onBluetoothDeviceFound: (cb: any) => (listener = cb),
    offBluetoothDeviceFound: (cb: any) => {
      if (cb === listener) removed = true;
    },
    startBluetoothDevicesDiscovery: (o: any) => o.success({}),
    getBluetoothDevices: () => assert.fail("must not fetch after cancellation"),
  });
  const link = new BleLink(f.wx, 25);
  const p = link.discover();
  const rejection = assert.rejects(p, /取消/);
  await new Promise((resolve) => setTimeout(resolve, 0));
  await link.close();
  await rejection;
  assert.equal(removed, true);
});
