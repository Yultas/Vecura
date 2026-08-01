<template>
  <div class="app-backdrop"></div>

  <LoadingView v-if="bootLoading || activeRoute === '/loading'" />

  <div v-else class="app-shell">
    <!-- Title bar with native-style window controls (Win64) -->
    <header class="titlebar" @dblclick="onTitlebarDblClick">
      <div class="titlebar-title">
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
          <SearchBar :recent="recent" :active-model="activeModel" @search="onSearch" />
        </div>
        <div class="main-scroll">
          <ImageGrid :hits="orderedHits" :loading="searching" @open="openPreview" />
        </div>
      </section>
    </div>

    <PreviewModal
      v-model="previewVisible"
      :hits="orderedHits"
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
import LoadingView from './views/LoadingView.vue'
import { call, eventsOn } from './api.js'
import { pushLog } from './logger.js'
import { Settings as SettingsIcon, CircleHelp, ScrollText, X, Minus, Square, Moon, Sun, PanelLeftClose, PanelLeftOpen } from '@lucide/vue'
import { WindowMinimise, WindowToggleMaximise, Quit } from '../wailsjs/runtime/runtime.js'

const route = useRoute()
const router = useRouter()

const activeRoute = computed(() => route.path)
const bootLoading = ref(true)
let finishBoot
const bootFinished = new Promise((resolve) => { finishBoot = resolve })
function notifyAfterBoot(type, message) {
  bootFinished.then(() => ElMessage[type](message))
}
const THEME_STORAGE_KEY = 'vecura:theme'
const SIDEBAR_STORAGE_KEY = 'vecura:sidebar-collapsed'
const theme = ref(localStorage.getItem(THEME_STORAGE_KEY) || 'dark')
document.documentElement.setAttribute('data-theme', theme.value)
const sidebarCollapsed = ref(localStorage.getItem(SIDEBAR_STORAGE_KEY) === 'true')
const models = ref([])
const recent = ref([])
const configFolder = ref('')
const hits = ref([])
const searching = ref(false)
const activeModel = ref('')

// Keep the gallery and preview navigation in the same newest-first order.
const orderedHits = computed(() => [...hits.value].sort((a, b) => {
  const byDate = (Number(b.date) || 0) - (Number(a.date) || 0)
  return byDate || (Number(b.id) || 0) - (Number(a.id) || 0)
}))
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
    ElMessage.warning('Model is not loaded. Select or start a model before scanning.')
    return
  }
  try {
    // ScanFolder validates the model synchronously before starting its
    // background work. Only show progress after that validation succeeds.
    await call('ScanFolder', path, activeModel.value)
    scanActive.value = true
    scanPercent.value = 0
    pushLog('info', ['Scan started', path, 'model=' + activeModel.value])
  } catch (e) {
    pushLog('error', ['Scan cannot start', e])
    ElMessage.warning('Model is not loaded. Scanning cannot be started.')
  }
}

async function onSearch(query) {
  // Keyword search works without an embedding model; a registered model only
  // adds semantic results on top. So we no longer require one up front.
  let provider = ''
  let modelId = ''
  if (activeModel.value && activeModel.value.includes('/')) {
    ;[provider, modelId] = activeModel.value.split('/')
  }
  searching.value = true
  try {
    const res = await call('Search', query, provider, modelId, SEARCH_LIMIT, '')
    hits.value = res || []
    await loadRecent()
  } catch (e) {
    ElMessage.error('Search failed: ' + e)
  } finally {
    searching.value = false
  }
}

function openPreview(idx) {
  previewIndex.value = idx
  previewVisible.value = true
}

onMounted(async () => {
  const splashStartedAt = performance.now()
  applyTheme()
  await loadModels()
  // Subscribe before restoring the config so an automatic startup scan
  // cannot emit its first progress event before the UI is listening.
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

  try {
    const cfg = await call('GetConfig')
    if (cfg) {
      if (cfg.activeModel) {
        // local/vl was the key used by the removed vision embedder. Migrate
        // it in memory so startup scan can report the real local model state.
        activeModel.value = cfg.activeModel === 'local/vl' ? 'local/llm' : cfg.activeModel
        notifyAfterBoot('success', 'Reconnected to model: ' + activeModel.value)
        if (cfg.activeModel === 'local/vl') {
          call('SetActiveModel', 'local/llm').catch(() => {})
        }
      }
      if (cfg.folderPath) configFolder.value = cfg.folderPath
      // Re-scan the previously selected folder after the model and config
      // have been restored. ScanFolder is incremental, so existing images
      // are skipped and only new/changed files are processed.
      if (cfg.folderPath && cfg.activeModel) {
        try {
          // The backend validates the restored model before starting the
          // asynchronous scan, so an unavailable model never shows progress.
          await call('ScanFolder', cfg.folderPath, cfg.activeModel)
          scanActive.value = true
          scanPercent.value = 0
          pushLog('info', ['Automatic scan started', cfg.folderPath, 'model=' + cfg.activeModel])
        } catch (e) {
          scanActive.value = false
          pushLog('error', ['Automatic scan cannot start', e])
          notifyAfterBoot('warning', 'Saved model is not loaded. Automatic scanning cannot be started.')
        }
      }
    }
  } catch (e) {
    console.error(e)
  }
  await loadRecent()
  // Check if local model server is running (non-blocking).
  try {
    const status = await call('GetLLamaStatus')
    // status is available if needed in the future
    void status
  } catch (_) {}
  // Keep the splash visible until both the initial data and the full
  // letter animation have finished. The last letter starts after 0.6s and
  // the staggered letters complete within 1.5s.
  const splashMinimum = 1500
  const splashElapsed = performance.now() - splashStartedAt
  if (splashElapsed < splashMinimum) {
    await new Promise((resolve) => setTimeout(resolve, splashMinimum - splashElapsed))
  }
  bootLoading.value = false
  finishBoot()
})
</script>
