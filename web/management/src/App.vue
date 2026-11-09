<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import type { Scope, Account } from "./types.ts";
import {
  adminNav,
  familyNav,
  adminRoutes,
  familyRoutes,
  operationsRoutes,
  linkTo,
  readRoute,
} from "./navigation.ts";
import { api, liveMode } from "./api.ts";
import CorePages from "./pages/CorePages.vue";
import DataPages from "./pages/DataPages.vue";
import OperationsPages from "./pages/OperationsPages.vue";
const scope: Scope = location.pathname.endsWith("family.html")
  ? "family"
  : "admin";
const route = ref(readRoute(scope)),
  account = ref<Account | null>(null),
  accountBusy = ref(false);
const sessionReady = ref(!liveMode),
  sessionError = ref(""),
  sessionNotice = ref("");
const resetMessage = ref(""),
  storageWarning = ref(api.storageWarning),
  storageMode = ref(api.storageMode),
  reloadKey = ref(0);
let authVersion = 0;
const implemented =
  scope === "admin"
    ? [
        "overview",
        "devices",
        "device",
        "conversations",
        "conversation",
        "configuration",
        "families",
        "family",
        "accounts",
        "login",
      ]
    : [
        "home",
        "device",
        "conversations",
        "conversation",
        "settings",
        "family",
        "login",
      ];
const nav = computed(() => {
  const items = scope === "admin" ? adminNav : familyNav;
  return liveMode
    ? items.filter((item) => implemented.includes(item[0]))
    : items;
});
const routes = (scope === "admin" ? adminRoutes : familyRoutes).filter(
  (item) => !liveMode || implemented.includes(item[0]),
);
const needsLogin = computed(
  () => liveMode && sessionReady.value && !account.value,
);
const displayedRoute = computed(() =>
  needsLogin.value ? { ...route.value, page: "login" } : route.value,
);
const knownPage = computed(
  () =>
    needsLogin.value ||
    routes.some((r) => r[0] === route.value.page) ||
    (scope === "admin" &&
      ["conversation", "settings"].includes(route.value.page)),
);
const dataRoutes = [
  "login",
  "families",
  "family",
  "accounts",
  "policies",
  "memories",
  "daily",
  "invite",
  "bind",
  "privacy",
  "memory",
];
const pageComponent = computed(() =>
  dataRoutes.includes(displayedRoute.value.page)
    ? DataPages
    : operationsRoutes.includes(route.value.page)
      ? OperationsPages
      : CorePages,
);
function refreshStorage() {
  storageWarning.value = api.storageWarning;
  storageMode.value = api.storageMode;
}
async function refreshAccount() {
  const version = ++authVersion;
  try {
    const current = await api.getAccount(scope);
    if (version === authVersion) {
      account.value = current;
      sessionError.value = "";
      if (current) sessionNotice.value = "";
    }
  } catch (error) {
    if (version === authVersion) {
      account.value = null;
      sessionError.value =
        error instanceof Error ? error.message : "无法检查登录状态";
    }
  } finally {
    if (version === authVersion) sessionReady.value = true;
  }
}
function refreshDemo() {
  refreshStorage();
  void refreshAccount();
}
function expired() {
  authVersion++;
  account.value = null;
  sessionReady.value = true;
  sessionNotice.value = "登录已过期，请重新登录。";
}
function update() {
  route.value = readRoute(scope);
  void refreshAccount();
}
function restorePage(event: PageTransitionEvent) {
  if (event.persisted) location.reload();
}
onMounted(() => {
  window.addEventListener("hashchange", update);
  window.addEventListener("pageshow", restorePage);
  window.addEventListener("demo-storage-changed", refreshDemo);
  window.addEventListener("management-session-expired", expired);
  void refreshAccount();
});
onUnmounted(() => {
  authVersion++;
  window.removeEventListener("hashchange", update);
  window.removeEventListener("pageshow", restorePage);
  window.removeEventListener("demo-storage-changed", refreshDemo);
  window.removeEventListener("management-session-expired", expired);
});
function focusMain() {
  document.getElementById("main-content")?.focus();
}
async function logout() {
  if (accountBusy.value) return;
  accountBusy.value = true;
  authVersion++;
  try {
    await api.logout(scope);
    account.value = null;
    sessionNotice.value = "";
    location.href = linkTo(scope, "login");
  } catch (error) {
    sessionError.value =
      error instanceof Error ? error.message : "退出失败，请重试";
  } finally {
    accountBusy.value = false;
    refreshStorage();
  }
}
function active(page: string) {
  return (
    route.value.page === page ||
    (page === "devices" && route.value.page === "device") ||
    (page === "conversations" && route.value.page === "conversation") ||
    (page === "settings" &&
      ["family", "invite", "bind", "privacy"].includes(route.value.page))
  );
}
function navQuery(page: string): Record<string, string> {
  const query: Record<string, string> = {};
  if (route.value.device) query.device = route.value.device;
  if (page === "family" && route.value.family)
    query.family = route.value.family;
  return query;
}
function directoryQuery(page: string): Record<string, string> {
  if (!liveMode && page === "conversation")
    return { device: "dev-living", chat: "chat-story" };
  if (!liveMode && page === "memory")
    return { device: "dev-living", memory: "memory-story" };
  return navQuery(page);
}
function directoryLink(page: string) {
  return linkTo(
    scope,
    liveMode && page === "conversation" ? "conversations" : page,
    directoryQuery(page),
  );
}
async function reset() {
  if (liveMode || !confirm("重置所有演示数据？")) return;
  try {
    await api.reset();
    resetMessage.value = "演示数据已重置";
    reloadKey.value++;
    refreshDemo();
  } catch {
    resetMessage.value = "重置失败，请重试";
  }
}
</script>
<template>
  <div :class="['app', scope]">
    <a class="skip-link" href="#main-content" @click.prevent="focusMain"
      >跳到主要内容</a
    >
    <aside v-if="scope === 'admin'" class="sidebar">
      <a class="brand" :href="linkTo(scope, 'overview')"
        ><span class="brand-icon">伴</span
        ><span>伴伴 <small>管理平台</small></span></a
      >
      <p class="nav-label">工作台</p>
      <nav v-if="!liveMode || account" aria-label="内部导航">
        <a
          v-for="item in nav"
          :key="item[0]"
          :class="{ active: active(item[0]) }"
          :aria-current="active(item[0]) ? 'page' : undefined"
          :href="linkTo(scope, item[0], navQuery(item[0]))"
          >{{ item[1] }}</a
        >
      </nav>
      <a class="switch" :href="linkTo('family', 'home')">打开家庭端 ↗</a>
    </aside>
    <div class="workspace">
      <header class="topbar">
        <div>
          <strong>{{ scope === "admin" ? "管理工作台" : "伴伴" }}</strong
          ><span class="demo">{{
            liveMode ? "真实数据 · 只读接入" : "前端演示 · 虚构数据"
          }}</span>
        </div>
        <div class="top-actions">
          <span v-if="storageMode === 'memory'" class="muted">仅内存保存</span
          ><button v-if="!liveMode" class="text-button" @click="reset">
            重置演示</button
          ><button
            v-if="account"
            class="text-button"
            :disabled="accountBusy"
            @click="logout"
          >
            {{ liveMode ? "退出登录" : "退出演示" }}</button
          ><a v-else :href="linkTo(scope, 'login')">{{
            liveMode ? "登录" : "演示登录"
          }}</a
          ><a v-if="scope === 'family'" :href="linkTo('admin', 'overview')"
            >内部端 ↗</a
          >
        </div>
      </header>
      <div role="status" class="reset-message">{{ resetMessage }}</div>
      <div v-if="storageWarning" role="alert" class="reset-message">
        {{ storageWarning }}
      </div>
      <div v-if="sessionNotice" role="status" class="reset-message">
        {{ sessionNotice }}
      </div>
      <details v-if="!liveMode || account" class="page-directory">
        <summary>页面目录（{{ routes.length }}）</summary>
        <div class="directory-grid">
          <a
            v-for="item in routes"
            :key="item[0]"
            :href="directoryLink(item[0])"
            >{{ item[1] }}</a
          >
        </div>
      </details>
      <main id="main-content" tabindex="-1">
        <section v-if="liveMode && !sessionReady" class="panel" role="status">
          正在检查登录状态…
        </section>
        <section v-else-if="sessionError" class="panel error" role="alert">
          <h1>连接暂时不可用</h1>
          <p>{{ sessionError }}</p>
          <button @click="refreshAccount">重试</button>
        </section>
        <section v-else-if="!knownPage" class="panel">
          <h1>页面未找到</h1>
          <p>请从页面目录选择一个有效页面。</p>
          <a
            class="button"
            :href="linkTo(scope, scope === 'admin' ? 'overview' : 'home')"
            >返回首页</a
          >
        </section>
        <component
          v-else
          :is="pageComponent"
          :key="reloadKey"
          :scope="scope"
          :route="displayedRoute"
          @storage-changed="refreshStorage"
        />
      </main>
    </div>
    <nav
      v-if="scope === 'family' && (!liveMode || account)"
      class="bottom-nav"
      aria-label="家庭导航"
    >
      <a
        v-for="item in nav"
        :key="item[0]"
        :class="{ active: active(item[0]) }"
        :aria-current="active(item[0]) ? 'page' : undefined"
        :href="linkTo(scope, item[0], navQuery(item[0]))"
        ><span class="nav-dot">{{ item[1].slice(0, 1) }}</span
        >{{ item[1] }}</a
      >
    </nav>
  </div>
</template>
