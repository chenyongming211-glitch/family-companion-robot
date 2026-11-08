import type { Snapshot, Capability, Task, Firmware } from './types.ts'

export function initialData(): Snapshot {
  return {
    devices: [
      { id: 'dev-living', familyId: 'family-lin', name: '团团', location: '客厅', model: '微雪 ESP32-S3', connection: '最近已连接', activity: '待机示例', lastSeen: '今天 14:42', battery: null, volume: 55, brightness: 65, revision: 1 },
      { id: 'dev-desk', familyId: 'family-lin', name: '圆圆', location: '书桌', model: 'M5 ESP32-S3', connection: '最近已连接', activity: '无语音连接', lastSeen: '今天 11:12', battery: null, volume: 40, brightness: 50, revision: 1 },
      { id: 'dev-zhou', familyId: 'family-zhou', name: '豆豆', location: '卧室', model: '微雪 ESP32-S3', connection: '未上报', activity: '未知', lastSeen: '昨天 20:10', battery: null, volume: 35, brightness: 50, revision: 1 },
    ],
    families: [
      { id: 'family-lin', name: '林的家', members: [
        { id: 'member-lin', name: '林女士', email: 'parent@example.test', role: '家庭管理员', access: '本家庭设备', status: '已加入' },
        { id: 'member-dad', name: '林先生', email: 'dad@example.test', role: '家庭成员', access: '客厅机器人', status: '已加入' },
        { id: 'member-child', name: '安安', email: 'child@example.test', role: '家庭成员', access: '未授权正文', status: '已加入' },
      ] },
      { id: 'family-zhou', name: '周的家', members: [{ id: 'member-zhou', name: '周女士', email: 'zhou@example.test', role: '家庭管理员', access: '未授权内部正文', status: '已加入' }] },
    ],
    conversations: [
      { id: 'chat-run', deviceId: 'dev-living', familyId: 'family-lin', title: '第一次跑完一公里', date: '2026-11-09', time: '14:32', status: '已归档示例', endReason: '连续30分钟无有效交互', summary: '分享运动进步，讨论热身与散步。', bodyAvailable: true, turns: [
        { id: 'turn-run-1', user: '我今天第一次跑完一公里！', played: '真不错，第一次完成一定很开心。你现在感觉怎么样？', generated: '真不错，第一次完成一定很开心。你现在感觉怎么样？', playback: 'played', saved: true },
        { id: 'turn-run-2', user: '有一点累，我想先慢慢走一会儿。', played: '可以，放慢脚步，等呼吸平稳下来，再休息一下。', generated: '可以，放慢脚步，等呼吸平稳下来，再休息一下。', playback: 'played', saved: true },
      ] },
      { id: 'chat-story', deviceId: 'dev-living', familyId: 'family-lin', title: '小狐狸和月亮的故事', date: '2026-11-09', time: '11:06', status: '已结束示例', endReason: '用户明确结束', summary: '故事途中插问月亮的形状。', bodyAvailable: true, turns: [
        { id: 'turn-story-1', user: '讲一个小狐狸找月亮的故事。', played: '小狐狸走出树屋，看见一轮圆月挂在树梢。', generated: '小狐狸走出树屋，看见一轮圆月挂在树梢。它决定沿着小路去找月亮，后来遇到了猫头鹰。', playback: 'interrupted', saved: true },
        { id: 'turn-story-2', user: '等一下，月亮每天都是圆的吗？', played: '不是，我们看到的月亮会随时间改变形状，有时像弯弯的小船。', generated: '不是，我们看到的月亮会随时间改变形状，有时像弯弯的小船。', playback: 'played', saved: true },
      ] },
      { id: 'chat-weather', deviceId: 'dev-desk', familyId: 'family-lin', title: '周末适合去公园吗', date: '2026-11-09', time: '09:18', status: '已归档示例', endReason: '连续30分钟无有效交互', summary: '讨论户外活动安排。', bodyAvailable: true, turns: [
        { id: 'turn-weather-1', user: '周末想去公园。', played: '可以先看看当天的天气，再选一个舒服的时段出门。', generated: '可以先看看当天的天气，再选一个舒服的时段出门。', playback: 'played', saved: true },
      ] },
      { id: 'chat-dinner', deviceId: 'dev-living', familyId: 'family-lin', title: '晚饭后散步计划', date: '2026-11-08', time: '19:42', status: '待同步示例', endReason: '连接中断，结束原因未确认', summary: '记录保存状态未知，不补猜正文。', bodyAvailable: true, turns: [
        { id: 'turn-dinner-1', user: '吃完饭想散步。', played: '', generated: '可以等休息一会儿，再出门慢慢走走。', playback: 'unknown', saved: false },
      ] },
      { id: 'chat-private', deviceId: 'dev-zhou', familyId: 'family-zhou', title: '一段未授权的聊天', date: '2026-11-08', time: '20:00', status: '正文未授权', endReason: '未知', summary: '', bodyAvailable: false, turns: [] },
    ],
    memories: [
      { id: 'memory-run', deviceId: 'dev-living', conversationId: 'chat-run', text: '提到第一次完成一公里，想先慢走休息。', date: '2026-11-09' },
      { id: 'memory-story', deviceId: 'dev-living', conversationId: 'chat-story', text: '聊过小狐狸和月亮，也问过月亮的形状。', date: '2026-11-09' },
      { id: 'memory-weather', deviceId: 'dev-desk', conversationId: 'chat-weather', text: '计划周末去公园。', date: '2026-11-09' },
    ],
    privacy: {
      'dev-living': { keepRecords: true, useMemory: true, retention: '30' },
      'dev-desk': { keepRecords: true, useMemory: true, retention: '30' },
      'dev-zhou': { keepRecords: true, useMemory: false, retention: '30' },
    },
    accounts: {},
    audit: [
      { id: 'audit-1', time: '今天 14:45', actor: '内部管理员', action: '查看正文', target: '林的家 / 运动聊天', result: '授权示例' },
      { id: 'audit-2', time: '今天 13:20', actor: '林女士', action: '配置变更', target: '团团 / 名称资料', result: '演示回执' },
      { id: 'audit-3', time: '昨天 18:46', actor: '内部只读账号', action: '查看正文', target: '周的家', result: '已拒绝示例' },
    ],
  }
}

export const capabilities: Capability[] = [
  { id: 'voice', name: '实时语音', stage: '已接入', description: '连续问答与实时语音回应。', detail: 'Go＋千问原生Realtime；内容可靠性仍需独立验收。' },
  { id: 'interrupt', name: '自然插话', stage: '已接入', description: '有效新输入可停止旧回复。', detail: '物理停声、播放证据与用户反馈分别验收。' },
  { id: 'search', name: '联网查询', stage: '已接入', description: '按需查询并保留返回的来源。', detail: '结果由实际查询决定，本演示不发模型请求。' },
  { id: 'control', name: '设备控制', stage: '已接入', description: '根据设备能力控制音量、亮度。', detail: '真实成功需要设备回读；本页只改变示例数据。' },
  { id: 'face', name: '屏幕表情', stage: '已接入', description: '系统状态、15种情绪及说话嘴型。', detail: 'CHICK-01；模型情绪与实际播放分别关联。' },
  { id: 'memory', name: '设备近期记忆', stage: '已接入', description: '同一设备最近7天、至多24轮片段。', detail: '清除设备记忆与删除聊天档案分别处理。' },
  { id: 'speaker', name: '成员声纹', stage: '验证中', description: '家庭成员识别和陌生人拒识。', detail: '五人每人至少19/20正确且零误归，陌生0/200；家庭现场未验收。' },
  { id: 'archive', name: '聊天归档与查档', stage: '待开发', description: '30分钟分段、归档摘要与按需查询。', detail: '业务规则已确认，执行及页面真实接口尚未接入；Eino仍为候选。' },
  { id: 'photo', name: '拍照判题', stage: '未接入', description: '关联图片、题目与分析结果。', detail: '需独立能力与权限验收后开放。' },
  { id: 'reminder', name: '提醒与日程', stage: '未接入', description: '持久计划、定时触发和投递回执。', detail: '调度与实际投递闭环尚未接入。' },
]
export const tasks: Task[] = [
  { id: 'archive', name: '会话归档摘要', stage: '待开发', trigger: '聊天结束后', detail: '逐轮保存先完成；归档摘要异步处理，带来源、有界重试。' },
  { id: 'lookup', name: '历史记录查询', stage: '待开发', trigger: '用户按需请求', detail: '查询已授权正文，迟到结果不得抢占新轮次。' },
  { id: 'reminder', name: '提醒与日程', stage: '未接入', trigger: '未来独立任务', detail: '实际调度及投递需独立验收。' },
]
export const firmware: Firmware[] = [
  { model: '微雪 ESP32-S3', stage: '当前产品线', features: 'CHICK-01表情 / 嘴型 / 播放回报', acceptance: '唤醒可靠性、长稳与功耗仍需验收' },
  { model: 'M5 ESP32-S3', stage: '当前产品线', features: '显示适配，按具体型号协商', acceptance: '按具体型号独立验收' },
]
