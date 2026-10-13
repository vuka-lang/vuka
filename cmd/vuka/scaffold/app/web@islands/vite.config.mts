import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'
import tailwindcss from '@tailwindcss/vite'
import vuka from './vuka/vite.js'

// The island frontend root. vuka build drives this config to compile the
// @ui.Island components (web/islands/*.vue) into chunks it serves from
// /_vuka/i/ — the vuka plugin injects the island entries and reads the
// manifest. Keep the UI plugins (vue, @nuxt/ui, tailwindcss) so an island can
// use Nuxt UI components; they resolve and style out of the box. The config
// is .mts because @nuxt/ui/vite is ESM-only.
export default defineConfig({
  plugins: [vue(), ui(), tailwindcss(), vuka()],
})
