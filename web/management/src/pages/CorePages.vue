<script setup lang="ts">
import UiIcon from "../components/UiIcon.vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { Scope, PageRoute, Device, Conversation } from "../types.ts";
import { api } from "../api.ts";
import RuntimeControls from "../components/RuntimeControls.vue";
import { onSensitiveInvalidated } from "../sensitive-session.ts";
import { linkTo } from "../navigation.ts";
const props = defineProps<{ scope: Scope; route: PageRoute }>();
const devices = ref<Device[]>([]),
  chats = ref<Conversation[]>([]),
  device = ref<Device | null>(null),
  chat = ref<Conversation | null>(null);
const loading = ref(false),
  error = ref(""),
  search = ref("");
const explicitDevice = computed(() => {
  void props.route;
  return new URLSearchParams(location.hash.split("?")[1] || "").has("device");
});
const offset = ref(0),
  hasMore = ref(false),
  pendingSources = ref(false),
  selectedDeviceId = ref("");
const chatGroups = computed(() => {
  const result: Array<{ date: string; items: Conversation[] }> = [];
  for (const c of chats.value.filter((c) => c.title.includes(search.value))) {
    const last = result[result.length - 1];
    if (last?.date === c.date) last.items.push(c);
    else result.push({ date: c.date, items: [c] });
  }
  return result;
});
function endingBasis(conversation: Conversation) {
  if (conversation.sourceKind === "business_chat") {
    if (conversation.archiveReason === "explicit_end")
      return "已收到明确结束信号，聊天已归档";
    if (conversation.archiveReason === "inactivity")
      return "轮次结算后30分钟无有效交流，聊天已归档";
  }
  return conversation.endReason;
}
let request = 0;
let inFlight = false;
let unsubscribeSensitive = () => {};
let refreshTimer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  unsubscribeSensitive = onSensitiveInvalidated(() => {
    request++;
    inFlight = false;
    device.value = null;
    chat.value = null;
    devices.value = [];
    chats.value = [];
    if (refreshTimer) clearInterval(refreshTimer);
    refreshTimer = undefined;
  });

  refreshTimer = setInterval(() => {
      if (
        document.visibilityState === "visible" &&
        ["conversations", "conversation", "devices", "overview", "home"].includes(props.route.page) &&
        !inFlight
      )
        void load(true);
  }, 5000);
});
onBeforeUnmount(() => {
  unsubscribeSensitive();
  request++;
  if (refreshTimer) clearInterval(refreshTimer);
});
const core = [
  "overview",
  "devices",
  "device",
  "conversations",
  "conversation",
  "configuration",
  "home",
  "settings",
];
function message(e: unknown) {
  const s = e instanceof Error ? e.message : "";
  return /[\u3400-\u9fff]/.test(s)
    ? s
    : /forbidden|unauthor/i.test(s)
      ? "此聊天正文未授权查看。"
      : /not.found|unknown/i.test(s)
        ? "没有找到这条记录，请检查链接。"
        : "加载失败，请稍后重试。";
}
async function load(silent = false) {
  if (silent && inFlight) return;
  inFlight = true;
  const token = ++request,
    selected = { ...props.route },
    scope = props.scope,
    hasDevice = explicitDevice.value;
  if (!silent) {
    loading.value = true;
    device.value = null;
    chat.value = null;
    devices.value = [];
    chats.value = [];
  }
  try {
    const ds = await api.listDevices(scope);
    if (["overview", "home", "devices"].includes(selected.page)) {
      await Promise.all(ds.map(async d => {
        const runtime = await api.getDeviceRuntime(d.id, scope);
        if (runtime.deviceId !== d.id) throw new Error("设备状态不匹配");
        d.connection = runtime.connection;
        d.lastSeen = runtime.seenAt;
      }));
    }
    const selectedDevice =
      selected.device ||
      (!hasDevice ? (ds[0]?.id ?? "") : selected.device);
    const page = selected.page;
    let d: Device | null = null,
      cs: Conversation[] = [],
      c: Conversation | null = null,
      more = false,
      pending = false;
    if (
      ["device", "settings", "configuration"].includes(page) &&
      (selectedDevice || hasDevice)
    ) {
      d = await api.getDevice(selectedDevice, scope);
      const runtime = await api.getDeviceRuntime(selectedDevice, scope);
      if (runtime.deviceId !== d.id) throw new Error("设备状态不匹配");
      d.connection = runtime.connection;
      d.lastSeen = runtime.seenAt;
      d.volume = runtime.volume;
      d.brightness = runtime.brightness;
    }
    if (["overview", "home", "conversations"].includes(page)) {
      const filter = {
        scope,
        ...(page === "conversations" &&
        ((scope === "family" && selectedDevice !== "") || hasDevice)
          ? { deviceId: selectedDevice }
          : {}),
        offset: page === "conversations" ? offset.value : 0,
        limit: page === "conversations" ? 50 : 5,
      };
      const result = await api.listConversationPage(filter);
      cs = result.items;
      more = result.hasMore;
      pending = "pendingSources" in result && result.pendingSources === true;
    }
    if (page === "conversation")
      c = await api.getConversation(
        selected.chat,
        scope,
        hasDevice ? selected.device : undefined,
      );
    if (token !== request) return;
    error.value = "";
    devices.value = ds;
    selectedDeviceId.value = selectedDevice;
    device.value = d;
    chats.value = cs;
    chat.value = c;
    hasMore.value = more;
    pendingSources.value = pending;
  } catch (e) {
    if (token === request) {
      device.value = null;
      chat.value = null;
      devices.value = [];
      chats.value = [];
      selectedDeviceId.value = "";
      hasMore.value = false;
      pendingSources.value = false;
      error.value = message(e);
    }
  } finally {
    if (token === request) {
      loading.value = false;
      inFlight = false;
    }
  }
}
function changePage(step: number) {
  offset.value = Math.max(0, offset.value + step * 50);
  void load();
}
function connectionLabel(value: string) {
  return { unknown: "未上报", online: "在线", offline: "离线", latest_connection: "最近有连接" }[value] ?? value;
}
function lastSeen(value: string) {
  if (!value) return "未上报";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleString("zh-CN", { timeZone: "Asia/Shanghai" });
}

watch(
  () => props.route,
  () => {
    offset.value = 0;
    load();
  },
  { immediate: true },
);
function deviceLink(id: string) {
  return linkTo(props.scope, "device", { device: id });
}
function selectChatDevice(event: Event) {
  const id = (event.target as HTMLSelectElement).value;
  window.location.href = linkTo(
    props.scope,
    "conversations",
    id ? { device: id } : {},
  );
}
function switchSettingsDevice(event: Event) {
  window.location.href = linkTo(props.scope, props.route.page, {
    device: (event.target as HTMLSelectElement).value,
  });
}
function switchDevice(event: Event) {
  window.location.href = deviceLink((event.target as HTMLSelectElement).value);
}
function chatLink(c: Conversation) {
  return linkTo(props.scope, "conversation", {
    device: c.deviceId,
    chat: c.id,
  });
}
</script>
<template>
  <div v-if="!core.includes(route.page)" class="panel">
    <h1>页面正在准备中</h1>
    <p>请从页面目录选择一个有效页面。</p>
  </div>
  <div v-else-if="loading" class="panel" role="status">
    正在读取记录…
  </div>
  <div v-else-if="error" class="panel error" role="alert">
    <h1>暂时无法显示</h1>
    <p>{{ error }}</p>
    <button @click="load()">重试</button>
  </div>
  <section
    v-else-if="
      !devices.length &&
      ['device', 'settings', 'configuration'].includes(route.page)
    "
    class="panel"
  >
    <h1>暂无可访问设备</h1>
    <p>请联系家庭管理员配置设备访问范围。</p>
  </section>
  <template v-else>
    <template v-if="route.page === 'overview' || route.page === 'home'">
      <div class="heading">
        <div>
          <p class="eyebrow">
            {{ scope === "admin" ? "工作台 / 总览" : "我的家庭" }}
          </p>
          <h1>{{ scope === "admin" ? "管理总览" : "家庭设备" }}</h1>
          <p>
            查看机器人状态，以及绑定后保存的聊天记录
          </p>
        </div>
        <a
          class="button"
          :href="
            linkTo(scope, scope === 'admin' ? 'devices' : 'device', {
              device: selectedDeviceId,
            })
          "
          >查看设备 →</a
        >
      </div>
      <div class="stats">
        <div class="panel">
          <span>设备</span><strong>{{ devices.length }}</strong
          ><small>当前可查看</small>
        </div>
        <div class="panel">
          <span>聊天记录</span><strong>{{ chats.length }}</strong
          ><small>当前已加载</small>
        </div>
        <div class="panel">
          <span>在线设备</span
          ><strong>{{ devices.filter((d) => d.connection === "online").length }}</strong
          ><small>最近上报在线</small>
        </div>
      </div>
      <div class="columns">
        <section class="panel">
          <div class="section-head">
            <h2>设备概况</h2>
            <a
              :href="
                linkTo(scope, scope === 'admin' ? 'devices' : 'device', {
                  device: selectedDeviceId,
                })
              "
              >查看全部 →</a
            >
          </div>
          <div v-if="!devices.length" class="empty">暂无设备</div>
          <a
            v-for="d in devices"
            :key="d.id"
            class="list-row"
            :href="deviceLink(d.id)"
            ><span class="device-icon"><UiIcon name="bot" /></span
            ><span
              ><b>{{ d.name }}</b
              ><small>{{ d.location }} · {{ d.model }}</small></span
            ><span class="badge" :data-status="d.connection">{{ connectionLabel(d.connection) }}</span></a
          >
        </section>
        <section class="panel">
          <div class="section-head">
            <h2>最近聊天</h2>
            <a
              :href="
                linkTo(scope, 'conversations', { device: selectedDeviceId })
              "
              >查看记录 →</a
            >
          </div>
          <div v-if="!chats.length" class="empty">
            暂无绑定后的聊天记录；机器人连接并说话后可刷新查看。
          </div>
          <a
            v-for="c in chats.slice(0, 4)"
            :key="c.id"
            class="list-row"
            :href="chatLink(c)"
            ><span
              ><b>{{ c.title }}</b
              ><small>{{ c.date }} {{ c.time }} · {{ c.status }}</small></span
            ><span class="row-arrow"><UiIcon name="chevron-right" /></span></a
          >
        </section>
      </div>
    </template>
    <template v-else-if="route.page === 'devices'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 设备</p>
          <h1>设备管理</h1>
          <p>
            查看当前账号获授权的设备。
          </p>
        </div>
      </div>
      <div class="panel">
        <div v-if="!devices.length" class="empty">暂无设备</div>
        <a
          v-for="d in devices"
          :key="d.id"
          class="list-row"
          :href="deviceLink(d.id)"
          ><span class="device-icon"><UiIcon name="bot" /></span
          ><span
            ><b>{{ d.name }}</b
            ><small>{{ d.id }} · {{ d.model }} · {{ d.location }}</small></span
          ><span class="badge">{{ connectionLabel(d.connection) }}</span
          ><span class="row-arrow"><UiIcon name="chevron-right" /></span></a
        >
      </div></template
    >
    <template v-else-if="route.page === 'device' && device"
      ><div class="heading">
        <div>
          <p class="eyebrow">
            设备 / {{ scope === "admin" ? device.id : device.name }}
          </p>
          <h1>{{ device.name }}</h1>
          <p>{{ device.location }} · {{ device.model }}</p>
        </div>

      </div>
      <div class="columns">
        <section class="panel">
          <h2>设备信息</h2>
          <dl>
            <div>
              <dt>当前状态</dt>
              <dd>
                {{ device.activity === "unknown" ? "未上报" : device.activity }}
              </dd>
            </div>
            <div>
              <dt>最近上报</dt>
              <dd>{{ lastSeen(device.lastSeen) }}</dd>
            </div>

          </dl>
          <a
            class="button"
            :href="
              linkTo(scope, scope === 'admin' ? 'configuration' : 'settings', {
                device: device.id,
              })
            "
            >设备设置</a
          >
        </section>
        <section class="panel">
          <h2>快速访问</h2>
          <a
            class="list-row"
            :href="linkTo(scope, 'conversations', { device: device.id })"
            ><span
              ><b>聊天记录</b
              ><small>查看这台设备的文字记录</small></span
            >›</a
          ><a
            class="list-row"
            :href="linkTo(scope, 'memories', { device: device.id })"
            ><span><b>设备记忆</b><small>查看近期记忆片段</small></span
            >›</a
          ><label for="device-switch">切换设备</label
          ><select id="device-switch" :value="device.id" @change="switchDevice">
            <option v-for="d in devices" :key="d.id" :value="d.id">
              {{ d.name }} · {{ d.location }}
            </option>
          </select>
        </section>
      </div>
      <RuntimeControls :key="device.id" :device-id="device.id" :scope="scope" /></template
    >
    <template v-else-if="route.page === 'conversations'"
      ><div class="heading">
        <div>
          <p class="eyebrow">记录 / 聊天</p>
          <h1>聊天记录</h1>
          <p>
            仅展示本次绑定后的获授权聊天。明确结束后归档；轮次结算后连续30分钟无有效交流则自动归档。
          </p>
        </div>
      </div>
      <div class="panel">
        <div class="section-head">
          <p>每5秒检查当前页面的新记录。</p>
          <button class="chat-refresh" :disabled="loading" @click="load()">
            刷新记录
          </button>
        </div>
        <label for="chat-device">选择设备</label
        ><select
          id="chat-device"
          :value="
            explicitDevice
              ? route.device
              : scope === 'admin'
                ? ''
                : selectedDeviceId
          "
          @change="selectChatDevice"
        >
          <option v-if="scope === 'admin'" value="">全部设备</option>
          <option v-for="d in devices" :key="d.id" :value="d.id">
            {{ d.name }} · {{ d.location }}
          </option></select
        >

        <label for="search">筛选当前页标题</label
          ><input id="search" v-model="search" placeholder="输入聊天标题" />
          <p class="muted">
            当前页按聊天开始日期整理，跨零点不拆分。
          </p>
          <p v-if="pendingSources" role="status">
            有记录仍在写入，列表可能继续更新。
          </p>
          <div
            v-if="!chats.filter((c) => c.title.includes(search)).length"
            class="empty"
          >
            没有匹配的聊天记录
          </div>
          <template v-for="group in chatGroups" :key="group.date"
            ><h2>{{ group.date }}</h2>
            <a
              v-for="c in group.items"
              :key="c.id"
              class="list-row"
              :href="chatLink(c)"
              ><span
                ><b>{{ c.title }}</b
                ><small
                  >{{ c.date }} {{ c.time }} · {{ c.status }} ·
                  {{
                    devices.find((d) => d.id === c.deviceId)?.name || "未知设备"
                  }}</small
                ><small v-if="c.sourceKind === 'business_chat'"
                  >{{ c.turnCount }}轮 · {{ c.connectionCount }}个连接</small
                ><small>{{
                  c.summary ||
                  (c.bodyAvailable ? "点击查看文字与播放记录" : "正文未授权")
                }}</small></span
              ><span class="row-arrow"><UiIcon name="chevron-right" /></span></a
            ></template
          >
      </div></template
    >
    <div v-if="route.page === 'conversations'" class="section-head">
      <button :disabled="offset === 0 || loading" @click="changePage(-1)">
        上一页</button
      ><span>第{{ Math.floor(offset / 50) + 1 }}页</span
      ><button :disabled="!hasMore || loading" @click="changePage(1)">
        下一页
      </button>
    </div>
    <template v-if="route.page === 'conversation' && chat"
      ><div class="heading">
        <div>
          <p class="eyebrow">
            <a :href="linkTo(scope, 'conversations', { device: chat.deviceId })"
              >聊天记录</a
            >
            / {{ chat.date }}
          </p>
          <h1>{{ chat.title }}</h1>
          <p>{{ chat.time }} · {{ chat.status }} · {{ endingBasis(chat) }}</p>
          <p v-if="chat.sourceKind === 'connection_record'">
            这是来源连接记录，按原始连接查看文字和播放证据。
          </p>
          <p v-if="chat.sourceKind === 'business_chat'">
            {{ chat.turnCount }}轮交流 · {{ chat.connectionCount }}个连接<span
              v-if="chat.endedAt"
            >
              · 归档时间：{{ lastSeen(chat.endedAt) }}</span
            >
          </p>
        </div>
      </div>
      <section class="panel">
        <p class="muted">{{ chat.summary }}</p>
        <p v-if="chat.recordStatus === 'pending'" role="status">
          记录仍在更新，请稍后刷新。
        </p>
        <p v-if="chat.status === '待结算'" role="status">
          缺少轮次结算或记录尚未完整，暂不推定聊天已结束。
        </p>
        <details v-if="chat.sources?.length">
          <summary>查看来源连接（{{ chat.sources.length }}）</summary>
          <a
            v-for="source in chat.sources"
            :key="source.recordId"
            class="list-row"
            :href="
              linkTo(scope, 'conversation', {
                device: chat.deviceId,
                chat: source.recordId,
              })
            "
            >{{ lastSeen(source.startedAt) }} · 来源连接 →</a
          >
        </details>

        <div v-if="!chat.turns.length" class="empty">暂无正文</div>
        <div v-for="turn in chat.turns" :key="turn.id" class="turn">
          <div class="bubble user">
            <b>用户 · 最终转写</b>
            <p>{{ turn.user }}</p>
          </div>
          <div class="bubble assistant">
            <b
              >伴伴 ·
              {{
                turn.playback === "played"
                  ? "已播放"
                  : turn.playback === "interrupted"
                    ? "播放中断"
                    : "播放状态未知"
              }}</b
            >
            <p>
              {{
                turn.played ||
                "暂无可靠的已播文字记录"
              }}
            </p>
            <small v-if="turn.textSource === 'output_asr'"
              >这部分文字来自已播音频识别，可能有误差。</small
            >
            <small>记录{{ turn.saved ? "已保存" : "保存状态未知" }}</small>
            <details v-if="turn.generated && turn.generated !== turn.played">
              <summary>查看生成稿</summary>
              <p>{{ turn.generated }}</p>
            </details>
          </div>
        </div>
      </section></template
    >
    <template
      v-else-if="
        (route.page === 'settings' || route.page === 'configuration') && device
      "
      ><div class="heading">
        <div>
          <p class="eyebrow">设备 / 设置</p>
          <h1>{{ scope === "admin" ? "设备配置" : device.name + "的设置" }}</h1>
          <p>
            调整设备音量和亮度，等待设备真实回执。
          </p>
        </div>
      </div>
      <div>
        <section class="panel form">
          <label for="settings-device">选择设备</label>
          <select id="settings-device" :value="device.id" @change="switchSettingsDevice">
            <option v-for="d in devices" :key="d.id" :value="d.id">{{ d.name }} · {{ d.location }}</option>
          </select>
          <p>{{ device.name }} · {{ device.model }}</p>
        </section>
        <RuntimeControls :key="device.id" :device-id="device.id" :scope="scope" />
      </div>
      </template
    >
  </template>
</template>
