export const ORIGIN = "https://robot.songguoxue.com";
export type Family = { id: string; name: string; canAddDevices: boolean };
export type Task = {
  id: string;
  status: "waiting" | "confirmed" | "expired" | "cancelled";
  expires_at: string;
  serial: string;
  model: string;
  challenge?: string;
  ble_name?: string;
  device_id?: string;
};
export class ClientError extends Error {
  code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}
export class Api {
  private cookie = "";
  get authenticated() { return !!this.cookie; }
  private epoch = 0;
  private platform: any;
  private listeners = new Set<(error: ClientError) => void>();
  private changes = new Set<() => void>();
  onUnauthorized(fn: (error: ClientError) => void) { this.listeners.add(fn); return () => this.listeners.delete(fn); }
  onChange(fn: () => void) { this.changes.add(fn); return () => this.changes.delete(fn); }
  constructor(platform: any, unauthorized?: (error: ClientError) => void) {
    this.platform = platform;
    if (unauthorized) this.listeners.add(unauthorized);
  }
  clear() {
    this.cookie = "";
    this.epoch++;
    for (const fn of this.changes) fn();
  }
  invalidatePending() {
    this.epoch++;
  }
  async request<T>(path: string, method = "GET", data?: unknown): Promise<T> {
    if (
      !(/^\/(session|login|logout|families|onboarding(?:\/[A-Za-z0-9_-]+)?)$/.test(path) || /^\/devices(?:\/dev_[a-f0-9]{32})?$/.test(path) || /^\/device-settings\/dev_[a-f0-9]{32}$/.test(path) || /^\/chats\?device=dev_[a-f0-9]{32}&offset=(?:0|[1-9][0-9]{0,4})&limit=20$/.test(path) || /^\/chats\/chat_[a-f0-9]{64}\?device=dev_[a-f0-9]{32}$/.test(path))
    )
      throw new ClientError("invalid_path", "请求地址无效");
    const epoch = this.epoch;
    return new Promise((resolve, reject) =>
      this.platform.request({
        url: ORIGIN + "/api/family" + path,
        method,
        data,
        timeout: 12000,
        enableCache: false,
        header: {
          "Content-Type": "application/json",
          ...(this.cookie ? { Cookie: this.cookie } : {}),
        },
        success: (r: any) => {
          if (epoch !== this.epoch) {
            reject(new ClientError("cancelled", "操作已取消"));
            return;
          }
          const cookies = Array.isArray(r.cookies) ? r.cookies : [];
          const raw = r.header?.["Set-Cookie"] || r.header?.["set-cookie"];
          if (typeof raw === "string") cookies.push(raw);
          for (const c of cookies) {
            const m = /^management_family=([^;]*)/.exec(c);
            if (m) this.cookie = m[1] ? "management_family=" + m[1] : "";
          }
          if (r.statusCode === 401) {
            this.clear();
            for (const fn of this.listeners) fn(new ClientError("unauthorized", "登录已过期，请重新登录"));
          }
          if (r.statusCode < 200 || r.statusCode >= 300) {
            const message: Record<number, string> = {
              400: "输入无效，请检查后重试",
              401: "登录已过期，请重新登录",
              403: "当前账号没有此操作权限",
              404:
                path === "/onboarding" && method === "POST"
                  ? "设备码尚未登记或已失效，请联系管理员"
                  : path.startsWith("/onboarding/") &&
                      (method === "GET" || method === "DELETE")
                    ? "添加任务不存在，请重新扫码"
                    : "服务入口不存在，请联系管理员",
              409: path.startsWith("/device-settings/") ? "设置已变化，请刷新后再保存" : "设备正在添加或已被绑定",
              410: "认领已过期，请重新扫码",
              429: "操作过于频繁，请稍后重试",
              503: "服务暂不可用，请稍后重试",
            };
            reject(
              new ClientError(
                r.statusCode === 401
                  ? "unauthorized"
                  : r.statusCode === 409 ? "conflict" : String(r.data?.code || "request_failed"),
                path === "/login" &&
                  r.statusCode === 401 &&
                  r.data?.code === "invalid_login"
                  ? "账号或密码不正确"
                  : message[r.statusCode] || "请求失败，请稍后重试",
              ),
            );
            return;
          }
          if (!r.data || typeof r.data !== "object") {
            reject(new ClientError("invalid_response", "服务响应无效"));
            return;
          }
          // A committed login owns a new session even when another request was
          // sent while login was pending. Its late 401/Set-Cookie is now stale.
          if (path === "/login") this.epoch++;
          resolve(r.data as T);
        },
        fail: () =>
          reject(new ClientError("network_error", "网络连接失败，请重试")),
      }),
    );
  }
  login(username: string, password: string) {
    this.clear();
    return this.request("/login", "POST", { username, password });
  }
  families() {
    return this.request<Family[]>("/families");
  }
  start(family_id: string, claim_code: string, name?: string) {
    return this.request<Task>("/onboarding", "POST", {
      family_id,
      claim_code,
      ...(name ? { name } : {}),
    });
  }
  status(id: string) {
    return this.request<Task>("/onboarding/" + encodeURIComponent(id));
  }
  cancel(id: string) {
    return this.request<Task>(
      "/onboarding/" + encodeURIComponent(id),
      "DELETE",
    );
  }
}
export function claimCode(raw: unknown): string {
  if (typeof raw !== "string" || raw.length > 102)
    throw new ClientError("bad_scan", "请扫描本产品的小程序设备码");
  let value = raw;
  if (value.startsWith("scene=")) {
    try {
      value = decodeURIComponent(value.slice(6));
    } catch {
      throw new ClientError("bad_scan", "二维码格式无效");
    }
  }
  if (!/^[a-f0-9]{32}$/.test(value))
    throw new ClientError("bad_scan", "请扫描本产品的小程序设备码");
  return value;
}

// Only this application's issued mini-code path is an accepted scene carrier.
export function scannedClaim(result: {
  path?: unknown;
  result?: unknown;
}): string {
  if (result.path !== undefined && result.path !== "") {
    if (typeof result.path !== "string" || result.path.length > 128)
      throw new ClientError("bad_scan", "请扫描本产品的小程序设备码");
    const match = /^\/?pages\/index\/index\?scene=([^?&#]{1,96})$/.exec(
      result.path,
    );
    if (!match) throw new ClientError("bad_scan", "请扫描本产品的小程序设备码");
    return sceneClaim(match[1]);
  }
  return claimCode(result.result);
}
function sceneClaim(raw: unknown): string {
  if (typeof raw !== "string" || raw.length > 96)
    throw new ClientError("bad_scan", "二维码格式无效");
  let decoded: string;
  try {
    decoded = decodeURIComponent(raw);
  } catch {
    throw new ClientError("bad_scan", "二维码格式无效");
  }
  if (!/^[a-f0-9]{32}$/.test(decoded))
    throw new ClientError("bad_scan", "请扫描本产品的小程序设备码");
  return decoded;
}
export function launchClaim(options: any): string {
  if (
    typeof options?.path !== "string" ||
    !/^\/?pages\/index\/index$/.test(options.path) ||
    !options.query ||
    typeof options.query !== "object" ||
    Array.isArray(options.query) ||
    Object.keys(options.query).length !== 1 ||
    !Object.prototype.hasOwnProperty.call(options.query, "scene")
  )
    throw new ClientError("bad_scan", "请扫描本产品的小程序设备码");
  return sceneClaim(options.query.scene);
}
