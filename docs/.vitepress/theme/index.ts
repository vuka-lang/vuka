import DefaultTheme from 'vitepress/theme'
import { h } from 'vue'
import StyleSwitch from './StyleSwitch.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  Layout: () =>
    h(DefaultTheme.Layout, null, {
      'sidebar-nav-before': () => h(StyleSwitch),
      'nav-screen-content-before': () => h(StyleSwitch),
    }),
}
