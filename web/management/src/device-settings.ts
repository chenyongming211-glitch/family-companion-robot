import type { Api } from "./api.ts";
import type { DeviceSettings, Scope } from "./types.ts";

export function deviceSettingsState() {
  return {
    settings: null as DeviceSettings | null,
    name: "", location: "", pending: false, needsRead: true, feedback: "",
  };
}
type Clock = {
  after: (fn: () => void, milliseconds: number) => unknown;
  cancel: (timer: unknown) => void;
  now?: () => number;
};

// Owns only this form's RAM state and request lifetime. The API validates wire
// contracts; the server owns authorization, normalization and persistence.
export function createDeviceSettingsController(
  api: Pick<Api, "getDeviceSettings" | "updateDeviceSettings">,
  state: ReturnType<typeof deviceSettingsState>,
  onSaved: (value: DeviceSettings) => void = () => {},
  clock: Clock = {
    after: (fn, ms) => setTimeout(fn, ms),
    cancel: timer => clearTimeout(timer as ReturnType<typeof setTimeout>),
  },
  onRead: (value: DeviceSettings) => void = () => {},
) {
  let epoch = 0, operation = 0, id = "", scope: Scope = "admin";
  let timer: unknown, interrupt: (() => void) | undefined;
  const now = clock.now ?? Date.now;
  function cancel() {
    if (timer !== undefined) clock.cancel(timer);
    timer = undefined;
    interrupt?.();
    interrupt = undefined;
  }
  function stop() {
    epoch++;
    operation++;
    id = "";
    cancel();
    Object.assign(state, deviceSettingsState());
  }
  function apply(value: DeviceSettings) {
    if (value.deviceId !== id) throw new Error("设备设置不匹配，请重新读取。");
    state.settings = value;
    state.name = value.name;
    state.location = value.location;
    state.needsRead = false;
  }
  async function perform(action: (current: () => boolean) => Promise<void>) {
    const token = epoch, attempt = ++operation, deadline = now() + 30000;
    state.pending = true;
    const interrupted = new Promise<void>(resolve => { interrupt = resolve; });
    const expire = () => {
      if (token !== epoch || attempt !== operation) return;
      operation++;
      state.pending = false;
      state.needsRead = true;
      state.settings = null;
      state.feedback = "等待超过30秒，结果未确认；请重新读取，不会自动重试。";
      cancel();
    };
    const current = () => {
      if (token !== epoch || attempt !== operation) return false;
      if (now() >= deadline) { expire(); return false; }
      return true;
    };
    timer = clock.after(expire, 30000);
    try {
      await Promise.race([action(current), interrupted]);
    } catch (error) {
      if (current()) {
        state.settings = null;
        state.needsRead = true;
        state.feedback = (error instanceof Error ? error.message + "；" : "结果未确认；") + "请重新读取后再保存，不会自动重试。";
      }
    } finally {
      if (token === epoch && attempt === operation) {
        state.pending = false;
        cancel();
      }
    }
  }
  async function refresh() {
    if (!id || state.pending) return;
    const selected = id, selectedScope = scope;
    state.settings = null;
    state.name = state.location = state.feedback = "";
    state.needsRead = true;
    await perform(async current => {
      const value = await api.getDeviceSettings(selected, selectedScope);
      if (current()) {
        apply(value);
        onRead(value);
      }
    });
  }
  return {
    async start(device: string, selectedScope: Scope) {
      stop(); id = device; scope = selectedScope; await refresh();
    },
    stop,
    refresh,
    async submit() {
      const settings = state.settings;
      if (!id || state.pending || state.needsRead || !settings?.canEdit) return;
      const name = state.name.trim(), location = state.location.trim();
      if (Array.from(name).length < 1 || Array.from(name).length > 12 || Array.from(location).length > 24) {
        state.feedback = "昵称需1至12个字，放置位置最多24个字。"; return;
      }
      const selected = id, selectedScope = scope;
      state.needsRead = true;
      state.feedback = "正在保存并重新读取确认…";
      await perform(async current => {
        const result = await api.updateDeviceSettings(selected, { name, location, version: settings.version }, selectedScope);
        if (!current()) return;
        if (result.deviceId !== selected || result.name !== name || result.location !== location || !result.canEdit)
          throw new Error("保存响应未核实");
        const confirmed = await api.getDeviceSettings(selected, selectedScope);
        if (!current()) return;
        if (confirmed.deviceId !== selected || confirmed.name !== result.name ||
            confirmed.location !== result.location || confirmed.version !== result.version || !confirmed.canEdit)
          throw new Error("保存后的读取结果未核实");
        apply(confirmed);
        state.feedback = "设备昵称和放置位置已保存。";
        onSaved(confirmed);
      });
    },
  };
}
