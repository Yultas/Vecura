import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Wails serves the built assets from frontend/dist. Use relative base so the
// app loads regardless of the served URL.
export default defineConfig({
  plugins: [vue()],
  base: './',
  build: {
    outDir: 'dist',
    sourcemap: false,
    emptyOutDir: true,
    target: 'esnext',
  },
  server: {
    port: 34115,
    strictPort: true,
    host: 'localhost',
  },
})
