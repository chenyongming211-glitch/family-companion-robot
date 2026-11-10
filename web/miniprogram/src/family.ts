import { Api, ClientError } from "./api.ts";
export function deviceID(value: unknown): string {
  if (typeof value !== "string" || !/^dev_[a-f0-9]{32}$/.test(value))
    throw new ClientError("invalid_route", "设备地址无效");
  return value;
}
export function chatID(value: unknown): string {
  if (typeof value !== "string" || !/^chat_[a-f0-9]{64}$/.test(value))
    throw new ClientError("invalid_route", "聊天地址无效");
  return value;
}
export function familySession(platform: any, app: any = getApp()): Api {
  if (!app.familyApi) app.familyApi = new Api(platform);
  return app.familyApi;
}
