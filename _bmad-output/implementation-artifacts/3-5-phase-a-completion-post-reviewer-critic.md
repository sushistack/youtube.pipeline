# Story 3.5: Phase A Completion & Post-Reviewer Critic

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want Phase A to finish with a second Critic checkpoint and a final persisted `scenario.json`,
so that `scenario_review` and downstream phases consume one authoritative, fully-validated scenario artifact containing every Phase A output, both Critic judgments, and a deterministic final quality summary.

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, and use inline fakes + `testutil.AssertEqual[T]` / `testutil.AssertJSONEq` (no testify, no gomock). Module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

**Continuity guard before implementation:** this story MUST extend the single canonical Phase A carrier introduced by Story 3.1 and expanded by Stories 3.3/3.4. Do **not** create a second final-artifact struct that drifts from `agents.PipelineState`. If typed slots from Story 3.4 do not exist yet, add them in the same canonical state carrier. One carrier, one authoritative `scenario.json`.

1. **AC-AUTHORITATIVE-PHASE-A-STATE-CONTRACT:** finalize the canonical Phase A JSON contract in-place and make the `scenario.json` write timing unambiguous.

   Required outcome:
   - `scenario.json` is written **only** when Phase A fully completes, meaning the chain has produced:
     - research output
     - structure output
     - narration output
     - visual breakdown output
     - review output
     - `critic.post_writer`
     - `critic.post_reviewer`
     - final quality summary + schema manifest
   - No earlier checkpoint may emit a partial `scenario.json`. This resolves the tension between Story 3.1's scaffold wording and Story 2.3's artifact-lifecycle rule: the only authoritative Phase A artifact is the final one created at the `critic -> scenario_review` boundary.

   Extend the canonical carrier with two final-artifact sections:

   ```go
   type PhaseAQualitySummary struct {
       PostWriterScore   int    `json:"post_writer_score"`
       PostReviewerScore int    `json:"post_reviewer_score"`
       CumulativeScore   int    `json:"cumulative_score"`
       FinalVerdict      string `json:"final_verdict"`
   }

   type ContractRef struct {
       Path   string `json:"path"`
       SHA256 string `json:"sha256"`
   }

   type PhaseAContractManifest struct {
       ResearchSchema          ContractRef `json:"research_schema"`
       StructureSchema         ContractRef `json:"structure_schema"`
       WriterSchema            ContractRef `json:"writer_schema"`
       VisualBreakdownSchema   ContractRef `json:"visual_breakdown_schema"`
       ReviewSchema            ContractRef `json:"review_schema"`
       CriticPostWriterSchema  ContractRef `json:"critic_post_writer_schema"`
       CriticPostReviewerSchema ContractRef `json:"critic_post_reviewer_schema"`
       PhaseAStateSchema       ContractRef `json:"phase_a_state_schema"`
   }
   ```

   Rules:
   - `ContractRef.Path` is repo-relative (for example `testdata/contracts/critic_post_reviewer.schema.json`), never absolute.
   - `ContractRef.SHA256` is the hex SHA-256 of the raw file bytes.
   - The final artifact lives in the same canonical state carrier; do **not** introduce a shadow `ScenarioDocument` type.
   - Add `testdata/contracts/phase_a_state.schema.json` and `testdata/contracts/phase_a_state.sample.json` as the final Phase A contract deferred by Story 3.1.
   - Add tests that assert:
     - both new sections round-trip through JSON
     - the contract manifest omits no required schema
     - repo-relative paths are used
     - `scenario.json` is absent before finalization

2. **AC-POST-REVIEWER-CRITIC-CONTRACT:** extend the Critic domain model for FR13's second checkpoint without breaking Story 3.3's first checkpoint contract.

   Required additions in `internal/domain/critic.go`:

   ```go
   const CriticCheckpointPostReviewer = "post_reviewer"
   ```

   Rules:
   - `CriticOutput.PostWriter` remains the first checkpoint from Story 3.3.
   - `CriticOutput.PostReviewer` is now populated by this story and serialized in `scenario.json`.
   - The second checkpoint reuses the same `CriticCheckpointReport` shape; do **not** create a parallel `FinalCriticReport` type.
   - Add:
     - `testdata/contracts/critic_post_reviewer.schema.json`
     - `testdata/contracts/critic_post_reviewer.sample.json`
   - Schema requirements:
     - Draft-07
     - `checkpoint` const `"post_reviewer"`
     - `verdict` enum `pass | retry | accept_with_notes`
     - rubric fields integer `0..100`
     - `overall_score` integer `0..100`
     - `feedback` Korean text
     - `additionalProperties: false` everywhere

   Tests:
   - `TestCriticOutput_JSONRoundTrip_BothCheckpoints`
   - `TestCriticOutput_JSONOmitEmptyPostReviewer`
   - `TestCriticCheckpointPostReviewer_Constant`

3. **AC-POST-REVIEWER-PRECHECK-AND-AGENT:** implement the second Critic invocation as a dedicated Phase A agent over the full scenario state.

   Add `internal/pipeline/agents/critic_post_reviewer.go` (or a coherent split if `critic.go` stays under the file-length cap):

   ```go
   func NewPostReviewerCritic(
       gen domain.TextGenerator,
       cfg TextAgentConfig,
       prompts PromptAssets,
       writerValidator *Validator,
       visualValidator *Validator,
       reviewValidator *Validator,
       criticValidator *Validator,
       terms *ForbiddenTerms,
       writerProvider string,
   ) AgentFunc
   ```

   And a dedicated precheck helper:

   ```go
   func runPostReviewerPrecheck(
       state *PipelineState,
       writerValidator *Validator,
       visualValidator *Validator,
       reviewValidator *Validator,
       terms *ForbiddenTerms,
   ) (domain.CriticPrecheck, *domain.CriticCheckpointReport, error)
   ```

   Behavior:
   - Required state:
     - `state.Narration` non-nil
     - `state.VisualBreakdown` non-nil
     - `state.Review` non-nil
     - `state.Critic.PostWriter` already populated
   - Reuse `pipeline.ValidateDistinctProviders(writerProvider, cfg.Provider)` before any LLM call.
   - Precheck obligations before the second Critic LLM call:
     - re-validate narration against the writer schema
     - validate visual breakdown against the visual schema
     - validate review output against the review schema
     - rerun forbidden-term matching against the narration text
   - If the review report indicates the scenario is not fact-safe for completion (`overall_pass == false` or Story 3.4's equivalent typed field), do **not** call the LLM. Instead synthesize:
     - `checkpoint = "post_reviewer"`
     - `verdict = "retry"`
     - `retry_reason = "review_failed"`
     - `overall_score = 0`
     - zero rubric scores
     - Korean `feedback`
     - `precheck.short_circuited = true`
   - If schema validation or forbidden-term matching fails, also short-circuit with:
     - `retry_reason = "schema_validation_failed"` or `"forbidden_terms_detected"`
   - Otherwise render the final prompt from the existing authorable assets:
     - `docs/prompts/scenario/critic_agent.md`
     - `docs/prompts/scenario/format_guide.md`
     - serialized whole-scenario JSON payload (narration + visual breakdown + review)
   - Parse JSON response, validate against `critic_post_reviewer.schema.json`, and if `verdict == retry && retry_reason == ""`, fill via the existing `DeriveRetryReason`.
   - Persist the result into `state.Critic.PostReviewer` without disturbing `state.Critic.PostWriter`.
   - This story MUST NOT add a second critic prompt file unless the existing `critic_agent.md` proves structurally unusable. Prefer prompt reuse.

   Tests:
   - `TestPostReviewerCritic_Run_Happy`
   - `TestPostReviewerCritic_Run_PrecheckReviewFailureShortCircuits`
   - `TestPostReviewerCritic_Run_PrecheckSchemaFailureShortCircuits`
   - `TestPostReviewerCritic_Run_PrecheckForbiddenTermsShortCircuits`
   - `TestPostReviewerCritic_Run_SameProviderBlocked`
   - `TestPostReviewerCritic_Run_FillsRetryReasonFromRubric`
   - `TestPostReviewerCritic_Run_PreservesPostWriter`

4. **AC-CUMULATIVE-QUALITY-SUMMARY:** generate a deterministic cross-checkpoint quality summary without overloading the semantics of `runs.critic_score`.

   Add a small helper in `internal/pipeline/quality.go` (or equivalent):

   ```go
   func ComputePhaseAQuality(
       postWriter *domain.CriticCheckpointReport,
       postReviewer *domain.CriticCheckpointReport,
   ) (PhaseAQualitySummary, error)

   func NormalizeCriticScore(overallScore int) float64
   ```

   Rules:
   - `ComputePhaseAQuality` requires both checkpoints to be non-nil.
   - Score formula is deterministic:
     - `cumulative_score = round(0.40*post_writer.overall_score + 0.60*post_reviewer.overall_score)`
   - `final_verdict = post_reviewer.verdict`
   - `post_writer_score` and `post_reviewer_score` store the raw `0..100` values.
   - `NormalizeCriticScore` converts `0..100` into the `0.00..1.00` scale already used by `runs.critic_score` and metrics thresholds.
   - `runs.critic_score` stores the **final post-reviewer score**, not the cumulative score. The cumulative score belongs in `scenario.json` only.

   Tests:
   - `TestComputePhaseAQuality_WeightedAverage`
   - `TestComputePhaseAQuality_RoundsHalfUp`
   - `TestComputePhaseAQuality_RejectsNilCheckpoint`
   - `TestNormalizeCriticScore`

5. **AC-FINAL-SCENARIO-ARTIFACT-WRITE:** write the final `scenario.json` exactly once, atomically, and only on successful Phase A completion.

   Implementation may live in `internal/pipeline/phase_a.go`, `internal/pipeline/advance.go`, or a small finalizer helper, but the contract is fixed:
   - `scenario.json` write is **not** an agent responsibility.
   - `scenario.json` is written only when:
     - both Critic checkpoints exist
     - final post-reviewer verdict is `pass` or `accept_with_notes`
     - the contract manifest has been filled
     - the quality summary has been computed
   - The file contents are the final canonical Phase A carrier, serialized with 2-space indentation.
   - The write uses the same atomic temp-file → `Sync` → rename pattern already established for authoritative artifacts.
   - The final JSON must contain:
     - every agent output field
     - both Critic reports
     - the final quality summary
     - the contract manifest
   - If the final verdict is `retry`, no `scenario.json` is written.

   Tests:
   - `TestFinalizePhaseA_WritesScenarioJSON_Happy`
   - `TestFinalizePhaseA_NoScenarioJSONOnRetry`
   - `TestFinalizePhaseA_ContractManifestStable`
   - `TestFinalizePhaseA_JSONContainsBothCriticReports`

6. **AC-ENGINE-ADVANCE-AND-RUN-PERSISTENCE:** wire Phase A completion into the engine and run store so the authoritative artifact, run stage, and final critic score move together.

   This story picks up the `Engine.Advance` work explicitly deferred by Story 3.1.

   Required persistence surface:
   - add one atomic run-store method rather than scattering multiple partial updates:

   ```go
   type PhaseAAdvanceResult struct {
       Stage        domain.Stage
       Status       domain.Status
       RetryReason  *string
       CriticScore  *float64
       ScenarioPath *string
   }

   func (s *RunStore) ApplyPhaseAResult(ctx context.Context, runID string, res PhaseAAdvanceResult) error
   ```

   Rules:
   - One SQL `UPDATE` sets `stage`, `status`, `retry_reason`, `critic_score`, and `scenario_path`.
   - `Engine.Advance` supports Phase A entry from `pending` through `critic`.
   - Because Story 2.3 explicitly says Phase A is in-memory until completion, `Advance` is allowed to rerun the full Phase A chain from scratch for any pre-`scenario_review` Phase A entry. Do **not** add partial pre-`scenario_review` disk persistence just to fake exact stage-local resume.
   - Happy path:
     - execute the full Phase A chain
     - compute final quality summary
     - write `scenario.json`
     - set run to:
       - `stage = scenario_review`
       - `status = waiting`
       - `scenario_path = "scenario.json"`
       - `critic_score = NormalizeCriticScore(postReviewer.OverallScore)`
       - `retry_reason = nil`
   - Final business-retry path:
     - do **not** write `scenario.json`
     - set run to:
       - `stage = write`
       - `status = failed`
       - `retry_reason = postReviewer.RetryReason`
       - `critic_score = NormalizeCriticScore(postReviewer.OverallScore)` when a real LLM verdict exists, otherwise `nil`
     - return an error wrapping `domain.ErrStageFailed` so callers can distinguish business retry from infrastructure breakage

   Tests:
   - `TestEngineAdvance_PhaseAHappyPath_MovesToScenarioReview`
   - `TestEngineAdvance_PhaseARetry_MovesBackToWriteWithoutScenarioJSON`
   - `TestRunStore_ApplyPhaseAResult_RoundTrip`

7. **AC-INTEGRATION-AND-FR-COVERAGE:** Phase A completion is only done when the second checkpoint, final artifact, and run-state persistence are proven together.

   Required integration coverage:
   - `TestPhaseAIntegration_TwoCriticCheckpointsAndScenarioJSONIntegrity`
     - mocked providers
     - both Critic checkpoints execute on the happy path
     - final `scenario.json` exists
     - JSON unmarshals into the final carrier
     - both Critic reports are present
     - contract manifest entries point to checked-in schema files
   - `TestPhaseAIntegration_FinalRetryLeavesNoScenarioJSON`
   - `TestPhaseAIntegration_PostReviewerReviewFailureShortCircuitsSecondLLMCall`

   Required FR mapping updates in `testdata/fr-coverage.json`:
   - `FR10` — complete Phase A scenario output now includes final visual breakdown + review + persisted scenario artifact
   - `FR11` — final Phase A contract/schema validation at handoff boundary
   - `FR13` — two Critic checkpoints
   - `FR24` — final verdict + rubric-based score persistence
   - `FR25` — second-checkpoint precheck short-circuit before LLM invocation

   Validation commands:
   - `go test ./...`
   - `go build ./...`
   - `go run scripts/lintlayers/main.go`
   - `go run scripts/frcoverage/main.go`

## Tasks / Subtasks

- [x] **T1: Finalize the canonical Phase A artifact shape** (AC: 1)
  - [x] Extend the existing Phase A carrier with `quality` and `contracts`.
  - [x] Add `phase_a_state.schema.json` and `phase_a_state.sample.json`.
  - [x] Add JSON round-trip tests for the final contract sections.

- [x] **T2: Add the post-reviewer Critic contract** (AC: 2)
  - [x] Extend `internal/domain/critic.go` with `CriticCheckpointPostReviewer`.
  - [x] Add `critic_post_reviewer.schema.json` and `critic_post_reviewer.sample.json`.
  - [x] Cover both-checkpoint JSON serialization.

- [x] **T3: Implement the post-reviewer Critic agent** (AC: 3)
  - [x] Add the final precheck helper.
  - [x] Reuse provider inequality guard and prompt assets.
  - [x] Persist `state.Critic.PostReviewer` without disturbing `PostWriter`.

- [x] **T4: Add deterministic quality-summary helpers** (AC: 4)
  - [x] Implement `ComputePhaseAQuality`.
  - [x] Implement `NormalizeCriticScore`.
  - [x] Add weighting and rounding tests.

- [x] **T5: Write the authoritative final artifact** (AC: 5)
  - [x] Implement final `scenario.json` write behind the Phase A completion boundary.
  - [x] Fill the contract manifest from checked-in schema files.
  - [x] Prove no file is written on retry.

- [x] **T6: Wire Engine.Advance + RunStore finalization** (AC: 6)
  - [x] Add `ApplyPhaseAResult` to the run store.
  - [x] Implement Phase A happy-path transition to `scenario_review`.
  - [x] Implement business-retry transition back to `write`.

- [x] **T7: Integration and traceability** (AC: 7)
  - [x] Add the Phase A integration tests covering two Critic checkpoints and final artifact integrity.
  - [x] Update `testdata/fr-coverage.json`.
  - [x] Run `go test ./...`, `go build ./...`, `go run scripts/lintlayers/main.go`, and `go run scripts/frcoverage/main.go`.

### Review Findings

- [x] [Review][Patch] **[BLOCKING] Post-writer Critic not wired into PhaseARunner chain** — Runner has 6 slots; `NewPostWriterCritic` factory exists but no slot calls it. Integration test pre-seeds `PostWriter` in reviewer spy rather than invoking real agent. Chain must be expanded to 7 slots with `StagePostWriterCritic` between Writer and VisualBreakdowner. [internal/pipeline/phase_a.go:195-205, internal/pipeline/phase_a_integration_test.go:247-255]
- [x] [Review][Patch] **[MAJOR] Post-reviewer short-circuit report omits CriticModel/CriticProvider and skips schema validation** — `buildPostReviewerShortCircuitReport` sets no model/provider (schema minLength:1 violated); `critic.go:169-173` assigns short-circuit report without calling `criticValidator.Validate` unlike post-writer path. [internal/pipeline/agents/critic_precheck.go:159-171, internal/pipeline/agents/critic.go:169-173]
- [x] [Review][Patch] **[MAJOR] Post-reviewer uses `CriticSourceVersionV1 = "v1-critic-post-writer"` — schema embeds wrong label** — Both checkpoints use identical source_version string "v1-critic-post-writer"; `critic_post_reviewer.schema.json` const is "v1-critic-post-writer". Need `CriticSourceVersionPostReviewerV1 = "v1-critic-post-reviewer"`. [internal/domain/critic.go, testdata/contracts/critic_post_reviewer.schema.json]
- [x] [Review][Patch] **[MEDIUM] `NormalizeCriticScore` missing [0,1] clamp** — Returns >1.0 when score >100, <0 when negative. [internal/pipeline/quality.go:31-33]
- [x] [Review][Patch] **[MEDIUM] `FinalVerdict` copied without enum validation** — `ComputePhaseAQuality` copies `postReviewer.Verdict` verbatim; no guard against unknown string values. [internal/pipeline/quality.go:27]
- [x] [Review][Patch] **[MINOR] Non-retry verdict carries stale `RetryReason`** — `critic.go:91-93, 197-199` fills `RetryReason` only on retry but never clears it if LLM returned a non-empty `retry_reason` with a pass/accept verdict. [internal/pipeline/agents/critic.go:91-93,197-199]
- [x] [Review][Patch] **[MINOR] Missing test: scenario.json absent before finalization** — AC-1 requires asserting scenario.json does not exist mid-run. [internal/pipeline/finalize_phase_a_test.go]
- [x] [Review][Defer] `scoreRubric` overwrites explicit 0 `OverallScore` [internal/pipeline/agents/critic.go:88,194] — deferred, design-ambiguous: 0 vs "unset" indistinguishable without schema change
- [x] [Review][Defer] `findProjectRoot` uses `os.Getwd()` [internal/pipeline/finalize_phase_a.go:~2903] — deferred, works in normal CLI invocation from project root; hardening deferred to NFR sprint
- [x] [Review][Defer] `os.Rename` atomicity on Windows / cross-filesystem EXDEV [internal/pipeline/finalize_phase_a.go:2847] — deferred, Linux-primary project
- [x] [Review][Defer] `CriticRubricWeights` exported mutable map global data-race [internal/pipeline/agents/critic.go] — deferred, pre-existing; no concurrent test currently exercises it

## Dev Notes

### Authoritative Write Timing

Story 3.1's scaffold language mentioned `scenario.json` persistence at the end of the chain, while Story 2.3 later clarified the artifact-lifecycle rule: Phase A has **no** on-disk artifact until it completes. This story resolves that ambiguity. The authoritative rule is:

- post-writer Critic result: in-memory only
- visual breakdown + review: in-memory only
- post-reviewer Critic success: now the chain may emit the single authoritative `scenario.json`

Do not leave a preliminary scenario artifact behind after the first checkpoint.

### Final Score Semantics

The codebase already treats `runs.critic_score` as a normalized `0.00..1.00` value used by metrics thresholds. Preserve that meaning. The cumulative score requested by this story is **not** the same field:

- `runs.critic_score` = normalized post-reviewer final score
- `scenario.json.quality.cumulative_score` = weighted `0..100` cross-checkpoint summary

Do not store the cumulative score into the DB column.

### Phase A Resume Reality

Story 2.3 explicitly establishes that pre-`scenario_review` Phase A work is in-memory only and that `scenario.json` is absent until Phase A completes. That means a resumed pre-Phase-A run cannot rely on partial disk artifacts. The acceptable V1 behavior is to rerun the full Phase A chain from scratch for any entry stage before `scenario_review`. Do **not** add hidden partial persistence just to preserve an illusion of exact stage-local replay.

### Prompt Reuse First

`docs/prompts/scenario/critic_agent.md` already accepts a `{scenario_json}` payload and is naturally aligned with the second checkpoint. Reuse it unless there is a proven structural blocker. A second prompt file would create unnecessary divergence between the two Critic checkpoints.

### Contract Manifest Purpose

The user requirement says final `scenario.json` must include the schemas. Interpret that as **schema references + digests**, not embedded raw schema documents. Embedding the whole schema bodies would bloat the artifact, duplicate version-controlled files, and make diffs noisy. Repo-relative paths + SHA-256 gives auditability without duplication.

### Story Dependency Awareness

- Story 3.3 owns Writer output, post-writer Critic output, forbidden-term loading, and provider guard.
- Story 3.4 owns typed visual-breakdown/review outputs and shot-override persistence.
- This story must reuse those contracts if they already exist; if not, it may add only the minimum missing typed fields needed to complete the final artifact, and those fields must live in the same canonical carrier.

### Anti-Patterns To Avoid

1. Do not write a partial `scenario.json` after the first Critic checkpoint.
2. Do not add a second final-artifact struct that mirrors `PipelineState`.
3. Do not store absolute filesystem paths in the contract manifest.
4. Do not overload `runs.critic_score` with the cumulative score.
5. Do not introduce a brand-new Critic prompt file when `critic_agent.md` already fits the second-checkpoint shape.
6. Do not add pre-`scenario_review` disk persistence for partial Phase A state.

### Previous Story Intelligence

- Story 3.1 explicitly deferred `Engine.Advance` wiring, final `phase_a_state` contract files, and any inverse mapping needed for recorder/finalization. This story picks up those deferred seams.
- Story 2.3 made the authoritative artifact timing rule explicit: pre-`scenario_review` `scenario.json` is an inconsistency, not a convenience cache.
- Story 3.3 future-proofed `CriticOutput` for `PostReviewer`; this story must populate that slot rather than replacing the Critic model.
- The current codebase already has a doctor-level Writer/Critic provider inequality check in [internal/config/doctor.go](../../internal/config/doctor.go); runtime defense-in-depth stays in place.

### References

- [_bmad-output/planning-artifacts/sprint-prompts.md](../planning-artifacts/sprint-prompts.md) — Story 3.5 sprint prompt
- [_bmad-output/planning-artifacts/epics.md](../planning-artifacts/epics.md) — Epic 3 / Stories 3.1–3.5 context
- [_bmad-output/planning-artifacts/architecture.md](../planning-artifacts/architecture.md) — Phase A in-memory flow and artifact boundary
- [_bmad-output/planning-artifacts/prd.md](../planning-artifacts/prd.md) — FR10, FR11, FR13, FR24, FR25, NFR-M2
- [_bmad-output/implementation-artifacts/3-1-agent-function-chain-pipeline-runner.md](3-1-agent-function-chain-pipeline-runner.md)
- [_bmad-output/implementation-artifacts/3-3-writer-agent-critic-post-writer-checkpoint.md](3-3-writer-agent-critic-post-writer-checkpoint.md)
- [_bmad-output/implementation-artifacts/2-3-stage-level-resume-artifact-lifecycle.md](2-3-stage-level-resume-artifact-lifecycle.md)
- [docs/prompts/scenario/critic_agent.md](../../docs/prompts/scenario/critic_agent.md)
- [docs/prompts/scenario/format_guide.md](../../docs/prompts/scenario/format_guide.md)
- [docs/prompts/scenario/03_5_visual_breakdown.md](../../docs/prompts/scenario/03_5_visual_breakdown.md)
- [docs/prompts/scenario/04_review.md](../../docs/prompts/scenario/04_review.md)
- [internal/pipeline/resume.go](../../internal/pipeline/resume.go)
- [internal/pipeline/observability.go](../../internal/pipeline/observability.go)
- [internal/db/run_store.go](../../internal/db/run_store.go)
- [internal/domain/types.go](../../internal/domain/types.go)
- [testdata/fr-coverage.json](../../testdata/fr-coverage.json)

## Dev Agent Record

### Agent Model Used

gpt-5.4

### Debug Log References

- `go test ./...`
- `go build ./...`
- `go run scripts/lintlayers/main.go`
- `go run scripts/frcoverage/main.go`

### Completion Notes List

- Added final `PipelineState` contract sections for `quality` and `contracts`, plus checked-in `phase_a_state` and `critic_post_reviewer` schema/sample artifacts.
- Implemented the post-reviewer Critic agent and precheck flow, including deterministic short-circuit retry reports for review failure, schema failure, and forbidden terms.
- Added deterministic Phase A quality helpers and a finalizer that computes quality, fills the schema manifest, and writes `scenario.json` only after true Phase A completion.
- Wired `Engine.Advance` and `RunStore.ApplyPhaseAResult` so Phase A success moves runs to `scenario_review` and business retries move runs back to `write` with the correct normalized critic score semantics.
- Added unit/integration coverage for both Critic checkpoints, final artifact integrity, run-store persistence, and FR traceability; full regression/build/layer/FR validation commands passed.

### File List

- `internal/pipeline/agents/agent.go`
- `internal/pipeline/agents/state_contract.go`
- `internal/pipeline/agents/critic.go`
- `internal/pipeline/agents/critic_precheck.go`
- `internal/pipeline/agents/critic_test.go`
- `internal/pipeline/agents/validator_test.go`
- `internal/pipeline/agents/reviewer_test.go`
- `internal/pipeline/agents/structurer_test.go`
- `internal/pipeline/agents/visual_breakdowner_test.go`
- `internal/domain/critic.go`
- `internal/domain/critic_test.go`
- `internal/domain/types.go`
- `internal/pipeline/phase_a.go`
- `internal/pipeline/phase_a_test.go`
- `internal/pipeline/phase_a_integration_test.go`
- `internal/pipeline/finalize_phase_a.go`
- `internal/pipeline/finalize_phase_a_test.go`
- `internal/pipeline/quality.go`
- `internal/pipeline/quality_test.go`
- `internal/pipeline/resume.go`
- `internal/pipeline/resume_test.go`
- `internal/pipeline/engine_test.go`
- `internal/db/run_store.go`
- `internal/db/run_store_test.go`
- `testdata/contracts/critic_post_reviewer.schema.json`
- `testdata/contracts/critic_post_reviewer.sample.json`
- `testdata/contracts/phase_a_state.schema.json`
- `testdata/contracts/phase_a_state.sample.json`
- `testdata/fr-coverage.json`

## Change Log

- 2026-04-18: Implemented Story 3.5 Phase A completion, post-reviewer Critic, final `scenario.json` finalization, and Engine/RunStore persistence wiring.
