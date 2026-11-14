// Real packaged LOCALFIRST browser check. No Exo simulator or model is started.
import { spawn } from 'node:child_process'
import { readFile, readdir, mkdir, writeFile, access } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createServer } from 'node:net'
import { chromium, expect } from '../web/node_modules/@playwright/test/index.mjs'

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const output = resolve(repo, '..', '.tools')
await mkdir(output, { recursive: true })
const packages = join(repo, 'offline-package')
let root
for (const name of (await readdir(packages)).filter(name => name.startsWith('windows-x64-')).sort().reverse()) {
  const candidate = join(packages, name)
  try { await access(join(candidate, 'localfirst-offline.exe')); await access(join(candidate, 'web', '.localfirst-offline.json')); root = candidate; break } catch { /* Ignore incomplete builds. */ }
}
if (!root) throw new Error('Build and package the Windows offline workspace first.')
async function unusedPort() {
  const socket = createServer()
  await new Promise((resolve, reject) => { socket.once('error', reject); socket.listen(0, '127.0.0.1', resolve) })
  const port = socket.address().port
  await new Promise((resolve, reject) => socket.close(error => error ? reject(error) : resolve()))
  return port
}
const port = await unusedPort(), absentPort = await unusedPort()
const url = `http://127.0.0.1:${port}`
const app = spawn(join(root, 'localfirst-offline.exe'), ['-addr', `127.0.0.1:${port}`], { cwd: root, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] })
let stderr = ''
app.stderr.on('data', data => { stderr += data.toString() })
let browser
try {
  let ready = false
  for (let attempt = 0; attempt < 60; attempt++) {
    if (app.exitCode !== null) throw new Error(`Helper exited: ${stderr}`)
    try { ready = (await (await fetch(`${url}/healthz`)).json()).server === 'localfirst-offline' } catch { /* Starting. */ }
    if (ready) break
    await new Promise(resolve => setTimeout(resolve, 200))
  }
  if (!ready) throw new Error('Helper did not start.')
  const metadata = JSON.parse(await readFile(join(root, 'web', '.localfirst-offline.json'), 'utf8'))
  const health = await (await fetch(`${url}/healthz`)).json()
  expect(health.cache_version).toBe(metadata.cacheVersion)
  expect(health.native_version).toBe(metadata.nativeVersion)
  browser = await chromium.launch({ channel: 'chrome', headless: true })
  const context = await browser.newContext({ serviceWorkers: 'allow', viewport: { width: 1440, height: 1000 } })
  const page = await context.newPage()
  const errors = [], external = [], aiRequests = []
  page.on('pageerror', error => errors.push(error.message))
  context.on('request', request => {
    const location = new URL(request.url())
    if (['http:', 'https:'].includes(location.protocol) && location.origin !== url) external.push(request.url())
    if (location.pathname.startsWith('/api/local-ai/')) aiRequests.push(location.pathname)
  })
  await page.goto(`${url}/api/offline-launch`)
  await expect(page.getByRole('heading', { name: 'Good work starts here.' })).toBeVisible({ timeout: 60000 })
  expect(aiRequests).toEqual([])
  await page.goto(`${url}/docs`)
  await page.getByRole('button', { name: 'New Document', exact: true }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel(/Name/i).fill('AI failure isolation check')
  await dialog.getByRole('button', { name: 'Create', exact: true }).click()
  const editor = page.locator('.ProseMirror')
  await expect(editor).toHaveAttribute('contenteditable', 'true')
  await editor.fill('Optional AI failures preserve this saved document.')
  await expect(page.getByText('Saved on this device', { exact: true })).toBeVisible()
  const documentURL = page.url()
  await page.getByRole('link', { name: 'Local AI Cluster', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Local AI Cluster', exact: true })).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: /^Disabled$/ })).toBeVisible()
  expect(aiRequests).toEqual(['/api/local-ai/bootstrap'])
  await page.getByRole('button', { name: 'Configure Exo' }).click()
  await page.getByLabel('Local Exo endpoint', { exact: true }).fill(`http://127.0.0.1:${absentPort}`)
  await page.getByRole('button', { name: 'Probe Exo service', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('Exo unavailable', { timeout: 15000 })
  await expect(page.getByRole('status').filter({ hasText: /^Disabled$/ })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Enable Local AI', exact: true })).toHaveCount(0)
  await page.screenshot({ path: join(output, 'local-ai-desktop.png'), fullPage: true })
  const finalHelp = page.getByText('AI restarts disabled and forgets all trusted devices.', { exact: false })
  await finalHelp.scrollIntoViewIfNeeded()
  await expect(finalHelp).toBeInViewport()
  await page.goto(documentURL)
  await expect(editor).toContainText('Optional AI failures preserve this saved document.')
  await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller), { timeout: 30000 }).toBe(true)
  await context.setOffline(true)
  await page.reload()
  await expect(editor).toContainText('Optional AI failures preserve this saved document.')
  await editor.fill('Still editable and saved while fully offline after an AI failure.')
  await expect(page.getByText('Saved on this device', { exact: true })).toBeVisible()
  await page.reload()
  await expect(editor).toContainText('Still editable and saved while fully offline after an AI failure.')
  await context.setOffline(false)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto(`${url}/local-ai`)
  await expect(page.getByRole('heading', { name: 'Local AI Cluster', exact: true })).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: /^Disabled$/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  const workspace = page.getByTestId('local-ai-workspace')
  expect(await workspace.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
  expect((await workspace.boundingBox()).width).toBeLessThanOrEqual(390)
  await page.screenshot({ path: join(output, 'local-ai-mobile.png'), fullPage: true })
  expect(errors).toEqual([]); expect(external).toEqual([])
  const result = { passed: true, package: root, cacheVersion: metadata.cacheVersion, nativeVersion: metadata.nativeVersion, checks: ['Packaged helper and lazy AI workspace open', 'No AI request before opening the workspace', 'Disabled workspace only bootstraps locally', 'Missing Exo reports failure without enabling or showing fake devices', 'Saved document remains editable and persists after fully offline reload', 'Mobile workspace has no page overflow', 'No page errors or external requests'], genuineExoInferenceVerified: false, physicalBluetoothTransferVerified: false, aiRequests, errors, external }
  await writeFile(join(output, 'local-ai-browser-results.json'), JSON.stringify(result, null, 2))
  console.log(JSON.stringify(result, null, 2))
} finally { if (browser) await browser.close(); app.kill() }
