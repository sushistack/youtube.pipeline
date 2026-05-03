# Next session — Visual breakdown ↔ narration alignment

Run with `/bmad-quick-dev`. This cycle assumes the **Structure Narrative Roles** cycle
(spec: `_bmad-output/implementation-artifacts/spec-structure-narrative-roles.md`) has
landed and `narration_beats` is being populated by the writer agent.

This document is self-contained — captures the decisions made on 2026-05-03 plus a
territory map of the visual_breakdown code. The plan step owns the implementation
shape.

---

## Goal

Make every shot's `visual_descriptor` actually describe the moment of narration that
shot sits on top of. Today the script and the imagery drift apart — viewers see the
SCP entity in foreground frames where the narration is talking about an empty room, a
distant site exterior, a researcher's POV, etc.

Two outcomes by end of cycle:

1. **1:1 narration ↔ shot mapping**: shot count is no longer derived from
   `ShotCountForDuration(seconds)`. It equals `len(scene.narration_beats)` — the
   beats produced by the writer in the prior cycle. Each shot consumes exactly one
   beat as its primary content.
2. **Frozen prefix is a constraint, not a verbatim concatenation**: `visual_descriptor`
   must preserve visual-identity continuity across shots, but the agent can shift
   focal subject (entity / environment / character POV / artifact close-up) per beat.
   No more "every shot starts with the same 200-character entity description."

---

## User-confirmed decisions (do not re-litigate)

Decided 2026-05-03 in the structure-narrative-roles planning conversation:

1. **Sequencing:** This cycle runs *after* structure-narrative-roles ships. The prior
   cycle adds the `narration_beats: []string` (or richer struct) field to scene
   metadata but does **not** read it. This cycle is the one that consumes it.
2. **Shot count = beat count:** Replace duration-driven `ShotCountForDuration`
   ([internal/pipeline/agents/visual_breakdown_helpers.go:33-49](internal/pipeline/agents/visual_breakdown_helpers.go#L33-L49))
   with `len(scene.narration_beats)`. Reject scenes where `narration_beats` is empty
   (writer must populate it; it is no longer optional once this cycle ships).
3. **Frozen prefix policy:** Soften from "verbatim prepend" to "preserve visual
   identity in style and continuity." The agent receives the frozen descriptor as
   *context*, not as a string to literally concatenate. `EnsureFrozenPrefix` becomes
   a soft validator (warn-and-fix) instead of a hard prepender.
4. **Per-shot narration anchor:** Each shot in the response must echo back which beat
   it is rendering (`narration_beat_index: int`). Writer provides ordered beats; the
   shot ordering must match.
5. **Backwards compatibility:** None. Both flags from the prior cycle
   (`use_role_based_assignment`, `use_act_specific_pacing`) must be ON for this cycle
   to even compile in the new path. `narration_beats` is mandatory once we land this.
6. **No new LLM calls per scene.** Still one call per scene to the visual breakdowner.

---

## Territory map (where to look — do not prescribe)

- `internal/pipeline/agents/visual_breakdowner.go` — agent runner; replace
  `ShotCountForDuration` invocation, change response shape to include
  `narration_beat_index` per shot, normalize durations across beats (likely uniform
  for now; per-beat-weighted later).
- `internal/pipeline/agents/visual_breakdown_helpers.go` — `ShotCountForDuration` and
  `EnsureFrozenPrefix` both need rework. Keep `BuildFrozenDescriptor` intact (still
  the source of truth for visual identity).
- `docs/prompts/scenario/03_5_visual_breakdown.md` — prompt template. Major rewrite:
  remove "every visual_descriptor must begin with {frozen_descriptor} verbatim" rule,
  replace with "preserve visual identity continuity across shots while letting focal
  subject shift per narration beat."
- `internal/domain/scenario.go` — `VisualShot` may need `NarrationBeatIndex int` and
  `NarrationBeatText string` (denormalized for downstream image-prompt assembly).
- `testdata/contracts/visual_breakdown_output.{schema,sample}.json` — schema bump,
  new sample fixture. Old sample stays as a regression check for the duration-driven
  path if a feature flag is added; otherwise deprecate.
- `internal/pipeline/agents/visual_breakdowner_test.go` — heavy churn; rewrite the
  shot-count tests entirely.

---

## Open questions for the plan step

(Don't answer these in this file — surface them at `/bmad-quick-dev` step-01 so the
plan owns the decision.)

- **Feature flag or hard cutover?** The prior cycle's flags were
  `use_role_based_assignment` / `use_act_specific_pacing`. A third flag
  `use_beat_aligned_shots` seems natural; alternatively, since narration_beats is
  mandatory upstream, this can be a hard cutover keyed on
  `len(scene.narration_beats) > 0`. Plan step decides.
- **What about scenes the writer didn't beat-split?** If the writer for an
  `incident` 100-rune scene only emits 1 beat, the shot is 1. Is that OK or do we
  enforce a minimum 2 beats per scene for visual variety? Recommend 1 — incident
  hooks should be visually monolithic ("one striking image"). Plan step confirms.
- **Per-beat duration weighting:** Today shot durations are uniform within a scene.
  When beats are uneven (one beat = "the scientist screams" / next beat = "silence
  for 8 seconds"), uniform split is wrong. Plan step decides whether to add
  per-beat duration estimation or keep uniform for the first cut.
- **Image-generation downstream consumer:** `5-4-frozen-descriptor-propagation-per-shot-image-generation`
  story file documents how `visual_descriptor` flows into image generation. If we
  weaken the frozen prefix, the image prompt assembly downstream needs a defensive
  prepend so visual identity is still anchored at image-gen time. Plan step verifies.

---

## Acceptance signals (what "done" looks like)

- `go test ./...` clean
- One end-to-end run on SCP-173 (or whatever known-good SCP the project uses for
  smoke) produces a video where:
  - Every shot's image clearly corresponds to one specific narration beat (manual
    spot-check, this is HITL-validated, not automated)
  - Shot 1 of a scene does not look identical to shot 3 of the same scene (visual
    variety within scene)
  - Visual identity (color palette, overall style, entity continuity) still reads
    as one cohesive video — not jarring style breaks
- Critic rubric score ≥80 maintained (the rubric does not check visual alignment
  directly today, but ensure no regression on the criteria it does check)

---

## Out of scope for this cycle (do **not** touch)

- Image generation prompt assembly (`internal/...image...`) — separate concern
- TTS pacing / audio alignment — narration durations may shift with beat splits but
  TTS recomputes from text length, no work needed there
- ComfyUI / dashscope / image client integration — pure prompt-text changes only
- Adding new agents — reuse the existing `visual_breakdowner`
- LLM provider / model — stays on whatever the prior cycle settled
