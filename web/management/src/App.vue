<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import type { Scope, Account } from './types.ts'
import { adminNav, familyNav, linkTo, readRoute } from './navigation.ts'
import { api } from './api.ts'
import CorePages from './pages/CorePages.vue'
import DataPages from './pages/DataPages.vue'
const scope: Scope = location.pathname.endsWith('family.html') ? 'family' : 'admin'
const route = ref(readRoute(scope))
const update = () => { route.value = readRoute(scope); refreshAccount() }
onMounted(() => { window.addEventListener('hashchange', update); window.addEventListener('demo-storage-changed', refreshDemo); refreshAccount() })
onUnmounted(() => { window.removeEventListener('hashchange', update); window.removeEventListener('demo-storage-changed', refreshDemo) })
const nav = computed(() => scope === 'admin' ? adminNav : familyNav)
const resetMessage = ref('')
const storageWarning = ref(api.storageWarning)
const storageMode = ref(api.storageMode)
const account = ref<Account | null>(null)
const accountBusy = ref(false)
const dataRoutes = ['login','families','family','accounts','policies','memories','daily','invite','bind','privacy','memory']
const pageComponent = computed(() => dataRoutes.includes(route.value.page) ? DataPages : CorePages)
function refreshStorage() { storageWarning.value = api.storageWarning; storageMode.value = api.storageMode }
function focusMain() { document.getElementById('main-content')?.focus() }
async function refreshAccount() { account.value = await api.getAccount(scope) }
function refreshDemo() { refreshStorage(); refreshAccount() }
async function logout() {
  if (accountBusy.value) return
  accountBusy.value = true
  try { await api.logout(scope); account.value = null; location.href = linkTo(scope,'login') }
  finally { accountBusy.value = false; refreshStorage() }
}
const reloadKey = ref(0)
function active(page: string) { return route.value.page === page || (page === 'devices' && ['device','configuration'].includes(route.value.page)) || (page === 'conversations' && route.value.page === 'conversation') || (page === 'settings' && ['family','invite','bind','privacy'].includes(route.value.page)) }
function navQuery(page: string): Record<string, string> { return page === 'family' ? { device: route.value.device, family: route.value.family } : { device: route.value.device } }
async function reset() { if (!confirm('重置所有演示数据？')) return; try { await api.reset(); resetMessage.value = '演示数据已重置'; reloadKey.value++; refreshDemo() } catch { resetMessage.value = '重置失败，请重试' } }
</script>
<template>
  <div :class="['app', scope]">
    <a class="skip-link" href="#main-content" @click.prevent="focusMain">跳到主要内容</a>
    <aside v-if="scope === 'admin'" class="sidebar"><a class="brand" :href="linkTo(scope,'overview')"><span class="brand-icon">伴</span><span>伴伴 <small>管理平台</small></span></a><p class="nav-label">工作台</p><nav aria-label="内部导航"><a v-for="item in nav" :key="item[0]" :class="{active: active(item[0])}" :aria-current="active(item[0]) ? 'page' : undefined" :href="linkTo(scope,item[0],navQuery(item[0]))">{{ item[1] }}</a></nav><a class="switch" :href="linkTo('family','home')">打开家庭端 ↗</a></aside>
    <div class="workspace"><header class="topbar"><div><strong>{{ scope === 'admin' ? '管理工作台' : '伴伴' }}</strong><span class="demo">前端演示 · 虚构数据</span></div><div class="top-actions"><span v-if="storageMode === 'memory'" class="muted">仅内存保存</span><button class="text-button" @click="reset">重置演示</button><button v-if="account" class="text-button" :disabled="accountBusy" @click="logout">退出演示</button><a v-else :href="linkTo(scope,'login')">演示登录</a><a v-if="scope === 'family'" :href="linkTo('admin','overview')">内部端 ↗</a></div></header><div role="status" class="reset-message">{{ resetMessage }}</div><div v-if="storageWarning" role="alert" class="reset-message">{{ storageWarning }}</div><main id="main-content" tabindex="-1"><component :is="pageComponent" :key="reloadKey" :scope="scope" :route="route" @storage-changed="refreshStorage" /></main></div>
    <nav v-if="scope === 'family'" class="bottom-nav" aria-label="家庭导航"><a v-for="item in nav" :key="item[0]" :class="{active: active(item[0])}" :aria-current="active(item[0]) ? 'page' : undefined" :href="linkTo(scope,item[0],navQuery(item[0]))"><span class="nav-dot">{{ item[1].slice(0,1) }}</span>{{ item[1] }}</a></nav>
  </div>
</template>
