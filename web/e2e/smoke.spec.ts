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
