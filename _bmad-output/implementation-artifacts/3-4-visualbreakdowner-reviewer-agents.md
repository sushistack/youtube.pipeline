# Story 3.4: VisualBreakdowner & Reviewer Agents

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a system,
I want to turn Korean narration into per-scene shot breakdowns and a fact/consistency review report,
so that Phase A produces schema-validated visual planning output that Phase B can render and the operator can confidently inspect at `scenario_review`.

## Prerequisites

**Stories 3.1, 3.2, and 3.3 must be implemented before this story can compile cleanly.** This story extends those contracts in place:

- Story 3.1 owns the canonical `internal/pipeline/agents` scaffold (`AgentFunc`, `PipelineState`, `PhaseARunner`, `NoopAgent`).
- Story 3.2 owns `domain.ResearcherOutput` / `domain.StructurerOutput` and the JSON Schema validator pattern.
- Story 3.3 owns `domain.NarrationScript`, `TextAgentConfig`, `PromptAssets`, the strict JSON decode helper, and the first LLM-backed Phase A agent pattern.

This story MUST promote the existing `PipelineState` fields in place. Do **not** introduce a second Phase A carrier, a parallel review-state struct, or a second prompt-loading mechanism.

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, and use inline fakes + `testutil.AssertEqual[T]` / `testutil.AssertJSONEq` (no testify, no gomock). Module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

1. **AC-DOMAIN-VISUAL-AND-REVIEW-TYPES:** add two new domain files, keeping the `domain/` 300-line cap intact.

   `internal/domain/visual_breakdown.go`:

   ```go
   package domain

   const (
       VisualBreakdownSourceVersionV1 = "v1-visual-breakdown"
       ShotFormulaVersionV1           = "tts-duration-v1"

       TransitionKenBurns      = "ken_burns"
       TransitionCrossDissolve = "cross_dissolve"
       TransitionHardCut       = "hard_cut"
   )

   type VisualBreakdownOutput struct {
       SCPID            string                  `json:"scp_id"`
       Title            string                  `json:"title"`
       FrozenDescriptor string                  `json:"frozen_descriptor"`
       Scenes           []VisualBreakdownScene  `json:"scenes"`
       ShotOverrides    map[int]ShotOverride    `json:"shot_overrides"`
       Metadata         VisualBreakdownMetadata `json:"metadata"`
       SourceVersion    string                  `json:"source_version"`
   }

   type VisualBreakdownScene struct {
       SceneNum              int          `json:"scene_num"`
       ActID                 string       `json:"act_id"`
       Narration             string       `json:"narration"`
       EstimatedTTSDurationS float64      `json:"estimated_tts_duration_s"`
       ShotCount             int          `json:"shot_count"`
       Shots                 []VisualShot `json:"shots"`
   }

   type VisualShot struct {
       ShotIndex          int     `json:"shot_index"`
       VisualDescriptor   string  `json:"visual_descriptor"`
       EstimatedDurationS float64 `json:"estimated_duration_s"`
       Transition         string  `json:"transition"`
   }

   type ShotOverride struct {
       ShotCount  *int    `json:"shot_count,omitempty"`
       Transition *string `json:"transition,omitempty"`
   }

   type VisualBreakdownMetadata struct {
       VisualBreakdownModel    string `json:"visual_breakdown_model"`
       VisualBreakdownProvider string `json:"visual_breakdown_provider"`
       PromptTemplate          string `json:"prompt_template"`
       ShotFormulaVersion      string `json:"shot_formula_version"`
   }
   ```

   `internal/domain/review.go`:

   ```go
   package domain

   const (
       ReviewSourceVersionV1 = "v1-reviewer-fact-check"

       ReviewIssueFactError           = "fact_error"
       ReviewIssueMissingFact         = "missing_fact"
       ReviewIssueDescriptorViolation = "descriptor_violation"
       ReviewIssueInventedContent     = "invented_content"
       ReviewIssueConsistencyIssue    = "consistency_issue"
   )

   type ReviewReport struct {
       OverallPass      bool               `json:"overall_pass"`
       CoveragePct      float64            `json:"coverage_pct"`
       Issues           []ReviewIssue      `json:"issues"`
       Corrections      []ReviewCorrection `json:"corrections"`
       ReviewerModel    string             `json:"reviewer_model"`
       ReviewerProvider string             `json:"reviewer_provider"`
       SourceVersion    string             `json:"source_version"`
   }

   type ReviewIssue struct {
       SceneNum    int    `json:"scene_num"`
       Type        string `json:"type"`
       Severity    string `json:"severity"`
       Description string `json:"description"`
       Correction  string `json:"correction"`
   }

   type ReviewCorrection struct {
       SceneNum  int    `json:"scene_num"`
       Field     string `json:"field"`
       Original  string `json:"original"`
       Corrected string `json:"corrected"`
   }
   ```

   Rules:
   - `ShotOverrides` is part of the persisted Phase A contract in this story. On successful first-pass generation it MUST be initialized to an empty map so `scenario.json` contains `"shot_overrides": {}` rather than omitting the key.
   - `map[int]ShotOverride` is acceptable in Go even though JSON keys serialize as strings; this still satisfies the contract shape `shot_overrides[scene_index]`.
   - `VisualShot.VisualDescriptor` is the Phase B handoff artifact. It MUST already contain the Frozen Descriptor verbatim as a prefix; Phase B must not reconstruct it later.
   - Add `internal/domain/visual_breakdown_test.go` and `internal/domain/review_test.go` covering JSON round-trip, snake_case tags, allowed transition constants, and the invariant that `shot_overrides` serializes as an object key named `shot_overrides`.

2. **AC-PROMOTE-PIPELINESTATE-FIELDS:** modify `internal/pipeline/agents/agent.go` to promote the remaining Phase A placeholders from `json.RawMessage` to typed pointers:

   ```go
   // BEFORE
   VisualBreakdown json.RawMessage `json:"visual_breakdown,omitempty"`
   Review          json.RawMessage `json:"review,omitempty"`

   // AFTER
   VisualBreakdown *domain.VisualBreakdownOutput `json:"visual_breakdown,omitempty"`
   Review          *domain.ReviewReport          `json:"review,omitempty"`
   ```

   Update Story 3.1's `agent_test.go` accordingly:
   - zero-valued `PipelineState` still marshals to exactly `{"run_id":"","scp_id":"","started_at":"","finished_at":""}`
   - a populated state round-trips with typed `VisualBreakdown` and `Review`
   - add `TestPipelineState_VisualBreakdownReviewTyped`

   Do **not** touch `state.Critic` here; Story 3.5 still owns the second checkpoint wiring.

3. **AC-PROMPT-ASSETS-AND-PROMPT-FIXES:** extend the prompt-loader surface introduced by Story 3.3 and fix the conflicting visual-breakdown prompt contract.

   `internal/pipeline/agents/assets.go` updates:

   ```go
   type PromptAssets struct {
       WriterTemplate          string
       CriticTemplate          string
       VisualBreakdownTemplate string
       ReviewerTemplate        string
       FormatGuide             string
   }
   ```

   `LoadPromptAssets` must additionally read:
   - `docs/prompts/scenario/03_5_visual_breakdown.md`
   - `docs/prompts/scenario/04_review.md`

   Required prompt artifact changes:
   - `docs/prompts/scenario/03_5_visual_breakdown.md`
     - replace the current 1:1 sentence-to-image authority with PRD authority: **the code-computed shot count is final**
     - add placeholders `{frozen_descriptor}`, `{estimated_tts_duration_s}`, and `{shot_count}`
     - explicitly say: "Produce exactly `{shot_count}` shots"
     - keep output JSON-only, no markdown fences
   - `docs/prompts/scenario/04_review.md`
     - request the same JSON shape as `domain.ReviewReport`
     - focus on factual accuracy, Frozen Descriptor consistency, shot-count/transition sanity, and invented-content detection
     - remove storytelling-score/rubric concerns from this reviewer prompt; those belong to the Critic stories

   Tests:
   - extend `assets_test.go` so the loader proves the two new templates are present
   - add a regression assertion that `03_5_visual_breakdown.md` no longer instructs "1:1 sentence-to-image mapping" or "Total shot count == {sentence_count}"

4. **AC-SHOT-COUNT-DURATION-AND-FROZEN-DESCRIPTOR-HELPERS:** add pure helper logic in `internal/pipeline/agents/visual_breakdown_helpers.go` (or a similarly named file).

   Required surface:

   ```go
   type SceneDurationEstimator interface {
       Estimate(scene domain.NarrationScene) float64
   }

   func NewHeuristicDurationEstimator() SceneDurationEstimator
   func ShotCountForDuration(seconds float64) int
   func NormalizeShotDurations(totalSeconds float64, shotCount int) []float64
   func BuildFrozenDescriptor(v domain.VisualIdentity) string
   func EnsureFrozenPrefix(frozen, descriptor string) string
   ```

   Rules:
   - `ShotCountForDuration` implements the exact PRD formula:
     - `<= 8` → `1`
     - `> 8 && <= 15` → `2`
     - `> 15 && <= 25` → `3`
     - `> 25 && <= 40` → `4`
     - `> 40` → `5`
   - `NormalizeShotDurations` is code-owned authority for `estimated_duration_s`; the model does **not** get final say on the numeric split.
     - split the scene duration across `shotCount` shots
     - round to 0.1s precision
     - preserve the total exactly within `±0.1s`
     - last shot absorbs the remainder so the sum stays stable
   - `BuildFrozenDescriptor` is the deterministic bridge from Story 3.2's structured `VisualIdentity` to the single dense text prefix required by FR10/FR14. Use a stable, non-LLM format:
     - `"Appearance: ...; Distinguishing features: ...; Environment: ...; Key visual moments: ..."`
   - `EnsureFrozenPrefix` prepends `frozen + "; "` when absent, but MUST NOT duplicate the prefix if the descriptor already starts with it verbatim.
   - `NewHeuristicDurationEstimator` may use a simple deterministic word-count heuristic; its exact formula is less important than stability. Agent tests must inject a fake estimator so shot-count tests are never hostage to heuristic tweaks.

   Tests:
   - `TestShotCountForDuration_Boundaries`
   - `TestNormalizeShotDurations_SumsToSceneDuration`
   - `TestBuildFrozenDescriptor_Stable`
   - `TestEnsureFrozenPrefix_AddsExactlyOnce`

5. **AC-VISUALBREAKDOWNER-AGENT:** `internal/pipeline/agents/visual_breakdowner.go` (prefer this clear filename over the architecture draft's stale `visual_breaker.go`) implements the LLM-backed VisualBreakdowner.

   ```go
   func NewVisualBreakdowner(
       gen domain.TextGenerator,
       cfg TextAgentConfig,
       prompts PromptAssets,
       validator *Validator,
       estimator SceneDurationEstimator,
   ) AgentFunc
   ```

   Behavior:
   - Validate:
     - `state` non-nil
     - `state.Research` non-nil
     - `state.Narration` non-nil
     - `gen`, `validator`, `estimator` non-nil
     - `cfg.Model` / `cfg.Provider` non-empty
   - For each `state.Narration.Scenes[i]`:
     - estimate `sceneDuration := estimator.Estimate(scene)`
     - compute `shotCount := ShotCountForDuration(sceneDuration)`
     - build `frozen := BuildFrozenDescriptor(state.Research.VisualIdentity)` once and reuse it for every scene
     - render the scene prompt with:
       - scene context from `NarrationScene`
       - serialized `state.Research.VisualIdentity`
       - `{frozen_descriptor}`
       - `{estimated_tts_duration_s}`
       - `{shot_count}`
     - call `gen.Generate(...)` exactly **once per scene**
   - Parse each model response as JSON only using Story 3.3's strict JSON decode helper.
   - Required response shape from the model:

     ```json
     {
       "scene_num": 1,
       "shots": [
         {
           "visual_descriptor": "...",
           "transition": "ken_burns"
         }
       ]
     }
     ```

     The model does **not** own `estimated_duration_s`; Go fills those via `NormalizeShotDurations`.
   - Reject the scene when:
     - returned `scene_num` does not match the requested scene
     - number of shots is not exactly `shotCount`
     - any `visual_descriptor` is empty after trim
     - any transition is outside `ken_burns|cross_dissolve|hard_cut`
   - On success:
     - prefix every shot descriptor with `EnsureFrozenPrefix(frozen, modelDescriptor)`
     - assign `shot_index` sequentially from 1
     - assign normalized `estimated_duration_s`
     - set `state.VisualBreakdown = &domain.VisualBreakdownOutput{...}`
     - initialize `ShotOverrides` to an empty map
     - fill `Metadata` from config and prompt version fields
     - set `SourceVersion = domain.VisualBreakdownSourceVersionV1`
     - validate the assembled top-level output against `visual_breakdown.schema.json` before mutating the state pointer

   Tests in `visual_breakdowner_test.go`:
   - `TestVisualBreakdowner_Run_Happy`
   - `TestVisualBreakdowner_Run_CallsGeneratorPerScene`
   - `TestVisualBreakdowner_Run_UsesShotCountFormula`
   - `TestVisualBreakdowner_Run_PrefixesFrozenDescriptor`
   - `TestVisualBreakdowner_Run_RejectsWrongShotCount`
   - `TestVisualBreakdowner_Run_RejectsInvalidTransition`
   - `TestVisualBreakdowner_Run_InitializesEmptyShotOverrides`
   - `TestVisualBreakdowner_Run_DoesNotMutateStateOnFailure`

6. **AC-REVIEWER-AGENT:** `internal/pipeline/agents/reviewer.go` implements the fact/consistency reviewer.

   ```go
   func NewReviewer(
       gen domain.TextGenerator,
       cfg TextAgentConfig,
       prompts PromptAssets,
       visualValidator *Validator,
       reviewValidator *Validator,
   ) AgentFunc
   ```

   Behavior:
   - Validate:
     - `state.Research` non-nil
     - `state.Narration` non-nil
     - `state.VisualBreakdown` non-nil
     - `gen`, both validators non-nil
     - `cfg.Model` / `cfg.Provider` non-empty
   - Re-validate `state.VisualBreakdown` with `visual_breakdown.schema.json` before any LLM call. This is deliberate defense-in-depth for FR11.
   - Render the reviewer prompt with:
     - source facts from `state.Research`
     - serialized narration script
     - serialized visual breakdown
     - `state.VisualBreakdown.FrozenDescriptor`
     - `prompts.FormatGuide`
   - Call `gen.Generate(...)` exactly once.
   - Parse JSON only, validate against `reviewer_report.schema.json`, and then fill:
     - `ReviewerModel`
     - `ReviewerProvider`
     - `SourceVersion = domain.ReviewSourceVersionV1`
   - `OverallPass` normalization rule:
     - if any issue has `severity == "critical"`, final stored `OverallPass` MUST be `false` even if the model returned `true`
   - `state.Review` is updated only after successful schema validation; on failure the prior state must remain untouched.

   Tests in `reviewer_test.go`:
   - `TestReviewer_Run_Happy`
   - `TestReviewer_Run_InvalidVisualBreakdownBlockedBeforeLLM`
   - `TestReviewer_Run_InvalidJSON`
   - `TestReviewer_Run_SchemaViolation`
   - `TestReviewer_Run_CriticalIssueForcesFail`
   - `TestReviewer_Run_DoesNotMutateStateOnFailure`

7. **AC-HANDOFF-VALIDATION-BEFORE-CRITIC:** the observable FR11 contract at the VisualBreakdowner → Reviewer → Critic boundary must be proven in runner-level integration, even if the implementation keeps validation inside the concrete agents.

   Required test:
   - `TestPhaseARunner_ReviewOutputValidatedBeforeCritic`

   Behavior under test:
   - wire a real or fake Reviewer that returns invalid review output
   - assert the chain fails before the Critic stage runs
   - assert the returned error wraps `domain.ErrValidation`
   - assert no `scenario.json` artifact is written on this failure path

   This story does **not** need a general-purpose validator registry in `PhaseARunner`; a targeted guard is enough. The load-bearing requirement is that by the time the runner would hand off to Critic, Reviewer output has already been schema-validated.

8. **AC-CONTRACTS-SAMPLES-AND-FR-COVERAGE:** add four new contract files under `testdata/contracts/`.

   Required files:
   - `visual_breakdown.schema.json`
   - `visual_breakdown.sample.json`
   - `reviewer_report.schema.json`
   - `reviewer_report.sample.json`

   Visual breakdown schema requirements:
   - Draft-07
   - top-level required: `scp_id`, `title`, `frozen_descriptor`, `scenes`, `shot_overrides`, `metadata`, `source_version`
   - `scenes`: `minItems: 8`, `maxItems: 12` (matches Writer scene count)
   - each scene requires `scene_num`, `act_id`, `narration`, `estimated_tts_duration_s`, `shot_count`, `shots`
   - `shot_count`: integer `minimum: 1`, `maximum: 5`
   - each shot requires `shot_index`, `visual_descriptor`, `estimated_duration_s`, `transition`
   - `transition`: enum `ken_burns`, `cross_dissolve`, `hard_cut`
   - `shot_overrides` is an object whose additional properties follow the `ShotOverride` shape
   - `metadata.shot_formula_version`: const `"tts-duration-v1"`
   - `source_version`: const `"v1-visual-breakdown"`
   - `additionalProperties: false` everywhere

   Reviewer schema requirements:
   - Draft-07
   - required: `overall_pass`, `coverage_pct`, `issues`, `corrections`, `reviewer_model`, `reviewer_provider`, `source_version`
   - issue `type`: enum `fact_error`, `missing_fact`, `descriptor_violation`, `invented_content`, `consistency_issue`
   - issue `severity`: enum `critical`, `warning`, `info`
   - correction `field`: enum `narration`, `visual_descriptor`
   - `source_version`: const `"v1-reviewer-fact-check"`
   - `additionalProperties: false` everywhere

   Sample fixtures:
   - use Story 3.2's `SCP-TEST` and Story 3.3's writer sample as canonical inputs
   - are produced via fakes, never real APIs
   - are checked in manually; no auto-regeneration path

   Required FR coverage updates in `testdata/fr-coverage.json`:
   - `FR10` — per-scene shot breakdown with required fields + overrides contract
   - `FR11` — schema validation for VisualBreakdowner output and Reviewer output before Critic handoff

   Minimum tests to cite:
   - `TestShotCountForDuration_Boundaries`
   - `TestVisualBreakdowner_Run_Happy`
   - `TestVisualBreakdowner_Run_PrefixesFrozenDescriptor`
   - `TestReviewer_Run_Happy`
   - `TestReviewer_Run_InvalidVisualBreakdownBlockedBeforeLLM`
   - `TestPhaseARunner_ReviewOutputValidatedBeforeCritic`

   Validation commands:
   - `go test ./...`
   - `go build ./...`
   - `go run scripts/lintlayers/main.go`

## Tasks / Subtasks

- [x] **T1: Domain contracts + state promotion** (AC: 1, 2)
  - [x] Add `internal/domain/visual_breakdown.go` and `internal/domain/review.go`.
  - [x] Promote `PipelineState.VisualBreakdown` and `PipelineState.Review` in place.
  - [x] Add JSON round-trip tests for the new domain types and typed state slots.

- [x] **T2: Prompt asset extension + prompt authority fix** (AC: 3)
  - [x] Extend `PromptAssets` and `LoadPromptAssets`.
  - [x] Update [docs/prompts/scenario/03_5_visual_breakdown.md](../../docs/prompts/scenario/03_5_visual_breakdown.md).
  - [x] Update [docs/prompts/scenario/04_review.md](../../docs/prompts/scenario/04_review.md).
  - [x] Add regression tests so the stale 1:1 sentence rule cannot creep back in.

- [x] **T3: Shot-count / duration / frozen-descriptor helpers** (AC: 4)
  - [x] Add `SceneDurationEstimator` + deterministic default implementation.
  - [x] Add PRD-exact shot-count formula helper.
  - [x] Add duration normalization and Frozen Descriptor prefix helpers.

- [x] **T4: VisualBreakdowner agent** (AC: 5)
  - [x] Add `internal/pipeline/agents/visual_breakdowner.go`.
  - [x] Call the text generator once per narration scene.
  - [x] Normalize durations in code and prefix Frozen Descriptor verbatim.
  - [x] Persist empty `shot_overrides` in the success path.

- [x] **T5: Reviewer agent** (AC: 6)
  - [x] Add `internal/pipeline/agents/reviewer.go`.
  - [x] Re-validate visual breakdown input before reviewer LLM invocation.
  - [x] Normalize `overall_pass` when critical issues exist.

- [x] **T6: Handoff validation guard** (AC: 7)
  - [x] Add a runner-level integration test proving invalid Reviewer output blocks Critic.
  - [x] Ensure this failure path leaves no `scenario.json`.

- [x] **T7: Contracts, samples, and FR mapping** (AC: 8)
  - [x] Add the four new schema/sample files under `testdata/contracts/`.
  - [x] Update `testdata/fr-coverage.json` for FR10 and FR11.
  - [x] Run `go test ./...`, `go build ./...`, and `go run scripts/lintlayers/main.go`.

## Dev Notes

### Shot Count Authority

The current artifacts disagree:

- PRD / epics: TTS-estimate formula decides shot count
- `docs/prompts/scenario/03_5_visual_breakdown.md`: currently says sentence-count authority
- `docs/prompts/image/01_shot_breakdown.md`: later Phase B cut decomposition

For this story, the authority is unambiguous: **PRD shot-count formula wins**. Do not reuse `docs/prompts/image/01_shot_breakdown.md` in Phase A and do not let sentence count determine shot count.

### Frozen Descriptor Bridge

Story 3.2 produced a structured `VisualIdentity`, not the single dense paragraph described in the PRD. This story must bridge that gap deterministically so later stories can rely on a verbatim reusable prefix without waiting for a future image-reference enrichment workflow.

### Override Contract Scope

This story introduces the persisted `shot_overrides` contract only. The operator-facing editing UI lives later in Epic 7, and Phase B override consumption lives later in Epic 5. V1 for Story 3.4 means:

- initial Phase A output writes `"shot_overrides": {}`
- the schema and Go types are stable
- later stories can safely fill and consume the map without changing `scenario.json`

### Previous Story Intelligence

- Story 3.2 already established the validator pattern and `SCP-TEST` corpus/sample fixture approach. Reuse it.
- Story 3.3 already established `TextAgentConfig`, strict JSON decoding, and prompt assets. Extend those pieces instead of inventing new helpers.
- Story 3.1 explicitly deferred inter-agent validation. This story adds the first load-bearing proof that invalid review output never reaches Critic.

### Anti-Patterns To Avoid

1. Do not let sentence count or prompt output decide shot count.
2. Do not trust model-provided shot durations as authoritative.
3. Do not omit `shot_overrides` from the serialized output.
4. Do not reconstruct Frozen Descriptor later in Phase B; it must already live inside every shot descriptor.
5. Do not overlap Reviewer responsibilities with Critic rubric/scoring logic.
6. Do not create a second prompt loader or a second JSON fence stripper.
7. Do not name the agent file `visual_breaker.go`; use `visual_breakdowner.go` to match the stage and tests.

### References

- [_bmad-output/planning-artifacts/epics.md:1207-1233 — Story 3.4 acceptance criteria](../planning-artifacts/epics.md#L1207)
- [_bmad-output/planning-artifacts/sprint-prompts.md:481-500 — Story 3.4 sprint prompt and review checklist](../planning-artifacts/sprint-prompts.md#L481)
- [_bmad-output/planning-artifacts/epics.md:402-421 — Epic 3 scope](../planning-artifacts/epics.md#L402)
- [_bmad-output/planning-artifacts/prd.md:1257-1257 — FR10](../planning-artifacts/prd.md#L1257)
- [_bmad-output/planning-artifacts/prd.md:1258-1258 — FR11](../planning-artifacts/prd.md#L1258)
- [_bmad-output/planning-artifacts/architecture.md:685-690 — Agent chain and schema validation](../planning-artifacts/architecture.md#L685)
- [_bmad-output/planning-artifacts/architecture.md:787-797 — `scenario.json` as Phase A artifact](../planning-artifacts/architecture.md#L787)
- [_bmad-output/planning-artifacts/implementation-readiness-report-2026-04-16.md:859-878 — shot-count authority conflict and recommended resolution](../planning-artifacts/implementation-readiness-report-2026-04-16.md#L859)
- [docs/prompts/scenario/03_5_visual_breakdown.md](../../docs/prompts/scenario/03_5_visual_breakdown.md)
- [docs/prompts/scenario/04_review.md](../../docs/prompts/scenario/04_review.md)
- [docs/prompts/image/01_shot_breakdown.md](../../docs/prompts/image/01_shot_breakdown.md) — later Phase B reference only, not Story 3.4 authority
- [_bmad-output/implementation-artifacts/3-1-agent-function-chain-pipeline-runner.md](3-1-agent-function-chain-pipeline-runner.md)
- [_bmad-output/implementation-artifacts/3-2-researcher-structurer-agents.md](3-2-researcher-structurer-agents.md)
- [_bmad-output/implementation-artifacts/3-3-writer-agent-critic-post-writer-checkpoint.md](3-3-writer-agent-critic-post-writer-checkpoint.md)

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- `go test ./...`
- `go build ./...`
- `go run scripts/lintlayers/main.go`

### Completion Notes List

- Added typed `domain.VisualBreakdownOutput` and `domain.ReviewReport` contracts, plus typed `PipelineState` promotion and round-trip coverage.
- Extended prompt asset loading, rewrote the visual breakdown and reviewer prompts to enforce code-owned shot-count authority, and added regression checks for the stale sentence-count instructions.
- Implemented deterministic duration, shot-count, Frozen Descriptor, `VisualBreakdowner`, and `Reviewer` logic with failure-safe state mutation behavior.
- Added runner-level validation coverage proving invalid review output never reaches Critic and does not write `scenario.json`.
- Added `visual_breakdown` and `reviewer_report` schemas/samples plus FR10/FR11 coverage updates.

### File List

- `_bmad-output/implementation-artifacts/sprint-status.yaml`
- `docs/prompts/scenario/03_5_visual_breakdown.md`
- `docs/prompts/scenario/04_review.md`
- `internal/domain/review.go`
- `internal/domain/review_test.go`
- `internal/domain/visual_breakdown.go`
- `internal/domain/visual_breakdown_test.go`
- `internal/pipeline/agents/agent.go`
- `internal/pipeline/agents/agent_test.go`
- `internal/pipeline/agents/assets.go`
- `internal/pipeline/agents/assets_test.go`
- `internal/pipeline/agents/reviewer.go`
- `internal/pipeline/agents/reviewer_test.go`
- `internal/pipeline/agents/validator_test.go`
- `internal/pipeline/agents/visual_breakdown_helpers.go`
- `internal/pipeline/agents/visual_breakdown_helpers_test.go`
- `internal/pipeline/agents/visual_breakdowner.go`
- `internal/pipeline/agents/visual_breakdowner_test.go`
- `internal/pipeline/agents/writer_test.go`
- `internal/pipeline/phase_a_integration_test.go`
- `internal/pipeline/phase_a_test.go`
- `testdata/contracts/reviewer_report.sample.json`
- `testdata/contracts/reviewer_report.schema.json`
- `testdata/contracts/visual_breakdown.sample.json`
- `testdata/contracts/visual_breakdown.schema.json`
- `testdata/fr-coverage.json`

## Change Log

- 2026-04-18: Implemented Story 3.4 visual breakdown and reviewer agents, added typed contracts and validators, updated prompt assets/prompts, and added contract plus runner validation coverage.

### Review Findings

Sources: Blind Hunter (adversarial), Edge Case Hunter (boundary), Acceptance Auditor (spec-vs-code). Triage: 21 patch, 1 resolved-decision, 6 defer, 2 dismiss.

**Resolved Decision:**
- [x] [Review][Decision] OverallPass override audit trail — Resolution: append synthetic audit `ReviewIssue` (type=`consistency_issue`, severity=`info`) whenever `hasCriticalIssue` forces `overall_pass=false`, giving HITL operators provenance without changing the schema.

**Patches (applied in batch):**
- [x] [Review][Patch] `ShotCountForDuration` returns 5 for NaN/+Inf, 1 for -Inf/negative — silently fabricates shots on corrupt input [internal/pipeline/agents/visual_breakdown_helpers.go:33-46]
- [x] [Review][Patch] `NormalizeShotDurations` propagates NaN/Inf to `EstimatedDurationS` (breaks JSON marshal) and accepts negative `totalSeconds` [internal/pipeline/agents/visual_breakdown_helpers.go:48-67]
- [x] [Review][Patch] `NormalizeShotDurations` dead double-correction arithmetic [internal/pipeline/agents/visual_breakdown_helpers.go:63-65]
- [x] [Review][Patch] `EnsureFrozenPrefix` returns empty string for empty-frozen+empty-descriptor; does not trim whitespace on frozen side [internal/pipeline/agents/visual_breakdown_helpers.go:79-88]
- [x] [Review][Patch] VisualBreakdowner metadata last-scene-wins (overwritten inside scene loop) [internal/pipeline/agents/visual_breakdowner.go:99-104]
- [x] [Review][Patch] VisualBreakdowner accepts empty `state.Narration.Scenes` silently (surfaces as confusing schema error) [internal/pipeline/agents/visual_breakdowner.go:70]
- [x] [Review][Patch] VisualBreakdowner does not detect duplicate `scene_num` in narration input [internal/pipeline/agents/visual_breakdowner.go:70-104]
- [x] [Review][Patch] Reviewer passes dead `{glossary_section}` placeholder never referenced in `04_review.md` [internal/pipeline/agents/reviewer.go:96]
- [x] [Review][Patch] Reviewer does not normalize nil `Issues`/`Corrections` to `[]` before validation [internal/pipeline/agents/reviewer.go:58-67]
- [x] [Review][Patch] `TestPhaseARunner_ReviewOutputValidatedBeforeCritic` fake-returns the error verbatim; does not exercise real `reviewValidator.Validate` path [internal/pipeline/phase_a_integration_test.go:199-256]
- [x] [Review][Patch] `VisualBreakdowner` unit tests use a stubbed local schema (stripped of `minItems`, `shot_count` bounds, transition enum, const `source_version`) instead of the real `visual_breakdown.schema.json` [internal/pipeline/agents/visual_breakdowner_test.go:211-241]
- [x] [Review][Patch] `TestLoadPromptAssets_Happy` regression check uses case-sensitive `"1:1 sentence-to-image mapping"` — stale prompt used capitalized `"1:1 Sentence-to-Image Mapping"` so the guard could never fire [internal/pipeline/agents/assets_test.go:22]
- [x] [Review][Patch] `TestVisualBreakdownOutput_TransitionConstantsAllowed` is tautological (`got` and `want` are literal copies of the same three constants) [internal/domain/visual_breakdown_test.go:37-51]
- [x] [Review][Patch] `TestNormalizeShotDurations_SumsToSceneDuration` tolerance of 0.1 is as wide as the rounding step and misses small-value drift [internal/pipeline/agents/visual_breakdown_helpers_test.go:36]
- [x] [Review][Patch] `DoesNotMutateStateOnFailure` tests compare `state.Foo != orig` where `orig == nil` — collapse to "still nil" check that cannot detect mutation of an existing object [internal/pipeline/agents/visual_breakdowner_test.go:139-147, reviewer_test.go:85-98, writer_test.go]
- [x] [Review][Patch] `TestVisualBreakdowner_Run_PrefixesFrozenDescriptor` uses a 1×1 fixture and only asserts `Shots[0]`; does not prove "every shot" invariant [internal/pipeline/agents/visual_breakdowner_test.go:76-90]
- [x] [Review][Patch] `TestVisualBreakdowner_Run_UsesShotCountFormula` exercises only one boundary tier (20→3); no table-driven coverage through the agent [internal/pipeline/agents/visual_breakdowner_test.go:62-74]
- [x] [Review][Patch] `ShotFormulaVersion` invariant not asserted in any agent happy-path test [internal/pipeline/agents/visual_breakdowner_test.go:27-45]
- [x] [Review][Patch] `visual_breakdown.schema.json` lacks `minimum: 0` on `estimated_duration_s` / `estimated_tts_duration_s` — negative durations pass [testdata/contracts/visual_breakdown.schema.json]
- [x] [Review][Patch] `phase_a_integration_test.go` fake reviewer mutates `state.Review` before returning — models a shape spec §6 explicitly forbids [internal/pipeline/phase_a_integration_test.go:205-210]
- [x] [Review][Patch] Remove stale `{glossary_section}` key substitution in reviewer prompt rendering (duplicate of the prior item, also applies to prompt asset verification)

**Deferred (filed to `deferred-work.md`):**
- [x] [Review][Defer] Blind/Edge: prompt placeholder silent fall-through (broader project pattern in Writer too; requires load-time template validator across all agents) — defer
- [x] [Review][Defer] Blind: `fr-coverage.json` FR12 annotation ("before the Writer stage runs") is narrower than actual guard scope (runs before every stage) — pre-existing Story 3.3 annotation, low-value rewrite
- [x] [Review][Defer] Blind: `writeScenario` does not fsync parent directory after atomic rename — pre-existing Story 3.3, already filed
- [x] [Review][Defer] Blind: `ScenarioPath` accepts empty `outputDir`/`runID` producing collision-prone paths — pre-existing Story 3.3 helper
- [x] [Review][Defer] Edge: `ShotCountForDuration` has no upper cap on per-scene duration (200s narration → 5 × 40s shots); downstream Phase B may assume ≤12s shots — intentional permissive design for V1, revisit with Phase B
- [x] [Review][Defer] Edge: `PipelineState` round-trip non-identical when `ShotOverrides` nil vs empty map — agent always initializes to `{}`; risk only in manual-edit scenario.json path, revisit with Epic 7 editor

**Dismissed:**
- Edge: Reviewer pre-LLM re-validation redundancy — spec §6 explicitly mandates this as "deliberate defense-in-depth for FR11".
- Blind: Reviewer overwrites LLM-supplied provenance fields — spec §6 explicitly lists `ReviewerModel`/`ReviewerProvider`/`SourceVersion` as runner-filled, not LLM-authoritative.
