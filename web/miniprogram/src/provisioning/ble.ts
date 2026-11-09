import { ClientError } from "../api.ts";
export const SERVICE = "1775244D-6B43-439B-877C-060F2D9BED07";
const TIMEOUT = 12000;
function failureMessage(name: string, error: unknown): string {
  // Only discovery APIs are safe to expose: protocol writes may carry secrets.
  const stage =
    name === "openBluetoothAdapter"
      ? "开启蓝牙失败"
      : name === "startBluetoothDevicesDiscovery"
        ? "开始搜索设备失败"
        : name === "getBluetoothDevices"
          ? "读取附近设备失败"
          : "";
  if (!stage) return "蓝牙操作失败，请检查权限并靠近设备";
  const details =
    error && typeof error === "object"
      ? (error as { errCode?: unknown; errMsg?: unknown })
      : {};
  const code = Number.isSafeInteger(details.errCode)
    ? `（errCode: ${details.errCode}）`
    : "";
  const message =
    typeof details.errMsg === "string"
      ? details.errMsg
          .replace(/[\u0000-\u001f\u007f-\u009f\u00ad\u061c\u200b-\u200f\u2028-\u202e\u2060-\u206f\ufeff]/g, "")
          .trim()
          .slice(0, 160)
      : "";
  return `${stage}${code}${message ? `：${message}` : ""}，请确认手机蓝牙已开启、微信蓝牙权限已允许，并靠近设备`;
}
// wx owns one adapter/discovery and one connection per device, across BleLinks.
const nativeLeases = new WeakMap<object, Map<string, object>>();
export class BleLink {
  private leases = new Map<string, object>();
  private native: Map<string, object>;
  private exchanging = false;
  private requestId = 0;
  private timeout: number;
  private wx: any;
  private id = "";
  private service = "";
  private endpoints = new Map<string, string>();
  private epoch = 0;
  private pending: ((e: Error) => void) | undefined;
  private disconnected: ((error: ClientError) => void) | undefined;
  constructor(
    platform: any,
    timeout = TIMEOUT,
    disconnected?: (error: ClientError) => void,
  ) {
    this.wx = platform;
    let leases = nativeLeases.get(platform);
    if (!leases) {
      leases = new Map();
      nativeLeases.set(platform, leases);
    }
    this.native = leases;
    this.timeout = timeout;
    this.disconnected = disconnected;
  }
  private discoveryHandler: ((r: any) => void) | undefined;
  private wakeDiscovery: (() => void) | undefined;
  private call(
    name: string,
    args: any = {},
    timeout = this.timeout,
    lateSuccess?: () => void,
  ): Promise<any> {
    return new Promise((resolve, reject) => {
      if (typeof this.wx[name] !== "function") {
        reject(new ClientError("ble_support", "请更新微信以支持安全蓝牙配网"));
        return;
      }
      let settled = false;
      const timer = setTimeout(() => {
        settled = true;
        reject(new ClientError("ble_timeout", "蓝牙响应超时，请重试"));
      }, timeout);
      this.wx[name]({
        ...args,
        success: (r: any) => {
          if (settled) {
            lateSuccess?.();
            return;
          }
          settled = true;
          clearTimeout(timer);
          resolve(r);
        },
        fail: (error: unknown) => {
          if (settled) return;
          settled = true;
          clearTimeout(timer);
          reject(
            new ClientError("ble_error", failureMessage(name, error)),
          );
        },
      });
    });
  }
  private claim(resource: string) {
    const lease = {};
    this.native.set(resource, lease);
    this.leases.set(resource, lease);
    return lease;
  }
  private async closeLease(
    resource: string,
    lease: object,
    name: string,
    args: any = {},
    lateSuccess = false,
  ) {
    const owner = this.native.get(resource);
    // Late native success may need compensation after close removed our lease,
    // but can never close a resource acquired by a replacement operation.
    if (owner !== lease && (owner !== undefined || !lateSuccess)) return;
    if (owner === lease) this.native.delete(resource);
    if (this.leases.get(resource) === lease) this.leases.delete(resource);
    await this.call(name, args).catch(() => {});
  }
  private async acquire(
    resource: string,
    name: string,
    args: any,
    closeName: string,
    closeArgs: any,
    generation: number,
  ) {
    const lease = this.claim(resource);
    const compensate = () =>
      this.closeLease(resource, lease, closeName, closeArgs, true);
    await this.call(name, args, this.timeout, () => {
      void compensate();
    });
    if (generation !== this.epoch) {
      await compensate();
      throw new ClientError("cancelled", "操作已取消");
    }
  }
  async discover(): Promise<
    {
      deviceId: string;
      name: string;
    }[]
  > {
    const generation = this.epoch;
    const check = () => {
      if (generation !== this.epoch)
        throw new ClientError("cancelled", "操作已取消");
    };
    await this.acquire(
      "adapter",
      "openBluetoothAdapter",
      {},
      "closeBluetoothAdapter",
      {},
      generation,
    );
    check();
    const found = new Map<
      string,
      {
        deviceId: string;
        name: string;
      }
    >();
    const onFound = (r: any) => {
      for (const d of r.devices || [])
        if (
          (d.advertisServiceUUIDs || []).some(
            (u: string) => u.toUpperCase() === SERVICE,
          )
        )
          found.set(d.deviceId, {
            deviceId: d.deviceId,
            name: d.localName || d.name || "附近的机器人",
          });
    };
    this.discoveryHandler = onFound;
    this.wx.onBluetoothDeviceFound(onFound);
    const discoveryLease = this.claim("discovery");
    try {
      await this.call(
        "startBluetoothDevicesDiscovery",
        {
          services: [SERVICE],
          allowDuplicatesKey: false,
        },
        this.timeout,
        () => {
          void this.closeLease(
            "discovery",
            discoveryLease,
            "stopBluetoothDevicesDiscovery",
            {},
            true,
          );
        },
      );
      check();
      await new Promise<void>((resolve) => {
        const timer = setTimeout(resolve, 5000);
        this.wakeDiscovery = () => {
          clearTimeout(timer);
          resolve();
        };
      });
      check();
      const r = await this.call("getBluetoothDevices");
      check();
      onFound(r);
      return [...found.values()];
    } finally {
      this.wakeDiscovery = undefined;
      this.discoveryHandler = undefined;
      this.wx.offBluetoothDeviceFound(onFound);
      await this.closeLease(
        "discovery",
        discoveryLease,
        "stopBluetoothDevicesDiscovery",
      );
    }
  }
  async connect(id: string): Promise<void> {
    this.id = id;
    const generation = ++this.epoch;
    await this.acquire(
      "connection:" + id,
      "createBLEConnection",
      { deviceId: id, timeout: 12000 },
      "closeBLEConnection",
      { deviceId: id },
      generation,
    );
    if (generation !== this.epoch)
      throw new ClientError("cancelled", "操作已取消");
    this.wx.onBLEConnectionStateChange(this.onConnection);
    const services = await this.call("getBLEDeviceServices", { deviceId: id });
    this.service =
      services.services.find((s: any) => s.uuid.toUpperCase() === SERVICE)
        ?.uuid || "";
    if (!this.service)
      throw new ClientError("ble_service", "设备没有安全配网服务");
    const r = await this.call("getBLEDeviceCharacteristics", {
      deviceId: id,
      serviceId: this.service,
    });
    if (generation !== this.epoch)
      throw new ClientError("cancelled", "操作已取消");
    // Official Espressif BLE UUID mapping, locked by matching firmware adapter.
    const mapping: Record<string, string> = {
      FF51: "prov-session",
      FF52: "prov-config",
      FF53: "proto-ver",
      FF50: "prov-scan",
      FF54: "family-claim",
    };
    for (const c of r.characteristics || []) {
      const short = c.uuid.toUpperCase().slice(4, 8);
      if (
        c.uuid.toUpperCase() ===
          SERVICE.slice(0, 4) + short + SERVICE.slice(8) &&
        mapping[short] &&
        c.properties.read &&
        c.properties.write
      )
        this.endpoints.set(mapping[short], c.uuid);
    }
    for (const name of [
      "prov-session",
      "prov-config",
      "proto-ver",
      "prov-scan",
      "family-claim",
    ])
      if (!this.endpoints.has(name))
        throw new ClientError(
          "ble_endpoints",
          "设备配网版本不兼容，请更新固件",
        );
    this.wx.onBLECharacteristicValueChange(this.onValue);
  }
  private onConnection = (r: any) => {
    if (
      r.deviceId === this.id &&
      !r.connected &&
      this.native.get("connection:" + this.id) ===
        this.leases.get("connection:" + this.id)
    ) {
      this.epoch++;
      const error = new ClientError(
        "ble_disconnected",
        "蓝牙已断开，请重新扫码",
      );
      this.pending?.(error);
      this.disconnected?.(error);
    }
  };
  private valueHandler: ((r: any) => void) | undefined;
  private onValue = (r: any) => this.valueHandler?.(r);
  private header(op: number, id: number, offset: number, total: number) {
    return new Uint8Array([
      0x46,
      0x52,
      1,
      op,
      id & 255,
      id >>> 8,
      offset & 255,
      offset >>> 8,
      total & 255,
      total >>> 8,
    ]);
  }
  async read(endpoint: string): Promise<Uint8Array> {
    return this.exchange(endpoint, new Uint8Array([69, 83, 80]));
  }
  async exchange(endpoint: string, payload: Uint8Array): Promise<Uint8Array> {
    if (this.exchanging) throw new ClientError("ble_busy", "蓝牙请求尚未完成");
    const characteristicId = this.endpoints.get(endpoint);
    if (!this.id || !characteristicId)
      throw new ClientError("ble_endpoint", "安全配网端点不可用");
    if (!payload.length || payload.length > 481)
      throw new ClientError("ble_size", "安全配网数据长度无效");
    this.exchanging = true;
    const generation = this.epoch;
    const id = (this.requestId = (this.requestId % 65535) + 1);
    const started = Date.now();
    const remaining = () => {
      if (generation !== this.epoch)
        throw new ClientError("cancelled", "操作已取消");
      const n = Math.min(this.timeout, 10000, 30000 - (Date.now() - started));
      if (n <= 0) throw new ClientError("ble_timeout", "蓝牙响应超时，请重试");
      return n;
    };
    const write = (value: Uint8Array) =>
      this.call(
        "writeBLECharacteristicValue",
        {
          deviceId: this.id,
          serviceId: this.service,
          characteristicId,
          value: value.slice().buffer,
          writeType: "write",
        },
        remaining(),
      );
    try {
      for (let offset = 0; offset < payload.length; offset += 10) {
        const part = payload.slice(offset, offset + 10),
          frame = new Uint8Array(10 + part.length);
        frame.set(this.header(1, id, offset, payload.length));
        frame.set(part, 10);
        await write(frame);
      }
      let offset = 0,
        total = -1;
      let assembled = new Uint8Array();
      do {
        const response = await this.readFrame(
          characteristicId,
          this.header(2, id, offset, 0),
          remaining(),
        );
        if (
          response.length < 10 ||
          response.length > 20 ||
          response[0] !== 0x46 ||
          response[1] !== 0x52 ||
          response[2] !== 1 ||
          response[3] !== 3 ||
          response[4] + 256 * response[5] !== id ||
          response[6] + 256 * response[7] !== offset
        )
          throw new ClientError("ble_frame", "蓝牙安全分包响应不匹配");
        const length = response[8] + 256 * response[9],
          part = response.subarray(10);
        if (
          length > 481 ||
          (total !== -1 && length !== total) ||
          offset + part.length > length ||
          (!part.length && length !== 0)
        )
          throw new ClientError("ble_frame", "蓝牙安全分包长度无效");
        if (total === -1) {
          total = length;
          assembled = new Uint8Array(total);
        }
        assembled.set(part, offset);
        offset += part.length;
      } while (offset < total);
      return assembled;
    } catch (error) {
      await this.close();
      throw error;
    } finally {
      this.exchanging = false;
    }
  }
  private readFrame(
    characteristicId: string,
    request: Uint8Array,
    timeout: number,
  ): Promise<Uint8Array> {
    return new Promise((resolve, reject) => {
      let settled = false;
      const finish = (error?: Error, result?: Uint8Array) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        this.pending = undefined;
        this.valueHandler = undefined;
        error ? reject(error) : resolve(result!);
      };
      const timer = setTimeout(
        () => finish(new ClientError("ble_timeout", "蓝牙响应超时，请重试")),
        timeout,
      );
      this.pending = (error) => finish(error);
      this.valueHandler = (r: any) => {
        if (
          r.deviceId === this.id &&
          r.characteristicId.toUpperCase() === characteristicId.toUpperCase()
        )
          finish(undefined, new Uint8Array(r.value));
      };
      this.call(
        "writeBLECharacteristicValue",
        {
          deviceId: this.id,
          serviceId: this.service,
          characteristicId,
          value: request.slice().buffer,
          writeType: "write",
        },
        timeout,
      )
        .then(() => {
          if (!settled)
            return this.call(
              "readBLECharacteristicValue",
              { deviceId: this.id, serviceId: this.service, characteristicId },
              timeout,
            );
        })
        .catch(() =>
          finish(new ClientError("ble_write", "蓝牙读写失败，请重试")),
        );
    });
  }
  async close() {
    this.epoch++;
    this.wakeDiscovery?.();
    if (this.discoveryHandler)
      this.wx.offBluetoothDeviceFound(this.discoveryHandler);
    this.discoveryHandler = undefined;
    this.pending?.(new ClientError("cancelled", "操作已取消"));
    this.wx.offBLECharacteristicValueChange(this.onValue);
    this.wx.offBLEConnectionStateChange(this.onConnection);
    const owned = [...this.leases];
    this.id = "";
    this.endpoints.clear();
    // Snapshot tokens before yielding. Recheck each against the actual wx owner
    // immediately before cleanup, including discovery's finally path.
    for (const [resource, lease] of owned) {
      if (resource.startsWith("connection:"))
        await this.closeLease(resource, lease, "closeBLEConnection", {
          deviceId: resource.slice(11),
        });
    }
    for (const [resource, lease] of owned) {
      if (resource === "discovery")
        await this.closeLease(resource, lease, "stopBluetoothDevicesDiscovery");
    }
    for (const [resource, lease] of owned) {
      if (resource === "adapter")
        await this.closeLease(resource, lease, "closeBluetoothAdapter");
    }
  }
}
