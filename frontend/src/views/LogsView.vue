<template>
  <div class="logs">
    <div class="logs-bar">
      <div class="logs-filters">
        <button
          v-for="lvl in levels"
          :key="lvl"
          class="log-chip"
          :class="[lvl, { off: !enabled[lvl] }]"
          type="button"
          @click="toggle(lvl)"
        >{{ lvl }}</button>
      </div>
      <button class="ghost-btn" type="button" @click="clear">Clear</button>
    </div>

    <div ref="scroller" class="logs-body">
      <div
        v-for="e in visible"
        :key="e.id"
        class="log-line"
        :class="e.level"
      >
        <span class="log-ts">{{ ts(e.ts) }}</span>
        <span class="log-lvl">{{ e.level }}</span>
        <span class="log-msg">{{ e.msg }}</span>
      </div>
      <div v-if="!visible.length" class="log-empty">No logs yet.</div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, nextTick } from 'vue'
import { logStore } from '../logger.js'

const scroller = ref(null)
const levels = ['log', 'info', 'warn', 'error', 'debug']
const enabled = reactive({ log: true, info: true, warn: true, error: true, debug: false })

function toggle(l) { enabled[l] = !enabled[l] }
function clear() { logStore.entries.splice(0) }

const visible = computed(() => logStore.entries.filter((e) => enabled[e.level]))

function pad(n, w = 2) { return String(n).padStart(w, '0') }
function ts(d) {
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
}

function scrollToEnd() {
  const s = scroller.value
  if (s) s.scrollTop = s.scrollHeight
}
onMounted(scrollToEnd)
watch(() => logStore.entries.length, () => nextTick(scrollToEnd))
</script>

<style scoped>
.logs {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}
.logs-bar {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--hairline);
}
.logs-filters { display: flex; flex-wrap: wrap; gap: 6px; }
.log-chip {
  height: 24px;
  padding: 0 10px;
  border-radius: 7px;
  border: 1px solid var(--field-border);
  background: var(--field-bg);
  color: var(--fg-3);
  font-size: 0.7rem;
  font-weight: 600;
  font-family: var(--font);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  cursor: pointer;
  transition: all 0.15s var(--ease);
}
.log-chip.off { opacity: 0.4; }
.log-chip.log { color: var(--fg-2); }
.log-chip.info { color: var(--accent); border-color: rgba(10, 132, 255, 0.4); }
.log-chip.warn { color: var(--orange); border-color: rgba(255, 159, 10, 0.45); }
.log-chip.error { color: var(--red); border-color: rgba(255, 69, 58, 0.5); }
.log-chip.debug { color: var(--fg-3); }
.logs-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 8px 12px 16px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 0.74rem;
  line-height: 1.5;
  user-select: text;
  -webkit-user-select: text;
  cursor: text;
}
/* The global `*` rule sets user-select:none on every element directly, which
   beats inheritance — so we must override the log lines themselves too. */
.logs-body *,
.logs-body {
  user-select: text !important;
  -webkit-user-select: text !important;
}
.log-line {
  display: flex;
  gap: 10px;
  padding: 1px 0;
  border-bottom: 1px solid rgba(128, 128, 128, 0.06);
  white-space: pre-wrap;
  word-break: break-word;
}
.log-ts { color: var(--fg-3); flex: none; }
.log-lvl {
  flex: none;
  width: 46px;
  text-transform: uppercase;
  font-size: 0.66rem;
  font-weight: 700;
  letter-spacing: 0.04em;
}
.log-msg { flex: 1; min-width: 0; color: var(--fg-2); }
.log-line.info .log-lvl { color: var(--accent); }
.log-line.warn .log-lvl { color: var(--orange); }
.log-line.warn .log-msg { color: var(--orange); }
.log-line.error .log-lvl { color: var(--red); }
.log-line.error .log-msg { color: var(--red); }
.log-line.debug .log-lvl { color: var(--fg-3); }
.log-line.debug .log-msg { color: var(--fg-3); }
.log-empty {
  padding: 20px;
  text-align: center;
  color: var(--fg-3);
}
</style>
