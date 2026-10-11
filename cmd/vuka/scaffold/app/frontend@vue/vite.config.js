import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vuka from './vuka/vite.js' // vuka run and vuka build copy it here

// The app serves the build at /app (web.FrontendAt).
export default defineConfig({
  base: '/app/',
  plugins: [vue(), vuka()],
})
