import type { Api } from "./api.ts";
import type { AuditAction, AuditFilter, AuditItem, Device } from "./types.ts";

export type AuditDraft = { device: string; from: string; to: string; action: AuditAction | "" };
export function auditFilter(draft: AuditDraft, offset = 0, limit = 50): AuditFilter {
  function day(value: string, next = false) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) throw new Error("请选择有效日期。");
    const [year, month, date] = value.split("-").map(Number) as [number, number, number];
    const local = new Date(year, month - 1, date);
    if (local.getFullYear() !== year || local.getMonth() !== month - 1 || local.getDate() !== date)
      throw new Error("请选择有效日期。");
    if (next) local.setDate(local.getDate() + 1);
    return local.toISOString();
  }
  const from = draft.from ? day(draft.from) : undefined;
  const to = draft.to ? day(draft.to, true) : undefined;
  if (from && to && from >= to) throw new Error("开始日期不能晚于结束日期。");
  return { ...(draft.device ? { device: draft.device } : {}),
    ...(from ? { from } : {}), ...(to ? { to } : {}),
    ...(draft.action ? { action: draft.action } : {}), offset, limit };
}
export function auditState() {
  return { draft: { device: "", from: "", to: "", action: "" } as AuditDraft,
    items: [] as AuditItem[], devices: [] as Device[], offset: 0, limit: 50,
    hasMore: false, pending: false, loading: false, error: "", deviceError: "" };
}
type AuditState = ReturnType<typeof auditState>;
type AuditApi = Pick<Api, "listAuditPage" | "listDevices">;
export function createAuditController(api: AuditApi, state: AuditState) {
  let epoch = 0, active = false;
  function clear() {
    epoch++;
    state.items = []; state.offset = 0; state.hasMore = false;
    state.pending = false; state.loading = false; state.error = "";
  }
  async function load(offset: number) {
    if (!active || state.loading) return;
    const token = ++epoch;
    state.loading = true; state.error = "";
    state.items = []; state.hasMore = false; state.pending = false;
    try {
      const filter = auditFilter(state.draft, offset, state.limit);
      const page = await api.listAuditPage(filter);
      if (active && token === epoch) {
        state.items = page.items; state.hasMore = page.hasMore;
        state.pending = page.pending; state.offset = offset;
      }
    } catch (error) {
      if (active && token === epoch)
        state.error = error instanceof Error ? error.message : "审计读取失败，请重试。";
    } finally {
      if (active && token === epoch) state.loading = false;
    }
  }
  // Device lookup belongs to the same visible session, independent of query epochs.
  let deviceEpoch = 0;
  async function activate() {
    active = true;
    const token = ++deviceEpoch;
    state.deviceError = "";
    const devices = api.listDevices("admin").then(value => {
      if (active && token === deviceEpoch) state.devices = value;
    }).catch(() => {
      if (active && token === deviceEpoch) state.deviceError = "设备列表暂不可用，可继续查询全部授权记录。";
    });
    await Promise.all([devices, load(0)]);
  }
  function invalidate() {
    active = false; deviceEpoch++;
    clear(); state.devices = []; state.deviceError = "";
  }
  return { activate, invalidate,
    filtersChanged: clear,
    refresh: () => load(0),
    next: () => state.hasMore && state.offset + state.limit <= 10000 ? load(state.offset + state.limit) : Promise.resolve(),
    previous: () => state.offset > 0 ? load(Math.max(0, state.offset - state.limit)) : Promise.resolve(),
  };
}
