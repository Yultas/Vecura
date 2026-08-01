<template>
  <div class="settings">
    <el-alert v-if="!ready" type="warning" :closable="false" title="Connecting to backend…" />
    <el-alert v-else-if="backendError" type="error" :closable="false" :title="'Backend error: ' + backendError" />
    <el-alert v-else-if="presets.length === 0" type="info" :closable="false" title="No provider presets loaded" />

    <div class="update-box">
      <div class="update-copy">
        <span class="update-title">Vecura {{ appVersion }}</span>
        <span v-if="updateInfo?.available" class="update-new">Version {{ updateInfo.version }} is available</span>
        <span v-else-if="updateChecking" class="update-new">Checking for updates…</span>
        <span v-else class="update-new">Keep Vecura up to date</span>
      </div>
      <button v-if="updateInfo?.available" class="primary-btn update-btn" :disabled="updateDownloading" @click="downloadUpdate">
        <LoaderCircle v-if="updateDownloading" class="spin" />
        <span v-else>Update</span>
      </button>
    </div>

    <template v-if="ready && !backendError">
      <div class="section-title model-head">
        <span>Provider</span>
        <button
          class="collapse-btn"
          type="button"
          :title="providerCollapsed ? 'Expand' : 'Collapse'"
          @click="providerCollapsed = !providerCollapsed"
        >{{ providerCollapsed ? '+' : '−' }}</button>
      </div>

      <template v-if="!providerCollapsed">
        <!-- Provider picker (custom frosted segmented control) -->
        <div class="provider-grid">
        <button
          v-for="p in presets"
          :key="p.id"
          class="provider-chip"
          :class="{ active: provider === p.id }"
          @click="selectProvider(p.id)"
        >
          <span class="provider-name">{{ p.name }}</span>
        </button>
      </div>

      <!-- Base URL with integrated check -->
      <div class="field-block">
        <label class="field-label">Base URL</label>
        <div class="field" :class="urlState">
          <input
            v-model="baseUrl"
            class="field-input"
            placeholder="https://api.openai.com/v1"
            @input="resetCheck"
            @change="persist"
            @keyup.enter="check"
          />
          <span v-if="urlState === 'valid'" class="field-badge ok"><svg viewBox="0 0 24 24"><path d="M5 13l4 4 10-10" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/></svg></span>
          <span v-else-if="urlState === 'invalid'" class="field-badge bad"><svg viewBox="0 0 24 24"><path d="M6 6l12 12M18 6L6 18" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"/></svg></span>
        </div>
      </div>

      <!-- API Key with integrated check -->
      <div class="field-block">
        <label class="field-label">API Key</label>
        <div class="field" :class="keyState">
          <input
            v-model="apiKey"
            :type="showKey ? 'text' : 'password'"
            class="field-input"
            placeholder="sk-..."
            @input="resetCheck"
            @change="persist"
            @keyup.enter="check"
          />
          <button class="field-affix" :title="showKey ? 'Hide' : 'Show'" @click="showKey = !showKey">
            <Eye v-if="showKey" />
            <EyeOff v-else />
          </button>
          <button class="field-check" :class="{ busy: checking }" :disabled="!baseUrl || checking" @click="check">
            <LoaderCircle v-if="checking" class="spin" />
            <span v-else>Check</span>
          </button>
        </div>
      </div>

        <!-- Fetched models -->
        <template v-if="fetchedModels.length">
          <div class="section-title model-head">
            <span>Available embedding models</span>
            <button
              class="collapse-btn"
              type="button"
              :title="modelsCollapsed ? 'Expand' : 'Collapse'"
              @click="modelsCollapsed = !modelsCollapsed"
            >{{ modelsCollapsed ? '+' : '−' }}</button>
          </div>
        <div v-show="!modelsCollapsed" class="model-list">
        <div class="field-block">
          <input
            v-model="modelQuery"
            class="field-input search-inline"
            placeholder="Search models by name…"
          />
        </div>

        <div v-if="visibleEmbed.length" class="model-sub">Embedding models</div>
        <button
          v-for="m in visibleEmbed"
          :key="m.id"
          class="model-row embed"
          :class="{ active: selectedModel === m.id }"
          @click="selectModel(m.id)"
        >
          <span class="model-radio-dot"></span>
          <span class="model-id">{{ m.id }}</span>
          <small v-if="m.contextLength" class="model-ctx">ctx {{ m.contextLength }}</small>
        </button>

        <template v-if="visibleOther.length">
          <div class="model-sub">Other models</div>
          <button
            v-for="m in visibleOther"
            :key="m.id"
            class="model-row"
            :class="{ active: selectedModel === m.id }"
            @click="selectModel(m.id)"
          >
            <span class="model-radio-dot"></span>
            <span class="model-id">{{ m.id }}</span>
            <small v-if="m.contextLength" class="model-ctx">ctx {{ m.contextLength }}</small>
          </button>
        </template>

        <div v-if="!visibleEmbed.length && !visibleOther.length" class="mini-empty">
          No models match “{{ modelQuery }}”
        </div>

        <!-- Manual model id entry (for providers that don't list embed models) -->
        <div class="field-block manual">
          <label class="field-label">Or enter a model id manually</label>
          <div class="field manual-field">
            <input
              v-model="manualModel"
              class="field-input"
              placeholder="nvidia/nemotron-3-embed-1b:free"
              @keyup.enter="useManualModel"
            />
            <button class="field-check" @click="useManualModel">Use</button>
          </div>
        </div>

      </div>
      </template>
      </template>

      <!-- Selection confirm + active model (kept visible while the list is collapsed) -->
      <button
        v-if="selectedModel"
        class="primary-btn"
        :class="{ busy: adding }"
        :disabled="adding"
        @click="addModel"
      >
        <LoaderCircle v-if="adding" class="spin" />
        <span v-else>Use selected model</span>
      </button>

      <div v-if="selectedInfo" class="active-card" :class="{ connected: activeModelConnected }">
        <span class="active-dot"></span>
        <div>
          <div class="active-key">{{ selectedInfo.key }}</div>
          <div class="active-sub">
            Dim {{ selectedInfo.dim }} • {{ selectedInfo.provider }} ·
            {{ activeModelConnected ? 'Connected' : 'Not connected' }}
          </div>
        </div>
      </div>

      <!-- Registered models -->
      <div class="section-title">Registered models</div>
      <div v-if="models.length" class="reg-list">
        <div v-for="m in models" :key="m.key" class="reg-row">
          <div class="reg-info">
            <div class="reg-key">{{ m.key }}</div>
            <div class="reg-dim">Dim {{ m.dim }}</div>
          </div>
          <div class="reg-actions">
            <button class="ghost-btn" :class="{ active: m.key === props.activeKey }" @click="$emit('use-model', m.key)">Use</button>
            <button class="ghost-btn danger" @click="remove(m.key)">✕</button>
          </div>
        </div>
      </div>
      <div v-else class="mini-empty">No models yet</div>

      <!-- Indexed folder card -->
      <div class="section-title">Indexed folder</div>
      <div class="folder-card" @click="pickFolder">
        <div class="folder-icon">
          <svg viewBox="0 0 24 24"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"/></svg>
        </div>
        <div class="folder-text">
          <div class="folder-path">{{ folderPath || 'Not selected' }}</div>
          <div class="folder-hint">{{ folderPath ? 'Click to change' : 'Click to choose a folder' }}</div>
        </div>
        <span class="folder-cta">Choose…</span>
      </div>

      <!-- Local Model -->
      <div class="section-title">Local Model (llama.cpp)</div>

      <!-- GPU Detection -->
      <div class="field-block">
        <label class="field-label">GPU</label>
        <div class="gpu-row">
          <span class="gpu-text">{{ gpuInfo ? gpuInfo.name + ' (' + gpuInfo.vendor + ')' : 'Not detected' }}</span>
          <button class="field-check" :disabled="gpuLoading" @click="detectGPU">
            <LoaderCircle v-if="gpuLoading" class="spin" />
            <span v-else>Detect</span>
          </button>
        </div>
      </div>

      <!-- Runtime status -->
      <div class="field-block">
        <label class="field-label">Runtime</label>
        <Transition name="status-fade" mode="out-in">
        <div v-if="llamaStatus.installed" class="gpu-row" key="installed">
          <span class="gpu-text">v{{ llamaStatus.version }} ({{ llamaStatus.backend }})</span>
          <span class="field-badge ok"><svg viewBox="0 0 24 24"><path d="M5 13l4 4 10-10" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/></svg></span>
        </div>
        <div v-else class="gpu-row" key="not-installed">
          <span class="gpu-text">Not installed</span>
        </div>
        </Transition>
        <label class="runtime-toggle">
          <span>
            <span class="toggle-title">Start automatically</span>
            <span class="toggle-hint">Launch the local runtime when Vecura starts</span>
          </span>
          <input v-model="autoStartLocal" type="checkbox" @change="saveAutoStart" />
          <span class="toggle-track"><span class="toggle-thumb"></span></span>
        </label>
      </div>

      <!-- Backend selection + download -->
      <div class="field-block">
        <label class="field-label">Download runtime</label>
        <div class="field">
          <div class="select-wrap" :class="{ open: backendOpen }">
            <button
              type="button"
              class="field-input select-trigger"
              :disabled="downloading"
              @click="backendOpen = !backendOpen"
              @blur="onBackendBlur"
            >
              <span>{{ backendLabel }}</span>
              <svg class="select-caret" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M6 9l6 6 6-6" />
              </svg>
            </button>
            <div v-if="backendOpen" class="select-menu">
              <button
                v-for="opt in backendOptions"
                :key="opt.value"
                type="button"
                class="select-option"
                :class="{ active: opt.value === selectedBackend }"
                @mousedown.prevent="chooseBackend(opt.value)"
              >{{ opt.label }}</button>
            </div>
          </div>
          <button class="field-check" :disabled="downloading" @click="downloadRuntime">
            <LoaderCircle v-if="downloading" class="spin" />
            <span v-else>Download</span>
          </button>
        </div>
        <Transition name="progress-fade">
          <div v-if="downloadProgress > 0 && downloading" class="progress-bar">
            <div class="progress-fill" :style="{ width: Math.round(downloadProgress * 100) + '%' }"></div>
          </div>
        </Transition>
      </div>

      <!-- Local Model selection -->
      <div class="field-block">
        <label class="field-label">Model (.gguf)</label>
        <div class="field">
          <input
            v-model="localModelPath"
            class="field-input"
            placeholder="Path to model.gguf"
            readonly
          />
          <button class="field-check" @click="browseLocalModel">Browse…</button>
        </div>
      </div>

      <div class="field-block">
        <label class="field-label">Embedding Dimension</label>
        <div class="field">
          <input
            v-model.number="localDim"
            class="field-input"
            type="number"
            placeholder="512"
          />
          <button v-if="!llamaStatus.serverOK" class="primary-btn" style="height:30px;font-size:0.82rem" :disabled="!localModelPath || localStarting" @click="applyLocalModel">
            <LoaderCircle v-if="localStarting" class="spin" />
            <span v-else>Start Server</span>
          </button>
          <button v-if="llamaStatus.serverOK" class="ghost-btn danger" style="height:30px;font-size:0.82rem;margin-left:6px" :disabled="serverStopping" @click="stopServer">
            <LoaderCircle v-if="serverStopping" class="spin" />
            <span v-else>Stop Server</span>
          </button>
        </div>
      </div>

      <!-- App Data -->
      <div class="section-title">App Data</div>
      <button class="ghost-btn danger" :disabled="clearing" @click="clearDB">
        <LoaderCircle v-if="clearing" class="spin" />
        <span v-else>Clear Data</span>
      </button>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { LoaderCircle, Eye, EyeOff } from '@lucide/vue'
import { call } from '../api.js'
import { PickFolder, GetModelInfo, DetectGPU, GetLLamaStatus, DownloadLLama, SelectGGUFFile, SetLocalModel, StopLLamaServer } from '../../wailsjs/go/api/App.js'

const props = defineProps({
  models: { type: Array, default: () => [] },
  activeKey: { type: String, default: '' },
  folderPath: { type: String, default: '' },
})
const emit = defineEmits(['models-changed', 'use-model', 'scan-folder'])

const ready = ref(false)
const backendError = ref('')
const appVersion = ref('dev')
const updateInfo = ref(null)
const updateChecking = ref(false)
const updateDownloading = ref(false)
const presets = ref([])
const provider = ref('openai')
const baseUrl = ref('')
const apiKey = ref('')
const showKey = ref(false)
const checking = ref(false)
const fetchedModels = ref([])
const selectedModel = ref('')
const adding = ref(false)
const selectedInfo = ref(null)
const folderPath = ref('')
const modelQuery = ref('')
const modelsCollapsed = ref(false)
const providerCollapsed = ref(false)
const manualModel = ref('')
const clearing = ref(false)
const savedProviders = ref({})  // provider id -> { baseUrl, apiKey } from config
// A saved URL alone is not enough to trigger a network request on startup.
// This records that the user previously completed a provider check.
const providerChecks = ref({})
const providerChecked = computed(() => !!providerChecks.value[provider.value])
const activeModelConnected = computed(() => {
  if (!selectedInfo.value) return false
  if (selectedInfo.value.key === 'local/llm') return llamaStatus.value.serverOK
  return selectedInfo.value.provider === provider.value && urlState.value === 'valid'
})

// Local model state
const gpuInfo = ref(null)
const gpuLoading = ref(false)
const llamaStatus = ref({ installed: false, version: '', backend: '', serverOK: false })
const selectedBackend = ref('cuda-12.4')
const autoStartLocal = ref(false)
const backendOpen = ref(false)
const backendOptions = [
  { value: 'cuda-12.4', label: 'CUDA 12.4 (NVIDIA)' },
  { value: 'cuda-13.3', label: 'CUDA 13.3 (NVIDIA)' },
  { value: 'vulkan', label: 'Vulkan (AMD/Intel)' },
  { value: 'cpu', label: 'CPU only' },
]
const backendLabel = computed(() => {
  const o = backendOptions.find((x) => x.value === selectedBackend.value)
  return o ? o.label : selectedBackend.value
})
function chooseBackend(v) {
  selectedBackend.value = v
  backendOpen.value = false
}
function onBackendBlur() {
  // Close shortly after blur so option mousedown still registers.
  setTimeout(() => { backendOpen.value = false }, 120)
}
const downloading = ref(false)
const downloadProgress = ref(0)
const localModelPath = ref('')

const localDim = ref(512)
const localStarting = ref(false)
const serverStopping = ref(false)

const urlState = ref('')   // '', 'valid', 'invalid'
const keyState = ref('')   // '', 'valid', 'invalid'

// Split fetched models into embedding (top) and the rest, both filtered by
// the name search box.
const embedModels = computed(() =>
  fetchedModels.value.filter((m) => m.isEmbed)
)
const otherModels = computed(() =>
  fetchedModels.value.filter((m) => !m.isEmbed)
)
const filterByName = (list) => {
  const q = modelQuery.value.trim().toLowerCase()
  if (!q) return list
  return list.filter((m) => m.id.toLowerCase().includes(q))
}
const visibleEmbed = computed(() => filterByName(embedModels.value))
const visibleOther = computed(() => filterByName(otherModels.value))

async function onProvider(isRestore = false) {
  const p = presets.value.find((x) => x.id === provider.value)
  if (p) {
    baseUrl.value = p.baseUrl
    // If the system provides a key via env, use it automatically so the
    // user does not have to paste it manually.
    if (p.keyFromEnv) {
      apiKey.value = p.keyFromEnv
    }
  }
  // Restore any previously saved connection for this specific provider so
  // switching away and back keeps the user's base URL / API key.
  const saved = savedProviders.value[provider.value]
  if (saved) {
    if (saved.baseUrl) baseUrl.value = saved.baseUrl
    if (saved.apiKey) apiKey.value = saved.apiKey
  }
  // Only clear discovered models/selection when the user actively switches
  // providers — not while restoring saved state on mount.
  if (!isRestore) {
    fetchedModels.value = []
    selectedModel.value = ''
  }
  resetCheck()
}

// Switch provider (user action): apply its connection, then persist.
function selectProvider(id) {
  provider.value = id
  onProvider(false)
  persist()
}

function resetCheck() {
  urlState.value = ''
  keyState.value = ''
}

async function check(isAuto = false) {
  if (!baseUrl.value) return ElMessage.warning('Base URL required')
  checking.value = true
  urlState.value = ''
  keyState.value = ''
  try {
    const list = await call('CheckProvider', baseUrl.value, apiKey.value)
    // Backend already returns only embedding-capable models (it asks the
    // provider to filter by the embeddings modality, with a client-side
    // heuristic fallback), so show the list as-is.
    fetchedModels.value = list || []
    if (isAuto === true) {
      // Auto-connect on launch: collapse the model list unless we failed to
      // reconnect to the previously active model — in that case the user
      // needs to see the available models to pick a new one.
      modelsCollapsed.value = !!selectedInfo.value
    } else {
      modelsCollapsed.value = false
    }
    urlState.value = 'valid'
    keyState.value = 'valid'
    if (isAuto !== true) {
      ElMessage.success('Key valid — models loaded')
    }
    // Persist the successful check immediately so future launches can
    // validate this provider again. Saving a URL alone must not opt in to
    // startup network access.
    providerChecks.value[provider.value] = true
    persist()
  } catch (e) {
    urlState.value = 'invalid'
    keyState.value = 'invalid'
    fetchedModels.value = []
    if (isAuto) {
      selectedModel.value = ''
      // Connection failed on auto-reconnect: show the (empty) model panel
      // rather than leaving it collapsed, since the user needs to retry.
      modelsCollapsed.value = false
    } else {
      ElMessage.error('Check failed: ' + e)
    }
  } finally {
    checking.value = false
  }
}

async function addModel() {
  if (!selectedModel.value) return
  adding.value = true
  try {
    const info = await call('AddModel', {
      provider: provider.value,
      baseUrl: baseUrl.value,
      apiKey: apiKey.value,
      model: selectedModel.value,
      dim: 0,
      batch: 128,
    })
    selectedInfo.value = info
    modelsCollapsed.value = true
    ElMessage.success('Model added: ' + info.key)
    emit('models-changed')
    emit('use-model', info.key)
    persist()
  } catch (e) {
    ElMessage.error('Add failed: ' + e)
  } finally {
    adding.value = false
  }
}

async function remove(key) {
  await call('RemoveModel', key)
  emit('models-changed')
}

// Use a manually typed model id (e.g. nvidia/nemotron-3-embed-1b:free)
// when the provider's /models list does not expose embedding models.
function useManualModel() {
  const id = manualModel.value.trim()
  if (!id) return ElMessage.warning('Enter a model id')
  selectedModel.value = id
  if (!fetchedModels.value.some((m) => m.id === id)) {
    fetchedModels.value = [{ id, isEmbed: true }, ...fetchedModels.value]
  }
  modelsCollapsed.value = true
  persist()
}

// Select a discovered model and collapse the list so the panel stays tidy.
function selectModel(id) {
  selectedModel.value = id
  modelsCollapsed.value = true
  persist()
}

async function pickFolder() {
  const p = await PickFolder()
  if (p) {
    folderPath.value = p
    emit('scan-folder', p)
    persist()
  }
}

async function clearDB() {
  clearing.value = true
  try {
    await call('ClearDB')
    ElMessage.success('Database cleared')
    emit('models-changed')
  } catch (e) {
    ElMessage.error('Clear failed: ' + e)
  } finally {
    clearing.value = false
  }
}

// ---- Local Model functions -----------------------------------------------

async function detectGPU() {
  gpuLoading.value = true
  try {
    gpuInfo.value = await DetectGPU()
  } catch (e) {
    ElMessage.error('GPU detection failed: ' + e)
  } finally {
    gpuLoading.value = false
  }
}

async function refreshLLamaStatus() {
  try {
    llamaStatus.value = await GetLLamaStatus()
    // The installed runtime is the source of truth for the download selector.
    if (llamaStatus.value.backend) selectedBackend.value = llamaStatus.value.backend
  } catch (_) {}
}

async function saveAutoStart() {
  try {
    await call('SetLocalAutoStart', autoStartLocal.value)
  } catch (e) {
    ElMessage.error('Could not save runtime startup setting: ' + e)
  }
}

async function checkForUpdates(force = false) {
  updateChecking.value = true
  try {
    const result = await call('CheckForUpdates', force)
    if (result) {
      updateInfo.value = result
      if (result.current) appVersion.value = result.current
      if (force && !result.available) ElMessage.success('Vecura is up to date')
      if (result.available && force) ElMessage.info('A new Vecura version is available')
    }
  } catch (e) {
    if (force) ElMessage.error('Update check failed: ' + e)
  } finally {
    updateChecking.value = false
  }
}

async function downloadUpdate() {
  if (!updateInfo.value?.available) return
  updateDownloading.value = true
  try {
    await call('DownloadUpdate', updateInfo.value)
  } catch (e) {
    updateDownloading.value = false
    ElMessage.error('Update failed: ' + e)
  }
}

async function downloadRuntime() {
  downloading.value = true
  downloadProgress.value = 0
  try {
    // Listen for progress events.
    const off = window.runtime && window.runtime.EventsOn
      ? window.runtime.EventsOn('llama:progress', (p) => { downloadProgress.value = p })
      : null
    await DownloadLLama(selectedBackend.value)
    if (off) off()
    downloadProgress.value = 1
    ElMessage.success('Runtime downloaded')
    await refreshLLamaStatus()
  } catch (e) {
    ElMessage.error('Download failed: ' + e)
  } finally {
    downloading.value = false
  }
}

async function browseLocalModel() {
  try {
    const p = await SelectGGUFFile()
    if (p) localModelPath.value = p
  } catch (_) {}
}

async function applyLocalModel() {
  if (!localModelPath.value) return
  localStarting.value = true
  try {
    await SetLocalModel(localModelPath.value, localDim.value || 512)
    try {
      const cfg = await call('GetConfig')
      if (cfg.localModelDim) localDim.value = cfg.localModelDim
    } catch (_) {}
    ElMessage.success('Local model started')
    // SetLocalModel registers the current local embedder as local/llm.
    // Notify the root so scans use the newly loaded model instead of any
    // legacy local/vl key restored from an older configuration.
    emit('use-model', 'local/llm')
    llamaStatus.value = { ...llamaStatus.value, serverOK: true }
  } catch (e) {
    ElMessage.error('Model failed: ' + e)
  } finally {
    localStarting.value = false
  }
}

async function stopServer() {
  serverStopping.value = true
  try {
    await StopLLamaServer()
    ElMessage.success('Server stopped')
    llamaStatus.value = { ...llamaStatus.value, serverOK: false }
  } catch (e) {
    ElMessage.error('Stop failed: ' + e)
  } finally {
    serverStopping.value = false
  }
}

// ---- Persistence ----------------------------------------------------------
// Critical fields (provider, baseUrl, apiKey, selectedModel) are saved
// immediately so closing the app never loses them. Non-critical fields
// (fetchedModels, folderPath) are debounced to avoid excessive writes.
let persistTimer = null
function schedulePersist() {
  if (persistTimer) clearTimeout(persistTimer)
  persistTimer = setTimeout(persist, 400)
}
async function persist() {
  if (persistTimer) { clearTimeout(persistTimer); persistTimer = null }
  try {
    await call('SaveSettings', {
      provider: provider.value,
      baseUrl: baseUrl.value,
      apiKey: apiKey.value,
      selectedModel: selectedModel.value,
      fetchedModels: fetchedModels.value,
      folderPath: folderPath.value,
      providerChecked: providerChecked.value,
    })
    savedProviders.value[provider.value] = {
      baseUrl: baseUrl.value,
      apiKey: apiKey.value,
    }
  } catch (e) {
    console.error('[settings] SaveSettings failed:', e)
  }
}

// Restore previously saved settings on mount.
async function restoreSettings() {
  try {
    const cfg = await call('GetConfig')
    if (!cfg) return
    if (cfg.providers) savedProviders.value = cfg.providers
    if (cfg.providerChecks) providerChecks.value = cfg.providerChecks
    if (cfg.provider) provider.value = cfg.provider
    const pc = cfg.providers && cfg.providers[cfg.provider]
    if (pc) {
      if (pc.baseUrl) baseUrl.value = pc.baseUrl
      if (pc.apiKey) apiKey.value = pc.apiKey
    }
    if (cfg.fetchedModels && cfg.fetchedModels.length) fetchedModels.value = cfg.fetchedModels
    // Backward compatibility for configs written before providerChecks was
    // introduced: cached models or a registered model prove a prior check.
    if ((!cfg.providerChecks || Object.keys(cfg.providerChecks).length === 0) && cfg.provider) {
      const hasRegisteredModel = (cfg.models || []).some((m) => m.provider === cfg.provider)
      if ((cfg.fetchedModels && cfg.fetchedModels.length) || hasRegisteredModel) {
        providerChecks.value[cfg.provider] = true
      }
    }
    if (cfg.selectedModel) selectedModel.value = cfg.selectedModel
    if (cfg.folderPath) folderPath.value = cfg.folderPath
    // Restore local model settings.
    if (cfg.localModelPath) localModelPath.value = cfg.localModelPath

    if (cfg.localModelDim) localDim.value = cfg.localModelDim
    if (cfg.llamaBackend) selectedBackend.value = cfg.llamaBackend
    if (typeof cfg.autoStartLocal === 'boolean') autoStartLocal.value = cfg.autoStartLocal
    // Restore the active model card so it is visible immediately on restart.
    if (cfg.activeModel) {
      const activeKey = cfg.activeModel === 'local/vl' ? 'local/llm' : cfg.activeModel
      try {
        const info = await GetModelInfo(activeKey)
        if (info) selectedInfo.value = info
      } catch (_) {
        // GetModelInfo may fail if the model wasn't re-registered yet.
        // Fall back to ListModels which always reflects the live registry.
        try {
          const all = await call('ListModels')
          const found = all && all.find(m => m.key === activeKey)
          if (found) selectedInfo.value = found
        } catch (_) { /* ignore */ }
      }
    }
  } catch (e) {
    console.error('[settings] GetConfig failed:', e)
  }
}

// Persist whenever any connection/selection field changes.
const restoring = ref(true)
watch(
  [provider, baseUrl, apiKey, selectedModel, fetchedModels, folderPath],
  () => { if (!restoring.value) schedulePersist() },
  { deep: true },
)

onMounted(async () => {
  // Automatic check is throttled by the backend to once per 24 hours.
  checkForUpdates(false)
  try {
    presets.value = await call('ProviderPresets')
    // Restore saved settings FIRST so the user's provider/model is applied
    // before any default fallback.
    await restoreSettings()
    // Only fall back to a default provider if nothing was saved.
    if (!provider.value) {
      const envBacked = presets.value.find((p) => p.keyFromEnv)
      if (envBacked) provider.value = envBacked.id
      else if (presets.value.length) provider.value = presets.value[0].id
    }
    onProvider(true)
    // Fallback so the indexed folder is visible immediately on launch even
    // before the saved config round-trips through GetConfig.
    if (!folderPath.value && props.folderPath) folderPath.value = props.folderPath
    // Refresh only providers that have successfully been checked before.
    // A preset's default URL (notably Ollama's localhost URL) must not cause
    // a connection attempt for a first-time user.
    if (selectedInfo.value && selectedInfo.value.key === 'local/llm') {
      providerCollapsed.value = true
      restoring.value = false
    } else if (baseUrl.value && providerChecked.value) {
      check(true).then(() => { restoring.value = false }).catch(() => { restoring.value = false })
    } else {
      restoring.value = false
    }
    ready.value = true
  } catch (e) {
    backendError.value = String(e)
    console.error('[settings] ProviderPresets failed:', e)
    restoring.value = false
  }
  // Detect GPU and refresh llama status in background (non-blocking).
  detectGPU()
  refreshLLamaStatus()
  // Auto-start happens in the backend and may finish after the first status
  // request. Poll briefly so the active card reflects the actual connection.
  const statusPoll = setInterval(refreshLLamaStatus, 1000)
  setTimeout(() => clearInterval(statusPoll), 10000)
})

// Flush any pending persist when the component is destroyed (e.g. app close).
onBeforeUnmount(() => {
  if (persistTimer) { clearTimeout(persistTimer); persistTimer = null }
  persist()
})
</script>

<style scoped>
.status-fade-enter-active, .status-fade-leave-active,
.progress-fade-enter-active, .progress-fade-leave-active {
  transition: opacity 0.2s var(--ease), transform 0.2s var(--ease);
}
.status-fade-enter-from, .status-fade-leave-to,
.progress-fade-enter-from, .progress-fade-leave-to {
  opacity: 0;
  transform: translateY(-3px);
}
.runtime-toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 12px;
  padding: 10px 12px;
  border: 1px solid var(--hairline);
  border-radius: 12px;
  background: var(--card-material);
  cursor: pointer;
  transition: background 0.2s var(--ease), border-color 0.2s var(--ease), transform 0.2s var(--ease);
}
.runtime-toggle:hover { background: var(--field-bg-focus); border-color: var(--hairline-strong); transform: translateY(-1px); }
.runtime-toggle input { position: absolute; opacity: 0; pointer-events: none; }
.toggle-title { display: block; color: var(--fg); font-size: 0.82rem; font-weight: 600; }
.toggle-hint { display: block; margin-top: 2px; color: var(--fg-3); font-size: 0.7rem; }
.toggle-track {
  width: 38px; height: 22px; padding: 2px; flex: none;
  border-radius: 999px; background: var(--field-border);
  transition: background 0.2s var(--ease);
}
.toggle-thumb {
  display: block; width: 18px; height: 18px; border-radius: 50%;
  background: var(--fg-3); box-shadow: 0 2px 5px rgba(0,0,0,0.25);
  transition: transform 0.22s var(--ease-spring), background 0.2s var(--ease);
}
.runtime-toggle:has(input:checked) .toggle-track { background: var(--accent); }
.runtime-toggle:has(input:checked) .toggle-thumb { transform: translateX(16px); background: #fff; }
.section-title {
  font-size: 0.72rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
  margin: 20px 2px 10px;
}
.section-title:first-child { margin-top: 2px; }

.update-box {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 14px;
  padding: 10px 12px;
  border: 1px solid var(--hairline);
  border-radius: 12px;
  background: var(--card-material);
  transition: background 0.2s var(--ease), border-color 0.2s var(--ease), transform 0.2s var(--ease);
}
.update-box:has(.update-btn) { border-color: rgba(10, 132, 255, 0.38); background: rgba(10, 132, 255, 0.08); }
.update-copy { min-width: 0; flex: 1; }
.update-title { display: block; color: var(--fg); font-size: 0.8rem; font-weight: 600; }
.update-new { display: block; margin-top: 2px; color: var(--fg-3); font-size: 0.7rem; }
.update-btn { height: 30px; padding: 0 12px; font-size: 0.76rem; }

/* Provider segmented chips */
.provider-grid { display: flex; flex-wrap: wrap; gap: 8px; }
.provider-chip {
  flex: 1 1 auto;
  min-width: 84px;
  padding: 9px 12px;
  border-radius: var(--radius-field);
  background: var(--field-bg);
  border: 1px solid var(--field-border);
  color: var(--fg-2);
  font-size: 0.84rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.18s var(--ease);
}
.provider-chip:hover { background: var(--field-bg-focus); color: var(--fg); }
.provider-chip.active {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
  box-shadow: 0 4px 14px rgba(10, 132, 255, 0.4);
}

/* Fields */
.field-block { margin-bottom: 14px; }
.field-label {
  display: block;
  font-size: 0.8rem;
  font-weight: 500;
  color: var(--fg-2);
  margin: 0 2px 6px;
}
.field {
  display: flex;
  align-items: center;
  height: 40px;
  border-radius: var(--radius-field);
  background: var(--field-bg);
  border: 1px solid var(--field-border);
  padding: 0 6px 0 12px;
  transition: background 0.2s var(--ease), border-color 0.2s var(--ease),
    box-shadow 0.2s var(--ease);
}
.field:focus-within {
  background: var(--field-bg-focus);
  border-color: var(--field-border-focus);
  box-shadow: 0 0 0 3px rgba(10, 132, 255, 0.22);
}
.field.valid { border-color: rgba(48, 209, 88, 0.55); box-shadow: 0 0 0 3px rgba(48, 209, 88, 0.16); }
.field.invalid { border-color: rgba(255, 69, 58, 0.6); box-shadow: 0 0 0 3px rgba(255, 69, 58, 0.18); }
.field-input {
  flex: 1;
  min-width: 0;
  height: 100%;
  border: none;
  background: transparent;
  color: var(--fg);
  font-size: 0.88rem;
  font-family: var(--font);
  letter-spacing: -0.01em;
  outline: none;
}
.field-input::placeholder { color: var(--fg-3); }
.field-affix {
  display: grid; place-items: center;
  width: 30px; height: 30px;
  border: none; background: transparent;
  color: var(--fg-3); cursor: pointer; border-radius: 8px;
  transition: background 0.15s, color 0.15s;
}
.field-affix:hover { background: var(--field-bg-focus); color: var(--fg); }
.field-badge {
  display: grid; place-items: center;
  width: 22px; height: 22px; border-radius: 50%;
  margin-left: 6px;
}
.field-badge.ok { color: var(--green); background: rgba(48, 209, 88, 0.14); }
.field-badge.bad { color: var(--red); background: rgba(255, 69, 58, 0.14); }
.field-badge svg { width: 13px; height: 13px; }
.field-check {
  margin-left: 8px;
  height: 30px;
  padding: 0 14px;
  border: none;
  border-radius: 8px;
  background: var(--accent);
  color: #fff;
  font-size: 0.82rem;
  font-weight: 600;
  font-family: var(--font);
  cursor: pointer;
  transition: background 0.15s var(--ease), transform 0.1s var(--ease);
}
.field-check:hover { background: var(--accent-press); }
.field-check:active { transform: scale(0.96); }
.field-check:disabled { opacity: 0.4; cursor: default; }
.field-check.busy { opacity: 0.8; }

/* Model list */
.search-inline { height: 36px; padding: 0 12px; border-radius: var(--radius-field); background: var(--field-bg); border: 1px solid var(--field-border); color: var(--fg); font-size: 0.84rem; font-family: var(--font); outline: none; transition: border-color 0.2s var(--ease), box-shadow 0.2s var(--ease); }
.search-inline:focus { border-color: var(--field-border-focus); box-shadow: 0 0 0 3px rgba(10,132,255,0.22); }
.search-inline::placeholder { color: var(--fg-3); }

.model-sub {
  font-size: 0.72rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg-3);
  margin: 12px 2px 8px;
}
.model-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: 20px 2px 10px;
}
.model-head .collapse-btn {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border-radius: 6px;
  border: 1px solid var(--field-border);
  background: var(--field-bg);
  color: var(--fg-2);
  font-size: 0.9rem;
  line-height: 1;
  cursor: pointer;
  transition: all 0.15s var(--ease);
}
.model-head .collapse-btn:hover { background: var(--field-bg-focus); color: var(--fg); }
.model-row.embed {
  border-color: rgba(10, 132, 255, 0.28);
  background: rgba(10, 132, 255, 0.08);
}
.model-row.embed:hover { background: rgba(10, 132, 255, 0.14); }

.manual { margin-top: 14px; }
.manual-field { padding-right: 6px; }
.manual-field .field-check { margin-left: 8px; }
.model-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 10px 12px;
  border-radius: 12px;
  background: var(--field-bg);
  border: 1px solid var(--field-border);
  color: var(--fg);
  cursor: pointer;
  text-align: left;
  font-family: var(--font);
  transition: all 0.16s var(--ease);
}
.model-row:hover { background: var(--field-bg-focus); }
.model-row.active { border-color: var(--accent); box-shadow: 0 0 0 2px rgba(10, 132, 255, 0.25); }
.model-radio-dot {
  width: 16px; height: 16px; border-radius: 50%;
  border: 2px solid var(--fg-3); flex: none;
  transition: all 0.16s var(--ease);
}
.model-row.active .model-radio-dot { border-color: var(--accent); background: radial-gradient(circle, var(--accent) 0 5px, transparent 6px); }
.model-id { font-weight: 600; font-size: 0.86rem; flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.model-ctx { color: var(--fg-3); font-size: 0.74rem; }

.primary-btn {
  height: 40px;
  flex: none;
  border: none;
  border-radius: 12px;
  background: var(--accent);
  color: #fff;
  font-size: 0.9rem;
  font-weight: 600;
  font-family: var(--font);
  cursor: pointer;
  display: flex; align-items: center; justify-content: center; gap: 8px;
  box-shadow: 0 6px 18px rgba(10, 132, 255, 0.35);
  transition: background 0.15s var(--ease), transform 0.1s var(--ease);
}
.primary-btn:hover { background: var(--accent-press); }
.primary-btn:active { transform: scale(0.98); }
.primary-btn:disabled { opacity: 0.6; cursor: default; }

.active-card {
  display: flex; align-items: center; gap: 10px;
  margin-top: 4px; padding: 10px 12px;
  border-radius: 12px;
  background: var(--card-material);
  border: 1px solid var(--hairline);
  transition: background 0.22s var(--ease), border-color 0.22s var(--ease), box-shadow 0.22s var(--ease);
}
.active-card.connected {
  background: rgba(48, 209, 88, 0.10);
  border-color: rgba(48, 209, 88, 0.30);
  box-shadow: 0 5px 16px rgba(48, 209, 88, 0.10);
}
.active-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--fg-3); flex: none; transition: background 0.22s var(--ease), box-shadow 0.22s var(--ease); }
.active-card.connected .active-dot { background: var(--green); box-shadow: 0 0 8px var(--green); }
.active-key { font-weight: 600; font-size: 0.84rem; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.active-sub { font-size: 0.74rem; color: var(--fg-3); }

/* Registered models */
.reg-list { display: flex; flex-direction: column; gap: 8px; }
.reg-row {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 12px;
  border-radius: 12px;
  background: var(--card-material);
  border: 1px solid var(--hairline);
}
.reg-info { min-width: 0; }
.reg-key { font-weight: 600; font-size: 0.84rem; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.reg-dim { font-size: 0.74rem; color: var(--fg-3); }
.reg-actions { display: flex; gap: 6px; flex: none; }
.active-card > div { min-width: 0; }

.mini-empty {
  padding: 14px;
  text-align: center;
  font-size: 0.82rem;
  color: var(--fg-3);
  border-radius: 12px;
  background: var(--card-material);
  border: 1px dashed var(--hairline-strong);
}

/* Folder card */
.folder-card {
  display: flex; align-items: center; gap: 12px;
  padding: 12px 14px;
  border-radius: 14px;
  background: var(--card-material);
  border: 1px solid var(--hairline);
  cursor: pointer;
  transition: all 0.18s var(--ease);
}
.folder-card:hover {
  background: var(--field-bg-focus);
  border-color: var(--hairline-strong);
  transform: translateY(-1px);
  box-shadow: 0 8px 22px rgba(0, 0, 0, 0.25);
}
.folder-icon {
  width: 38px; height: 38px; border-radius: 10px;
  display: grid; place-items: center;
  background: linear-gradient(160deg, rgba(10,132,255,0.25), rgba(176,106,255,0.25));
  color: var(--accent);
  flex: none;
}
.folder-icon svg { width: 20px; height: 20px; }
.folder-text { flex: 1; min-width: 0; }
.folder-path {
  font-size: 0.82rem; font-weight: 500; color: var(--fg);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.folder-hint { font-size: 0.72rem; color: var(--fg-3); }
.folder-cta {
  font-size: 0.78rem; font-weight: 600; color: var(--accent);
  padding: 6px 10px; border-radius: 8px;
  background: var(--field-bg);
  flex: none;
}

/* Local Model section */
.gpu-row {
  display: flex; align-items: center; gap: 10px; width: 100%;
}
.gpu-text {
  flex: 1; min-width: 0;
  font-size: 0.84rem; color: var(--fg);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
/* Custom backend dropdown — native <select> popup ignores CSS in
   WebView2 (renders a white system widget), so we use a div-based menu
   that fully respects the theme. */
.select-wrap {
  position: relative;
  flex: 1;
  min-width: 0;
  height: 100%;
}
.select-trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  height: 100%;
  border: none;
  background: transparent;
  color: var(--fg);
  font-size: 0.88rem;
  font-family: var(--font);
  letter-spacing: -0.01em;
  cursor: pointer;
  outline: none;
  text-align: left;
}
.select-trigger:disabled { cursor: default; opacity: 0.6; }
.select-caret {
  width: 14px; height: 14px;
  flex: none;
  color: var(--fg-3);
  transition: transform 0.18s var(--ease);
}
.select-wrap.open .select-caret { transform: rotate(180deg); }
.select-menu {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  right: 0;
  z-index: 30;
  padding: 6px;
  border-radius: 12px;
  background: var(--el-bg-color-overlay, rgba(28, 28, 30, 0.96));
  border: 1px solid var(--hairline);
  box-shadow: var(--shadow-pop);
  backdrop-filter: blur(20px) saturate(160%);
  -webkit-backdrop-filter: blur(20px) saturate(160%);
}
.select-option {
  display: block;
  width: 100%;
  text-align: left;
  padding: 8px 10px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--fg);
  font-size: 0.86rem;
  font-family: var(--font);
  cursor: pointer;
}
.select-option:hover { background: var(--field-bg-focus); }
.select-option.active { color: var(--accent); font-weight: 600; }
.progress-bar {
  margin-top: 8px;
  height: 6px;
  border-radius: 3px;
  background: var(--field-bg);
  overflow: hidden;
}
.progress-fill {
  height: 100%;
  border-radius: 3px;
  background: var(--accent);
  transition: width 0.3s var(--ease);
}
</style>
