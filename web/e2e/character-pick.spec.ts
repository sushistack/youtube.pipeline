import { expect, test } from './fixtures'
import { ProductionShellPO } from './po/production-shell.po'
import {
  installApiMocks,
  makeSpies,
  type CharacterCandidateFixture,
} from './po/mock-api'

// UI-E2E-03 — Character Pick → Vision Descriptor → Freeze (P0)
// Phase A→B handoff integrity (UX-DR 17, 41, 62). Asserts that:
//  - The candidate grid responds to digit shortcuts (1–9, 0)
//  - Confirming the selection drops into the descriptor phase
//  - Saving the descriptor POSTs frozen_descriptor + candidate_id
//  - The character_query_key seed boots the grid directly (no search step)

const RUN_ID = 'char-001'

function seededCandidates(): CharacterCandidateFixture[] {
  return Array.from({ length: 5 }, (_, i) => ({
    id: `cand-${i + 1}`,
    title: `Candidate ${i + 1}`,
    image_url: `data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='1' height='1'/>`,
    page_url: `https://example.test/cand-${i + 1}`,
    preview_url: null,
    source_label: 'mock',
  }))
}

test('digit-3 selects, Enter confirms, Save freezes the descriptor', async ({
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
          scp_id: 'char',
          stage: 'character_pick',
          status: 'waiting',
          character_query_key: 'qk-char-001',
        },
      ],
      characters: { [RUN_ID]: seededCandidates() },
      descriptorPrefill: {
        [RUN_ID]: {
          auto: 'A plague doctor in dark robes, ornate beaked mask',
          prior: null,
        },
      },
    },
    spies,
  })

  const shell = new ProductionShellPO(page)
  await shell.gotoAndDismissOnboarding(RUN_ID)

  // Grid auto-focuses because run.character_query_key is set.
  const grid = shell.characterGrid()
  await expect(grid).toBeVisible()
  await expect(grid).toBeFocused()

  // Digit-3 selects candidate at index 2.
  await page.keyboard.press('3')
  await expect(shell.characterCellByDigit('3')).toHaveAttribute(
    'aria-selected',
    'true',
  )

  // Enter confirms → drops into descriptor phase.
  await page.keyboard.press('Enter')
  const descriptorCell = shell.visionDescriptorReadCell()
  await expect(descriptorCell).toBeVisible()

  // Tab into edit, write a frozen descriptor, blur to commit the draft into
  // the parent's descriptor ref (Enter inside the textarea is a newline, not
  // a confirm — confirm only fires from the read-mode cell). Then re-focus
  // the read cell and press Enter to confirm.
  await descriptorCell.focus()
  await page.keyboard.press('Tab')
  const textarea = shell.visionDescriptorTextarea()
  await expect(textarea).toBeFocused()
  await textarea.fill('Frozen reference: tall figure, beaked mask, black coat')
  await textarea.blur()
  await shell.visionDescriptorReadCell().focus()
  await page.keyboard.press('Enter')

  // POST /api/runs/:id/characters/pick fired with the chosen candidate +
  // edited descriptor.
  await expect.poll(() => spies.characterPicks.length).toBeGreaterThan(0)
  expect(spies.characterPicks[0]).toMatchObject({
    runId: RUN_ID,
    candidate_id: 'cand-3',
    frozen_descriptor: 'Frozen reference: tall figure, beaked mask, black coat',
  })

  expect(consoleGuard.consoleErrors).toEqual([])
  expect(consoleGuard.pageErrors).toEqual([])
})

test('digits 1–9 and 0 each select the matching candidate cell', async ({
  page,
  skipOnboarding,
}) => {
  void skipOnboarding
  // Use a 10-candidate set so that 0 maps to the last cell.
  const candidates: CharacterCandidateFixture[] = Array.from(
    { length: 10 },
    (_, i) => ({
      id: `cand-${i + 1}`,
      title: `Candidate ${i + 1}`,
      image_url: `data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='1' height='1'/>`,
      page_url: `https://example.test/cand-${i + 1}`,
    }),
  )
  await installApiMocks(page, {
    state: {
      runs: [
        {
          id: RUN_ID,
          scp_id: 'char',
          stage: 'character_pick',
          status: 'waiting',
          character_query_key: 'qk-char-001',
        },
      ],
      characters: { [RUN_ID]: candidates },
    },
  })

  const shell = new ProductionShellPO(page)
  await shell.gotoAndDismissOnboarding(RUN_ID)

  await expect(shell.characterGrid()).toBeFocused()

  // Table-driven: every digit picks the candidate at its slot.
  const slots: Array<{ key: string; expectedId: string }> = [
    { key: '1', expectedId: 'cand-1' },
    { key: '2', expectedId: 'cand-2' },
    { key: '3', expectedId: 'cand-3' },
    { key: '4', expectedId: 'cand-4' },
    { key: '5', expectedId: 'cand-5' },
    { key: '6', expectedId: 'cand-6' },
    { key: '7', expectedId: 'cand-7' },
    { key: '8', expectedId: 'cand-8' },
    { key: '9', expectedId: 'cand-9' },
    { key: '0', expectedId: 'cand-10' },
  ]

  for (const slot of slots) {
    await page.keyboard.press(slot.key)
    await expect(
      page.locator(`[data-candidate-id="${slot.expectedId}"]`),
    ).toHaveAttribute('aria-selected', 'true')
  }
})
