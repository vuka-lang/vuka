import { readFileSync } from 'node:fs'
import container from 'markdown-it-container'
import { defineConfig } from 'vitepress'

// Code samples use Vuka's own grammar (the VS Code extension's), on top of Go's.
// The Vuka / Go sample preference (theme/style.ts), applied before the
// first paint: ?style= in the address, else the saved choice, else Vuka.
const styleScript = `(function(){var s;try{s=new URLSearchParams(location.search).get('style')}catch(e){}if(s!=='go'&&s!=='vuka'){try{s=localStorage.getItem('vuka-docs-style')}catch(e){}}else{try{localStorage.setItem('vuka-docs-style',s)}catch(e){}}if(s!=='go')s='vuka';document.documentElement.classList.add('prefer-'+s)})()`

// ::: vuka and ::: go blocks show only under their preference; both are
// rendered, so search finds either.
function styleBlock(name: 'vuka' | 'go') {
  return [
    container,
    name,
    {
      render(tokens: any[], idx: number) {
        return tokens[idx].nesting === 1 ? `<div class="style-block ${name}">\n` : '</div>\n'
      },
    },
  ] as const
}

// An error page whose samples are all .vuka is about a .vuka file: under
// the Go preference its Wrong and Right sections give way to a note. A page
// with Go samples, or ::: vuka / ::: go twins, shows as written.
function vukaErrorPages(md: any) {
  md.core.ruler.push('vuka_error_pages', (state: any) => {
    if (!/^errors\/[EW]\d{4}\.md$/.test(state.env?.relativePath ?? '')) return
    const toks = state.tokens
    const styled = toks.some((t: any) => t.type === 'container_vuka_open' || t.type === 'container_go_open')
    const fences = toks.filter((t: any) => t.type === 'fence').map((t: any) => t.info.trim().split(/\s+/)[0])
    if (styled || fences.length === 0 || fences.some((l: string) => l !== 'vuka')) return
    const at = toks.findIndex((t: any) => t.type === 'heading_open' && t.tag === 'h2')
    if (at < 0) return
    const html = (content: string) => Object.assign(new state.Token('html_block', '', 0), { content })
    toks.splice(at, 0,
      html('<div class="style-block go"><div class="tip custom-block"><p>This code is reported for <code>.vuka</code> source, which plain Go has no form of. Switch the samples to Vuka to see them.</p></div></div>\n'),
      html('<div class="style-block vuka">\n'))
    toks.push(html('</div>\n'))
  })
}

// An ORM attribute or auth gate in running text, `@orm.Char{Max: n}`, is
// the value Go passes (to s.Field, or a route as an option): under the Go
// preference it reads `orm.Char{Max: n}`, a bare `@orm.PK` `orm.PK{}`, a
// bare `@auth.Public` `auth.Public()`. Decorators (@orm.Transaction,
// @orm.Entity, @orm.Database) aren't values and stay as written.
function attrValues(md: any) {
  const code = md.renderer.rules.code_inline
  md.renderer.rules.code_inline = (tokens: any[], idx: number, opts: any, env: any, self: any) => {
    const m = /^@((orm|mirror|auth)\.([A-Z]\w*))([\s\S]*)$/.exec(tokens[idx].content)
    if (!m || ['Transaction', 'Entity', 'Database'].includes(m[3])) return code(tokens, idx, opts, env, self)
    const esc = md.utils.escapeHtml
    const at = '<span class="vuka-only">@</span>'
    const rest = esc(m[4]).replace(/(^|\s)@(?=(?:orm|mirror|auth)\.)/g, `$1${at}`)
    const bare = m[2] === 'auth' ? '()' : '{}' // auth's gates are functions, the ORM's attributes structs
    return `<code>${at}${esc(m[1])}${rest}${m[4] ? '' : `<span class="go-only">${bare}</span>`}</code>`
  }
}

const vuka = JSON.parse(readFileSync(new URL('../../editors/vscode/syntaxes/vuka.tmLanguage.json', import.meta.url), 'utf8'))

export default defineConfig({
  title: 'Vuka',
  description: 'Go, with Result, Option, ?, match, overloading, decorators, statics and JSX components. Transpiles to plain Go.',
  base: '/',
  lastUpdated: true,
  cleanUrls: true,
  srcExclude: ['design/**'], // design notes live in the repository, not on the site
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/logo.svg' }],
    ['script', {}, styleScript],
  ],
  markdown: {
    config(md) {
      md.use(...styleBlock('vuka')).use(...styleBlock('go'))
      vukaErrorPages(md)
      attrValues(md)
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
          { text: 'Two ways to write Vuka', link: '/guide/two-ways' },
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
          { text: 'JSX', link: '/features/jsx' },
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
