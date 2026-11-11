<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import type {
  Scope,
  PageRoute,
  Device,
  Family,
  Memory,
  Privacy,
  Account,
  Conversation,
} from "../types.ts";
import { api, liveMode } from "../api.ts";
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
const privacy = ref<Privacy | null>(null),
  account = ref<Account | null>(null);
const accounts = ref<
    Array<{ name: string; email: string; role: string; access: string }>
  >([]),
  daily = ref<Array<{ date: string; conversations: Conversation[] }>>([]);
const boundDevice = ref<Device | null>(null);
const selectedMemoryDeviceId = ref("");
const loading = ref(false),
  error = ref(""),
  feedback = ref(""),
  saving = ref(false);
const email = ref(""),
  secret = ref(""),
  inviteEmail = ref(""),
  role = ref("家庭成员"),
  allowHistory = ref(false),
  code = ref(""),
  deviceName = ref(""),
  model = ref("微雪 ESP32-S3"),
  joinCode = ref("");
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
  devices.value=[];families.value=[];family.value=null;accounts.value=[];daily.value=[];
});});
onBeforeUnmount(() => {
  unsubscribeSensitive();memoryClear.invalidate();
  request++;
});
function message(e: unknown) {
  return e instanceof Error && e.message ? e.message : "操作失败，请稍后重试。";
}
function changed() {
  window.dispatchEvent(new Event("demo-storage-changed"));
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
  boundDevice.value = null;
  family.value = null;
  memory.value = null;
  privacy.value = null;
  memories.value = [];
  families.value = [];
  daily.value = [];
  selectedMemoryDeviceId.value = "";
  try {
    const a = await api.getAccount(scope);
    const ds = page === "login" ? [] : await api.listDevices(scope);
    let fs: Family[] = [],
      f: Family | null = null,
      ms: Memory[] = [],
      m: Memory | null = null,
      p: Privacy | null = null;
    let authorizedClear=false;
    let ac: typeof accounts.value = [],
      days: typeof daily.value = [];
    if (page === "families") fs = await api.listFamilies(scope);
    if (["family", "invite", "bind"].includes(page)) {
      let selectedFamily = familyId;
      if (
        liveMode &&
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
      liveMode && !params.has("device") ? (ds[0]?.id ?? "") : deviceId;
    if (
      ["memories", "memory"].includes(page) &&
      (!liveMode || memoryDevice || params.has("device"))
    )
    {
      ms = await api.listMemories({ deviceId: memoryDevice, scope });
      if(liveMode){
        const runtime=await api.getDeviceRuntime(memoryDevice,scope);
        authorizedClear=runtime.deviceId===memoryDevice && runtime.canControl;
      }
    }
    if (
      page === "memory" ||
      (page === "memories" && scope === "admin" && params.has("memory"))
    ) {
      if (!liveMode || memoryDevice || params.has("device")) {
        m = await api.getMemory(memoryId, scope, memoryDevice);
        if (m.deviceId !== memoryDevice)
          throw new Error("这条记忆不属于所选设备。");
      }
    }
    if (["privacy", "policies"].includes(page))
      p = await api.getPrivacy(deviceId, scope);
    if (page === "daily") days = await api.getDailySummaries(scope, deviceId);
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
    privacy.value = p;
    accounts.value = ac;
    daily.value = days;
    email.value = liveMode
      ? ""
      : scope === "admin"
        ? "admin@example.test"
        : "family@example.test";
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
    changed();
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
      feedback.value = liveMode ? "已退出账号。" : "已退出演示账号。";
    }
  });
}
async function invite() {
  const id = props.route.family,
    scope = props.scope,
    token = request,
    value = {
      email: inviteEmail.value,
      role: role.value,
      allowHistory: allowHistory.value,
    };
  await perform(async () => {
    const member = await api.inviteMember(id, value, scope);
    if (token === request) {
      feedback.value = `已添加待加入成员：${member.email}。演示不会发送邮件。`;
      inviteEmail.value = "";
    }
  });
}
async function join() {
  const token = request,
    value = joinCode.value;
  await perform(async () => {
    const f = await api.joinFamily(value);
    if (token === request)
      location.href = linkTo("family", "family", { family: f.id });
  });
}
async function bind() {
  const familyId = props.route.family,
    scope = props.scope,
    token = request,
    value = {
      familyId,
      code: code.value,
      name: deviceName.value,
      model: model.value,
    };
  await perform(async () => {
    const d = await api.bindDevice(value, scope);
    if (token === request) {
      feedback.value = `已绑定演示设备：${d.name}。`;
      const updatedDevices = await api.listDevices(scope);
      if (token === request) {
        devices.value = updatedDevices;
        boundDevice.value = d;
      }
    }
  });
}
async function access(memberId: string, value: string) {
  const id = props.route.family,
    scope = props.scope,
    token = request;
  await perform(async () => {
    const f = await api.updateMemberAccess(id, memberId, value, scope);
    if (token === request) {
      family.value = f;
      feedback.value = "演示权限已更新。";
    }
  });
}
async function savePrivacy() {
  if (!privacy.value) return;
  const id = props.route.device,
    scope = props.scope,
    token = request,
    value = { ...privacy.value };
  await perform(async () => {
    const p = await api.updatePrivacy(id, value, scope);
    if (token === request) {
      privacy.value = p;
      feedback.value = "演示偏好已保存；实际服务策略不受影响。";
    }
  });
}
async function clear() {
  if (liveMode) {
    await memoryClear.clear(()=>confirm("清除这台设备的近期记忆？聊天记录会保留，新对话可产生新记忆。"));
    canClear.value=memoryClear.canClear();
    if(!canClear.value){memories.value=[];memory.value=null;}
    return;
  }
  const id = props.route.device,
    scope = props.scope,
    token = request;
  if (!confirm("清除这台设备的全部演示记忆？聊天记录会保留。")) return;
  await perform(async () => {
    await api.clearMemories(id, scope);
    if (token === request) {
      memories.value = [];
      memory.value = null;
      feedback.value = "演示记忆已清除，聊天记录保留。";
    }
  });
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
  if (!liveMode) return date;
  const value = new Date(date);
  return Number.isNaN(value.getTime())
    ? "时间未知"
    : value.toLocaleString("zh-CN", { timeZone: "Asia/Shanghai" });
}
</script>
<template>
  <div v-if="loading" class="panel" role="status">
    {{ liveMode ? "正在读取资料…" : "正在加载演示数据…" }}
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
          <p class="eyebrow">{{ liveMode ? "账号登录" : "演示账号" }}</p>
          <h1>{{ scope === "admin" ? "内部登录" : "家庭登录" }}</h1>
          <p>
            {{
              liveMode
                ? "请使用为你配置的管理平台账号。"
                : "仅用于前端演示，请勿输入真实凭据。"
            }}
          </p>
        </div>
      </div>
      <form class="panel form" @submit.prevent="login">
        <label for="login-email">{{ liveMode ? "账号" : "邮箱" }}</label
        ><input
          id="login-email"
          v-model="email"
          :type="liveMode ? 'text' : 'email'"
          :pattern="liveMode ? undefined : '[^@\\s]+@example\\.test'"
          autocomplete="username"
          required
        /><small v-if="!liveMode">仅接受 @example.test 示例邮箱。</small
        ><label for="login-secret">{{
          liveMode ? "密码" : scope === "admin" ? "演示密码" : "6 位演示验证码"
        }}</label
        ><input
          id="login-secret"
          v-model="secret"
          :type="liveMode || scope === 'admin' ? 'password' : 'text'"
          autocomplete="current-password"
          required
          :inputmode="!liveMode && scope === 'family' ? 'numeric' : undefined"
        /><small v-if="!liveMode">不发送真实邮件或短信。</small
        ><button :disabled="saving">
          {{ saving ? "登录中…" : liveMode ? "登录" : "登录演示" }}
        </button>
        <p role="status">{{ feedback }}</p>
        <p v-if="account">
          {{ liveMode ? "当前账号：" : "当前演示账号：" }}{{ account.email }}
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
            {{
              liveMode ? "查看已授权家庭与设备。" : "查看虚构家庭及成员信息。"
            }}
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
              {{
                liveMode ? "已授权家庭" : f.members.length + " 位成员"
              }}</small
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
            {{
              liveMode
                ? "当前账号可访问的家庭与设备。"
                : "以下为本浏览器中的演示数据。"
            }}
          </p>
        </div>
      </div>
      <div class="columns">
        <section class="panel">
          <h2>{{ liveMode ? "访问范围" : "家庭成员" }}</h2>
          <p v-if="liveMode">
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
            ><select
              v-if="scope === 'admin' && !liveMode"
              :value="member.access"
              :disabled="saving"
              aria-label="正文访问权限"
              @change="
                access(member.id, ($event.target as HTMLSelectElement).value)
              "
            >
              <option
                v-if="
                  !['未授权正文', '客厅机器人', '本家庭设备'].includes(
                    member.access,
                  )
                "
                :value="member.access"
                disabled
              >
                {{ member.access }}
              </option>
              <option>未授权正文</option>
              <option
                v-if="
                  devices.some(
                    (d) => d.familyId === family?.id && d.location === '客厅',
                  )
                "
              >
                客厅机器人
              </option>
              <option>本家庭设备</option>
            </select>
          </div>
          <p role="status">{{ feedback }}</p>
          <a
            v-if="scope === 'family' && !liveMode"
            class="button"
            :href="linkTo(scope, 'invite', { family: family.id })"
            >邀请成员</a
          >
        </section>
        <section class="panel">
          <h2>家庭设备</h2>
          <a
            v-for="d in devices.filter((d) => d.familyId === family?.id)"
            :key="d.id"
            class="list-row"
            :href="linkTo(scope, 'device', { device: d.id })"
            >{{ d.name }} <span>›</span></a
          >
          <p
            v-if="!devices.some((d) => d.familyId === family?.id)"
            class="empty"
          >
            暂无设备
          </p>
          <a
            v-if="scope === 'family' && !liveMode"
            :href="linkTo(scope, 'bind', { family: family.id })"
            >绑定设备 →</a
          >
        </section>
      </div>
      <form
        v-if="scope === 'family' && !liveMode"
        class="panel form data-extra"
        @submit.prevent="join"
      >
        <h2>加入家庭</h2>
        <label for="join-code">6 位演示邀请码</label
        ><input
          id="join-code"
          v-model="joinCode"
          inputmode="numeric"
          required
        /><button :disabled="saving">加入演示家庭</button>
        <p role="status">{{ feedback }}</p>
      </form></template
    >
    <template v-else-if="route.page === 'invite' && family"
      ><div class="heading">
        <div>
          <p class="eyebrow">
            <a :href="linkTo(scope, 'family', { family: family.id })">{{
              family.name
            }}</a>
            / 邀请
          </p>
          <h1>邀请家庭成员</h1>
          <p>添加待加入的演示成员，不会发送真实邮件。</p>
        </div>
      </div>
      <form class="panel form" @submit.prevent="invite">
        <label for="invite-email">邮箱</label
        ><input
          id="invite-email"
          v-model="inviteEmail"
          type="email"
          pattern="[^@\s]+@example\.test"
          required
          placeholder="member@example.test"
        /><label for="invite-role">角色</label
        ><select id="invite-role" v-model="role">
          <option>家庭成员</option>
          <option>家庭管理员</option></select
        ><label class="check"
          ><input
            v-model="allowHistory"
            type="checkbox"
          />允许查看历史聊天正文</label
        ><button :disabled="saving">添加邀请</button>
        <p role="status">{{ feedback }}</p>
      </form></template
    >
    <template v-else-if="route.page === 'bind' && family"
      ><div class="heading">
        <div>
          <p class="eyebrow">{{ family.name }} / 设备</p>
          <h1>绑定设备</h1>
          <p>使用 6 位演示码添加虚构设备；已使用的演示码不能重复绑定。</p>
        </div>
      </div>
      <form class="panel form" @submit.prevent="bind">
        <label for="bind-code">演示绑定码</label
        ><input
          id="bind-code"
          v-model="code"
          inputmode="numeric"
          required
        /><label for="bind-name">设备名称</label
        ><input id="bind-name" v-model="deviceName" required /><small
          >最多 12 个字符。</small
        ><label for="bind-model">型号</label
        ><select id="bind-model" v-model="model">
          <option>微雪 ESP32-S3</option>
          <option>M5 ESP32-S3</option></select
        ><button :disabled="saving">绑定演示设备</button>
        <p role="status">{{ feedback }}</p>
        <a
          v-if="boundDevice"
          :href="linkTo(scope, 'device', { device: boundDevice.id })"
          >查看 {{ boundDevice.name }} →</a
        >
      </form></template
    >
    <template v-else-if="route.page === 'accounts'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 账号</p>
          <h1>账号管理</h1>
          <p>
            {{
              liveMode
                ? "内部工作角色与聊天正文授权分别管理。"
                : "内部角色与聊天正文授权分别展示，均为演示。"
            }}
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
    <template v-else-if="route.page === 'privacy' || route.page === 'policies'"
      ><div class="heading">
        <div>
          <p class="eyebrow">设备 / 隐私</p>
          <h1>{{ route.page === "policies" ? "权限策略" : "隐私设置" }}</h1>
          <p>演示偏好仅保存在当前浏览器，不改变实际服务策略。</p>
        </div>
      </div>
      <section v-if="route.page === 'policies'" class="panel data-extra">
        <h2>聊天会话规则</h2>
        <p>规则已确认，业务执行与归档尚未接入。</p>
        <dl>
          <div>
            <dt>用户明确结束</dt>
            <dd>优先结束当前聊天</dd>
          </div>
          <div>
            <dt>30分钟无有效交互</dt>
            <dd>从最后一轮有效交流结束计时，生成和播放中不误结束</dd>
          </div>
          <div>
            <dt>按天汇总</dt>
            <dd>一天可包含多段聊天，跨零点不强制切分</dd>
          </div>
        </dl>
        <p>聊天档案与模型近期记忆分别管理，原始录音默认不保存。</p>
      </section>
      <form
        v-if="privacy"
        class="panel form data-extra"
        @submit.prevent="savePrivacy"
      >
        <label for="privacy-device">选择设备</label
        ><select
          id="privacy-device"
          :value="route.device"
          @change="switchDevice"
        >
          <option v-for="d in devices" :key="d.id" :value="d.id">
            {{ d.name }} · {{ d.location }}
          </option></select
        ><label class="check"
          ><input
            v-model="privacy.keepRecords"
            type="checkbox"
            @change="!privacy.keepRecords && (privacy.useMemory = false)"
          />保留聊天记录</label
        ><label class="check"
          ><input
            v-model="privacy.useMemory"
            type="checkbox"
            :disabled="!privacy.keepRecords"
          />使用记忆</label
        ><small>使用记忆需要保留聊天记录。</small
        ><label for="retention">建议保留期限</label
        ><select id="retention" v-model="privacy.retention">
          <option value="7">7 天</option>
          <option value="30">30 天（建议）</option>
          <option value="90">90 天</option></select
        ><small>30 天只是方案建议，并非当前服务器实际策略。</small
        ><button :disabled="saving">保存演示偏好</button>
        <p role="status">{{ feedback }}</p>
      </form></template
    >
    <template v-else-if="route.page === 'memories' || route.page === 'memory'"
      ><div class="heading">
        <div>
          <p class="eyebrow">记录 / 记忆</p>
          <h1>{{ route.page === "memory" ? "记忆详情" : "设备记忆" }}</h1>
          <p>
            {{
              liveMode
                ? "机器人保存的近期上下文片段：最近7天、最多24轮；仅显示本次绑定后的获授权内容。"
                : "设备近期记忆示例：最近7天、最多24轮。人物长期记忆仍待独立验收。"
            }}
          </p>
        </div>
      </div>
      <section v-if="liveMode && !devices.length" class="panel empty">
        暂无可访问设备
      </section>
      <section v-else class="panel">
        <div v-if="liveMode" class="section-head">
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
        <button v-if="liveMode && canClear" class="data-danger" :disabled="clearState.pending" @click="clear">{{clearState.pending?'正在确认清除…':'清除本设备近期记忆'}}</button>
        <p v-if="liveMode" role="status">{{clearState.feedback}}</p>
        <div v-if="memory" class="data-detail">
          <h2>记忆详情</h2>
          <template v-if="liveMode"
            ><h3>用户 · 保存的转写片段</h3>
            <p>{{ memory.user || "未保存用户文字" }}</p>
            <h3>伴伴 · 保存的回复片段</h3>
            <p>{{ memory.assistant || "未保存回复文字" }}</p>
            <small v-if="memory.textSource === 'output_asr'"
              >回复片段来自已播音频识别，可能有误差。</small
            ></template
          >
          <p v-else>{{ memory.text }}</p>
          <small>{{ memoryDate(memory.date) }}</small>
          <p><a :href="chatLink(memory)">查看来源聊天 →</a></p>
        </div>
        <template v-if="route.page === 'memories'"
          ><div v-if="!memories.length" class="empty">
            {{ liveMode ? "暂无本次绑定后可查看的近期记忆" : "暂无记忆" }}
          </div>
          <a
            v-for="m in memories"
            :key="m.id"
            class="list-row"
            :href="memoryLink(m)"
            ><span
              ><b>{{
                liveMode && m.text.length > 100
                  ? m.text.slice(0, 100) + "…"
                  : m.text
              }}</b
              ><small>{{ memoryDate(m.date) }}</small></span
            >›</a
          ><button
            v-if="!liveMode"
            class="data-danger"
            :disabled="saving || !memories.length"
            @click="clear"
          >
            清除本设备记忆
          </button>
          <p role="status">{{ feedback }}</p></template
        ><a
          v-else
          :href="linkTo(scope, 'memories', { device: selectedMemoryDeviceId })"
          >返回记忆列表</a
        >
      </section></template
    >
    <template v-else-if="route.page === 'daily'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 记录</p>
          <h1>每日概览</h1>
          <p>按日期查看所选设备的演示聊天摘要。</p>
        </div>
      </div>
      <section class="panel">
        <label for="daily-device">选择设备</label
        ><select id="daily-device" :value="route.device" @change="switchDevice">
          <option v-for="d in devices" :key="d.id" :value="d.id">
            {{ d.name }} · {{ d.location }}
          </option>
        </select>
        <div v-if="!daily.length" class="empty">暂无每日记录</div>
        <div v-for="day in daily" :key="day.date">
          <h2>{{ day.date }}</h2>
          <a
            v-for="c in day.conversations"
            :key="c.id"
            class="list-row"
            :href="
              linkTo(scope, 'conversation', { device: c.deviceId, chat: c.id })
            "
            ><span
              ><b>{{ c.title }}</b
              ><small>{{ c.time }} · {{ c.summary || "暂无摘要" }}</small></span
            >›</a
          >
        </div>
      </section></template
    >
  </template>
</template>
