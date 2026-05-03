---
title: 'Structure Narrative Roles'
type: 'refactor'
created: '2026-05-03'
status: 'in-review'
baseline_commit: 'ebd013881391689d416627b15ab75bb19501c1aa'
context:
  - '{project-root}/_bmad-output/planning-artifacts/next-session-enhance-prompts.md'
  - '{project-root}/_bmad-output/planning-artifacts/next-session-visual-breakdown-alignment.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The structurer assigns dramatic beats to acts by `index % 4`, so the script has no real "hook → tension → reveal → bridge" arc; every act gets the same scene budget and a single 220-rune narration cap, which prevents the climactic act from breathing and forces the hook to ramble.

**Approach:** Replace the round-robin entirely. Researcher labels each beat with a narrative role using one LLM classification call. Classifier prompt mandates at least one beat per role (hook, tension, reveal, bridge). Validation rejects malformed or imbalanced output, retry budget absorbs transient flakes, and a final failure hard-fails the run — there is no silent fallback to broken behavior. Structurer assigns beats to acts by role with no second path. Per-act scene multipliers and per-act narration caps replace the constants. Single code path, no feature flags — Jay is the sole operator and only one behavior should exist.

## Boundaries & Constraints

**Always:**
- Classifier validation rejects: malformed JSON, beat-count mismatch, duplicate indices, unknown role token, or any of the four roles missing from the response. Each rejection consumes one retry; retry budget = 3 (one initial + two retries).
- After retries exhaust, return `domain.ErrStageFailed` so Phase A surfaces a clear failure to the operator. **No silent degradation, no modulo fallback** — the old round-robin code is deleted, not preserved.
- All schema/fixture changes are done in this PR. `SourceVersionV1` bumps from `v1.1-deterministic` to `v1.2-roles`, invalidating all Phase A caches; first run after merge re-derives researcher and structurer outputs.
- Researcher's LLM call piggybacks on the existing writer model/provider config (`cfg.WriterModel`, `cfg.WriterProvider`) so no new defaults or `.env` keys appear.

**Ask First:**
- Any change to `internal/api/handler_run.go`, `internal/service/run_service.go`, or `web/**` (mixed-uncommitted territory — out of scope per Jay's commit-scope policy).
- Any change to the visual_breakdown stage (deferred; spec at `_bmad-output/planning-artifacts/next-session-visual-breakdown-alignment.md`).

**Never:**
- Adding feature flags or env-var toggles for this behavior. One code path.
- Adding new external dependencies.
- Modifying the writer's per-act LLM call count (still one call per act).
- Modifying critic rubric thresholds.
- Touching the visual_breakdown stage.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|---------------|----------------------------|----------------|
| Healthy LLM, valid balanced classification on first try | corpus with ≥4 beats | Act 1 holds beats classified `hook`; Act 2 `tension`; Act 3 `reveal`; Act 4 `bridge`. Synopsis prefixed `[ROLE: <korean>]`. scenesPerBeat = {incident:1, mystery:2, revelation:3, unresolved:1}. runeCap = {incident:100, mystery:220, revelation:320, unresolved:180}. SourceVersion `v1.2-roles`. | n/a |
| First call malformed JSON, retry succeeds | classifier returns garbage then valid output | Retry consumed; final happy-path result. | log retry decision |
| All retries fail (network errors, malformed JSON, missing roles, etc.) | classifier never returns valid balanced output | Researcher returns `domain.ErrStageFailed`. Phase A run fails. Operator sees it in audit log + UI. **No degraded output is emitted.** | bubble error |
| Classifier returns 4 hooks, 0 bridges | imbalanced classification | Validation rejects (missing role); retry budget consumed. | retry then fail |
| Classifier returns duplicate indices or wrong beat count | malformed shape | Validation rejects; retry budget consumed. | retry then fail |
| Writer emits 320-rune narration in revelation act | LLM output | accept | n/a |
| Writer emits 320-rune narration in incident act | LLM output | reject with cap-exceeded; one retry per existing writer budget | retry then fail |
| Corpus too sparse (<4 beats) | researcher input | existing `domain.ErrValidation` triggers before classifier runs (today's behavior, unchanged) | bubble up |
| Run with cached `v1.1-deterministic` resumes | cached Phase A | Phase A cache loader detects mismatch and re-derives. | n/a |

</frozen-after-approval>

## Code Map

- `internal/domain/scenario.go` — add `DramaticBeat.RoleSuggestion` (optional), `Act.Role` (optional), role consts (`RoleHook`/`RoleTension`/`RoleReveal`/`RoleBridge`), Korean-label map, `ActScenesPerBeat`/`ActNarrationRuneCap` maps, bump `SourceVersionV1` to `"v1.2-roles"` and update history comment.
- `internal/pipeline/agents/researcher.go` — `NewResearcher` signature gains `TextGenerator` + role-classifier `TextAgentConfig`. After deterministic beat construction, run classifier with retry budget = 3 (1 attempt + 2 retries). On valid balanced response: populate `RoleSuggestion`. On budget exhaustion: return `domain.ErrStageFailed`. SourceVersion always `v1.2-roles`.
- `docs/prompts/scenario/01_5_role_classifier.md` (NEW) — prompt requires strict JSON `{"classifications":[{"index":N,"role":"hook|tension|reveal|bridge"}]}` and explicitly mandates "every role must appear at least once across the classifications."
- `internal/pipeline/agents/structurer.go` — replace `assignedBeatIDs` with role-based assignment: each beat goes to the act whose role matches its `RoleSuggestion`. Old modulo code is **deleted**. Drop `scenesPerBeat = 2` constant; use `domain.ActScenesPerBeat[actID]`. Synopsis and `KeyPoints` carry `[ROLE: <korean>] ` prefix.
- `internal/pipeline/agents/writer.go` — replace constant `narrationRuneCap = 220` with lookup `domain.ActNarrationRuneCap[spec.Act.ID]` inside `validateWriterActResponse`. Comment block updated.
- `cmd/pipeline/serve.go` — wire role-classifier `TextAgentConfig` (model = `cfg.WriterModel`, provider = `cfg.WriterProvider`, modest max_tokens, concurrency 1) into `NewResearcher`.
- `testdata/contracts/researcher_output.schema.json` — add optional `role_suggestion` (enum `["hook","tension","reveal","bridge",""]`); `source_version` const → `"v1.2-roles"`.
- `testdata/contracts/structurer_output.schema.json` — add optional `role` field on Act items; `source_version` const → `"v1.2-roles"`.
- `testdata/contracts/structurer_output.sample.json` — regenerate from SCP-TEST corpus with the new code.
- `testdata/contracts/researcher_output.sample.json` — regenerate.

## Tasks & Acceptance

**Execution:**
- [x] `internal/domain/scenario.go` — add role consts + Korean labels + `ActScenesPerBeat` + `ActNarrationRuneCap`; extend `DramaticBeat` and `Act` with optional fields; bump `SourceVersionV1` to `"v1.2-roles"` with comment history line.
- [x] `docs/prompts/scenario/01_5_role_classifier.md` — write classifier prompt: explain four roles in plain Korean, list beats with index/source/description, demand strict JSON shape, hard-cap output.
- [x] `internal/pipeline/agents/researcher.go` — extend signature; build beats deterministically as today; call classifier with retry budget 3; validate returned roles (count == beat count, indices unique, every value in role enum, all four roles appear ≥ once); on budget exhaustion return `domain.ErrStageFailed` with the underlying validation reason in the message.
- [x] `internal/pipeline/agents/researcher_test.go` — stub TextGenerator covering: happy path on first try → roles populated; happy path after one bad attempt → still succeeds; malformed JSON across all 3 attempts → `ErrStageFailed`; missing role across all 3 attempts → `ErrStageFailed`; duplicate indices → `ErrStageFailed`; index/count mismatch → `ErrStageFailed`; LLM transport error → `ErrStageFailed`.
- [x] `internal/pipeline/agents/structurer.go` — implement role-based assignment; **delete** modulo code (`assignedBeatIDs`) and `scenesPerBeat = 2` constant; per-act multipliers from domain map; synopsis + key_points get `[ROLE: <korean>] ` prefix; `Act.Role` populated.
- [x] `internal/pipeline/agents/structurer_test.go` — happy path: all four roles → exact assignment, budgets reflect per-act multipliers; multiple beats per role → all flow into correct act preserving original beat index order; missing `RoleSuggestion` on any beat → return `ErrValidation` (researcher contract violation, should never happen post-classifier); existing fixture-comparison test compares against regenerated sample file.
- [x] `internal/pipeline/agents/writer.go` — `validateWriterActResponse` looks up cap from `domain.ActNarrationRuneCap[spec.Act.ID]`; update comment near old `narrationRuneCap` block to point at the map.
- [x] `internal/pipeline/agents/writer_test.go` — assert: revelation 320-rune passes, 321 fails; incident 100-rune passes, 101 fails; mystery 220 passes (unchanged); unresolved 180 passes, 181 fails.
- [x] `cmd/pipeline/serve.go` — wire role-classifier TextAgentConfig into NewResearcher.
- [x] `testdata/contracts/researcher_output.schema.json` + `structurer_output.schema.json` — schema edits for new optional fields + source_version const bump.
- [x] `testdata/contracts/researcher_output.sample.json` + `structurer_output.sample.json` — regenerate from SCP-TEST corpus and commit.

**Acceptance Criteria:**
- Given a healthy classifier returning all four roles correctly, when SCP-TEST is processed, then `acts[0].role == "hook"` and contains only beats whose `RoleSuggestion == "hook"`; same for tension/reveal/bridge; synopses begin with `[ROLE: ...]`; act scene budgets reflect per-act multipliers.
- Given a classifier that returns malformed or imbalanced output across all 3 attempts, when SCP-TEST is processed, then the researcher returns `domain.ErrStageFailed`, the run is marked failed in Phase A, and **no degraded structurer output is emitted**.
- Given a writer act emits a 320-rune narration in revelation, when validation runs, then it passes; given the same narration in incident, when validation runs, then it rejects with a cap-exceeded error.
- Given a Phase A cache entry tagged `v1.1-deterministic`, when a run resumes after this PR merges, then the cache is invalidated and researcher/structurer re-derive.
- Given `go test ./...` runs after the change, then the entire suite passes.
- Given `grep -rn "scenesPerBeat\|assignedBeatIDs" internal/` runs, then it returns no matches (old code is fully deleted, not merely unreached).

## Spec Change Log

## Design Notes

**Role-classifier inlined into researcher.** Adding a separate "RoleLabeler" agent would require a new pipeline stage, new state field, new resume point, new HITL handling. Inlining the call inside `NewResearcher` keeps the pipeline graph identical to today — researcher just emits richer beats. Single LLM call per run is acceptable cost.

**Korean role labels feed prompts directly.** `hook → 흥미로운 상황`, `tension → 급박한 상황`, `reveal → SCP 설명`, `bridge → 부연 / 다른 SCP와의 관계`. Stored as a `map[string]string` in `scenario.go` so structurer synopsis and (later) writer prompts pull from one source. English identifiers stay in the data model (matches existing `ActIncident` style).

**No fallback, fail-fast on classifier failure.** Falling back to modulo round-robin would deliver exactly the broken arc this refactor exists to fix. So the policy is: classifier output is either valid + balanced (every role assigned ≥1 beat) or it is rejected, retried, and ultimately fails the run. The operator decides whether to re-trigger. This matches Jay's "rigorous pipeline / no half-implementations" policy.

**Per-act scene multiplier rationale.** Incident=1 (cold open is one striking moment, not a montage); mystery=2 (information drip needs spread); revelation=3 (multi-step reveal is the climax — needs room); unresolved=1 (clean cliffhanger). Total stays close to today's 8 scenes for a 4-beat corpus.

**Per-act runeCap rationale.** Incident=100 enforces the ≤15s cold-open rule already in the writer prompt. Mystery=220 unchanged. Revelation=320 lets the climax breathe (longer, more sensory). Unresolved=180 enforces brevity for the bridge. Caps are per scene, not per act.

## Verification

**Commands:**
- `go test ./internal/domain/...`
- `go test ./internal/pipeline/agents/...`
- `go test ./...`
- `go build ./...`

**Manual checks:**
- Run one Phase A on SCP-173 and inspect the audit log: researcher must show one extra `text_generation` event labeled `role_classifier`; the resulting structure JSON must have `role` populated on every act.
- Inspect regenerated `testdata/contracts/structurer_output.sample.json`: `acts[0].dramatic_beat_ids` should map to incident-flavored beats from the SCP-TEST fixture (sentinel motion, shadow lingering — not random modulo picks).
