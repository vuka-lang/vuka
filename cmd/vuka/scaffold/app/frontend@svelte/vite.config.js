import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import vuka from './vuka/vite.js' // vuka run and vuka build copy it here

// The app serves the build at /app (web.FrontendAt).
export default defineConfig({
  base: '/app/',
  plugins: [svelte(), vuka()],
})
