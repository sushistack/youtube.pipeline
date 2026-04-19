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

  await expect(page.getByRole('heading', { name: 'Production' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Keyboard-first command rail' })).toBeVisible()
  await expect(page.getByText('Waiting for keyboard command')).toBeVisible()

  await page.keyboard.press('Enter')
  await expect(page.getByText('Approved Shot 1')).toBeVisible()

  await page.keyboard.press('Escape')
  await expect(page.getByText('Rejected Shot 1')).toBeVisible()

  expect(pageErrors).toEqual([])
  expect(consoleErrors).toEqual([])
})

