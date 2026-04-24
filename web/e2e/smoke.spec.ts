import { expect, test } from '@playwright/test'

test('loads the Go-served SPA shell and honors Enter/Escape keyboard actions', async ({
  page,
}) => {
  const consoleErrors: string[] = []
  const pageErrors: string[] = []

  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push(message.text())
    }
  })
  page.on('pageerror', (error) => {
    pageErrors.push(error.message)
  })

  await page.goto('/production')
  await page.getByRole('button', { name: 'Continue to workspace' }).click()

  await expect(page.getByRole('heading', { name: 'Production' })).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Create a new pipeline run' }),
  ).toBeVisible()

  await page.keyboard.press('Control+N')
  const panel = page.getByRole('alertdialog', { name: 'Create a new pipeline run' })
  await expect(panel).toBeVisible()

  await panel.getByRole('textbox', { name: 'SCP ID' }).press('Escape')
  await expect(page.getByRole('alertdialog')).toHaveCount(0)

  expect(pageErrors).toEqual([])
  expect(consoleErrors).toEqual([])
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

  // The prompt editor loads through the backend and should be fillable.
  await expect(page.getByLabel('Critic prompt body')).toBeVisible()

  // Shadow must start disabled until Golden passes in this session (AC-6).
  const shadowHeading = page.getByRole('heading', { name: 'Shadow Eval' })
  const shadowSection = page
    .locator('section')
    .filter({ has: shadowHeading })
    .first()
  await expect(
    shadowSection.getByRole('button', { name: /Run Shadow eval/i }),
  ).toBeDisabled()
})
