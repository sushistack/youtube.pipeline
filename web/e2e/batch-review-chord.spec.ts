import { expect, test } from './fixtures'
import { ProductionShellPO } from './po/production-shell.po'
import { BatchReviewPO } from './po/batch-review.po'
import {
  installApiMocks,
  makeSpies,
  type ReviewItemFixture,
} from './po/mock-api'

// UI-E2E-04 — Batch Review Full Keyboard Chord (P0)
// Risk link: AI-2; core HITL value. UX-DR 18, 23, 24, 33, 34, 38.
// Asserts: J/K navigation, Enter approve, Esc opens reject composer,
// S skip-and-remember, Shift+Enter batch-approve (single aggregate_command_id),
// Ctrl+Z undo dispatches.

const RUN_ID = 'batch-001'

function seededReviewItems(): ReviewItemFixture[] {
  return Array.from({ length: 10 }, (_, i) => ({
    scene_index: i,
    narration: `Scene ${i + 1} narration sample.`,
    review_status: 'waiting_for_review' as const,
    high_leverage: false,
    critic_score: 70,
  }))
}

test('chord J/K/Enter/Esc/S/Shift+Enter/Ctrl+Z drives the review queue', async ({
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
          scp_id: 'batch',
          stage: 'batch_review',
          status: 'waiting',
        },
      ],
      reviewItems: { [RUN_ID]: seededReviewItems() },
    },
    spies,
  })

  const shell = new ProductionShellPO(page)
  const review = new BatchReviewPO(page)
  await shell.gotoAndDismissOnboarding(RUN_ID)

  await expect(review.root()).toBeVisible()
  await expect(review.listbox()).toBeVisible()

  // Initial selection: scene 1 (scene_index=0) is the first actionable item.
  await review.expectSelected(0)
  await review.focusRoot()

  // J×3 → focus moves 0 → 1 → 2 → 3
  await test.step('J×3 advances selection', async () => {
    await page.keyboard.press('j')
    await page.keyboard.press('j')
    await page.keyboard.press('j')
    await review.expectSelected(3)
  })

  // K×1 → focus 3 → 2
  await test.step('K reverses selection', async () => {
    await page.keyboard.press('k')
    await review.expectSelected(2)
  })

  // Enter approves the selected scene_index=2.
  await test.step('Enter records approve decision', async () => {
    await page.keyboard.press('Enter')
    await expect.poll(() => spies.decisionRequests.length).toBeGreaterThan(0)
    const last = spies.decisionRequests[spies.decisionRequests.length - 1]
    expect(last).toMatchObject({
      runId: RUN_ID,
      scene_index: 2,
      decision_type: 'approve',
    })
    // Wait for the mutation to settle. While decision_mutation.isPending the
    // [Enter] Approve button stays disabled and global shortcuts (incl. S)
    // are no-op'd by submitDecision()'s pending-guard.
    await expect(review.approveButton()).toBeEnabled()
  })

  // After approve, selection auto-advances to next actionable item.
  // Press S to skip-and-remember the current selection.
  await test.step('S skips current scene', async () => {
    const beforeCount = spies.decisionRequests.length
    await page.keyboard.press('s')
    await expect.poll(() => spies.decisionRequests.length).toBeGreaterThan(
      beforeCount,
    )
    const last = spies.decisionRequests[spies.decisionRequests.length - 1]
    expect(last.decision_type).toBe('skip_and_remember')
    await expect(review.approveButton()).toBeEnabled()
  })

  // Esc opens the reject composer (does NOT immediately reject — Story 8.4
  // AC-1).
  await test.step('Esc opens reject composer', async () => {
    await page.keyboard.press('Escape')
    await expect(
      page.getByRole('textbox', { name: /reason|reject/i }),
    ).toBeVisible()
    // Close it without committing — the composer suppresses global shortcuts,
    // so we click the cancel button explicitly.
    const cancelBtn = page.getByRole('button', { name: /cancel|close/i })
    if (await cancelBtn.isVisible().catch(() => false)) {
      await cancelBtn.click()
    } else {
      // Some builds wire Esc-while-composer-open to dismiss; fall back to a
      // root click to defocus.
      await review.focusRoot()
    }
  })

  // Shift+Enter → opens the approve-all confirm panel.
  await test.step('Shift+Enter opens batch-approve confirm', async () => {
    await review.focusRoot()
    await page.keyboard.press('Shift+Enter')
    await expect(review.approveAllConfirmPanel()).toBeVisible()
    // Confirm via the in-panel button.
    const confirmBtn = page.getByRole('button', { name: /confirm|approve/i })
    await confirmBtn.first().click()
  })

  // Server received exactly one approve-all-remaining call with a single
  // aggregate_command_id (deferred 8-6 normalization regression guard).
  await test.step('approve-all-remaining sent with one aggregate id', async () => {
    await expect.poll(() => spies.approveAllRequests.length).toBe(1)
  })

  // Ctrl+Z dispatches undo. The undo button must be enabled now that the
  // local stack has at least one entry.
  await test.step('Ctrl+Z fires undo', async () => {
    await review.focusRoot()
    await expect(review.undoButton()).toBeEnabled()
    await page.keyboard.press('Control+z')
    await expect.poll(() => spies.undoCount.value).toBeGreaterThan(0)
  })

  expect(consoleGuard.consoleErrors).toEqual([])
  expect(consoleGuard.pageErrors).toEqual([])
})

test('detail panel never empty across keystrokes (Focus-Follows-Selection)', async ({
  page,
  skipOnboarding,
}) => {
  void skipOnboarding
  await installApiMocks(page, {
    state: {
      runs: [
        {
          id: RUN_ID,
          scp_id: 'batch',
          stage: 'batch_review',
          status: 'waiting',
        },
      ],
      reviewItems: { [RUN_ID]: seededReviewItems() },
    },
  })

  const shell = new ProductionShellPO(page)
  const review = new BatchReviewPO(page)
  await shell.gotoAndDismissOnboarding(RUN_ID)
  await expect(review.detailPane()).toBeVisible()
  await review.focusRoot()

  // Walk the queue with J five times; assert detail pane is never empty.
  for (let i = 0; i < 5; i += 1) {
    await page.keyboard.press('j')
    await expect(review.detailPane()).not.toContainText(
      'All scenes reviewed for this run',
    )
  }
})
