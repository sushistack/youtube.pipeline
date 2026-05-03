# Next session — Visual breakdown stabilization + narration alignment

Run with `/bmad-quick-dev`. This cycle has **two independent goals**:

1. **(Blocking) Stabilize the `visual_breakdowner` stage** — currently fails on
   transient LLM omissions (e.g. `invalid transition ""`), zero retry budget,
   blocking every Phase A dogfood after the writer succeeds.
2. **(Strategic) Align shot-level imagery with narration semantics** — original
   intent of this cycle. Replace `ShotCountForDuration` with beat-aligned shot
   mapping so each shot's `visual_descriptor` describes the moment of narration
   it sits on top of, not a uniform time-sliced fragment.

Plan step decides whether to ship both goals together or split. Goal 1 alone is
~30 min of work and unblocks current dogfood; Goal 2 is a larger refactor.
Recommendation: **single-spec for both** since they touch the same files and
goal 2's prompt rewrite is the natural place to harden the contract that
goal 1 patches.

---

## Current state on main (read this first)

These cycles already shipped — your starting state:

- **`8488d19` v1.2-roles** — role-based act assignment + per-act narration caps.
  - Hard cutover, no flags. `use_role_based_assignment` / `use_act_specific_pacing`
    flags **were never built** — referenced in old handoff drafts but rejected
    in plan step (memory: `feedback_no_dead_layers`).
  - `domain.SourceVersionV1 = "v1.2-roles"`.
- **`4e924e9` fewshot exemplar injection** — writer prompt gets per-act exemplar
  inject from `docs/exemplars/scp-049.exemplar.md` + `scp-2790.exemplar.md`.
- **`388bb03` writer_debug.json dump** — TEMPORARY patch in `phase_a.go`. Writes
  `{outputDir}/{runID}/writer_debug.json` after writer succeeds so fewshot
  effect can be inspected when visual_breakdowner blocks. **This cycle removes
  it** (see "Cleanup tasks" below).

**Important: `narration_beats` was NOT shipped.** The original handoff assumed
the structure-narrative-roles cycle would add `narration_beats` to writer
output. It didn't — that work was descoped to keep that cycle's blast radius
tight. So beat-aligned shot mapping needs to either:
- (a) add `narration_beats: []string` to `domain.NarrationScene` as part of
  this cycle (writer change + structurer/critic schema bump), OR
- (b) split each scene's `narration` string at sentence boundaries inside the
  visual_breakdowner agent (no writer change, but heuristic).

Plan step picks. (a) is cleaner long-term; (b) is faster to ship.

---

## Goal 1: stabilize visual_breakdowner (blocking — fix first)

### Symptom (live)

Every Phase A dogfood since v1.2-roles shipped fails at:
```
visual breakdowner: scene N shot 1 invalid transition "": validation error
```
- Writer succeeds (writer_debug.json proves it).
- visual_breakdowner LLM call returns shots with `transition` field omitted
  or empty in ~1 of 16 scenes per run.
- Stage has **zero retry budget** so a single drop kills the run.

### Root causes

1. **Zero per-scene retry budget.** Unlike writer (`writerPerActRetryBudget=1`),
   `visual_breakdowner.go` is single-shot per scene. Mirror writer's pattern.
   See `internal/pipeline/agents/visual_breakdowner.go:103-111`.
2. **`transition` is enum-strict but prompt-soft.** Allowed values
   (`ken_burns`/`cross_dissolve`/`hard_cut`) appear once at
   `docs/prompts/scenario/03_5_visual_breakdown.md:53` and once in the example
   block at `:43`. LLM frequently omits the field on Korean prompts.
3. **No negative test.** All `visual_breakdowner_test.go` cases inject a valid
   `Transition` value. Add a test where the LLM returns a shot with empty
   transition — the retry must trigger and a fallback (or retry-with-correction)
   must produce a valid value.

### Hardening options (pick during plan step)

- **(a) Retry on enum violation** — like writer's schema retry. Simple.
- **(b) Decode-time fallback to `ken_burns`** — silent default. Risky:
  every scene that drops transition silently becomes ken_burns, which is the
  most boring transition. Probably bad.
- **(c) Prompt tightening** — make the field non-skippable. Already in scope
  if Goal 2 rewrites the prompt. ~free.
- **Recommended: (a) + (c)** — retry catches the rest after prompt tightens.

---

## Goal 2: narration ↔ shot alignment (strategic refactor)

### Outcome target

1. **1:1 narration ↔ shot mapping**: shot count is no longer derived from
   `ShotCountForDuration(seconds)`. It equals the number of beats in the scene
   (per (a) or (b) decision above). Each shot consumes exactly one beat as its
   primary content.
2. **Frozen prefix is a constraint, not a verbatim concatenation**:
   `visual_descriptor` must preserve visual-identity continuity across shots,
   but the agent can shift focal subject (entity / environment / character POV
   / artifact close-up) per beat. No more "every shot starts with the same
   200-character entity description."

### Decisions locked from prior planning

- **No new LLM calls per scene.** Still one call per scene to the visual
  breakdowner.
- **Per-shot narration anchor**: each shot in the response must echo back which
  beat/sentence it is rendering (`narration_beat_index: int`). Shot ordering
  must match beat ordering.
- **Soft frozen prefix**: `EnsureFrozenPrefix` becomes a soft validator
  (warn-and-fix) instead of a hard prepender. `BuildFrozenDescriptor` stays
  intact (still the source of truth for visual identity).

---

## Territory map (where to look — do not prescribe)

- `internal/pipeline/agents/visual_breakdowner.go` — agent runner. Goal 1: add
  retry budget. Goal 2: replace `ShotCountForDuration` invocation, change
  response shape to include `narration_beat_index` per shot, normalize
  durations across beats.
- `internal/pipeline/agents/visual_breakdown_helpers.go` —
  `ShotCountForDuration` and `EnsureFrozenPrefix` both need rework. Keep
  `BuildFrozenDescriptor` intact.
- `docs/prompts/scenario/03_5_visual_breakdown.md` — prompt template. Major
  rewrite: tighten transition contract (Goal 1), remove "every
  visual_descriptor must begin with {frozen_descriptor} verbatim" rule and
  replace with "preserve visual identity continuity across shots while letting
  focal subject shift per beat" (Goal 2).
- `internal/domain/scenario.go` — `VisualShot` may need `NarrationBeatIndex int`
  and `NarrationBeatText string` (denormalized for downstream image-prompt
  assembly). If decision (a): also add `NarrationScene.NarrationBeats []string`.
- `testdata/contracts/visual_breakdown_output.{schema,sample}.json` — schema
  bump, new sample fixture. Old sample is OK to deprecate (no flag fallback).
- `internal/pipeline/agents/visual_breakdowner_test.go` — heavy churn. Rewrite
  shot-count tests entirely + add Goal 1's negative-transition test.
- `internal/pipeline/agents/writer.go` (only if decision (a)): writer must emit
  narration_beats on every scene. Touch is small (split narration on `. ` /
  sentence-end markers, or add a writer-side beat extractor).
- `testdata/contracts/writer_output.{schema,sample}.json` (only if decision
  (a)): bump schema for `narration_beats` field.

---

## Cleanup tasks (must include in spec)

- **Remove the writer_debug.json patch** added in `388bb03`. The lines to delete
  are in `internal/pipeline/phase_a.go`:
  - `writeWriterDebug` function (added at end of file)
  - The `if entry.ps == agents.StageWriter && state.Narration != nil { ... }`
    block in the run loop after `writeCache`.
  - Reasoning: with visual_breakdowner stabilized, scenario.json is reliably
    written end-to-end and the debug dump is dead weight.

---

## Open questions for the plan step

(Don't answer these in this file — surface them at `/bmad-quick-dev` step-01 so
the plan owns the decision.)

- **(a) writer adds `narration_beats` field, or (b) visual_breakdowner splits
  narration internally?** See "Current state" above.
- **What about scenes the writer didn't beat-split?** If the writer for an
  `incident` 100-rune scene only emits 1 beat, shot count = 1. Is that OK or
  enforce minimum 2 beats per scene for visual variety? Recommend 1 — incident
  hooks should be visually monolithic ("one striking image"). Plan confirms.
- **Per-beat duration weighting:** Today shot durations are uniform within a
  scene. When beats are uneven (one beat = "the scientist screams" / next beat
  = "silence for 8 seconds"), uniform split is wrong. Plan decides whether to
  add per-beat duration estimation or keep uniform for the first cut.
- **Image-generation downstream consumer:** Story file
  `_bmad-output/implementation-artifacts/5-4-frozen-descriptor-propagation-per-shot-image-generation.md`
  documents how `visual_descriptor` flows into image generation. If we weaken
  the frozen prefix, the image prompt assembly downstream needs a defensive
  prepend so visual identity is still anchored at image-gen time. Plan
  verifies.

---

## Acceptance signals (what "done" looks like)

- `go test ./...` clean (modulo the 3 pre-existing api/service settings
  failures that are out-of-scope per the structure-narrative-roles spec's
  Ask-First boundary on `internal/api/`, `internal/service/`).
- **Goal 1**: SCP-049 dogfood resume completes the visual_breakdowner stage on
  the first try. Or — at most — one retry per scene observed in logs.
  scenario.json is written end-to-end.
- **Goal 2 (HITL)**: One end-to-end run on SCP-049 produces a video where:
  - Every shot's image clearly corresponds to one specific narration beat
    (manual spot-check, this is HITL-validated, not automated).
  - Shot 1 of a scene does not look identical to shot 3 of the same scene
    (visual variety within scene).
  - Visual identity (color palette, overall style, entity continuity) still
    reads as one cohesive video — not jarring style breaks.
- Critic rubric score ≥80 maintained (the rubric does not check visual
  alignment directly today, but ensure no regression on criteria it does
  check).
- Cleanup verified: `grep "writer_debug" internal/` returns no matches.

---

## Out of scope for this cycle (do **not** touch)

- Image generation prompt assembly (`internal/...image...`) — separate concern.
- TTS pacing / audio alignment — narration durations may shift with beat
  splits, but TTS recomputes from text length; no work needed there.
- ComfyUI / dashscope / image client integration — pure prompt-text changes
  only.
- Adding new agents — reuse the existing `visual_breakdowner`.
- LLM provider / model — stays on whatever the prior cycle settled.
- **Critic prompt** — fewshot critic exemplar + 어미 패턴 검증은 별도 cycle
  (queued in deferred work as Phase D after fewshot effect verification).
- **Adding more fewshot exemplars** — Phase 2 (queued; trigger after this
  cycle's HITL eval shows whether single-channel imitation worked or
  multi-channel is needed).

---

## Context: fewshot dogfood result (informational, do not act on)

The 2026-05-03 fewshot dogfood (writer_debug.json from SCP-049 run) showed
**mixed but positive** results vs the pre-fewshot baseline:

- ✅ 2nd-person immersion frequency ↑↑ (1× → 4-5×)
- ✅ Quoted dialogue ↑↑ (0 → 3 instances)
- ✅ Lore richness ↑ (lavender + medieval French + reanimation all covered)
- ⚠️ Cold-open hook slightly less cinematic
- ❌ Korean narrator signature endings (`~한답니다`/`~인데요`/`~죠`) NOT learned
  — writer output reverted to standard `~ㅂ니다` documentary tone

The ❌ above is queued for Phase D (critic prompt 어미-pattern check), NOT this
cycle. **Do not change writer prompt or fewshot exemplars in this cycle** —
contains the change to visual_breakdown only.
