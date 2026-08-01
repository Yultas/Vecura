import { createRouter, createWebHashHistory } from 'vue-router'
import SettingsView from './views/SettingsView.vue'
import HelpView from './views/HelpView.vue'
import LoadingView from './views/LoadingView.vue'

const routes = [
  { path: '/', redirect: '/settings' },
  { path: '/settings', name: 'settings', component: SettingsView, meta: { title: 'Settings' } },
  { path: '/help', name: 'help', component: HelpView, meta: { title: 'Help' } },
  { path: '/loading', name: 'loading', component: LoadingView, meta: { title: 'Loading' } },
]

export const router = createRouter({
  history: createWebHashHistory(),
  routes,
})
