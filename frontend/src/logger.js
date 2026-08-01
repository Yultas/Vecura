import { reactive } from 'vue'

// Ring buffer of captured application logs, rendered by LogsView.
const MAX = 2000
export const logStore = reactive({ entries: [] })

let seq = 0

function fmt(a) {
  if (typeof a === 'string') return a
  if (a instanceof Error) return a.stack || a.message
  if (a && typeof a === 'object') {
    try { return JSON.stringify(a) } catch { /* fall through */ }
  }
  return String(a)
}

export function pushLog(level, args) {
  logStore.entries.push({
    id: seq++,
    ts: new Date(),
    level,
    msg: Array.prototype.map.call(args, fmt).join(' '),
  })
  if (logStore.entries.length > MAX) {
    logStore.entries.splice(0, logStore.entries.length - MAX)
  }
}

let installed = false

// installLogCapture mirrors every console.* call (and uncaught errors) into
// logStore so the Logs tab shows the full application log stream.
export function installLogCapture() {
  if (installed) return
  installed = true

  const orig = {
    log: console.log.bind(console),
    info: console.info.bind(console),
    warn: console.warn.bind(console),
    error: console.error.bind(console),
    debug: console.debug.bind(console),
  }

  const wrap = (lvl) => (...args) => {
    try { pushLog(lvl, args) } catch { /* ignore */ }
    orig[lvl](...args)
  }
  console.log = wrap('log')
  console.info = wrap('info')
  console.warn = wrap('warn')
  console.error = wrap('error')
  console.debug = wrap('debug')

  if (typeof window !== 'undefined') {
    window.addEventListener('error', (e) => {
      pushLog('error', ['[window.error]', e.message, (e.filename || '') + ':' + (e.lineno || '?')])
    })
    window.addEventListener('unhandledrejection', (e) => {
      const r = e.reason
      pushLog('error', ['[unhandledrejection]', (r && (r.stack || r.message)) || r])
    })
  }
}
