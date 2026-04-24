# Story 10.2: Tuning Surface - Prompt & Rubric Lab

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want to edit the Critic prompt and run Golden/Shadow/Fast Feedback checks from the Tuning tab,
so that I can improve quality safely and compare later run outcomes by prompt version.

## Prerequisites

**Hard reuse requirements from earlier stories:**

- **Story 4.1 is the Golden source of truth.** Reuse `internal/critic/eval.RunGolden`, `AddPair`, `ListPairs`, `EvaluateFreshness`, and the manifest-backed fixture model under `testdata/golden/eval/`. Do not invent a second Golden storage layer in SQLite or the browser.
- **Story 4.2 is the Shadow source of truth.** Reuse `internal/critic/eval.RunShadow`, `ShadowSource`, `LoadShadowInput`, and the existing `shadow_eval_window` config. Do not shell out to `go test`; call the production Go package directly through backend handlers/services.
- **Story 4.3 is the calibration source of truth.** Reuse `internal/db.CalibrationStore` and persisted `critic_calibration_snapshots`. The Tuning tab reads trend/history; it does not recompute Cohen's kappa in TypeScript.
- **Story 6.2 established the SPA route shell.** The `/tuning` route already exists and currently renders a placeholder `TuningShell.tsx`; this story upgrades that shell rather than introducing a new top-level route or modal workflow.
- **Story 8.7 established the current frontend interaction style.** Continue using React Query, route-shell composition, inline error states, and the existing `apiClient` / `queryKeys` patterns. Do not bypass those with ad-hoc fetch calls.

**Canonical artifact rules that must not regress:**

- `docs/prompts/scenario/critic_agent.md` remains the canonical editable Critic prompt file. The Tuning tab may mirror or edit it through the backend, but must not create a second database-owned prompt body.
- `testdata/golden/eval/manifest.json` remains the canonical Golden fixture index and freshness record.
- Run-level quality comparison for later metrics requires prompt-version data to be persisted on each newly created run after a prompt save. Existing runs must remain immutable.

**Current codebase reality to account for:**

- `web/src/components/shells/TuningShell.tsx` is still a placeholder.
- `internal/api/routes.go` has no tuning endpoints yet.
- `runs` currently has no prompt-version columns, so this story must add the minimal persistence needed for later statistical grouping.

## Acceptance Criteria

### AC-1: Tuning tab becomes a compact single-column operator workspace

**Given** the operator navigates to `/tuning`
**When** `TuningShell` renders
**Then** it presents a Figma-style compact, single-column, scrollable workspace with six sections in this order:

1. Critic Prompt
2. Fast Feedback
3. Golden Eval
4. Shadow Eval
5. Fixture Management
6. Calibration

**And** the shell keeps the existing route-shell framing and typography conventions from the current SPA
**And** it does not introduce a second nested tabset inside `/tuning`
**And** each section can show loading, success, warning, and fail-loud inline states without collapsing the whole page.

**Rules:**

- Follow the UX spec's "properties panel / compact data density" direction for Tuning, not the Production split-pane pattern.
- No cross-tab navigation is allowed from happy-path controls. Cross-tab links are permitted only as clearly-labeled diagnostic deep links in error or warning context.
- The Tuning tab must remain usable on laptop widths; sections stack vertically and never depend on side-by-side wide tables to be understood.

**Tests:**

- `TuningShell.test.tsx` verifies the six section headings render in order.
- Responsive component test verifies compact stacking behavior at narrow widths.
- Accessibility test verifies one `h1`, section labels, and keyboard reachability of all primary actions.

### AC-2: Critic prompt editor loads and saves the canonical prompt file through the backend

**Given** the operator opens the Critic Prompt section
**When** the page loads
**Then** the current contents of `docs/prompts/scenario/critic_agent.md` are loaded via a new backend tuning API and shown in a multiline editor
**And** save writes back to the same file, not to a duplicate DB row or browser-only cache.

**Required backend surface:**

- `GET /api/tuning/critic-prompt`
- `PUT /api/tuning/critic-prompt`

**Required response metadata on save/load:**

- prompt body
- `saved_at` (RFC3339 UTC)
- `prompt_hash` (SHA-256 of raw bytes)
- `git_short_sha` (7 chars when available, `"nogit"` fallback)
- `version_tag`

**Version tag format:**

- `version_tag = <utc-timestamp>-<git_short_sha>`
- example: `20260424T031522Z-f6b34b6`

**Rules:**

- Save normalizes the prompt file to a trailing newline, but otherwise preserves the operator-authored content.
- If Git metadata cannot be resolved, save still succeeds with `git_short_sha="nogit"`.
- Prompt editing is explicit-save, not blur-save. The operator must have a visible Save action and dirty-state indication.
- Do not execute Golden or Shadow automatically on save in this AC. Save only persists the prompt and returns metadata.

**Tests:**

- API handler test verifies GET returns the exact file contents plus metadata envelope.
- API handler test verifies PUT persists the file and returns a stable `version_tag`.
- Frontend test verifies dirty state, save button enable/disable, and fail-loud error copy.

### AC-3: New runs are tagged with the active Critic prompt version for later comparison

**Given** a Critic prompt has been saved through the Tuning tab
**When** a new run is created afterwards
**Then** the run persists the active prompt version metadata at creation time
**And** that metadata is exposed through the existing run API contracts so later metrics/history features can group by prompt version.

**Minimum persistence required:**

- add nullable columns on `runs` for:
  - `critic_prompt_version`
  - `critic_prompt_hash`

**Rules:**

- Tagging happens at run creation time and is immutable for that run.
- Existing rows remain null until new runs are created after this story lands; do not backfill historical runs.
- The stored version must equal the most recently saved Critic prompt version tag, not a recomputed value at render time.
- The run API and TypeScript contracts must surface the two new fields without breaking existing consumers.

**Tests:**

- Migration test proves the new columns exist.
- Run creation integration test proves a newly created run stores the active prompt version/hash.
- Contract test proves the run detail/list schemas accept the new nullable fields.

### AC-4: Fast Feedback runs the Critic stage against a deterministic 10-sample scene set

**Given** the operator has edited the Critic prompt
**When** they trigger `Fast Feedback`
**Then** the backend runs a deterministic, lightweight evaluation pass against 10 sample scenes
**And** the UI returns results quickly enough to support prompt iteration without requiring a full live run.

**Scope guard for V1:**

- The selectable stage surface may be designed for future extension, but Story 10.2 only needs to execute the current Critic evaluation path because that is the only prompt edited in this story.
- The 10-sample corpus must be deterministic and version-controlled, not chosen randomly from live data.

**Required implementation shape:**

- Add a small file-backed sample corpus under `testdata/fixtures/fast_feedback/`
- Add a backend runner that converts those samples into the same evaluator input family used by Golden/Shadow
- Add `POST /api/tuning/fast-feedback`

**Returned report must include:**

- sample count
- pass / retry / accept-with-notes counts
- per-sample verdict and overall score
- total duration
- prompt `version_tag` used for the run

**Rules:**

- Do not shell out to `go test` or `pipeline` subprocesses for Fast Feedback.
- Fast Feedback is read-only and ephemeral; it must not mutate the Golden manifest, Shadow sources, or run records.
- If fewer than 10 samples are present in the fixture set, the API fails loudly rather than silently using a shorter corpus.

**Tests:**

- Unit/integration test proves Fast Feedback always evaluates exactly 10 samples in deterministic order.
- API test proves the report shape includes the saved prompt `version_tag`.
- Frontend test verifies pending, success, and inline failure states.

### AC-5: Golden Eval runner UI reuses Story 4.1 mechanics and surfaces freshness/governance state

**Given** the operator opens the Golden Eval or Fixture Management sections
**When** the tuning data loads
**Then** the UI shows:

- current fixture pair list from the manifest
- pair count
- staleness warnings from `EvaluateFreshness`
- last successful Golden report if present

**And** the operator can add a fixture pair and run Golden eval from the UI.

**Required backend surface:**

- `GET /api/tuning/golden`
- `POST /api/tuning/golden/run`
- `POST /api/tuning/golden/pairs`

**Rules:**

- The fixture-add API accepts one positive file and one negative file together; there is no single-fixture add path.
- The 1:1 ratio rule is enforced by reusing manifest/pair validation from Story 4.1.
- Golden run success/failure uses the same report semantics as `RunGolden`, including manifest refresh and prompt-hash update on success.
- The UI must surface staleness warnings non-blockingly, matching the Story 4.1 advisory model.
- The browser must not write directly into `testdata/golden/eval/`; all writes go through the backend.

**Tests:**

- API test verifies pair-add rejects missing positive or missing negative input.
- API test verifies Golden run returns recall, detected negatives, false rejects, and manifest freshness metadata.
- Frontend test verifies staleness warning banner and fixture list rendering.

### AC-6: Shadow Eval runner UI reuses Story 4.2 mechanics and is sequence-gated after Golden

**Given** the operator is tuning the Critic prompt
**When** Golden has not yet passed in the current Tuning session
**Then** the Shadow Eval primary action is disabled with explanatory copy that Golden must pass first
**And** when Golden passes, Shadow becomes runnable from the same page.

**Required backend surface:**

- `POST /api/tuning/shadow/run`

**Required report fields:**

- window
- evaluated
- false rejections
- `summary_line`
- per-case result lines or structured result rows equivalent to `ShadowResult`

**Rules:**

- Backend execution reuses `RunShadow` with the configured `shadow_eval_window`.
- The UI treats false rejections as a failed result state, but the API still returns the full report payload rather than converting regressions into transport-level errors.
- Do not write any generated Shadow report file to `testdata/` or the repo.
- Do not add a CLI wrapper in this story; the goal is the UI surface.

**Tests:**

- API test verifies Shadow report is returned even when false rejections are present.
- Frontend test verifies the button disabled state before Golden success and enabled state after it.
- Frontend test verifies result rows and summary rendering for both pass and regression cases.

### AC-7: Saving a prompt immediately suggests Shadow Eval instead of auto-running it

**Given** a prompt save succeeds
**When** the save response returns
**Then** the Tuning page shows an inline recommendation banner or callout that a Shadow Eval should be run next
**And** the banner includes the saved `version_tag`
**And** the banner can launch the Shadow action directly once Golden prerequisites are satisfied.

**Rules:**

- This is a suggestion, not an automatic background job.
- The suggestion persists until the operator dismisses it, saves a newer prompt, or completes a Shadow run for that same `version_tag`.
- If Golden has not yet passed for the current prompt, the banner explains the required order: save -> Golden -> Shadow.

**Tests:**

- Frontend test verifies the recommendation banner appears after save with the returned `version_tag`.
- Frontend test verifies the banner copy changes depending on whether Golden has passed yet.

### AC-8: Calibration section reads persisted kappa trend from Story 4.3 and shows provisional state clearly

**Given** the Calibration section loads
**When** the backend returns persisted calibration snapshots
**Then** the UI shows:

- latest kappa value when available
- provisional badge when `n < window`
- latest `computed_at`
- a chronological trend view for recent points
- unavailable reason when kappa is nil

**Required backend surface:**

- `GET /api/tuning/calibration?window=<n>&limit=<n>`

**Rules:**

- Read from persisted `critic_calibration_snapshots`; do not recompute in the browser.
- Trend points must be returned oldest -> newest, matching `RecentCriticCalibrationTrend`.
- A nil/unavailable kappa is a first-class state, not an error toast.
- The first version may render the trend as a compact chart, sparkline, or table, but the trend direction must be visually legible.

**Tests:**

- API test verifies oldest -> newest ordering and provisional fields.
- Frontend test verifies provisional and unavailable states.
- Component test verifies the latest-point summary and trend visualization both render from the same payload.

## Tasks / Subtasks

- [ ] **T1: Add tuning backend API surface** (AC: 2, 5, 6, 8)
  - Add a Tuning handler/service boundary and register tuning routes in [internal/api/routes.go](/home/jay/projects/youtube.pipeline/internal/api/routes.go).
  - Reuse existing production packages (`internal/critic/eval`, `internal/db.CalibrationStore`) rather than subprocess execution.
  - Keep route registration centralized in the existing route file.

- [ ] **T2: Add prompt persistence + version metadata** (AC: 2, 3, 7)
  - Read/write `docs/prompts/scenario/critic_agent.md`.
  - Add a small helper to derive `git_short_sha` with `"nogit"` fallback.
  - Add run-level prompt-version persistence through a migration and run-create plumbing.

- [ ] **T3: Build Fast Feedback backend runner and sample corpus** (AC: 4)
  - Add deterministic fixture-backed 10-scene samples under `testdata/fixtures/fast_feedback/`.
  - Implement a backend report type and API endpoint for Fast Feedback.
  - Keep the evaluator input path aligned with the existing Critic contracts.

- [ ] **T4: Build TuningShell UI and React Query hooks** (AC: 1, 2, 4, 5, 6, 7, 8)
  - Expand [web/src/components/shells/TuningShell.tsx](/home/jay/projects/youtube.pipeline/web/src/components/shells/TuningShell.tsx) into the six-section workspace.
  - Extend [web/src/lib/apiClient.ts](/home/jay/projects/youtube.pipeline/web/src/lib/apiClient.ts), [web/src/lib/queryKeys.ts](/home/jay/projects/youtube.pipeline/web/src/lib/queryKeys.ts), and relevant Zod contracts.
  - Use inline error and loading states consistent with current Production/Settings patterns.

- [ ] **T5: Add fixture management UI with pair-only enforcement** (AC: 5)
  - Show manifest-backed pair list, freshness warnings, and add-pair affordance.
  - Enforce positive+negative pair submission in one action.

- [ ] **T6: Add Golden/Shadow/calibration tests across Go + React** (AC: 2-8)
  - Go: handler/service/integration tests for tuning endpoints.
  - React: `TuningShell.test.tsx` and contract/apiClient tests.
  - E2E: Playwright happy path for prompt edit -> Golden -> Shadow suggestion and calibration visibility.

## Dev Notes

- Prefer adding a dedicated `internal/api/handler_tuning.go` plus a small service adapter instead of bloating existing run handlers.
- For prompt save metadata, a helper near the tuning service is better than wiring git logic into handlers.
- The UI should treat Golden and Shadow as asynchronous jobs with explicit request/response cycles, but V1 may keep them synchronous HTTP calls as long as the UX shows a busy state and the operations are local/dev-scale.
- Keep all new TypeScript contracts versioned with the existing `{ version: 1, data: ... }` response envelope convention.
- Be careful not to let prompt-version additions break existing `runSummarySchema` consumers; nullable fields are the safest path.

## Validation

- `go test ./internal/api ./internal/service ./internal/critic/eval ./internal/db`
- `go test ./cmd/pipeline ./...`
- `cd web && npm test -- --run TuningShell`
- existing Playwright smoke remains green
- add a new Playwright tuning flow spec once the surface exists

## Open Questions / Assumptions

- Assumption: V1 Fast Feedback is Critic-only even if the UI language says "specific stage", because this story edits only the Critic prompt and the repo already has mature Golden/Shadow Critic infrastructure. If broader stage tuning is required later, extend the same API shape rather than redesigning it.
- Assumption: run-level prompt comparison only needs `critic_prompt_version` and `critic_prompt_hash` now; richer prompt-history browsing belongs to a later story if needed.
