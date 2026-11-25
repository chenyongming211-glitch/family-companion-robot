<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, watch } from "vue";
import { api } from "../api.ts";
import { createVoiceRoutesController, voiceRoutesState } from "../voice-routes.ts";
import { onSensitiveInvalidated } from "../sensitive-session.ts";
import type { VoiceRoute } from "../types.ts";

const props = defineProps<{deviceId: string}>();
const state = reactive(voiceRoutesState());
const control = createVoiceRoutesController(api, state);
const routes: {id: VoiceRoute; name: string; output: string; retired?: boolean}[] = [
  {id: "A", name: "Omni 语音理解 → 文字 → CosyVoice（已停用）", output: "理解语音后生成文字，完整合成后播放；已停用并收敛至 C 链路。", retired: true},
  {id: "B", name: "Fun-ASR → Qwen Plus → CosyVoice（冷备）", output: "先识别文字，再生成回答，使用与 A 相同的语音合成；完整合成后播放。"},
  {id: "C", name: "Omni 原生直接语音（默认主线）", output: "直接生成语音，流式播放。"},
];
let unsubscribe = () => {};
function start() {if (document.visibilityState === "visible") void control.start(props.deviceId);}
function visibility() {if (document.visibilityState === "visible") start(); else control.stop();}
onMounted(() => {
  unsubscribe = onSensitiveInvalidated(control.stop);
  document.addEventListener("visibilitychange", visibility); start();
});
watch(() => props.deviceId, () => {control.stop(); start();});
onBeforeUnmount(() => {unsubscribe(); document.removeEventListener("visibilitychange", visibility); control.stop();});
</script>
<template>
  <section class="panel voice-routes" aria-label="语音链路" :aria-busy="state.pending">
    <div class="section-head">
      <h2>语音链路</h2>
      <button id="voice-route-refresh" type="button" class="secondary" :disabled="state.pending" @click="control.refresh()">刷新状态</button>
    </div>
    <p id="voice-route-notice">点击任一方案后，当前对话将立即结束。再次唤醒或按键连接时使用所选方案。</p>
    <p class="muted">A、B 会等待完整语音合成并校验后开始播放；C 随生成随播放。</p>
    <div class="voice-route-options" role="group" aria-label="选择语音链路" aria-describedby="voice-route-notice">
      <button v-for="route in routes" :id="'voice-route-' + route.id" :key="route.id" type="button"
        :aria-pressed="state.view?.selectedRoute === route.id" :disabled="Boolean(route.retired) || !control.canSwitch()" @click="control.select(route.id)">
        <strong>{{ route.id }} · {{ route.name }}</strong>
        <span>{{ route.output }}</span>
      </button>
    </div>
    <template v-if="state.view">
      <p>已选择：{{ state.view.selectedRoute }}（下次连接使用）</p>
      <p>当前连接实际链路：{{ state.view.activeRoute ?? (state.view.connection === 'idle' ? '无连接' : state.view.connection === 'connecting' ? '尚未确定（连接建立中）' : '未知') }}<span v-if="state.view.activeRoute">（{{ state.view.connection === 'connecting' ? '连接建立中' : '对话中' }}）</span></p>
      <p v-if="state.view.connection === 'unknown'" class="muted">当前连接状态未知，请刷新状态；已保存的选择不代表旧对话已结束。</p>
      <p v-if="!state.view.canSwitch" class="muted">当前账号没有此设备的语音链路切换权限。</p>
    </template>
    <p id="voice-route-status" role="status" aria-live="polite">{{ state.feedback || (state.view ? '' : '语音链路尚未读取，请刷新状态。') }}</p>
  </section>
</template>
<style scoped>
.voice-routes { margin-top: 24px; }
.voice-route-options { display: grid; gap: 12px; margin: 20px 0; }
.voice-route-options button { display: flex; flex-direction: column; align-items: flex-start; gap: 8px; width: 100%; padding: 16px; text-align: left; white-space: normal; overflow-wrap: anywhere; background: var(--surface, #fff); color: var(--text); border: 1px solid var(--line); }
.voice-route-options button:hover:not(:disabled) { background: var(--accent-soft); color: var(--text); border-color: var(--accent); }
.voice-route-options button[aria-pressed="true"] { border-color: var(--accent, #3267d6); box-shadow: inset 3px 0 var(--accent, #3267d6); }
.voice-route-options button:focus-visible { outline: 3px solid var(--accent, #3267d6); outline-offset: 3px; }
.voice-route-options span { font-weight: normal; line-height: 1.6; }
@media (max-width: 600px) { .voice-routes .section-head { align-items: flex-start; gap: 12px; flex-wrap: wrap; } }
</style>
