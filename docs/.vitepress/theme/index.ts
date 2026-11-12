import { defineAsyncComponent } from 'vue'
import DefaultTheme from 'vitepress/theme'
import './custom.css'
import UiExample from './ui/UiExample.vue'
import UiPageActions from './ui/UiPageActions.vue'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('UiExample', UiExample)
    app.component('UiPageActions', UiPageActions)
    app.component('Playground', defineAsyncComponent(() => import('./playground/Playground.vue')))
  },
}
