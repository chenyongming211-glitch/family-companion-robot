import type { Api } from "./api.ts";
import type { LibraryTrack, MusicLibrary, Scope } from "./types.ts";

export function musicLibraryState() {
  return {
    library: null as MusicLibrary | null,
    title: "", artist: "", file: null as File | null, personalUse: false,
    fileVersion: 0, pending: false, needsRead: true, feedback: "",
    deleting: null as number | null,
  };
}
type Clock = {
  after: (fn: () => void, milliseconds: number) => unknown;
  cancel: (timer: unknown) => void;
  now?: () => number;
};
// Owns private form RAM, request cancellation and read-after-write confirmation.
// The API validates wire data; the server owns authorization, media and capacity.
export function createMusicLibraryController(
  api: Pick<Api, "getMusicLibrary" | "uploadMusicTrack" | "deleteMusicTrack">,
  state: ReturnType<typeof musicLibraryState>,
  clock: Clock = { after: (fn, ms) => setTimeout(fn, ms), cancel: timer => clearTimeout(timer as ReturnType<typeof setTimeout>) },
) {
  let epoch = 0, operation = 0, id = "", scope: Scope = "family";
  let timer: unknown, abort: AbortController | undefined, interrupt: (() => void) | undefined;
  const now = clock.now ?? Date.now;
  function clearForm() {
    state.title = state.artist = "";
    state.file = null; state.personalUse = false; state.fileVersion++;
  }
  function cancel() {
    if (timer !== undefined) clock.cancel(timer);
    timer = undefined;
    abort?.abort(); abort = undefined;
    interrupt?.(); interrupt = undefined;
  }
  function stop() {
    epoch++; operation++; id = ""; cancel();
    const fileVersion = state.fileVersion + 1;
    Object.assign(state, musicLibraryState(), { fileVersion });
  }
  function apply(value: MusicLibrary) {
    if (value.deviceId !== id) throw new Error("曲库所属设备未核实");
    state.library = value; state.needsRead = false;
    if (!value.canManage) clearForm();
  }
  async function perform(action: (current: () => boolean, signal: AbortSignal) => Promise<void>, milliseconds: number) {
    const token = epoch, attempt = ++operation, deadline = now() + milliseconds;
    state.pending = true;
    abort = new AbortController();
    const signal = abort.signal;
    const interrupted = new Promise<void>(resolve => { interrupt = resolve; });
    function expire() {
      if (token !== epoch || attempt !== operation) return;
      operation++; cancel();
      state.pending = false; state.needsRead = true; state.library = null; state.deleting = null;
      clearForm();
      state.feedback = `等待超过${milliseconds / 1000}秒，结果未确认；请重新读取后再操作，不会自动重试。`;
    }
    const current = () => {
      if (token !== epoch || attempt !== operation) return false;
      if (now() >= deadline) { expire(); return false; }
      return true;
    };
    timer = clock.after(expire, milliseconds);
    try {
      await Promise.race([action(current, signal), interrupted]);
    } catch (error) {
      if (current()) {
        state.library = null; state.needsRead = true; state.deleting = null;
        clearForm();
        state.feedback = (error instanceof Error ? error.message : "曲库结果未确认") + "；请重新读取后再操作，不会自动重试。";
      }
    } finally {
      if (token === epoch && attempt === operation) { state.pending = false; cancel(); }
    }
  }
  async function refresh() {
    if (!id || state.pending) return;
    const selected = id, selectedScope = scope;
    state.library = null; state.needsRead = true; state.deleting = null;
    state.feedback = "正在读取私人曲库…";
    await perform(async (current, signal) => {
      const value = await api.getMusicLibrary(selected, selectedScope, signal);
      if (current()) { apply(value); state.feedback = ""; }
    }, 30000);
  }
  function manageable() { return !!id && !state.pending && !state.needsRead && state.library?.canManage; }
  return {
    async start(device: string, selectedScope: Scope) { stop(); id = device; scope = selectedScope; await refresh(); },
    stop, refresh,
    async upload() {
      if (!manageable()) return;
      const title = state.title.trim(), artist = state.artist.trim(), file = state.file;
      if (!title || !artist || Array.from(title).length > 500 || Array.from(artist).length > 2000 || /[\p{Cc}]/u.test(title + artist)) {
        state.feedback = "请填写曲名（最多500字）和歌手（最多2000字），不要包含控制字符。"; return;
      }
      if (!state.personalUse) { state.feedback = "请先确认仅供家庭个人使用。"; return; }
      if (!file || !/\.(mp3|flac)$/i.test(file.name) || file.size < 1 || file.size > state.library!.maxTrackBytes) {
        state.feedback = "请选择非空 MP3 或 FLAC 文件，单曲最多30 MiB。"; return;
      }
      const selected = id, selectedScope = scope;
      state.needsRead = true; state.deleting = null;
      state.feedback = "正在上传并校验音频，最多等待120秒；完成后重新读取确认，请勿重复提交。";
      await perform(async (current, signal) => {
        const result = await api.uploadMusicTrack(selected, { title, artist, personalUse: true, file }, selectedScope, signal);
        if (!current()) return;
        if (result.title !== title || result.artist !== artist) throw new Error("导入响应未核实");
        const confirmed = await api.getMusicLibrary(selected, selectedScope, signal);
        if (!current()) return;
        const found = confirmed.items.find(t => t.id === result.id);
        if (!found || !sameTrack(found, result)) throw new Error("导入后的读取结果未核实");
        apply(confirmed); clearForm();
        state.feedback = "歌曲已导入私人曲库。";
      }, 120000);
    },
    askDelete(trackId: number) {
      if (manageable() && state.library!.items.some(t => t.id === trackId)) { state.deleting = trackId; state.feedback = ""; }
    },
    cancelDelete() { if (!state.pending) state.deleting = null; },
    async confirmDelete() {
      const trackId = state.deleting;
      if (!manageable() || trackId === null) return;
      const selected = id, selectedScope = scope;
      state.needsRead = true; state.feedback = "正在删除并重新读取确认…";
      await perform(async (current, signal) => {
        const receipt = await api.deleteMusicTrack(selected, trackId, selectedScope, signal);
        if (!current()) return;
        if (receipt.deleted !== trackId) throw new Error("删除响应未核实");
        const confirmed = await api.getMusicLibrary(selected, selectedScope, signal);
        if (!current()) return;
        if (confirmed.items.some(t => t.id === trackId)) throw new Error("删除后的读取结果未核实");
        apply(confirmed); state.deleting = null; state.feedback = "歌曲已删除。";
      }, 30000);
    },
  };
}
function sameTrack(a: LibraryTrack, b: LibraryTrack) {
  return (["id", "title", "artist", "durationMs", "bytes", "format", "bitrate", "sampleRate", "channels", "addedAt"] as const).every(k => a[k] === b[k]);
}
