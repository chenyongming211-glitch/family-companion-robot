<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import type { Scope } from './types.ts'
import { adminNav, familyNav, linkTo, readRoute } from './navigation.ts'
import { api } from './api.ts'
import CorePages from './pages/CorePages.vue'
const scope: Scope = location.pathname.endsWith('family.html') ? 'family' : 'admin'
const route = ref(readRoute(scope))
const update = () => { route.value = readRoute(scope) }
onMounted(() => window.addEventListener('hashchange', update))
onUnmounted(() => window.removeEventListener('hashchange', update))
const nav = computed(() => scope === 'admin' ? adminNav : familyNav)
const resetMessage = ref('')
const storageWarning = ref(api.storageWarning)
const storageMode = ref(api.storageMode)
function refreshStorage() { storageWarning.value = api.storageWarning; storageMode.value = api.storageMode }
const reloadKey = ref(0)
function active(page: string) { return route.value.page === page || (page === 'devices' && ['device','configuration'].includes(route.value.page)) || (page === 'conversations' && route.value.page === 'conversation') || (page === 'settings' && ['family','invite','bind','privacy'].includes(route.value.page)) }
function navQuery() { return { device: route.value.device, chat: route.value.chat, memory: route.value.memory, family: route.value.family } }
async function reset() { if (!confirm('重置所有演示数据？')) return; try { await api.reset(); resetMessage.value = '演示数据已重置'; reloadKey.value++; refreshStorage() } catch { resetMessage.value = '重置失败，请重试' } }
</script>
<template>
  <div :class="['app', scope]">
    <aside v-if="scope === 'admin'" class="sidebar"><a class="brand" :href="linkTo(scope,'overview')"><span class="brand-icon">伴</span><span>伴伴 <small>管理平台</small></span></a><p class="nav-label">工作台</p><nav aria-label="内部导航"><a v-for="item in nav" :key="item[0]" :class="{active: active(item[0])}" :href="linkTo(scope,item[0],navQuery())">{{ item[1] }}</a></nav><a class="switch" :href="linkTo('family','home')">打开家庭端 ↗</a></aside>
    <div class="workspace"><header class="topbar"><div><strong>{{ scope === 'admin' ? '管理工作台' : '伴伴' }}</strong><span class="demo">前端演示 · 虚构数据</span></div><div class="top-actions"><span v-if="storageMode === 'memory'" class="muted">仅内存保存</span><button class="text-button" @click="reset">重置演示</button><a v-if="scope === 'family'" :href="linkTo('admin','overview')">内部端 ↗</a></div></header><div role="status" class="reset-message">{{ resetMessage }}</div><div v-if="storageWarning" role="alert" class="reset-message">{{ storageWarning }}</div><main><CorePages :key="reloadKey" :scope="scope" :route="route" @storage-changed="refreshStorage" /></main></div>
    <nav v-if="scope === 'family'" class="bottom-nav" aria-label="家庭导航"><a v-for="item in nav" :key="item[0]" :class="{active: active(item[0])}" :href="linkTo(scope,item[0],navQuery())"><span class="nav-dot">{{ item[1].slice(0,1) }}</span>{{ item[1] }}</a></nav>
  </div>
</template>
