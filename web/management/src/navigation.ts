import type { Scope, PageRoute } from "./types.ts";
export const adminNav = [
  ["overview", "总览"],
  ["devices", "设备"],
  ["conversations", "聊天记录"],
  ["families", "家庭"],
  ["accounts", "账号"],
  ["memories", "记忆"],
  ["audit", "审计"],
  ["configuration", "配置"],
] as const;
export const familyNav = [
  ["home", "首页"],
  ["device", "设备"],
  ["conversations", "聊天"],
  ["memories", "记忆"],
  ["settings", "设置"],
] as const;
export const adminRoutes = [
  ["overview", "工作台"],
  ["families", "家庭列表"],
  ["family", "家庭详情"],
  ["devices", "设备列表"],
  ["device", "设备详情"],
  ["conversations", "对话中心"],
  ["memories", "记忆管理"],
  ["configuration", "产品配置"],
  ["audit", "操作审计"],
  ["accounts", "账号与角色"],
  ["login", "内部登录"],
] as const;
export const familyRoutes = [
  ["home", "家庭首页"],
  ["device", "设备详情"],
  ["conversations", "聊天列表"],
  ["conversation", "聊天详情"],
  ["memories", "记忆列表"],
  ["memory", "记忆详情"],
  ["family", "我的家庭"],
  ["settings", "设备设置"],
  ["login", "家庭登录"],
] as const;
export function linkTo(
  scope: Scope,
  page: string,
  query: Record<string, string> = {},
): string {
  const params = new URLSearchParams(query);
  return `${scope === "admin" ? "index.html" : "family.html"}#/${page}${params.size ? `?${params}` : ""}`;
}
export function readRoute(scope: Scope): PageRoute {
  const [path, query = ""] = location.hash.slice(1).split("?");
  const params = new URLSearchParams(query);
  return {
    page: path?.replace(/^\//, "") || (scope === "admin" ? "overview" : "home"),
    device: params.get("device") ?? "",
    chat: params.get("chat") ?? "",
    memory: params.get("memory") ?? "",
    family: params.get("family") ?? "",
  };
}
