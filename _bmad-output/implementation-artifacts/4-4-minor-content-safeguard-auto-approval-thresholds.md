# Story 4.4: Minor-Content Safeguard & Auto-Approval Thresholds

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want automated safeguards for sensitive minor-related content and high-confidence auto-approval for safe scenes,
so that the system blocks risky material, records why it stopped, and removes low-value review work from my queue.

## Prerequisites

**Story 3.5 should be implemented before this story is treated as production-complete.** Story 4.4 depends on the final Critic checkpoint semantics introduced there:

- `runs.critic_score` remains the **run-level** normalized final score. Story 4.4 must not reinterpret that field as a scene-level review gate.
- `scenario.json` / final Critic output is the canonical Phase A artifact that later review preparation can inspect.

**Story 4.1 should be implemented before the Golden-fixture part of this story is considered complete.** Story 4.4 extends the Golden eval layout introduced there:

- use `testdata/golden/eval/manifest.json`
- use the same pair directory layout under `testdata/golden/eval/00000x/`
- do **not** create a second ad-hoc minors-fixture folder

**Story 4.3 is not a hard prerequisite, but this story must preserve its calibration boundary.** System decisions and operator decisions must stay distinguishable:

- `system_auto_approved` is a machine decision, not operator approval
- `override` is an operator action and must carry a note
- current kappa / trust metrics must continue to ignore non-operator decision types unless explicitly upgraded later

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, and use inline fakes + `testutil.AssertEqual[T]` / `testutil.AssertJSONEq` (no testify, no gomock). Module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

**Continuity guard before implementation:** this story is about **scene review gating**, not run lifecycle state. Do **not** overload `domain.Status` on `runs`, and do **not** keep stretching `segments.status` to mean both media-progress state and review disposition. Add a dedicated review-state surface for scenes.

1. **AC-DEDICATED-SCENE-REVIEW-STATE:** add first-class scene review gating state instead of overloading the existing run/segment status fields.

   Required domain contract in `internal/domain/review_gate.go` (or an equivalently named file):

   ```go
   package domain

   type ReviewStatus string

   const (
       ReviewStatusPending          ReviewStatus = "pending"
       ReviewStatusWaitingForReview ReviewStatus = "waiting_for_review"
       ReviewStatusAutoApproved     ReviewStatus = "auto_approved"
       ReviewStatusApproved         ReviewStatus = "approved"
       ReviewStatusRejected         ReviewStatus = "rejected"
   )

   const (
       DecisionTypeSystemAutoApproved = "system_auto_approved"
       DecisionTypeOverride           = "override"
       SafeguardFlagMinors            = "Safeguard Triggered: Minors"
   )
   ```

   Required persistence changes:

   - add a new migration `migrations/006_scene_review_gate.sql`
   - extend `segments` with:
     - `review_status TEXT NOT NULL DEFAULT 'pending'`
     - `safeguard_flags TEXT NOT NULL DEFAULT '[]'`
   - add an index:

   ```sql
   CREATE INDEX IF NOT EXISTS idx_segments_run_review_status
       ON segments(run_id, review_status, scene_index);
   ```

   Required Go model/store changes:

   - `internal/domain/types.go` `Episode` gains:
     - `ReviewStatus ReviewStatus 'json:"review_status"'`
     - `SafeguardFlags []string 'json:"safeguard_flags,omitempty"'`
   - `internal/db/segment_store.go` round-trips both fields
   - `segments.status` stays the existing artifact/progress field; Story 4.4 must not repurpose it

   Tests:
   - `TestReviewStatus_IsValidSet`
   - `TestEpisode_JSONRoundTrip_ReviewStatusAndSafeguards`
   - `TestSegmentStore_ListByRunID_DecodesReviewGateFields`
   - `TestMigration006_AddsSceneReviewGateColumns`

2. **AC-AUTO-APPROVAL-THRESHOLD-CONFIG:** add a configuration field for scene-level auto-approval using the repository's existing normalized `0.00..1.00` score scale.

   Required config extension:

   ```go
   type PipelineConfig struct {
       ...
       AutoApprovalThreshold float64 `yaml:"auto_approval_threshold" mapstructure:"auto_approval_threshold"`
   }
   ```

   Rules:
   - `AutoApprovalThreshold` applies to **scene-level** `segments.critic_score`, never to `runs.critic_score`
   - valid range is `(0.0, 1.0)`; values `<= 0` or `>= 1.0` return `domain.ErrValidation`
   - default is `0.85` unless product requirements are updated before implementation
   - `internal/config/loader.go` adds the matching `v.SetDefault`
   - `pipeline init` must emit the new YAML key via `domain.DefaultConfig()`

   Tests:
   - `TestDefaultConfig_AutoApprovalThreshold`
   - `TestConfigLoad_AutoApprovalThresholdFromYAML`
   - `TestConfigLoad_RejectsOutOfRangeAutoApprovalThreshold`

3. **AC-AUTHORABLE-MINORS-REGEX-ASSET:** add a version-controlled, operator-authorable regex artifact for the rule-based minors safeguard half.

   Required artifact:
   - `docs/policy/minor_sensitive_contexts.ko.txt`

   Required loader surface in `internal/pipeline/agents/policy_minors.go` (or equivalent):

   ```go
   type MinorSensitivePatterns struct {
       Version string
       Raw     []string
   }

   type MinorRegexHit struct {
       SceneNum int
       Pattern  string
   }

   func LoadMinorSensitivePatterns(projectRoot string) (*MinorSensitivePatterns, error)
   func (p *MinorSensitivePatterns) MatchNarration(script *domain.NarrationScript) []MinorRegexHit
   ```

   Rules:
   - loading semantics match Story 3.3's forbidden-terms artifact:
     - UTF-8 only
     - one literal-or-regex pattern per line
     - `#` comments allowed
     - blank lines ignored
     - invalid regex → `domain.ErrValidation`
   - `Version` is the SHA-256 hex digest of raw file bytes
   - matching is scene-aware: hits must report the 1-based `scene_num` from the narration script
   - case-insensitive for ASCII, exact for Korean text
   - if Story 3.3 already introduced reusable text-policy parsing helpers, reuse them instead of cloning a second parser

   Tests:
   - `TestMinorSensitivePatterns_LoadAndMatch`
   - `TestMinorSensitivePatterns_InvalidRegexRejected`
   - `TestMinorSensitivePatterns_VersionStable`

4. **AC-CRITIC-MINORS-SUBCHECK-CONTRACT:** extend the final Critic checkpoint contract so the LLM can independently flag policy-sensitive minor scenes.

   Extend the canonical Critic model (do **not** create a second review-only report type):

   ```go
   type MinorPolicyFinding struct {
       SceneNum int    `json:"scene_num"`
       Reason   string `json:"reason"`
   }

   type CriticCheckpointReport struct {
       ...
       MinorPolicyFindings []MinorPolicyFinding `json:"minor_policy_findings,omitempty"`
   }
   ```

   Scope rules:
   - the new field is required for the **post-reviewer** Critic contract used for review preparation
   - Story 4.4 may leave post-writer Critic unchanged if that keeps backward compatibility cleaner
   - every `scene_num` must map to an actual narration scene; out-of-range scene numbers are `domain.ErrValidation`
   - this field is **not** blended into `overall_score`; safeguards override score-based auto-approval rather than diluting the score numerically

   Required prompt / contract updates:
   - update `docs/prompts/scenario/critic_agent.md` so the Critic emits `minor_policy_findings`
   - update `testdata/contracts/critic_post_reviewer.schema.json`
   - update `testdata/contracts/critic_post_reviewer.sample.json`

   Prompt requirements:
   - instruct the Critic to list only scenes that depict minors in violent, sexualized, exploitative, or otherwise policy-sensitive contexts
   - require concise Korean reasons per flagged scene
   - keep JSON-only output, no markdown fences

   Tests:
   - `TestCriticCheckpointReport_JSONRoundTrip_MinorPolicyFindings`
   - `TestCriticPostReviewerSchema_AcceptsMinorPolicyFindings`
   - `TestCriticPostReviewerSchema_RejectsOutOfRangeSceneNum`

5. **AC-PURE-SCENE-GATE-DECISION-LOGIC:** implement the auto-approval/safeguard decision as a pure helper so the core logic is deterministic and independently testable.

   Required surface in `internal/pipeline/review_gate.go` (or equivalent):

   ```go
   type SceneGateInput struct {
       SceneIndex      int
       CriticScore     *float64
       RegexTriggered  bool
       CriticTriggered bool
   }

   type SceneGateResult struct {
       ReviewStatus   domain.ReviewStatus
       SafeguardFlags []string
       AutoApproved   bool
   }

   func DecideSceneGate(input SceneGateInput, threshold float64) (SceneGateResult, error)
   func MergeMinorSignals(regexHits []MinorRegexHit, criticFindings []domain.MinorPolicyFinding) map[int][]string
   ```

   Rules:
   - threshold validation uses the same `(0,1)` rule as config loading
   - any minors signal (`RegexTriggered || CriticTriggered`) wins over score and returns:
     - `review_status = waiting_for_review`
     - `safeguard_flags = ["Safeguard Triggered: Minors"]`
     - `auto_approved = false`
   - when no safeguard triggers and `critic_score > auto_approval_threshold`, return:
     - `review_status = auto_approved`
     - `auto_approved = true`
   - when no safeguard triggers and the score is nil / missing / not above threshold, return:
     - `review_status = waiting_for_review`
   - the comparison is strict `>` to match the sprint prompt wording
   - `MergeMinorSignals` must union regex and Critic findings per scene; duplicate triggers still produce a single stored safeguard flag

   Tests:
   - `TestDecideSceneGate_MinorsOverrideHighScore`
   - `TestDecideSceneGate_AutoApprovesOnlyWhenStrictlyAboveThreshold`
   - `TestDecideSceneGate_MissingScoreFallsBackToWaitingForReview`
   - `TestMergeMinorSignals_UnionByScene`

6. **AC-PERSIST-SYSTEM-AUTO-APPROVALS-IDEMPOTENTLY:** persist auto-approved scenes as explicit system decisions and scene review-state transitions.

   Required store/service behavior:

   - add write support to `internal/db/decision_store.go` (or a dedicated review-gate store) for scene-level decision insertion
   - add write support to `internal/db/segment_store.go` for scene review-state updates
   - batch-review preparation for a run must update segment review state and system decisions in one transaction so the run cannot end up half auto-approved

   Required decision recording for each auto-approved scene:
   - `scene_id = strconv.Itoa(scene_index)`
   - `decision_type = "system_auto_approved"`
   - `note = NULL`
   - `context_snapshot` includes the threshold and the source `critic_score`

   Required idempotency rules:
   - re-running the preparation step for the same run must not append duplicate non-superseded `system_auto_approved` rows
   - if a scene later receives an operator decision, the system decision remains historical context; do not rewrite it into `"approve"`
   - system decisions must **not** set `runs.human_override`

   Tests:
   - `TestPrepareBatchReview_AutoApprovedSceneRecorded`
   - `TestPrepareBatchReview_AutoApprovalIdempotentOnRerun`
   - `TestPrepareBatchReview_SystemDecisionDoesNotFlipHumanOverride`

7. **AC-OVERRIDE-WITH-MANDATORY-NOTE:** when the minors safeguard triggers, the operator can explicitly override it only with a non-empty note, and that override resolves the scene for downstream automation.

   Required service surface in `internal/service/review_gate.go` (or equivalent):

   ```go
   func (s *ReviewGateService) OverrideMinorSafeguard(
       ctx context.Context,
       runID string,
       sceneIndex int,
       note string,
   ) error
   ```

   Behavior:
   - the target scene must currently have:
     - `review_status = waiting_for_review`
     - `safeguard_flags` containing `"Safeguard Triggered: Minors"`
   - `strings.TrimSpace(note)` must be non-empty or return `domain.ErrValidation`
   - on success:
     - insert decision row with `decision_type = "override"`
     - persist the note in `decisions.note`
     - keep the safeguard flag for auditability; do **not** erase history
     - set `review_status = approved`
     - set the run's sticky `human_override` bit
   - if the scene is not safeguarded, return `domain.ErrConflict`
   - this story does **not** add a UI modal; backend contract only

   Tests:
   - `TestOverrideMinorSafeguard_RejectsEmptyNote`
   - `TestOverrideMinorSafeguard_Happy`
   - `TestOverrideMinorSafeguard_RejectsNonSafeguardedScene`
   - `TestOverrideMinorSafeguard_SetsRunHumanOverride`

8. **AC-HITL-SUMMARY-AND-STAGE-ENTRY-INTEGRATION:** the new scene review statuses must integrate cleanly with batch-review preparation and existing HITL status/reporting helpers.

   Required behavior:
   - `internal/pipeline/hitl_session.go` and `internal/service/hitl_service.go` must stop treating every non-approve/non-reject scene as generic pending
   - `waiting_for_review` is the actionable queue state
   - `auto_approved` and `approved` both count as positive resolved states for summary/next-scene purposes
   - `rejected` remains a negative resolved state
   - if batch-review preparation finishes and **every** scene is resolved (`auto_approved`, `approved`, or `rejected`) with no `waiting_for_review` scenes left, downstream automation may continue immediately; do **not** force a useless human pause
   - the pure `NextStage` transition matrix from Story 2.1 stays unchanged; this story owns entry behavior around `batch_review`, not the graph itself

   Minimum compatibility updates:
   - `BuildSessionSnapshot` / `NextSceneIndex` / `SummaryString`
   - `DecisionCountsByRunID` and any summary/count helpers that would otherwise leave auto-approved scenes in the pending bucket

   Tests:
   - `TestBuildSessionSnapshot_TracksAutoApprovedAndWaitingForReview`
   - `TestNextSceneIndex_SkipsAutoApprovedScenes`
   - `TestDecisionCountsByRunID_AutoApprovedNotPending`
   - `TestBatchReviewPreparation_NoManualScenesCanAutoContinue`

9. **AC-GOLDEN-MINORS-KNOWN-FAIL-FIXTURE:** extend the Golden fixture set with at least one known-fail minors case in the same governed layout introduced by Story 4.1.

   Required fixture outcome:
   - add at least one negative fixture whose `category` is exactly `"minors"`
   - pair it with a valid positive fixture via the existing 1:1 manifest layout
   - if the repository already contains the baseline negative categories from Story 4.1, the negative set must now include:
     - `fact_error`
     - `descriptor_violation`
     - `weak_hook`
     - `minors`

   Rules:
   - use `testdata/golden/eval/00000x/negative.json` + manifest entry, not a one-off loose file
   - the minors fixture should describe a scene where the regex artifact and/or Critic sub-check would reasonably trigger
   - this story does **not** weaken the 1:1 pair invariant from Story 4.1

   Tests:
   - `TestGoldenEvalManifest_ContainsMinorsKnownFail`
   - `TestGoldenEvalFixture_MinorsCategoryValidates`

10. **AC-FR-COVERAGE-AND-VALIDATION-COMMANDS:** treat this story as incomplete until traceability and validation move with the code.

   Required `testdata/fr-coverage.json` updates:
   - `FR30` — minors safeguard blocks downstream automation and requires operator review
   - `FR31a` — auto-approval threshold + `system_auto_approved` decision recording
   - `FR36` — override note persistence in the decisions store
   - `NFR-T5` — minors known-fail Golden fixture presence

   Validation commands:
   - `go test ./...`
   - `go build ./...`
   - `go run scripts/lintlayers/main.go`
   - `go run scripts/frcoverage/main.go`
   - if Story 4.1 is already merged: `go test ./internal/critic/eval -run Golden -v`

## Tasks / Subtasks

- [x] **T1: Add scene review-state primitives and migration** (AC: 1)
  - [x] Add `internal/domain/review_gate.go`.
  - [x] Add `migrations/006_scene_review_gate.sql`.
  - [x] Extend `Episode` and `SegmentStore` round-trip behavior.

- [x] **T2: Add threshold config** (AC: 2)
  - [x] Extend `domain.PipelineConfig`, defaults, loader, and config tests.
  - [x] Ensure `pipeline init` emits the new YAML field.

- [x] **T3: Add minors regex artifact + loader** (AC: 3)
  - [x] Add `docs/policy/minor_sensitive_contexts.ko.txt`.
  - [x] Add loader/matcher tests.

- [x] **T4: Extend Critic contract for minors findings** (AC: 4)
  - [x] Update prompt and post-reviewer schema/sample files.
  - [x] Extend the Critic domain model without creating a second report type.

- [x] **T5: Implement pure review-gate logic** (AC: 5)
  - [x] Add `DecideSceneGate`.
  - [x] Add `MergeMinorSignals`.
  - [x] Cover threshold and safeguard-precedence tests.

- [x] **T6: Persist system auto-approvals** (AC: 6)
  - [x] Add scene review-state write methods.
  - [x] Add system decision insertion with idempotency.
  - [x] Keep `runs.human_override` untouched for system decisions.

- [x] **T7: Add operator override flow** (AC: 7)
  - [x] Add `OverrideMinorSafeguard`.
  - [x] Enforce non-empty note.
  - [x] Set `runs.human_override` on successful operator override.

- [x] **T8: Integrate with HITL/batch-review entry** (AC: 8)
  - [x] Update summary/count/snapshot helpers so auto-approved scenes no longer appear pending.
  - [x] Implement skip-when-no-manual-review-needed behavior in the concrete batch-review entry path.

- [x] **T9: Add the minors Golden fixture and traceability updates** (AC: 9, 10)
  - [x] Extend the governed Golden eval fixture set with one `"minors"` negative.
  - [x] Update `testdata/fr-coverage.json`.
  - [x] Run the validation command set.

## Dev Notes

### Scene-Level, Not Run-Level

FR31a is about **review items / scenes**. The repository already has both `runs.critic_score` and `segments.critic_score`. Story 4.4 must use `segments.critic_score` for auto-approval and leave `runs.critic_score` alone as the run-level final metric introduced by the Phase A stories.

### Do Not Reuse `segments.status`

The current repository uses `segments.status` in fixtures for artifact/progress lifecycle concerns (`pending`, `completed`). If Story 4.4 reuses that same field for `auto_approved` and `waiting_for_review`, later Phase B and HITL code will inherit ambiguous semantics. A dedicated `review_status` column is the safer line.

### Why Safeguard Beats Score

The minors safeguard is a policy/risk gate, not a quality score penalty. A scene can be technically strong and still require operator review. That is why the logic is:

- minors triggered → `waiting_for_review`
- otherwise score above threshold → `auto_approved`
- otherwise → `waiting_for_review`

Do not blend safeguard hits into the numeric score and hope threshold math will replicate the product rule.

### System Decisions Must Stay Distinguishable

`system_auto_approved` exists so later trust / override-rate analysis can answer, "what did the machine approve on its own?" If the implementation collapses that into `approve`, Epic 4's metrics become less trustworthy immediately.

### Override Is an Audit Event, Not a Silent Bypass

The operator note is mandatory because the whole point of this safeguard is to leave an audit trail explaining why risky content was allowed through. Keep the safeguard flag on the scene even after override so later review/history surfaces can show that the content was manually released.

### Batch Review Should Not Pause for Zero Work

If every scene ends in `auto_approved` or another resolved state, pausing the run at `batch_review` creates fake manual work and breaks the UX promise behind FR31a. Implement the skip rule in the concrete batch-review entry flow, but do not mutate the pure `NextStage` matrix for it.

### Golden Layout Must Follow Story 4.1 Even If 4.1 Has Not Landed Yet

Current repository state does not yet contain `testdata/golden/eval/`, but Story 4.4 must still target that exact future layout. Do not create a temporary `testdata/golden/minors/` shortcut that would need migration later.

### Current Repo Command Name

Some older planning text still mentions `scripts/check-layer-imports.go`. The repository currently uses:

- `go run scripts/lintlayers/main.go`

Use the real command in tests, docs, and validation notes.

### Existing Code Paths This Story Extends

- `internal/domain/config.go` / `internal/config/loader.go` for the threshold field
- `internal/db/segment_store.go` and `internal/db/decision_store.go` for review-state persistence and audit rows
- `internal/pipeline/hitl_session.go` and `internal/service/hitl_service.go` for summary/count correctness once auto-approved scenes exist
- `internal/pipeline/resume.go` currently contains the `Engine.Advance` stub; whichever concrete batch-review entrypoint lands first must own the skip-or-wait behavior from this story

## References

- [_bmad-output/planning-artifacts/epics.md:1333-1360 — Story 4.4 source acceptance criteria](../planning-artifacts/epics.md#L1333)
- [_bmad-output/planning-artifacts/sprint-prompts.md:610-626 — Story 4.4 sprint prompt and review checklist](../planning-artifacts/sprint-prompts.md#L610)
- [_bmad-output/planning-artifacts/prd.md — FR30 / FR31a / FR36 / NFR-T5](../planning-artifacts/prd.md)
- [_bmad-output/planning-artifacts/ux-design-specification.md:197-204 — auto-approve trust requirements](../planning-artifacts/ux-design-specification.md#L197)
- [_bmad-output/implementation-artifacts/3-3-writer-agent-critic-post-writer-checkpoint.md](3-3-writer-agent-critic-post-writer-checkpoint.md)
- [_bmad-output/implementation-artifacts/3-5-phase-a-completion-post-reviewer-critic.md](3-5-phase-a-completion-post-reviewer-critic.md)
- [_bmad-output/implementation-artifacts/4-1-golden-eval-set-governance-validation.md](4-1-golden-eval-set-governance-validation.md)
- [docs/prompts/scenario/critic_agent.md](../../docs/prompts/scenario/critic_agent.md)
- [internal/domain/config.go](../../internal/domain/config.go)
- [internal/domain/types.go](../../internal/domain/types.go)
- [internal/db/decision_store.go](../../internal/db/decision_store.go)
- [internal/db/segment_store.go](../../internal/db/segment_store.go)
- [internal/pipeline/hitl_session.go](../../internal/pipeline/hitl_session.go)
- [internal/service/hitl_service.go](../../internal/service/hitl_service.go)
- [scripts/lintlayers/main.go](../../scripts/lintlayers/main.go)
- [scripts/frcoverage/main.go](../../scripts/frcoverage/main.go)
- [testdata/fr-coverage.json](../../testdata/fr-coverage.json)

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- `go test ./...`
- `go build ./...`
- `go run scripts/lintlayers/main.go`
- `go run scripts/frcoverage/main.go`
- `go test ./internal/critic/eval -run Golden -v`

### Completion Notes List

- Added dedicated scene review-gate primitives, persisted `review_status` + `safeguard_flags`, and proved the new columns/index through migration and store tests.
- Added `auto_approval_threshold` config wiring end-to-end, including loader validation and `pipeline init` YAML emission.
- Added the authorable minors regex asset plus loader/matcher support, reusing the repo's policy-file parsing behavior and stable SHA-256 versioning.
- Extended the post-reviewer Critic contract with `minor_policy_findings`, updated prompt/schema/sample assets, and validated out-of-range scene references.
- Implemented pure review-gate decision helpers, transactional batch-review preparation, idempotent `system_auto_approved` decision writes, and mandatory-note override handling.
- Updated HITL/session counting behavior so auto-approved scenes resolve cleanly and zero-manual-review runs can continue directly to `assemble`.
- Added a governed Golden minors known-fail pair and FR coverage mappings.
- Migration note: the repo already had `006_critic_calibration_snapshots.sql`, so the scene review-gate schema landed as `migrations/007_scene_review_gate.sql` to preserve upgrade safety.

### File List

- `cmd/pipeline/init_test.go`
- `docs/policy/minor_sensitive_contexts.ko.txt`
- `docs/prompts/scenario/critic_agent.md`
- `internal/config/loader.go`
- `internal/config/loader_test.go`
- `internal/critic/eval/minors_fixture_test.go`
- `internal/db/decision_store.go`
- `internal/db/decision_store_test.go`
- `internal/db/segment_store.go`
- `internal/db/segment_store_test.go`
- `internal/db/sqlite_test.go`
- `internal/domain/config.go`
- `internal/domain/config_test.go`
- `internal/domain/critic.go`
- `internal/domain/critic_test.go`
- `internal/domain/review_gate.go`
- `internal/domain/review_gate_test.go`
- `internal/domain/types.go`
- `internal/domain/types_test.go`
- `internal/pipeline/agents/critic.go`
- `internal/pipeline/agents/critic_contract_test.go`
- `internal/pipeline/agents/policy.go`
- `internal/pipeline/agents/policy_test.go`
- `internal/pipeline/hitl_session.go`
- `internal/pipeline/hitl_session_test.go`
- `internal/pipeline/review_gate.go`
- `internal/pipeline/review_gate_test.go`
- `internal/service/hitl_service_integration_test.go`
- `internal/service/hitl_service_test.go`
- `internal/service/review_gate.go`
- `internal/service/review_gate_test.go`
- `migrations/007_scene_review_gate.sql`
- `testdata/contracts/critic_post_reviewer.sample.json`
- `testdata/contracts/critic_post_reviewer.schema.json`
- `testdata/fixtures/paused_10scenes_4approved.sql`
- `testdata/fixtures/paused_at_batch_review.sql`
- `testdata/fixtures/paused_with_changes.sql`
- `testdata/fr-coverage.json`
- `testdata/golden/eval/000003/negative.json`
- `testdata/golden/eval/000003/positive.json`
- `testdata/golden/eval/manifest.json`

### Change Log

- 2026-04-18 — Implemented Story 4.4 end-to-end: dedicated scene review-gate state, minors safeguard policy asset + Critic contract, deterministic gate logic, transactional batch-review preparation, override audit flow, HITL summary integration, Golden minors fixture coverage, and validation updates.
