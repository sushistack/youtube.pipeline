# Brownfield Current State — Phase A Pipeline (2026-05-02)

Compiled before the SCP-Explained quality-uplift refactor (`next-session-enhance-prompts.md`). All claims are file:line backed; nothing speculative.

## 1. Pipeline shape (verified)

The spec assumes `Research → Structure → Script writing → Shot planning → Review → Critic pass → Scenario revision`. **Reality differs in two material ways**:

1. There are **two critic checkpoints**, not one.
2. The "Shot planning" stage is named **VisualBreakdowner** in code and runs *after* the post-writer critic.

Actual seven-stage chain (`internal/pipeline/agents/agent.go:85-94`):

```
Researcher → Structurer → Writer → PostWriterCritic
            → VisualBreakdowner → Reviewer → PostReviewerCritic
```

Orchestrator: `PhaseARunner` at `internal/pipeline/phase_a.go:29-299`. Retry routing on `CriticVerdictRetry` short-circuits Visual/Reviewer/PostReviewer (`phase_a.go:260-266`). On retry the engine rewinds to `StageWrite` (`internal/pipeline/engine.go:48-49`); writer prompt receives `state.PriorCriticFeedback` (`phase_a.go:62-66`).

Anti-progress detector (`internal/pipeline/antiprogress.go`) hard-stops if two consecutive Writer outputs exceed 0.92 cosine similarity.

After PostReviewer pass, engine advances to `StageScenarioReview` (HITL) before character/image phases.

## 2. Agent interfaces

All agents implement `agents.AgentFunc` (`internal/pipeline/agents/agent.go:25`):

```go
type AgentFunc func(ctx context.Context, state *PipelineState) error
```

State is in-memory only during Phase A; runner serializes to `{outputDir}/{runID}/scenario.json` after Critic returns. Direct function calls — no channels, no message bus, no MCP. Dependencies (TextGenerator, validators, prompt assets) are injected via factory closures.

| Agent | File | Out field |
|---|---|---|
| Researcher | `internal/pipeline/agents/researcher.go` | `state.Research *domain.ResearcherOutput` |
| Structurer (deterministic, no LLM) | `internal/pipeline/agents/structurer.go` | `state.Structure *domain.StructurerOutput` |
| Writer (per-act fan-out) | `internal/pipeline/agents/writer.go` | `state.Narration *domain.NarrationScript` |
| PostWriterCritic | `internal/pipeline/agents/critic.go` | `state.Critic.PostWriter` |
| VisualBreakdowner (per-scene fan-out) | `internal/pipeline/agents/visual_breakdowner.go` | `state.VisualBreakdown` |
| Reviewer | `internal/pipeline/agents/reviewer.go` | `state.Review` |
| PostReviewerCritic | `internal/pipeline/agents/critic.go` | `state.Critic.PostReviewer` |

## 3. Prompt storage

Prompts live as plain markdown files in `docs/prompts/scenario/` and are loaded at startup via `os.ReadFile` (NOT `embed.FS`). `internal/pipeline/agents/assets.go:11-60` defines `PromptAssets` with `WriterTemplate`, `CriticTemplate`, `VisualBreakdownTemplate`, `ReviewerTemplate`, `FormatGuide`.

Substitution mechanism is `strings.NewReplacer` over `{var}` tokens (e.g., `internal/pipeline/agents/writer.go:415-429`) — *not* Go `text/template`.

Files:

- `docs/prompts/scenario/01_research.md` (reference; researcher is deterministic)
- `docs/prompts/scenario/02_structure.md` (reference; structurer is deterministic)
- `docs/prompts/scenario/03_writing.md` (writer)
- `docs/prompts/scenario/03_5_visual_breakdown.md`
- `docs/prompts/scenario/04_review.md`
- `docs/prompts/scenario/critic_agent.md`
- `docs/prompts/scenario/format_guide.md`

## 4. LLM client abstraction

Interface: `domain.TextGenerator` (`internal/domain/llm.go:67-71`):

```go
type TextGenerator interface {
    Generate(ctx context.Context, req TextRequest) (TextResponse, error)
}
```

Implementations under `internal/llmclient/`: `deepseek/`, `gemini/`, `dashscope/`, `dryrun/`. Streaming is **not used**. Retry/backoff in `internal/llmclient/retry.go`. Per memory rule `feedback_api_dashscope_only.md`: Qwen calls go through DashScope only.

## 5. Existing schemas & contracts

Domain types in `internal/domain/`:

| Type | Defined | Existing JSON Schema |
|---|---|---|
| `ResearcherOutput` | `scenario.go:5-19` | `testdata/contracts/researcher_output.schema.json` |
| `StructurerOutput` | `scenario.go:40-45` | `testdata/contracts/structurer_output.schema.json` |
| `NarrationScript` | `narration.go:8-14` | `testdata/contracts/writer_output.schema.json` |
| `VisualBreakdownOutput` | `visual_breakdown.go:12-20` | `testdata/contracts/visual_breakdown.schema.json` |
| `ReviewReport` | `review.go:13-21` | `testdata/contracts/reviewer_report.schema.json` |
| `CriticOutput` (post-writer + post-reviewer) | `critic.go:22-25` | `testdata/contracts/critic_post_writer.schema.json`, `critic_post_reviewer.schema.json` |

Validation runtime: `internal/pipeline/agents/validator.go:14-56` using `github.com/santhosh-tekuri/jsonschema/v5` (already in `go.mod`). Schemas are compiled at startup; external `$ref` rejected.

Each domain type carries a `source_version` string (e.g., `domain.SourceVersionV1 = "v1.1-deterministic"` at `scenario.go:76`) used by the cache loader to invalidate stale cached outputs.

**Important divergence** from spec section 4: existing types are *not* a drop-in match for the v2 schemas. The spec adds new fields (`KoreanTerms`, `RelatedSCPs`, `LoreConnections`, `NarrativeArc`, `HookAngle`, `TwistPoint`, `EndingHook`, `OutroHookKO`, `SourceAttribution`, `EmotionCurve`, `SFXHint`, `AssetReuseMap`) that have no counterpart today. v2 must be additive — see `change_impact.md`.

## 6. Critic implementation

Two checkpoints, both implemented in `internal/pipeline/agents/critic.go`:

- `NewPostWriterCritic(...)` — runs after Writer
- `NewPostReviewerCritic(...)` — runs after Reviewer

Current rubric is **4 criteria, equally weighted** (`internal/domain/critic.go:15-20`):

```go
var CriticRubricWeights = map[string]float64{
    "hook":                0.25,
    "fact_accuracy":       0.25,
    "emotional_variation": 0.25,
    "immersion":           0.25,
}
```

Verdicts: `pass` / `retry` / `accept_with_notes` (`domain/critic.go:6-9`). Score is 1–100 integer. Precheck (`internal/pipeline/agents/critic_precheck.go`) runs forbidden-term scan before LLM call; a hit auto-retries without LLM.

Spec section 5 wants a **10-criterion rubric** with new criteria (hook ≤15s, twist 70-85%, sentence rhythm, sensory language, POV consistency, visual reusability) — none of these are scored today. Several can be evaluated **deterministically** without an LLM (criteria 1, 4, 6, 7, 8, 10); the rest still require model judgment (2, 3, 5, 9). See `change_impact.md` for the v2 rubric implementation plan.

## 7. Config loader

`internal/config/loader.go:14-84` uses **Viper** with this priority order: defaults → `.env` (godotenv) → `config.yaml` → environment variables → CLI flags. There is **no feature-flag mechanism** today — toggles are flat config values (e.g., `DryRun bool`).

For v2 gating we therefore route through plain `os.Getenv` (`PIPELINE_CONTRACT_VERSION=v2`, `USE_TEMPLATE_PROMPTS=true`) read at agent construction time. This avoids touching the existing config struct surface.

## 8. Tests

- 139 `*_test.go` files across `internal/` and `cmd/`.
- Standard `testing.T`, table-driven, custom helpers in `internal/testutil/` (`BlockExternalHTTP(t)`, `NewTestDB(t)`, `LoadRunStateFixture(t, fixture)`, `CaptureLog(t)`). **No testify**.
- Schema fixtures live in `testdata/contracts/*.schema.json` paired with `*.sample.json`.
- Test command (`Makefile:14`):

  ```
  CGO_ENABLED=0 go test ./cmd/... ./internal/... ./migrations/... -count=1 -timeout=120s
  ```

- A layer linter runs via `make lint-layers` (`scripts/lintlayers/`) — keep new packages off forbidden imports.

## 9. Scenario revision loop

Already exists. Critic retry verdict → engine rewinds to `StageWrite` → writer re-runs with `PriorCriticFeedback` injected. Anti-progress detector caps loop at the divergence floor. The "Scenario Revision" stage in the spec is **not** a separate code stage — it is the writer re-run after critic rejection. This refactor will not touch the loop mechanics.

## 10. Korean style configuration

No centralized YAML or struct. Korean terminology and SCP lore are baked into the markdown prompt templates (`docs/prompts/scenario/03_writing.md`, `critic_agent.md`, `format_guide.md`). `internal/pipeline/transliteration.go` handles English → Korean term mapping for *image* prompts only.

The single source of truth from the spec (`configs/style_guide.yaml` + `internal/style/`) does not exist today. Adding it is purely additive.
