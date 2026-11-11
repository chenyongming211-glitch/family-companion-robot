import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";

function deferred() {
  let resolve!: (value: any) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<any>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
async function page() {
  const source = readFileSync(
    new URL("../src/pages/CorePages.vue", import.meta.url),
    "utf8",
  )
    .split('<script setup lang="ts">')[1]!
    .split("</script>")[0]!
    .replace(/^import .*;\n/gm, "");
  const code = ts.transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022 },
  }).outputText;
  let poll!: () => void;
  let detail = async () => ({ id: "chat", transcript: "private old text" });
  let reads = 0;
  const api = {
    listDevices: async () => [{ id: "device" }],
    getConversation: () => {
      reads++;
      return detail();
    },
  };
  const vue = {
    ref: (value: any) => ({ value }),
    computed: (get: () => any) => ({
      get value() {
        return get();
      },
    }),
    watch: () => {},
    onMounted: (fn: () => void) => fn(),
    onBeforeUnmount: () => {},
  };
  const run = new Function(
    "onSensitiveInvalidated",
    "api",
    "pageProps",
    "vue",
    "setInterval",
    "document",
    "location",
    `const { ref, computed, watch, onMounted, onBeforeUnmount } = vue; const defineProps = () => pageProps; const defineEmits = () => () => {}; const linkTo = () => ''; ${code}; return { load, chat, chats, devices, error, loading };`,
  );
  const state = run(
    () => () => {},
    api,
    {
      scope: "family",
      route: { page: "conversation", chat: "chat", device: "device" },
    },
    vue,
    (fn: () => void) => {
      poll = fn;
    },
    { visibilityState: "visible" },
    { hash: "?device=device" },
  );
  return {
    state,
    poll: () => poll(),
    setDetail: (fn: typeof detail) => {
      detail = fn;
    },
    reads: () => reads,
  };
}
async function tick() {
  await new Promise((resolve) => setImmediate(resolve));
}

for (const failure of ["forbidden", "network unavailable"])
  test(`failed refresh (${failure}) discards cached transcript and keeps error during deferred retry`, async () => {
    const p = await page();
    await p.state.load();
    assert.equal(p.state.chat.value.transcript, "private old text");
    p.setDetail(async () => {
      throw new Error(failure);
    });
    await p.state.load(true);
    assert.equal(p.state.chat.value, null);
    assert.deepEqual(p.state.devices.value, []);
    assert.deepEqual(p.state.chats.value, []);
    const retry = deferred();
    p.setDetail(() => retry.promise);
    const pending = p.state.load(true);
    await tick();
    assert.ok(p.state.error.value);
    assert.equal(p.state.chat.value, null);
    retry.reject(new Error(failure));
    await pending;
    assert.ok(p.state.error.value);
    p.setDetail(async () => ({ id: "new", transcript: "new allowed text" }));
    await p.state.load();
    assert.equal(p.state.error.value, "");
    assert.equal(p.state.chat.value.transcript, "new allowed text");
  });

test("interval skips overlapping refreshes and pending result commits", async () => {
  const p = await page();
  await p.state.load();
  const slow = deferred();
  p.setDetail(() => slow.promise);
  p.poll();
  await tick();
  p.poll(); // another five-second interval while the previous request is unresolved
  await tick();
  assert.equal(p.reads(), 2);
  slow.resolve({ id: "new", transcript: "fresh text" });
  await tick();
  assert.equal(p.state.chat.value.transcript, "fresh text");
});
