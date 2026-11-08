<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type { Scope, PageRoute, Device, Conversation } from "../types.ts";
import { api } from "../api.ts";
import { linkTo } from "../navigation.ts";
const props = defineProps<{ scope: Scope; route: PageRoute }>();
const emit = defineEmits<{ storageChanged: [] }>();
const devices = ref<Device[]>([]),
  chats = ref<Conversation[]>([]),
  device = ref<Device | null>(null),
  chat = ref<Conversation | null>(null);
const loading = ref(false),
  error = ref(""),
  saving = ref(false),
  feedback = ref(""),
  search = ref("");
const displayState = ref("normal");
const explicitDevice = computed(() => {
  void props.route;
  return new URLSearchParams(location.hash.split("?")[1] || "").has("device");
});
const name = ref(""),
  volume = ref(0),
  brightness = ref(0);
let request = 0;
onBeforeUnmount(() => {
  request++;
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
async function load() {
  const token = ++request;
  loading.value = true;
  error.value = "";
  feedback.value = "";
  device.value = null;
  chat.value = null;
  devices.value = [];
  chats.value = [];
  try {
    const page = props.route.page;
    const ds = await api.listDevices(props.scope);
    let d: Device | null = null,
      cs: Conversation[] = [],
      c: Conversation | null = null;
    if (["device", "settings", "configuration"].includes(page))
      d = await api.getDevice(props.route.device, props.scope);
    if (["overview", "home", "conversations"].includes(page))
      cs = await api.listConversations({
        scope: props.scope,
        ...(page === "conversations" &&
        (props.scope === "family" || explicitDevice.value)
          ? { deviceId: props.route.device }
          : {}),
      });
    if (page === "conversation")
      c = await api.getConversation(
        props.route.chat,
        props.scope,
        explicitDevice.value ? props.route.device : undefined,
      );
    if (token !== request) return;
    devices.value = ds;
    device.value = d;
    chats.value = cs;
    chat.value = c;
    if (d) {
      name.value = d.name;
      volume.value = d.volume;
      brightness.value = d.brightness;
    }
  } catch (e) {
    if (token === request) error.value = message(e);
  } finally {
    if (token === request) loading.value = false;
  }
}
watch(
  () => props.route,
  () => {
    displayState.value = "normal";
    load();
  },
  { immediate: true },
);
async function save() {
  if (!device.value) return;
  const id = device.value.id;
  const token = request;
  saving.value = true;
  feedback.value = "";
  if (Array.from(name.value.trim()).length > 12) {
    feedback.value = "名称不能超过 12 个字符。";
    saving.value = false;
    return;
  }
  try {
    const d = await api.updateDevice(
      id,
      {
        name: name.value,
        volume: Number(volume.value),
        brightness: Number(brightness.value),
      },
      props.scope,
    );
    if (token === request) {
      device.value = d;
      devices.value = devices.value.map((item) =>
        item.id === d.id ? d : item,
      );
      feedback.value = "演示设置已保存，不会控制真实设备。";
    }
  } catch (e) {
    if (token === request)
      feedback.value =
        e instanceof Error
          ? `保存失败：${e.message}`
          : "保存失败，请检查输入。";
  } finally {
    saving.value = false;
    emit("storageChanged");
  }
}
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
async function deleteChat() {
  if (
    !chat.value ||
    saving.value ||
    !confirm("删除这段演示聊天及其关联的演示记忆、每日条目？真实记录不受影响。")
  )
    return;
  const current = chat.value,
    token = request,
    scope = props.scope;
  saving.value = true;
  feedback.value = "";
  try {
    await api.deleteConversation(current.id, scope, current.deviceId);
    emit("storageChanged");
    if (token === request)
      location.href = linkTo(scope, "conversations", {
        device: current.deviceId,
      });
  } catch (e) {
    if (token === request) feedback.value = message(e);
  } finally {
    saving.value = false;
  }
}
</script>
<template>
  <div v-if="!core.includes(route.page)" class="panel">
    <h1>页面正在准备中</h1>
    <p>此演示页面将在后续批次提供。</p>
  </div>
  <div v-else-if="loading" class="panel" role="status">正在加载演示数据…</div>
  <div v-else-if="error" class="panel error" role="alert">
    <h1>暂时无法显示</h1>
    <p>{{ error }}</p>
    <button @click="load">重试</button>
  </div>
  <template v-else>
    <template v-if="route.page === 'overview' || route.page === 'home'">
      <div class="heading">
        <div>
          <p class="eyebrow">
            {{ scope === "admin" ? "工作台 / 总览" : "你好，欢迎回来" }}
          </p>
          <h1>{{ scope === "admin" ? "管理总览" : "和家人一起，慢慢长大" }}</h1>
          <p>这里展示虚构的设备和聊天示例。</p>
        </div>
        <a
          class="button"
          :href="
            linkTo(scope, scope === 'admin' ? 'devices' : 'device', {
              device: route.device,
            })
          "
          >查看设备 →</a
        >
      </div>
      <div class="stats">
        <div class="panel">
          <span>设备</span><strong>{{ devices.length }}</strong
          ><small>演示设备</small>
        </div>
        <div class="panel">
          <span>聊天记录</span><strong>{{ chats.length }}</strong
          ><small>示例会话</small>
        </div>
        <div class="panel">
          <span>服务状态</span><strong class="status">演示模式</strong
          ><small>未连接真实服务</small>
        </div>
      </div>
      <div class="columns">
        <section class="panel">
          <div class="section-head">
            <h2>设备概况</h2>
            <a
              :href="
                linkTo(scope, scope === 'admin' ? 'devices' : 'device', {
                  device: route.device,
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
            ><span class="device-icon">◉</span
            ><span
              ><b>{{ d.name }}</b
              ><small>{{ d.location }} · {{ d.model }}</small></span
            ><span class="badge">{{ d.connection }}</span></a
          >
        </section>
        <section class="panel">
          <div class="section-head">
            <h2>最近聊天</h2>
            <a :href="linkTo(scope, 'conversations', { device: route.device })"
              >查看记录 →</a
            >
          </div>
          <div v-if="!chats.length" class="empty">暂无聊天记录</div>
          <a
            v-for="c in chats.slice(0, 4)"
            :key="c.id"
            class="list-row"
            :href="chatLink(c)"
            ><span
              ><b>{{ c.title }}</b
              ><small>{{ c.date }} {{ c.time }} · {{ c.status }}</small></span
            ><span>›</span></a
          >
        </section>
      </div>
    </template>
    <template v-else-if="route.page === 'devices'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 设备</p>
          <h1>设备管理</h1>
          <p>查看设备与家庭的示例关联。</p>
        </div>
      </div>
      <div class="panel">
        <div v-if="!devices.length" class="empty">暂无设备</div>
        <a
          v-for="d in devices"
          :key="d.id"
          class="list-row"
          :href="deviceLink(d.id)"
          ><span class="device-icon">◉</span
          ><span
            ><b>{{ d.name }}</b
            ><small>{{ d.id }} · {{ d.model }} · {{ d.location }}</small></span
          ><span class="badge">{{ d.connection }}</span
          ><span>›</span></a
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
        <span class="badge">{{ device.connection }}</span>
      </div>
      <div class="columns">
        <section class="panel">
          <h2>设备信息</h2>
          <dl>
            <div>
              <dt>当前状态</dt>
              <dd>{{ device.activity }}</dd>
            </div>
            <div>
              <dt>最近上报</dt>
              <dd>{{ device.lastSeen }}</dd>
            </div>
            <div>
              <dt>音量</dt>
              <dd>{{ device.volume }}%</dd>
            </div>
            <div>
              <dt>屏幕亮度</dt>
              <dd>{{ device.brightness }}%</dd>
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
            ><span><b>聊天记录</b><small>查看这台设备的示例聊天</small></span
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
      </div></template
    >
    <template v-else-if="route.page === 'conversations'"
      ><div class="heading">
        <div>
          <p class="eyebrow">记录 / 聊天</p>
          <h1>聊天记录</h1>
          <p>按日期查看示例会话；实际归档功能待开发。</p>
        </div>
      </div>
      <div class="panel">
        <label for="chat-device">选择设备</label
        ><select
          id="chat-device"
          :value="
            explicitDevice
              ? route.device
              : scope === 'admin'
                ? ''
                : route.device
          "
          @change="selectChatDevice"
        >
          <option v-if="scope === 'admin'" value="">全部设备</option>
          <option v-for="d in devices" :key="d.id" :value="d.id">
            {{ d.name }} · {{ d.location }}
          </option></select
        ><label for="chat-state">演示显示状态</label
        ><select id="chat-state" v-model="displayState">
          <option value="normal">正常</option>
          <option value="loading">加载中</option>
          <option value="empty">空记录</option>
          <option value="error">加载失败</option>
          <option value="unauthorized">未授权</option>
        </select>
        <p v-if="displayState !== 'normal'" role="status">
          {{
            {
              loading: "正在加载演示数据…",
              empty: "暂无聊天记录",
              error: "加载失败，请稍后重试。",
              unauthorized: "此聊天正文未授权查看。",
            }[displayState as "loading" | "empty" | "error" | "unauthorized"]
          }}
        </p>
        <template v-if="displayState === 'normal'"
          ><label for="search">搜索标题</label
          ><input id="search" v-model="search" placeholder="输入聊天标题" />
          <div
            v-if="!chats.filter((c) => c.title.includes(search)).length"
            class="empty"
          >
            没有匹配的聊天记录
          </div>
          <a
            v-for="c in chats.filter((c) => c.title.includes(search))"
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
              ><small>{{ c.summary || "正文不可用" }}</small></span
            ><span>›</span></a
          ></template
        >
      </div></template
    >
    <template v-else-if="route.page === 'conversation' && chat"
      ><div class="heading">
        <div>
          <p class="eyebrow">
            <a :href="linkTo(scope, 'conversations', { device: chat.deviceId })"
              >聊天记录</a
            >
            / {{ chat.date }}
          </p>
          <h1>{{ chat.title }}</h1>
          <p>{{ chat.time }} · {{ chat.status }} · {{ chat.endReason }}</p>
        </div>
      </div>
      <section class="panel">
        <p class="muted">{{ chat.summary }}</p>
        <button class="danger-button" :disabled="saving" @click="deleteChat">
          删除这段演示聊天
        </button>
        <p role="status">{{ feedback }}</p>
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
            <p>{{ turn.played || "暂无已播正文" }}</p>
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
          <p>只修改当前浏览器中的示例数据。</p>
        </div>
      </div>
      <form class="panel form" @submit.prevent="save">
        <label for="settings-device">选择设备</label
        ><select
          id="settings-device"
          :value="device.id"
          @change="switchSettingsDevice"
        >
          <option v-for="d in devices" :key="d.id" :value="d.id">
            {{ d.name }} · {{ d.location }}
          </option></select
        ><label for="name">设备名称</label
        ><input id="name" v-model="name" required /><small class="muted"
          >最多 12 个 Unicode
          字符。名称用于新会话；唤醒词独立设置，不随名称改变。</small
        ><label for="volume">音量：{{ volume }}%</label
        ><input
          id="volume"
          v-model.number="volume"
          type="range"
          min="0"
          max="100"
        /><label for="brightness">屏幕亮度：{{ brightness }}%</label
        ><input
          id="brightness"
          v-model.number="brightness"
          type="range"
          min="0"
          max="100"
        /><button :disabled="saving">
          {{ saving ? "保存中…" : "保存设置" }}
        </button>
        <p role="status">{{ feedback }}</p>
        <a
          v-if="scope === 'family'"
          class="list-row"
          :href="linkTo(scope, 'privacy', { device: device.id })"
          >数据与隐私 →</a
        ><a
          v-if="scope === 'family'"
          class="list-row"
          :href="
            linkTo(scope, 'family', { device: device.id, family: route.family })
          "
          >家庭与成员 →</a
        >
      </form></template
    >
  </template>
</template>
