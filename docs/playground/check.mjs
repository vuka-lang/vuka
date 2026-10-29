// Checks the playground in Chrome: it loads and transpiles, an edit updates
// the Go, an error shows a squiggle and a linked code, and a share link
// reproduces the project. Screenshots go to SHOTS when it is set.
//
//	npm run build && npx vitepress preview --port 4173 &
//	node playground/check.mjs http://localhost:4173/playground
import { existsSync, mkdirSync } from 'node:fs'
import puppeteer from 'puppeteer-core'

const url = process.argv[2] ?? 'http://localhost:4173/playground'
const shots = process.env.SHOTS
const chrome =
  process.env.CHROME ??
  ['/Applications/Google Chrome.app/Contents/MacOS/Google Chrome', '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].find(existsSync)
if (!chrome) throw new Error('no Chrome: set CHROME')
if (shots) mkdirSync(shots, { recursive: true })

const browser = await puppeteer.launch({ executablePath: chrome, headless: true, args: ['--no-sandbox'] })
const fail = (msg) => {
  throw new Error(msg)
}

async function open(href, { theme = 'light', width = 1280, height = 900 } = {}) {
  const page = await browser.newPage()
  await page.setViewport({ width, height })
  await page.evaluateOnNewDocument((t) => localStorage.setItem('vitepress-theme-appearance', t), theme)
  const errors = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.errors = errors
  await page.goto(href, { waitUntil: 'networkidle0' })
  await transpiled(page)
  return page
}

async function transpiled(page) {
  await page.waitForFunction(() => /Transpiled in/.test(document.querySelector('[data-testid=status]')?.textContent ?? ''), { timeout: 60000 })
}

const goText = (page) => page.$eval('[data-testid=go] .cm-content', (el) => el.innerText)
const sourceText = (page) => page.$eval('[data-testid=source] .cm-content', (el) => el.innerText)

async function typeAtEnd(page, text) {
  await page.click('[data-testid=source] .cm-content')
  const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
  await page.keyboard.down(mod)
  await page.keyboard.press('End')
  await page.keyboard.up(mod)
  await page.keyboard.type(text)
}

const page = await open(url)
const go = await goText(page)
if (!go.includes('func (p *Index) Render() ui.Node')) fail(`the Go pane holds no Render:\n${go}`)
if (await page.$('.pg-diags li')) fail('the counter has diagnostics')
if (shots) await page.screenshot({ path: `${shots}/playground-light.png` })

await typeAtEnd(page, '\n<p>{count * 2} doubled</p>')
await page.waitForFunction(() => document.querySelector('[data-testid=go] .cm-content')?.innerText.includes('doubled'), { timeout: 10000 })

await typeAtEnd(page, '\n<p>{Greeting()}</p>')
await page.waitForSelector('[data-testid=source] .cm-lintRange-error', { timeout: 10000 })
const link = await page.$eval('.pg-diags li.error a.pg-code', (a) => ({ code: a.textContent, href: a.getAttribute('href') }))
if (!/^E\d{4}$/.test(link.code) || link.href !== `/errors/${link.code}`) fail(`the error's code links to ${link.href}`)
const where = await page.$eval('.pg-diags li.error .pg-where', (b) => b.textContent)
if (!where.startsWith('pages/index.vuka.html:')) fail(`the error is at ${where}`)
if (shots) await page.screenshot({ path: `${shots}/playground-error-light.png` })

await page.click('button.pg-button:nth-of-type(2)')
await page.waitForFunction(() => location.hash.startsWith('#p='))
const shared = page.url()
const before = await sourceText(page)
const again = await open(shared, { theme: 'dark' })
await again.waitForSelector('[data-testid=source] .cm-lintRange-error', { timeout: 10000 })
if ((await sourceText(again)) !== before) fail('the share link opens another project')
if (shots) await again.screenshot({ path: `${shots}/playground-error-dark.png` })

const dark = await open(url, { theme: 'dark' })
if (shots) await dark.screenshot({ path: `${shots}/playground-dark.png` })
const phone = await open(url, { width: 390, height: 844 })
const stacked = await phone.$$eval('.pg-pane', (ps) => ps[1].getBoundingClientRect().top > ps[0].getBoundingClientRect().bottom - 1)
if (!stacked) fail('the panes are side by side on a phone')
if (shots) await phone.screenshot({ path: `${shots}/playground-phone.png`, fullPage: true })

for (const p of [page, again, dark, phone]) if (p.errors.length) fail(`page errors: ${p.errors.join('; ')}`)
await browser.close()
console.log('playground: ok')
