import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createMockApi } from '../src/api.ts'

test('device filtering never mixes chats between the two robots', async () => {
  const api = createMockApi()
  const desk = await api.listConversations({ deviceId: 'dev-desk' })
  assert.deepEqual(desk.map(item => item.id), ['chat-weather'])
  assert.ok((await api.listConversations({ deviceId: 'dev-living', query: '月亮' })).every(item => item.deviceId === 'dev-living'))
})

test('unknown device and unauthorized body are explicit failures', async () => {
  const api = createMockApi()
  await assert.rejects(api.getDevice('missing'), /未找到/)
  await assert.rejects(api.getConversation('chat-private'), /未授权/)
  await assert.rejects(api.getDevice('dev-zhou', 'family'), /未授权/)
  assert.equal((await api.listDevices('family')).length, 2)
})

test('invalid settings cannot partly mutate a device', async () => {
  const api = createMockApi()
  const original = await api.getDevice('dev-living')
  await assert.rejects(api.updateDevice(original.id, { name: '修改后的名字', volume: 101, brightness: 50 }), /0.*100/)
  await assert.rejects(api.updateDevice(original.id, { name: '无效\n名字', volume: 50, brightness: 50 }), /名称/)
  await assert.rejects(api.updateDevice(original.id, { name: '超'.repeat(13), volume: 50, brightness: 50 }), /名称/)
  assert.deepEqual(await api.getDevice(original.id), original)
})

test('saved settings are scoped to one device and reads are detached snapshots', async () => {
  const api = createMockApi()
  const first = await api.getDevice('dev-living')
  first.name = '外部篡改'
  assert.equal((await api.getDevice('dev-living')).name, '团团')
  const changed = await api.updateDevice('dev-living', { name: ' 新伙伴 ', volume: 0, brightness: 100 })
  assert.equal(changed.name, '新伙伴')
  assert.equal(changed.revision, 2)
  assert.equal((await api.getDevice('dev-desk')).volume, 40)
})

test('session persistence and reset retain only demonstration data', async () => {
  const items = new Map<string, string>()
  const storage = { getItem: (key: string) => items.get(key) ?? null, setItem: (key: string, value: string) => { items.set(key, value) }, removeItem: (key: string) => { items.delete(key) } }
  const first = createMockApi(storage)
  await first.updateDevice('dev-living', { name: '暖暖', volume: 30, brightness: 70 })
  assert.equal((await createMockApi(storage).getDevice('dev-living')).name, '暖暖')
  await first.reset()
  assert.equal((await createMockApi(storage).getDevice('dev-living')).name, '团团')
})

test('explicit device filters and conversation ownership are validated', async () => {
  const api = createMockApi()
  await assert.rejects(api.listConversations({ deviceId: '', scope: 'family' }), /未找到/)
  await assert.rejects(api.getConversation('chat-run', 'family', ''), /未找到/)
  await assert.rejects(api.getConversation('chat-run', 'family', 'missing'), /未找到/)
  await assert.rejects(api.getConversation('chat-run', 'family', 'dev-desk'), /设备/)
  assert.equal((await api.getConversation('chat-run', 'family', 'dev-living')).id, 'chat-run')
})

test('incompatible cached turns are discarded before rendering', async () => {
  const items = new Map<string, string>()
  const storage = { getItem: (key: string) => items.get(key) ?? null, setItem: (key: string, value: string) => { items.set(key, value) }, removeItem: (key: string) => { items.delete(key) } }
  const api = createMockApi(storage)
  await api.reset()
  const key = [...items.keys()][0]!
  const snapshot = JSON.parse(items.get(key)!)
  snapshot.data.conversations[0].turns = [null]
  items.set(key, JSON.stringify(snapshot))
  const restored = createMockApi(storage)
  assert.match(restored.storageWarning, /不兼容/)
  assert.ok((await restored.getConversation('chat-run')).turns.every(turn => turn !== null))
})
