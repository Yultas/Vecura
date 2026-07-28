<template>
  <div class="app-backdrop"></div>

  <div class="app-shell">
    <!-- Title bar with native-style window controls (Win64) -->
    <header class="titlebar" @dblclick="onTitlebarDblClick">
      <div class="titlebar-title">
        <span class="app-glyph"><svg viewBox="0 0 24 24"><path d="M4 7l8-4 8 4-8 4-8-4zM4 12l8 4 8-4M4 17l8 4 8-4" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/></svg></span>
        Vecura
      </div>
      <div class="traffic">
        <button class="light-dot light-red" title="Close" @click="winClose"><X /></button>
        <button class="light-dot light-yellow" title="Minimize" @click="winMin"><Minus /></button>
        <button class="light-dot light-green" title="Maximize" @click="winMax"><Square /></button>
      </div>
    </header>

    <div class="app-body">
      <!-- Floating sidebar card -->
      <aside class="sidebar" :class="{ collapsed: sidebarCollapsed }">
        <div class="sidebar-top">
          <button class="sidebar-toggle" @click="toggleSidebar" :title="sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'">
            <PanelLeftClose v-if="!sidebarCollapsed" :size="18" />
            <PanelLeftOpen v-else :size="18" />
          </button>
        </div>
        <nav class="sidebar-nav">
          <div class="nav-item" :class="{ active: activeRoute === '/settings' }" @click="onMenu('/settings')">
            <el-icon><SettingsIcon /></el-icon><span class="nav-label">Settings</span>
          </div>
          <div class="nav-item" :class="{ active: activeRoute === '/help' }" @click="onMenu('/help')">
            <el-icon><CircleHelp /></el-icon><span class="nav-label">Help</span>
          </div>
          <div class="nav-item" :class="{ active: logsOpen }" @click="logsOpen = true">
            <el-icon><ScrollText /></el-icon><span class="nav-label">Logs</span>
          </div>
        </nav>

        <div class="sidebar-scroll">
          <router-view v-slot="{ Component }">
            <KeepAlive>
              <component
                :is="Component"
                :models="models"
                :active-key="activeModel"
                :folder-path="configFolder"
                @models-changed="loadModels"
                @use-model="onUseModel"
                @scan-folder="onScanFolder"
              />
            </KeepAlive>
          </router-view>
        </div>

        <div class="sidebar-foot">
          <button class="theme-toggle" @click="toggleTheme" :title="theme === 'dark' ? 'Switch to light' : 'Switch to dark'" :aria-label="theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'">
            <Transition name="theme-spin" mode="out-in">
              <Sun v-if="theme === 'dark'" :key="'sun'" />
              <Moon v-else :key="'moon'" />
            </Transition>
          </button>
        </div>
      </aside>

      <!-- Main content card -->
      <section class="main">
        <div class="main-search">
          <SearchBar :recent="recent" :active-model="activeModel" :has-vl="hasVL" @search="onSearch" @search-image="onSearchImage" />
        </div>
        <div class="main-scroll">
          <ImageGrid :hits="hits" :loading="searching" @open="openPreview" />
        </div>
      </section>
    </div>

    <PreviewModal
      v-model="previewVisible"
      :hits="hits"
      :index="previewIndex"
      @update:index="previewIndex = $event"
    />

    <el-dialog
      v-model="logsOpen"
      title="Application logs"
      width="760px"
      class="logs-dialog"
      :close-on-click-modal="false"
    >
      <LogsView />
    </el-dialog>

    <div v-if="scanActive" class="scan-chip">
      <el-progress type="circle" :percentage="scanPercent" :width="58" :stroke-width="4" />
      <span class="scan-text">{{ scanText }}</span>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import SearchBar from './components/SearchBar.vue'
import ImageGrid from './components/ImageGrid.vue'
import PreviewModal from './components/PreviewModal.vue'
import LogsView from './views/LogsView.vue'
import { call, eventsOn } from './api.js'
import { pushLog } from './logger.js'
import { Settings as SettingsIcon, CircleHelp, ScrollText, X, Minus, Square, Moon, Sun, PanelLeftClose, PanelLeftOpen } from '@lucide/vue'
import { WindowMinimise, WindowToggleMaximise, Quit } from '../wailsjs/runtime/runtime.js'

const route = useRoute()
const router = useRouter()

const activeRoute = computed(() => route.path)
const THEME_STORAGE_KEY = 'vecura:theme'
const SIDEBAR_STORAGE_KEY = 'vecura:sidebar-collapsed'
const theme = ref(localStorage.getItem(THEME_STORAGE_KEY) || 'dark')
const sidebarCollapsed = ref(localStorage.getItem(SIDEBAR_STORAGE_KEY) === 'true')
const models = ref([])
const recent = ref([])
const configFolder = ref('')
const hits = ref([])
const searching = ref(false)
const activeModel = ref('')
const hasVL = ref(false)
const previewVisible = ref(false)
const previewIndex = ref(0)

const scanActive = ref(false)
const scanPercent = ref(0)
const scanText = ref('')

const logsOpen = ref(false)

// Upper bound on results returned per search. Keyword + semantic hits are
// merged and ranked server-side, so this just caps how much the grid ever
// has to virtualize at once.
const SEARCH_LIMIT = 1000

function applyTheme() {
  document.documentElement.setAttribute('data-theme', theme.value)
  localStorage.setItem(THEME_STORAGE_KEY, theme.value)
}
function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark'
  applyTheme()
}
function toggleSidebar() {
  sidebarCollapsed.value = !sidebarCollapsed.value
  localStorage.setItem(SIDEBAR_STORAGE_KEY, sidebarCollapsed.value)
}
function onMenu(index) {
  router.push(index)
}

// Win64 window controls (frameless app has no OS chrome)
function winClose() { Quit() }
function winMin() { WindowMinimise() }
function winMax() { WindowToggleMaximise() }

// Double-clicking the empty title bar area toggles maximize/restore, just
// like native window chrome. Ignore double-clicks on the traffic-light
// buttons themselves.
function onTitlebarDblClick(e) {
  if (e.target.closest('.traffic')) return
  winMax()
}

async function loadModels() {
  try {
    models.value = await call('ListModels')
  } catch (e) {
    console.error(e)
  }
}
async function loadRecent() {
  try {
    recent.value = await call('RecentSearches')
  } catch (e) {
    console.error(e)
  }
}

function onUseModel(key) {
  activeModel.value = key
  call('SetActiveModel', key).catch(() => {})
}

async function onScanFolder(path) {
  if (!activeModel.value) {
    ElMessage.warning('Select a model before scanning')
    return
  }
  scanActive.value = true
  scanPercent.value = 0
  pushLog('info', ['Scan started', path, 'model=' + activeModel.value])
  try {
    await call('ScanFolder', path, activeModel.value)
    pushLog('info', ['Scan finished', path])
  } catch (e) {
    pushLog('error', ['Scan failed', e])
    ElMessage.error('Scan failed: ' + e)
  }
}

async function onSearch(query, mode) {
  // Keyword search works without an embedding model; a registered model only
  // adds semantic results on top. So we no longer require one up front.
  let provider = ''
  let modelId = ''
  if (activeModel.value && activeModel.value.includes('/')) {
    ;[provider, modelId] = activeModel.value.split('/')
  }
  searching.value = true
  try {
    const res = await call('Search', query, provider, modelId, SEARCH_LIMIT, '', mode || 'auto')
    hits.value = res || []
    await loadRecent()
  } catch (e) {
    ElMessage.error('Search failed: ' + e)
  } finally {
    searching.value = false
  }
}

async function onSearchImage(file) {
  // Read the file as base64 data URI and search via VL model.
  const reader = new FileReader()
  reader.onload = async () => {
    const dataURI = reader.result // e.g. "data:image/png;base64,..."
    searching.value = true
    try {
      const res = await call('SearchByImageDataURI', dataURI, SEARCH_LIMIT)
      hits.value = res || []
    } catch (e) {
      ElMessage.error('Image search failed: ' + e)
    } finally {
      searching.value = false
    }
  }
  reader.readAsDataURL(file)
}

function openPreview(idx) {
  previewIndex.value = idx
  previewVisible.value = true
}

onMounted(async () => {
  applyTheme()
  await loadModels()
  try {
    const cfg = await call('GetConfig')
    if (cfg) {
      if (cfg.activeModel) {
        activeModel.value = cfg.activeModel
        ElMessage.success('Reconnected to model: ' + cfg.activeModel)
      }
      if (cfg.folderPath) configFolder.value = cfg.folderPath
    }
  } catch (e) {
    console.error(e)
  }
  await loadRecent()
  // Check if VL model is configured and server is running.
  try {
    const status = await call('GetLLamaStatus')
    hasVL.value = status && status.installed && status.serverOK
  } catch (_) {}
  eventsOn('scan:progress', (p) => {
    if (p.Total > 0) scanPercent.value = Math.round((p.Done / p.Total) * 100)
    scanText.value = `Scanning ${p.Done}/${p.Total}`
    scanActive.value = !p.Finished
    if (p.Finished) pushLog('info', ['Scan progress: finished', p.Done + '/' + p.Total])
    else if (p.Total > 0 && p.Done === p.Total) pushLog('info', ['Scan progress', p.Done + '/' + p.Total])
  })
  // Backend log events — forwarded from Go logBlog functions.
  eventsOn('backend:log', (level, msg) => {
    pushLog(level, ['[backend]', msg])
  })
})
</script>
