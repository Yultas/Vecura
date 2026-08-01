<template>
  <div ref="scroller" class="grid-scroller" @scroll="onScroll">
    <div v-if="!hits.length && !loading" class="empty">
      <div class="empty-art">
        <svg viewBox="0 0 64 64" width="56" height="56">
          <rect x="8" y="14" width="48" height="36" rx="8" fill="none" stroke="currentColor" stroke-width="2.5"/>
          <circle cx="23" cy="28" r="5" fill="none" stroke="currentColor" stroke-width="2.5"/>
          <path d="M14 46l12-12 8 8 10-10 10 10" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
      </div>
      <div class="empty-title">No results yet</div>
      <div class="empty-sub">Type a query above to search your indexed images.</div>
    </div>

    <div v-if="loading" class="loading">
      <LoaderCircle class="spin" /> Searching…
    </div>

    <div class="grid-inner" :style="{ height: innerHeight + 'px' }">
      <div
        v-for="h in visibleHits"
        :key="h.id + '-' + h.path"
        class="cell"
        :style="cellStyle(h._idx)"
        @click="$emit('open', h._idx)"
      >
        <el-image
          :src="thumbnailSrc(h.path)"
          fit="contain"
          lazy
          class="thumb"
        >
          <template #error>
            <div class="img-error">no thumb</div>
          </template>
        </el-image>

      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { LoaderCircle } from '@lucide/vue'

const props = defineProps({
  hits: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
})
defineEmits(['open'])

const COL_W = 184
const ROW_H = 196
const THUMBNAIL_PREFIX = '/local-thumbs/'
const scroller = ref(null)
const scrollTop = ref(0)
const viewH = ref(600)
const viewW = ref(0)
const cols = ref(5)

// Search relevance is useful for finding images, but the gallery itself is
// presented chronologically: newest source files first. Keep the original
// object references out of the mutation and assign indices after sorting so
// opening a tile still points at the correctly ordered preview item.
const indexed = computed(() =>
  props.hits
    .map((h) => ({ ...h }))
    .sort((a, b) => {
      const byDate = (Number(b.date) || 0) - (Number(a.date) || 0)
      return byDate || (Number(b.id) || 0) - (Number(a.id) || 0)
    })
    .map((h, i) => ({ ...h, _idx: i }))
)
const rows = computed(() => Math.ceil(indexed.value.length / cols.value))
const innerHeight = computed(() => rows.value * ROW_H)
const gridOffset = computed(() => {
  const totalW = cols.value * COL_W
  return Math.max(0, viewW.value - totalW) / 2
})

function onResize() {
  if (!scroller.value) return
  viewH.value = scroller.value.clientHeight
  viewW.value = scroller.value.clientWidth
  cols.value = Math.max(1, Math.floor(scroller.value.clientWidth / COL_W))
}

const visibleHits = computed(() => {
  const firstRow = Math.max(0, Math.floor(scrollTop.value / ROW_H) - 2)
  const lastRow = Math.min(rows.value, Math.ceil((scrollTop.value + viewH.value) / ROW_H) + 2)
  const first = firstRow * cols.value
  const last = Math.min(indexed.value.length, lastRow * cols.value)
  return indexed.value.slice(first, last)
})

function thumbnailSrc(path) {
  const filename = path.split(/[\\/]/).pop()
  return filename ? `${THUMBNAIL_PREFIX}${encodeURIComponent(filename)}.png` : ''
}

function cellStyle(idx) {
  const col = idx % cols.value
  const row = Math.floor(idx / cols.value)
  return {
    left: gridOffset.value + col * COL_W + 'px',
    top: row * ROW_H + 'px',
    width: COL_W - 14 + 'px',
    height: ROW_H - 14 + 'px',
  }
}

// Scroll events fire far more often than the display can repaint (every
// pixel on a trackpad/high-poll-rate mouse). Updating scrollTop directly on
// each event forces visibleHits/cellStyle to recompute and re-render on
// every one of those events, which is what made the grid feel stuttery.
// Coalescing updates to at most once per animation frame fixes that without
// losing responsiveness.
let scrollRAF = 0
let latestScrollTop = 0
function onScroll(e) {
  latestScrollTop = e.target.scrollTop
  if (scrollRAF) return
  scrollRAF = requestAnimationFrame(() => {
    scrollRAF = 0
    scrollTop.value = latestScrollTop
  })
}

let ro
onMounted(async () => {
  await nextTick()
  onResize()
  ro = new ResizeObserver(onResize)
  if (scroller.value) ro.observe(scroller.value)
})
onBeforeUnmount(() => {
  ro && ro.disconnect()
  if (scrollRAF) cancelAnimationFrame(scrollRAF)
})
</script>

<style scoped>
.grid-scroller {
  height: 100%;
  overflow-y: auto;
  position: relative;
}
.grid-inner { position: relative; width: 100%; }
.cell {
  position: absolute;
  border-radius: 14px;
  overflow: hidden;
  cursor: pointer;
  background: var(--card-material);
  border: 1px solid var(--hairline);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.28);
  transition: transform 0.22s var(--ease), box-shadow 0.22s var(--ease),
    border-color 0.22s var(--ease);
  -webkit-backdrop-filter: blur(8px) saturate(160%);
  backdrop-filter: blur(8px) saturate(160%);
}
.cell:hover {
  transform: translateY(-4px) scale(1.015);
  box-shadow: 0 16px 36px rgba(0, 0, 0, 0.42);
  border-color: var(--hairline-strong);
}
.cell:active { transform: translateY(-1px) scale(0.99); }
.thumb { width: 100%; height: 100%; display: block; }
.img-error { font-size: 0.7rem; color: var(--fg-3); display: flex; height: 100%; align-items: center; justify-content: center; }

.empty {
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  text-align: center;
  color: var(--fg-3);
}
.empty-art {
  width: 96px; height: 96px;
  border-radius: 24px;
  display: grid; place-items: center;
  color: var(--fg-3);
  background: var(--card-material);
  border: 1px solid var(--hairline);
  -webkit-backdrop-filter: blur(10px);
  backdrop-filter: blur(10px);
  margin-bottom: 6px;
}
.empty-title { font-size: 1.02rem; font-weight: 600; color: var(--fg-2); letter-spacing: -0.01em; }
.empty-sub { font-size: 0.85rem; color: var(--fg-3); max-width: 280px; }

.loading { padding: 40px; text-align: center; color: var(--fg-3); font-size: 0.88rem; }
.loading .is-loading { margin-right: 6px; }
</style>
