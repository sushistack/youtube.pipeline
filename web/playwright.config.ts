import { defineConfig } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const port = Number(process.env.PLAYWRIGHT_PORT ?? '4173')
const baseURL = process.env.PLAYWRIGHT_BASE_URL ?? `http://127.0.0.1:${port}`
const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  retries: 0,
  use: {
    baseURL,
    browserName: 'chromium',
    headless: true,
  },
  projects: [
    {
      name: 'chromium',
    },
  ],
  webServer: {
    command: 'npm run serve:e2e',
    cwd: __dirname,
    reuseExistingServer: !process.env.CI,
    stdout: 'pipe',
    stderr: 'pipe',
    timeout: 120_000,
    url: baseURL,
  },
})
