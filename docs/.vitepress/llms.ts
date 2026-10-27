import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import type { SiteConfig } from 'vitepress'

const site = 'https://vuka-lang.github.io'

type Item = { text: string; link?: string; items?: Item[] }

type Page = { title: string; path: string; source: string; summary: string; markdown: string }

// sourceOf maps a sidebar link to its Markdown file: /web/ is web/index.md,
// /web/routes is web/routes.md. Links into a page (#…) and other sites are
// not pages of their own.
function sourceOf(srcDir: string, link: string): string | undefined {
  if (!link.startsWith('/') || link.includes('#')) return
  const rel = link.endsWith('/') ? link + 'index.md' : link + '.md'
  const file = join(srcDir, rel)
  return existsSync(file) ? rel.slice(1) : undefined
}

function absolute(link: string): string {
  return link.startsWith('/') ? site + link : link
}

// clean turns a page's source into plain Markdown: no frontmatter, sample
// context comments, containers or table-of-contents markers; heading ids
// dropped; Go-only wording kept as an aside; links absolute.
function clean(md: string): string {
  return md
    .replace(/^---\n[\s\S]*?\n---\n/, '')
    .replace(/<!--[\s\S]*?-->\n?/g, '')
    .replace(/^\[\[toc\]\]\n/gm, '')
    .replace(/^:::.*\n/gm, '')
    .replace(/^(#{1,6} .*?)\s*\{#[\w-]+\}\s*$/gm, '$1')
    .replace(/<span class="vuka-only">([\s\S]*?)<\/span>/g, '$1')
    .replace(/<span class="go-only">([\s\S]*?)<\/span>/g, ' (in plain Go: $1)')
    .replace(/\]\((\/[^)\s]*)\)/g, (_, l) => '](' + absolute(l) + ')')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}

// summary is a page's description: its frontmatter description, else the
// first paragraph of prose, on one line.
function summary(md: string): string {
  const fm = md.match(/^---\n([\s\S]*?)\n---\n/)
  const desc = fm?.[1].match(/^description:\s*(.+)$/m)
  if (desc) return desc[1].replace(/^['"]|['"]$/g, '')
  let fence = false
  const lines: string[] = []
  for (const line of clean(md).split('\n')) {
    if (line.startsWith('```')) fence = !fence
    if (fence || line.startsWith('```')) continue
    const prose = line.trim() !== '' && !/^(#|>|\||[-*+] |\d+\. |<|!\[)/.test(line.trim())
    if (prose) lines.push(line.trim())
    else if (lines.length) break
  }
  const text = lines.join(' ').replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
  let out = ''
  for (const sentence of text.split(/(?<=[\w)`'"*][.!?])\s+/)) {
    if (out && out.length + sentence.length > 280) break
    out += (out ? ' ' : '') + sentence
  }
  return out.trim()
}

function pagesOf(srcDir: string, items: Item[], seen: Set<string>): Page[] {
  const out: Page[] = []
  for (const it of items) {
    const source = it.link && sourceOf(srcDir, it.link)
    if (source && !seen.has(source)) {
      seen.add(source)
      const md = readFileSync(join(srcDir, source), 'utf8')
      const path = '/' + source
      out.push({ title: it.text, path, source, summary: summary(md), markdown: clean(md) })
    }
    if (it.items) out.push(...pagesOf(srcDir, it.items, seen))
  }
  return out
}

// writeLLMs writes /llms.txt (an index of the docs in the sidebar's order),
// /llms-full.txt (every page in that order) and each page as Markdown next
// to its HTML (/web/routes.md), for language models and the tools around
// them.
export function writeLLMs(config: SiteConfig) {
  const { srcDir, outDir } = config
  const sidebar = (config.site.themeConfig.sidebar ?? []) as Item[]
  const seen = new Set<string>()
  const groups = sidebar.map((g) => ({ text: g.text, pages: pagesOf(srcDir, g.items ?? [], seen) }))

  const index = [
    '# Vuka',
    '',
    `> ${config.site.description}`,
    '',
    'Vuka is a Go toolchain: `.vuka` files (Go with Result, Option, `?`, match, attributes) and `.vuka.html` templates transpile to plain Go, with a web framework (routes, folder routes, live pages, forms, GraphQL, auth), a UI component library and an ORM.',
    '',
    `Every page below is also served as Markdown at the linked URL. The whole documentation in one file: ${site}/llms-full.txt. An error code's page: \`vuka explain <code>\` prints it offline.`,
    '',
  ]
  for (const g of groups) {
    if (!g.pages.length) continue
    index.push(`## ${g.text}`, '')
    for (const p of g.pages) index.push(`- [${p.title}](${site}${p.path})${p.summary ? ': ' + p.summary : ''}`)
    index.push('')
  }

  const full = [`# Vuka documentation`, '', `> ${config.site.description}`, '', `Source: ${site}`, '']
  for (const g of groups) {
    for (const p of g.pages) full.push('---', '', `<!-- ${site}${p.path} -->`, '', p.markdown, '')
  }

  writeFileSync(join(outDir, 'llms.txt'), index.join('\n'))
  writeFileSync(join(outDir, 'llms-full.txt'), full.join('\n'))
  for (const g of groups) {
    for (const p of g.pages) {
      const file = join(outDir, p.source)
      mkdirSync(dirname(file), { recursive: true })
      writeFileSync(file, p.markdown + '\n')
    }
  }
}
