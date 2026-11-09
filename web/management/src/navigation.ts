import { liveMode } from "./api.ts";
import type { Scope, PageRoute } from "./types.ts";
export const adminNav = [
  ["overview", "总览"],
  ["devices", "设备"],
  ["conversations", "聊天记录"],
  ["families", "家庭"],
  ["accounts", "账号"],
  ["policies", "会话与数据"],
  ["memories", "记忆"],
  ["daily", "每日概览"],
  ["capabilities", "能力"],
  ["expressions", "表情"],
  ["voiceprints", "声纹"],
  ["tasks", "任务"],
  ["firmware", "固件"],
  ["operations", "运维"],
  ["audit", "审计"],
  ["configuration", "配置"],
] as const;
export const familyNav = [
  ["home", "首页"],
  ["device", "设备"],
  ["conversations", "聊天"],
  ["memories", "记忆"],
  ["settings", "我的"],
] as const;
export const adminRoutes = [
  ["overview", "工作台"],
  ["families", "家庭列表"],
  ["family", "家庭详情"],
  ["devices", "设备列表"],
  ["device", "设备详情"],
  ["conversations", "对话中心"],
  ["daily", "每日摘要"],
  ["memories", "记忆管理"],
  ["capabilities", "能力总览"],
  ["expressions", "表情预览"],
  ["voiceprints", "声纹管理"],
  ["tasks", "任务中心"],
  ["configuration", "产品配置"],
  ["policies", "会话与数据策略"],
  ["firmware", "固件版本"],
  ["operations", "运行状态"],
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
  ["invite", "邀请与加入"],
  ["bind", "绑定设备"],
  ["settings", "设备设置"],
  ["privacy", "数据与隐私"],
  ["login", "家庭登录"],
] as const;
export const operationsRoutes = [
  "capabilities",
  "expressions",
  "voiceprints",
  "tasks",
  "firmware",
  "operations",
  "audit",
];
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
    device: params.get("device") ?? (liveMode ? "" : "dev-living"),
    chat: params.get("chat") ?? (liveMode ? "" : "chat-run"),
    memory: params.get("memory") ?? (liveMode ? "" : "memory-run"),
    family: params.get("family") ?? (liveMode ? "" : "family-lin"),
  };
}
