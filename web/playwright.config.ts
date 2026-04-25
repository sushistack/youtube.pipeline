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
  // Single worker so the shared Go backend (one webServer instance, one
  // SQLite DB at .tmp/playwright/pipeline.db) is not raced across parallel
  // specs. Per-worker backend isolation would be the long-term fix; for now
  // a single worker keeps the inventory + run state deterministic.
  workers: 1,
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
