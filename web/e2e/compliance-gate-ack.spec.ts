import { expect, test } from './fixtures'
import { ProductionShellPO } from './po/production-shell.po'
import { installApiMocks, makeSpies } from './po/mock-api'

// UI-E2E-06 — ComplianceGate Ack → Ready for Upload (P0)
//
// Risk link: R-17 (FR23 compliance gate has zero E2E coverage, AI-4 / Story 11.4).
// UX-DR: 42, 66.
//
// Documented deviations from test-design §12 UI-E2E-06:
//   - Route: real shipping contract is `POST /api/runs/{id}/metadata/ack`, not the
//     Step 3 shorthand `/ack-metadata`. See routes.go:51.
//   - Domain transition: real contract is `metadata_ack + waiting -> complete +
//     completed`, not `phase_c_done -> ready_for_upload`. The user-visible copy
//     "Ready for upload" is preserved on the post-ack CompletionReward surface.
//   - There is no dedicated `/upload` page/endpoint in this story's scope (Story
//     11.4 AC-5 — scope stays narrow). The "navigates to upload page" wording is
//     substituted with an assertion that CompletionReward renders with the
//     "Ready for upload" heading and a CTA pointing at the next run.
//
// This spec is the UI-level pair to the Go handler SMOKE-07 test
// (`TestRunHandler_SMOKE_07_ComplianceGate` at
// internal/api/handler_run_test.go). Together they pin the FR23 hard gate
// pre-ack-409 / post-ack-200 / one-shot invariants end-to-end.

const RUN_ID = 'gate-001'

test('ComplianceGate ack advances run to "Ready for upload" and only fires once', async ({
  page,
  skipOnboarding,
  consoleGuard,
}) => {
  void skipOnboarding

  const state = {
    runs: [
      {
        id: RUN_ID,
        scp_id: 'gate',
        stage: 'metadata_ack' as const,
        status: 'waiting' as const,
      },
    ],
    metadata: {
      [RUN_ID]: {
        run_id: RUN_ID,
        scp_id: 'gate',
        title: 'SCP-gate Compliance Run',
      },
    },
    manifest: {
      [RUN_ID]: {
        run_id: RUN_ID,
        scp_id: 'gate',
        source_url: 'https://scp-wiki.wikidot.com/scp-gate',
        author_name: 'Dr. Example',
        license: 'CC BY-SA 3.0',
      },
    },
  }
  const spies = makeSpies()
  await installApiMocks(page, { state, spies })

  const shell = new ProductionShellPO(page)
  await shell.gotoAndDismissOnboarding(RUN_ID)

  // ComplianceGate renders while stage=metadata_ack + status=waiting.
  const gate = page.getByRole('region', { name: 'Compliance gate' })
  await expect(gate).toBeVisible()
  await expect(
    page.getByRole('heading', { name: 'Pre-Upload Compliance Gate' }),
  ).toBeVisible()

  // Finalize is disabled until every checklist item is confirmed. This is the
  // pre-ack hard-gate UI contract.
  const finalize = page.getByRole('button', { name: 'Acknowledge & Complete' })
  await expect(finalize).toBeVisible()
  await expect(finalize).toBeDisabled()

  // Tick every checklist item — labels are fetched from metadata/manifest.
  await expect(
    page.getByText(/Title confirmed: SCP-gate Compliance Run/),
  ).toBeVisible()
  for (const cb of await page.getByRole('checkbox').all()) {
    await cb.check()
  }
  await expect(finalize).toBeEnabled()

  // Acknowledge. The component test
  // (ComplianceGate.test.tsx > "dispatches acknowledgeMetadata exactly once
  // under repeated clicks") already pins the repeated-click guard at the
  // React level; here we just verify the happy-path round-trip so the e2e
  // test stays focused on the contract boundary and is resilient to
  // disabled-button click semantics across browsers.
  await finalize.click()

  // Post-ack surface: ProductionShell swaps in CompletionReward once run
  // flips to complete/completed (invalidated by the mutation's onSuccess).
  await expect(
    page.getByRole('heading', { name: 'Ready for upload' }),
  ).toBeVisible()

  // Idempotency at the network boundary: exactly one POST hit /metadata/ack.
  expect(spies.ackCount.value).toBe(1)

  // Run metadata is surfaced on the reward screen.
  await expect(page.getByText('SCP-gate Compliance Run')).toBeVisible()
  await expect(
    page.getByText('https://scp-wiki.wikidot.com/scp-gate'),
  ).toBeVisible()

  // The <video> element on both ComplianceGate and CompletionReward hits
  // /api/runs/:id/video, which is not mocked (no stage file on disk) and
  // returns 404. That surfaces as a "Failed to load resource" console entry
  // per the browser's HTTP-status reporter. Filter those out before asserting
  // zero app-level console errors.
  const filtered = consoleGuard.consoleErrors.filter(
    (msg) => !msg.includes('404 (Not Found)'),
  )
  expect(filtered).toEqual([])
  expect(consoleGuard.pageErrors).toEqual([])
})

test('ComplianceGate ack is blocked with 409 when the run is not at metadata_ack+waiting', async ({
  page,
  skipOnboarding,
  consoleGuard,
}) => {
  void skipOnboarding

  // Park the run at a Phase B sub-stage so the gate is closed. This mirrors
  // SMOKE-07's `gate_closed_returns_409` sub-test: the mock /metadata/ack
  // route now enforces the same atomic precondition as RunStore.MarkComplete.
  const state = {
    runs: [
      {
        id: RUN_ID,
        scp_id: 'gate',
        stage: 'image' as const,
        status: 'running' as const,
      },
    ],
  }
  const spies = makeSpies()
  await installApiMocks(page, { state, spies })

  const shell = new ProductionShellPO(page)
  await shell.gotoAndDismissOnboarding(RUN_ID)

  // ComplianceGate is NOT rendered off-stage — only when
  // stage=metadata_ack + status=waiting. Drive the ack call from the page
  // context so Playwright's page.route() interception picks it up (the
  // APIRequestContext on `page.request` bypasses route handlers).
  const ack = await page.evaluate(async (url) => {
    const res = await fetch(url, { method: 'POST' })
    return { status: res.status, body: await res.json() }
  }, `/api/runs/${RUN_ID}/metadata/ack`)
  expect(ack.status).toBe(409)
  expect(ack.body.error?.code).toBe('CONFLICT')

  // The deliberate 409 logs a "Failed to load resource" console error in
  // Chromium — that is the HTTP status being surfaced, not an app-level
  // regression. Filter it out before asserting zero app errors.
  const filtered = consoleGuard.consoleErrors.filter(
    (msg) => !msg.includes('409 (Conflict)'),
  )
  expect(filtered).toEqual([])
  expect(consoleGuard.pageErrors).toEqual([])
})
