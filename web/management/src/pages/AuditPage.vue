<script setup lang="ts">
import { onBeforeUnmount, reactive, ref, watch } from "vue";
import { api } from "../api.ts";
import { auditState, createAuditController } from "../audit-control.ts";
import { onSensitiveInvalidated } from "../sensitive-session.ts";
import { linkTo } from "../navigation.ts";
import type { AuditAction, PageRoute, Scope } from "../types.ts";

const props = defineProps<{ scope: Scope; route: PageRoute }>();
const state = reactive(auditState());
const active = ref(false);
const control = createAuditController({
  listAuditPage: filter => {
    if (api.mode !== "live") throw new Error("真实审计仅在内部后台提供。");
    return api.listAuditPage(filter);
  },
  listDevices: () => api.listDevices("admin"),
}, state);
const actions: Record<AuditAction, string> = {
  access: "访问记录", device_control: "设备控制", memory_clear: "记忆清除", settings: "设置变更",
};
const statuses = { recorded: "已记录", requested: "已请求", confirmed: "已确认", failed: "失败", unknown: "结果未知" };
function clear() {
  active.value = false;
  control.invalidate();
}
const unsubscribe = onSensitiveInvalidated(clear);
function visibility() {
  if (document.visibilityState !== "visible") clear();
}
document.addEventListener("visibilitychange", visibility);
watch(() => [props.scope, props.route], () => {
  clear();
  state.draft.device = props.route.device;
  if (api.mode === "live" && props.scope === "admin" && props.route.page === "audit" && document.visibilityState === "visible") {
    active.value = true;
    void control.activate();
  }
}, { immediate: true });
function edit(key: "device" | "from" | "to" | "action", event: Event) {
  const value = (event.target as HTMLInputElement | HTMLSelectElement).value;
  if (key === "action") state.draft.action = value as AuditAction | "";
  else state.draft[key] = value;
  control.filtersChanged();
}
function time(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "未记录" : date.toLocaleString("zh-CN", { hour12: false });
}
function number(value: number | undefined) {
  return typeof value === "number" ? `${value}%` : "未记录";
}
onBeforeUnmount(() => {
  clear(); unsubscribe();
  document.removeEventListener("visibilitychange", visibility);
});
</script>
<template>
  <div class="audit-page">
    <div class="heading">
      <div><p class="eyebrow">工作台 / 审计</p><h1>操作审计</h1>
        <p>查看获授权设备的操作记录与核实结果。旧记录缺少回执时，结果显示未知。</p>
        <p>本页显示平台已记录的操作；语音指令和设备按键变化暂未纳入。</p></div>
    </div>
    <section v-if="active" class="panel">
      <form id="audit-form" class="audit-filters" @submit.prevent="control.refresh()">
        <label for="audit-device">设备<select id="audit-device" :value="state.draft.device" @change="edit('device', $event)">
          <option value="">全部授权设备</option>
          <option v-if="state.draft.device && !state.devices.some(d => d.id === state.draft.device)" :value="state.draft.device">当前筛选设备</option>
          <option v-for="device in state.devices" :key="device.id" :value="device.id">{{ device.name }}</option>
        </select></label>
        <label for="audit-from">开始日期<input id="audit-from" type="date" :value="state.draft.from" @input="edit('from', $event)" /></label>
        <label for="audit-to">结束日期<input id="audit-to" type="date" :value="state.draft.to" @input="edit('to', $event)" /></label>
        <label for="audit-action">操作类型<select id="audit-action" :value="state.draft.action" @change="edit('action', $event)">
          <option value="">全部类型</option><option v-for="(label, action) in actions" :key="action" :value="action">{{ label }}</option>
        </select></label>
        <button id="audit-query" type="submit" :disabled="state.loading">查询</button>
        <button id="audit-refresh" type="button" class="secondary" :disabled="state.loading" @click="control.refresh()">刷新</button>
      </form>
      <p class="muted">日期按本地时区计算，包含结束当天。每页最多 50 条。</p>
      <p v-if="state.deviceError" role="status">{{ state.deviceError }}</p>
    </section>
    <section v-if="!active" class="panel" role="status">页面内容已清理，请重新进入审计页面读取。</section>
    <section v-else class="panel" aria-label="审计记录" :aria-busy="state.loading">
      <p v-if="state.loading" role="status">正在读取审计记录…</p>
      <p v-else-if="state.error" class="error" role="alert">{{ state.error }}</p>
      <template v-else>
        <p v-if="state.pending" class="notice" role="status">日志正在写入，末条记录尚未完成；稍后刷新可查看。</p>
        <p v-if="!state.items.length" class="empty">暂无审计记录，请调整条件或查询。</p>
        <ol v-else class="audit-list">
          <li v-for="item in state.items" :key="item.id" class="audit-entry">
            <div class="audit-entry-heading"><strong>{{ item.description }}</strong><span class="audit-result" :class="item.status">{{ statuses[item.status] }}</span></div>
            <p class="muted"><time :datetime="item.time">{{ time(item.time) }}</time> · {{ item.actor || '历史账号' }} · {{ item.scope === 'admin' ? '内部后台' : '家庭端' }}</p>
            <dl class="audit-facts">
              <div><dt>设备</dt><dd><a v-if="item.canOpenDevice" :href="linkTo('admin', 'device', { device: item.deviceId })">{{ item.target || '历史设备' }} ↗</a><span v-else>{{ item.target || '历史设备' }}</span></dd></div>
              <div><dt>操作</dt><dd>{{ actions[item.action] }}</dd></div>
              <template v-if="item.action === 'device_control' || item.field || item.value !== undefined || item.actual !== undefined">
                <div><dt>{{ item.field === 'volume' ? '目标音量' : item.field === 'brightness' ? '目标亮度' : '目标值' }}</dt><dd>{{ number(item.value) }}</dd></div>
                <div><dt>实际回执</dt><dd>{{ number(item.actual) }}</dd></div>
              </template>
            </dl>
            <p class="audit-outcome">{{ item.result }}</p>
          </li>
        </ol>
        <div v-if="!state.error" class="audit-pagination">
          <button id="audit-previous" type="button" class="secondary" :disabled="state.loading || state.offset === 0" @click="control.previous()">上一页</button>
          <span>第 {{ state.offset / state.limit + 1 }} 页 · 本页 {{ state.items.length }} 条</span>
          <button id="audit-next" type="button" class="secondary" :disabled="state.loading || !state.hasMore || state.offset + state.limit > 10000" @click="control.next()">下一页</button>
        </div>
        <p v-if="state.hasMore && state.offset + state.limit > 10000" role="status">已到可查询分页范围，请缩小设备或日期条件。</p>
      </template>
    </section>
  </div>
</template>
<style scoped>
.audit-filters { display: flex; align-items: end; flex-wrap: wrap; gap: 12px; }
.audit-filters label { display: grid; gap: 7px; min-width: 150px; flex: 1; }
.audit-filters input, .audit-filters select { min-height: 44px; width: 100%; padding: 8px; border: 1px solid #dce0e5; border-radius: 6px; background: white; color: inherit; }
.audit-list { list-style: none; padding: 0; margin: 0; }
.audit-entry { padding: 20px 0; border-bottom: 1px solid #e5e7eb; overflow-wrap: anywhere; }
.audit-entry:first-child { padding-top: 4px; }
.audit-entry-heading, .audit-pagination { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.audit-result { flex: none; padding: 5px 10px; border-radius: 5px; background: #f0f2f5; font-size: 12px; }
.audit-result.confirmed { background: #e5f2e9; color: #245d37; }
.audit-result.failed { background: #fcebea; color: #9c302a; }
.audit-facts { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; margin: 16px 0; }
.audit-facts dt { color: #6f7784; font-size: 12px; margin-bottom: 6px; }
.audit-facts dd { margin: 0; }
.audit-facts a { color: #a44716; text-decoration: underline; text-underline-offset: 3px; }
.audit-outcome { margin-bottom: 0; }
.audit-pagination { padding-top: 20px; font-size: 13px; }
@media (max-width: 600px) { .audit-filters label { min-width: 100%; } .audit-filters button { flex: 1; } .audit-facts { grid-template-columns: repeat(2, minmax(0, 1fr)); } .audit-pagination { flex-wrap: wrap; } .audit-pagination span { order: -1; flex-basis: 100%; text-align: center; } }
</style>
