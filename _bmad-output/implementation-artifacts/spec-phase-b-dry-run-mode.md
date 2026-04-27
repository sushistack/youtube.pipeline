---
title: 'Phase B dry-run mode (no DashScope image / TTS calls)'
type: 'feature'
created: '2026-04-27'
status: 'done'
baseline_commit: '58642c01086ee12b6035c3c5db940cbb96f495e2'
context:
  - '{project-root}/internal/domain/llm.go'
  - '{project-root}/internal/llmclient/dashscope/image.go'
  - '{project-root}/internal/llmclient/dashscope/tts.go'
  - '{project-root}/cmd/pipeline/serve.go'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Iterating on image / TTS prompts during local development burns DashScope credits on every retry, but the operator only needs the *prompt path + audit log + downstream pipeline state machine* to behave correctly. Real pixels and audio are wasted spend until prompts are dialed in.

**Approach:** Add a single `DryRun` boolean to effective `PipelineConfig`. When true, `buildPhaseBRunner` swaps the DashScope `ImageGenerator` / `TTSSynthesizer` clients for in-process fakes that produce valid placeholder PNG / WAV files at the same on-disk paths. The Phase B track code, segment store writes, audit logging, BatchReview UX, and Phase A LLMs are all untouched. A guard at `Engine.Advance(StageAssemble)` blocks ffmpeg assembly for runs created under dry-run, so placeholder assets never compose into a final video.

## Boundaries & Constraints

**Always:**
- Fake clients implement the existing `domain.ImageGenerator` / `domain.TTSSynthesizer` interfaces — no new interfaces, no track-code modifications.
- Fakes write a real, openable file at the caller-provided `OutputPath` (via the same temp+rename atomic-write pattern used by `dashscope.writeFileAtomic`) — downstream code reads it with `os.Open` / ffmpeg `concat`, so a zero-byte stub or no-op breaks Phase C.
- Fake WAV is 44.1 kHz / 16-bit / mono PCM (matches `tts.go:188` byte-rate constant `176_400`). The reported `DurationMs` MUST equal `len(audioBytes) * 1000 / 176400` so the existing track logic's totalDuration math stays consistent across real and dry-run.
- `runs.dry_run` is snapshotted at row creation from the *effective* config — there is no per-run override UI. Mid-flight settings toggling does not retroactively change a row.
- Settings save / load round-trips the field through the YAML-on-disk path (`config.yaml`) and the DB-backed `settings_versions` row — same path every other config field uses.

**Ask First:**
- Any change to `domain.PipelineConfig` ordering or other call-sites that already pin the runtime snapshot (e.g. `prompt.ActivePromptVersion()` parallel) — surface before refactoring.
- If validation logic in `validateSettingsConfig` needs to reject anything related to dry-run combined with other fields (e.g. CostCapImage), HALT — current decision is no extra validation: dry-run is a free-toggle bool.

**Never:**
- Don't add per-stage flags (`dry_run_image`, `dry_run_tts`) — agreed: single switch, both expensive surfaces together. Out of scope.
- Don't modify `internal/pipeline/image_track.go`, `internal/pipeline/tts_track.go`, or `phase_b.go`. Fakes drop in at the interface boundary; touching tracks is dead-layer work.
- Don't add a UI badge / banner on Production / BatchReview shells. User explicitly requested "이질감 없게" — Settings page is the source of truth and the StageAssemble guard is the safety net.
- Don't touch the in-flight `git status` modified files (handler_run.go, BatchReview.tsx, etc.) — they are unrelated work; commits in this spec must stage only dry-run-relevant files.
- Don't gate Phase A (DeepSeek/Gemini text generation) — out of scope, separate flag if ever needed.
- Don't put the "block placeholder export" guard in `service/export_service.go` (that's the JSON/CSV decision export from Story 10.5, not Phase D ffmpeg). Guard goes in `internal/pipeline/resume.go` at the `case run.Stage == domain.StageAssemble:` arm of `Engine.Advance`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Image fake — happy path | `dryrun.ImageClient.Generate` with valid `OutputPath`, width=2688, height=1536 | A `width × height` PNG of solid color #2a2a2a is atomically written to `OutputPath`. Returns `ImageResponse{ImagePath: req.OutputPath, Provider: "dryrun", Model: req.Model, CostUSD: 0, DurationMs: small_real_elapsed}`. | Same validation errors as real client (empty prompt/model/output path) → `ErrValidation` |
| Image fake — Edit | `dryrun.ImageClient.Edit` with a reference image URL | Same PNG output as Generate; ref URL is ignored. Returns ImageResponse with `Model: req.Model` (the edit model id). | Same validation as real client. |
| TTS fake — happy path | `dryrun.TTSClient.Synthesize` with text `"안녕하세요"` (5 codepoints), format `"wav"`, OutputPath set | A valid WAV (44 byte RIFF header + zero-PCM body) is atomically written. Body byte count = `round(len([]rune(text)) * 0.14 * 176400)`. Returns `TTSResponse{AudioPath: OutputPath, Provider: "dryrun", Model: req.Model, DurationMs: len(audioBytes)*1000/176400, CostUSD: 0}`. | Same validation as real client (empty text/output path) → `ErrValidation` |
| TTS fake — chunked narration | tts_track splits long text → calls Synthesize per chunk | Each chunk produces an independent valid WAV with consistent codec params; ffmpeg concat-demuxer with `-c copy` merges them without error. | If write fails → wrap as `ErrStageFailed` |
| Settings round-trip | User toggles `dry_run` in UI, saves; reloads page | New value persisted to `config.yaml` and DB; `LoadEffectiveRuntimeConfig` returns it; new run picks it up. | Validation never rejects dry_run on its own — it's a free bool. |
| Run snapshot | `RunService.Create` invoked while `cfg.DryRun=true` | New `runs` row has `dry_run = 1`. `Get(runID)` returns `domain.Run{DryRun: true, ...}`. Settings toggled to false afterward does not retroactively flip the row. | If new column missing → migration didn't run → server fails to start (existing migrate-on-startup pattern). |
| Assemble guard | `Engine.Advance(runID)` called for run with `Stage=StageAssemble`, `DryRun=true` | Returns wrapped `domain.ErrValidation` with message naming the dry-run reason. Run stays at StageAssemble/Waiting. PhaseCRunner is not invoked. | Real (non-dry) runs unaffected. |
| API key missing in dry-run | `cfg.DryRun=true` AND DASHSCOPE_API_KEY empty | `buildPhaseBRunner` returns nil error and a runner wired with fakes; Phase B advances normally. | Without dry-run, the existing `"DASHSCOPE_API_KEY not set"` error still fires. |

</frozen-after-approval>

## Code Map

- `internal/domain/config.go` -- `PipelineConfig` struct gains `DryRun bool` with YAML tag `dry_run`.
- `internal/domain/types.go` -- `domain.Run` struct gains `DryRun bool` with JSON tag `dry_run`.
- `internal/llmclient/dryrun/image.go` -- NEW. `ImageClient` implementing `domain.ImageGenerator` (constructor + Generate + Edit). Reuses temp-then-rename atomic write logic mirroring `dashscope.writeFileAtomic`.
- `internal/llmclient/dryrun/tts.go` -- NEW. `TTSClient` implementing `domain.TTSSynthesizer`. Synthesizes a 44.1 kHz / 16-bit mono PCM WAV with header + silent body sized to `~0.14 sec/codepoint`.
- `internal/llmclient/dryrun/image_test.go` -- NEW. Unit test: PNG decodes to expected dimensions / color, file is atomically present, returns CostUSD=0 / Provider="dryrun".
- `internal/llmclient/dryrun/tts_test.go` -- NEW. Unit test: WAV parses with `os.Open` + `encoding/binary` header check, byte rate matches 176400, response DurationMs matches `len(audioBytes)*1000/176400`.
- `migrations/017_runs_dry_run.sql` -- NEW. `ALTER TABLE runs ADD COLUMN dry_run INTEGER NOT NULL DEFAULT 0;`
- `internal/db/run_store.go` -- Extend the two SELECT column lists (lines 161 and 208) and the INSERT (line 113) to include `dry_run`. Extend `scanRun` (line 912) to scan it. `CreateWithPromptVersion` gains a `dryRun bool` parameter.
- `internal/service/run_service.go` -- `RunService.Create` reads `cfg.DryRun` from `SettingsService.LoadEffectiveRuntimeConfig` (new dependency) and passes it to `CreateWithPromptVersion`. Mirrors how `prompt.ActivePromptVersion()` is read at the same call site.
- `internal/pipeline/resume.go` -- In `Engine.Advance` at the `case run.Stage == domain.StageAssemble:` arm (line 233), insert an early return when `run.DryRun` is true.
- `cmd/pipeline/serve.go` -- `buildPhaseBRunner` (line 76) gains a `dryRun bool` parameter. When true: build `dryrun.ImageClient` / `dryrun.TTSClient` instead, and skip the API-key-required check (line 85). Caller at line 448 passes `files.Config.DryRun`. Wire `RunService` constructor to receive `SettingsService` so it can read effective config at run-create time (or pass a small `DryRunReader` interface).
- `internal/service/settings_types.go` -- `SettingsConfigInput` gains `DryRun bool` with JSON tag `dry_run`.
- `internal/service/settings_service.go` -- `normalizeSettingsConfig`, `applyEditableConfig` propagate `DryRun`. `effectiveSettingsLog` map gets `"config.dry_run"` line for audit parity.
- `internal/config/settings_files.go` -- YAML round-trip (line 110-area write block, line 389-area struct) propagates `dry_run`.
- `internal/db/settings_files.go` (or wherever `domain.SettingsFileSnapshot` JSON-serializes for `settings_versions`) -- ensure no field ordering breaks. (Verify path during impl; expected to be transparent JSON-marshal.)
- `web/src/contracts/settingsContracts.ts` -- `settingsConfigSchema` gains `dry_run: z.boolean()`.
- `web/src/components/shells/SettingsShell.tsx` -- `defaultConfig()` adds `dry_run: false`. New checkbox control beneath the cost-cap grid (or in ProviderConfigPanel). Label: "Dry run (Phase B)" with helper text: "Skip DashScope image and TTS calls. Generates placeholder assets at zero cost. Final video assembly is blocked while enabled."
- `web/src/components/settings/ProviderConfigPanel.tsx` -- Accept the bool field, render as a single checkbox row (separate from the text/number grids). The existing `onChange(field, value)` callback signature widens from `string | number` to `string | number | boolean`.

## Tasks & Acceptance

**Execution (in commit order; each step ends with `go build ./... && go test ./...` green; UI step adds `npm run lint && npm run typecheck && npm test` in `web/`):**

- [x] **C1.** `internal/llmclient/dryrun/image.go`, `tts.go` + sibling tests -- Implement fakes against `domain.ImageGenerator` / `domain.TTSSynthesizer`. Atomic PNG/WAV writes. Tests assert: interface satisfaction, atomic presence, PNG dimensions+color, WAV header validity, DurationMs formula, Provider="dryrun", CostUSD=0.
- [x] **C2.** `internal/domain/config.go` (+ `internal/domain/config_test.go` if present) -- Add `DryRun` field. Confirm `DefaultConfig` leaves it false. Add `internal/config/settings_files.go` YAML round-trip + a round-trip test.
- [x] **C3.** `migrations/017_runs_dry_run.sql` + `internal/db/run_store.go` + `internal/domain/types.go` -- Add column, extend `Run` struct, INSERT, SELECTs, `scanRun`, and `CreateWithPromptVersion(...dryRun bool)` signature. Update existing `run_store` tests so SELECTs still pass (tests are sensitive to column counts).
- [x] **C4.** `internal/service/run_service.go` -- Inject access to effective config (smallest possible interface — `EffectiveDryRun(ctx) (bool, error)` or reuse `SettingsService`). `Create` reads it and snapshots to the new column. Add a unit test: with a stub returning true, the created run has `DryRun=true`.
- [x] **C5.** `internal/pipeline/resume.go` -- Insert dry-run guard at `case run.Stage == domain.StageAssemble:` (before the `e.phaseC == nil` check). New unit test in `engine_test.go` (the existing assemble dispatch test file): when run.DryRun=true, Advance returns ErrValidation and PhaseCRunner is not invoked.
- [x] **C6.** `cmd/pipeline/serve.go` -- `buildPhaseBRunner` gains `dryRun` parameter. Branch the client construction. Skip API-key check in dry-run. Caller passes `files.Config.DryRun`. Wire `RunService` constructor with the new dependency. (No new test — covered by C1+C4 unit tests + manual smoke.)
- [x] **C7.** `internal/service/settings_types.go`, `settings_service.go` -- Round-trip `DryRun`. Update the settings save/load test so a true value survives.
- [x] **C8.** `web/src/contracts/settingsContracts.ts` + `web/src/components/shells/SettingsShell.tsx` + `web/src/components/settings/ProviderConfigPanel.tsx` -- Add field + checkbox UI + onChange handler typing. Update SettingsShell test if it asserts on draft shape.

**Acceptance Criteria (system-level, not duplicating I/O Matrix):**
- Given `dry_run` is enabled and `DASHSCOPE_API_KEY` is unset, when the operator starts a new run and reaches StageImage/StageTTS, then Phase B completes successfully, no outbound HTTPS request is issued (verify with `audit.log` showing Provider="dryrun"), and BatchReview lists every scene with placeholder images and silent audio playable in the existing `<audio>` element.
- Given an existing run was created in normal (non-dry) mode and is mid-flight, when the operator toggles dry_run on in Settings, then that run's `runs.dry_run` row stays 0; only the *next* `RunService.Create` snapshots the new value.
- Given a run with `dry_run=1` has reached StageAssemble/Waiting, when the operator clicks "Generate Video" (POST /api/runs/{id}/advance), then the API returns 400/422 with a message naming the dry-run reason, the run stays at StageAssemble/Waiting, and no `final.mp4` is produced.

## Spec Change Log

### Iteration 1 — Patches applied during step-04 review (no spec amendment)

Three reviewers ran (Blind hunter, Edge case hunter, Acceptance auditor). Acceptance auditor returned `ALL_PASS` against the spec contract. The findings below were classified `patch` — caused by the change, trivially fixable without amending the spec.

- **P1 — Resume bypassed dry-run guard.** Edge F1 / Blind F3: `Engine.ExecuteResume` also dispatches `runPhaseC` for `StageAssemble` rows; the original implementation only guarded `Engine.Advance`. Centralized the guard inside `runPhaseC` so both Advance and Resume are covered with one check. Updated `internal/pipeline/engine_test.go` to assert via tracking stubs that no Phase C dependency is touched.
- **P2 — Phase B executor ignored `run.DryRun`.** Edge F2 / Blind F4: `dynamicPhaseBExecutor.Run` rebuilt the runner from current effective config rather than the row's snapshot. A Settings flip between Create and Phase B execution would have flipped the run's mode mid-flight, breaking the snapshot promise. Fixed in `cmd/pipeline/serve.go`: load the run row, override `cfg.DryRun` with `run.DryRun` before calling `buildPhaseBRunner`.
- **P3 — `EffectiveDryRun` error path.** Edge F6 / Blind F5: `RunService.Create` hard-failed on provider error, contradicting the "production-default safety" comment. Now defaults to `dryRun=false` on error, mirroring `PromptVersionProvider`'s nil-tolerant philosophy.
- **P4 — Image fill optimization.** Edge F8: comment promised `image.Uniform` fill but code was a per-pixel double loop. Replaced with `draw.Draw(img, rect, &image.Uniform{...}, ..., draw.Src)`. At 2688×1536 this drops a multi-second per-shot CPU cost.

**KEEP (must survive any future re-derivation):**
- The runs.dry_run column as immutable per-run snapshot (not a re-read field).
- The dryrun fakes living in `internal/llmclient/dryrun/`, not inside `dashscope/`.
- The Phase B track code (`image_track.go`, `tts_track.go`, `phase_b.go`) remaining untouched.
- No UI badge / banner on Production / BatchReview shells.
- Settings round-trip parity with all other config fields.
- Centralized assemble guard (inside `runPhaseC`) — covers both Advance and Resume from one spot.

### Findings deferred (not this story's problem) — appended to deferred-work.md

- **D1.** Dry-run still routes through DashScope rate limiter (~10 RPM), making big dry runs slow even though no API call happens. (Edge F9.)
- **D2.** CLI Resume reads YAML on disk, not the DB-effective settings. Pre-existing pattern. (Edge F3.)
- **D3.** Production WAV duration constant `176_400` may not match actual qwen3-tts output (44.1 kHz / 16-bit / mono PCM is 88_200 byte rate). Spec required dryrun to mirror the production constant exactly; auditing whether the production constant itself is correct is a separate piece of work. (Edge F4.)
- **D4.** Phase A using `WriterProvider: dashscope` or `CriticProvider: dashscope` plus `DryRun: true` and no `DASHSCOPE_API_KEY` will pass the dry-run-relaxed Phase B check but 401 deep in Phase A. Default settings keep Phase A on DeepSeek/Gemini, so this only bites users who deliberately switched. (Edge F5.)

## Design Notes

**Fake WAV header (44-byte RIFF/PCM):** the canonical layout is `"RIFF" + uint32(36+dataSize) + "WAVE" + "fmt " + uint32(16) + uint16(1) + uint16(numCh) + uint32(sampleRate) + uint32(byteRate) + uint16(blockAlign) + uint16(bitsPerSample) + "data" + uint32(dataSize)`. With numCh=1, sampleRate=44100, bitsPerSample=16: byteRate=88200, blockAlign=2. ffmpeg concat with `-c copy` requires identical codec params across inputs — keeping these constants fixed in the fake is what makes per-chunk synthesis + concat work. `tts.go:194` constant `176_400` is `byteRate * 2` — a stereo assumption baked into the duration math; the fake matches the *production* constant exactly so dry-run and real produce identical DurationMs for the same byte count.

**Why Provider="dryrun" instead of "dashscope":** the audit log already records the provider per call. Setting it to "dryrun" lets `audit.log` greps unambiguously distinguish dry runs from real ones — useful when sharing logs or debugging "wait, did that one really cost $0?"

**Why guard StageAssemble (not BatchReview, not export):** the StageAssemble dispatch in `Engine.Advance` is the one and only place ffmpeg assembly is triggered for a run. Guarding here means: BatchReview UX with placeholders works fully (operator can review/approve/reject placeholder-asset scenes), the operator can click "Generate Video" and get a clear error that names the dry-run reason, and there's no way to produce `final.mp4` from placeholder bytes inside the system. Disk-level access is not gated — the user can still inspect placeholder PNGs / WAVs under `outputDir/{runID}/`, which is what they want for visual prompt iteration.

**Dependency wiring for run-create snapshot:** `RunService` currently takes a `PromptVersionProvider`. The cleanest mirror is to add a tiny reader interface (`DryRunProvider interface { EffectiveDryRun(context.Context) (bool, error) }`) and have `SettingsService` satisfy it via `LoadEffectiveRuntimeConfig`. Avoids a circular import and keeps the run-create code path testable with a stub that returns a fixed bool.

## Verification

**Commands:**
- `go test ./...` -- expected: all packages green including new `internal/llmclient/dryrun/` tests and updated `run_store` / engine / service tests.
- `go build ./...` -- expected: clean compile.
- `cd web && npm run lint && npm run typecheck && npm test` -- expected: all green; SettingsShell test (if asserting shape) updated.
- `gofmt -l . | wc -l` -- expected: 0.

**Manual checks (smoke against a running dev server):**
- Toggle `dry_run` in Settings UI → save → reload page → toggle still on. (Round-trip OK.)
- Start a fresh run with `DASHSCOPE_API_KEY` unset in `.env` → run progresses through Phase A using DeepSeek/Gemini → Phase B emits placeholder PNG (#2a2a2a fill at 2688×1536) and silent WAV at expected paths under `outputDir/{runID}/`. No 401 errors in server log.
- BatchReview UI shows the placeholder image preview and the audio player renders / plays (silently).
- Approve all scenes → click "Generate Video" → receive an error toast / status naming the dry-run reason. No `final.mp4` written.
- Disable dry_run, start a *new* run → real DashScope calls happen, costs accumulate, final video can be assembled.

## Suggested Review Order

**The toggle and its contract**

- The single config field that drives all of dry-run, with the user-facing contract in its comment.
  [`config.go:106`](../../internal/domain/config.go#L106)

- Migration creating the immutable per-run snapshot column — this is the durability story.
  [`017_runs_dry_run.sql:1`](../../migrations/017_runs_dry_run.sql#L1)

- New per-run JSON field on the domain Run; explains why golden fixtures were updated.
  [`types.go:131`](../../internal/domain/types.go#L131)

**Phase B client substitution**

- Build-time branch picks fakes vs. real DashScope clients; skips the API-key hard-fail under DryRun.
  [`serve.go:109`](../../cmd/pipeline/serve.go#L109)

- Phase B executor overrides effective `cfg.DryRun` with `run.DryRun` so the snapshot wins over Settings flips.
  [`serve.go:479`](../../cmd/pipeline/serve.go#L479)

- Fake image client — placeholder PNG via `draw.Draw` Uniform fill + atomic write.
  [`image.go:74`](../../internal/llmclient/dryrun/image.go#L74)

- Fake TTS client — RIFF/PCM WAV with byte rate exactly matching the production duration estimator.
  [`tts.go:56`](../../internal/llmclient/dryrun/tts.go#L56)

**Snapshot at creation, guard at assembly**

- Run creation snapshots effective DryRun; provider error degrades to `false` (real-mode = safe-mode).
  [`run_service.go:140`](../../internal/service/run_service.go#L140)

- Settings → effective-config bridge; tiny Provider interface for run-create snapshotting.
  [`settings_service.go:180`](../../internal/service/settings_service.go#L180)

- Centralized dry-run guard inside `runPhaseC` covers both `Engine.Advance` and `ExecuteResume`.
  [`resume.go:403`](../../internal/pipeline/resume.go#L403)

- INSERT extends with `dry_run`; Get/List SELECTs and `scanRun` round-trip the column.
  [`run_store.go:121`](../../internal/db/run_store.go#L121)

**Settings round-trip and UI toggle**

- YAML on-disk side: ordered config struct gains `dry_run,omitempty`.
  [`settings_files.go:411`](../../internal/config/settings_files.go#L411)

- DB-backed effective config side: `SettingsConfigInput.DryRun` propagates to/from snapshot.
  [`settings_types.go:39`](../../internal/service/settings_types.go#L39)

- TS contract: `dry_run: z.boolean()` on the settings schema.
  [`settingsContracts.ts:21`](../../web/src/contracts/settingsContracts.ts#L21)

- Single checkbox under cost-cap grid with helper text — no Production/BatchReview badge by design.
  [`ProviderConfigPanel.tsx:107`](../../web/src/components/settings/ProviderConfigPanel.tsx#L107)

**Tests (peripheral but load-bearing)**

- WAV header + duration formula guarantee dry-run and production agree on `tts_duration_ms`.
  [`tts_test.go:22`](../../internal/llmclient/dryrun/tts_test.go#L22)

- Snapshot is immutable: Settings toggling after Create does not mutate the run row.
  [`run_service_test.go:340`](../../internal/service/run_service_test.go#L340)

- Phase C never sees the request when `run.DryRun=true`; tracking stubs confirm short-circuit.
  [`engine_test.go:367`](../../internal/pipeline/engine_test.go#L367)
