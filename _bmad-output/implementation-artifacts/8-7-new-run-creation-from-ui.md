# Story 8.7: New Run Creation from UI

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to create a new pipeline run directly from the web UI,
so that I can exercise the Production / Batch Review surfaces end-to-end without dropping into the terminal.

## Prerequisites

**Hard dependencies:** Story 6.2 established the `Sidebar` + `AppShell` layout. Story 7.1 established the run-inventory fetch (`fetchRunList` + `queryKeys.runs.list()`) that is shared by `Sidebar` and `ProductionShell`. Story 7.5 established the empty-state surface inside `ProductionShell` (the "No runs yet" block) that is the natural secondary entry point for this flow. Story 8.1 established the keyboard-first interaction style for `ProductionShell` descendants (`useKeyboardShortcuts`, no raw `window` listeners). Story 8.5 established the inline-panel interaction pattern (`InlineConfirmPanel`, `role="alertdialog"`, focus trap, `Enter`/`Esc`) that this story mirrors for an SCP-ID entry panel.

**Backend reality — do NOT reimplement:** `POST /api/runs` already exists and is fully tested.
- Route: `routes.go:29` → `deps.Run.Create` ([internal/api/handler_run.go:75-96](internal/api/handler_run.go#L75-L96))
- Request body: `{"scp_id": string}` (only field; unknown fields rejected)
- SCP ID validation regex (service layer): `^[A-Za-z0-9_-]+$` ([internal/service/run_service.go:16](internal/service/run_service.go#L16))
- Success: **HTTP 201** with `{"version": 1, "data": <RunDetail>, "error": null}`. `data.stage = "pending"`, `data.status = "pending"`, `data.id` auto-generated as `scp-<scpID>-run-<n>`.
- Error: HTTP 400 with `{"error": {"code": "VALIDATION_ERROR", "message": ...}}` for malformed JSON, missing `scp_id`, or regex mismatch.
- `output_dir` is **server-configured only** — never expose it in the request. Handler comment ([internal/api/handler_run.go:20-21](internal/api/handler_run.go#L20-L21)) enforces this for security.
- **Duplicate `scp_id` is permitted by design** — the handler auto-increments the run number; the UI must not preemptively reject a repeat SCP ID.

**Architecture guardrail (decision captured — do NOT regress):**
- UX-DR68 originally staged this as V1=clipboard-copy, V1.5=web-triggered. This story skips the clipboard intermediate and implements web-triggered directly, because the backend handler already exists and a clipboard-only V1 would be a dead layer.
- **Create-only. DO NOT auto-resume.** The run must land in `stage=pending, status=pending` and stay there. Starting the run (calling `POST /api/runs/{id}/resume`) is a deliberate, separate operator action and is **out of scope** for 8.7. Operators want cost control before an LLM-billed run starts.

**Parallel-work caution:** Epic 8 stories 8-1 through 8-6 actively touch `web/src/components/shells/ProductionShell.tsx`, `web/src/components/shared/Sidebar.tsx`, `web/src/components/shared/InlineConfirmPanel.tsx`, `web/src/lib/apiClient.ts`, `web/src/contracts/runContracts.ts`, `web/src/lib/queryKeys.ts`, `web/src/lib/keyboardShortcuts.ts`, `web/src/stores/useUIStore.ts`, and `web/src/index.css`. Layer changes onto that work; do not revert adjacent edits.

## Acceptance Criteria

### AC-1: "New Run" button is always visible in the Production sidebar header

**Given** the operator is on the Production tab with the sidebar expanded
**When** the sidebar renders
**Then** a `New Run` button is visible in the sidebar header area (next to the brand / collapse toggle) above the run inventory list
**And** the button is keyboard-accessible (reachable via `Tab`) with a visible focus ring
**And** the button exposes `aria-label="Create a new pipeline run"` and shows a visible `⌘N` / `Ctrl+N` hint label
**And** a secondary "New Run" CTA also renders inside the existing `ProductionShell` empty-state block at [web/src/components/shells/ProductionShell.tsx:281-286](web/src/components/shells/ProductionShell.tsx#L281-L286) so first-time users with zero runs have a discoverable entry point.

**Rules:**
- Do NOT place the button in a floating position-fixed FAB; it must live in the existing sidebar header DOM so it survives the collapsed-sidebar viewport rules from UX-DR60.
- When the sidebar is collapsed (`data-collapsed="true"`), render the button as icon-only with a tooltip label, mirroring the brand/toggle collapse behavior already in [Sidebar.tsx:56-92](web/src/components/shared/Sidebar.tsx#L56-L92).
- Do NOT add the button to the Tuning or Settings tabs — it belongs to Production only. Reuse the `location.pathname === '/production'` check already present in `Sidebar`.

**Tests:**
- Component test (`Sidebar.test.tsx`) verifies the button renders when `pathname === '/production'` and is absent on `/tuning` / `/settings`.
- Component test verifies collapsed/expanded visual states.
- Accessibility test verifies `aria-label` and keyboard focusability.

---

### AC-2: Clicking "New Run" (or pressing the shortcut) opens an inline SCP-ID entry panel

**Given** the sidebar is mounted and the `New Run` button is visible
**When** the operator clicks the button OR presses `⌘N` (macOS) / `Ctrl+N` (other platforms)
**Then** an inline entry panel appears with `role="alertdialog"` and a labeled text input for SCP ID
**And** focus moves into the text input on mount and is trapped inside the panel (Tab loops among input + Create + Cancel)
**And** the panel copy names the action ("Create a new pipeline run") and hints the format ("Alphanumeric, hyphen, or underscore. Example: `049`")
**And** the panel does NOT overlay the whole viewport — it renders inline near the sidebar header, following the Inline Confirmation Pattern established by `InlineConfirmPanel`.

**Rules:**
- Do NOT introduce a Radix Dialog, native `<dialog>`, or any new modal primitive — this codebase deliberately uses inline alertdialog panels with focus traps.
- Do NOT reuse `InlineConfirmPanel` directly — its props are hardcoded for approve-all copy and it has no text-input slot. Create a dedicated `NewRunPanel` component and keep it local to the Production shell.
- `Esc` dismisses the panel without firing the create action and returns focus to the invoking `New Run` button.
- While the panel is open, suppress any Production-shell keyboard shortcuts that would otherwise fire from the trapped input (`J`, `K`, `S`, `Enter` as approve, etc.) — the existing `useKeyboardShortcuts` scoping pattern handles this when the panel owns focus.

**Tests:**
- Component test (`NewRunPanel.test.tsx`) verifies `role="alertdialog"`, initial focus on input, Tab focus trap, and Esc-to-close with focus restore.
- Keyboard test verifies the shortcut (`Ctrl+N` / `Cmd+N`) opens the panel and `Esc` closes it without leaking to batch-review shortcuts.

---

### AC-3: Keyboard shortcut engine learns `mod+n` (Cmd on macOS, Ctrl elsewhere)

**Given** the keyboard shortcut engine currently supports `s`, `j`, `k`, `digit-1..0`, `ctrl+z`, `enter`, `shift+enter`, `escape`, `tab`, `space`
**When** Story 8.7 ships
**Then** `SUPPORTED_SHORTCUT_KEYS` in [web/src/lib/keyboardShortcuts.ts:1-21](web/src/lib/keyboardShortcuts.ts#L1-L21) includes a new entry `mod+n`
**And** `normalizeShortcut(event)` recognizes `event.metaKey` on macOS (`navigator.platform` match for `Mac`) and `event.ctrlKey` on other platforms as the `mod` modifier — both map to the same canonical `mod+n`
**And** the hint label renderer reflects the platform (shows `⌘N` on macOS, `Ctrl+N` otherwise).

**Rules:**
- Do NOT add two separate shortcut entries (`cmd+n` and `ctrl+n`). Use one canonical `mod+n` key to match common editor conventions and keep the hint system platform-aware.
- Browsers capture `Ctrl+N` globally for "new window" — the handler must call `event.preventDefault()` exactly when the Production tab is mounted and the shortcut definition is active, so it does not hijack the shortcut on other tabs.
- Do NOT handle `mod+n` through a raw `window.addEventListener`. Register via the existing `useKeyboardShortcuts` provider like every other shortcut in the codebase.

**Tests:**
- Unit test in `keyboardShortcuts.test.ts` verifies `normalizeShortcut` produces `{key: 'mod+n', digit: null}` for `{metaKey: true, key: 'n'}` on mac-detected platforms and for `{ctrlKey: true, key: 'n'}` elsewhere.
- Unit test verifies the shortcut is NOT triggered by plain `n`, by `shift+n`, or by combinations with unrelated modifiers.
- Integration test in `Sidebar.test.tsx` or equivalent verifies `mod+n` opens the panel only while the Production tab is mounted.

---

### AC-4: SCP-ID input is validated client-side before the POST

**Given** the entry panel is open
**When** the operator types an SCP ID
**Then** the Create submit control is disabled while the input is empty or whitespace-only
**And** the input enforces the backend regex `^[A-Za-z0-9_-]+$` — invalid characters (spaces, slashes, dots, Korean text, etc.) render an inline error "SCP ID must be alphanumeric, hyphen, or underscore" and block submission
**And** the input trims leading/trailing whitespace before validation and before submission.

**Rules:**
- Client regex MUST equal the backend regex exactly ([run_service.go:16](internal/service/run_service.go#L16)) — drift between the two regexes would cause confusing inconsistent errors. Consider exporting the pattern once from `runContracts.ts` so both the zod schema and the input validator share the source.
- Do NOT auto-uppercase or auto-lowercase the input — SCP IDs like `173-J` are case-sensitive identifiers.
- Do NOT block duplicate SCP IDs client-side. The backend is intentionally permissive (auto-increments the run number); surfacing a "duplicate" error from the client would be a lie.
- Preserve the raw input on validation failure — do not clear the field.

**Tests:**
- Component test verifies the Create button is disabled for `""`, `" "`, `abc def`, `abc/def`, `abc.def`, `가나다` and enabled for `049`, `173-J`, `test_01`, `SCP-2521`.
- Component test verifies trim-before-submit (input `"  049  "` submits as `"049"`).

---

### AC-5: Submitting POSTs to `/api/runs` and refreshes the run inventory

**Given** a valid SCP ID is entered
**When** the operator clicks Create or presses `Enter` inside the input
**Then** the UI calls `apiClient.createRun(scp_id)` which performs `POST /api/runs` with `{"scp_id": "..."}` and parses the response via a new `createRunResponseSchema`
**And** on HTTP 201, the client invalidates `queryKeys.runs.list()` so the sidebar inventory refetches with the new run included
**And** the newly created run is auto-selected by setting `?run=<new run id>` on the Production route (via `setSearchParams({ run: newRunId })`)
**And** the entry panel closes and focus returns to the invoking `New Run` button
**And** while the request is in-flight, the Create button shows a disabled + "Creating…" state and the input is read-only.

**Rules:**
- Add `createRun(scp_id: string)` to [web/src/lib/apiClient.ts](web/src/lib/apiClient.ts) using the existing `apiRequest<T>` wrapper — do not hand-roll a second fetch pattern. Mirror the shape of `resumeRun` at line 93.
- Add `createRunRequestSchema` and `createRunResponseSchema` to [web/src/contracts/runContracts.ts](web/src/contracts/runContracts.ts). The response envelope is `{version: z.literal(1), data: runDetailSchema, error: z.null()}` — reuse the existing `runDetailSchema` at [runContracts.ts:30-48](web/src/contracts/runContracts.ts#L30-L48); do not duplicate run fields.
- Auto-selection uses `setSearchParams` from `react-router`, consistent with the existing selection flow in [ProductionShell.tsx:115-133](web/src/components/shells/ProductionShell.tsx#L115-L133). Do NOT introduce a new Zustand field for selected-run-id.
- Cache invalidation uses `queryClient.invalidateQueries({ queryKey: queryKeys.runs.list() })`. Do NOT manually `setQueryData` with the new run — `refetch` is authoritative.

**Tests:**
- MSW-backed integration test (or Vitest with `vi.fn()` on `apiClient`) verifies: POST called with `{"scp_id": "049"}`, sidebar inventory refetch triggered, URL updates to `?run=scp-049-run-1`, panel closes, focus returns to trigger button.
- Contract test (co-located with existing `runContracts.test.ts`) verifies the new schemas parse the exact JSON produced by the backend handler (reference [internal/api/handler_run_test.go:82-103](internal/api/handler_run_test.go#L82-L103) for the authoritative response shape).

---

### AC-6: Backend failures surface as Fail-Loud-with-Fix inline errors

**Given** the POST has been dispatched
**When** the server returns a non-201 response
**Then** the entry panel stays open and displays an inline error region (`role="alert"`) with the mapped user-facing message
**And** the operator can correct the input and re-submit without reloading the page
**And** the panel surfaces three specific failure modes:
  - **400 VALIDATION_ERROR** → "The server rejected that SCP ID: `<server message>`. Check the format and try again."
  - **Network / fetch failure** → "Couldn't reach the server. Check that `pipeline serve` is running, then retry."
  - **Any other status** → "Server error (`<status>`). The run was not created. Retry, or check the server logs."

**Rules:**
- Reuse the existing `ApiClientError` class from [apiClient.ts:30-40](web/src/lib/apiClient.ts#L30-L40) — do not invent a second error type.
- Follow the UX Fail-Loud-with-Fix pattern: the message must (a) name the problem, (b) name the cause, (c) name the fix. Do not use generic "Something went wrong" copy.
- Do NOT auto-dismiss the error. It clears only when the operator edits the input or re-submits.
- Do NOT fire a toast in addition to the inline error — it would duplicate the signal.

**Tests:**
- Component test verifies each of the three error branches renders the correct copy under the corresponding mocked response (400, thrown fetch error, 500).
- Test verifies the panel remains open and the input retains the failed value after a validation error.
- Test verifies re-submit works after correcting the input (no stuck disabled state).

---

### AC-7: Newly created run renders a pending empty-state inside the Production view

**Given** the new run is now selected (`?run=scp-049-run-1`) and its `stage=pending, status=pending`
**When** `ProductionShell` renders for this run
**Then** the main content area shows a pending empty-state card with: the run ID (mono), the SCP ID, a `Pending` status badge, and operator guidance copy
**And** the guidance copy explicitly names the next action:
  > "Run created. It has not started yet. To begin Phase A, run `pipeline resume <run-id>` in your terminal."
**And** the copy includes a one-click "Copy command" button that copies the literal `pipeline resume <run-id>` (with the actual run ID substituted) to the clipboard.

**Rules:**
- This is **the only** place in 8.7 where a clipboard-copy interaction appears — it is the seam between "run created" and "run running". It is not the same as the abandoned UX-DR68 V1 clipboard flow (which was the *creation* command).
- Do NOT call `POST /api/runs/{id}/resume` from this surface in 8.7. Even a "Start now" button is out of scope.
- The pending empty-state MUST NOT render if the run's stage has advanced past `pending` — reuse the same `current_run.stage` read that the existing shell branches on.
- Use the existing status-badge styling for consistency with `RunCard` status pills.

**Tests:**
- Component test (`ProductionShell.test.tsx`) verifies pending-state card renders when `current_run.stage === 'pending'` and `current_run.status === 'pending'`, and is absent otherwise.
- Test verifies "Copy command" writes the exact string `pipeline resume <run-id>` to `navigator.clipboard` (mock it) and shows a brief confirmation.

---

### AC-8: Playwright smoke test exercises the end-to-end creation flow

**Given** the Playwright e2e harness from Story 6.5 is green
**When** this story lands
**Then** a new e2e spec at `web/e2e/new-run-creation.spec.ts` covers:
  - Click `New Run` button in the sidebar header on `/production`
  - Type a unique SCP ID (timestamp-suffixed to avoid collisions with seeded fixtures)
  - Click Create
  - Assert: a new `RunCard` matching that SCP ID appears in the sidebar inventory
  - Assert: the URL contains `?run=scp-<scpID>-run-<n>`
  - Assert: the pending empty-state card is visible with the correct "Copy command" text
**And** the spec runs against the `npm run serve:e2e` harness already configured in [web/playwright.config.ts](web/playwright.config.ts) and passes locally and in CI.

**Rules:**
- Do NOT seed a fixture run for this spec — the point of the smoke is to exercise the real create path.
- Do NOT assert stage progression — the run must stay at `pending`. Asserting otherwise would accidentally require `resume` wiring and violate the scope boundary.
- Use a timestamp or UUID-based SCP ID to guarantee the test is re-runnable against a dirty dev database; the backend allows duplicates but the URL assertion needs `scp-<id>-run-<n>` to be deterministic per test run.

**Tests:**
- The spec itself is the test. Additionally, update `web/e2e/README.md` (or equivalent index) if one exists.

---

## Tasks / Subtasks

- [x] **T1: Extend the keyboard shortcut engine with `mod+n`** (AC: #3)
  - Add `'mod+n'` to `SUPPORTED_SHORTCUT_KEYS` in [web/src/lib/keyboardShortcuts.ts](web/src/lib/keyboardShortcuts.ts).
  - Extend `normalizeShortcut` to detect `metaKey` on mac platforms and `ctrlKey` elsewhere (use a small `isMacPlatform()` helper; reuse if one exists).
  - Add a platform-aware hint formatter (`⌘N` vs `Ctrl+N`).
  - Add unit coverage in `keyboardShortcuts.test.ts`.

- [x] **T2: Add `createRun` contracts and API client method** (AC: #4, #5)
  - Add `SCP_ID_PATTERN` (exported) + `createRunRequestSchema` + `createRunResponseSchema` to [web/src/contracts/runContracts.ts](web/src/contracts/runContracts.ts).
  - Add `createRun(scp_id: string)` to [web/src/lib/apiClient.ts](web/src/lib/apiClient.ts) using `apiRequest<T>` mirroring `resumeRun` at line 93. POST to `/runs` (note: `apiRequest` prepends `/api`, do not hardcode).
  - Add contract test in `runContracts.test.ts` that parses a fixture mirroring [internal/api/handler_run_test.go](internal/api/handler_run_test.go) success output.

- [x] **T3: Build the `NewRunPanel` component** (AC: #2, #4, #6)
  - New file `web/src/components/production/NewRunPanel.tsx` with props: `on_cancel()`, `on_success(run)`, and an internal submit state.
  - `role="alertdialog"`, focus trap (reference the pattern in [InlineConfirmPanel.tsx](web/src/components/shared/InlineConfirmPanel.tsx)), auto-focus the input on mount.
  - Client-side regex validation using the shared `SCP_ID_PATTERN`.
  - Submit calls `createRun`, handles the three error branches from AC-6, writes inline `role="alert"` error on failure.
  - Component test `NewRunPanel.test.tsx` covers validation, submit success, each error branch, Esc cancel + focus restore.

- [x] **T4: Wire the `New Run` button into `Sidebar`** (AC: #1, #2, #3, #5)
  - Add a button to the `sidebar__header` region in [web/src/components/shared/Sidebar.tsx](web/src/components/shared/Sidebar.tsx), rendered only when `location.pathname === '/production'`.
  - Register `mod+n` via `useKeyboardShortcuts` with `scope: 'context'` so it only fires while Production is mounted.
  - When triggered (click or shortcut), mount `NewRunPanel` inline inside the sidebar header region.
  - On success: invalidate `queryKeys.runs.list()`, call `setSearchParams({ run: run.id })`, close panel, restore focus to the button.
  - Update `Sidebar.test.tsx` for button visibility, collapsed-state rendering, tab restriction, and the full click → panel → success → inventory-refetch flow (with a mocked `createRun`).

- [x] **T5: Add a secondary CTA in the `ProductionShell` empty-state** (AC: #1)
  - At [ProductionShell.tsx:281-286](web/src/components/shells/ProductionShell.tsx#L281-L286), add a secondary `New Run` button that triggers the same open-panel behavior (hoist the open/close state to a shared parent, or emit an event via a small context so Sidebar and ProductionShell can both request the panel to open).
  - Prefer the lightest coordination — if one lives in `Sidebar`, the shell CTA can just focus/click the sidebar button programmatically via a ref forwarded through a shared context.

- [x] **T6: Render pending empty-state for `stage=pending, status=pending` runs** (AC: #7)
  - Add a conditional branch inside `ProductionShell` that renders a pending-state card when `current_run.stage === 'pending'` (otherwise fall through to the existing stage-specific views).
  - Include run ID (mono), SCP ID, Pending badge, guidance copy, and `navigator.clipboard.writeText` button for `pipeline resume <run-id>`.
  - Update `ProductionShell.test.tsx` with coverage for the pending branch + clipboard-copy mock.

- [x] **T7: Playwright smoke test** (AC: #8)
  - New spec `web/e2e/new-run-creation.spec.ts`.
  - Use `Date.now()`-suffixed SCP ID to keep tests re-runnable.
  - Assert sidebar inventory includes new run, URL contains `?run=`, pending empty-state renders.
  - Verify the spec runs in `npm run e2e` (or whatever the existing npm script name is — check [web/package.json](web/package.json)).

- [x] **T8: CSS + theme token additions** (AC: #1, #2, #7)
  - Add styles to [web/src/index.css](web/src/index.css) for: `.sidebar__new-run-btn`, `.new-run-panel`, `.new-run-panel__error`, `.production__pending-state`, `.production__pending-copy-btn`.
  - Reuse existing theme tokens; do not introduce new color/spacing tokens without strong justification.

## Dev Notes

### Story Intent and Scope Boundary

- 8.7 closes the missing "front door" to the Production workflow — the backend has accepted new runs since Epic 2, but the UI forced operators into the CLI.
- **In scope:** create-only. The run lands at `stage=pending, status=pending` and the UI guides the operator toward `pipeline resume` in the terminal.
- **Out of scope:** auto-starting Phase A (no `POST /api/runs/{id}/resume` invocation from this flow), a UI-driven "Start" button (split to a future story if and when desired), a run-cancellation path, any bulk-import flow, duplicate-SCP deduplication UI.
- Do NOT extend 8.7 into "feature creep" — a crisp, cost-controlled entry point is the entire value.

### Current Codebase Reality

| What | Where | State |
|---|---|---|
| `POST /api/runs` backend | [internal/api/routes.go:29](internal/api/routes.go#L29), [internal/api/handler_run.go:75](internal/api/handler_run.go#L75) | **Done — consume as-is** |
| `SCP_ID` regex | [internal/service/run_service.go:16](internal/service/run_service.go#L16) | Done |
| `RunDetail` zod schema | [web/src/contracts/runContracts.ts:30-48](web/src/contracts/runContracts.ts#L30-L48) | Done — reuse |
| `createRunRequestSchema` / `createRunResponseSchema` | `runContracts.ts` | **Missing — add in T2** |
| `createRun` API client method | `apiClient.ts` | **Missing — add in T2** |
| `New Run` button | `Sidebar.tsx` header | **Missing — add in T4** |
| `NewRunPanel` component | `web/src/components/production/` | **Missing — create in T3** |
| Keyboard `mod+n` support | `keyboardShortcuts.ts` | **Missing — add in T1** |
| Pending-state empty card | `ProductionShell.tsx` | **Missing — add in T6** |
| Playwright spec | `web/e2e/` | **Missing — add in T7** |
| `InlineConfirmPanel` primitive | [web/src/components/shared/InlineConfirmPanel.tsx](web/src/components/shared/InlineConfirmPanel.tsx) | Done — study the focus-trap pattern, do NOT reuse directly |
| `queryKeys.runs.list()` | [web/src/lib/queryKeys.ts](web/src/lib/queryKeys.ts) | Done — invalidate post-success |
| Run inventory fetch | `Sidebar.tsx` + `ProductionShell.tsx` both call `fetchRunList` | Done — refetch is triggered by invalidation |

### Backend Contract Quick-Reference

**Request:**
```json
POST /api/runs
Content-Type: application/json

{"scp_id": "049"}
```

**Success (201):**
```json
{
  "version": 1,
  "data": {
    "id": "scp-049-run-1",
    "scp_id": "049",
    "stage": "pending",
    "status": "pending",
    "retry_count": 0,
    "retry_reason": null,
    "critic_score": null,
    "cost_usd": 0,
    "token_in": 0,
    "token_out": 0,
    "duration_ms": 0,
    "human_override": false,
    "character_query_key": null,
    "selected_character_id": null,
    "frozen_descriptor": null,
    "created_at": "2026-04-19T00:00:00Z",
    "updated_at": "2026-04-19T00:00:00Z"
  },
  "error": null
}
```

**Validation error (400):**
```json
{
  "version": 1,
  "data": null,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "scp_id is required"
  }
}
```

Source of truth: [internal/api/handler_run_test.go:82-103](internal/api/handler_run_test.go#L82-L103).

### Button Placement Rationale

Two candidate locations were considered:

1. **Sidebar header (chosen)** — Persistent, discoverable on every Production view regardless of whether runs exist. Survives sidebar collapse via icon-only mode. Consistent with VS Code / Linear / etc. conventions where "New" actions live in the primary nav chrome.
2. **ProductionShell empty state only** — Insufficient: disappears as soon as any run exists, so returning operators would hunt for it.

Both surfaces render the button (AC-1 + T5), but the sidebar header is the **primary** entry point and the empty-state CTA is a **secondary** reinforcement for first-run operators.

### Why a Custom `NewRunPanel` and Not `InlineConfirmPanel`

`InlineConfirmPanel` ([web/src/components/shared/InlineConfirmPanel.tsx](web/src/components/shared/InlineConfirmPanel.tsx)) has hardcoded props (`confirm_label`, `count`) tied to the approve-all use case and no text-input slot. Generalizing it to accept an input would widen its API surface and risk regressing Story 8.5. A focused `NewRunPanel` keeps each inline-panel component single-purpose — the correct pattern in this codebase.

### Why `mod+n` and Not `mod+k` / Other Shortcuts

- `mod+n` is the near-universal "New" shortcut (Gmail compose, Notion new page, Slack new message, Trello new card, Linear new issue).
- `mod+k` is reserved in most apps for command palette / search and would collide if a palette is added later.
- `n` alone would conflict with the plain-letter convention (`s` for skip, `j`/`k` for navigation) and would fire accidentally during typing.
- Browsers hijack `Ctrl+N` for "new window" — call `preventDefault()` when the shortcut is active to capture it for the app. This is the same trade Linear/Notion make.

### Security Notes

- `output_dir` is server-configured only; **never** expose a field for it in the panel or in `createRunRequestSchema`. The backend deliberately ignores any such field; adding one to the UI would create a false expectation.
- Clipboard writes (AC-7) use `navigator.clipboard.writeText` — gate on the permission API and fall back to showing the command as selectable text if clipboard is unavailable (headless CI, older browsers).
- No SCP ID escaping issues in the URL or DOM because the validator enforces `^[A-Za-z0-9_-]+$` before submission — the same characters are safe in URLs, HTML, and filesystem paths. Do not relax this validator without revisiting escape logic downstream.

### Testing Standards Summary

| Layer | Framework | Pattern file to study |
|---|---|---|
| Unit (util / store) | Vitest | [web/src/stores/useUIStore.test.ts](web/src/stores/useUIStore.test.ts) |
| Component | Vitest + React Testing Library + `@testing-library/user-event` | [web/src/components/shared/InlineConfirmPanel.test.tsx](web/src/components/shared/InlineConfirmPanel.test.tsx), [web/src/components/shared/RunCard.test.tsx](web/src/components/shared/RunCard.test.tsx) |
| Contract (zod) | Vitest | [web/src/contracts/runContracts.test.ts](web/src/contracts/runContracts.test.ts) |
| Integration (hook + query) | Vitest + `@tanstack/react-query` test utilities | [web/src/hooks/useRunStatus.test.tsx](web/src/hooks/useRunStatus.test.tsx) |
| E2E | Playwright | [web/playwright.config.ts](web/playwright.config.ts) — testDir `web/e2e/` |

API mocking in component tests uses direct `vi.fn()` stubs on `apiClient` methods rather than MSW; keep that convention unless integrating with an already-MSW-wired test.

### Suggested File Touches

- [web/src/lib/keyboardShortcuts.ts](web/src/lib/keyboardShortcuts.ts) — add `mod+n`, platform detection
- `web/src/lib/keyboardShortcuts.test.ts` — new coverage
- [web/src/lib/apiClient.ts](web/src/lib/apiClient.ts) — add `createRun`
- [web/src/contracts/runContracts.ts](web/src/contracts/runContracts.ts) — add `SCP_ID_PATTERN`, `createRunRequestSchema`, `createRunResponseSchema`
- [web/src/contracts/runContracts.test.ts](web/src/contracts/runContracts.test.ts) — schema round-trip tests
- `web/src/components/production/NewRunPanel.tsx` — **NEW**
- `web/src/components/production/NewRunPanel.test.tsx` — **NEW**
- [web/src/components/shared/Sidebar.tsx](web/src/components/shared/Sidebar.tsx) — button + panel wiring
- `web/src/components/shared/Sidebar.test.tsx` — new or extended coverage
- [web/src/components/shells/ProductionShell.tsx](web/src/components/shells/ProductionShell.tsx) — secondary CTA + pending empty-state
- [web/src/components/shells/ProductionShell.test.tsx](web/src/components/shells/ProductionShell.test.tsx) — pending branch coverage
- [web/src/index.css](web/src/index.css) — style tokens
- `web/e2e/new-run-creation.spec.ts` — **NEW**

Backend should **not** need any changes for 8.7. If a missing endpoint is discovered, stop and raise it rather than adding backend code under this story.

### Project Structure Notes

- Component lives under `components/production/` (not `components/shared/`) because its interaction is Production-specific, even though it is mounted from the Sidebar. Other Production-specific components follow the same convention ([BatchReview.tsx](web/src/components/production/BatchReview.tsx), [CharacterPick.tsx](web/src/components/production/CharacterPick.tsx)).
- `SCP_ID_PATTERN` lives in `contracts/runContracts.ts` rather than a separate `validators.ts` so that client-side validation and the zod schema share a single source of truth.
- Keyboard shortcut platform detection (`isMacPlatform()`) should live inside `keyboardShortcuts.ts` itself, not in a new util file — colocated with the one consumer that needs it.

### References

- Epic 8 scope: [_bmad-output/planning-artifacts/epics.md:532-565](_bmad-output/planning-artifacts/epics.md#L532-L565)
- UX-DR68 origin (parked in Epic 7, picked up here): [_bmad-output/planning-artifacts/epics.md:272](_bmad-output/planning-artifacts/epics.md#L272)
- Backend handler: [internal/api/handler_run.go:75-96](internal/api/handler_run.go#L75-L96)
- Backend service validation: [internal/service/run_service.go:16,49-60](internal/service/run_service.go#L16)
- Backend handler success test (authoritative response shape): [internal/api/handler_run_test.go:82-103](internal/api/handler_run_test.go#L82-L103)
- Domain error codes: [internal/domain/errors.go:16-29](internal/domain/errors.go#L16-L29)
- Inline panel reference pattern: [web/src/components/shared/InlineConfirmPanel.tsx](web/src/components/shared/InlineConfirmPanel.tsx)

## Open Questions / Saved Clarifications

1. **Should the pending empty-state include a "Start now" button that calls `/api/runs/{id}/resume`?** Explicitly deferred per the scope boundary above — revisit in a separate story (suggested: 8-8 "UI-Triggered Run Start") once cost telemetry from Epic 7's status bar is judged sufficient for confident one-click starts.
2. **Should duplicate SCP IDs surface a soft warning ("A run for `049` already exists — continue?")?** Open. The backend permits duplicates by design, but some operators may find it confusing. If the warning is desired, it can be added in a small follow-up without breaking 8.7 semantics.
3. **Playwright baseURL vs dev-server assumptions:** [web/playwright.config.ts](web/playwright.config.ts) boots `npm run serve:e2e`. Confirm that script still exists and that the e2e harness already seeds a clean SQLite DB per run; if not, T7 may need a brief teardown step.

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- `npm run lint`
- `npm run test:unit`
- `npm run test:e2e`

### Completion Notes List

- Added `mod+n` shortcut support with platform-aware hint rendering (`⌘N` on macOS, `Ctrl+N` elsewhere).
- Added create-run contracts and API client support, including alignment with the backend success envelope where success omits the `error` field.
- Built `NewRunPanel` with inline validation, focus trap behavior, fail-loud inline error mapping, and disabled in-flight submission handling.
- Wired the shared new-run flow through the Production sidebar and empty state, with URL selection of the newly created run plus authoritative run-list refresh.
- Added the pending-state guidance card with clipboard copy affordance and hardened `ProductionShell` so explicit `?run=` selections are not overwritten by fallback selection logic.
- Added and updated unit, integration, and Playwright coverage for the new-run workflow and refreshed the existing smoke test to match the current onboarding + sidebar behavior.

### File List

- `web/src/lib/keyboardShortcuts.ts`
- `web/src/lib/keyboardShortcuts.test.ts`
- `web/src/contracts/runContracts.ts`
- `web/src/contracts/runContracts.test.ts`
- `web/src/lib/apiClient.ts`
- `web/src/lib/clipboard.ts`
- `web/src/components/production/NewRunContext.tsx`
- `web/src/components/production/newRunCoordinatorContext.ts`
- `web/src/components/production/useNewRunCoordinator.ts`
- `web/src/components/production/NewRunPanel.tsx`
- `web/src/components/production/NewRunPanel.test.tsx`
- `web/src/components/shared/AppShell.tsx`
- `web/src/components/shared/Sidebar.tsx`
- `web/src/components/shared/Sidebar.test.tsx`
- `web/src/components/shells/ProductionShell.tsx`
- `web/src/components/shells/ProductionShell.test.tsx`
- `web/src/components/shared/DetailPanel.test.tsx`
- `web/src/components/shared/SceneCard.test.tsx`
- `web/src/index.css`
- `web/e2e/new-run-creation.spec.ts`
- `web/e2e/smoke.spec.ts`
- `web/README.md`

### Review Findings

- [x] [Review][Patch] CSS orphaned rule block after `.production__pending-copy-confirmation` causes parse error and lost card styling [web/src/index.css:1169-1173]
- [x] [Review][Patch] `NewRunPanel` sets `aria-modal="true"` — invalid on inline non-modal panel, hides rest of UI from screen readers [web/src/components/production/NewRunPanel.tsx:162]
- [x] [Review][Patch] `NewRunPanel` Escape handler calls `preventDefault` but not `stopImmediatePropagation` — `BatchReview` escape→reject shortcut fires concurrently [web/src/components/production/NewRunPanel.tsx:86-89]
- [x] [Review][Patch] `clipboard.ts` has no guard for unavailable `navigator.clipboard` and no error handling [web/src/lib/clipboard.ts:1]
- [x] [Review][Patch] E2E spec missing sidebar `RunCard` inventory assertion after run creation [web/e2e/new-run-creation.spec.ts:24]
- [x] [Review][Patch] `handleNewRunSuccess` fire-and-forget IIFE swallows network errors; includes unspecced `fetchQuery` call [web/src/components/shared/Sidebar.tsx:95-102]
- [x] [Review][Patch] `createRunResponseSchema` uses `z.null().optional()` instead of spec-required `z.null()` [web/src/contracts/runContracts.ts:59]
- [x] [Review][Patch] Sidebar test only checks `/settings` absence; `/tuning` path not tested [web/src/components/shared/Sidebar.test.tsx:119]
- [x] [Review][Patch] `NewRunPanel.test.tsx` missing Shift+Tab reverse-cycle assertion [web/src/components/production/NewRunPanel.test.tsx:88-94]
- [x] [Review][Patch] `set_is_submitting(false)` in `finally` runs after panel unmount if `on_success` closes panel synchronously [web/src/components/production/NewRunPanel.tsx:152-153]
- [x] [Review][Patch] `open_new_run_panel` has no double-open guard; `mod+n` while panel is open clobbers `restore_focus` ref [web/src/components/production/NewRunContext.tsx:24]
