# Story 11.2: DeepSeek Adapter + Tuning Surface FULL

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the Tuning surface to execute Golden and Shadow against a real second text provider,
so that FR12 Writer!=Critic is satisfiable in production and the CP-5 / AI-5 blocker clears for the remaining E2E suite.

## Blocker Target

This story closes **CP-5 Story 10-2 FULL** plus **AI-5 `LoadShadowInput`** from the post-epic quality plan.

- Source: `_bmad-output/test-artifacts/quality-strategy-2026-04-25.md` §3 P0
- Source: `_bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md` §10, §12
- Decision: DeepSeek is the V1 second text provider; Gemini stays doc-only / fallback tier for V1.5.

## Current Reality To Reuse

- The Tuning vertical slice already has partial foundations in place:
  - `internal/api/handler_tuning.go`
  - `internal/service/tuning_service.go`
  - `web/src/components/shells/TuningShell.tsx`
  - `web/src/components/tuning/*`
  - `migrations/013_run_critic_prompt_version.sql`
  - `internal/db/run_store.go`
- The remaining blocker is not a greenfield Tuning build. It is the completion pass that makes the existing surface executable with a **real production DeepSeek runtime** and a **live-run-safe Shadow path**.
- `cmd/pipeline/serve.go` still wires `eval.NotConfiguredEvaluator{}` into `NewTuningService`, so Golden/Shadow/Fast Feedback cannot satisfy the production path today.
- `internal/llmclient/deepseek/` is still package-doc only.
- `internal/critic/eval.LoadShadowInput(...)` still resolves relative `scenario_path` against `projectRoot`, not `{outputDir}/{runID}`.

## Acceptance Criteria

### AC-1: DeepSeek ships as the real V1 second text provider

**Given** FR12 requires Writer and Critic to be satisfiable as different production providers  
**When** this story lands  
**Then** `internal/llmclient/deepseek/` contains a real production adapter implementing `domain.TextGenerator`  
**And** the adapter is wired through the existing limiter / HTTP client / config patterns rather than a one-off runtime path  
**And** DashScope remains the first production text provider already in use for the writer path  
**And** Gemini stays doc-only / deferred for V1.

**Rules:**

- Reuse the existing provider construction rules from Epic 1 and Story 5.1: constructor-injected `*http.Client`, limiter-backed execution, no `http.DefaultClient`.
- DeepSeek must be contract-tested at the client boundary.
- No SiliconFlow or alternate vendor substitution is introduced in this story.

### AC-2: Tuning backend runs Golden, Shadow, and Fast Feedback through a real evaluator

**Given** the Tuning API surface and shell already exist  
**When** the operator saves a Critic prompt and runs Tuning actions  
**Then** `/api/tuning/critic-prompt`, `/api/tuning/golden/*`, `/api/tuning/shadow/run`, and `/api/tuning/fast-feedback` execute through a real Critic-backed evaluator rather than `eval.NotConfiguredEvaluator{}`  
**And** the existing AC-6 Golden-before-Shadow session gate remains intact  
**And** prompt-version stamping on newly created runs continues to work.

**Rules:**

- Reuse `internal/critic/eval.RunGolden`, `RunShadow`, and the existing Tuning service / handler boundaries; do not introduce a second evaluator stack.
- Preserve the canonical prompt file at `docs/prompts/scenario/critic_agent.md`.
- Keep run-level `critic_prompt_version` and `critic_prompt_hash` immutable once stamped at run creation.

### AC-3: AI-5 `LoadShadowInput` production-path bug is fixed on the real live-run layout

**Given** a completed Phase-A run has `runs.scenario_path = "scenario.json"` or another run-relative path  
**When** Shadow replay loads that run from the live DB  
**Then** `LoadShadowInput` resolves the artifact against `{outputDir}/{runID}` rather than `projectRoot`  
**And** live-DB Shadow replay succeeds without file-not-found failure on the production directory layout  
**And** the existing fixture-shape / schema-validation guarantees remain intact.

**Rules:**

- Keep absolute-path handling unchanged.
- Preserve the existing Golden-fixture-shape reuse rule in `internal/critic/eval`.
- Add or extend integration coverage against a real DB + FS run layout, not fixture-only project-relative paths.

### AC-4: Existing partial Tuning UI is hardened, not replaced

**Given** the current repo already contains `TuningShell` and section components  
**When** this story is implemented  
**Then** the operator can complete the intended `/tuning` flow on the existing page:

1. Edit Critic prompt
2. Save version
3. Run Golden
4. Observe Shadow unlock only after Golden passes
5. Run Shadow
6. Inspect diff / report output

**And** the UI visibly reflects DeepSeek as the Critic-side provider where provider identity is surfaced  
**And** failures remain inline / fail-loud instead of silently degrading to placeholder behavior.

**Rules:**

- Evolve the current `web/src/components/tuning/*` implementation; do not throw it away and rebuild a second shell.
- Preserve current React Query / `apiClient` / `queryKeys` patterns.
- Any remaining placeholder copy or non-functional sections that block the above flow must be removed or completed.

### AC-5: Blocker-release verification criteria are carried unchanged from Step 3

This story is not complete until the blocker-release checks below are true:

- `SMOKE-06 green against a real live-DB Phase-A output`
- `UI-E2E-07 tuning surface loads DeepSeek as critic`
- `UI-E2E-07 green`

## Impacted Files

Expected implementation touch-set for this story:

- `internal/llmclient/deepseek/`
  - add real adapter implementation and contract tests
- `cmd/pipeline/serve.go`
  - replace `eval.NotConfiguredEvaluator{}` wiring with the real evaluator path
- `internal/service/tuning_service.go`
  - ensure Golden / Shadow / Fast Feedback consume the real evaluator cleanly
- `internal/api/handler_tuning.go`
- `internal/api/handler_tuning_test.go`
- `internal/critic/eval/shadow.go`
- `internal/critic/eval/shadow_source.go`
- `internal/critic/eval/shadow_test.go`
- `internal/service/run_service.go`
- `internal/db/run_store.go`
- `internal/db/run_store_test.go`
- `internal/domain/types.go`
- `web/src/components/shells/TuningShell.tsx`
- `web/src/components/tuning/CriticPromptSection.tsx`
- `web/src/components/tuning/GoldenEvalSection.tsx`
- `web/src/components/tuning/ShadowEvalSection.tsx`
- `web/src/components/tuning/FastFeedbackSection.tsx`
- `web/src/components/tuning/SaveRecommendationBanner.tsx`
- `web/src/contracts/tuningContracts.ts`
- `web/src/contracts/runContracts.ts`
- `web/src/lib/apiClient.ts`
- `web/src/lib/queryKeys.ts`
- `web/src/components/shells/TuningShell.test.tsx`

Verification files that become executable once this story lands:

- `internal/pipeline/smoke06_shadow_live_test.go`
- `web/e2e/` tuning E2E spec for `UI-E2E-07`

## Tasks / Subtasks

- [x] **T1: Implement the real DeepSeek text adapter** (AC: 1)
  - [x] Create the production client in `internal/llmclient/deepseek/`
  - [x] Conform to existing provider-construction, limiter, and test-isolation rules
  - [x] Add contract tests for success, normalization, and error handling

- [x] **T2: Wire the real evaluator into the Tuning runtime** (AC: 1, 2)
  - [x] Replace `eval.NotConfiguredEvaluator{}` in `cmd/pipeline/serve.go`
  - [x] Ensure Tuning service actions execute end-to-end through the real Critic path
  - [x] Preserve prompt-version stamping on run creation

- [x] **T3: Fix AI-5 live-run Shadow path resolution** (AC: 3)
  - [x] Update `LoadShadowInput` to resolve run-relative `scenario_path` against `{outputDir}/{runID}`
  - [x] Keep absolute-path and schema-validation behavior intact
  - [x] Add live DB + filesystem coverage for the production layout

- [x] **T4: Complete the existing Tuning shell for the real operator flow** (AC: 2, 4)
  - [x] Ensure prompt save -> Golden -> Shadow unlock -> diff inspection is fully functional
  - [x] Surface DeepSeek provider identity where the tuning result/report shows provider metadata
  - [x] Remove any remaining placeholder-only or not-configured behavior in the happy path

- [x] **T5: Add story-level verification coverage** (AC: 5)
  - [x] Add/extend unit and integration tests around DeepSeek adapter + Shadow path fix
  - [x] Leave Step 6-ready hooks for `SMOKE-06` and `UI-E2E-07`

## Unblocked E2E Scenarios

When this story is complete, Step 6 can backfill:

- **SMOKE-06 — Shadow Eval Against Live Run**
  - Go integration
  - verification target: live DB + FS path, `LoadShadowInput` success, `shadow_results` persistence
- **UI-E2E-07 — Tuning Surface End-to-End**
  - Playwright
  - verification target: prompt edit/save, Golden pass, Shadow unlock, diff view, DeepSeek critic load

## Estimated Effort

**Jay solo estimate:** **18-26 hours** total, about **2.5-3.5 dev-days**.

Suggested split:

- DeepSeek adapter + contract tests: 6-8h
- Real evaluator wiring in serve/tuning runtime: 3-5h
- `LoadShadowInput` live-path fix + integration coverage: 4-6h
- Tuning UI completion / contract hardening: 5-7h

## Dev Notes

- Story 10.2's broad vertical-slice document should be treated as the architectural parent. This story is the **blocker-closing completion pass**, not a second independent Tuning feature.
- The repo already contains partial Story 10.2 implementation artifacts. Build on those files instead of replacing them.
- Keep the production-vs-test seam clean:
  - production runtime uses the real DeepSeek adapter
  - tests continue to block external HTTP by default
  - E2E fixtures stay recorded / deterministic
- `SMOKE-06` and `UI-E2E-07` are the authoritative unblock proofs. If the code "works locally" but these cannot be generated and turned green, the blocker is not actually cleared.

## Validation

- `go test ./internal/llmclient/... ./internal/service ./internal/api ./internal/critic/eval ./internal/db`
- `cd web && npm test -- --run TuningShell`
- Story completion gate:
  - `SMOKE-06 green against a real live-DB Phase-A output`
  - `UI-E2E-07 tuning surface loads DeepSeek as critic`
  - `UI-E2E-07 green`

## Open Questions / Assumptions

- Assumption: DeepSeek is the only V1 second text provider required for blocker release; Gemini remains deferred without blocking CP-5.
- Assumption: the current partial Tuning UI and backend files are intended to be completed, not rolled back or rewritten under a new path.
- Assumption: Step 6 will generate the final Playwright / Go E2E artifacts after this story lands, but this story must leave those scenarios immediately executable.

## Dev Agent Record

### Completion Notes

- Implemented a production `internal/llmclient/deepseek` text client with limiter-backed chat-completions calls, normalized response mapping, and contract tests for success and error taxonomy.
- Added `eval.RuntimeEvaluator` so Tuning Golden/Shadow/Fast Feedback execute through the real post-writer Critic path using current effective runtime settings and the canonical `critic_agent.md` prompt.
- Fixed AI-5 by resolving run-relative `scenario_path` values against `{outputDir}/{runID}` first, while preserving absolute-path and project-relative fallback behavior.
- Surfaced DeepSeek critic identity in Tuning Fast Feedback and Shadow reports, and added executable unblock proofs for `SMOKE-06` and `UI-E2E-07`.
- Validation run:
  - `go test ./...`
  - `cd web && npx vitest run`
  - `cd web && npx playwright test e2e/tuning-surface-deepseek.spec.ts`
  - `cd web && npx vitest run src/components/shells/TuningShell.test.tsx`
- Noted validation script mismatch: `cd web && npm test -- --run TuningShell` now passes lint + unit tests, but the trailing `--run TuningShell` is forwarded to Playwright as a file filter and exits with `No tests found`.

## File List

- cmd/pipeline/serve.go
- cmd/quality-gate/shadow.go
- internal/critic/eval/eval.go
- internal/critic/eval/runtime_evaluator.go
- internal/critic/eval/runtime_evaluator_test.go
- internal/critic/eval/shadow.go
- internal/critic/eval/shadow_integration_test.go
- internal/critic/eval/shadow_test.go
- internal/llmclient/deepseek/text.go
- internal/llmclient/deepseek/text_test.go
- internal/pipeline/smoke06_shadow_live_test.go
- internal/service/tuning_service.go
- internal/service/tuning_types.go
- web/e2e/fixtures.ts
- web/e2e/tuning-surface-deepseek.spec.ts
- web/src/components/shells/TuningShell.test.tsx
- web/src/components/tuning/FastFeedbackSection.tsx
- web/src/components/tuning/ShadowEvalSection.tsx
- web/src/contracts/tuningContracts.ts

## Change Log

- 2026-04-25: Implemented the real DeepSeek-backed Tuning evaluator/runtime, fixed live-run Shadow path resolution, added `SMOKE-06` and `UI-E2E-07`, and surfaced DeepSeek critic metadata in the Tuning UI.
- 2026-04-25: Code review batch-applied 12 patches covering Shadow path safety, DeepSeek client robustness, evaluator wiring fail-loud, and provider attribution honesty.

### Review Findings

Findings raised by parallel review (Blind Hunter + Edge Case Hunter + Acceptance Auditor) on 2026-04-25.

Patches (batch-applied):

- [x] [Review][Patch] LoadShadowInput silently fell back to projectRoot on any os.Stat error (HIGH) [internal/critic/eval/shadow.go:187-194]
- [x] [Review][Patch] ScenarioPath `..` traversal not rejected (HIGH) [internal/critic/eval/shadow.go LoadShadowInput]
- [x] [Review][Patch] Empty OutputDir produced meaningless run-relative resolution (HIGH) [internal/service/tuning_service.go RunShadow + cmd/pipeline/serve.go]
- [x] [Review][Patch] DeepSeek `Temperature=0` silently dropped via zero-as-unset guard (HIGH) [internal/llmclient/deepseek/text.go:118-121]
- [x] [Review][Patch] RuntimeEvaluator boot-time failure swallowed; fell back to NotConfiguredEvaluator with Warn log only (HIGH) [cmd/pipeline/serve.go:273-285]
- [x] [Review][Patch] writerProvider silently defaulted to "dashscope" when fixture metadata missing — falsified attribution (MEDIUM) [internal/critic/eval/runtime_evaluator.go:121-124]
- [x] [Review][Patch] DeepSeek response decoder accepted non-application/json Content-Type (MEDIUM) [internal/llmclient/deepseek/text.go:149-152]
- [x] [Review][Patch] DeepSeek response body had no read cap; OOM/slowloris risk (MEDIUM) [internal/llmclient/deepseek/text.go:149-152]
- [x] [Review][Patch] checkStatus error message echoed raw body bytes without UTF-8 sanitization (MEDIUM) [internal/llmclient/deepseek/text.go:184-199]
- [x] [Review][Patch] Whitespace-only Prompt passed validation (MEDIUM) [internal/llmclient/deepseek/text.go:82-88]
- [x] [Review][Patch] Test helpers `mustJSONString` / `jsonEscape` reimplemented json.Marshal (LOW) [internal/critic/eval/runtime_evaluator_test.go:129-136]
- [x] [Review][Patch] Playwright `getByText("deepseek")` could match multiple nodes under strict mode (LOW) [web/e2e/tuning-surface-deepseek.spec.ts:162-164]

Deferred (logged in deferred-work.md):

- [x] [Review][Defer] DeepSeek 429 `Retry-After` header dropped — back-off cannot honor provider hint (HIGH, but architectural — needs structured ErrRateLimited across providers)
- [x] [Review][Defer] `RunShadow(projectRoot, outputDir, ...)` two adjacent string params invite caller bugs (MEDIUM — wrap into Options struct)
- [x] [Review][Defer] RuntimeEvaluator rebuilds DeepSeek client + reloads prompt assets per fixture call (MEDIUM — perf, not correctness)
- [x] [Review][Defer] CallLimiter.Do races with caller on cancel via `out` closure (MEDIUM — pre-existing in limiter.go, outside this story's diff)
- [x] [Review][Defer] Cosmetic single-quote → double-quote churn in web fixtures and tuning section components (LOW — reverting is more churn than the churn itself)
- [x] [Review][Defer] FinishReason="length" treated as success — silent truncation when max_tokens hit (LOW — improvement, not bug)

Dismissed as noise:

- chatCompletionRequest.MaxTokens has both `omitempty` and a `> 0` guard — redundant but harmless.
- choices[0].Message.Content taken without Role=="assistant" check — defensive over-engineering.
- `os.Stat` then `os.ReadFile` in LoadShadowInput is technically TOCTOU — negligible in this context.
- Spec "Impacted Files" list diverges from actual diff — informational; the implementation was more surgical than predicted.
