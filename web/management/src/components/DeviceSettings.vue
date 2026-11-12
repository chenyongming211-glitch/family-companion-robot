<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, watch } from "vue";
import { api } from "../api.ts";
import type { DeviceSettings, Scope } from "../types.ts";
import { createDeviceSettingsController, deviceSettingsState } from "../device-settings.ts";
import { onSensitiveInvalidated } from "../sensitive-session.ts";

const props = defineProps<{ deviceId: string; scope: Scope }>();
const emit = defineEmits<{ saved: [value: DeviceSettings] }>();
const state = reactive(deviceSettingsState());
const control = createDeviceSettingsController(api, state, value => emit("saved", value));
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
</script>
<template>
  <section class="panel form device-settings" aria-label="设备昵称和放置位置">
    <h2>设备信息</h2>
    <p>设置设备昵称和放置位置，方便家人辨认。</p>
    <form @submit.prevent="control.submit()">
      <label for="settings-name">设备昵称</label>
      <input id="settings-name" v-model="state.name" :disabled="state.pending || state.needsRead || !state.settings?.canEdit" autocomplete="off" aria-describedby="settings-name-help" />
      <p id="settings-name-help" class="muted">1至12个字（{{ Array.from(state.name.trim()).length }}/12）</p>
      <label for="settings-location">放置位置</label>
      <input id="settings-location" v-model="state.location" :disabled="state.pending || state.needsRead || !state.settings?.canEdit" autocomplete="off" aria-describedby="settings-location-help" />
      <p id="settings-location-help" class="muted">可以留空，最多24个字（{{ Array.from(state.location.trim()).length }}/24）</p>
      <p v-if="state.settings && !state.settings.canEdit" class="muted">当前账号无设备管理权限，只能查看。</p>
      <div class="section-head">
        <button type="submit" :disabled="state.pending || state.needsRead || !state.settings?.canEdit">{{ state.pending ? '正在读取或保存…' : '保存设置' }}</button>
        <button type="button" class="secondary" :disabled="state.pending" @click="control.refresh()">重新读取</button>
      </div>
    </form>
    <p role="status">{{ state.feedback }}</p>
  </section>
</template>
<style scoped>
.device-settings { margin-top: 24px; }
.device-settings input { width: 100%; }
</style>
