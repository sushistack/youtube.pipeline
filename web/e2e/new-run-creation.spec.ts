import { expect, test } from '@playwright/test'

test('creates a new run from the production UI and lands on pending guidance', async ({
  page,
}) => {
  const scp_id = `pw-${Date.now()}`
  const expected_run_id = new RegExp(`scp-${scp_id}-run-\\d+`)

  await page.goto('/production')
  await page.getByRole('button', { name: 'Continue to workspace' }).click()

  await expect(
    page.getByRole('button', { name: 'Create a new pipeline run' }),
  ).toBeVisible()

  await page.getByRole('button', { name: 'Create a new pipeline run' }).click()
  await expect(
    page.getByRole('alertdialog', { name: 'Create a new pipeline run' }),
  ).toBeVisible()
  const panel = page.getByRole('alertdialog', { name: 'Create a new pipeline run' })
  await panel.getByRole('textbox', { name: 'SCP ID' }).fill(scp_id)
  await panel.getByRole('button', { name: 'Create', exact: true }).click()

  await expect(page).toHaveURL(expected_run_id)
  await expect(
    page.locator('.run-card').filter({ hasText: `SCP-${scp_id}` }).first(),
  ).toBeVisible()
  await expect(page.getByLabel('Pending run guidance')).toBeVisible()
  await expect(
    page.getByText('Run created. It has not started yet.'),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: 'Copy command' })).toBeVisible()
  await expect(
    page.locator('code').filter({
      hasText: new RegExp(`pipeline resume scp-${scp_id}-run-\\d+`),
    }).first(),
  ).toBeVisible()
})
