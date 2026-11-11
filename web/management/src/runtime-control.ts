import type { Api } from "./api.ts";
import type { DeviceRuntime, Scope, RuntimeField } from './types.ts';

export function runtimeState() {
  return {
    runtime: null as DeviceRuntime | null,
    volume: null as number | null,
    brightness: null as number | null,
    pending: false,
    feedback: '',
  };
}
type State = ReturnType<typeof runtimeState>;
type RuntimeApi = Pick<Api, "getDeviceRuntime" | "controlDevice">;
type Clock = {
  now: () => number;
  every: (fn: () => void) => unknown;
  cancel: (timer: unknown) => void;
};

// Owns the current view and one local operation. The server owns authorization,
// command identity and status; a value alone is never an operation receipt.
export function createRuntimeController(
  api: RuntimeApi,
  state: State,
  clock: Clock = {
    now: Date.now,
    every: fn => setInterval(fn, 2000),
    cancel: id => clearInterval(id as ReturnType<typeof setInterval>),
  },
) {
  let generation = 0, operation = 0, id = '', scope: Scope = 'admin';
  let timer: unknown, reading = false;
  const editing = new Set<RuntimeField>();
  let expected: {
    id: string | null;
    field: RuntimeField;
    value: number;
    boot: string;
    deadline: number;
  } | null = null;

  function finish(text: string) {
    operation++;
    expected = null;
    state.pending = false;
    state.feedback = text;
    editing.clear();
  }
  function checkDeadline() {
    if (expected && clock.now() >= expected.deadline)
      finish('等待超过50秒，结果未知；不会自动重发。');
  }
  function apply(value: DeviceRuntime) {
    if (value.deviceId !== id) throw new Error('设备状态不匹配，未核实操作。');
    state.runtime = value;
    for (const field of ['volume', 'brightness'] as const)
      if (!editing.has(field)) state[field] = value[field];
    checkDeadline();
    if (!expected) return;
    const c = value.command, e = expected;
    if (value.boot !== e.boot || (e.id && c && c.id !== e.id)) {
      finish('设备或命令已变化，原操作结果未知，请刷新。');
      return;
    }
    // The PUT may still be unresolved. Observations cannot establish which
    // command belongs to it until its own response supplies the command ID.
    if (e.id && c?.id === e.id && c.field === e.field && c.value === e.value) {
      if (c.status === 'confirmed' && c.actual === e.value && value[e.field] === e.value) {
        finish('设备已生效。');
        state[e.field] = e.value;
      } else if (['failed', 'expired', 'unknown'].includes(c.status)) {
        finish('设备操作未核实，请检查状态后再操作。');
      }
    }
  }
  async function refresh() {
    // Check independently of HTTP completion: a hung PUT or GET cannot extend
    // the observation window or leave the UI pending forever.
    checkDeadline();
    if (!id || reading) return;
    const token = generation, selected = id;
    reading = true;
    try {
      const value = await api.getDeviceRuntime(selected, scope);
      if (token === generation) apply(value);
    } catch (e) {
      if (token === generation) {
        state.runtime = null;
        state.volume = state.brightness = null;
        state.feedback = e instanceof Error ? e.message : '无法读取设备状态。';
      }
    } finally {
      if (token === generation) reading = false;
    }
  }
  function stop() {
    generation++;
    operation++;
    id = '';
    reading = false;
    if (timer !== undefined) clock.cancel(timer);
    timer = undefined;
    expected = null;
    editing.clear();
    Object.assign(state, runtimeState());
  }
  return {
    async start(device: string, selectedScope: Scope) {
      stop();
      id = device;
      scope = selectedScope;
      const token = generation;
      await refresh();
      if (token === generation && id) timer = clock.every(() => { void refresh(); });
    },
    refresh,
    stop,
    preview(field: RuntimeField, value: number) {
      if (!state.pending) {
        editing.add(field);
        state[field] = value;
      }
    },
    cancelEdit(field: RuntimeField) {
      editing.delete(field);
      state[field] = state.runtime?.[field] ?? null;
    },
    async submit(field: RuntimeField, value: number) {
      const runtime = state.runtime;
      if (state.pending || !id || !runtime?.canControl || runtime.connection !== 'online' ||
          !runtime.boot || runtime[field] === null ||
          ['queued', 'sent'].includes(runtime.command?.status ?? '') ||
          !Number.isInteger(value) || value < 0 || value > 100 || value % 10 !== 0) return;
      if (value === runtime[field]) { editing.delete(field); return; }
      const token = generation, selected = id, boot = runtime.boot, attempt = ++operation;
      expected = { id: null, field, value, boot, deadline: clock.now() + 50000 };
      state.pending = true;
      state.feedback = '正在提交，等待设备回执…';
      try {
        const result = await api.controlDevice(selected, { field, value, boot }, scope);
        checkDeadline();
        if (token !== generation || attempt !== operation || !expected) return;
        const c = result.command;
        if (result.deviceId !== selected || result.boot !== boot || !c?.id ||
            c.field !== field || c.value !== value)
          throw new Error('命令响应不匹配，操作未核实。');
        expected.id = c.id;
        apply(result);
      } catch (e) {
        if (token === generation && attempt === operation)
          finish(e instanceof Error ? e.message : '操作未核实，请刷新状态。');
      }
    },
  };
}

type TimeoutClock = { after: (fn: () => void) => unknown; cancel: (id: unknown) => void };
export function createMemoryClearController(
  api: Pick<Api, "clearMemories">,
  state: { pending: boolean; feedback: string },
  onCleared: (isCurrent: () => boolean) => unknown,
  clock: TimeoutClock = {
    after: fn => setTimeout(fn, 50000),
    cancel: id => clearTimeout(id as ReturnType<typeof setTimeout>),
  },
) {
  let generation = 0, id = '', scope: Scope = 'admin', allowed = false, timer: unknown;
  function cancelTimer() {
    if (timer !== undefined) clock.cancel(timer);
    timer = undefined;
  }
  return {
    canClear() { return allowed; },
    authorize(device: string, selected: Scope, canClear: boolean) {
      generation++;
      cancelTimer();
      id = device;
      scope = selected;
      allowed = canClear;
      state.pending = false;
    },
    invalidate() {
      generation++;
      cancelTimer();
      allowed = false;
      id = '';
      state.pending = false;
      state.feedback = '';
    },
    async clear(ask: () => boolean | Promise<boolean>) {
      if (!allowed || !id || state.pending) return;
      state.pending = true;
      state.feedback = '';
      const token = generation, device = id, selected = scope;
      try {
        const confirmed = await ask();
        if (token !== generation || !confirmed) return;
        let timedOut = false;
        const isCurrent = () => token === generation && !timedOut;
        const timeout = new Promise<never>((_, reject) => {
          timer = clock.after(() => {
            timedOut = true;
            if (token === generation) allowed = false;
            reject(new Error('清除等待超过50秒，结果未知；请重新读取记忆，不会自动重试。'));
          });
        });
        const action = (async () => {
          await api.clearMemories(device, selected);
          if (isCurrent()) await onCleared(isCurrent);
        })();
        await Promise.race([action, timeout]);
        if (isCurrent())
          state.feedback = '近期记忆已清除，聊天记录保留；新对话可产生新记忆。';
      } catch (e) {
        if (token === generation) {
          if (e && typeof e === 'object' && 'code' in e &&
              ['forbidden', 'unauthenticated', 'not_found'].includes(String(e.code))) allowed = false;
          state.feedback = e instanceof Error ? e.message : '清除结果未核实，请刷新。';
        }
      } finally {
        if (token === generation) { cancelTimer(); state.pending = false; }
      }
    },
  };
}
