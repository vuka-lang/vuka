import { readFileSync } from 'node:fs'
import { defineConfig } from 'vitepress'
import container from 'markdown-it-container'
import { writeLLMs } from './llms'

const vuka = JSON.parse(readFileSync(new URL('../../editors/vscode/syntaxes/vuka.tmLanguage.json', import.meta.url), 'utf8'))
const vukaHTML = JSON.parse(readFileSync(new URL('../../editors/vscode/syntaxes/vuka-html.tmLanguage.json', import.meta.url), 'utf8'))
const uiComponents = JSON.parse(readFileSync(new URL('../ui/components/sidebar.json', import.meta.url), 'utf8'))

export default defineConfig({
  title: 'Vuka',
  description: 'Go, with Result, Option, ?, match, overloading, decorators, statics and .vuka.html components. Transpiles to plain Go.',
  base: '/',
  lastUpdated: true,
  cleanUrls: true,
  srcExclude: ['design/**', 'rfc/**'], // design notes and RFCs live in the repository, not on the site
  buildEnd: writeLLMs,
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/logo.svg' }],
    ['link', { rel: 'preconnect', href: 'https://fonts.googleapis.com' }],
    ['link', { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' }],
    ['link', { rel: 'stylesheet', href: 'https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;600&display=swap' }],
  ],
  markdown: {
    toc: { level: [2, 3] },
    config(md) {
      // Diagnostic sources are kept verbatim. Unwrap their legacy sample
      // containers without a preference, hidden content or sample switch.
      for (const name of ['vuka', 'go']) {
        md.use(container, name, { render: () => '' })
      }
    },
    languages: ['go', 'html', 'css', 'typescript', { ...vuka, name: 'vuka', embeddedLangs: ['go'] }, { ...vukaHTML, name: 'vuka-html', embeddedLangs: ['vuka', 'go', 'html', 'css', 'typescript'] }],
  },
  themeConfig: {
    logo: '/logo.svg',
    nav: [
      { text: 'Docs', link: '/guide/getting-started' },
      { text: 'Language', link: '/features/result-option' },
      { text: 'Web', link: '/web/' },
      { text: 'ORM', link: '/orm/' },
      { text: 'Errors', link: '/errors/' },
      { text: 'Playground', link: '/playground' },
      { text: 'Releases', link: '/releases' },
    ],
    outline: { level: [2, 3], label: 'On this page' },
    sidebar: [
      {
        text: 'Prologue',
        collapsed: false,
        items: [
          { text: 'Release Notes', link: '/releases' },
          { text: 'Upgrade Guide', link: '/releases#moving-an-app-from-1-x' },
          { text: 'Versions', link: '/project/versions' },
          { text: 'Compatibility Promise', link: '/project/compatibility' },
          { text: 'Releasing', link: '/project/releasing' },
          { text: 'Credits', link: '/credits' },
        ],
      },
      {
        text: 'Getting Started',
        collapsed: false,
        items: [
          { text: 'Installation', link: '/guide/getting-started' },
          { text: 'Your First Web App', link: '/web/' },
          { text: 'Directory Structure', link: '/web/getting-started' },
          { text: 'Configuration', link: '/web/configuration' },
          { text: 'Project Settings (vuka.toml)', link: '/reference/vuka-toml' },
          { text: 'Deployment', link: '/web/deployment' },
          { text: 'Examples', link: '/examples' },
          { text: 'LLMs & AI Assistants', link: '/guide/llms' },
        ],
      },
      {
        text: 'Architecture Concepts',
        collapsed: false,
        items: [
          { text: 'How It Works', link: '/guide/how-it-works' },
          { text: 'Source Files', link: '/guide/two-ways' },
          { text: 'Shape Files', link: '/ui/' },
          { text: 'Using Go Code', link: '/guide/using-go' },
          { text: 'Working With Go', link: '/guide/go-interop' },
          { text: 'Dependency Injection', link: '/web/dependency-injection' },
        ],
      },
      {
        text: 'The Basics',
        collapsed: false,
        items: [
          { text: 'Routing', link: '/web/routes' },
          { text: 'Folder Routes', link: '/web/folder-routes' },
          { text: 'Middleware', link: '/web/middleware' },
          { text: 'Parameters & Validation', link: '/web/parameters' },
          { text: 'Pages & Layouts', link: '/web/views' },
          { text: 'Parts of a Page', link: '/web/parts' },
          { text: 'Context Processors', link: '/web/context-processors' },
          { text: 'Templates', link: '/ui/templates' },
          { text: 'Components', link: '/ui/components' },
          { text: 'UI Components (Vendored)', link: '/ui/vendored' },
          { text: 'Vendored Components', link: '/ui/components/', collapsed: true, items: uiComponents },
          { text: 'Stateful Components', link: '/ui/stateful' },
          { text: 'Live Pages', link: '/web/live' },
          { text: 'Forms', link: '/ui/forms' },
          { text: 'GraphQL', link: '/web/graphql' },
          { text: 'Client State', link: '/ui/state' },
          { text: 'JS Commands', link: '/ui/js' },
          { text: 'Component Scripts', link: '/ui/scripts' },
          { text: 'Navigation', link: '/web/navigation' },
          { text: 'URL State', link: '/web/url-state' },
          { text: 'Tailwind CSS', link: '/web/styling' },
          { text: 'Styles & Scripts', link: '/web/assets' },
          { text: 'Static Files', link: '/web/static-files' },
          { text: 'Frontends', link: '/web/frontend' },
          { text: 'Islands', link: '/web/islands' },
        ],
      },
      {
        text: 'Digging Deeper',
        collapsed: false,
        items: [
          { text: 'The vuka Command', link: '/tools/cli' },
          { text: 'Editors', link: '/tools/editors' },
          { text: 'vukavet', link: '/tools/vukavet' },
          { text: 'Development Toolbar', link: '/web/toolbar' },
          { text: 'Mail', link: '/web/mail' },
          { text: 'Crons', link: '/web/crons' },
          { text: 'Rendering', link: '/ui/rendering' },
          { text: 'templ Interop', link: '/ui/templ' },
          { text: 'Live Sessions', link: '/ui/live' },
        ],
      },
      {
        text: 'Security',
        collapsed: false,
        items: [
          { text: 'Authentication', link: '/web/auth' },
          { text: 'Guards', link: '/web/folder-routes#guards' },
          { text: 'Security Headers', link: '/web/deployment#security-headers' },
        ],
      },
      {
        text: 'Database',
        collapsed: false,
        items: [
          { text: 'Getting Started', link: '/orm/' },
          { text: 'Models', link: '/orm/models' },
          { text: 'Fields', link: '/orm/fields' },
          { text: 'Queries', link: '/orm/queries' },
          { text: 'Expressions & Functions', link: '/orm/expressions' },
          { text: 'Search', link: '/orm/search' },
          { text: 'Transactions', link: '/orm/transactions' },
          { text: 'Migrations', link: '/orm/migrations' },
          { text: 'Databases', link: '/orm/databases' },
          { text: 'Observability', link: '/orm/observability' },
          { text: 'Errors', link: '/orm/errors' },
        ],
      },
      {
        text: 'Testing',
        collapsed: false,
        items: [
          { text: 'Testing Web Apps', link: '/web/testing' },
          { text: 'Testing Components', link: '/ui/testing' },
          { text: 'Database Testing', link: '/orm/testing' },
        ],
      },
      {
        text: 'Language',
        collapsed: false,
        items: [
          { text: 'Result & Option', link: '/features/result-option' },
          { text: 'The ? Operator', link: '/features/try' },
          { text: 'match', link: '/features/match' },
          { text: 'Enums', link: '/features/enums' },
          { text: 'Overloading', link: '/features/overloading' },
          { text: 'Attributes', link: '/features/attributes' },
          { text: 'Field Attributes & References', link: '/features/fields' },
          { text: 'Decorators', link: '/features/decorators' },
          { text: 'Statics & Self', link: '/features/statics' },
          { text: 'UI Markup', link: '/features/jsx' },
        ],
      },
      {
        text: 'Reference',
        collapsed: false,
        items: [
          { text: 'Errors', link: '/errors/' },
          { text: 'vuka.toml', link: '/reference/vuka-toml' },
          { text: 'Live Protocol', link: '/reference/live-protocol' },
        ],
      },
    ],
    socialLinks: [{ icon: 'github', link: 'https://github.com/vuka-lang/vuka' }],
    editLink: {
      pattern: 'https://github.com/vuka-lang/vuka/edit/main/docs/:path',
      text: 'Edit this page on GitHub',
    },
    search: { provider: 'local' },
    footer: {
      message:
        'Released under the MIT License. Vuka builds on <a href="https://go.dev">Go</a> and its tools, which are made by the Go Authors. Vuka is not affiliated with Google or the Go project. Go is a trademark of Google LLC.',
    },
  },
})
