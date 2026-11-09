export const ORIGIN = "https://robot.songguoxue.com";
export type Family = { id: string; name: string; canAddDevices: boolean };
export type Task = {
  id: string;
  status: "waiting" | "confirmed" | "expired" | "cancelled";
  expires_at: string;
  serial: string;
  model: string;
  challenge?: string;
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
  private epoch = 0;
  private platform: any;
  private unauthorized?: (error: ClientError) => void;
  constructor(platform: any, unauthorized?: (error: ClientError) => void) {
    this.platform = platform;
    this.unauthorized = unauthorized;
  }
  clear() {
    this.cookie = "";
    this.epoch++;
  }
  invalidatePending() {
    this.epoch++;
  }
  async request<T>(path: string, method = "GET", data?: unknown): Promise<T> {
    if (
      !/^\/(session|login|logout|families|onboarding(?:\/[A-Za-z0-9_-]+)?)$/.test(
        path,
      )
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
            this.unauthorized?.(
              new ClientError("unauthorized", "登录已过期，请重新登录"),
            );
          }
          if (r.statusCode < 200 || r.statusCode >= 300) {
            const message: Record<number, string> = {
              400: "输入无效，请检查后重试",
              401: "登录已过期，请重新登录",
              403: "当前家庭没有添加设备权限",
              404: "添加任务不存在，请重新扫码",
              409: "设备正在添加或已被绑定",
              410: "认领已过期，请重新扫码",
              429: "操作过于频繁，请稍后重试",
              503: "服务暂不可用，请稍后重试",
            };
            reject(
              new ClientError(
                r.statusCode === 401
                  ? "unauthorized"
                  : String(r.data?.code || "request_failed"),
                message[r.statusCode] || "请求失败，请稍后重试",
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
export function claimCode(raw: string): string {
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
