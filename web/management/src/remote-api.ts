import { invalidateSensitive, sessionGeneration } from "./sensitive-session.ts";
import { ApiError } from "./api-error.ts";
import type {
  Account,
  LibraryTrack,
  MusicLibrary,
  MusicUpload,
  AuditFilter,
  AuditPage,
  DeviceRuntime,
  DeviceSettings,
  DeviceSettingsInput,
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

function checkedDeviceSettings(id: string, value: DeviceSettings): DeviceSettings {
  if (!value || value.deviceId !== id || typeof value.name !== "string" ||
      typeof value.location !== "string" || typeof value.version !== "string" ||
      !/^[a-f0-9]{64}$/.test(value.version) || typeof value.canEdit !== "boolean")
    throw new ApiError("invalid_settings", "设备设置响应未核实，请重新读取。");
  return value;
}

function privateTrackID(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 2 ** 52 && value < 2 ** 53;
}
function checkedMusicTrack(value: LibraryTrack): LibraryTrack {
  const integer = (n: unknown, min: number, max: number) => typeof n === "number" && Number.isSafeInteger(n) && n >= min && n <= max;
  const text = (t: unknown, max: number) => typeof t === "string" && t.trim().length > 0 && Array.from(t).length <= max && !/[\p{Cc}]/u.test(t);
  if (!value || !privateTrackID(value.id) || !text(value.title, 500) || !text(value.artist, 2000) ||
      !integer(value.durationMs, 1000, 1200000) || !integer(value.bytes, 1, 30 * 1024 * 1024) ||
      !["mp3", "flac"].includes(value.format) || !integer(value.bitrate, 0, Number.MAX_SAFE_INTEGER) ||
      !integer(value.sampleRate, 8000, 192000) || !integer(value.channels, 1, 2) ||
      typeof value.addedAt !== "string" || !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(value.addedAt) || Number.isNaN(Date.parse(value.addedAt)))
    throw new ApiError("invalid_music_library", "曲库响应未核实，请重新读取。");
  return value;
}
function checkedMusicLibrary(id: string, value: MusicLibrary): MusicLibrary {
  if (!value || value.deviceId !== id || typeof value.canManage !== "boolean" ||
      value.maxBytes !== 300 * 1024 * 1024 || value.maxTrackBytes !== 30 * 1024 * 1024 ||
      !Number.isSafeInteger(value.usedBytes) || value.usedBytes < 0 || value.usedBytes > value.maxBytes ||
      !Array.isArray(value.items) || value.items.length > 100)
    throw new ApiError("invalid_music_library", "曲库响应未核实，请重新读取。");
  value.items.forEach(checkedMusicTrack);
  if (new Set(value.items.map(t => t.id)).size !== value.items.length || value.items.reduce((n, t) => n + t.bytes, 0) !== value.usedBytes)
    throw new ApiError("invalid_music_library", "曲库响应未核实，请重新读取。");
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
          ...(init.body && !(init.body instanceof FormData) ? { "Content-Type": "application/json" } : {}),
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
        response.status,
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
        response.status,
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
  return {
    async getMusicLibrary(id: string, scope: Scope, signal?: AbortSignal) {
      return checkedMusicLibrary(id, await request<MusicLibrary>(scope, `music-library/${encodeURIComponent(id)}`, {signal}));
    },
    async uploadMusicTrack(id: string, value: MusicUpload, scope: Scope, signal?: AbortSignal) {
      const body = new FormData();
      body.append("title", value.title);
      body.append("artist", value.artist);
      body.append("personalUse", String(value.personalUse));
      body.append("file", value.file);
      return checkedMusicTrack(await request<LibraryTrack>(scope, `music-library/${encodeURIComponent(id)}`, {method: "POST", body, signal}));
    },
    async deleteMusicTrack(id: string, trackId: number, scope: Scope, signal?: AbortSignal) {
      if (!privateTrackID(trackId)) throw new ApiError("invalid_music_library", "曲目编号未核实，请重新读取。");
      const receipt = await request<{deleted: number}>(scope, `music-library/${encodeURIComponent(id)}?id=${trackId}`, {method: "DELETE", signal});
      if (!receipt || receipt.deleted !== trackId) throw new ApiError("invalid_music_library", "删除响应未核实，请重新读取。");
      return receipt;
    },
    async listDevices(scope: Scope = "admin") {
      return request<Device[]>(scope, "devices");
    },
    async getDevice(id: string, scope: Scope = "admin") {
      return request<Device>(scope, `devices/${encodeURIComponent(id)}`);
    },
    async getDeviceSettings(id: string, scope: Scope) {
      return checkedDeviceSettings(id, await request<DeviceSettings>(scope, `device-settings/${encodeURIComponent(id)}`));
    },
    async updateDeviceSettings(id: string, value: DeviceSettingsInput, scope: Scope) {
      return checkedDeviceSettings(id, await request<DeviceSettings>(scope, `device-settings/${encodeURIComponent(id)}`, {
        method: "PUT", body: JSON.stringify({ name: value.name, location: value.location, version: value.version }),
      }));
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
          accounts.delete(scope);
          if(hadAccount) {
            invalidateSensitive();
            if(typeof window!=="undefined")window.dispatchEvent(new Event("management-session-expired"));
          }
          return null;
        }
        throw error;
      }
    },
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
  };
}
