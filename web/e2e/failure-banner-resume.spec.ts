import { expect, test } from './fixtures'
import { ProductionShellPO } from './po/production-shell.po'
import { installApiMocks, makeSpies } from './po/mock-api'

// UI-E2E-10 — FailureBanner → Enter Resume (P1)
// UX-DR 16. Asserts the keyboard resume contract: a failed run surfaces the
// FailureBanner; pressing Enter dispatches POST /api/runs/:id/resume; the
// banner unmounts once the run flips to running.

const RUN_ID = 'fail-001'

test('Enter on the failure banner resumes the run', async ({
  page,
  skipOnboarding,
  consoleGuard,
}) => {
  void skipOnboarding
  const spies = makeSpies()
  await installApiMocks(page, {
    state: {
      runs: [
        {
          id: RUN_ID,
          scp_id: 'fail',
          stage: 'review',
          status: 'failed',
          retry_reason: 'rate_limit',
        },
      ],
    },
    spies,
  })

  const shell = new ProductionShellPO(page)
  await shell.gotoAndDismissOnboarding(RUN_ID)

  const banner = shell.failureBanner()
  await expect(banner).toBeVisible()
  await expect(
    banner.getByRole('heading', { name: /rate limit/i }),
  ).toBeVisible()

  // Press Enter — the FailureBanner's keyboard shortcut is registered with a
  // 'context' scope and triggers regardless of focus target as long as the
  // active element is not editable, which our default body focus satisfies.
  await page.keyboard.press('Enter')

  await expect.poll(() => spies.resumeCount.value).toBe(1)

  // Banner unmounts after the run flips to running on the next status fetch.
  await expect(shell.failureBanner()).toHaveCount(0)

  expect(consoleGuard.consoleErrors).toEqual([])
  expect(consoleGuard.pageErrors).toEqual([])
})

test('Resume button click is equivalent to Enter (a11y fallback)', async ({
  page,
  skipOnboarding,
}) => {
  void skipOnboarding
  const spies = makeSpies()
  await installApiMocks(page, {
    state: {
      runs: [
        {
          id: RUN_ID,
          scp_id: 'fail',
          stage: 'review',
          status: 'failed',
          retry_reason: 'unknown_failure',
        },
      ],
    },
    spies,
  })

  const shell = new ProductionShellPO(page)
  await shell.gotoAndDismissOnboarding(RUN_ID)

  await expect(shell.failureBanner()).toBeVisible()
  // The resume CTA contains both the [Enter] hint span and the "Resume" label.
  await page
    .getByRole('button')
    .filter({ hasText: /resume/i })
    .first()
    .click()
  await expect.poll(() => spies.resumeCount.value).toBe(1)
})
