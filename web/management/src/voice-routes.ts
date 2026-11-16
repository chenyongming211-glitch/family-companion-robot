import type { Api } from "./api.ts";
import type { DeviceVoiceRoute, VoiceRoute } from "./types.ts";

export function voiceRoutesState() {
  return {view: null as DeviceVoiceRoute | null, pending: false, needsRead: true, feedback: ""};
}
type Clock = {after: (fn: () => void, ms: number) => unknown; cancel: (timer: unknown) => void; now?: () => number};

// Owns this panel's request lifetime and RAM state. The API validates wire
// contracts; management owns persisted selection and voice owns actual sessions.
export function createVoiceRoutesController(
  api: Pick<Api, "getDeviceVoiceRoute" | "setDeviceVoiceRoute">,
  state: ReturnType<typeof voiceRoutesState>,
  clock: Clock = {after: (fn, ms) => setTimeout(fn, ms), cancel: timer => clearTimeout(timer as ReturnType<typeof setTimeout>)},
) {
  let epoch = 0, operation = 0, id = "";
  let timer: unknown, abort: AbortController | undefined, interrupt: (() => void) | undefined;
  const now = clock.now ?? Date.now;
  function cancel() {
    if (timer !== undefined) clock.cancel(timer);
    timer = undefined; abort?.abort(); abort = undefined; interrupt?.(); interrupt = undefined;
  }
  function stop() {
    epoch++; operation++; id = ""; cancel(); Object.assign(state, voiceRoutesState());
  }
  function apply(view: DeviceVoiceRoute) {
    if (view.deviceId !== id) throw new Error("语音链路所属设备未核实");
    state.view = view; state.needsRead = false; state.feedback = "";
  }
  async function perform(action: (current: () => boolean, signal: AbortSignal) => Promise<void>) {
    const token = epoch, attempt = ++operation, deadline = now() + 30000;
    state.pending = true; abort = new AbortController(); const signal = abort.signal;
    const interrupted = new Promise<void>(resolve => {interrupt = resolve;});
    function unconfirmed(message: string) {
      state.view = null; state.needsRead = true;
      state.feedback = message + "；请重新读取后再操作，不会自动重试。";
    }
    function expire() {
      if (token !== epoch || attempt !== operation) return;
      operation++; cancel(); state.pending = false; unconfirmed("等待超过30秒，结果未确认");
    }
    const current = () => {
      if (token !== epoch || attempt !== operation) return false;
      if (now() >= deadline) {expire(); return false;}
      return true;
    };
    timer = clock.after(expire, 30000);
    try {await Promise.race([action(current, signal), interrupted]);}
    catch (error) {if (current()) unconfirmed(error instanceof Error ? error.message : "语音链路结果未确认");}
    finally {if (token === epoch && attempt === operation) {state.pending = false; cancel();}}
  }
  async function refresh() {
    if (!id || state.pending) return;
    const selected = id;
    state.view = null; state.needsRead = true; state.feedback = "正在读取语音链路…";
    await perform(async (current, signal) => {
      const view = await api.getDeviceVoiceRoute(selected, signal);
      if (current()) apply(view);
    });
  }
  function canSwitch() {return !!id && !state.pending && !state.needsRead && !!state.view?.canSwitch;}
  return {
    async start(device: string) {stop(); id = device; await refresh();},
    stop, refresh, canSwitch,
    async select(route: VoiceRoute) {
      if (!canSwitch() || !["A", "B", "C"].includes(route)) return;
      const selected = id, view = state.view!;
      state.needsRead = true; state.feedback = "正在保存并结束当前对话，随后重新读取确认…";
      await perform(async (current, signal) => {
        const result = await api.setDeviceVoiceRoute(selected, route, view.version, signal);
        if (!current()) return;
        if (result.deviceId !== selected || result.selectedRoute !== route || !result.canSwitch)
          throw new Error("保存响应未核实");
        const confirmed = await api.getDeviceVoiceRoute(selected, signal);
        if (!current()) return;
        if (confirmed.deviceId !== selected || confirmed.selectedRoute !== route ||
            confirmed.version !== result.version || !confirmed.canSwitch)
          throw new Error("保存后的读取结果未核实");
        apply(confirmed);
        const ended = confirmed.connection === "idle" || confirmed.activeRoute === route;
        state.feedback = result.disconnectConfirmed && result.connection !== "unknown" && confirmed.connection !== "unknown" && ended
          ? "已切换，请重新唤醒或按键开始新对话。"
          : "选择已保存，旧对话结束尚未确认。请刷新状态，确认后再唤醒或按键。";
      });
    },
  };
}
