import type { MockApi } from "./mock-api.ts";
import { ApiError } from "./mock-api.ts";
import type { Account, Conversation, Device, Family, Scope } from "./types.ts";

export type RecordFilter = {
  deviceId?: string;
  query?: string;
  scope?: Scope;
  offset?: number;
  limit?: number;
};
export type RecordPage = { items: Conversation[]; hasMore: boolean };

export function createRemoteApi(fetcher: typeof fetch = globalThis.fetch) {
  async function request<T>(
    scope: Scope,
    resource: string,
    init: RequestInit = {},
    anonymous = false,
  ): Promise<T> {
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
    let data: unknown;
    try {
      data = await response.json();
    } catch {
      throw new ApiError(
        "invalid_response",
        `管理服务响应异常（HTTP ${response.status}）。`,
      );
    }
    if (!response.ok) {
      if (
        response.status === 401 &&
        !anonymous &&
        typeof window !== "undefined"
      )
        window.dispatchEvent(new Event("management-session-expired"));
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
    return request(filter.scope ?? "admin", `conversations?${params}`);
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
        `conversations/${encodeURIComponent(id)}${query}`,
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
      return request<Account>(
        scope,
        "login",
        {
          method: "POST",
          body: JSON.stringify({ username, password }),
        },
        true,
      );
    },
    async logout(scope: Scope) {
      await request(scope, "logout", { method: "POST", body: "{}" });
    },
    async getAccount(scope: Scope) {
      try {
        return await request<Account>(scope, "session", {}, true);
      } catch (error) {
        if (error instanceof ApiError && error.code === "unauthenticated")
          return null;
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
    listMemories: unsupported,
    getMemory: unsupported,
    clearMemories: unsupported,
    getPrivacy: unsupported,
    updatePrivacy: unsupported,
    getDailySummaries: unsupported,
    listCapabilities: unsupported,
    listExpressions: unsupported,
    listTasks: unsupported,
    listFirmware: unsupported,
    listAudit: unsupported,
    getOperations: unsupported,
  } satisfies Omit<MockApi, "storageMode" | "mode"> & {
    storageMode: "server";
    mode: "live";
  };
}
