<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import type { Scope, Account } from "./types.ts";
import {
  adminNav,
  familyNav,
  adminRoutes,
  familyRoutes,
  linkTo,
  readRoute,
} from "./navigation.ts";
import { api } from "./api.ts";
import { onSensitiveInvalidated } from "./sensitive-session.ts";
import CorePages from "./pages/CorePages.vue";
import DataPages from "./pages/DataPages.vue";
import AuditPage from "./pages/AuditPage.vue";
const scope: Scope = location.pathname.endsWith("family.html")
  ? "family"
  : "admin";
const route = ref(readRoute(scope)),
  account = ref<Account | null>(null),
  accountBusy = ref(false);
const sessionReady = ref(false),
  sessionError = ref(""),
  sessionNotice = ref("");
const reloadKey = ref(0);
let authVersion = 0;
const pageVisible = ref(document.visibilityState === "visible");
let unsubscribeSensitive = () => {};
function foreground() {
  pageVisible.value = document.visibilityState === "visible";
  if (pageVisible.value) {
    reloadKey.value++;
    void refreshAccount();
  }
}
const nav = computed(() => scope === "admin" ? adminNav : familyNav);
const routes = scope === "admin" ? adminRoutes : familyRoutes;
const needsLogin = computed(
  () => sessionReady.value && !account.value,
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
const dataRoutes = ["login", "families", "family", "accounts", "memories", "memory"];
const pageComponent = computed(() =>
  scope === "admin" && displayedRoute.value.page === "audit"
    ? AuditPage
    : dataRoutes.includes(displayedRoute.value.page)
    ? DataPages
    : CorePages,
);
async function refreshAccount() {
  const version = ++authVersion;
  try {
    const current = await api.getAccount(scope);
    if (version === authVersion) {
      account.value = current;
      pageVisible.value = document.visibilityState === "visible";
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
function expired() {
  authVersion++;
  account.value = null;
  sessionReady.value = true;
  pageVisible.value = document.visibilityState === "visible";
  sessionNotice.value = "登录已过期，请重新登录。";
}
function update() {
  pageVisible.value = document.visibilityState === "visible";
  route.value = readRoute(scope);
  void refreshAccount();
}
function restorePage(event: PageTransitionEvent) {
  if (event.persisted) location.reload();
}
onMounted(() => {
  unsubscribeSensitive = onSensitiveInvalidated(() => {
    pageVisible.value = false;
    reloadKey.value++;
  });
  document.addEventListener("visibilitychange", foreground);
  window.addEventListener("hashchange", update);
  window.addEventListener("pageshow", restorePage);
  window.addEventListener("management-session-expired", expired);
  window.addEventListener("management-account-changed", refreshAccount);
  void refreshAccount();
});
onUnmounted(() => {
  unsubscribeSensitive();
  document.removeEventListener("visibilitychange", foreground);
  authVersion++;
  window.removeEventListener("hashchange", update);
  window.removeEventListener("pageshow", restorePage);
  window.removeEventListener("management-session-expired", expired);
  window.removeEventListener("management-account-changed", refreshAccount);
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
    pageVisible.value = true;
    sessionNotice.value = "";
    location.href = linkTo(scope, "login");
  } catch (error) {
    sessionError.value =
      error instanceof Error ? error.message : "退出失败，请重试";
  } finally {
    accountBusy.value = false;
  }
}
function active(page: string) {
  return (
    route.value.page === page ||
    (page === "devices" && route.value.page === "device") ||
    (page === "conversations" && route.value.page === "conversation") ||
    (page === "settings" &&
      route.value.page === "family")
  );
}
function navQuery(page: string): Record<string, string> {
  const query: Record<string, string> = {};
  if (route.value.device) query.device = route.value.device;
  if (page === "family" && route.value.family)
    query.family = route.value.family;
  return query;
}
function directoryLink(page: string) {
  return linkTo(
    scope,
    page === "conversation" ? "conversations" : page,
    navQuery(page),
  );
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
      <nav v-if="account" aria-label="内部导航">
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
          ><span class="data-source">真实数据 · 设备回执确认</span>
        </div>
        <div class="top-actions">
          <button
            v-if="account"
            class="text-button"
            :disabled="accountBusy"
            @click="logout"
          >
            退出登录</button
          ><a v-else :href="linkTo(scope, 'login')">登录</a
          ><a v-if="scope === 'family'" :href="linkTo('admin', 'overview')"
            >内部端 ↗</a
          >
        </div>
      </header>
      <div v-if="sessionNotice" role="status" class="reset-message">
        {{ sessionNotice }}
      </div>
      <details v-if="account" class="page-directory">
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
        <section v-if="!sessionReady" class="panel" role="status">
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
        <section v-else-if="!pageVisible" class="panel" role="status">页面内容已清理，请重新进入页面读取。</section>
        <component
          v-else
          :is="pageComponent"
          :key="reloadKey"
          :scope="scope"
          :route="displayedRoute"
        />
      </main>
    </div>
    <nav
      v-if="scope === 'family' && (account)"
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
