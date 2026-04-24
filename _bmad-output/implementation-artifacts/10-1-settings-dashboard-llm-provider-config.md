# Story 10.1: Settings Dashboard — LLM & Provider Config

Status: done

## Story

As an operator,
I want to manage system settings and provider configuration through the Settings dashboard,
so that I can tune model behavior and budget guardrails without editing raw files by hand.

## Prerequisites

**Hard dependencies already in place:**
- Story 6.2 established the 3-route shell (`/production`, `/tuning`, `/settings`) and the Settings route entry point through `SettingsShell`.
- Story 7.1 established live run telemetry (`cost_usd`, stage/status polling, shared query-key patterns) that this story should reuse for budget visualization rather than re-implement.
- Story 8.6 established the Settings-side `TimelineView` interaction model and keyboard-scoped list behavior. Story 10.1 must extend the Settings tab, not replace the existing decision-history surface.

**Current codebase reality you must build on:**
- `web/src/components/shells/SettingsShell.tsx` is still a thin shell that renders intro copy plus `TimelineView`. This story turns that placeholder into the real operational settings workspace.
- `internal/domain/PipelineConfig` already defines the editable non-secret settings surface:
  `writer_model`, `critic_model`, `tts_model`, `tts_voice`, `tts_audio_format`, `image_model`,
  `writer_provider`, `critic_provider`, `image_provider`, `tts_provider`, `dashscope_region`,
  stage cost caps, `cost_cap_per_run`, `anti_progress_threshold`, `golden_staleness_days`,
  `shadow_eval_window`, `auto_approval_threshold`, `blocked_voice_ids`, and path settings.
- `.env` is secret-only by architecture. `internal/config/loader.go` loads `.env` first for secrets, then `config.yaml` for non-secret settings. Do not blur that boundary in the UI.
- `internal/config/doctor.go` already contains real validation rules/operators depend on:
  API key presence, writable filesystem paths, FFmpeg availability, and Writer ≠ Critic provider separation.
- The pipeline state machine already defines the only safe "pause points" for a running workflow:
  next stage entry or a fresh run. `internal/pipeline/engine.go` makes stage transitions explicit; Story 10.1 must queue config changes until those seams rather than mutating an in-flight stage mid-execution.

**What does NOT exist yet and must be created carefully:**
- There is currently no backend API for reading/writing `config.yaml` or `.env` from the SPA.
- There is currently no persistence model for "pending configuration changes" that should apply only on the next stage boundary or next run.
- There is currently no budget-summary endpoint that combines current run spend with configured soft/hard caps for the Settings UI.

**Security / architecture guardrails:**
- The browser must never parse or rewrite raw config files directly. File I/O belongs on the Go side behind explicit API handlers.
- `.env` editing must be narrowly scoped to known secret keys. Do not expose arbitrary file-text editing, multiline raw editors, or a generic "write any env line" surface.
- `config.yaml` remains the home for non-secret knobs; `.env` remains the home for API keys and other secrets loaded via environment variables.
- Avoid introducing runtime behavior where a save instantly mutates an already-running stage implementation. Queue and surface the pending application state instead.

## Acceptance Criteria

### AC-1: Settings tab becomes a real configuration workspace without regressing TimelineView

**Given** the operator opens `/settings`
**When** the Settings shell renders
**Then** the page shows a dedicated configuration panel for LLM/provider settings and cost guardrails above or beside the existing Timeline section
**And** the existing `TimelineView` remains available in the same route
**And** the layout follows the UX guidance for Settings as a dense single-surface operational workspace rather than a wizard or modal flow.

**Rules:**
- Do not replace the entire route with a full-screen editor.
- Do not move TimelineView out of Settings; Story 8.6 already established it as part of this tab.
- Prefer compact property-panel anatomy that matches the architecture/UX direction for Settings & History.

**Tests:**
- Component test verifies `SettingsShell` renders both the configuration surface and TimelineView together.
- Integration test verifies the settings panel is present on `/settings` and absent from `/production` / `/tuning`.

---

### AC-2: Operator can load current config and edit validated non-secret + secret fields through typed forms

**Given** the Settings dashboard loads successfully
**When** the operator views the configuration form
**Then** the current values for editable `config.yaml` fields are displayed in typed controls
**And** the current values for supported `.env` secret keys are represented safely in secret inputs
**And** the UI clearly distinguishes which values persist to `config.yaml` versus `.env`
**And** client-side validation catches obviously invalid input before submit.

**Minimum editable scope for Story 10.1:**
- `config.yaml`:
  - provider/model fields for writer, critic, image, and TTS
  - `tts_voice`, `tts_audio_format`, `dashscope_region`
  - stage cost caps (`cost_cap_research`, `cost_cap_write`, `cost_cap_image`, `cost_cap_tts`, `cost_cap_assemble`)
  - `cost_cap_per_run`
- `.env`:
  - `DASHSCOPE_API_KEY`
  - `DEEPSEEK_API_KEY`
  - `GEMINI_API_KEY`

**Validation requirements:**
- Required provider/model fields cannot be empty.
- Writer and Critic providers must remain different, matching `config/doctor.go`.
- Numeric caps must be non-negative and `cost_cap_per_run` must be greater than or equal to the highest relevant stage cap.
- Secret inputs may be left unchanged without forcing the operator to re-enter them.
- The API must reject unsupported keys and malformed payloads.

**Rules:**
- Do not expose raw YAML or raw `.env` textareas in V1.
- Do not allow the client to submit unknown config keys.
- Preserve file-format stability on save; comments/order may change only if the chosen serializer already does so consistently.

**Tests:**
- Contract/unit tests for request/response schemas.
- Handler/service tests for valid save, invalid payload, unsupported key, and Writer == Critic rejection.
- Component tests for inline validation and secret-field masking behavior.

---

### AC-3: Save persists to `config.yaml` / `.env` through backend APIs, not browser-side file mutation

**Given** the operator submits valid changes from the Settings dashboard
**When** the save succeeds
**Then** the backend persists non-secret values to `config.yaml`
**And** supported secret values persist to `.env` with secrets remaining file-backed, not database-backed
**And** the response returns the normalized saved configuration plus metadata describing whether the changes apply immediately or are queued
**And** the UI shows a factual inline success state.

**Implementation guardrail:**
- Introduce a dedicated settings read/write API surface, for example:
  - `GET /api/settings`
  - `PUT /api/settings`
- The request payload should separate `config` and `env` sections instead of mixing them into one flat bag.

**Rules:**
- Do not shell out to `sed`, `awk`, or ad-hoc CLI mutation helpers from handlers if a normal Go read/modify/write path is feasible.
- `.env` writes must preserve file permissions expectations (`0600` on creation today; do not accidentally broaden secrecy posture).
- Writes should be atomic enough to avoid partially written config on process interruption.

**Tests:**
- Integration test verifies read → edit → save → reload cycle for both files.
- Tests verify omitted secret fields are preserved rather than blanked.

---

### AC-4: Active-run changes are queued until next stage entry or next new run

**Given** one or more runs are currently active
**When** the operator saves settings that could affect pipeline execution
**Then** the system does not inject those changes into the currently executing stage
**And** the save result is marked as `queued`
**And** the queued configuration is applied only at the next safe seam:
  - the next stage entry for that run, or
  - the creation/start of a new run
**And** the Settings UI clearly communicates that a pending configuration version exists and has not fully taken effect yet.

**Safe-seam definition for this story:**
- Mid-stage mutation is forbidden.
- Stage-boundary application means the runtime reads the new config before entering the next stage, not while the current stage is already executing.
- For `pending` runs that have not started yet, the queued config may be treated as immediately effective for that run before `EventStart`.

**Required backend shape:**
- Persist a pending-settings record/version that can coexist with the currently effective settings.
- Expose enough metadata for the UI to show:
  - current effective version
  - pending version
  - queued_at timestamp
  - application status (`effective` vs `queued`)

**Rules:**
- Do not mutate a live stage's provider/model/voice/cost-cap inputs in memory halfway through the stage.
- Do not solve this with a global in-process mutable singleton only; the queued state must survive server restarts.
- If multiple saves occur while changes are still queued, last save wins unless the implementation deliberately versions/merges them. Pick one explicit rule and test it.

**Tests:**
- Service/integration test proving a run already inside a stage keeps old config for that stage.
- Integration test proving the queued config becomes effective on the next stage transition.
- Integration test proving a brand-new run uses the newest saved config.

---

### AC-5: Settings dashboard shows soft/hard budget indicators against actual observed spend

**Given** current observed cost usage from run telemetry
**When** the operator opens Settings
**Then** a visual budget indicator shows current spend against the configured soft/hard caps
**And** the indicator distinguishes safe, near-cap, and exceeded states
**And** the operator can see both the numeric amounts and the relative progress.

**Story-10.1 visualization minimum:**
- Current spend in USD
- Configured `cost_cap_per_run` hard cap
- A soft-cap threshold derived from configuration or a documented percentage of the hard cap
- Progress component + textual label (for example `72% of hard cap used`)

**Data requirements:**
- Reuse the existing `cost_usd` observability model already surfaced in run detail/status.
- If no active run is selected, the UI may show the latest run or a dashboard aggregate, but the chosen source must be explicit and documented in the implementation.

**Rules:**
- Do not invent fake budget numbers in the UI.
- If soft-cap is derived rather than explicitly configured, document the derivation in code/tests and surface the same logic consistently across UI and backend.
- Reuse existing shared formatter helpers (`formatCurrency`, shared status tone patterns) where possible.

**Tests:**
- Component test covers below-soft-cap, between-soft-and-hard, and exceeded states.
- Contract/integration test verifies the budget payload matches actual run cost data and configured caps.

---

### AC-6: Save flow runs server-side validation and reports actionable errors inline

**Given** the operator submits invalid settings
**When** the backend rejects the change
**Then** the UI keeps the form open
**And** the relevant error is shown inline without losing unsaved input
**And** no partial configuration is applied.

**Required rejection cases:**
- Writer provider equals Critic provider
- Missing required provider/model identifiers
- Negative or malformed numeric caps
- Missing required API key when the operator explicitly clears a key and saves
- Filesystem persistence failure

**Tests:**
- Handler/service tests for each rejection class
- Component tests verifying inline error rendering and field-state preservation

## Tasks / Subtasks

- [x] Task 1: Add backend settings contracts + persistence seams (AC: 2, 3, 6)
  - [x] Define request/response DTOs and Zod/Go contract parity for `GET /api/settings` and `PUT /api/settings`
  - [x] Create a backend settings service responsible for loading effective settings, masked secrets metadata, validation, and atomic persistence
  - [x] Implement typed read/write helpers for `config.yaml` and supported `.env` keys only

- [x] Task 2: Implement server-side validation rules from existing config semantics (AC: 2, 6)
  - [x] Reuse `internal/domain/PipelineConfig` as the source of truth for editable config fields
  - [x] Enforce Writer ≠ Critic provider rule from `internal/config/doctor.go`
  - [x] Add validation for numeric budget fields and supported secret-key handling

- [x] Task 3: Add queued-settings application model (AC: 4)
  - [x] Introduce persistent storage for effective vs pending settings versions
  - [x] Define last-write-wins or explicit version semantics for repeated saves while queued
  - [x] Hook config resolution into safe application seams only: next stage entry and new run start
  - [x] Add tests proving current-stage immutability and next-stage/new-run adoption

- [x] Task 4: Expose budget summary data for Settings (AC: 5)
  - [x] Add an API payload that combines configured caps with observed spend
  - [x] Decide and document the soft-cap rule if it is derived rather than stored
  - [x] Reuse existing observability/run-cost sources instead of duplicating cost calculations

- [x] Task 5: Build the Settings configuration UI in the SPA (AC: 1, 2, 3, 5, 6)
  - [x] Extend `web/src/components/shells/SettingsShell.tsx` to include a real settings panel while preserving `TimelineView`
  - [x] Add focused components under `web/src/components/settings/` for provider/model fields, secrets fields, budget indicator, and queued-change banner
  - [x] Add React Query hooks and API client methods for load/save/settings summary
  - [x] Show inline success, queued, and validation-error states without toast-only messaging

- [x] Task 6: Verification coverage (AC: 1-6)
  - [x] Backend unit/integration tests for settings read/write, queued application, and budget summary
  - [x] Frontend component/integration tests for SettingsShell form behavior and indicators
  - [x] If the Settings route becomes a key operator flow, add/update a Playwright smoke covering load + edit + save + queued-state message

### Review Findings (2026-04-24)

**Summary:** 11 decision-needed, 17 patch, 9 defer, 5 dismissed. All decision-needed + patch findings resolved 정석적 (textbook) 방법으로.

**Focus-area outcomes after fixes:**
- **Focus 1 (file separation)**: now PASS — `settings_versions.env_json` replaced with `env_fingerprint` (SHA-256 hash); raw secrets never DB-persisted.
- **Focus 2 (queuing logic)**: now PASS — disk writes deferred until `PromotePendingAtSafeSeam`; `effective_version` seeded by startup `Bootstrap`; promotion centralized in `pipeline.Engine` stage-advance path (Advance + Resume + assemble→metadata_ack); runs pinned via `run_settings_assignments`.
- **Focus 3 (live budget)**: now PASS — `useSettingsQuery` polls every 5s with `staleTime: 2s`; budget reflects live `cost_usd`.

- [x] [Review][Decision] D1 Safe-seam bypass → resolved: `Save` now writes to DB only while active runs exist; disk is materialized exclusively in `PromotePendingAtSafeSeam`. `LoadEffectiveRuntimeFiles` reads from the DB-effective version; disk fallback is gated behind `Bootstrap` seeding. See [internal/service/settings_service.go:50-102](../../internal/service/settings_service.go) + [cmd/pipeline/serve.go](../../cmd/pipeline/serve.go).
- [x] [Review][Decision] D2 Secrets in DB plaintext → resolved: migration 012 replaces `env_json` column with `env_fingerprint` (SHA-256 hex). Raw secret bytes never reach SQLite. Fingerprint is deterministic across equivalent env maps for version auditability. See [migrations/012_settings_state.sql](../../migrations/012_settings_state.sql) + [internal/db/settings_store.go:envFingerprint](../../internal/db/settings_store.go).
- [x] [Review][Decision] D3 Promotion at one seam → resolved: `Engine.promoteSettingsAtBoundary` invoked in `Advance` (before Phase A) and `Resume` (before retried stage runs + before `assemble → metadata_ack`). `SetSettingsPromoter` wiring added; `CharacterPick` promotion kept for HITL seam completeness. See [internal/pipeline/resume.go](../../internal/pipeline/resume.go).
- [x] [Review][Decision] D4 Two-file atomicity → resolved: `SettingsFileManager.Write` snapshots both files before either write, restores `config.yaml` on `.env` failure (pseudo-2PC). Surface failures that can't roll back include both errors. See [internal/config/settings_files.go:40-71](../../internal/config/settings_files.go).
- [x] [Review][Decision] D5 Budget indicator live → resolved: `useSettingsQuery` now has `refetchInterval: 5_000` + `staleTime: 2_000`; `BudgetIndicator` consumes the snapshot directly so live `cost_usd` flows through the same channel without a second subscription. See [web/src/hooks/useSettings.ts](../../web/src/hooks/useSettings.ts).
- [x] [Review][Decision] D6 ETag / If-Match → resolved: `GET /api/settings` sets `ETag: "N"`; `PUT /api/settings` enforces `If-Match` via `SettingsService.Save(..., ifMatchVersion)`; mismatch → `ErrSettingsConflict` → HTTP 409 `SETTINGS_STALE`. Handler test `TestSettingsHandler_PutReturns409WhenIfMatchStale` covers the path. See [internal/api/handler_settings.go](../../internal/api/handler_settings.go), [internal/service/settings_service.go:Save](../../internal/service/settings_service.go).
- [x] [Review][Decision] D7 Shared limiter → resolved: `ProviderLimiterFactory` now constructed once at `runServe` startup and injected into `dynamicPhaseBExecutor.limiterFactory`; every Phase B rebuild reuses the same limiter budget for `DashScopeTTS()` / `DashScopeImage()`. See [cmd/pipeline/serve.go:dynamicPhaseBExecutor](../../cmd/pipeline/serve.go).
- [x] [Review][Decision] D8 `run_settings_assignments` → resolved: `RunService.Create` calls `AssignRunToEffectiveVersion` after run insert. `dynamicPhaseBExecutor.Run` resolves via `LoadRuntimeFilesForRun(runID)` which consults `run_settings_assignments` first, preserving per-run pin even when a newer version is promoted mid-flight. Test `TestSettingsService_LoadRuntimeFilesForRun_UsesPinnedVersion` covers the invariant. See [internal/db/settings_store.go:AssignRunToVersion](../../internal/db/settings_store.go).
- [x] [Review][Decision] D9 Orphan pending → resolved: `SettingsService.Save` promotes any lingering pending version before applying a new save when `ActiveRunsExist` has flipped to false, then materializes to disk so queued intent reaches effective exactly once. See [internal/service/settings_service.go:Save](../../internal/service/settings_service.go).
- [x] [Review][Decision] D10 Corrupted YAML recovery → resolved: `config.ErrCorruptedConfig` sentinel returned on parse failure; handler translates to HTTP 422 `SETTINGS_CORRUPTED`; SPA shows "Reset to defaults" action wired to `POST /api/settings/reset`. Test `renders corruption recovery UI when config.yaml is unreadable` covers the SPA path. See [internal/config/settings_files.go:ErrCorruptedConfig](../../internal/config/settings_files.go), [web/src/components/shells/SettingsShell.tsx](../../web/src/components/shells/SettingsShell.tsx).
- [x] [Review][Decision] D11 Clear secret → resolved: `updateSettings` accepts `Record<string, string | null>`; `null` becomes `null` in JSON and is treated by the service as "clear". `SecretFieldsPanel` has a Clear / Undo-clear button and shows "Will be cleared on save" placeholder. See [web/src/lib/apiClient.ts](../../web/src/lib/apiClient.ts), [web/src/components/settings/SecretFieldsPanel.tsx](../../web/src/components/settings/SecretFieldsPanel.tsx).

- [x] [Review][Patch] P1 `Bootstrap` seeds `effective_version` from disk at startup via `SettingsStore.EnsureEffectiveVersion`; called from `runServe` (and test helpers) so fresh installs never trigger the raw-disk fallback.
- [x] [Review][Patch] P2 `RunService.Create` validates `scpID` BEFORE promotion; a malformed request no longer triggers `PromotePendingAtSafeSeam` side effects.
- [x] [Review][Patch] P3 `parseEnvLine` now uses matched-pair quote strip — `"foo"` and `'bar'` round-trip correctly; asymmetric values no longer lose characters.
- [x] [Review][Patch] P4 `writeAtomic` now fsyncs the temp file before rename and fsyncs the parent directory after; rename is durable across crashes.
- [x] [Review][Patch] P5 `.env` writes always force `0600` regardless of existing mode — a previously widened file cannot stay widened through a Save.
- [x] [Review][Patch] P6 `BudgetSourceRun` splits into three ordered tiers: running/waiting → failed → latest. Active-run spend is no longer masked by a newer failed run; `TestSettingsService_BudgetPrefersRunningOverFailed` covers the invariant.
- [x] [Review][Patch] P7 `loadStateFromQuerier` handles `sql.ErrNoRows` by returning a zero-valued state — no more 500 on missing sentinel row.
- [x] [Review][Patch] P8 Handler errors route through `clientMessage` sanitization; raw validation error text no longer reaches the envelope `message` field.
- [x] [Review][Patch] P9 `Save` error wrapping uses a single `%w` chain; `SettingsValidationError` implements `Is(domain.ErrValidation)` so the `errors.Is` branch in the handler is correct and the fallback arm is unreachable code (removed).
- [x] [Review][Patch] P10 `writeConfigFile` stats the existing file and preserves its mode when present; only falls back to `0644` for brand-new files.
- [x] [Review][Patch] P11 `SettingsValidationError.Error()` sorts field keys before picking the fallback message — deterministic output across runs.
- [x] [Review][Patch] P12 `ProviderConfigPanel` ignores empty / NaN numeric input rather than serializing `NaN` → `null`; submit is blocked by client `validateSettings` via an explicit "Must be a number" error.
- [x] [Review][Patch] P13 `QueuedChangeBanner` accuracy follows from P1 + D1: runtime never reads disk while queued, so the banner's "not yet effective" claim is truthful by construction.
- [x] [Review][Patch] P14 `App.test.tsx` mocks now include `ETag` header; each `render(<App />)` call still instantiates a fresh `QueryClient` via `useState` so cross-test cache leakage is structurally impossible.
- [x] [Review][Patch] P15 `TestSchema_SettingsStateSentinelRowExists` and `TestSchema_SettingsVersionsHasFingerprintNotEnvJSON` assert both the migration-seeded sentinel row and the column layout.
- [x] [Review][Patch] P16 `internal/pipeline/settings_promotion_test.go` covers: Advance-promotes, Resume-promotes, promoter-error-is-non-fatal. Service-level `TestSettingsService_SaveQueuedWhenActiveRunExists` additionally asserts disk is NOT touched while queued.
- [x] [Review][Patch] P17 `SaveSnapshot` and `PromotePending` now read post-update state inside the same `*sql.Tx` (via `loadStateFromQuerier`) before commit — concurrent writers cannot observe a different state than they wrote.

- [x] [Review][Defer] DF1 `dynamicPhaseBExecutor` re-parses `config.yaml`/`.env` from disk every invocation — transient I/O errors fail active stages; related to D7 caching decision [cmd/pipeline/serve.go:50-54](../../cmd/pipeline/serve.go) — deferred, pre-existing
- [x] [Review][Defer] DF2 `dynamicPhaseBExecutor` logs nothing about which settings version ran — no post-hoc correlation of regressions to version [cmd/pipeline/serve.go:50-65](../../cmd/pipeline/serve.go) — deferred, observability enhancement
- [x] [Review][Defer] DF3 Budget `progress_ratio=0` with `hardCap=0` shows empty bar + "Exceeded" pill — edge-case UX [internal/service/settings_service.go:156-165](../../internal/service/settings_service.go) — deferred, unusual config
- [x] [Review][Defer] DF4 `DataDir`/`OutputDir`/`DBPath` reset to defaults on first save if `config.yaml` missing at startup — `doctor` already catches missing config upstream [internal/config/settings_files.go:1589-1617](../../internal/config/settings_files.go) — deferred, upstream guard exists
- [x] [Review][Defer] DF5 `SecretFieldsPanel` 10s `staleTime` allows cross-tab lost-update of secrets ([web/src/hooks/useSettings.ts:7](../../web/src/hooks/useSettings.ts)) — deferred, multi-operator coordination out of MVP
- [x] [Review][Defer] DF6 `QueuedChangeBanner` shows no pending-version preview/diff — operator can't tell if queued state is their save or someone else's [web/src/components/settings/QueuedChangeBanner.tsx](../../web/src/components/settings/QueuedChangeBanner.tsx) — deferred, UX enhancement
- [x] [Review][Defer] DF7 Soft-cap ratio `0.8` only lives in backend `settingsSoftCapRatio` constant; frontend can't independently validate — Dev Notes documents derivation [internal/service/settings_types.go:6](../../internal/service/settings_types.go) — deferred, payload consistency is sufficient
- [x] [Review][Defer] DF8 No audited secret-logging scrubbing check — `writeDomainError` doesn't log secrets today but service error wrapping could surface env-writer OS errors [internal/service/settings_service.go](../../internal/service/settings_service.go) — deferred, preventive hardening, not an identified leak
- [x] [Review][Defer] DF9 `QueuedAt` uses `time.RFC3339` without sub-second precision — micro-collision ambiguity on burst saves [internal/service/settings_service.go:69-72](../../internal/service/settings_service.go) — deferred, rare and low-impact

## Dev Notes

### Story Intent and Scope Boundary

- Story 10.1 turns the Settings tab from a placeholder/history page into the operator's real control surface for provider/model and budget management.
- The value is not just editing files through a prettier UI. The hard part is preserving runtime safety:
  configuration edits must be validated, persisted cleanly, and applied only at safe execution boundaries.
- Keep the scope focused on provider/model settings, API-key management, and budget visibility.

**Out of scope for 10.1:**
- Prompt/rubric editing and fast-feedback experimentation (Story 10.2)
- Retention/VACUUM operations (Story 10.3)
- CI golden/shadow gate wiring (Story 10.4)
- Generic arbitrary config-file editing for every `PipelineConfig` field

### Architecture Intelligence

- `internal/config/loader.go` already codifies the hierarchy: defaults → `.env` → `config.yaml` → env vars → CLI flags. The new settings APIs must not silently break that contract.
- Because CLI flags and runtime environment variables override file values, the UI should treat file persistence as "saved project defaults", not a universal reflection of every possible effective runtime override. If a stronger override is detected later, surface it as metadata rather than pretending the file is authoritative.
- `internal/domain/PipelineConfig` is the canonical schema for non-secret persisted settings. Avoid creating a second divergent config model.
- `internal/pipeline/engine.go` gives the right conceptual seam for queued application. Do not attempt to hot-swap config in the middle of `research`, `write`, `image`, etc.

### Frontend Guidance

- Follow the existing SPA organization:
  - `web/src/components/shells/SettingsShell.tsx`
  - new settings-focused components under `web/src/components/settings/`
  - query keys in `web/src/lib/queryKeys.ts`
  - API calls in `web/src/lib/apiClient.ts`
- Reuse the repo's established patterns:
  - TanStack Query for server state
  - inline status/error copy instead of toast-driven workflows
  - existing currency/formatting helpers for budget display
- Preserve the current Settings tone: an operational control room, not a marketing-style form.

### Backend Guidance

- A dedicated backend settings module is preferable to stuffing file read/write logic into handlers.
- Keep persistence narrow and explicit:
  - typed config struct for editable YAML fields
  - explicit env key whitelist for secrets
  - atomic file writes where possible
- If queued-settings persistence needs a DB table, prefer a small, explicit table or durable store entry rather than an in-memory map.

### File / Module Expectations

- Likely backend touchpoints:
  - `internal/api/routes.go`
  - new settings handler/service package(s) under `internal/api/` and `internal/service/`
  - `internal/config/` helpers or a new focused persistence helper
  - migration/storage for queued settings if DB-backed
- Likely frontend touchpoints:
  - `web/src/components/shells/SettingsShell.tsx`
  - `web/src/components/settings/*`
  - `web/src/lib/apiClient.ts`
  - `web/src/lib/queryKeys.ts`
  - `web/src/contracts/runContracts.ts` or a dedicated settings contracts file if that keeps responsibilities clearer
  - `web/src/index.css` for settings-panel and budget-indicator styling

### Testing Requirements

- Backend:
  - read/write cycle for `config.yaml`
  - masked/unchanged secret behavior for `.env`
  - queued-settings application timing
  - validation failures and atomicity
- Frontend:
  - SettingsShell composition with TimelineView preserved
  - form validation and masked secret editing
  - queued-state banner/message
  - budget-indicator tone changes by threshold
- End-to-end:
  - at minimum, one operator-path smoke from `/settings` load to successful save

### Git Intelligence Summary

- Recent commits are still centered on Epic 9 story generation, while the web shell already contains the Production/Settings route framework. Story 10.1 should layer onto that existing shell rather than inventing a parallel admin app.
- The workspace is currently clean, so the dev agent can create the story implementation branch/file changes without first untangling unrelated local edits.

## Open Questions / Assumptions

- Assumption: Story 10.1 should cover only the most operator-relevant subset of `PipelineConfig`, not every existing config key. The scope above is intentionally constrained to LLM/provider/budget settings plus required API keys.
- Assumption: soft cap can be derived from hard cap if no explicit persisted soft-cap field exists yet. If the team wants a user-editable soft-cap value, add it deliberately to `PipelineConfig` rather than hiding an ad-hoc frontend-only preference.
- Assumption: queued-settings durability likely belongs in the database or another restart-safe store. An in-memory queue would violate the "save" expectation for operators.

## Dev Agent Record

### Debug Log

- Added migration `012_settings_state.sql` for persisted settings versions, effective/pending state, and run assignment storage.
- Implemented typed settings file management in `internal/config/settings_files.go` and a new settings store/service/handler stack for `GET /api/settings` and `PUT /api/settings`.
- Wired queued-settings promotion into safe seams for new run creation and character-pick stage advancement, and switched the serve-time Phase B executor to load the effective runtime snapshot dynamically.
- Replaced the placeholder Settings shell with a real configuration workspace while preserving `TimelineView`, plus added client contracts/hooks and inline validation flows.

### Completion Notes

- Soft cap is derived as `80%` of `cost_cap_per_run` and used consistently by backend payload generation and the UI budget indicator.
- Save behavior is last-write-wins while queued: the latest persisted version becomes the pending version until a safe seam promotes it.
- `.env` save behavior preserves unsupported or untouched lines and keeps supported secrets masked in the SPA response; blank untouched secret inputs do not overwrite existing values.
- Playwright smoke coverage was updated for `/settings`, but Playwright was not executed in this implementation pass.

## File List

- `migrations/012_settings_state.sql`
- `internal/domain/settings.go`
- `internal/config/settings_files.go`
- `internal/db/settings_store.go`
- `internal/db/sqlite_test.go`
- `internal/service/settings_types.go`
- `internal/service/settings_service.go`
- `internal/service/settings_service_test.go`
- `internal/service/run_service.go`
- `internal/service/character_service.go`
- `internal/api/response.go`
- `internal/api/routes.go`
- `internal/api/handler_settings.go`
- `internal/api/handler_settings_test.go`
- `cmd/pipeline/serve.go`
- `cmd/pipeline/resume.go`
- `web/src/contracts/settingsContracts.ts`
- `web/src/hooks/useSettings.ts`
- `web/src/lib/apiClient.ts`
- `web/src/lib/queryKeys.ts`
- `web/src/components/settings/BudgetIndicator.tsx`
- `web/src/components/settings/ProviderConfigPanel.tsx`
- `web/src/components/settings/QueuedChangeBanner.tsx`
- `web/src/components/settings/SecretFieldsPanel.tsx`
- `web/src/components/shells/SettingsShell.tsx`
- `web/src/components/shells/SettingsShell.test.tsx`
- `web/src/App.test.tsx`
- `web/src/index.css`
- `web/e2e/smoke.spec.ts`

## Change Log

- 2026-04-24: Implemented the Settings dashboard provider/config workspace, backend settings APIs, queued-settings persistence/promotion, budget summary payloads, SPA validation flow, and supporting tests.
