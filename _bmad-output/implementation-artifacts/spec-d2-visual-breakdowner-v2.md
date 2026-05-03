---
title: 'D2 — visual_breakdowner v2 (consume ActScript / BeatAnchor)'
type: 'feature'
created: '2026-05-04'
status: 'draft'
context:
  - '_bmad-output/planning-artifacts/next-session-monologue-mode-decoupling.md'
  - '_bmad-output/implementation-artifacts/spec-d1-domain-types-and-writer-v2.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** After D1 ships, `visual_breakdowner` still consumes `[]NarrationScene` via the `LegacyScenes()` bridge in `domain.NarrationScript`. The bridge is dead-layer risk justified only by progressive D2–D6 migration; D2 is the first migration that retires its share of the bridge.

**Approach:** Switch `visual_breakdowner` to consume `[]ActScript` directly. Emit `VisualAct { Shots []VisualShot }` per act, where each shot's `narration_anchor` carries the rune-offset slice from its source `BeatAnchor`. One shot per BeatAnchor (1:1). Per-act fan-out preserved (4 acts errgroup-parallel). Once `visual_breakdowner` no longer calls `LegacyScenes()`, mark the bridge with a `// TODO(D4): remove with critic v2` comment.

## Boundaries & Constraints

**Always:**
- One shot per BeatAnchor; shot ordering matches beat ordering; `narration_anchor` preserves rune offsets exactly.
- Per-act fan-out preserved (errgroup, 4 acts).
- Carry cycle-C visual_breakdowner retry policy (commit `2ef1d3c`): `domain.ErrStageFailed` is retryable, ctx cancellation propagates verbatim.
- Image-prompt assembly NOT redesigned (per D plan out-of-scope) — only the anchor mechanism changes.

**Ask First:**
- If 1:1 mapping breaks (a beat that yields zero shots, or a beat the model wants to subdivide into multiple shots), HALT before relaxing the constraint.
- If `BeatAnchor.FactTags` propagation to shot-level requires reshape that researcher/structurer didn't anticipate (per plan P4), HALT.

**Never:**
- No regression to per-scene shot generation.
- No fabrication of beats or rewriting of monologue text from the visual side.
- No removal of the `LegacyScenes()` bridge in this spec — bridge dies in D4 (last consumer).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected | Error Handling |
|---|---|---|---|
| 4 acts × 8–10 beats each | `state.Narration.Acts` v2 | `state.VisualScript.Acts × 4`, each with `Shots` count == its BeatAnchor count, anchors preserved | N/A |
| Provider returns empty content / 5xx | transient upstream | retry per cycle-C policy | retry → escalate `ErrStageFailed` |
| Model returns shot count ≠ beat count | bad LLM output | per-act retry; on exhaustion fail with count mismatch | `ErrValidation` |
| Shot anchor offsets don't match source BeatAnchor offsets | bad LLM output | per-act retry; on exhaustion fail | `ErrValidation` |

</frozen-after-approval>

## Code Map

- `internal/domain/visual.go` (or wherever `VisualScene` lives) -- add `VisualAct { ActID string; Shots []VisualShot }`; flag `VisualScene` `Deprecated` with TODO(D4).
- `internal/pipeline/agents/visual_breakdowner.go` -- consume `[]ActScript`; emit `[]VisualAct`. Remove `LegacyScenes()` call.
- `docs/prompts/scenario/03_5_visual_breakdown.md` -- rewrite for `ActScript` + `BeatAnchor[]` input shape; "given monologue + beat slices, produce one shot per beat".
- `prompts/agents/visual_breakdowner.tmpl` (if separate template exists) -- align with prompt rewrite.
- `testdata/contracts/visual_breakdown.{schema,sample}.json` -- rewrite for v2 shape.
- `internal/pipeline/agents/visual_breakdowner_test.go` -- rewrite end-to-end; preserve cycle-C retry coverage.

## Tasks & Acceptance

**Execution:**
- [ ] `internal/domain/visual.go` -- add `VisualAct`; deprecate `VisualScene` with TODO(D4) marker.
- [ ] `docs/prompts/scenario/03_5_visual_breakdown.md` -- rewrite for v2 input shape.
- [ ] `testdata/contracts/visual_breakdown.{schema,sample}.json` -- v2 rewrite.
- [ ] `internal/pipeline/agents/visual_breakdowner.go` -- consume `[]ActScript`, emit `[]VisualAct`, remove `LegacyScenes()` call.
- [ ] `internal/pipeline/agents/visual_breakdowner_test.go` -- rewrite end-to-end + retry coverage + I/O matrix.
- [ ] Unit-test every row of the I/O matrix.

**Acceptance Criteria:**
- Given clean repo on `feat/monologue-mode-v2` post-D1, when `go build ./...` runs, then it succeeds.
- Given the same repo, when `go test ./...` and `go test -race ./internal/pipeline/agents/...` run, then all tests pass.
- Given an SCP-049 dogfood input post-D1, when `visual_breakdowner` runs, then `len(state.VisualScript.Acts) == 4` and `sum(len(a.Shots)) == sum(len(a.Beats))` across acts (1:1 mapping invariant).
- Given the same dogfood, when `LegacyScenes()` is grep'd in `visual_breakdowner.go`, then it returns no matches.

## Design Notes

`VisualAct.Shots[i].NarrationAnchor` MUST equal `ActScript.Acts[k].Beats[i]` byte-for-byte (rune-offset, mood, location, characters_present, fact_tags). The 1:1 invariant is what lets D3 (TTS) and downstream image-gen run independently — image regeneration must not perturb monologue text or beat anchors.

## Verification

**Commands:**
- `go build ./...`
- `go test ./...` + `go test -race ./internal/pipeline/agents/...`
- SCP-049 phase-A dogfood -- inspect `state.VisualScript` shape + 1:1 invariant.

**Manual checks:**
- `grep -rn "LegacyScenes" internal/pipeline/agents/visual_breakdowner.go` -- expected: zero matches.
