import { expect, test } from './fixtures'
import { ProductionShellPO } from './po/production-shell.po'
import { installApiMocks } from './po/mock-api'
import type { RunStage, RunStatus } from './po/mock-api'

// UI-E2E-09 — Run Inventory Search + Filter (P1)
// UX-DR 63. The Sidebar inventory exposes a single search input that performs
// a client-side substring filter across run.id, run.scp_id, run.stage,
// run.status. Stage- and status-only filter widgets are NOT implemented; the
// test exercises the substring contract that the current UI actually offers.

const RUNS: Array<{ id: string; scp_id: string; stage: RunStage; status: RunStatus }> = [
  { id: 'scp-alpha-run-1', scp_id: 'alpha', stage: 'complete', status: 'completed' },
  { id: 'scp-beta-run-1', scp_id: 'beta', stage: 'complete', status: 'completed' },
  { id: 'scp-gamma-run-1', scp_id: 'gamma', stage: 'review', status: 'failed' },
  { id: 'scp-delta-run-1', scp_id: 'delta', stage: 'image', status: 'running' },
  { id: 'scp-epsilon-run-1', scp_id: 'epsilon', stage: 'review', status: 'waiting' },
]

test('search input narrows the inventory by SCP id, stage, and status', async ({
  page,
  skipOnboarding,
  consoleGuard,
}) => {
  void skipOnboarding
  await installApiMocks(page, {
    state: { runs: RUNS },
  })

  const shell = new ProductionShellPO(page)
  await shell.gotoAndDismissOnboarding()

  await expect(shell.runCard()).toHaveCount(RUNS.length)

  const searchbox = shell.runInventorySearch()
  await expect(searchbox).toBeVisible()

  await test.step('typing "alp" narrows to only the alpha run', async () => {
    await searchbox.fill('alp')
    await expect(shell.runCard()).toHaveCount(1)
    await expect(shell.runCardFor('alpha')).toBeVisible()
  })

  await test.step('clearing and typing a stage substring filters by stage', async () => {
    await searchbox.fill('')
    // 'image' uniquely identifies the delta run via its stage.
    await searchbox.fill('image')
    await expect(shell.runCard()).toHaveCount(1)
    await expect(shell.runCardFor('delta')).toBeVisible()
  })

  await test.step('clearing and typing "failed" filters by status', async () => {
    await searchbox.fill('')
    await searchbox.fill('failed')
    await expect(shell.runCard()).toHaveCount(1)
    await expect(shell.runCardFor('gamma')).toBeVisible()
  })

  await test.step('clearing the input restores all runs', async () => {
    await searchbox.fill('')
    await expect(shell.runCard()).toHaveCount(RUNS.length)
  })

  expect(consoleGuard.consoleErrors).toEqual([])
  expect(consoleGuard.pageErrors).toEqual([])
})
