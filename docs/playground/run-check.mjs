// Checks Run in Chrome against a runner (RUNNER, default
// http://localhost:8090): the counter runs in the iframe with its
// stylesheet applied, a click counts over its websocket, an edit run again
// replaces it. Third-party cookies are blocked, so a runner serving
// programs at /s/<id>/ (RUNNER_RUN_ORIGIN=http://run.localhost:8090) is
// checked without them. Screenshots go to SHOTS when it is set.
//
//	RUNNER_SANDBOX=local RUNNER_UNSAFE_LOCAL=1 go run ./playground/runner &
//	VITE_VUKA_RUNNER=http://localhost:8090 npx vitepress dev &
//	node playground/run-check.mjs http://localhost:5173/playground
import { existsSync, mkdirSync } from 'node:fs'
import puppeteer from 'puppeteer-core'

const url = process.argv[2] ?? 'http://localhost:5173/playground'
const api = process.env.RUNNER ?? 'http://localhost:8090'
const shots = process.env.SHOTS
const chrome =
  process.env.CHROME ??
  ['/Applications/Google Chrome.app/Contents/MacOS/Google Chrome', '/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium'].find(existsSync)
if (!chrome) throw new Error('no Chrome: set CHROME')
if (shots) mkdirSync(shots, { recursive: true })

const browser = await puppeteer.launch({ executablePath: chrome, headless: true, args: ['--no-sandbox', '--test-third-party-cookie-phaseout'] })
const fail = (msg) => {
  throw new Error(msg)
}

async function open(theme) {
  const page = await browser.newPage()
  await page.setViewport({ width: 1280, height: 900 })
  await page.evaluateOnNewDocument((t) => localStorage.setItem('vitepress-theme-appearance', t), theme)
  page.errors = []
  page.on('pageerror', (e) => page.errors.push(e.message))
  await page.goto(url, { waitUntil: 'networkidle0' })
  await page.waitForFunction(() => /Transpiled in/.test(document.querySelector('[data-testid=status]')?.textContent ?? ''), { timeout: 60000 })
  return page
}

// program is the iframe's frame once it shows text.
async function program(page, text, not) {
  const deadline = Date.now() + 90000
  while (Date.now() < deadline) {
    for (const f of page.frames()) {
      if (f === page.mainFrame() || (not && f.url() === not)) continue
      const body = await f.evaluate(() => document.body?.innerText ?? '').catch(() => '')
      if (body.includes(text)) return f
    }
    await new Promise((r) => setTimeout(r, 250))
  }
  const log = await page.$eval('[data-testid=log]', (el) => el.innerText).catch(() => '')
  if (shots) await page.screenshot({ path: `${shots}/run-failed.png` })
  fail(`no program shows ${JSON.stringify(text)} (frames ${page.frames().map((f) => f.url())}); log:\n${log}`)
}

// styled fails unless the program's Tailwind stylesheet applied: the h1 is
// text-2xl, the button padded.
async function styled(frame) {
  try {
    await frame.waitForFunction(
      () => {
        const h1 = document.querySelector('h1'), b = document.querySelector('button')
        return h1 && b && getComputedStyle(h1).fontSize === '24px' && getComputedStyle(b).paddingLeft === '12px'
      },
      { timeout: 10000 },
    )
  } catch {
    const got = await frame.evaluate(() => {
      const h1 = document.querySelector('h1'), b = document.querySelector('button')
      return { url: location.href, h1: h1 && getComputedStyle(h1).fontSize, button: b && getComputedStyle(b).paddingLeft, sheets: [...document.styleSheets].map((s) => s.href) }
    })
    fail(`the program's stylesheet didn't apply: ${JSON.stringify(got)}`)
  }
}

const sessions = async () => (await (await fetch(api + '/healthz')).json()).sessions

const page = await open('light')
await page.click('[data-testid=run-button]')
let frame = await program(page, 'Clicked 0 times')
await styled(frame)
console.log('program at', frame.url())
if (shots) await page.screenshot({ path: `${shots}/run-light.png` })

await frame.click('button')
await frame.waitForFunction(() => document.body.innerText.includes('Clicked 1 times'), { timeout: 10000 })
await frame.click('button')
await frame.waitForFunction(() => document.body.innerText.includes('Clicked 2 times'), { timeout: 10000 })
if (shots) await page.screenshot({ path: `${shots}/run-clicked-light.png` })

const first = frame.url()
await page.click('[data-testid=source] .cm-content')
const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
await page.keyboard.down(mod)
await page.keyboard.press('End')
await page.keyboard.up(mod)
await page.keyboard.type('\n<p>Edited and run again</p>')
await page.keyboard.down(mod)
await page.keyboard.press('Enter')
await page.keyboard.up(mod)
frame = await program(page, 'Edited and run again', first)
await styled(frame)
if (frame.url() === first) fail('the run again serves from the first session')
if ((await frame.evaluate(() => document.body.innerText)).includes('Clicked 2 times')) fail('the new run kept the old state')
for (let i = 0; i < 40 && (await sessions()) !== 1; i++) await new Promise((r) => setTimeout(r, 250))
if ((await sessions()) !== 1) fail(`the runner holds ${await sessions()} sessions after the run again`)

const dark = await open('dark')
await dark.click('[data-testid=run-button]')
await styled(await program(dark, 'Clicked 0 times'))
if (shots) await dark.screenshot({ path: `${shots}/run-dark.png` })

for (const p of [page, dark]) if (p.errors.length) fail(`page errors: ${p.errors.join('; ')}`)
await browser.close()
console.log('playground run: ok')
