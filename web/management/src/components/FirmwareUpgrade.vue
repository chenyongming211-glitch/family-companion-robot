<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, watch } from "vue";
import { api } from "../api.ts";
import type { Scope } from "../types.ts";
import { createFirmwareUpgradeController, firmwareCandidates, firmwareJobMessage, firmwareUpgradeState } from "../firmware-upgrade.ts";
import { onSensitiveInvalidated } from "../sensitive-session.ts";

const props = defineProps<{ deviceId: string; scope: Scope }>();
const state = reactive(firmwareUpgradeState());
const control = createFirmwareUpgradeController(api, state);
const releases = computed(() => state.view ? firmwareCandidates(state.view) : []);
const confirming = computed(() => releases.value.find(release => release.id === state.confirming));
let unsubscribe = () => {};
function start() { if (document.visibilityState === "visible") void control.start(props.deviceId, props.scope); }
function visibility() { if (document.visibilityState === "visible") start(); else control.stop(); }
function size(bytes: number) { return `${(bytes / 1024 / 1024).toFixed(2)} MiB（${bytes.toLocaleString("zh-CN")} 字节）`; }
onMounted(() => {
  unsubscribe = onSensitiveInvalidated(control.stop);
  document.addEventListener("visibilitychange", visibility);
  start();
});
watch(() => [props.deviceId, props.scope], () => {control.stop(); start();});
onBeforeUnmount(() => {
  unsubscribe(); document.removeEventListener("visibilitychange", visibility); control.stop();
});
</script>
<template>
  <section class="panel firmware-upgrade" aria-label="设备固件升级" :aria-busy="state.pending">
    <div class="section-head">
      <h2>固件升级</h2>
      <button id="firmware-refresh" type="button" class="secondary" :disabled="state.pending" @click="control.refresh()">检查升级</button>
    </div>
    <p>手动选择版本并确认后，设备通过 Wi-Fi 下载升级。升级期间暂时不能对话，请保持供电与 Wi-Fi。</p>
    <template v-if="state.view">
      <p v-if="state.view.current">当前版本：{{ state.view.current.version }}</p>
      <p v-else class="muted">当前固件版本未知（旧固件尚未报告版本）。</p>
      <p v-if="state.view.connection !== 'online'" class="muted">设备{{ state.view.connection === 'offline' ? '离线' : '尚未上报在线状态' }}，当前无法发起升级。请在设备联网后检查升级。</p>
      <p v-else-if="!state.view.canUpgrade" class="muted">当前无法发起升级。需要有管理权限的账号，并等待设备空闲和自检通过后重新检查。</p>
      <p v-if="state.view.mode === 'restart' && !state.view.job" class="muted">此旧固件需要重启后开始升级。请先选择版本并确认创建任务，再按任务提示重启设备。</p>
      <p v-if="!releases.length">暂无可升级版本。</p>
      <ul v-else class="firmware-releases">
        <li v-for="release in releases" :key="release.id">
          <div>
            <strong>版本 {{ release.version }}</strong>
            <p>{{ size(release.size) }}</p>
            <p class="muted" :title="release.sourceCommit">来源 {{ release.sourceCommit.slice(0, 7) }}</p>
          </div>
          <button type="button" :disabled="!control.canUpgrade(release.id) || !!state.confirming" @click="control.askUpgrade(release.id)">升级到 {{ release.version }}</button>
        </li>
      </ul>
      <div v-if="confirming" class="firmware-confirm" role="group" aria-label="固件升级确认">
        <p>确认升级到 {{ confirming.version }}？升级期间设备暂时不能对话，请保持供电与 Wi-Fi。</p>
        <button id="firmware-confirm" type="button" :disabled="state.pending" @click="control.confirmUpgrade()">确认升级</button>
        <button id="firmware-cancel" type="button" class="secondary" :disabled="state.pending" @click="control.cancelUpgrade()">取消</button>
      </div>
      <p v-if="state.view.job" class="muted">最近升级任务：版本 {{ state.view.job.version }}</p>
    </template>
    <p id="firmware-status" role="status" aria-live="polite">{{ state.feedback || (state.view ? firmwareJobMessage(state.view) : '升级状态尚未读取，请点击检查升级。') }}</p>
  </section>
</template>
<style scoped>
.firmware-upgrade { margin-top: 24px; }
.firmware-releases { list-style: none; padding: 0; margin: 16px 0; }
.firmware-releases li { border-top: 1px solid var(--line); padding: 16px 0; display: flex; align-items: flex-start; flex-wrap: wrap; gap: 12px; }
.firmware-releases li > div { flex: 1 1 200px; min-width: 0; overflow-wrap: anywhere; }
.firmware-releases p { margin: 6px 0; }
.firmware-confirm { padding: 16px 0; border-top: 1px solid var(--line); }
.firmware-confirm button { margin-right: 8px; margin-bottom: 8px; }
</style>
