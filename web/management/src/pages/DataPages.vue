<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import type {
  Scope,
  PageRoute,
  Device,
  Family,
  Memory,
  Account,
} from "../types.ts";
import { api } from "../api.ts";
import { linkTo } from "../navigation.ts";
import "../data-pages.css";
import { createMemoryClearController } from "../runtime-control.ts";
import { onSensitiveInvalidated } from "../sensitive-session.ts";
const props = defineProps<{ scope: Scope; route: PageRoute }>();
const devices = ref<Device[]>([]),
  families = ref<Family[]>([]),
  family = ref<Family | null>(null),
  memories = ref<Memory[]>([]),
  memory = ref<Memory | null>(null);
const account = ref<Account | null>(null);
const accounts = ref<
    Array<{ name: string; email: string; role: string; access: string }>
  >([]);
const selectedMemoryDeviceId = ref("");
const loading = ref(false),
  error = ref(""),
  feedback = ref(""),
  saving = ref(false);
const email = ref(""), secret = ref("");
let request = 0;
const clearState=reactive({pending:false,feedback:""});
const canClear=ref(false);
const memoryClear=createMemoryClearController(api,clearState,async(isCurrent)=>{
  const token=request,id=selectedMemoryDeviceId.value;
  memories.value=[];memory.value=null;
  try {
    const items=await api.listMemories({deviceId:id,scope:props.scope});
    if(token===request && isCurrent())memories.value=items;
  } catch(e) {
    if(token===request)canClear.value=false;
    throw e;
  }
});
let unsubscribeSensitive=()=>{};
onMounted(()=>{unsubscribeSensitive=onSensitiveInvalidated(()=>{
  request++; memoryClear.invalidate(); canClear.value=false;
  memories.value=[]; memory.value=null; secret.value=""; account.value=null;
  devices.value=[];families.value=[];family.value=null;accounts.value=[];
});});
onBeforeUnmount(() => {
  unsubscribeSensitive();memoryClear.invalidate();
  request++;
});
function message(e: unknown) {
  return e instanceof Error && e.message ? e.message : "操作失败，请稍后重试。";
}
async function load() {
  if (clearState.pending) return;
  const params = new URLSearchParams(location.hash.split("?")[1] || "");
  const token = ++request,
    page = props.route.page,
    scope = props.scope,
    deviceId = props.route.device,
    familyId = props.route.family,
    memoryId = props.route.memory;
  memoryClear.invalidate();canClear.value=false;
  loading.value = true;
  error.value = "";
  feedback.value = "";
  family.value = null;
  memory.value = null;
  memories.value = [];
  families.value = [];
  selectedMemoryDeviceId.value = "";
  try {
    const a = await api.getAccount(scope);
    const ds = page === "login" ? [] : await api.listDevices(scope);
    let fs: Family[] = [],
      f: Family | null = null,
      ms: Memory[] = [],
      m: Memory | null = null;
    let authorizedClear=false;
    let ac: typeof accounts.value = [];
    if (page === "families") fs = await api.listFamilies(scope);
    if (page === "family") {
      let selectedFamily = familyId;
      if (
        !selectedFamily &&
        !new URLSearchParams(location.hash.split("?")[1] || "").has("family")
      ) {
        fs = await api.listFamilies(scope);
        selectedFamily = fs[0]?.id ?? "";
      }
      if (!selectedFamily) throw new Error("当前账号未关联可访问家庭。");
      f = await api.getFamily(selectedFamily, scope);
    }
    if (page === "accounts") ac = await api.listAccounts();
    const memoryDevice =
      !params.has("device") ? (ds[0]?.id ?? "") : deviceId;
    if (
      ["memories", "memory"].includes(page) &&
      (memoryDevice || params.has("device"))
    )
    {
      ms = await api.listMemories({ deviceId: memoryDevice, scope });
      const runtime=await api.getDeviceRuntime(memoryDevice,scope);
      authorizedClear=runtime.deviceId===memoryDevice && runtime.canControl;
    }
    if (
      page === "memory" ||
      (page === "memories" && scope === "admin" && params.has("memory"))
    ) {
      if (memoryDevice || params.has("device")) {
        m = await api.getMemory(memoryId, scope, memoryDevice);
        if (m.deviceId !== memoryDevice)
          throw new Error("这条记忆不属于所选设备。");
      }
    }
    if (token !== request) return;
    devices.value = ds;
    selectedMemoryDeviceId.value = memoryDevice;
    canClear.value=authorizedClear;
    memoryClear.authorize(memoryDevice,scope,authorizedClear);
    account.value = a;
    families.value = fs;
    family.value = f;
    memories.value = ms;
    memory.value = m;
    accounts.value = ac;
    email.value = "";
    secret.value = "";
  } catch (e) {
    if (token === request) {memories.value=[];memory.value=null;canClear.value=false;memoryClear.invalidate();error.value = message(e);}
  } finally {
    if (token === request) loading.value = false;
  }
}
watch(() => props.route, () => {
  // Actual navigation invalidates the old view; a same-view refresh cannot
  // release a clear that may still execute on the server.
  memoryClear.invalidate();
  void load();
}, { immediate: true });
function switchDevice(event: Event) {
  location.href = linkTo(
    props.scope,
    props.route.page === "memory" ? "memories" : props.route.page,
    { device: (event.target as HTMLSelectElement).value },
  );
}
async function perform(action: () => Promise<void>) {
  if (saving.value) return;
  const token = request;
  saving.value = true;
  feedback.value = "";
  try {
    await action();
  } catch (e) {
    if (token === request) feedback.value = message(e);
  } finally {
    saving.value = false;
    window.dispatchEvent(new Event("management-account-changed"));
  }
}
async function login() {
  const scope = props.scope,
    e = email.value,
    s = secret.value;
  await perform(async () => {
    await api.login(scope, e, s);
    if (props.route.page === "login" && props.scope === scope && document.visibilityState === "visible")
      location.href = linkTo(scope, scope === "admin" ? "overview" : "home");
  });
}
async function logout() {
  const scope = props.scope,
    token = request;
  await perform(async () => {
    await api.logout(scope);
    if (token === request) {
      account.value = null;
      feedback.value = "已退出账号。";
    }
  });
}
async function clear() {
  await memoryClear.clear(()=>confirm("清除这台设备的近期记忆？聊天记录会保留，新对话可产生新记忆。"));
  canClear.value=memoryClear.canClear();
  if(!canClear.value){memories.value=[];memory.value=null;}
}
function memoryLink(m: Memory) {
  return linkTo(props.scope, props.scope === "admin" ? "memories" : "memory", {
    device: m.deviceId,
    memory: m.id,
  });
}
function chatLink(m: Memory) {
  return linkTo(props.scope, "conversation", {
    device: m.deviceId,
    chat: m.chatId || m.conversationId,
    ...(m.turnId ? {turn:m.turnId} : {}),
  });
}
function memoryDate(date: string) {
  const value = new Date(date);
  return Number.isNaN(value.getTime())
    ? "时间未知"
    : value.toLocaleString("zh-CN", { timeZone: "Asia/Shanghai" });
}
</script>
<template>
  <div v-if="loading" class="panel" role="status">
    正在读取资料…
  </div>
  <div v-else-if="error" class="panel error" role="alert">
    <h1>暂时无法显示</h1>
    <p>{{ error }}</p>
    <button @click="load">重试</button>
  </div>
  <template v-else>
    <template v-if="route.page === 'login'"
      ><div class="heading">
        <div>
          <p class="eyebrow">账号登录</p>
          <h1>{{ scope === "admin" ? "内部登录" : "家庭登录" }}</h1>
          <p>
            {{ scope === "admin" ? "使用管理账号查看已授权的家庭与设备" : "使用家庭账号查看已绑定的机器人" }}
          </p>
        </div>
      </div>
      <form class="panel form" @submit.prevent="login">
        <label for="login-email">账号</label
        ><input
          id="login-email"
          v-model="email"
          type="text"
          autocomplete="username"
          required
        /><label for="login-secret">密码</label
        ><input
          id="login-secret"
          v-model="secret"
          type="password"
          autocomplete="current-password"
          required
        /><button :disabled="saving">
          {{ saving ? "登录中…" : "登录" }}
        </button>
        <p role="status">{{ feedback }}</p>
        <p v-if="account">
          当前账号：{{ account.email }}
          <button
            type="button"
            class="text-button"
            :disabled="saving"
            @click="logout"
          >
            退出
          </button>
        </p>
      </form></template
    >
    <template v-else-if="route.page === 'families'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 家庭</p>
          <h1>家庭管理</h1>
          <p>
            查看已授权家庭与设备。
          </p>
        </div>
      </div>
      <div class="panel">
        <div v-if="!families.length" class="empty">暂无家庭</div>
        <a
          v-for="f in families"
          :key="f.id"
          class="list-row"
          :href="linkTo(scope, 'family', { family: f.id })"
          ><span
            ><b>{{ f.name }}</b
            ><small
              >{{ f.id }} ·
              已授权家庭</small
            ></span
          >›</a
        >
      </div></template
    >
    <template v-else-if="route.page === 'family' && family"
      ><div class="heading">
        <div>
          <p class="eyebrow">家庭 / 成员</p>
          <h1>{{ family.name }}</h1>
          <p>
            当前账号可访问的家庭与设备。
          </p>
        </div>
      </div>
      <div class="columns">
        <section class="panel">
          <h2>访问范围</h2>
          <p>
            本账号可查看本家庭获授权设备在本次绑定后的新记录。成员邀请与权限编辑尚未接入。
          </p>
          <div
            v-for="member in family.members"
            :key="member.id"
            class="list-row"
          >
            <span
              ><b>{{ member.name }}</b
              ><small
                >{{ member.email }} · {{ member.role }} ·
                {{ member.status }}</small
              ><small>正文访问：{{ member.access }}</small></span
            >
          </div>
          <p role="status">{{ feedback }}</p>

        </section>
        <section class="panel">
          <h2>家庭设备</h2>
          <a
            v-for="d in devices.filter((d) => d.familyId === family?.id)"
            :key="d.id"
            class="list-row"
            :href="linkTo(scope, 'device', { device: d.id })"
            >{{ d.name }} <span class="row-arrow" aria-hidden="true">›</span></a
          >
          <p
            v-if="!devices.some((d) => d.familyId === family?.id)"
            class="empty"
          >
            暂无设备
          </p>

        </section>
      </div>
      </template
    >

    <template v-else-if="route.page === 'accounts'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 账号</p>
          <h1>账号管理</h1>
          <p>
            内部工作角色与聊天正文授权分别管理。
          </p>
        </div>
      </div>
      <section class="panel">
        <div v-for="a in accounts" :key="a.email" class="list-row">
          <span
            ><b>{{ a.name }}</b
            ><small>{{ a.email }}</small></span
          ><span
            >内部角色：{{ a.role }}<small>正文授权：{{ a.access }}</small></span
          >
        </div>
        <div v-if="!accounts.length" class="empty">暂无账号</div>
      </section></template
    >

    <template v-else-if="route.page === 'memories' || route.page === 'memory'"
      ><div class="heading">
        <div>
          <p class="eyebrow">记录 / 记忆</p>
          <h1>{{ route.page === "memory" ? "记忆详情" : "设备记忆" }}</h1>
          <p>
            用于续聊的对话片段，最多包含最近7天、24轮；仅显示本次绑定后的获授权内容。
          </p>
        </div>
      </div>
      <section v-if="!devices.length" class="panel empty">
        暂无可访问设备
      </section>
      <section v-else class="panel">
        <div class="section-head">
          <p>片段可能截断，完整内容请查看来源聊天。长期人物记忆尚未接入。</p>
          <button class="memory-refresh" :disabled="clearState.pending" @click="load">刷新记忆</button>
        </div>
        <label for="memory-device">选择设备</label
        ><select
          id="memory-device"
          :value="selectedMemoryDeviceId"
          @change="switchDevice"
        >
          <option v-for="d in devices" :key="d.id" :value="d.id">
            {{ d.name }} · {{ d.location }}
          </option>
        </select>
        <button v-if="canClear" class="data-danger" :disabled="clearState.pending" @click="clear">{{clearState.pending?'正在确认清除…':'清除本设备近期记忆'}}</button>
        <p role="status">{{clearState.feedback}}</p>
        <div v-if="memory" class="data-detail">
          <h2>记忆详情</h2>
          <h3>用户 · 保存的转写片段</h3>
            <p>{{ memory.user || "未保存用户文字" }}</p>
            <h3>伴伴 · 保存的回复片段</h3>
            <p>{{ memory.assistant || "未保存回复文字" }}</p>
            <small v-if="memory.textSource === 'output_asr'"
              >回复片段来自已播音频识别，可能有误差。</small
            >

          <small>{{ memoryDate(memory.date) }}</small>
          <p><a :href="chatLink(memory)">查看来源聊天 →</a></p>
        </div>
        <template v-if="route.page === 'memories'"
          ><div v-if="!memories.length" class="empty">
            暂无本次绑定后可查看的近期记忆
          </div>
          <a
            v-for="m in memories"
            :key="m.id"
            class="list-row"
            :href="memoryLink(m)"
            ><span
              ><b>{{
                m.text.length > 100
                  ? m.text.slice(0, 100) + "…"
                  : m.text
              }}</b
              ><small>{{ memoryDate(m.date) }}</small></span
            >›</a
          >
          <p role="status">{{ feedback }}</p></template
        ><a
          v-else
          :href="linkTo(scope, 'memories', { device: selectedMemoryDeviceId })"
          >返回记忆列表</a
        >
      </section></template
    >

  </template>
</template>
