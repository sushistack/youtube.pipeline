import { expect, test } from './fixtures'
import { ProductionShellPO } from './po/production-shell.po'

// UI-E2E-01 — FR52-web SPA Smoke (P0)
// Risk link: R-06 (root e2e todo + continue-on-error). Regression guard on
// deferred 6-1 (spa.go serving index.html with 200 for /assets/* misses).
// The fresh-boot onboarding click is part of the assertion — do NOT use the
// skipOnboarding fixture here.

test('loads the Go-served SPA shell and honors Enter/Escape keyboard actions', async ({
  page,
  consoleGuard,
}) => {
  const shell = new ProductionShellPO(page)

  await shell.goto()
  await page.getByRole('button', { name: 'Continue to workspace' }).click()

  await shell.expectShellReady()

  await page.keyboard.press('Control+N')
  await expect(shell.newRunPanel()).toBeVisible()

  await shell.newRunPanel().getByRole('textbox', { name: 'SCP ID' }).press('Escape')
  await expect(page.getByRole('alertdialog')).toHaveCount(0)

  expect(consoleGuard.pageErrors).toEqual([])
  expect(consoleGuard.consoleErrors).toEqual([])
})

test('serves real asset 404s (not index.html) for missing bundles', async ({
  page,
}) => {
  // Regression guard on deferred 6-1: spa.go previously returned 200 + the SPA
  // HTML for every unmatched path, including /assets/*.js misses, which
  // hid missing-bundle bugs and stalled Playwright caching.
  const response = await page.request.get('/assets/does-not-exist.js')
  expect(response.status()).toBe(404)

  const body = await response.text()
  expect(body).not.toContain('<!doctype html>')
  expect(body).not.toContain('<html')
})

test('loads the settings workspace alongside the timeline', async ({ page }) => {
  await page.goto('/settings')
  const onboarding = page.getByRole('button', { name: 'Continue to workspace' })
  if (await onboarding.isVisible().catch(() => false)) {
    await onboarding.click()
  }

  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
  await expect(
    page.getByRole('heading', { name: 'Models and cost guardrails' }),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save settings' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Timeline' })).toBeVisible()
})

test('loads the tuning tab with six sections and Shadow gated behind Golden', async ({
  page,
}) => {
  await page.goto('/tuning')
  const onboarding = page.getByRole('button', { name: 'Continue to workspace' })
  if (await onboarding.isVisible().catch(() => false)) {
    await onboarding.click()
  }

  await expect(page.getByRole('heading', { name: 'Tuning', level: 1 })).toBeVisible()

  for (const label of [
    'Critic Prompt',
    'Fast Feedback',
    'Golden Eval',
    'Shadow Eval',
    'Fixture Management',
    'Calibration',
  ]) {
    await expect(page.getByRole('heading', { name: label, level: 2 })).toBeVisible()
  }

  await expect(page.getByLabel('Critic prompt body')).toBeVisible()

  const shadowHeading = page.getByRole('heading', { name: 'Shadow Eval' })
  const shadowSection = page
    .locator('section')
    .filter({ has: shadowHeading })
    .first()
  await expect(
    shadowSection.getByRole('button', { name: /Run Shadow eval/i }),
  ).toBeDisabled()
})
