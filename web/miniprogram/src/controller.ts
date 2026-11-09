import { Api, ClientError, claimCode, type Family, type Task } from "./api.ts";
import { initial, reduce, type State } from "./state.ts";
import { BleLink } from "./provisioning/ble.ts";
import type { Security2Session } from "./provisioning/security2.ts";
import { WifiProvisioner, utf8, text } from "./provisioning/wifi.ts";
export type View = {
  state: State;
  families: Family[];
  devices: {
    deviceId: string;
    name: string;
  }[];
  networks: string[];
  serial: string;
  model: string;
  progress: string;
};
export class Onboarding {
  private api: Api;
  private platform: any;
  private view: View = {
    state: { ...initial },
    families: [],
    devices: [],
    networks: [],
    serial: "",
    model: "",
    progress: "",
  };
  private code = "";
  private password = "";
  private task?: Task;
  private link?: BleLink;
  private security?: Security2Session;
  private wifi?: WifiProvisioner;
  private connecting = false;
  private scanning = false;
  private sessionGeneration = 0;
  get sessionEpoch() {
    return this.sessionGeneration;
  }
  private deadline = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private emit: (view: View) => void;
  constructor(platform: any, emit: (view: View) => void) {
    this.platform = platform;
    this.api = new Api(platform, (error) => {
      this.sessionGeneration++;
      this.view.families = [];
      this.view.state = {
        phase: "error",
        generation: this.view.state.generation + 1,
        error: error.message,
      };
      this.failure(error, this.view.state.generation);
    });
    this.emit = emit;
  }
  private publish() {
    this.emit({ ...this.view, state: { ...this.view.state } });
  }
  private event(event: any) {
    this.view.state = reduce(this.view.state, event);
    this.publish();
  }
  private valid(generation: number) {
    return this.view.state.generation === generation;
  }
  private failure(error: unknown, generation: number) {
    if (!this.valid(generation)) return;
    this.code = "";
    this.password = "";
    this.wifi = undefined;
    this.security?.destroy();
    this.security = undefined;
    void this.link?.close();
    this.event({
      type: "error",
      generation,
      error:
        error instanceof ClientError
          ? error.message
          : "安全配网失败，请重新扫码重试",
    });
  }
  setCode(value: string) {
    this.code = claimCode(value);
  }
  async login(username: string, password: string) {
    const generation = this.view.state.generation;
    const session = ++this.sessionGeneration;
    this.api.clear();
    this.view.families = [];
    this.publish();
    await this.api.login(username, password);
    if (!this.valid(generation) || session !== this.sessionGeneration)
      throw new ClientError("cancelled", "操作已取消");
    const families = await this.api.families();
    if (!this.valid(generation) || session !== this.sessionGeneration)
      throw new ClientError("cancelled", "操作已取消");
    this.view.families = families.filter((f) => f.canAddDevices);
    this.publish();
    if (!this.view.families.length)
      throw new ClientError("permission", "当前账号没有可添加设备的家庭");
  }
  async start(familyId: string, name: string) {
    if (["connecting", "wifi", "applying", "waiting_cloud"].includes(this.view.state.phase)) return;
    if (!this.code) throw new ClientError("scan", "请先扫码");
    if (!this.view.families.some((f) => f.id === familyId))
      throw new ClientError("family", "请选择有权限的家庭");
    const code = this.code;
    const displayName = name.trim();
    // Own the operation before release yields; cancellation must invalidate it.
    this.event({ type: "start" });
    const generation = this.view.state.generation;
    await this.release();
    if (!this.valid(generation)) return;
    try {
      const task = await this.api.start(familyId, code, displayName);
      if (!this.valid(generation)) return;
      this.task = task;
      this.deadline = Math.min(
        Date.now() + 180000,
        Date.parse(task.expires_at),
      );
      this.view.serial = task.serial;
      this.view.model = task.model;
      if (task.status === "confirmed") {
        if (!/^dev_[a-f0-9]{32}$/.test(task.device_id || ""))
          throw new ClientError("task", "云端绑定响应无效");
        this.code = "";
        this.event({
          type: "cloud_status",
          generation,
          status: task.status,
          deviceId: task.device_id,
        });
        return;
      }
      if (
        task.status !== "waiting" ||
        !/^FAMILY_[a-f0-9]{12}$/.test(task.ble_name || "") ||
        !task.challenge ||
        !task.serial ||
        !Number.isFinite(Date.parse(task.expires_at))
      )
        throw new ClientError("task", "认领任务无效，请重新扫码");
      this.view.progress = "正在寻找机器人";
      this.publish();
      this.link = new BleLink(this.platform, undefined, (error) =>
        this.failure(error, generation),
      );
      const devices = await this.link.discover();
      if (!this.valid(generation)) return;
      const matches = devices.filter((d) => d.name === task.ble_name);
      const targets = [...new Set(matches.map((d) => d.deviceId))];
      if (targets.length !== 1)
        throw new ClientError("ble_missing", targets.length ? "发现多个同名机器人，请重新扫码重试" : "没有找到对应机器人，请让机器人进入配网模式后重新扫码");
      this.view.devices = matches;
      await this.connect(targets[0]);
    } catch (error) {
      this.failure(error, generation);
    }
  }
  async connect(deviceId: string) {
    const generation = this.view.state.generation;
    if (
      this.connecting ||
      this.view.state.phase !== "connecting" ||
      !this.link ||
      !this.task ||
      !this.code ||
      !this.view.devices.some((d) => d.deviceId === deviceId)
    )
      return;
    this.connecting = true;
    try {
      this.view.progress = "正在验证设备身份";
      this.publish();
      await this.link.connect(deviceId);
      if (!this.valid(generation)) return;
      const version = JSON.parse(text(await this.link.read("proto-ver")));
      if (version.prov?.sec_ver !== 2 || version.prov?.sec_patch_ver !== 1)
        throw new ClientError(
          "security_version",
          "设备安全配网版本不兼容，请更新固件",
        );
      if (typeof BigInt !== "function" || !this.platform.getRandomValues)
        throw new ClientError("crypto_support", "请更新微信以支持安全配网");
      const random = (length: number): Promise<Uint8Array> =>
        new Promise((resolve, reject) =>
          this.platform.getRandomValues({
            length,
            success: (r: any) => {
              const bytes = new Uint8Array(r.randomValues);
              if (bytes.length !== length) reject(new Error("rng"));
              else resolve(bytes);
            },
            fail: () => reject(new Error("rng")),
          }),
        );
      const Session = require("../../provisioning/security2.js")
        .Security2Session as typeof Security2Session;
      this.security = new Session(this.task.serial, this.code, random);
      await this.security.establish((payload) =>
        this.link!.exchange("prov-session", payload),
      );
      if (!this.valid(generation)) return;
      this.code = "";
      const exchange = async (endpoint: string, payload: Uint8Array) => {
        if (!this.valid(generation) || !this.security || !this.link)
          throw new ClientError("cancelled", "操作已取消");
        const encrypted = this.security.encrypt(payload);
        const response = await this.link.exchange(endpoint, encrypted);
        if (!this.valid(generation) || !this.security)
          throw new ClientError("cancelled", "操作已取消");
        return this.security.decrypt(response);
      };
      const accepted = JSON.parse(
        text(
          await exchange(
            "family-claim",
            utf8(
              JSON.stringify({
                id: this.task.id,
                challenge: this.task.challenge,
              }),
            ),
          ),
        ),
      );
      if (accepted.ok !== true)
        throw new ClientError(
          "claim_rejected",
          "设备拒绝了认领任务，请重新扫码",
        );
      this.wifi = new WifiProvisioner(exchange);
      this.view.progress = "正在扫描 Wi-Fi";
      this.publish();
      const networks = await this.wifi.scan();
      if (!this.valid(generation)) return;
      this.view.networks = networks;
      this.event({ type: "secure", generation });
    } catch (error) {
      this.failure(error, generation);
    } finally {
      if (this.valid(generation)) this.connecting = false;
    }
  }
  async scanWifi() {
    if (this.scanning || this.view.state.phase !== "wifi") return;
    this.scanning = true;
    const generation = this.view.state.generation;
    try {
      if (!this.wifi) throw new ClientError("security", "请先建立安全连接");
      const networks = await this.wifi.scan();
      if (!this.valid(generation)) return;
      this.view.networks = networks;
      this.publish();
    } catch (error) {
      this.failure(error, generation);
    } finally {
      this.scanning = false;
    }
  }
  async apply(ssid: string, password: string) {
    const generation = this.view.state.generation;
    if (this.scanning || this.view.state.phase !== "wifi" || !this.wifi) return;
    this.password = password;
    try {
      this.event({ type: "apply", generation });
      await this.wifi.apply(ssid, this.password);
      this.password = "";
      if (!this.valid(generation)) return;
      this.event({ type: "ble_applied", generation });
      await this.release();
      void this.poll(generation);
    } catch (error) {
      this.failure(error, generation);
    }
  }
  private async poll(generation: number) {
    if (!this.valid(generation) || !this.task) return;
    if (Date.now() >= this.deadline) {
      this.event({ type: "cloud_status", generation, status: "expired" });
      return;
    }
    try {
      const task = await this.api.status(this.task.id);
      if (!this.valid(generation)) return;
      if (
        task.status === "confirmed" &&
        !/^dev_[a-f0-9]{32}$/.test(task.device_id || "")
      )
        throw new ClientError("task", "云端绑定响应无效");
      this.event({
        type: "cloud_status",
        generation,
        status: task.status,
        deviceId: task.device_id,
      });
      if (task.status === "waiting")
        this.timer = setTimeout(() => void this.poll(generation), 2000);
    } catch (error) {
      this.failure(error, generation);
    }
  }
  private async release() {
    this.password = "";
    this.connecting = false;
    this.security?.destroy();
    this.security = undefined;
    this.wifi = undefined;
    const link = this.link;
    this.link = undefined;
    if (link) await link.close();
  }
  async cancel() {
    const task = this.task;
    const session = this.sessionGeneration;
    this.task = undefined;
    this.code = "";
    this.view.devices = [];
    this.view.networks = [];
    this.event({ type: "cancel" });
    if (this.timer) clearTimeout(this.timer);
    await this.release();
    if (task?.status === "waiting" && session === this.sessionGeneration)
      // Api publishes a current-session 401 even for this best-effort DELETE.
      await this.api.cancel(task.id).catch(() => {});
  }
  async suspend() {
    this.api.invalidatePending();
    this.view.serial = "";
    this.view.model = "";
    await this.cancel();
  }
  async leave() {
    this.sessionGeneration++;
    this.api.clear();
    this.view.families = [];
    this.view.serial = "";
    this.view.model = "";
    await this.cancel();
  }
}
