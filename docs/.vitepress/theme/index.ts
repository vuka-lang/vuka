import { defineAsyncComponent } from 'vue'
import DefaultTheme from 'vitepress/theme'
import './custom.css'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('Playground', defineAsyncComponent(() => import('./playground/Playground.vue')))
  },
}
