# Story 3.3: Writer Agent & Critic (Post-Writer Checkpoint)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the system to generate Korean narration and run an immediate post-Writer Critic checkpoint,
so that Phase A catches weak hooks, factual drift, forbidden-term violations, and provider misconfiguration before the scenario continues downstream.

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, and use inline fakes + `testutil.AssertEqual[T]` / `testutil.AssertJSONEq` (no testify, no gomock). Module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

**Continuity guard before implementation:** this story MUST extend the single canonical Phase A state carrier from Stories 3.1 and 3.2. Do **not** create a second `PipelineState` type. If Story 3.1 lands first with `json.RawMessage` placeholders, upgrade the existing Writer/Critic slots in place; if Story 3.2 lands first with typed fields, add Writer/Critic fields to that same struct. One carrier only.

1. **AC-DOMAIN-WRITER-AND-CRITIC-TYPES:** add two new domain files, keeping the `domain/` 300-line cap intact.

   `internal/domain/narration.go`:

   ```go
   package domain

   const (
       NarrationSourceVersionV1 = "v1-llm-writer"
       LanguageKorean           = "ko"
   )

   type NarrationScript struct {
       SCPID         string             `json:"scp_id"`
       Title         string             `json:"title"`
       Scenes        []NarrationScene   `json:"scenes"`
       Metadata      NarrationMetadata  `json:"metadata"`
       SourceVersion string             `json:"source_version"`
   }

   type NarrationScene struct {
       SceneNum          int        `json:"scene_num"`
       ActID             string     `json:"act_id"`
       Narration         string     `json:"narration"`
       FactTags          []FactTag  `json:"fact_tags"`
       Mood              string     `json:"mood"`
       EntityVisible     bool       `json:"entity_visible"`
       Location          string     `json:"location"`
       CharactersPresent []string   `json:"characters_present"`
       ColorPalette      string     `json:"color_palette"`
       Atmosphere        string     `json:"atmosphere"`
   }

   type FactTag struct {
       Key     string `json:"key"`
       Content string `json:"content"`
   }

   type NarrationMetadata struct {
       Language              string `json:"language"`
       SceneCount            int    `json:"scene_count"`
       WriterModel           string `json:"writer_model"`
       WriterProvider        string `json:"writer_provider"`
       PromptTemplate        string `json:"prompt_template"`
       FormatGuideTemplate   string `json:"format_guide_template"`
       ForbiddenTermsVersion string `json:"forbidden_terms_version"`
   }
   ```

   `internal/domain/critic.go`:

   ```go
   package domain

   const (
       CriticSourceVersionV1 = "v1-critic-post-writer"

       CriticVerdictPass            = "pass"
       CriticVerdictRetry           = "retry"
       CriticVerdictAcceptWithNotes = "accept_with_notes"

       CriticCheckpointPostWriter = "post_writer"
   )

   var CriticRubricWeights = map[string]float64{
       "hook":                0.25,
       "fact_accuracy":       0.25,
       "emotional_variation": 0.25,
       "immersion":           0.25,
   }

   type CriticOutput struct {
       PostWriter   *CriticCheckpointReport `json:"post_writer,omitempty"`
       PostReviewer *CriticCheckpointReport `json:"post_reviewer,omitempty"`
   }

   type CriticCheckpointReport struct {
       Checkpoint    string             `json:"checkpoint"`
       Verdict       string             `json:"verdict"`
       RetryReason   string             `json:"retry_reason,omitempty"`
       OverallScore  int                `json:"overall_score"`
       Rubric        CriticRubricScores `json:"rubric"`
       Feedback      string             `json:"feedback"`
       SceneNotes    []CriticSceneNote  `json:"scene_notes"`
       Precheck      CriticPrecheck     `json:"precheck"`
       CriticModel   string             `json:"critic_model"`
       CriticProvider string            `json:"critic_provider"`
       SourceVersion string             `json:"source_version"`
   }

   type CriticRubricScores struct {
       Hook               int `json:"hook"`
       FactAccuracy       int `json:"fact_accuracy"`
       EmotionalVariation int `json:"emotional_variation"`
       Immersion          int `json:"immersion"`
   }

   type CriticSceneNote struct {
       SceneNum   int    `json:"scene_num"`
       Issue      string `json:"issue"`
       Suggestion string `json:"suggestion"`
   }

   type CriticPrecheck struct {
       SchemaValid        bool     `json:"schema_valid"`
       ForbiddenTermHits  []string `json:"forbidden_term_hits"`
       ShortCircuited     bool     `json:"short_circuited"`
   }
   ```

   Rules:
   - `CriticOutput` is future-proofed for Story 3.5's second checkpoint. This story populates **only** `PostWriter`; `PostReviewer` stays `nil`.
   - `retry_reason` is a machine-readable string for downstream persistence into `runs.retry_reason`; do not derive it by parsing Korean feedback later.
   - Add `internal/domain/narration_test.go` and `internal/domain/critic_test.go` covering JSON round-trip, snake_case tags, equal-weight sum `== 1.0`, and the invariant that `PostReviewer` remains omitted from JSON when nil.

2. **AC-AUTHORABLE-ASSETS-AND-LOADERS:** Writer/Critic prompts and forbidden terms remain version-controlled artifacts, but loading them must happen outside the agent execution path so the `AgentFunc` call itself stays pure except for injected LLM capability.

   Required artifacts:
   - `docs/prompts/scenario/03_writing.md` updated to include a `{forbidden_terms_section}` placeholder and an explicit "JSON only, no markdown fences" reminder.
   - `docs/prompts/scenario/critic_agent.md` updated to request the **same JSON shape** as `CriticCheckpointReport` minus `precheck` metadata, including rubric sub-scores and `retry_reason`.
   - `docs/policy/forbidden_terms.ko.txt` (NEW) — UTF-8, one literal-or-regex pattern per line, `#` comments allowed, blank lines ignored.

   Loader surface in `internal/pipeline/agents/assets.go` and `policy.go`:

   ```go
   type PromptAssets struct {
       WriterTemplate string
       CriticTemplate string
       FormatGuide    string
   }

   func LoadPromptAssets(projectRoot string) (PromptAssets, error)

   type ForbiddenTerms struct {
       Version string
       Raw     []string
   }

   type ForbiddenTermHit struct {
       SceneNum int
       Pattern  string
   }

   func LoadForbiddenTerms(projectRoot string) (*ForbiddenTerms, error)
   func (f *ForbiddenTerms) MatchNarration(script *domain.NarrationScript) []ForbiddenTermHit
   ```

   Requirements:
   - `Version` is the stable SHA-256 hex digest of the raw file contents; store that in `NarrationMetadata.ForbiddenTermsVersion`.
   - `LoadPromptAssets` reads:
     - `docs/prompts/scenario/03_writing.md`
     - `docs/prompts/scenario/critic_agent.md`
     - `docs/prompts/scenario/format_guide.md`
   - `LoadForbiddenTerms` returns `domain.ErrValidation` for missing file, invalid UTF-8, or any regex compile error.
   - Matching is case-insensitive for ASCII and exact for Korean text. Compile each pattern as-is; do **not** auto-escape lines because the artifact is deliberately authorable as regex.
   - Add `assets_test.go` and `policy_test.go` covering comment skipping, invalid regex failure, hash/version stability, and scene-number-aware matches.

3. **AC-WRITER-AGENT:** `internal/pipeline/agents/writer.go` implements the Korean narration generator as an `AgentFunc` factory over injected assets and `domain.TextGenerator`.

   ```go
   type TextAgentConfig struct {
       Model       string
       Provider    string
       MaxTokens   int
       Temperature float64
   }

   func NewWriter(
       gen domain.TextGenerator,
       cfg TextAgentConfig,
       prompts PromptAssets,
       validator *Validator,
       terms *ForbiddenTerms,
   ) AgentFunc
   ```

   Behavior:
   - Input validation:
     - `state` non-nil
     - `state.Research` non-nil
     - `state.Structure` non-nil
     - `cfg.Model` and `cfg.Provider` non-empty
     - `gen`, `validator`, `terms` non-nil
   - Prompt rendering uses:
     - `state.Structure` as `{scene_structure}`
     - `state.Research.VisualIdentity` as `{scp_visual_reference}`
     - `prompts.FormatGuide` as `{format_guide}`
     - rendered forbidden-term bullet list as `{forbidden_terms_section}`
   - The agent calls `gen.Generate(ctx, domain.TextRequest{...})` exactly once. This story does **not** add an internal retry loop; retry orchestration remains at the pipeline level.
   - Parse the model response as JSON only. Implement a small helper that trims outer whitespace and one surrounding ```` ```json ... ``` ```` fence if present before `json.Unmarshal`; any other non-JSON wrapper returns `domain.ErrValidation`.
   - Validate the decoded value against `writer_output.schema.json`.
   - Run `terms.MatchNarration` across every `scene.narration`. Any hit returns `domain.ErrValidation` with a stable message containing scene numbers and patterns; `state.Narration` stays untouched.
   - On success set `state.Narration = &script`, fill `script.Metadata` from the generator response / config, and set `script.SourceVersion = domain.NarrationSourceVersionV1`.

   Tests in `writer_test.go`:
   - `TestWriter_Run_Happy`
   - `TestWriter_Run_StripsCodeFence`
   - `TestWriter_Run_InvalidJSON`
   - `TestWriter_Run_NilStructure`
   - `TestWriter_Run_SchemaViolation`
   - `TestWriter_Run_ForbiddenTermsRejected`
   - `TestWriter_Run_MetadataFilled`
   - `TestWriter_Run_DoesNotMutateStateOnFailure`

4. **AC-RUNTIME-PROVIDER-GUARD:** implement a runtime Writer/Critic provider guard in the pipeline layer so FR12 is enforced at run entry, not only in `pipeline doctor`.

   `internal/pipeline/provider_guard.go`:

   ```go
   func ValidateDistinctProviders(writerProvider, criticProvider string) error
   ```

   Rules:
   - Empty writer or critic provider is `domain.ErrValidation`.
   - Same provider returns the exact message already used by preflight:
     `"Writer and Critic must use different LLM providers"`.
   - The canonical Phase A runner entrypoint (`PhaseARunner.Run` from Story 3.1, or the equivalent runner if Story 3.1 has not landed yet) must call this guard before the Writer or Critic LLM is invoked.
   - This is defense-in-depth with `internal/config.WriterCriticCheck`; do **not** remove or weaken the preflight check.

   Tests:
   - `TestValidateDistinctProviders_DifferentOK`
   - `TestValidateDistinctProviders_SameRejected`
   - `TestValidateDistinctProviders_EmptyRejected`
   - A runner-level test that proves the guard triggers before any Writer generator call.

5. **AC-CRITIC-PRECHECK-AND-RETRY-REASON:** before any LLM Critic call, run rule-based prechecks against the Writer output and short-circuit to a deterministic retry report on failure.

   `internal/pipeline/agents/critic_precheck.go` (or a private helper inside `critic.go`):

   ```go
   func runPostWriterPrecheck(
       script *domain.NarrationScript,
       validator *Validator,
       terms *ForbiddenTerms,
   ) (domain.CriticPrecheck, error)

   func DeriveRetryReason(scores domain.CriticRubricScores) string
   ```

   Rules:
   - Re-validate the narration against `writer_output.schema.json` even though the Writer already did. This is deliberate defense-in-depth for edited or reconstructed state.
   - Re-run forbidden-term matching before Critic invocation.
   - If schema validation fails or forbidden terms are found, **do not** call the Critic model. Instead synthesize a `CriticCheckpointReport` with:
     - `checkpoint = "post_writer"`
     - `verdict = "retry"`
     - `retry_reason = "schema_validation_failed"` or `"forbidden_terms_detected"`
     - `overall_score = 0`
     - all rubric scores `0`
     - Korean `feedback` describing the blocking issue
     - `precheck.short_circuited = true`
   - `DeriveRetryReason` is used only for LLM-produced retry verdicts. It picks the lowest rubric score; tie-break order is `hook`, `fact_accuracy`, `emotional_variation`, `immersion`. Returned strings:
     - `weak_hook`
     - `fact_accuracy`
     - `emotional_variation`
     - `immersion`

   Tests:
   - `TestPostWriterPrecheck_SchemaFailureShortCircuits`
   - `TestPostWriterPrecheck_ForbiddenTermsShortCircuits`
   - `TestDeriveRetryReason_LowestWins`
   - `TestDeriveRetryReason_TieBreakOrder`

6. **AC-POST-WRITER-CRITIC-AGENT:** `internal/pipeline/agents/critic.go` implements the post-Writer checkpoint agent.

   ```go
   func NewPostWriterCritic(
       gen domain.TextGenerator,
       cfg TextAgentConfig,
       prompts PromptAssets,
       writerValidator *Validator,
       criticValidator *Validator,
       terms *ForbiddenTerms,
       writerProvider string,
   ) AgentFunc
   ```

   Behavior:
   - Validate `state.Narration` is non-nil.
   - Call `pipeline.ValidateDistinctProviders(writerProvider, cfg.Provider)` before any LLM call.
   - Run the precheck helper from AC-CRITIC-PRECHECK-AND-RETRY-REASON.
   - On precheck short-circuit:
     - populate `state.Critic.PostWriter`
     - leave `state.Critic.PostReviewer` nil
     - return `nil` so the caller can inspect the verdict rather than treat it as infrastructure failure
   - Otherwise render the Critic prompt from:
     - `prompts.CriticTemplate`
     - `prompts.FormatGuide`
     - serialized `state.Narration`
   - Parse JSON response, validate against `critic_post_writer.schema.json`, compute `overall_score` if the model omitted it, and if `verdict == retry && retry_reason == ""` fill it via `DeriveRetryReason`.
   - Persist the result into `state.Critic.PostWriter` without erasing a future `PostReviewer` slot.
   - `feedback` must remain Korean; tests should reject an obviously English-only canned response.

   Tests in `critic_test.go`:
   - `TestPostWriterCritic_Run_Happy`
   - `TestPostWriterCritic_Run_SameProviderBlocked`
   - `TestPostWriterCritic_Run_PrecheckRetryWithoutLLMCall`
   - `TestPostWriterCritic_Run_FillsRetryReasonFromRubric`
   - `TestPostWriterCritic_Run_PreservesPostReviewerNil`
   - `TestPostWriterCritic_Run_InvalidCriticJSON`
   - `TestPostWriterCritic_Run_CriticSchemaViolation`

   Machine-readable contract:
   - `state.Critic.PostWriter.RetryReason` is the single source of truth for why a post-Writer retry was requested.
   - If the canonical runner / engine wiring already exists during implementation, mirror that string into stage observability / `runs.retry_reason` at the same time the verdict is recorded.

7. **AC-CONTRACTS-AND-SAMPLES:** add and manually review four new contract files under `testdata/contracts/`.

   Required files:
   - `writer_output.schema.json`
   - `writer_output.sample.json`
   - `critic_post_writer.schema.json`
   - `critic_post_writer.sample.json`

   Writer schema requirements:
   - Draft-07
   - top-level required: `scp_id`, `title`, `scenes`, `metadata`, `source_version`
   - `scenes`: array `minItems: 8`, `maxItems: 12`
   - each scene requires `scene_num`, `act_id`, `narration`, `fact_tags`, `mood`, `entity_visible`, `location`, `characters_present`, `color_palette`, `atmosphere`
   - `characters_present`: `minItems: 1`
   - `metadata.language`: const `"ko"`
   - `source_version`: const `"v1-llm-writer"`
   - `additionalProperties: false` everywhere

   Critic schema requirements:
   - Draft-07
   - required: `checkpoint`, `verdict`, `overall_score`, `rubric`, `feedback`, `scene_notes`, `precheck`, `critic_model`, `critic_provider`, `source_version`
   - `checkpoint`: const `"post_writer"`
   - `verdict`: enum `pass`, `retry`, `accept_with_notes`
   - each rubric field integer `0..100`
   - `overall_score` integer `0..100`
   - `precheck.schema_valid` boolean, `precheck.forbidden_term_hits` array, `precheck.short_circuited` boolean
   - `source_version`: const `"v1-critic-post-writer"`
   - `additionalProperties: false` everywhere

   Sample fixtures:
   - use the `SCP-TEST` path introduced by Story 3.2 as the canonical happy input
   - are produced by fake generators, not real APIs
   - are checked in manually; no `-update` flag or auto-regeneration path

8. **AC-TESTS-AND-FR-COVERAGE:** implementation is complete only when unit coverage, contract coverage, and FR mapping are updated together.

   Required FR coverage updates in `testdata/fr-coverage.json`:
   - `FR12` — runtime Writer/Critic provider inequality
   - `FR13` — post-Writer Critic invocation
   - `FR24` — verdict + rubric sub-scores
   - `FR25` — precheck short-circuit (schema + forbidden regex)
   - `FR48` — Writer forbidden-term enforcement

   Minimum tests to cite:
   - `TestValidateDistinctProviders_SameRejected`
   - `TestPostWriterCritic_Run_Happy`
   - `TestPostWriterCritic_Run_PrecheckRetryWithoutLLMCall`
   - `TestPostWriterCritic_Run_FillsRetryReasonFromRubric`
   - `TestWriter_Run_ForbiddenTermsRejected`
   - `TestForbiddenTerms_LoadAndMatch`

   Validation commands:
   - `go test ./...`
   - `go build ./...`
   - `go run scripts/lintlayers/main.go`

## Tasks / Subtasks

- [x] **T1: Domain types + single state carrier guard** (AC: 1)
  - [x] Add `internal/domain/narration.go` and `internal/domain/critic.go`.
  - [x] Update the canonical `PipelineState` in place so it carries `Narration` and `Critic` without introducing a duplicate state type.
  - [x] Add JSON round-trip tests for both new domain files.

- [x] **T2: Authorable prompt/policy artifacts** (AC: 2)
  - [x] Update [docs/prompts/scenario/03_writing.md](../../docs/prompts/scenario/03_writing.md).
  - [x] Update [docs/prompts/scenario/critic_agent.md](../../docs/prompts/scenario/critic_agent.md).
  - [x] Add [docs/policy/forbidden_terms.ko.txt](../../docs/policy/forbidden_terms.ko.txt).
  - [x] Implement prompt and forbidden-term loaders with file-hash versioning.

- [x] **T3: Writer agent** (AC: 3)
  - [x] Add `internal/pipeline/agents/writer.go`.
  - [x] Add strict JSON decode helper.
  - [x] Validate schema and forbidden-term list before mutating state.
  - [x] Add Writer tests for happy path, invalid JSON, schema violation, and forbidden terms.

- [x] **T4: Runtime provider guard** (AC: 4)
  - [x] Add `internal/pipeline/provider_guard.go`.
  - [x] Reuse the exact doctor error string for same-provider rejection.
  - [x] Wire the guard into the canonical Phase A runtime entrypoint before Writer/Critic calls.

- [x] **T5: Precheck + retry reason helper** (AC: 5)
  - [x] Add precheck helper.
  - [x] Add deterministic `DeriveRetryReason`.
  - [x] Add tests for schema short-circuit, forbidden-term short-circuit, and tie-breaking.

- [x] **T6: Post-Writer Critic agent** (AC: 6)
  - [x] Add `internal/pipeline/agents/critic.go`.
  - [x] Short-circuit on precheck failure without calling the Critic LLM.
  - [x] Persist result into `state.Critic.PostWriter`.
  - [x] Preserve the `PostReviewer` slot for Story 3.5.

- [x] **T7: Contracts, samples, and FR mapping** (AC: 7, 8)
  - [x] Add the four JSON contract/sample files under `testdata/contracts/`.
  - [x] Update `testdata/fr-coverage.json` for FR12/FR13/FR24/FR25/FR48.
  - [x] Run `go test ./...`, `go build ./...`, and `go run scripts/lintlayers/main.go`.

## Dev Notes

### Single-Carrier Rule

Stories 3.1 and 3.2 were generated independently and their draft assumptions about `PipelineState` are not perfectly aligned. This story resolves that ambiguity in one direction only: keep **one** canonical Phase A carrier and extend it. The Writer/Critic work must not create a second state struct in another package, even temporarily. If a migration from `json.RawMessage` to typed fields is needed, do it in place.

### Why Prompt/Policy Loading Happens Outside `AgentFunc`

The architecture's purity rule is about the execution path. Loading markdown templates or the forbidden-term artifact from disk on every run would quietly turn Writer/Critic into filesystem-coupled agents. Instead, loaders run at construction time, produce immutable in-memory assets, and the `AgentFunc` closure uses only injected dependencies plus `state`.

### Why Precheck Short-Circuits Return `nil`

A post-Writer Critic verdict of `retry` is a **business outcome**, not an infrastructure failure. Returning an error for a valid retry verdict would blur "the model judged this draft weak" with "the pipeline is broken." This story therefore stores a synthetic retry report in `state.Critic.PostWriter` and returns `nil` when the precheck blocks the draft.

### Retry Reason Contract

`runs.retry_reason` already exists in the domain model, but pipeline wiring for Phase A is still staged across Stories 3.1 and 3.5. The safe contract here is:

- `state.Critic.PostWriter.RetryReason` is always populated for retry verdicts.
- If runtime observability persistence already exists when this story is implemented, mirror that exact string into the run/store record immediately.
- Do not parse Korean feedback text later to reconstruct a retry reason.

### Previous Story Intelligence

- Story 1.5 already enforces Writer ≠ Critic at doctor/preflight. This story adds runtime defense-in-depth; it does **not** replace the preflight guard. [Source: [internal/config/doctor.go](../../internal/config/doctor.go)]
- Story 2.1 established that `critic` is the branching stage (`EventRetry` goes back to `write`). The post-Writer checkpoint here is the data contract that future stage wiring will consume. [Source: [_bmad-output/implementation-artifacts/2-1-state-machine-core-stage-transitions.md](2-1-state-machine-core-stage-transitions.md)]
- Story 3.2 introduced `SCP-TEST` and schema-validator expectations. Reuse that fixture and validator pattern instead of inventing a second contract system.

### Anti-Patterns To Avoid

1. Do not hard-code the forbidden-term list in Go source.
2. Do not let the Writer or Critic agent read prompt files during `Run`.
3. Do not treat a Critic `retry` verdict as a transport/runtime error.
4. Do not add a second `PipelineState` just to avoid touching earlier story work.
5. Do not parse prompt output with regex when strict JSON decoding is available.
6. Do not weaken the same-provider guard because doctor already checks it.
7. Do not overwrite a future `PostReviewer` field when storing the post-Writer result.

### References

- [_bmad-output/planning-artifacts/epics.md:1187-1208 — Story 3.3 acceptance criteria](../planning-artifacts/epics.md#L1187)
- [_bmad-output/planning-artifacts/sprint-prompts.md:453-470 — Story 3.3 sprint prompt and review checklist](../planning-artifacts/sprint-prompts.md#L453)
- [_bmad-output/planning-artifacts/epics.md:402-421 — Epic 3 scope](../planning-artifacts/epics.md#L402)
- [_bmad-output/planning-artifacts/prd.md:1272-1281 — FR12/FR13/FR24/FR25](../planning-artifacts/prd.md#L1272)
- [_bmad-output/planning-artifacts/prd.md:1318-1318 — FR48](../planning-artifacts/prd.md#L1318)
- [_bmad-output/planning-artifacts/prd.md:1447-1450 — NFR-M2/NFR-M3](../planning-artifacts/prd.md#L1447)
- [_bmad-output/planning-artifacts/architecture.md:175-175 — Writer ≠ Critic defense in depth](../planning-artifacts/architecture.md#L175)
- [_bmad-output/planning-artifacts/architecture.md:212-212 — rule-based prechecks before Critic](../planning-artifacts/architecture.md#L212)
- [_bmad-output/planning-artifacts/architecture.md:686-690 — Agent function chain + schema validation](../planning-artifacts/architecture.md#L686)
- [docs/prompts/scenario/03_writing.md](../../docs/prompts/scenario/03_writing.md)
- [docs/prompts/scenario/critic_agent.md](../../docs/prompts/scenario/critic_agent.md)
- [docs/prompts/scenario/format_guide.md](../../docs/prompts/scenario/format_guide.md)
- [internal/domain/config.go](../../internal/domain/config.go) — Writer/Critic provider defaults
- [internal/domain/errors.go](../../internal/domain/errors.go) — `ErrValidation` / `ErrStageFailed`
- [internal/domain/llm.go](../../internal/domain/llm.go) — `TextGenerator` contract
- [internal/testutil/assert.go](../../internal/testutil/assert.go)
- [internal/testutil/nohttp.go](../../internal/testutil/nohttp.go)
- [testdata/fr-coverage.json](../../testdata/fr-coverage.json)
- [_bmad-output/implementation-artifacts/3-1-agent-function-chain-pipeline-runner.md](3-1-agent-function-chain-pipeline-runner.md)
- [_bmad-output/implementation-artifacts/3-2-researcher-structurer-agents.md](3-2-researcher-structurer-agents.md)

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- `2026-04-18T06:14:52Z` sprint status moved to `in-progress`
- `go test ./internal/domain ./internal/pipeline/agents ./internal/pipeline`
- `go build ./...`
- `go test ./...`
- `go run scripts/lintlayers/main.go`

### Completion Notes List

- Promoted the single canonical `PipelineState` to typed `Narration` and `Critic` slots instead of introducing a second carrier.
- Added prompt/policy asset loaders plus SHA-256 forbidden-term versioning so Writer/Critic `AgentFunc` execution stays filesystem-pure.
- Implemented Writer strict JSON decoding, schema validation, metadata filling, and forbidden-term rejection without mutating state on failure.
- Implemented runtime Writer/Critic provider validation in `PhaseARunner.Run`; the Critic agent mirrors the same validation behavior locally because importing `pipeline` from `agents` would create a Go package cycle.
- Implemented post-Writer Critic prechecks, deterministic retry reasons, typed contract/sample fixtures, and FR coverage/test updates.

### File List

- `docs/prompts/scenario/03_writing.md`
- `docs/prompts/scenario/critic_agent.md`
- `docs/policy/forbidden_terms.ko.txt`
- `internal/domain/narration.go`
- `internal/domain/critic.go`
- `internal/domain/narration_test.go`
- `internal/domain/critic_test.go`
- `internal/pipeline/agents/agent.go`
- `internal/pipeline/agents/agent_test.go`
- `internal/pipeline/agents/assets.go`
- `internal/pipeline/agents/assets_test.go`
- `internal/pipeline/agents/policy.go`
- `internal/pipeline/agents/policy_test.go`
- `internal/pipeline/agents/writer.go`
- `internal/pipeline/agents/writer_test.go`
- `internal/pipeline/agents/critic_precheck.go`
- `internal/pipeline/agents/critic.go`
- `internal/pipeline/agents/critic_test.go`
- `internal/pipeline/agents/validator_test.go`
- `internal/pipeline/provider_guard.go`
- `internal/pipeline/provider_guard_test.go`
- `internal/pipeline/phase_a.go`
- `internal/pipeline/phase_a_test.go`
- `internal/pipeline/phase_a_integration_test.go`
- `testdata/contracts/structurer_output.sample.json`
- `testdata/contracts/writer_output.schema.json`
- `testdata/contracts/writer_output.sample.json`
- `testdata/contracts/critic_post_writer.schema.json`
- `testdata/contracts/critic_post_writer.sample.json`
- `testdata/fr-coverage.json`

### Change Log

- 2026-04-18: Implemented Story 3.3 Writer + post-Writer Critic, runtime provider guard, forbidden-term policy loading, typed contracts, and full verification updates.
- 2026-04-18: Code review (Blind Hunter + Edge Case Hunter + Acceptance Auditor) — see Review Findings below.

### Review Findings

**Adversarial review:** 3 parallel layers (Blind Hunter / Edge Case Hunter / Acceptance Auditor) over Story 3.3 scope. Triaged: 10 patch, 1 defer, ~20 dismissed as by-design / spec-compliant / false positive.

#### patch (HIGH)

- [x] [Review][Patch] Same-provider error must wrap `domain.ErrValidation` for `domain.Classify` routing [internal/pipeline/provider_guard.go:14, internal/pipeline/agents/critic.go:142]
- [x] [Review][Patch] Precheck short-circuit report must go through `criticValidator.Validate` too [internal/pipeline/agents/critic.go:51-55]
- [x] [Review][Patch] `critic_agent.md` `retry_reason` enum must document the 2 precheck system values (`schema_validation_failed`, `forbidden_terms_detected`) as reserved [docs/prompts/scenario/critic_agent.md]
- [x] [Review][Patch] `decodeJSONResponse` must strip UTF-8 BOM before bare-JSON prefix check [internal/pipeline/agents/critic.go:147-166]
- [x] [Review][Patch] `trimSingleFence` must tolerate CRLF line endings and case-insensitive fence labels (e.g., ` ```JSON `) [internal/pipeline/agents/critic.go:168-183]
- [x] [Review][Patch] `ForbiddenTerms.MatchNarration` must scan `title`, `fact_tags.content`, `location`, `atmosphere`, `mood` — not only `narration` (NFR-M2 policy coverage gap) [internal/pipeline/agents/policy.go:67-89]

#### patch (MEDIUM)

- [x] [Review][Patch] `containsHangul` must include Hangul Jamo (U+1100–U+11FF) and Compatibility Jamo (U+3130–U+318F) ranges + cache `regexp` at package level [internal/pipeline/agents/critic.go:133-135]
- [x] [Review][Patch] `validateRunID` must reject control characters, NUL, whitespace [internal/pipeline/phase_a.go:239-252]
- [x] [Review][Patch] `scoreRubric` must use `math.Round` — truncation biases rubric-derived scores downward by up to 1 [internal/pipeline/agents/critic.go:126-131]
- [x] [Review][Patch] `NewPhaseARunner` must reject empty `writerProvider`/`criticProvider` at construction (fail-fast, not at Run) [internal/pipeline/phase_a.go:53-96]

#### defer

- [x] [Review][Defer] `writeScenario` missing parent-directory fsync — cross-architecture durability concern, not introduced by Story 3.3 [internal/pipeline/phase_a.go:289-333] — deferred, pre-existing architecture

#### dismissed (with rationale)

- Blind Hunter #1 (test compile error) — false positive; `go build` + `go test ./internal/pipeline/...` pass.
- `fillNarrationMetadata` overwrite of model-returned metadata — **by design** per AC-3 ("fill `script.Metadata` from the generator response / config").
- `overall_score == 0` silent recompute — **by design** fallback for LLM-omitted field.
- Regex pattern injection risk in `LoadForbiddenTerms` — **explicitly allowed** per spec ("Compile each pattern as-is; do not auto-escape lines because the artifact is deliberately authorable as regex").
- `validateDistinctProvidersLocal` in `agents/critic.go` duplicating pipeline-level guard — **accepted** per Completion Notes (Go package cycle prevents shared import); mitigated by H1 wrapping both in `ErrValidation` consistently.
- `PromptAssets` carrying `VisualBreakdownTemplate` / `ReviewerTemplate` + matching files — Story 3.4 scope already landed in this commit; files exist, tests pass.
- `sample-sha256` placeholder in `writer_output.sample.json` — fixture convention; no runtime assertion of SHA-256 shape.
- `describeValidation` truncation to 3 errors — UX trade-off, not a correctness bug.
- LLM transport errors not wrapped in `ErrValidation` — transport/5xx errors are distinct from validation; classification layer handles separately.
- `DeriveRetryReason` returning `weak_hook` on ties / all-high scores — spec-mandated tie-break order; callers should not interpret as clinical diagnosis.
- `ensureCriticState` overwriting prior `PostWriter` on rerun — spec permits; preservation of `PostReviewer` is the only invariant.
- Several other low-severity UX/observability nits — tracked implicitly; re-raise if operator friction shows up.
