// Takes the console screenshots shown on the website. See README.md.
//
//   node shoot.mjs <outDir>
//
// Needs a Vite dev server for gui/frontend at GUI_URL (default http://localhost:5199), Chrome at CHROME
// (default: macOS), playwright-core and cwebp. The Go bindings are replaced by mock.js.
import {chromium} from 'playwright-core'
import {execFileSync} from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {fileURLToPath} from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const mock = fs.readFileSync(path.join(here, 'mock.js'), 'utf8')
const outDir = path.resolve(process.argv[2] || 'shots')
const url = process.env.GUI_URL || 'http://localhost:5199/'
const chrome = process.env.CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'forgeai-shots-'))

// Sidebar position of each view.
const views = {overview: 0, jobs: 1, sandboxes: 2, volumes: 4, resources: 7}

fs.mkdirSync(outDir, {recursive: true})
const browser = await chromium.launch({executablePath: chrome})
for (const lang of ['en', 'zh']) {
  for (const theme of ['light', 'dark']) {
    const ctx = await browser.newContext({
      viewport: {width: 1440, height: 1000}, deviceScaleFactor: 1.5, colorScheme: theme,
      locale: lang === 'zh' ? 'zh-CN' : 'en-US',
    })
    await ctx.addInitScript(mock)
    await ctx.addInitScript(([l, t]) => {
      localStorage.setItem('forgeai.locale', l)
      localStorage.setItem('forgeai.theme', t)
    }, [lang, theme])
    const page = await ctx.newPage()
    page.on('pageerror', e => console.error('pageerror', e.message))
    await page.goto(url)
    await page.waitForTimeout(1500)
    for (const [name, index] of Object.entries(views)) {
      await page.locator('.nav-item').nth(index).click()
      await page.waitForTimeout(1200)
      const png = path.join(tmp, `${name}.png`)
      await page.screenshot({path: png})
      execFileSync('cwebp', ['-quiet', '-q', '82', png, '-o', path.join(outDir, `${name}-${lang}-${theme}.webp`)])
    }
    await ctx.close()
  }
}
await browser.close()
fs.rmSync(tmp, {recursive: true})
