<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, watch } from "vue";
import { api } from "../api.ts";
import type { Scope } from "../types.ts";
import { createMusicLibraryController, musicLibraryState } from "../music-library.ts";
import { onSensitiveInvalidated } from "../sensitive-session.ts";

const props = defineProps<{ deviceId: string; scope: Scope }>();
const state = reactive(musicLibraryState());
const control = createMusicLibraryController(api, state);
let unsubscribe = () => {};
function start() {
  if (document.visibilityState === "visible") void control.start(props.deviceId, props.scope);
}
function visibility() {
  if (document.visibilityState === "visible") start();
  else control.stop();
}
function chooseFile(event: Event) {
  state.file = (event.target as HTMLInputElement).files?.[0] ?? null;
}
function duration(ms: number) {
  const seconds = Math.floor(ms / 1000);
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}
function size(bytes: number) { return `${(bytes / 1024 / 1024).toFixed(1)} MiB`; }
onMounted(() => {
  unsubscribe = onSensitiveInvalidated(control.stop);
  document.addEventListener("visibilitychange", visibility);
  start();
});
watch(() => [props.deviceId, props.scope], () => { control.stop(); start(); });
onBeforeUnmount(() => {
  unsubscribe(); document.removeEventListener("visibilitychange", visibility); control.stop();
});
</script>
<template>
  <section class="panel music-library" aria-label="家庭私人曲库" :aria-busy="state.pending">
    <div class="section-head">
      <h2>私人曲库</h2>
      <button type="button" class="secondary" :disabled="state.pending" @click="control.refresh()">重新读取曲库</button>
    </div>
    <p>导入家庭个人使用的歌曲后，可以向设备点歌。同一家庭的设备共用曲库，删除会影响家人的点歌。</p>
    <p>私人点歌后的本次对话仅保留在聊天档案。</p>
    <p class="muted">支持 MP3、FLAC，单曲最多30 MiB、时长1秒至20分钟。全服务器各家庭共用300 MiB、100首上限，实际剩余空间以导入结果为准。设备仍以24 kHz单声道播放。</p>
    <template v-if="state.library">
      <p>本家庭已用 {{ size(state.library.usedBytes) }}，共 {{ state.library.items.length }} 首。</p>
      <p v-if="!state.library.canManage" class="muted">当前账号无曲库管理权限，只能查看。</p>
      <form v-else class="form" @submit.prevent="control.upload()">
        <fieldset :disabled="state.pending || state.needsRead">
          <legend>导入歌曲</legend>
          <label for="music-title">曲名</label>
          <input id="music-title" v-model="state.title" autocomplete="off" required aria-describedby="music-metadata-help" />
          <label for="music-artist">歌手</label>
          <input id="music-artist" v-model="state.artist" autocomplete="off" required aria-describedby="music-metadata-help" />
          <p id="music-metadata-help" class="muted">曲名最多500字，歌手最多500字；均由你填写，不代表原唱认证。</p>
          <label for="music-file">音频文件</label>
          <input id="music-file" :key="state.fileVersion" type="file" accept=".mp3,.flac,audio/mpeg,audio/flac" required @change="chooseFile" />
          <label class="music-consent" for="music-personal">
            <input id="music-personal" v-model="state.personalUse" type="checkbox" required />
            <span>我确认仅供家庭个人使用</span>
          </label>
          <button type="submit">{{ state.pending ? '正在处理，请稍候…' : '导入歌曲' }}</button>
        </fieldset>
      </form>
      <p v-if="state.library.items.length === 0">暂无歌曲，家庭管理者可以导入。</p>
      <ul v-else class="music-tracks">
        <li v-for="track in state.library.items" :key="track.id">
          <div>
            <strong>{{ track.title }}</strong>
            <p>{{ track.artist }} · {{ duration(track.durationMs) }}</p>
            <p class="muted">{{ track.format.toUpperCase() }} · {{ track.sampleRate }} Hz · {{ track.channels === 1 ? '单声道' : '双声道' }} · {{ track.bitrate === 0 ? '码率未知' : `${(track.bitrate / 1000).toFixed(1)} kbps` }} · {{ size(track.bytes) }}</p>
          </div>
          <button v-if="state.library.canManage && state.deleting !== track.id" type="button" class="secondary" :disabled="state.pending || state.needsRead" @click="control.askDelete(track.id)">删除</button>
          <div v-if="state.deleting === track.id" class="music-delete" role="group" aria-label="删除歌曲确认">
            <p>确认从家庭曲库删除「{{ track.title }}」？</p>
            <button type="button" :disabled="state.pending" @click="control.confirmDelete()">确认删除</button>
            <button type="button" class="secondary" :disabled="state.pending" @click="control.cancelDelete()">取消</button>
          </div>
        </li>
      </ul>
    </template>
    <p v-else-if="!state.pending" class="muted">曲库尚未读取或暂不可用，请重新读取。</p>
    <p role="status" aria-live="polite">{{ state.feedback }}</p>
  </section>
</template>
<style scoped>
.music-library { margin-top: 24px; }
.music-library fieldset { border: 0; margin: 0; padding: 0; min-width: 0; }
.music-library legend { margin-bottom: 12px; font-weight: 600; }
.music-library input:not([type="checkbox"]) { width: 100%; min-width: 0; }
.music-consent { display: flex; align-items: center; gap: 10px; margin: 16px 0; }
.music-consent input { flex: 0 0 auto; }
.music-tracks { list-style: none; padding: 0; margin: 16px 0 0; }
.music-tracks li { border-top: 1px solid var(--line); padding: 16px 0; display: flex; align-items: flex-start; flex-wrap: wrap; gap: 12px; }
.music-tracks li > div:first-child { flex: 1 1 220px; min-width: 0; overflow-wrap: anywhere; }
.music-tracks p { margin: 6px 0; }
.music-delete { flex-basis: 100%; }
.music-delete button { margin-right: 8px; }
</style>
