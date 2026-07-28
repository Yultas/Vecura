<template>
  <div class="search-field" :class="{ focused: focused }">
    <el-icon class="search-icon"><SearchIcon /></el-icon>
    <transition name="prefix-pop">
      <span v-if="detectedPrefix" class="prefix-chip" :class="detectedPrefix">
        @{{ detectedPrefix === 'p' ? 'p' : 'c' }}
        <span class="prefix-label">{{ detectedPrefix === 'p' ? 'prompt' : 'image' }}</span>
      </span>
    </transition>
    <el-autocomplete
      v-model="query"
      :fetch-suggestions="querySearch"
      :placeholder="placeholder"
      clearable
      :fit-input-width="true"
      class="search-input"
      @select="onSelect"
      @keyup.enter="submit"
      @focus="focused = true"
      @blur="focused = false"
    >
      <template #default="{ item }">
        <div class="suggest" :title="item.value">{{ item.value }}</div>
      </template>
    </el-autocomplete>
    <button
      v-if="hasVL"
      class="mode-toggle"
      :title="modeLabel"
      @click="cycleMode"
    >
      <component :is="modeIcon" />
    </button>
    <button
      v-if="hasVL"
      class="img-search-btn"
      title="Search by image"
      @click="triggerImageSearch"
    >
      <CameraIcon />
    </button>
    <input
      ref="fileInput"
      type="file"
      accept="image/*"
      style="display:none"
      @change="onFileSelected"
    />
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { Search as SearchIcon, Camera, Eye, Type, Layers } from '@lucide/vue'
import { call } from '../api.js'

const props = defineProps({
  recent: { type: Array, default: () => [] },
  activeModel: { type: String, default: '' },
  hasVL: { type: Boolean, default: false },
})
const emit = defineEmits(['search', 'search-image'])

const query = ref('')
const focused = ref(false)
const fileInput = ref(null)
const searchMode = ref('auto') // 'auto' | 'text' | 'vl'

// A leading @p / @c in the query overrides the UI mode for that search.
const detectedPrefix = computed(() => {
  const q = query.value.trim()
  if (q.startsWith('@p')) return 'p'
  if (q.startsWith('@c')) return 'c'
  return ''
})

// Placeholder teaches the routing prefixes; it hides once the user starts
// typing so it never competes with their input.
const placeholder = computed(() => {
  if (query.value) return ''
  return 'Search (@p prompt · @c image), e.g. @c sunset over mountains'
})

const MODES = ['auto', 'text', 'vl']
const MODE_LABELS = { auto: 'Auto (text + vision)', text: 'Text only', vl: 'Vision only' }
const MODE_ICONS = { auto: Layers, text: Type, vl: Eye }

const modeLabel = computed(() => MODE_LABELS[searchMode.value])
const modeIcon = computed(() => MODE_ICONS[searchMode.value])

function cycleMode() {
  const idx = MODES.indexOf(searchMode.value)
  searchMode.value = MODES[(idx + 1) % MODES.length]
}

function querySearch(q, cb) {
  const list = (props.recent || [])
    .filter((r) => r.toLowerCase().includes(q.toLowerCase()))
    .map((r) => ({ value: r }))
  cb(list)
}

function onSelect(item) {
  query.value = item.value
  submit()
}

function submit() {
  const q = query.value.trim()
  if (!q) return
  // When a routing prefix is present it wins over the mode toggle; the
  // backend parses @p/@c first. Otherwise the selected mode is used.
  emit('search', q, searchMode.value)
}

function triggerImageSearch() {
  fileInput.value.click()
}

function onFileSelected(e) {
  const file = e.target.files && e.target.files[0]
  if (!file) return
  emit('search-image', file)
  e.target.value = '' // reset so same file can be re-selected
}
</script>

<style scoped>
.search-field {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 38px;
  width: 40%;
  margin: 0 auto;
  padding: 0 12px;
  border-radius: var(--radius-field);
  background: var(--field-bg);
  border: 1px solid var(--field-border);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.04);
  transition: background 0.2s var(--ease), border-color 0.2s var(--ease),
    box-shadow 0.2s var(--ease), transform 0.12s var(--ease);
}
.search-field.focused {
  background: var(--field-bg-focus);
  border-color: var(--field-border-focus);
  box-shadow: 0 0 0 3px rgba(10, 132, 255, 0.22),
    inset 0 1px 0 rgba(255, 255, 255, 0.06);
}
.search-icon { color: var(--fg-3); font-size: 15px; flex: none; }
.prefix-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex: none;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 0.78rem;
  font-weight: 600;
  letter-spacing: 0.01em;
  line-height: 1;
}
.prefix-chip.p { background: rgba(10, 132, 255, 0.16); color: #4aa3ff; }
.prefix-chip.c { background: rgba(48, 209, 88, 0.16); color: #30d158; }
.prefix-label { font-weight: 500; opacity: 0.85; }
.prefix-pop-enter-active,
.prefix-pop-leave-active { transition: opacity 0.15s var(--ease), transform 0.15s var(--ease); }
.prefix-pop-enter-from,
.prefix-pop-leave-to { opacity: 0; transform: scale(0.9); }
.search-input { flex: 1; }
.search-input :deep(.el-input__wrapper) {
  background: transparent !important;
  box-shadow: none !important;
  padding: 0 !important;
}
.search-input :deep(.el-input__inner) {
  color: var(--fg);
  font-size: 0.92rem;
  letter-spacing: -0.01em;
}
.search-input :deep(.el-input__inner::placeholder) { color: var(--fg-3); }
.search-input :deep(.el-input__clear) { color: var(--fg-3); }
.suggest {
  padding: 2px 0;
  font-size: 0.88rem;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.img-search-btn {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border: none;
  background: transparent;
  color: var(--fg-3);
  cursor: pointer;
  border-radius: 8px;
  flex: none;
  transition: background 0.15s, color 0.15s;
}
.img-search-btn:hover {
  background: var(--field-bg-focus);
  color: var(--fg);
}
.img-search-btn :deep(svg) { width: 16px; height: 16px; }
.mode-toggle {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border: none;
  background: transparent;
  color: var(--fg-3);
  cursor: pointer;
  border-radius: 8px;
  flex: none;
  transition: background 0.15s, color 0.15s;
}
.mode-toggle:hover {
  background: var(--field-bg-focus);
  color: var(--fg);
}
.mode-toggle :deep(svg) { width: 16px; height: 16px; }
</style>
