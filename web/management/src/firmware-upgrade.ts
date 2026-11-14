import type { Api } from "./api.ts";
import type { DeviceFirmware, FirmwareJob, Scope } from "./types.ts";

export function firmwareUpgradeState() {
  return { view: null as DeviceFirmware | null, pending: false, needsRead: true, feedback: "", confirming: null as string | null };
}
function active(job: FirmwareJob | null) {
  return !!job && ["queued", "sent", "downloading", "restarting"].includes(job.status);
}
export function firmwareCandidates(view: DeviceFirmware) {
  const current = view.current;
  return view.releases.filter(release => {
    if (!current) return true;
    if (release.elfSha256 === current.elfSha256) return false;
    const target = release.version.split(".").map(Number), installed = current.version.split(".").map(Number);
    for (let i = 0; i < 3; i++) if (target[i] !== installed[i]) return target[i]! > installed[i]!;
    return false;
  });
}
export function firmwareJobMessage(view: DeviceFirmware) {
  const job = view.job;
  if (!job) return "";
  const reason: Record<string, string> = {
    busy: "设备正在使用", invalid_command: "升级指令无效", invalid_url: "下载地址未通过校验",
    http_error: "下载服务响应异常", download_failed: "下载未完成", length_mismatch: "固件长度不匹配",
    hash_mismatch: "固件摘要不匹配", image_mismatch: "固件型号或版本不匹配", slot_too_small: "设备空间不足",
    write_failed: "固件写入失败", boot_failed: "新固件启动未通过", health_failed: "设备音频自检未通过",
    timeout: "等待设备确认超时", authority_changed: "设备管理授权已变化", device_restarted: "设备已重新启动",
  };
  switch (job.status) {
    case "queued": return view.mode === "restart"
      ? "升级任务已创建，等待重启核对。请在设备旁关机再开机一次，保持供电与 Wi-Fi。"
      : "升级任务已创建，等待设备接收。";
    case "sent": return "升级指令已下发，等待设备下载回执。";
    case "downloading": return `设备正在下载固件${job.progress === null ? "" : `，${job.progress}%`}。请保持供电与 Wi-Fi。`;
    case "restarting": return "设备正在重启，等待新固件启动和音频自检确认。";
    case "confirmed": return `升级成功：设备已确认版本 ${job.version}，启动和音频自检通过。`;
    case "failed": return `升级失败${reason[job.error] ? `：${reason[job.error]}` : ""}。请重新读取并核对设备后再操作，不会自动重试。`;
    case "unknown": return `升级结果未知${reason[job.error] ? `：${reason[job.error]}` : ""}。请重新读取并核对设备，不会自动重试。`;
  }
}
type Clock = { after: (fn: () => void, milliseconds: number) => unknown; cancel: (timer: unknown) => void; now?: () => number };

// GET polling observes persisted receipts. POST is reachable only through an
// explicit confirmation, and is never retried by the controller.
export function createFirmwareUpgradeController(
  api: Pick<Api, "getDeviceFirmware" | "upgradeDeviceFirmware">,
  state: ReturnType<typeof firmwareUpgradeState>,
  clock: Clock = {after: (fn, ms) => setTimeout(fn, ms), cancel: timer => clearTimeout(timer as ReturnType<typeof setTimeout>)},
) {
  let epoch = 0, operation = 0, id = "", scope: Scope = "family";
  let deadlineTimer: unknown, pollTimer: unknown, abort: AbortController | undefined, interrupt: (() => void) | undefined;
  const now = clock.now ?? Date.now;
  function cancelPoll() { if (pollTimer !== undefined) clock.cancel(pollTimer); pollTimer = undefined; }
  function cancelRequest() {
    if (deadlineTimer !== undefined) clock.cancel(deadlineTimer);
    deadlineTimer = undefined; abort?.abort(); abort = undefined; interrupt?.(); interrupt = undefined;
  }
  function stop() {
    epoch++; operation++; id = ""; cancelPoll(); cancelRequest(); Object.assign(state, firmwareUpgradeState());
  }
  function apply(view: DeviceFirmware) {
    if (view.deviceId !== id) throw new Error("升级任务所属设备未核实");
    state.view = view; state.needsRead = false; state.confirming = null; state.feedback = "";
  }
  function canUpgrade(releaseId: string) {
    const view = state.view;
    return !!id && !state.pending && !state.needsRead && !!view?.canUpgrade && view.connection === "online" &&
      !active(view.job) && firmwareCandidates(view).some(release => release.id === releaseId);
  }
  async function perform(action: (current: () => boolean, signal: AbortSignal) => Promise<void>) {
    const token = epoch, attempt = ++operation, deadline = now() + 30000;
    state.pending = true; abort = new AbortController(); const signal = abort.signal;
    const interrupted = new Promise<void>(resolve => {interrupt = resolve;});
    function unconfirmed(message: string) {
      state.view = null; state.needsRead = true; state.confirming = null;
      state.feedback = message + "；请重新读取并核对设备后再操作，不会自动重试。";
    }
    function expire() {
      if (token !== epoch || attempt !== operation) return;
      operation++; cancelRequest(); state.pending = false; unconfirmed("等待超过30秒，结果未确认");
    }
    const current = () => {
      if (token !== epoch || attempt !== operation) return false;
      if (now() >= deadline) {expire(); return false;}
      return true;
    };
    deadlineTimer = clock.after(expire, 30000);
    try { await Promise.race([action(current, signal), interrupted]); }
    catch (error) { if (current()) unconfirmed(error instanceof Error ? error.message : "升级结果未确认"); }
    finally {
      if (token === epoch && attempt === operation) {
        state.pending = false; cancelRequest();
        if (!state.needsRead && active(state.view?.job ?? null))
          pollTimer = clock.after(() => {pollTimer = undefined; void refresh();}, 3000);
      }
    }
  }
  async function refresh() {
    if (!id || state.pending) return;
    cancelPoll(); state.confirming = null; state.needsRead = true; state.feedback = "正在读取固件升级状态…";
    const selected = id, selectedScope = scope;
    await perform(async (current, signal) => {
      const view = await api.getDeviceFirmware(selected, selectedScope, signal);
      if (current()) apply(view);
    });
  }
  return {
    async start(device: string, selectedScope: Scope) {stop(); id = device; scope = selectedScope; await refresh();},
    stop, refresh, canUpgrade,
    askUpgrade(releaseId: string) {if (canUpgrade(releaseId)) {state.confirming = releaseId; state.feedback = "";}},
    cancelUpgrade() {if (!state.pending) state.confirming = null;},
    async confirmUpgrade() {
      const releaseId = state.confirming;
      if (!releaseId || !canUpgrade(releaseId)) return;
      const view = state.view!, release = view.releases.find(r => r.id === releaseId)!;
      const selected = id, selectedScope = scope;
      state.confirming = null; state.needsRead = true; state.feedback = "正在提交升级任务，等待服务确认，请勿重复操作…";
      await perform(async (current, signal) => {
        const result = await api.upgradeDeviceFirmware(selected, {releaseId, boot: view.boot}, selectedScope, signal);
        if (!current()) return;
        if (result.job?.releaseId !== releaseId || result.job.version !== release.version) throw new Error("升级任务响应未核实");
        apply(result);
      });
    },
  };
}
