import type { Scope, Device, Snapshot } from './types.ts'
import { initialData } from './seed.ts'
export type DemoStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

const storageKey = 'companion-management-demo-v1'
const clone = <T>(value: T): T => structuredClone(value)
export class ApiError extends Error {
  code: string
  constructor(code: string, message: string) { super(message); this.code = code }
}
function fail(code: string, message: string): never { throw new ApiError(code, message) }
function validName(name: string): string {
  if (typeof name !== 'string' || /[\p{Cc}\p{Cf}]/u.test(name)) fail('invalid_name', '名称不能包含控制字符')
  const trimmed = name.trim()
  if (!trimmed || [...trimmed].length > 12) fail('invalid_name', '名称需为1至12个字符')
  return trimmed
}
function validPercent(value: number): boolean { return Number.isInteger(value) && value >= 0 && value <= 100 }
function validSnapshot(data: unknown): data is Snapshot {
  if (!data || typeof data !== 'object') return false
  const s = data as Snapshot
  if (![s.devices, s.conversations, s.memories, s.families, s.audit].every(Array.isArray) || !s.privacy || !s.accounts) return false
  try {
    for (const d of s.devices) {
      if (!d.id || typeof d.id !== 'string' || !d.familyId || !s.families.some(f => f.id === d.familyId) || typeof d.name !== 'string' || validName(d.name) !== d.name || !validPercent(d.volume) || !validPercent(d.brightness)) return false
    }
    for (const c of s.conversations) {
      if (!s.devices.some(d => d.id === c.deviceId && d.familyId === c.familyId) || typeof c.title !== 'string' || typeof c.bodyAvailable !== 'boolean' || !Array.isArray(c.turns)) return false
      for (const t of c.turns) if (!t || typeof t !== 'object' || typeof t.id !== 'string' || typeof t.user !== 'string' || typeof t.played !== 'string' || typeof t.generated !== 'string' || !['played', 'interrupted', 'unknown'].includes(t.playback) || typeof t.saved !== 'boolean') return false
    }
    for (const m of s.memories) if (!s.conversations.some(c => c.id === m.conversationId && c.deviceId === m.deviceId) || typeof m.text !== 'string') return false
    for (const f of s.families) if (typeof f.name !== 'string' || !Array.isArray(f.members)) return false
    for (const d of s.devices) {
      const p = s.privacy[d.id]
      if (!p || typeof p.keepRecords !== 'boolean' || typeof p.useMemory !== 'boolean' || !['7','30','90'].includes(p.retention) || (!p.keepRecords && p.useMemory)) return false
    }
  } catch { return false }
  return true
}

export function createMockApi(storage?: DemoStorage) {
  let state = initialData()
  let storageMode: 'memory' | 'session' = storage ? 'session' : 'memory'
  let storageWarning = ''
  if (storage) {
    try {
      const saved = storage.getItem(storageKey)
      if (saved) {
        const envelope = JSON.parse(saved)
        if (envelope.version === 1 && validSnapshot(envelope.data)) state = envelope.data
        else { storageWarning = '旧演示数据不兼容，已恢复初始样例。'; storage.removeItem(storageKey) }
      }
    } catch { storageMode = 'memory'; storageWarning = '浏览器存储不可用，当前使用内存样例。' }
  }
  function persist() {
    if (storageMode !== 'session' || !storage) return
    try { storage.setItem(storageKey, JSON.stringify({ version: 1, data: state })) }
    catch { storageMode = 'memory'; storageWarning = '浏览器无法保存演示变更；刷新可能恢复之前的样例。' }
  }
  function findDevice(id: string, scope: Scope = 'admin'): Device {
    const d = state.devices.find(item => item.id === id)
    if (!d) return fail('not_found', '未找到这台设备')
    if (scope === 'family' && d.familyId !== 'family-lin') return fail('forbidden', '当前家庭未授权访问这台设备')
    return d
  }
  function audit(action: string, target: string) {
    state.audit.unshift({ id: `audit-demo-${Date.now()}-${state.audit.length}`, time: '刚刚 · 演示', actor: '演示用户', action, target, result: '演示回执' })
  }
  return {
    get storageMode() { return storageMode },
    get storageWarning() { return storageWarning },
    async listDevices(scope: Scope = 'admin') { return clone(state.devices.filter(d => scope === 'admin' || d.familyId === 'family-lin')) },
    async getDevice(id: string, scope: Scope = 'admin') { return clone(findDevice(id, scope)) },
    async updateDevice(id: string, value: Pick<Device, 'name' | 'volume' | 'brightness'>, scope: Scope = 'admin') {
      const d = findDevice(id, scope), name = validName(value.name)
      if (!validPercent(value.volume) || !validPercent(value.brightness)) fail('invalid_value', '音量和亮度需为0至100的整数')
      Object.assign(d, { name, volume: value.volume, brightness: value.brightness, revision: d.revision + 1 })
      audit('配置变更', d.name); persist(); return clone(d)
    },
    async listConversations(filter: { deviceId?: string; query?: string; scope?: Scope } = {}) {
      if (filter.deviceId !== undefined) findDevice(filter.deviceId, filter.scope)
      return clone(state.conversations.filter(c => (filter.deviceId === undefined || c.deviceId === filter.deviceId) && (filter.scope !== 'family' || c.familyId === 'family-lin') && (!filter.query || c.title.includes(filter.query) || c.summary.includes(filter.query))).map(c => ({ ...c, turns: [] })))
    },
    async getConversation(id: string, scope: Scope = 'admin', expectedDeviceId?: string) {
      if (expectedDeviceId !== undefined) findDevice(expectedDeviceId, scope)
      const c = state.conversations.find(item => item.id === id)
      if (!c) return fail('not_found', '未找到这段聊天')
      findDevice(c.deviceId, scope)
      if (expectedDeviceId !== undefined && c.deviceId !== expectedDeviceId) fail('not_found', '这段聊天不属于所选设备')
      if (!c.bodyAvailable) return fail('forbidden', '这段聊天正文未授权查看')
      return clone(c)
    },
    async reset() { state = initialData(); persist() },
  }
}
function browserStorage(): DemoStorage | undefined {
  try { return globalThis.sessionStorage } catch { return undefined }
}
export const api = createMockApi(browserStorage())
export type MockApi = ReturnType<typeof createMockApi>
