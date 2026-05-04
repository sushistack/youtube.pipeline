---
title: 'D2 — visual_breakdowner v2 (consume ActScript / BeatAnchor)'
type: 'feature'
created: '2026-05-04'
status: 'done'
baseline_commit: '5003a3b9c36dfd42d2cd480265dd500093785ea7'
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

- `internal/domain/visual_breakdown.go` -- rename `VisualBreakdownOutput` → `VisualScript`; replace `Scenes []VisualBreakdownScene` with `Acts []VisualAct`; add `VisualAct { ActID string; Shots []VisualShot }`; redesign `VisualShot` so `NarrationAnchor domain.BeatAnchor` replaces `NarrationBeatIndex` + `NarrationBeatText`; flag `VisualBreakdownScene` `Deprecated` with `// TODO(D-vis-final)`; add `(*VisualScript).LegacyScenes(narration *NarrationScript) []VisualBreakdownScene` bridge; bump `SourceVersion` to `"v2-visual"`. `ShotOverrides map[int]ShotOverride` retained — key shifts to "global 1-indexed shot number across flattened `Acts[*].Shots[*]`" (image-prompt redesign OOS per frozen rule).
- `internal/pipeline/agents/agent.go` -- rename state field `VisualBreakdown *VisualBreakdownOutput` → `VisualScript *VisualScript`, json tag `visual_script`.
- `internal/pipeline/agents/visual_breakdowner.go` -- consume `state.Narration.Acts` directly; per-act errgroup fan-out (4 parallel, replaces v1 per-scene fan-out); emit `[]VisualAct` with one shot per `BeatAnchor`; remove `state.Narration.LegacyScenes()` call; preserve cycle-C retry policy per act.
- `docs/prompts/scenario/03_5_visual_breakdown.md` -- rewrite: input is one act's monologue + ordered BeatAnchor slices; output is one shot per beat with anchor metadata preserved verbatim.
- `prompts/agents/visual_breakdowner.tmpl` -- mirror rewrite; placeholder set updated for v2 (act-level monologue + beat-list).
- `internal/pipeline/agents/assets.go` -- update `VisualBreakdownTemplate` token list / `prompt_lint_test.go` to match v2 placeholders.
- `testdata/contracts/visual_breakdown.{schema,sample}.json` -- v2 rewrite: top-level `acts` (len 4), each `{act_id, shots[]}`, each shot carries `narration_anchor` with full BeatAnchor fields.
- `internal/pipeline/agents/visual_breakdowner_test.go` -- full rewrite: per-act fan-out, retry coverage (transport / JSON-decode / schema / wrong-shot-count / anchor-mismatch), I/O matrix row-by-row, reintroduce v1's `t.Skip`'d `ShotCountMatchesNarrationBeats` + `PassesDescriptorsThroughVerbatim` against v2 shape.
- Downstream visual readers migrated to `state.VisualScript.LegacyScenes(state.Narration)` (D1 pattern) plus state-field rename:
  - `internal/pipeline/image_track.go`, `internal/pipeline/tts_track.go`, `internal/pipeline/resume.go`
  - `internal/service/scene_service.go`
  - `internal/contract/v2/adapter.go`
  - `internal/critic/rubricv2/scorer.go`
  - any other site referencing `state.VisualBreakdown` or `VisualBreakdownOutput`.

## Tasks & Acceptance

**Execution:**
- [x] `internal/domain/visual_breakdown.go` -- rename type, add `VisualAct`, redesign `VisualShot`, add `LegacyScenes(narration)` bridge, bump source_version, deprecate `VisualBreakdownScene`.
- [x] `internal/pipeline/agents/agent.go` -- rename state field + json tag.
- [x] `docs/prompts/scenario/03_5_visual_breakdown.md` + `prompts/agents/visual_breakdowner.tmpl` + `internal/pipeline/agents/assets.go` -- v2 prompt rewrite + placeholder alignment.
- [x] `testdata/contracts/visual_breakdown.{schema,sample}.json` -- v2 rewrite per shape above.
- [x] `internal/pipeline/agents/visual_breakdowner.go` -- consume `[]ActScript`, per-act fan-out, emit `[]VisualAct`, remove `Narration.LegacyScenes()` call, anchor-equality validator.
- [x] `internal/pipeline/agents/visual_breakdowner_test.go` -- full rewrite + retry + I/O matrix + reintroduce skipped tests.
- [x] Downstream visual readers -- migrate to `state.VisualScript.LegacyScenes(state.Narration)` and adapt to renamed state field.
- [x] Unit-test every row of the I/O matrix.

**Acceptance Criteria:**
- Given clean repo on `feat/monologue-mode-v2` post-D1, when `go build ./...` runs, then it succeeds.
- Given the same repo, when `go test ./...` and `go test -race ./internal/pipeline/agents/...` run, then all tests pass.
- Given SCP-049 phase-A dogfood post-D1, when `visual_breakdowner` runs, then `len(state.VisualScript.Acts) == 4`, for every act `len(act.Shots) == len(narration.Acts[k].Beats)` (1:1 invariant), and every `Shots[i].NarrationAnchor` equals the source `BeatAnchor` byte-for-byte.
- Given the same dogfood, when `grep -rn "LegacyScenes" internal/pipeline/agents/visual_breakdowner.go` runs, then it returns zero matches.
- Given a serialized state JSON, when inspected, then top-level visual key is `visual_script` (renamed from `visual_breakdown`) with `source_version: "v2-visual"`.

### Review Findings

(2026-05-04 — bmad-code-review on commit `95a1e7f` vs baseline `5003a3b`; 3 reviewers: Blind Hunter / Edge Case Hunter / Acceptance Auditor.)

**Patch (8 fixed, 1 reclassified to defer):**
- [x] [Review][Patch] `character_service.GetDescriptorPrefill` reads stale `json:"visual_breakdown"` key while producer writes `visual_script` — production breakage [internal/service/character_service.go:234-248]
- [x] [Review][Patch] `NarrationScript.LegacyScenes()` missing the `// TODO(D4): remove with critic v2` marker required by frozen Intent paragraph 2 [internal/domain/narration.go:101]
- [x] [Review][Patch] `reviewer.go` marshals raw v2 `VisualScript` JSON into `{visual_descriptions}` instead of bridging via `LegacyScenes(narration)` per Code Map prescription [internal/pipeline/agents/reviewer.go:131-138]
- [x] [Review][Patch] Anchor offset preflight + remove silent clamp — negative / inverted / out-of-range / zero-length offsets pass anchor-equality validator and persist verbatim into scenario.json [internal/pipeline/agents/visual_breakdowner.go:298-365, 397-432]
- [x] [Review][Patch] `(*VisualScript).LegacyScenes(narration)` silently emits empty Narration when narration is nil / has ActID drift / has count mismatch [internal/domain/visual_breakdown.go:116-180]
- [x] [Review][Patch] Prompt fact_tags ordering + object-format clarity — LLM may reorder or emit string-array shape, causing flaky retries [docs/prompts/scenario/03_5_visual_breakdown.md, prompts/agents/visual_breakdowner.tmpl]
- [x] [Review][Patch→Defer] `loadVisualBreakdownerTemplate` wraps asset-load failure with `domain.ErrValidation` — reclassified to defer because the same pattern is shared by ALL four asset loaders (`readAsset`, `loadWriterTemplate`, `loadSegmenterTemplate`, `loadVisualBreakdownerTemplate`); fixing only the D2 one introduces inconsistency. Tracked in `deferred-work.md` for systemic cleanup [internal/pipeline/agents/assets.go]
- [x] [Review][Patch] `prompt_lint_test.go` covers `docs/prompts/scenario/03_5_visual_breakdown.md` only — `prompts/agents/visual_breakdowner.tmpl` placeholder set unchecked, drift = silent runtime bug under `useTemplatePrompts=true` [internal/pipeline/agents/prompt_lint_test.go]
- [x] [Review][Patch] `buildVisualAct` retains unused `_ string` (frozen descriptor) + `_ *slog.Logger` parameters — code rot [internal/pipeline/agents/visual_breakdowner.go:385-391]

**Defer (9, pre-existing or out-of-scope):**
- [x] [Review][Defer] Schema `narration_anchor.fact_tags` allows empty `key`/`content` strings — pre-existing schema gap, not D2-introduced
- [x] [Review][Defer] Heuristic estimator falls back to 4.0s for Korean per-beat slices (`strings.Fields` returns 1 element on Korean text) — D-tts territory; pre-existing v1 behavior on short scenes
- [x] [Review][Defer] Schema `acts.minItems == maxItems == 4` hard-pins the act count — D1 plan choice (P2: 4 acts)
- [x] [Review][Defer] `ShotOverrides` semantic shift to global 1-indexed shot number not enforced (no v2 producer writes overrides yet) — design notes acknowledge; D-vis-final HITL UI scope
- [x] [Review][Defer] Bridge `EstimatedTTSDurationS = shot.EstimatedDurationS` semantic-mismatch with v1 per-scene total — D3 (TTS) territory
- [x] [Review][Defer] `LegacyScenes` invoked multiple times per run in `image_track` (perf, not correctness)
- [x] [Review][Defer] `format_guide` placeholder dropped from visual_breakdowner prompt — intentional in diff
- [x] [Review][Defer] `sampleVisualBreakdownForReview` test-fixture coupling cliff (future-rot, not a current bug)
- [x] [Review][Defer] `buildCharacterMap` symmetric SceneNum mismatch undetectable on cross-version state corruption — defended by 1:1 invariant + anchor-equality validator

**Dismissed (8 noise / by-design):** bridge ShotIndex=1 + NarrationBeatIndex=0 (correct under 1:1 mapping); bridge omits ShotOverrides (by design — wrapper field, not bridge return); `parseActPromptID` backtick fragility (test-only); LLM `shot_index` discarded by agent (defensive override safe under anchor-equality); FactTag DeepEqual latent regression (speculative — both fields are strings today); `cfg.Concurrency=1` serializes (operator knob, default=4 holds); `strings.Replacer` re-substitution (Go contract — single-pass); `output.Acts` aliases `results` slice (Go convention, doc-only contract).

## Spec Change Log

<!-- Append-only. Empty until the first bad_spec loopback. -->

### 2026-05-04 — D2 implementation deltas (additive scope)

The Code Map enumerated the visual_breakdowner agent + bridge surface; the
broader `state.VisualBreakdown → state.VisualScript` rename forced touching
several call sites + test fixtures that the Code Map did not enumerate but
that `go build ./...` / `go test ./...` acceptance forces. All additive edits
are mechanical renames or v1→v2 fixture re-shapes — no new logic introduced
outside the agent + bridge:

- **State-field rename touchpoints (production):** `internal/pipeline/agents/{reviewer,critic,critic_precheck}.go`,
  `internal/pipeline/{phase_a,finalize_phase_a,phase_c_metadata}.go` —
  every `state.VisualBreakdown` rewritten to `state.VisualScript`.
- **`internal/pipeline/image_track.go`:** loop now reads
  `state.VisualScript.LegacyScenes(state.Narration)` (in-memory bridge);
  `buildCharacterMap` + `anyCharacterScene` re-routed through the bridge.
  This is the only non-rename production change — bridges v1 image-prompt
  consumers to v2 state without redesigning image-prompt assembly (frozen).
- **`prompts/agents/visual_breakdowner.tmpl` added + `prompts.AgentVisualBreakdowner`
  registered**; `assets.go` gains a `loadVisualBreakdownerTemplate` mirror of
  the existing writer/segmenter loader pair.
- **`LegacyShotV1` introduced** in `domain/visual_breakdown.go` as the v1
  per-shot return-element of `(*VisualScript).LegacyScenes()`. v1 callers
  read `NarrationBeatIndex`/`NarrationBeatText` via this struct; the v2
  `VisualShot` carries `NarrationAnchor` only. Both deprecated with
  `// TODO(D-vis-final): remove with last visual consumer migration`.
- **Test fixtures rebuilt to v2 shape:** `internal/pipeline/{phase_a_test,
  phase_a_integration_test,finalize_phase_a_test,phase_c_metadata_test,
  image_track_test}.go`, `internal/pipeline/agents/{agent_test,reviewer_test,
  critic_test}.go`, plus the full rewrite of `visual_breakdowner_test.go`.
  Two image_track tests previously asserting v1 multi-shot-per-scene
  (`TestImageTrack_WritesImagesToSceneShotDirectories`,
  `TestImageTrack_SegmentsShotsRemainAlignedWithScenarioShotOrder`)
  re-spec'd to v2's 1:1 invariant — same canonical-path / order contracts,
  expressed against bridged 1-shot-per-scene output.

**Pre-existing failures left as-is** (per D1 spec change log; reproduced
on the D2 baseline `5003a3b` before any D2 edits):
`TestSettingsHandler_PutReturns409WhenIfMatchStale`,
`TestSettingsService_SaveWritesEffectiveAndDisk`,
`TestSettingsService_SaveRejectsIfMatchMismatch`,
`TestSceneHandler_ListReviewItems_ReturnsPayload`. Out of D2 scope.

### 2026-05-04 — D2 review/fix cycle (8 patches applied, 1 deferred)

bmad-code-review on commit `95a1e7f` (D2 baseline) ran three parallel
adversarial layers (Blind Hunter / Edge Case Hunter / Acceptance Auditor)
and surfaced 9 patch findings. Eight applied; one reclassified to defer.
All fixes are in-scope for D2 and were applied as a single follow-up
commit on top of `95a1e7f`:

- **P0** (production-blocking / frozen-block instruction):
  - `internal/service/character_service.go` — json tag `visual_breakdown` →
    `visual_script` (+ test fixture); descriptor-prefill API was 404'ing
    silently against post-D2 scenario.json.
  - `internal/domain/narration.go:102` — added the `// TODO(D4): remove
    with critic v2` marker required by frozen Intent paragraph 2.
  - `internal/pipeline/agents/reviewer.go` — replaced raw v2 marshal of
    `state.VisualScript` with `state.VisualScript.LegacyScenes(state.Narration)`
    bridge per Code Map prescription.
- **P1** (correctness — silent corruption surface):
  - `internal/pipeline/agents/visual_breakdowner.go` — added per-beat
    anchor offset preflight (rejects negative offsets / EndOffset >
    monologue rune count / StartOffset >= EndOffset before LLM fan-out).
    Removed the silent clamp in `buildVisualAct`; out-of-bounds slicing
    now panics so bridge/state corruption surfaces immediately.
  - `internal/domain/visual_breakdown.go` — `(*VisualScript).LegacyScenes`
    now panics on nil narration or missing-ActID drift; the in-memory
    bridge still defensively clamps offsets for legacy on-disk reads.
- **P2** (prompt robustness):
  - `docs/prompts/scenario/03_5_visual_breakdown.md` +
    `prompts/agents/visual_breakdowner.tmpl` — explicit fact_tags
    object-array shape + order-preservation rules; `characters_present`
    order rule mirrored.
- **P3** (hygiene):
  - `internal/pipeline/agents/visual_breakdowner.go` — `buildVisualAct`
    signature trimmed to `(act, decoded, estimator)` (dropped unused
    frozen-string + slog.Logger params).
  - `internal/pipeline/agents/prompt_lint_test.go` — new
    `TestPromptPlaceholders_DocsAndEmbeddedMirrorsAgree` enforces
    docs/prompts vs prompts/agents/*.tmpl placeholder parity for
    writer / segmenter / visual_breakdowner.
- **Deferred:** `loadVisualBreakdownerTemplate` ErrValidation wrap is the
  same systemic pattern across all four asset loaders (`readAsset`,
  `loadWriterTemplate`, `loadSegmenterTemplate`,
  `loadVisualBreakdownerTemplate`) — fixing only the D2 one would
  introduce inconsistency. Logged in `deferred-work.md` for systemic
  cleanup.

## Design Notes

**Decision log (resolved during step-02 ambiguity-clearance, 2026-05-04):**

1. **Wrapper rename.** `VisualBreakdownOutput` → `VisualScript` (state field + json tag `visual_script` + `source_version="v2-visual"`). Mirrors D1's `Narration*` naming. `FrozenDescriptor`/`Metadata`/`SourceVersion`/`ShotOverrides` carried; only `Scenes` is structurally replaced by `Acts`.
2. **`VisualShot.NarrationAnchor` shape.** Full `domain.BeatAnchor` value, no derived text field. The byte-for-byte invariant in the frozen block enumerates every BeatAnchor field; storing the same struct value is the only honest implementation. Text is derivable from `narration.Acts[k].Monologue[anchor.StartOffset:anchor.EndOffset]` and the bridge performs that slice for v1 readers.
3. **`ShotOverrides` keying.** Type retained as `map[int]ShotOverride`; semantic key shifts from v1 `scene_num` to "global 1-indexed shot number across flattened `Acts[*].Shots[*]`" — the same number the bridge emits as `VisualBreakdownScene.SceneNum`. This is the minimum-touch rekey compatible with the frozen "image-prompt assembly NOT redesigned" rule; HITL override writers continue to address by globally-numbered shot.
4. **Bridge.** `(*VisualScript).LegacyScenes(narration *NarrationScript) []VisualBreakdownScene` mirrors D1's pattern: flattens `(act, shot)` to global 1-indexed `SceneNum`, rune-slices `narration.Acts[k].Monologue` for `Narration`, sets `ShotCount=1`, wraps the single v2 `VisualShot` reformatted to v1 `{NarrationBeatIndex, NarrationBeatText}` for back-compat. Marked `Deprecated:` with `// TODO(D-vis-final): remove with last visual consumer migration`. Lives only until D3–D6 retire the last consumer; same dead-layer-risk justification as D1's `NarrationScript.LegacyScenes()`.

**Per-act vs per-scene fan-out.** v1 ran ~32 per-scene goroutines, one prompt per scene. v2 runs 4 per-act goroutines, one prompt per act (act monologue + 8–10 beats → 8–10 shots). Per-act context improves shot coherence; provider concurrency drops from ~32 to 4 (rate-friendlier).

**Anchor-equality validator.** Per-act validator re-checks `output.Shots[i].NarrationAnchor == act.Beats[i]` field-by-field after every LLM response. Mismatch is a retryable `ErrValidation` (per cycle-C retry policy). This is what makes the byte-for-byte invariant load-bearing for D3 (TTS) and image regeneration independence — image regen must never perturb monologue text or beat anchors.

## Verification

**Commands:**
- `go build ./...`
- `go test ./...` + `go test -race ./internal/pipeline/agents/...`
- SCP-049 phase-A dogfood -- inspect `state.VisualScript` shape, 1:1 invariant, byte-for-byte anchor invariant.

**Manual checks:**
- `grep -rn "LegacyScenes" internal/pipeline/agents/visual_breakdowner.go` -- expected: zero matches.
- `grep -rn "VisualBreakdownOutput" internal/` -- expected: zero matches (type renamed; `VisualBreakdownScene` survives as deprecated bridge return).
- `grep -rn "state\.VisualBreakdown\b" internal/ cmd/` -- expected: zero matches (state field renamed).
- Inspect serialized state JSON post-dogfood -- top-level key `visual_script`, `source_version: "v2-visual"`, `acts: [4]`, each act `{act_id, shots: [N]}`, each shot's `narration_anchor` carries full BeatAnchor fields.
