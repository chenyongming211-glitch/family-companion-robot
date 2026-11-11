import { invalidateSensitive, sessionGeneration } from "./sensitive-session.ts";
import type { MockApi } from "./mock-api.ts";
import { ApiError } from "./mock-api.ts";
import type {
  Account,
  AuditFilter,
  AuditPage,
  DeviceRuntime,
  ControlInput,
  Conversation,
  Device,
  Family,
  Memory,
  Scope,
} from "./types.ts";

export type RecordFilter = {
  deviceId?: string;
  query?: string;
  scope?: Scope;
  offset?: number;
  limit?: number;
};
export type RecordPage = {
  items: Conversation[];
  hasMore: boolean;
  pendingSources?: boolean;
};


function checkedRuntime(id: string, value: DeviceRuntime): DeviceRuntime {
  const percent = (number: unknown) => typeof number === "number" &&
    Number.isInteger(number) && number >= 0 && number <= 100;
  const optionalPercent = (number: unknown) => number === null || percent(number);
  const c = value?.command;
  if (!value || value.deviceId !== id ||
      !["unknown", "offline", "online"].includes(value.connection) ||
      typeof value.seenAt !== "string" || typeof value.boot !== "string" ||
      (value.connection === "online" && (!value.boot || !value.seenAt || Number.isNaN(Date.parse(value.seenAt)))) ||
      typeof value.canControl !== "boolean" ||
      !optionalPercent(value.volume) || !optionalPercent(value.brightness) ||
      !(c === null || (c && typeof c.id === "string" && c.id &&
        ["volume", "brightness"].includes(c.field) && percent(c.value) &&
        ["queued", "sent", "confirmed", "failed", "expired", "unknown"].includes(c.status) &&
        optionalPercent(c.actual))))
    throw new ApiError("invalid_runtime", "设备状态响应未核实，请刷新后再操作。");
  return value;
}

export function createRemoteApi(fetcher: typeof fetch = globalThis.fetch) {
  const accounts = new Map<Scope, string>();
  async function request<T>(
    scope: Scope,
    resource: string,
    init: RequestInit = {},
    anonymous = false,
  ): Promise<T> {
    const generation = sessionGeneration();
    let response: Response;
    try {
      response = await fetcher(`/api/${scope}/${resource}`, {
        ...init,
        credentials: "same-origin",
        cache: "no-store",
        headers: {
          Accept: "application/json",
          ...(init.body ? { "Content-Type": "application/json" } : {}),
        },
      });
    } catch {
      throw new ApiError("network_error", "管理服务连接失败，请重试。");
    }
    if (generation !== sessionGeneration()) throw new ApiError("stale_response", "页面已离开或账号已变化，请重新读取。");
    if (response.status === 401 && !anonymous) {
      invalidateSensitive();
      if (typeof window !== "undefined") window.dispatchEvent(new Event("management-session-expired"));
    }
    let data: unknown;
    try {
      data = await response.json();
    } catch {
      throw new ApiError(
        "invalid_response",
        `管理服务响应异常（HTTP ${response.status}）。`,
      );
    }
    if (response.ok && generation !== sessionGeneration()) throw new ApiError("stale_response", "页面已离开或账号已变化，请重新读取。");
    if (!response.ok) {
      const error = data as { code?: string; message?: string };
      throw new ApiError(
        typeof error?.code === "string" ? error.code : "request_failed",
        typeof error?.message === "string"
          ? error.message
          : `管理请求失败（HTTP ${response.status}）。`,
      );
    }
    return data as T;
  }
  async function page(filter: RecordFilter = {}): Promise<RecordPage> {
    const params = new URLSearchParams({
      offset: String(filter.offset ?? 0),
      limit: String(filter.limit ?? 50),
    });
    if (filter.deviceId !== undefined) params.set("device", filter.deviceId);
    return request(filter.scope ?? "admin", `chats?${params}`);
  }
  const unsupported = async (..._args: unknown[]): Promise<never> => {
    throw new ApiError(
      "unsupported",
      "此操作尚未接入真实后端，当前仅支持只读查询。",
    );
  };
  return {
    mode: "live" as const,
    storageMode: "server" as const,
    storageWarning: "",
    async listDevices(scope: Scope = "admin") {
      return request<Device[]>(scope, "devices");
    },
    async getDevice(id: string, scope: Scope = "admin") {
      return request<Device>(scope, `devices/${encodeURIComponent(id)}`);
    },
    async listConversations(filter: RecordFilter = {}) {
      return (await page(filter)).items;
    },
    listConversationPage: page,
    async getConversation(
      id: string,
      scope: Scope = "admin",
      expectedDeviceId?: string,
    ) {
      const query =
        expectedDeviceId === undefined
          ? ""
          : `?${new URLSearchParams({ device: expectedDeviceId })}`;
      return request<Conversation>(
        scope,
        `${/^chat_[a-f0-9]{64}$/.test(id) ? "chats" : "conversations"}/${encodeURIComponent(id)}${query}`,
      );
    },
    async listFamilies(scope: Scope = "admin") {
      return request<Family[]>(scope, "families");
    },
    async getFamily(id: string, scope: Scope = "admin") {
      return request<Family>(scope, `families/${encodeURIComponent(id)}`);
    },
    async listAccounts() {
      return request<
        Array<{ name: string; email: string; role: string; access: string }>
      >("admin", "accounts");
    },
    async login(scope: Scope, username: string, password: string) {
      const account = await request<Account>(
        scope,
        "login",
        {
          method: "POST",
          body: JSON.stringify({ username, password }),
        },
        true,
      );
      invalidateSensitive();
      accounts.set(scope, JSON.stringify(account));
      return account;
    },
    async logout(scope: Scope) {
      invalidateSensitive();
      accounts.delete(scope);
      await request(scope, "logout", { method: "POST", body: "{}" });
    },
    async getAccount(scope: Scope) {
      try {
        const account = await request<Account>(scope, "session", {}, true);
        const identity = JSON.stringify(account);
        if (accounts.has(scope) && accounts.get(scope) !== identity) invalidateSensitive();
        accounts.set(scope, identity);
        return account;
      } catch (error) {
        if (error instanceof ApiError && error.code === "unauthenticated") {
          const hadAccount=accounts.has(scope);
          invalidateSensitive(); accounts.delete(scope);
          if(hadAccount && typeof window!=="undefined")window.dispatchEvent(new Event("management-session-expired"));
          return null;
        }
        throw error;
      }
    },
    updateDevice: unsupported,
    deleteConversation: unsupported,
    reset: unsupported,
    inviteMember: unsupported,
    joinFamily: unsupported,
    bindDevice: unsupported,
    updateMemberAccess: unsupported,
    async listMemories(filter: { deviceId?: string; scope?: Scope } = {}) {
      const params = new URLSearchParams();
      if (filter.deviceId !== undefined) params.set("device", filter.deviceId);
      return request<Memory[]>(filter.scope ?? "admin", `memories?${params}`);
    },
    async getMemory(
      id: string,
      scope: Scope = "admin",
      expectedDeviceId?: string,
    ) {
      const params = new URLSearchParams();
      if (expectedDeviceId !== undefined)
        params.set("device", expectedDeviceId);
      return request<Memory>(
        scope,
        `memories/${encodeURIComponent(id)}?${params}`,
      );
    },
    async getDeviceRuntime(id: string, scope: Scope = "admin") {
      return checkedRuntime(id, await request<DeviceRuntime>(scope, `device-runtime/${encodeURIComponent(id)}`));
    },
    async controlDevice(id: string, value: ControlInput, scope: Scope = "admin") {
      return checkedRuntime(id, await request<DeviceRuntime>(scope, `device-runtime/${encodeURIComponent(id)}`, {method:"PUT",body:JSON.stringify({field:value.field,value:value.value,boot:value.boot})}));
    },
    async clearMemories(id: string, scope: Scope = "admin") {
      const receipt = await request<{deviceId:string;status:string}>(scope, `memory-clear/${encodeURIComponent(id)}`, {method:"POST",body:JSON.stringify({confirm:true})});
      if (receipt?.deviceId !== id || receipt.status !== "confirmed") throw new ApiError("memory_clear_unconfirmed", "清除结果未核实，请刷新近期记忆。");
    },
    getPrivacy: unsupported,
    updatePrivacy: unsupported,
    getDailySummaries: unsupported,
    listCapabilities: unsupported,
    listExpressions: unsupported,
    listTasks: unsupported,
    listFirmware: unsupported,
    async listAuditPage(filter: AuditFilter = {}) {
      const params = new URLSearchParams({
        offset: String(filter.offset ?? 0),
        limit: String(filter.limit ?? 50),
      });
      for (const key of ["device", "from", "to", "action"] as const) {
        if (filter[key]) params.set(key, filter[key]);
      }
      return request<AuditPage>("admin", `audit?${params}`);
    },
    listAudit: unsupported,
    getOperations: unsupported,
  } satisfies Omit<MockApi, "storageMode" | "mode"> & {
    storageMode: "server";
    mode: "live";
    listAuditPage(filter?: AuditFilter): Promise<AuditPage>;
  };
}
