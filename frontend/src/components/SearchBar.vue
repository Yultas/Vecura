<template>
  <div class="search-field" :class="{ focused: focused }">
    <el-icon class="search-icon"><SearchIcon /></el-icon>
    <el-autocomplete
      v-model="query"
      :fetch-suggestions="querySearch"
      placeholder="Search…"
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
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { Search as SearchIcon } from '@lucide/vue'

const props = defineProps({
  recent: { type: Array, default: () => [] },
  activeModel: { type: String, default: '' },
})
const emit = defineEmits(['search'])

const query = ref('')
const focused = ref(false)

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
  emit('search', q)
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
</style>
