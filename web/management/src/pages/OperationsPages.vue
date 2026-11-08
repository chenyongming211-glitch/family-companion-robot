<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type {
  Scope,
  PageRoute,
  Capability,
  Task,
  Firmware,
  AuditEvent,
} from "../types.ts";
import { api } from "../api.ts";
import FacePreview from "../components/FacePreview.vue";
import "../operations-pages.css";
const props = defineProps<{ scope: Scope; route: PageRoute }>();
const capabilities = ref<Capability[]>([]),
  expressions = ref<Array<{ id: string; name: string; description: string }>>(
    [],
  ),
  tasks = ref<Task[]>([]),
  firmware = ref<Firmware[]>([]),
  audit = ref<AuditEvent[]>([]);
const operations = ref<{
  metrics: Array<{ label: string; value: string; description: string }>;
  events: Array<{ time: string; label: string; status: string }>;
}>({ metrics: [], events: [] });
const loading = ref(false),
  error = ref(""),
  stage = ref("全部"),
  selected = ref(""),
  query = ref(""),
  emotion = ref("neutral"),
  layer = ref("L1"),
  state = ref("正在聆听"),
  speaking = ref(false);
const stages = computed(() => [
  ...new Set(capabilities.value.map((c) => c.stage)),
]);
const filtered = computed(() =>
  capabilities.value.filter(
    (c) => stage.value === "全部" || c.stage === stage.value,
  ),
);
let request = 0;
onBeforeUnmount(() => {
  request++;
});
async function load() {
  const token = ++request,
    page = props.route.page,
    search = query.value;
  loading.value = true;
  error.value = "";
  selected.value = "";
  try {
    if (page === "capabilities") {
      const v = await api.listCapabilities();
      if (token === request) capabilities.value = v;
    }
    if (page === "expressions") {
      const v = await api.listExpressions();
      if (token === request) expressions.value = v;
    }
    if (page === "tasks") {
      const v = await api.listTasks();
      if (token === request) tasks.value = v;
    }
    if (page === "firmware") {
      const v = await api.listFirmware();
      if (token === request) firmware.value = v;
    }
    if (page === "operations") {
      const v = await api.getOperations();
      if (token === request) operations.value = v;
    }
    if (page === "audit") {
      const v = await api.listAudit(search);
      if (token === request) audit.value = v;
    }
  } catch (e) {
    if (token === request)
      error.value = e instanceof Error ? e.message : "加载失败，请重试。";
  } finally {
    if (token === request) loading.value = false;
  }
}
watch(() => props.route, load, { immediate: true });
</script>
<template>
  <div v-if="loading" class="panel" role="status">正在加载演示数据…</div>
  <div v-else-if="error" class="panel error" role="alert">
    <h1>暂时无法显示</h1>
    <p>{{ error }}</p>
    <button @click="load">重试</button>
  </div>
  <template v-else>
    <template v-if="route.page === 'capabilities'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 能力</p>
          <h1>能力管理</h1>
          <p>查看能力阶段与范围；演示数据不代表实际运行状态。</p>
        </div>
      </div>
      <section class="panel">
        <div class="ops-controls">
          <label for="cap-stage">能力阶段</label
          ><select id="cap-stage" v-model="stage">
            <option>全部</option>
            <option v-for="s in stages" :key="s">{{ s }}</option>
          </select>
        </div>
        <div v-if="!filtered.length" class="empty">该阶段暂无示例能力</div>
        <div v-for="c in filtered" :key="c.id" class="list-row">
          <span
            ><b>{{ c.name }}</b
            ><small>{{ c.stage }} · {{ c.description }}</small
            ><span v-if="selected === c.id" class="ops-detail"
              ><b>能力详情</b><small>{{ c.detail }}</small></span
            ></span
          ><button
            type="button"
            :aria-expanded="selected === c.id"
            @click="selected = selected === c.id ? '' : c.id"
          >
            {{ selected === c.id ? "收起" : "查看详情" }}
          </button>
        </div>
      </section></template
    >
    <template v-else-if="route.page === 'expressions'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 表情</p>
          <h1>表情预览</h1>
          <p>
            示意预览，不调用模型、音频或设备。L0 是状态文字，L1 是情绪，L2
            是模拟嘴型。
          </p>
        </div>
      </div>
      <div class="columns">
        <section class="panel">
          <h2>选择预览层</h2>
          <div class="ops-options">
            <button
              v-for="l in ['L0', 'L1', 'L2']"
              :key="l"
              :aria-pressed="layer === l"
              @click="layer = l"
            >
              {{ l }}
            </button>
          </div>
          <template v-if="layer === 'L0'"
            ><label for="face-state">状态</label
            ><select id="face-state" v-model="state">
              <option>正在聆听</option>
              <option>正在思考</option>
              <option>正在说话</option>
              <option>等待连接</option>
              <option>待命</option>
              <option>睡眠</option>
              <option>连接错误</option>
            </select>
            <p>思考属于 L0 状态，不是第 16 种情绪。</p></template
          ><template v-else
            ><h2>15 种情绪</h2>
            <div class="ops-options">
              <button
                v-for="e in expressions"
                :key="e.id"
                :aria-pressed="emotion === e.id"
                @click="emotion = e.id"
              >
                {{ e.name }}
              </button>
            </div>
            <p>{{ expressions.find((e) => e.id === emotion)?.description }}</p>
            <label v-if="layer === 'L2'" class="check"
              ><input v-model="speaking" type="checkbox" />模拟说话嘴型</label
            ></template
          >
        </section>
        <section class="panel ops-face">
          <h2>预览</h2>
          <FacePreview
            :emotion="layer === 'L0' ? 'neutral' : emotion"
            :speaking="layer === 'L2' && speaking"
          />
          <p v-if="layer === 'L0'">{{ state }}</p>
          <small>示意图，与实机表现可能不同。</small>
        </section>
      </div></template
    >
    <template v-else-if="route.page === 'voiceprints'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 身份</p>
          <h1>声纹验收</h1>
          <p>当前家庭成员识别仍待真实机器人验收。</p>
        </div>
      </div>
      <div class="ops-grid">
        <section class="panel">
          <h2>采集来源</h2>
          <p>
            使用机器人麦克风采集注册语音，每人约 20
            秒有效语音，再用新采集的机器人语音查询。
          </p>
          <p>手机注册、机器人查询作为单独对照，不混入主验收。</p>
        </section>
        <section class="panel">
          <h2>质量与验收条件</h2>
          <p>
            五位家庭成员每人至少 19/20 次正确接受，成员间误认须为零；20
            位陌生人各查询 10 次，误接受须为 0/200。
          </p>
          <p>采集质量与实际结果尚待核验；此页面不展示虚构识别分数。</p>
        </section>
      </div></template
    >
    <template v-else-if="route.page === 'tasks'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 编排</p>
          <h1>任务与编排</h1>
          <p>任务示例仅展示阶段与预期，不触发实际执行。Eino 仍是候选方案。</p>
        </div>
      </div>
      <section class="panel">
        <div v-if="!tasks.length" class="empty">暂无任务示例</div>
        <div v-for="t in tasks" :key="t.id" class="list-row">
          <span
            ><b>{{ t.name }}</b
            ><small>{{ t.stage }}</small
            ><span v-if="selected === t.id" class="ops-detail"
              ><b>触发条件</b><small>{{ t.trigger }}</small
              ><b>预期输出与范围</b><small>{{ t.detail }}</small></span
            ></span
          ><button
            :aria-expanded="selected === t.id"
            @click="selected = selected === t.id ? '' : t.id"
          >
            {{ selected === t.id ? "收起" : "查看详情" }}
          </button>
        </div>
      </section></template
    >
    <template v-else-if="route.page === 'firmware'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 设备</p>
          <h1>固件能力</h1>
          <p>当前支持微雪和 M5 的 ESP32-S3；页面不执行刷机。</p>
        </div>
      </div>
      <div class="ops-grid">
        <section v-for="f in firmware" :key="f.model" class="panel">
          <h2>{{ f.model }}</h2>
          <p>{{ f.stage }}</p>
          <h3>能力范围</h3>
          <p>{{ f.features }}</p>
          <h3>独立验收</h3>
          <p>{{ f.acceptance }}</p>
        </section>
      </div>
      <p v-if="!firmware.length" class="empty">暂无固件示例</p></template
    >
    <template v-else-if="route.page === 'operations'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 运行</p>
          <h1>运行概览</h1>
          <p>
            以下指标和事件均为虚构演示，不代表实际服务状态；未知数据不等于零。
          </p>
        </div>
      </div>
      <div class="ops-grid">
        <section v-for="m in operations.metrics" :key="m.label" class="panel">
          <small>虚构示例 · {{ m.label }}</small>
          <h2>{{ m.value }}</h2>
          <p>{{ m.description }}</p>
        </section>
      </div>
      <section class="panel ops-detail">
        <h2>演示事件</h2>
        <div v-for="(e, i) in operations.events" :key="i" class="list-row">
          <span
            ><b>{{ e.label }}</b
            ><small>{{ e.time }} · {{ e.status }} · 虚构示例</small
            ><small v-if="selected === String(i)"
              >该事件为演示数据，无法用于判断实际服务状态。</small
            ></span
          ><button
            :aria-expanded="selected === String(i)"
            @click="selected = selected === String(i) ? '' : String(i)"
          >
            {{ selected === String(i) ? "收起" : "查看详情" }}
          </button>
        </div>
        <p v-if="!operations.events.length">暂无演示事件。</p>
      </section></template
    >
    <template v-else-if="route.page === 'audit'"
      ><div class="heading">
        <div>
          <p class="eyebrow">工作台 / 审计</p>
          <h1>审计记录</h1>
          <p>查询当前浏览器的演示记录，不代表真实审计日志。</p>
        </div>
      </div>
      <section class="panel">
        <form class="ops-controls" @submit.prevent="load">
          <label for="audit-query">查询</label
          ><input
            id="audit-query"
            v-model="query"
            placeholder="操作、对象或操作者"
          /><button>查询</button>
        </form>
        <div v-if="!audit.length" class="empty">没有匹配的演示记录</div>
        <div v-for="a in audit" :key="a.id" class="list-row">
          <span
            ><b>{{ a.action }}</b
            ><small>{{ a.time }} · {{ a.actor }} · {{ a.target }}</small
            ><span v-if="selected === a.id" class="ops-detail"
              ><b>演示记录详情</b
              ><small>对象：{{ a.target }} · 结果：{{ a.result }}</small
              ><small>记录编号：{{ a.id }}</small></span
            ></span
          ><button
            :aria-expanded="selected === a.id"
            @click="selected = selected === a.id ? '' : a.id"
          >
            {{ selected === a.id ? "收起" : "查看详情" }}
          </button>
        </div>
      </section></template
    >
  </template>
</template>
