import { expect, test } from './fixtures'
import { ProductionShellPO } from './po/production-shell.po'
import { installApiMocks, makeSpies } from './po/mock-api'

// UI-E2E-02 — Dashboard → Scenario Inspector → Inline Narration Edit (P0)
// Risk link: deferred 7-2 (InlineNarrationEditor baseline re-sync).
// Asserts: blur-commit persists, reload restores edited text, Ctrl+Z reverts
// to the previously saved baseline.

const RUN_ID = 'edit-001'

test('persists narration edits on blur and reverts via Ctrl+Z', async ({
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
          scp_id: 'edit',
          stage: 'scenario_review',
          status: 'waiting',
        },
      ],
      scenes: {
        [RUN_ID]: [
          { scene_index: 0, narration: 'original-text' },
          { scene_index: 1, narration: 'second scene' },
          { scene_index: 2, narration: 'third scene' },
        ],
      },
    },
    spies,
  })

  const shell = new ProductionShellPO(page)
  await shell.gotoAndDismissOnboarding(RUN_ID)

  await expect(shell.scenarioInspector()).toBeVisible()

  // Step 1: open scene 1 (scene_index=0) by clicking the read cell.
  const readCell = shell.narrationReadCell(0)
  await expect(readCell).toBeVisible()
  await readCell.click()

  // Step 2: type the new value, then blur to commit.
  const textarea = shell.narrationTextarea(0)
  await expect(textarea).toBeFocused()
  await textarea.fill('edited-text')
  await textarea.blur()

  // Server received the PATCH; spy validates payload.
  await expect.poll(() => spies.narrationEdits.length).toBeGreaterThan(0)
  expect(spies.narrationEdits[0]).toMatchObject({
    runId: RUN_ID,
    scene_index: 0,
    narration: 'edited-text',
  })

  // Read view should now reflect the saved narration.
  await expect(shell.narrationReadCell(0)).toContainText('edited-text')

  // Step 3: reload — persistence regression guard.
  await page.reload()
  await expect(shell.narrationReadCell(0)).toContainText('edited-text')

  // Step 4: re-enter edit mode and Ctrl+Z. The baseline at edit-mode entry is
  // the saved "edited-text"; revert restores that, NOT "original-text"
  // (deferred 7-2 baseline re-sync — Story 7.2 fix verifies this).
  await shell.narrationReadCell(0).click()
  const textarea2 = shell.narrationTextarea(0)
  await expect(textarea2).toBeFocused()
  await textarea2.fill('mid-typing-noise')
  await page.keyboard.press('Control+z')
  await expect(textarea2).toHaveValue('edited-text')

  expect(consoleGuard.consoleErrors).toEqual([])
  expect(consoleGuard.pageErrors).toEqual([])
})
