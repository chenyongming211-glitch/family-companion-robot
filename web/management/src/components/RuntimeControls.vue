<script setup lang="ts">
import { onMounted, onBeforeUnmount, reactive, watch } from "vue";
import type { Scope, RuntimeField } from "../types.ts";
import { api } from "../api.ts";
import { createRuntimeController, runtimeState } from "../runtime-control.ts";
import { onSensitiveInvalidated } from "../sensitive-session.ts";

const props = defineProps<{ deviceId: string; scope: Scope }>();
const state = reactive(runtimeState());
const control = createRuntimeController(api, state);
const fields = [["volume", "音量"], ["brightness", "屏幕亮度"]] as const;
const commandStatuses: Record<string, string> = {
  queued: "已排队", sent: "已发送，等待回执", confirmed: "设备已确认",
  failed: "失败", expired: "已过期", unknown: "结果未知",
};
let unsubscribe = () => {};
function start() {
  if (document.visibilityState === "visible") void control.start(props.deviceId, props.scope);
}
function visibility() {
  if (document.visibilityState === "visible") start();
  else control.stop();
}
onMounted(() => {
  unsubscribe = onSensitiveInvalidated(control.stop);
  document.addEventListener("visibilitychange", visibility);
  start();
});
watch(() => [props.deviceId, props.scope], () => { control.stop(); start(); });
onBeforeUnmount(() => {
  unsubscribe();
  document.removeEventListener("visibilitychange", visibility);
  control.stop();
});
function value(event: Event) {
  return Number((event.target as HTMLInputElement).value);
}
function disabled(field: RuntimeField) {
  return state.pending || state.runtime?.connection !== "online" ||
    !state.runtime?.canControl || state.runtime[field] === null ||
    ["queued", "sent"].includes(state.runtime.command?.status ?? "");
}
</script>
<template>
  <section class="panel runtime-controls" aria-label="真实设备状态与控制">
    <div class="section-head">
      <h2>设备状态与控制</h2>
      <span class="badge" :data-status="state.runtime?.connection">{{ state.runtime?.connection === 'online' ? '在线' : state.runtime?.connection === 'offline' ? '离线' : '尚未上报' }}</span>
    </div>
    <p>拖动预览，松手提交；机器人确认后才显示生效</p>
    <p v-if="state.runtime?.seenAt" class="muted">
      最近更新：{{ new Date(state.runtime.seenAt).toLocaleString('zh-CN') }}
    </p>
    <div v-for="[field, label] in fields" :key="field" class="runtime-field">
      <label :for="'runtime-' + field">{{ label }}：{{ state[field] === null ? '尚未上报' : state[field] + '%' }}</label>
      <input
        :id="'runtime-' + field" type="range" min="0" max="100" step="10"
        :value="state[field] ?? 50" :disabled="disabled(field)"
        :aria-valuetext="state[field] === null ? '尚未上报' : state[field] + '%'"
        @input="control.preview(field, value($event))"
        @change="control.submit(field, value($event))"
        @pointercancel="control.cancelEdit(field)"
      />
      <div class="runtime-ticks" aria-hidden="true"><span v-for="n in 11" :key="n">○</span></div>
    </div>
    <p v-if="state.runtime && !state.runtime.canControl" class="muted">当前账号无设备管理权限。</p>
    <p v-else-if="state.runtime?.connection !== 'online'" class="muted">等待设备在线并上报能力后可设置。</p>
    <p v-if="!state.pending && ['queued', 'sent'].includes(state.runtime?.command?.status ?? '')" role="status">设备已有操作等待回执，请稍后。</p>
    <p v-if="state.runtime?.command" class="muted">
      最近操作：{{ state.runtime.command.field === 'volume' ? '音量' : '屏幕亮度' }}
      {{ state.runtime.command.value }}% · {{ commandStatuses[state.runtime.command.status] ?? '状态未知' }}
      <span v-if="state.runtime.command.actual !== null"> · 实际 {{ state.runtime.command.actual }}%</span>
    </p>
    <p role="status">{{ state.feedback }}</p>
  </section>
</template>
<style scoped>
.runtime-controls { margin-top: 24px; }
.runtime-field { margin-top: 24px; }
.runtime-field label { display: block; }
.runtime-ticks { display: flex; justify-content: space-between; padding: 0 8px; font-size: 14px; color: var(--text-muted); }
.runtime-field input { width: 100%; padding: 0; min-height: 44px; }
.runtime-field input:disabled { opacity: .45; }
</style>
