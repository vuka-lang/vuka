import { readFileSync } from 'node:fs'
import { defineConfig } from 'vitepress'

// Code samples use Vuka's own grammar (the VS Code extension's), on top of Go's.
const vuka = JSON.parse(readFileSync(new URL('../../editors/vscode/syntaxes/vuka.tmLanguage.json', import.meta.url), 'utf8'))

export default defineConfig({
  title: 'Vuka',
  description: 'Go, with Result, Option, ?, match, overloading, decorators and statics. Transpiles to plain Go.',
  base: '/vuka/',
  lastUpdated: true,
  cleanUrls: true,
  head: [['link', { rel: 'icon', href: '/vuka/icon.svg' }]],
  markdown: {
    languages: ['go', { ...vuka, name: 'vuka', embeddedLangs: ['go'] }],
  },
  themeConfig: {
    logo: '/logo.svg',
    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Features', link: '/features/result-option' },
      { text: 'Tools', link: '/tools/cli' },
      { text: 'Examples', link: '/examples' },
    ],
    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Getting started', link: '/guide/getting-started' },
          { text: 'Using Go code', link: '/guide/using-go' },
          { text: 'How it works', link: '/guide/how-it-works' },
          { text: 'Working with Go', link: '/guide/go-interop' },
        ],
      },
      {
        text: 'What Vuka adds',
        items: [
          { text: 'Result and Option', link: '/features/result-option' },
          { text: 'The ? operator', link: '/features/try' },
          { text: 'match', link: '/features/match' },
          { text: 'Overloading', link: '/features/overloading' },
          { text: 'Attributes', link: '/features/attributes' },
          { text: 'Decorators', link: '/features/decorators' },
          { text: 'Statics and Self', link: '/features/statics' },
          { text: 'Dependency injection', link: '/features/dependency-injection' },
        ],
      },
      {
        text: 'Tools',
        items: [
          { text: 'The vuka command', link: '/tools/cli' },
          { text: 'Editors', link: '/tools/editors' },
        ],
      },
      { text: 'Examples', link: '/examples' },
      { text: 'Credits', link: '/credits' },
    ],
    socialLinks: [{ icon: 'github', link: 'https://github.com/vuka-lang/vuka' }],
    editLink: {
      pattern: 'https://github.com/vuka-lang/vuka/edit/main/docs/:path',
      text: 'Edit this page on GitHub',
    },
    search: { provider: 'local' },
    footer: {
      message:
        'Released under the MIT License. Vuka is built on <a href="https://go.dev">Go</a> and its tools, by the Go Authors; it is not affiliated with Google or the Go project. Go is a trademark of Google LLC.',
    },
  },
})
