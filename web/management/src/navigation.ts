import type { Scope, PageRoute } from './types.ts'
export const adminNav = [ ['overview','总览'], ['devices','设备'], ['conversations','聊天记录'], ['families','家庭'], ['accounts','账号'], ['policies','会话与数据'], ['memories','记忆'], ['daily','每日概览'], ['capabilities','能力'], ['expressions','表情'], ['voiceprints','声纹'], ['tasks','任务'], ['firmware','固件'], ['operations','运维'], ['audit','审计'], ['configuration','配置'] ] as const
export const familyNav = [ ['home','首页'], ['device','设备'], ['conversations','聊天'], ['memories','记忆'], ['settings','我的'] ] as const
export function linkTo(scope: Scope, page: string, query: Record<string,string> = {}): string {
  const params = new URLSearchParams(query)
  return `${scope === 'admin' ? 'index.html' : 'family.html'}#/${page}${params.size ? `?${params}` : ''}`
}
export function readRoute(scope: Scope): PageRoute {
  const [path, query = ''] = location.hash.slice(1).split('?')
  const params = new URLSearchParams(query)
  return { page: path?.replace(/^\//, '') || (scope === 'admin' ? 'overview' : 'home'), device: params.get('device') ?? 'dev-living', chat: params.get('chat') ?? 'chat-run', memory: params.get('memory') ?? 'memory-run', family: params.get('family') ?? 'family-lin' }
}
