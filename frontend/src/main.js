import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import App from './App.vue'
import { router } from './router.js'
import { installLogCapture } from './logger.js'
import './style.css'

// Icons are imported directly from @lucide/vue where used, so the full
// @element-plus/icons-vue set (~300 components) does not need to be
// registered globally here.
const app = createApp(App)
app.use(ElementPlus)
app.use(router)
installLogCapture()
app.mount('#app')
