import { Onboarding } from "../../controller.ts";
import { setCleanup, setLaunch } from "../../lifecycle.ts";
import { launchClaim, scannedClaim } from "../../api.ts";
function errorMessage(e: unknown) {
  return e instanceof Error ? e.message : "操作失败，请重试";
}
Page({
  data: {
    phase: "idle",
    error: "",
    loggedIn: false,
    scanned: false,
    username: "",
    loginPassword: "",
    families: [],
    familyIndex: -1,
    familyNames: [],
    progress: "",
    devices: [],
    networks: [],
    ssid: "",
    password: "",
    serial: "",
    model: "",
    deviceId: "",
  },
  controller: undefined as Onboarding | undefined,
  active: false,
  scanEpoch: 0,
  visibilityEpoch: 0,
  scanOperation: undefined as
    | {
        epoch: number;
        sessionEpoch: number;
        expiresAt: number;
        done: boolean;
        code?: string;
        error?: string;
      }
    | undefined,
  scanTimer: undefined as ReturnType<typeof setTimeout> | undefined,
  scanNotice: "",
  readyCode: "",
  readyExpiresAt: 0,
  readySessionEpoch: 0,
  loginContinuing: false,
  accountAttempted: false,
  onLoad(this: any, options: any) {
    this.active = true;
    this.controller = new Onboarding(wx, (view) => {
      if (
        (this.scanOperation &&
        this.scanOperation.sessionEpoch !== this.controller.sessionEpoch) ||
        (this.readyCode && this.readySessionEpoch !== this.controller.sessionEpoch)
      )
        if (!this.loginContinuing) this.invalidateScan();
      this.setData({
        loggedIn: view.families.length > 0,
        phase: view.state.phase,
        error: view.state.error || "",
        deviceId: view.state.deviceId || "",
        families: view.families,
        familyNames: view.families.map((f) => f.name),
        progress: view.progress,
        networks: view.networks,
        serial: view.serial,
        model: view.model,
      });
    });
    const cleanup = (terminal = false) => {
      this.readyCode = "";
      if (this.scanTimer && !this.scanOperation) clearTimeout(this.scanTimer);
      this.active = false;
      this.visibilityEpoch++;
      if (terminal) {
        this.invalidateScan();
        void this.controller?.leave();
      } else void this.controller?.suspend();
      this.setData({
        password: "",
        loginPassword: "",
        scanned: false,
        ssid: "",
      });
    };
    setCleanup(() => cleanup());
    this.cleanup = cleanup;
    setLaunch((options) => this.receiveLaunch(options));
    if (options.scene !== undefined)
      this.receiveLaunch({
        path: "pages/index/index",
        query: { scene: options.scene },
      });
  },
  onShow(this: any) {
    this.active = true;
    this.applyScan();
    if (this.scanNotice) {
      this.setData({ error: this.scanNotice });
      this.scanNotice = "";
    }
  },
  onHide(this: any) {
    this.cleanup?.();
  },
  onUnload(this: any) {
    this.cleanup?.(true);
    setCleanup(undefined);
    setLaunch(undefined);
  },
  input(this: any, e: any) {
    const key = e.currentTarget.dataset.key;
    if (key === "username" && (this.scanOperation || this.data.loggedIn || this.accountAttempted) && e.detail.value !== this.data.username)
      this.invalidateScan();
    if (["username", "loginPassword", "ssid", "password"].includes(key))
      this.setData({ [key]: e.detail.value });
  },
  family(this: any, e: any) {
    const index = Number(e.detail.value);
    if (!Number.isInteger(index) || !this.data.families[index]) return;
    this.setData({ familyIndex: index });
    this.continueScan();
  },
  network(this: any, e: any) {
    this.setData({ ssid: this.data.networks[Number(e.detail.value)] || "" });
  },
  async login(this: any) {
    this.accountAttempted = true;
    const pending = this.readyCode;
    const expiresAt = this.readyExpiresAt;
    this.invalidateScan();
    if (pending && Date.now() < expiresAt) this.retainCode(pending, expiresAt);
    const epoch = this.scanEpoch;
    this.loginContinuing = true;
    const visibility = this.visibilityEpoch;
    const password = this.data.loginPassword;
    this.setData({ loginPassword: "", error: "" });
    try {
      await this.controller.login(this.data.username, password);
      if (this.active && visibility === this.visibilityEpoch) {
        this.setData({ loggedIn: true, familyIndex: this.data.families.length === 1 ? 0 : -1 });
        if (this.readyCode && epoch === this.scanEpoch && Date.now() < expiresAt) {
          this.readySessionEpoch = this.controller.sessionEpoch;
          this.controller.setCode(this.readyCode);
          this.continueScan();
        }
      }
    } catch (e) {
      if (epoch === this.scanEpoch) {
        this.invalidateScan();
        if (this.active && visibility === this.visibilityEpoch)
          this.setData({ error: errorMessage(e) });
      }
    } finally { this.loginContinuing = false; }
  },
  invalidateScan(this: any) {
    this.readyCode = "";
    this.scanEpoch++;
    if (this.scanTimer) clearTimeout(this.scanTimer);
    this.scanTimer = undefined;
    if (this.scanOperation) {
      this.scanOperation.code = undefined;
      this.scanOperation.error = undefined;
    }
    this.scanOperation = undefined;
    this.scanNotice = "";
  },
  applyScan(this: any) {
    const operation = this.scanOperation;
    if (!this.active || !operation) return;
    if (Date.now() >= operation.expiresAt) {
      this.invalidateScan();
      this.setData({ error: "扫码已超时，请重新扫描" });
      return;
    }
    if (!operation.done) return;
    if (
      operation.epoch !== this.scanEpoch ||
      operation.sessionEpoch !== this.controller.sessionEpoch
    ) {
      this.invalidateScan();
      return;
    }
    const { code, error } = operation;
    this.invalidateScan();
    if (code) {
      this.retainCode(code, operation.expiresAt);
      this.controller.setCode(code);
      this.setData({ scanned: true, error: "" });
      this.continueScan();
    } else this.setData({ error: error || "扫码未完成，请重试" });
  },
  receiveLaunch(this: any, options: any) {
    if (options?.query?.scene === undefined) return;
    // A new external code supersedes both any scanner and the previous task.
    // Keep the scene only in the existing bounded, session-scoped RAM queue.
    void this.controller.suspend();
    this.setData({
      scanned: false,
      password: "",
      loginPassword: "",
      ssid: "",
    });
    const operation = this.beginScan();
    try {
      operation.code = launchClaim(options);
    } catch (error) {
      operation.error = errorMessage(error);
    }
    operation.done = true;
    this.applyScan();
  },
  beginScan(this: any) {
    this.invalidateScan();
    const operation = {
      epoch: this.scanEpoch,
      sessionEpoch: this.controller.sessionEpoch,
      expiresAt: Date.now() + 60000,
      done: false,
      code: undefined as string | undefined,
      error: undefined as string | undefined,
    };
    this.scanOperation = operation;
    // Keep a hidden result only for this bounded native scan operation. It never
    // reaches controller state until onShow; terminal cleanup erases the queue.
    this.scanTimer = setTimeout(() => {
      if (this.scanOperation !== operation) return;
      this.invalidateScan();
      this.scanNotice = "扫码已超时，请重新扫描";
      if (this.active) {
        this.setData({ error: this.scanNotice });
        this.scanNotice = "";
      }
    }, 60000);
    return operation;
  },
  scan(this: any) {
    if (!this.active) return;
    const operation = this.beginScan();
    const valid = () =>
      this.scanOperation === operation &&
      operation.epoch === this.scanEpoch &&
      operation.sessionEpoch === this.controller.sessionEpoch &&
      Date.now() < operation.expiresAt &&
      !operation.done;
    wx.scanCode({
      onlyFromCamera: true,
      scanType: ["qrCode", "wxCode"],
      success: (r: any) => {
        if (!valid()) return;
        try {
          operation.code = scannedClaim(r);
        } catch (e) {
          operation.error = errorMessage(e);
        }
        operation.done = true;
        this.applyScan();
      },
      fail: () => {
        if (!valid()) return;
        operation.done = true;
        operation.error = "扫码未完成，请重试";
        this.applyScan();
      },
    });
  },
  retainCode(this: any, code: string, expiresAt: number) {
    this.readyCode = code;
    this.readySessionEpoch = this.controller.sessionEpoch;
    this.readyExpiresAt = expiresAt;
    if (this.scanTimer) clearTimeout(this.scanTimer);
    this.scanTimer = setTimeout(() => {
      this.invalidateScan();
      void this.controller.cancel();
      if (this.active) this.setData({ scanned: false, error: "扫码已超时，请重新扫描" });
    }, Math.max(0, expiresAt - Date.now()));
  },
  continueScan(this: any) {
    if (!this.active || !this.readyCode || !this.data.loggedIn) return;
    if (Date.now() >= this.readyExpiresAt) {
      this.invalidateScan();
      this.setData({ scanned: false, error: "扫码已超时，请重新扫描" });
      return;
    }
    const families = this.data.families;
    const index = families.length === 1 ? 0 : this.data.familyIndex;
    if (!families[index]) return;
    this.readyCode = "";
    if (this.scanTimer) clearTimeout(this.scanTimer);
    this.scanTimer = undefined;
    const epoch = this.scanEpoch;
    this.setData({ error: "", scanned: false });
    void this.controller.start(families[index].id, "").catch((e: unknown) => {
      if (this.active && epoch === this.scanEpoch) this.setData({ error: errorMessage(e) });
    });
  },
  async refresh(this: any) {
    await this.controller.scanWifi();
  },
  async apply(this: any) {
    const password = this.data.password;
    this.setData({ password: "" });
    await this.controller.apply(this.data.ssid, password);
  },
  async cancel(this: any) {
    this.invalidateScan();
    this.setData({ password: "", scanned: false, error: "" });
    await this.controller.cancel();
  },
});
