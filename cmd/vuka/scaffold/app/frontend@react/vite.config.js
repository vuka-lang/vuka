import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import vuka from './vuka/vite.js' // vuka run and vuka build copy it here

// The app serves the build at /app (web.FrontendAt).
export default defineConfig({
  base: '/app/',
  plugins: [react(), vuka()],
})
