// Safe Wails backend bindings accessor.
// Wails injects window.go and window.runtime; they may not be ready at the
// exact moment the app mounts, so we poll briefly before giving up.

let ready = false
function isReady() {
  return !!(window.go && window.go.api && window.go.api.App && window.runtime)
}

function waitReady(timeoutMs = 4000) {
  return new Promise((resolve) => {
    if (isReady()) return resolve(true)
    const start = Date.now()
    const t = setInterval(() => {
      if (isReady() || Date.now() - start > timeoutMs) {
        clearInterval(t)
        ready = isReady()
        resolve(ready)
      }
    }, 50)
  })
}

export async function call(method, ...args) {
  await waitReady()
  const App = window.go && window.go.api && window.go.api.App
  if (!App || typeof App[method] !== 'function') {
    throw new Error('Backend method not available: ' + method)
  }
  return App[method](...args)
}

export function eventsOn(name, cb) {
  if (window.runtime && window.runtime.EventsOn) window.runtime.EventsOn(name, cb)
}

export function openDirectory() {
  if (window.runtime && window.runtime.OpenDirectoryDialog) {
    return window.runtime.OpenDirectoryDialog({ Title: 'Select image folder' })
  }
  return Promise.resolve('')
}
