<template>
  <el-dialog
    :model-value="modelValue"
    @update:model-value="$emit('update:modelValue', $event)"
    :style="{ '--el-dialog-width': calculatedWidth, '--el-dialog-margin-top': '5vh' }"
    class="preview-dialog"
    :show-close="true"
  >
    <div class="preview-wrap" v-if="current">
      <Transition name="fade" mode="out-in">
        <el-image :key="fullUri" :src="fullUri" fit="contain" class="preview-img" v-loading="loadingImg">
          <template #error><div class="img-error">failed to load</div></template>
        </el-image>
      </Transition>
      <div class="preview-meta">
        <div class="path">{{ current.path }}</div>
      </div>
      <div class="nav">
        <button class="nav-btn" @click="step(-1)" aria-label="Previous"><IconArrowLeft /></button>
        <button
          class="nav-btn"
          :class="{ active: showPrompt }"
          :disabled="!current.prompt"
          @click="showPrompt = !showPrompt"
          aria-label="Show prompt"
          title="Show prompt"
        ><IconPrompt /></button>
        <button class="nav-btn" @click="reveal(current.path)" aria-label="Reveal in folder" title="Reveal in folder"><FolderOpen /></button>
        <button class="nav-btn" @click="step(1)" aria-label="Next"><IconArrowRight /></button>
      </div>
    </div>
  </el-dialog>

  <!-- Separate dialog, sized independently of the image so it always fits
       regardless of the image's aspect ratio (portrait vs. wide/landscape). -->
  <el-dialog
    v-model="showPrompt"
    append-to-body
    :show-close="true"
    class="prompt-dialog"
    title="Prompt"
  >
    <div class="prompt-panel-head">
      <span class="prompt-panel-label">Keywords</span>
      <button
        class="copy-btn"
        :class="{ copied }"
        @click="copyPrompt"
        aria-label="Copy prompt"
        :title="copied ? 'Copied' : 'Copy prompt'"
      >
        <IconCheck v-if="copied" />
        <IconCopy v-else />
      </button>
    </div>
    <div class="prompt-text">{{ cleanedPrompt || 'No prompt available' }}</div>
  </el-dialog>
</template>

<script setup>
import { ref, watch, computed, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import {
  ArrowLeft as IconArrowLeft,
  ArrowRight as IconArrowRight,
  FolderOpen,
  MessageSquareText as IconPrompt,
  Copy as IconCopy,
  Check as IconCheck,
} from '@lucide/vue'
import { call } from '../api.js'

const props = defineProps({
  modelValue: Boolean,
  hits: { type: Array, default: () => [] },
  index: { type: Number, default: 0 },
})
const emit = defineEmits(['update:modelValue', 'update:index'])

const fullUri = ref('')
const loadingImg = ref(false)
const calculatedWidth = ref('80%')
const showPrompt = ref(false)
const copied = ref(false)
let copiedTimer = null

const current = computed(() => props.hits[props.index] || null)

// Automatic1111 / ComfyUI-style metadata keys that mark the start of the
// "junk" tail (sampler settings, seed, model info, LoRA hashes, ...)
// appended after the actual prompt text. Everything from the first match
// onward is dropped.
const METADATA_KEYS = [
  'negative prompt', 'steps', 'sampler', 'schedule type', 'cfg scale', 'seed',
  'size', 'model hash', 'model', 'vae hash', 'vae', 'denoising strength',
  'clip skip', 'hires upscale', 'hires upscaler', 'hires steps', 'hires resize',
  'lora hashes', 'ti hashes', 'version', 'adetailer model', 'adetailer confidence',
  'face restoration', 'batch size', 'batch pos', 'ensd', 'eta',
]
const METADATA_RE = new RegExp(
  '(?:^|[,\\n])\\s*(?:' + METADATA_KEYS.map((k) => k.replace(/ /g, '\\s+')).join('|') + ')\\s*:',
  'i'
)

// Strips generation-parameter noise (negative prompt, sampler/seed/model
// settings, LoRA tags, emphasis weights) and returns only the plain,
// de-duplicated keyword list from a raw embedded prompt.
function cleanPrompt(raw) {
  if (!raw) return ''
  const cut = raw.search(METADATA_RE)
  const text = cut === -1 ? raw : raw.slice(0, cut)

  const seen = new Set()
  const keywords = []
  for (let part of text.split(',')) {
    part = part
      .replace(/<[^>]*>/g, '')             // <lora:name:0.8>, <hypernet:...>
      .replace(/[()[\]{}]/g, '')           // emphasis: (word:1.2), ((word)), [word]
      .replace(/:\s*-?\d+(\.\d+)?\s*$/, '') // leftover ":1.2" weight suffix
      .replace(/\s+/g, ' ')
      .trim()
    if (!part || /^break$/i.test(part)) continue
    const key = part.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    keywords.push(part)
  }
  return keywords.join(', ')
}

const cleanedPrompt = computed(() => cleanPrompt(current.value?.prompt || ''))

watch(
  () => [props.modelValue, props.index],
  async () => {
    showPrompt.value = false
    copied.value = false
    if (!props.modelValue || !current.value) return
    loadingImg.value = true
    fullUri.value = ''
    try {
      const uri = await call('ImageDataURI', current.value.path)

      // Calculate width before showing to avoid blinking
      const img = new Image()
      img.onload = () => {
        const aspect = img.width / img.height
        // height is bounded by 70vh
        const maxH = window.innerHeight * 0.7
        const w = Math.min(img.width, maxH * aspect)
        // Set width to image width + padding
        calculatedWidth.value = `min(100%, ${w + 80}px)`
        fullUri.value = uri
        loadingImg.value = false
      }
      img.onerror = () => {
        fullUri.value = uri
        loadingImg.value = false
      }
      img.src = uri
    } catch (e) {
      fullUri.value = ''
      loadingImg.value = false
    }
  },
  { immediate: true }
)

function reveal(path) {
  call('RevealInExplorer', path).catch(err => {
    console.error('Failed to reveal in explorer', err)
  })
}

async function copyPrompt() {
  const text = cleanedPrompt.value
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
  } catch (e) {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    try { document.execCommand('copy') } catch (err) { console.error('Copy fallback failed', err) }
    document.body.removeChild(ta)
  }
  copied.value = true
  ElMessage.success('Скопировано')
  clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => { copied.value = false }, 1500)
}

function step(dir) {
  const n = props.hits.length
  if (!n) return
  emit('update:index', (props.index + dir + n) % n)
}

function onKeydown(e) {
  if (e.key === 'ArrowLeft') step(-1)
  else if (e.key === 'ArrowRight') step(1)
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) window.addEventListener('keydown', onKeydown)
    else window.removeEventListener('keydown', onKeydown)
  }
)
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  clearTimeout(copiedTimer)
})
</script>

<style scoped>
.preview-wrap { display: flex; flex-direction: column; align-items: center; gap: 12px; }
.preview-img { max-height: 70vh; max-width: 100%; border-radius: 12px; }
.preview-meta { text-align: center; color: var(--fg-3); font-size: 0.82rem; word-break: break-all; }
.nav { display: flex; gap: 12px; }
.nav-btn {
  width: 40px; height: 40px;
  display: grid; place-items: center;
  border-radius: 50%;
  border: 1px solid var(--hairline-strong);
  background: var(--field-bg);
  color: var(--fg);
  cursor: pointer;
  transition: all 0.15s var(--ease);
}
.nav-btn:hover { background: var(--field-bg-focus); transform: scale(1.06); }
.nav-btn:active { transform: scale(0.94); }
.nav-btn svg { width: 18px; height: 18px; }
.nav-btn.active {
  background: var(--field-bg-focus);
  color: var(--accent);
  border-color: var(--field-border-focus);
  box-shadow: inset 0 0 0 1px var(--field-border-focus), 0 0 0 2px rgba(10, 132, 255, 0.25);
}
.nav-btn.active:hover { color: var(--accent); }
.nav-btn:disabled { opacity: 0.4; cursor: default; pointer-events: none; }
.img-error { color: var(--fg-3); padding: 20px; }

/* Prompt dialog content (chrome/background comes from the .prompt-dialog
   global override in style.css, since el-dialog is teleported to body). */
.prompt-panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}
.prompt-panel-label {
  font-size: 0.72rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
}
.prompt-text {
  text-align: left;
  font-size: 0.88rem;
  line-height: 1.5;
  color: var(--fg-2);
  white-space: pre-wrap;
  word-break: break-word;
  cursor: text;
  user-select: text;
  -webkit-user-select: text;
}
.copy-btn {
  flex: none;
  width: 26px; height: 26px;
  display: grid; place-items: center;
  border-radius: 50%;
  border: 1px solid var(--hairline-strong);
  background: var(--field-bg);
  color: var(--fg-2);
  cursor: pointer;
  transition: all 0.15s var(--ease);
}
.copy-btn:hover { background: var(--field-bg-focus); color: var(--fg); }
.copy-btn:active { transform: scale(0.94); }
.copy-btn.copied { color: var(--green); border-color: rgba(48, 209, 88, 0.4); }
.copy-btn svg { width: 14px; height: 14px; }

/* Fade transition */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s var(--ease);
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
