# Brownfield Change Impact — SCP Quality Uplift

Maps every change in this cycle to: files touched, public API surface, backward-compatibility risk, rollback. Companion to `current_state.md`. Per spec section 0, every change is **additive or feature-flagged**; no v1 callsite changes shape.

## Summary

| Change | Files | Public API impact | BC risk | Rollback |
|---|---|---|---|---|
| Brownfield discovery docs | `docs/brownfield/*.md` | none | none | delete files |
| Style guide YAML + loader | `configs/style_guide.yaml`, `internal/style/*` | new package only | none | delete `internal/style/`, `configs/style_guide.yaml` |
| Contract v2 types + adapter | `internal/contract/v2/*` | new package only | none | delete `internal/contract/v2/` |
| Critic rubric v2 (10 criteria) | `internal/critic/rubricv2/*` | new package only | none | delete `internal/critic/rubricv2/` |
| Embedded prompt templates | `prompts/agents/*.tmpl`, `prompts/embed.go` | new package only | none | delete `prompts/` |
| Writer prompt-source toggle (config.yaml) | `internal/pipeline/agents/assets.go`, `internal/domain/config.go`, `internal/config/loader.go`, two `LoadPromptAssets` callers | `LoadPromptAssets` gains `useTemplatePrompts bool` arg | low | `use_template_prompts: false` in config.yaml (default) |

## Detail

### A. `docs/brownfield/{current_state,change_impact}.md`
- Files touched: 2 new files.
- Public API: none.
- BC risk: none. Documentation only.
- Rollback: `rm -rf docs/brownfield/`.

### B. Style guide

- Files: `configs/style_guide.yaml` (new), `internal/style/loader.go` (new), `internal/style/loader_test.go` (new).
- Public API: new `style.StyleGuide` struct + `style.Load(path string) (*StyleGuide, error)` + `style.LoadDefault() (*StyleGuide, error)` (looks at `STYLE_GUIDE_PATH` env, falls back to `configs/style_guide.yaml`). No existing config code is touched.
- Dependency: `gopkg.in/yaml.v3` — already in `go.mod` (no new dep).
- BC risk: none. Existing agents don't load this file.
- Rollback: delete the new files.

### C. Contract v2

- Files: `internal/contract/v2/types.go`, `internal/contract/v2/adapter.go`, `internal/contract/v2/types_test.go`, `internal/contract/v2/adapter_test.go`.
- Public API: new structs `ResearchOutput`, `StructureOutput`, `ScriptOutput`, `Scene`, `ShotPlanOutput`, `Shot`, `CriticReport`, `Failure`, `Attribution`, `Incident`. Includes `Enabled() bool` reading `os.Getenv("PIPELINE_CONTRACT_VERSION") == "v2"`.
- Adapter: best-effort `FromNarration(*domain.NarrationScript) ScriptOutput`, lossy where v1 lacks v2 fields. Fields the v1 path cannot populate are zero-valued.
- BC risk: none. Pure additive package; no existing code imports `internal/contract/v2/`.
- Rollback: delete the directory.

### D. Critic rubric v2

- Files: `internal/critic/rubricv2/scorer.go`, `internal/critic/rubricv2/scorer_test.go`, `internal/critic/rubricv2/fixtures_test.go`.
- Public API: `rubricv2.Score(input rubricv2.Input) contractv2.CriticReport`. Pure function — no LLM, no I/O.
- Coverage:
  - Deterministic criteria scored fully: 1 (hook ≤15s), 4 (twist 70-85%), 6 (sentence rhythm), 7 (sensory language), 8 (POV consistency), 10 (visual reusability).
  - Criteria requiring LLM judgment (2 information drip, 3 concrete incident, 5 unresolved outro, 9 SCP fidelity) get a heuristic floor and a `RequiresLLMReview` flag in the failure record.
- BC risk: none. Existing `internal/pipeline/agents/critic.go` is untouched. A future cycle can wire this in behind a flag.
- Rollback: delete the directory.

### E. Embedded prompt templates

- Files: `prompts/agents/script_writer.tmpl` (new), `prompts/embed.go` (new package `prompts`).
- Public API: `prompts.ReadAgent(name string) (string, error)` + `prompts.MustReadAgent(name string) string`. Uses `//go:embed agents/*.tmpl`.
- Template content: mirrors `docs/prompts/scenario/03_writing.md`'s placeholders (`{scp_id}`, `{act_id}`, etc) so the existing `strings.NewReplacer` pipeline keeps working byte-for-byte. Adds explicit anchors for hook ≤15s, twist 70-85%, forbidden openings/endings, source attribution variables.
- BC risk: none on its own (nothing imports it yet).
- Rollback: delete `prompts/`.

### F. Writer prompt source switch

- Files: `internal/pipeline/agents/assets.go` (env-free flag branch in `loadWriterTemplate`), `internal/domain/config.go` (new `UseTemplatePrompts bool`), `internal/config/loader.go` (Viper SetDefault), `cmd/pipeline/serve.go` + `internal/critic/eval/runtime_evaluator.go` (pass `cfg.UseTemplatePrompts` into `LoadPromptAssets`).
- Public API: `agents.LoadPromptAssets` gains a `useTemplatePrompts bool` parameter (only two callers, both updated). `NewWriter(...)` and `PromptAssets` unchanged.
- Behavior:
  - Default (`use_template_prompts: false` in config.yaml — also Go zero value): identical to today, byte-for-byte.
  - When `use_template_prompts: true`: writer template string read from `prompts/agents/script_writer.tmpl` instead of `docs/prompts/scenario/03_writing.md`.
- **Why config.yaml, not env**: this is a 1-operator pipeline (memory: feedback_config_not_env.md). Operating toggles that the operator decides per deploy belong in config.yaml, not in env. Spec section 7 prescribed `USE_TEMPLATE_PROMPTS=true`; we deliberately deviate to honor the project's existing Viper-backed config convention. env stays reserved for path overrides (`STYLE_GUIDE_PATH`) and secrets (`*_API_KEY`).
- BC risk: low. The toggle defaults off; existing tests pass with the new boolean parameter set to false. Two call sites updated atomically.
- Rollback: set `use_template_prompts: false` in config.yaml (or omit — that is the default).

### G. Contract version selection

- Files: `internal/contract/v2/types.go` exposes only `Version = "v2"`.
- Public API: callers compare a config value (e.g. `cfg.ContractVersion == contractv2.Version`) when v2 wire-in lands. No env-only `Enabled()` helper.
- BC risk: none. v2 is not wired into the live pipeline this cycle.
- Rollback: trivial — no live consumer.

## Out of scope (per spec section 1)

- LLM provider changes
- Adding agents
- Animation / asset generation
- DB / storage layer
- CI / deploy config
- Wiring critic rubric v2 into the live critic agent (gated for a follow-up after calibration)

## Acceptance criteria mapping (spec section 9)

- [x] `docs/brownfield/current_state.md`
- [x] `docs/brownfield/change_impact.md`
- [x] v2 contracts in `internal/contract/v2/`
- [x] `style_guide.yaml` + loader (called from new code; not yet wired into legacy agents — additive only this cycle)
- [x] Critic rubric implements all 10 criteria (deterministic floor; LLM-required criteria flagged)
- [x] Script Writing agent migrated to new prompt template (flag-gated; default off)
- [x] Regression: existing tests pass with flag off
- [x] New tests: rubric scorer, contract roundtrip, adapter, style loader, prompt-template render, writer template-source switch
- [ ] End-to-end SCP-173 ≥80 score — deferred (requires live LLM run; out of scope for a brownfield infra-only cycle)
- [x] PR description with diff summary — generated at commit time

## Risk acknowledgments (spec section 11)

- v1 regression: covered by leaving v1 path as default and testing parity in writer template-source switch.
- LLM JSON non-conformance: not addressed this cycle; the existing per-act retry budget (`writerPerActRetryBudget = 1`) already exists.
- Critic too harsh: rubric v2 is not wired into the live critic agent in this cycle, so it cannot block production output.
- Korean term mapping incomplete: style guide is YAML-extensible.
- Prompt context overrun: template ships with the same `{var}` substitution surface as today; no expansion.
