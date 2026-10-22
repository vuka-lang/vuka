import { readFileSync } from 'node:fs'
import { defineConfig } from 'vitepress'
import container from 'markdown-it-container'

const vuka = JSON.parse(readFileSync(new URL('../../editors/vscode/syntaxes/vuka.tmLanguage.json', import.meta.url), 'utf8'))

export default defineConfig({
  title: 'Vuka',
  description: 'Go, with Result, Option, ?, match, overloading, decorators, statics and .vuka.html components. Transpiles to plain Go.',
  base: '/',
  lastUpdated: true,
  cleanUrls: true,
  srcExclude: ['design/**', 'rfc/**'], // design notes and RFCs live in the repository, not on the site
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/logo.svg' }],
  ],
  markdown: {
    config(md) {
      // Diagnostic sources are kept verbatim. Unwrap their legacy sample
      // containers without a preference, hidden content or sample switch.
      for (const name of ['vuka', 'go']) {
        md.use(container, name, { render: () => '' })
      }
    },
    languages: ['go', { ...vuka, name: 'vuka', embeddedLangs: ['go'] }],
  },
  themeConfig: {
    logo: '/logo.svg',
    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Features', link: '/features/result-option' },
      { text: 'Tools', link: '/tools/cli' },
      { text: 'UI', link: '/ui/' },
      { text: 'Web', link: '/web/' },
      { text: 'ORM', link: '/orm/' },
      { text: 'Examples', link: '/examples' },
      { text: 'Releases', link: '/releases' },
    ],
    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Getting started', link: '/guide/getting-started' },
          { text: 'Source files', link: '/guide/two-ways' },
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
          { text: 'Enums', link: '/features/enums' },
          { text: 'Overloading', link: '/features/overloading' },
          { text: 'Attributes', link: '/features/attributes' },
          { text: 'Field attributes and references', link: '/features/fields' },
          { text: 'Decorators', link: '/features/decorators' },
          { text: 'Statics and Self', link: '/features/statics' },
          { text: 'Dependency injection', link: '/features/dependency-injection' },
          { text: 'UI markup', link: '/features/jsx' },
        ],
      },
      {
        text: 'Tools',
        items: [
          { text: 'The vuka command', link: '/tools/cli' },
          { text: 'Editors', link: '/tools/editors' },
          { text: 'vukavet', link: '/tools/vukavet' },
        ],
      },
      {
        text: 'UI',
        collapsed: false,
        items: [
          { text: 'Introduction', link: '/ui/' },
          { text: 'Templates', link: '/ui/templates' },
          { text: 'Components', link: '/ui/components' },
          { text: 'Stateful components', link: '/ui/stateful' },
          { text: 'JS commands', link: '/ui/js' },
          { text: 'Client state', link: '/ui/state' },
          { text: 'Rendering', link: '/ui/rendering' },
          { text: 'templ interop', link: '/ui/templ' },
          { text: 'Live sessions', link: '/ui/live' },
          { text: 'Testing', link: '/ui/testing' },
        ],
      },
      {
        text: 'Web',
        collapsed: false,
        items: [
          { text: 'Introduction', link: '/web/' },
          { text: 'Getting started', link: '/web/getting-started' },
          { text: 'Routes', link: '/web/routes' },
          { text: 'Folder routes', link: '/web/folder-routes' },
          { text: 'Parameters and results', link: '/web/parameters' },
          { text: 'Views', link: '/web/views' },
          { text: 'Styling with Tailwind', link: '/web/styling' },
          { text: 'Styles and scripts', link: '/web/assets' },
          { text: 'Dependency injection', link: '/web/dependency-injection' },
          { text: 'Middleware', link: '/web/middleware' },
          { text: 'Authentication', link: '/web/auth' },
          { text: 'Live components', link: '/web/live' },
          { text: 'Navigation', link: '/web/navigation' },
          { text: 'Frontends', link: '/web/frontend' },
          { text: 'Islands', link: '/web/islands' },
          { text: 'Configuration and .env', link: '/web/configuration' },
          { text: 'Mail', link: '/web/mail' },
          { text: 'Crons', link: '/web/crons' },
          { text: 'Routers and deployment', link: '/web/deployment' },
          { text: 'Testing', link: '/web/testing' },
          { text: 'Development toolbar', link: '/web/toolbar' },
        ],
      },
      {
        text: 'ORM',
        collapsed: false,
        items: [
          { text: 'Introduction', link: '/orm/' },
          { text: 'Models', link: '/orm/models' },
          { text: 'Fields', link: '/orm/fields' },
          { text: 'Queries', link: '/orm/queries' },
          { text: 'Expressions and functions', link: '/orm/expressions' },
          { text: 'Search', link: '/orm/search' },
          { text: 'Transactions', link: '/orm/transactions' },
          { text: 'Migrations', link: '/orm/migrations' },
          { text: 'Databases', link: '/orm/databases' },
          { text: 'Observability', link: '/orm/observability' },
          { text: 'Errors', link: '/orm/errors' },
          { text: 'Testing', link: '/orm/testing' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'Errors', link: '/errors/' },
          { text: 'Live protocol', link: '/reference/live-protocol' },
        ],
      },
      {
        text: 'Project',
        items: [
          { text: 'Versions and compatibility', link: '/project/versions' },
          { text: 'Compatibility promise', link: '/project/compatibility' },
          { text: 'Releases', link: '/releases' },
          { text: 'Releasing (maintainers)', link: '/project/releasing' },
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
        'Released under the MIT License. Vuka builds on <a href="https://go.dev">Go</a> and its tools, which are made by the Go Authors. Vuka is not affiliated with Google or the Go project. Go is a trademark of Google LLC.',
    },
  },
})
