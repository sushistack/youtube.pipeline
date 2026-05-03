---
title: 'D1 — domain types v2 + writer 2-stage (monologue + beat segmenter)'
type: 'feature'
created: '2026-05-04'
status: 'in-review'
baseline_commit: 'eb5a30d0f873039c3cb66f15aae3fd93d5e1f71c'
context:
  - '_bmad-output/planning-artifacts/next-session-monologue-mode-decoupling.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** v1 `NarrationScript` conflates "what is said" (per-scene narration) with "what is shown" (per-scene visual metadata), forcing TTS into per-scene synthesis with audible scene-boundary cuts. Cycle-C dogfood vs `docs/exemplars/scp-049-hada.txt` proved the gap with golden monologue is architectural, not prompt-tunable. Full carry/drop classification + open-question resolutions live in the linked plan — **read it before starting**.

**Approach:** Add v2 domain types `ActScript` (per-act continuous KR monologue + ordered `[]BeatAnchor`) and `BeatAnchor` (rune-offset slice into a monologue + visual metadata + fact_tags). Rewrite the writer agent into two stages per act: stage 1 = monologue text (`qwen-max`), stage 2 = beat segmentation (`qwen-plus`). Per-act fan-out preserved (acts 2/3/4 parallel; stage 2 of act N can pipeline with stage 1 of act N+1). `NarrationScene` stays in `narration.go` flagged `Deprecated` with a `LegacyScenes()` bridge so D2–D6 migrate downstream agents off it incrementally; the bridge is deleted in the same PR as the last consumer.

## Boundaries & Constraints

**Always:**
- Writer is atomic across both stages: a partial result (some acts have monologue but no beats) NEVER persists. D6 owns resume implications.
- BeatAnchor offsets are rune-level (`utf8.RuneCountInString`), not byte-level. Stage-2 validator rejects out-of-range, overlapping, or non-ascending offsets.
- 8–10 beats per act (plan resolution P2). Stage-2 validator enforces.
- Plan's "What survives" list carries: `{containment_constraints}` block, Korean 공포 유튜버 톤 guidance, `{forbidden_terms_section}` infrastructure, per-act metadata gate (re-applied to BeatAnchor fields in stage 2).
- Anti-hada cycle-C patches MUST NOT reappear in v2 prompts: no closer-단정문, no No-CTA, no one-scene-one-visual-beat, no per-scene rune caps. (Reverted in commit `f71565c`.)
- `source_version` bumps to `"v2-monologue"`.
- Carry cycle-C hardening (commit `2ef1d3c`): `finish_reason=length` is retryable both stages, max_tokens headroom retained, 5-min HTTP timeout retained.
- Stage 1 = qwen-max, stage 2 = qwen-plus. Per-act retry budget applies independently to each stage.

**Ask First:**
- If `qwen-plus` produces unreliable offsets in stage 2 (>2 offset errors per dogfood run after retries), HALT before swapping models.
- If `LegacyScenes()` would have to fabricate data not present in any BeatAnchor (e.g., a per-scene `narration` substring that doesn't map to a beat slice), HALT — do not silently invent.

**Never:**
- No v1-compat read path on persisted runs (clean cut, plan P1).
- No dual-schema persistence. `ActScript[]` is the only on-disk shape; the bridge is in-memory only.
- No frontend, no image-prompt assembly, no polisher, no TTS, no critic, no visual_breakdowner v2 — those are D2–D7.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected | Error Handling |
|---|---|---|---|
| Both stages succeed, 4 acts | act synopses + research | `state.Narration.Acts` × 4, each with `Monologue` + 8–10 `BeatAnchor`s | N/A |
| Stage-2 offsets out of range / overlapping / non-ascending | bad LLM output | per-act retry; on exhaustion fail with offending offset | `ErrValidation` |
| Stage-2 beat count outside [8, 10] | bad LLM output | per-act retry; on exhaustion fail with count mismatch | `ErrValidation` |
| `finish_reason=length` either stage | provider over-cap | retry per cycle-C policy; on exhaustion surface truncation | retry → escalate |
| Stage 1 OK for act N, stage 2 fails after retries | exhaustion | whole writer stage fails atomically; `state.Narration` unset | `ErrStageFailed` |

</frozen-after-approval>

## Code Map

- `internal/domain/narration.go` -- add `ActScript` / `BeatAnchor` types + v2 `NarrationScript` fields; flag `NarrationScene` `Deprecated`; add `(*NarrationScript).LegacyScenes()` bridge; add `SourceVersion="v2-monologue"`.
- `internal/domain/scenario.go` -- `ActNarrationRuneCap` → `ActMonologueRuneCap` (rescaled ~4× per plan touch surface).
- `internal/pipeline/agents/writer.go` -- two-stage rewrite. `runWriterAct` splits into `runWriterActMonologue` + `runWriterActBeats`; per-stage validators; truncation-retry policy preserved per stage.
- `internal/pipeline/agents/writer_test.go` -- per-act fan-out tests rewritten end-to-end; truncation-retry coverage preserved per stage; I/O matrix rows each get a unit test.
- `docs/prompts/scenario/03_writing.md` -- full rewrite as monologue-spec stage-1 prompt per plan carry/drop list.
- `docs/prompts/scenario/03_segmenting.md` -- NEW stage-2 prompt: "given monologue, produce 8–10 `BeatAnchor`s with offset slices".
- `testdata/contracts/writer_output.{schema,sample}.json` -- rewrite for v2 `ActScript` shape.
- `testdata/contracts/writer_segmenting.{schema,sample}.json` -- NEW stage-2 contract.
- `cmd/pipeline/serve.go` -- add `segmenterCfg` (qwen-plus); `writerCfg` retains cycle-C max_tokens/timeout.

## Tasks & Acceptance

**Execution:**
- [x] `internal/domain/narration.go` -- add v2 types + bridge + version enum; deprecate NarrationScene.
- [x] `internal/domain/scenario.go` -- rename + rescale rune cap.
- [x] `docs/prompts/scenario/03_writing.md` -- monologue rewrite per plan carry/drop list.
- [x] `docs/prompts/scenario/03_segmenting.md` -- new stage-2 prompt.
- [x] `testdata/contracts/writer_output.{schema,sample}.json` -- v2 rewrite.
- [x] `testdata/contracts/writer_segmenting.{schema,sample}.json` -- new.
- [x] `internal/pipeline/agents/writer.go` -- two-stage rewrite + per-stage validators.
- [x] `internal/pipeline/agents/writer_test.go` -- end-to-end rewrite + I/O matrix unit coverage.
- [x] `cmd/pipeline/serve.go` -- add segmenterCfg.

**Acceptance Criteria:**
- Given clean repo on `feat/monologue-mode-v2`, when `go build ./...` runs, then it succeeds with no `NarrationScene`-related compile errors.
- Given the same repo, when `go test ./...` and `go test -race ./internal/pipeline/agents/...` run, then all tests pass (downstream tests consume `Narration.LegacyScenes()`).
- Given an SCP-049 dogfood input, when phase A runs end-to-end, then `state.Narration.SourceVersion=="v2-monologue"`, `len(Acts)==4`, each Act has 8–10 BeatAnchors with monotonic non-overlapping rune-offset slices into its Monologue, and total monologue rune count ≥ 4500 (Lever P parity floor).

## Spec Change Log

<!-- Append-only. Empty until the first bad_spec loopback. -->

### 2026-05-04 — D1 implementation deltas (additive scope)

The spec's task list scoped the writer + types only, but `go build ./...` /
`go test ./...` acceptance forces touching every consumer of the v1
`NarrationScript.Scenes` field. These additive edits all migrate downstream
agents/services from `script.Scenes` to `script.LegacyScenes()` (in-memory
bridge) — no v2 logic is introduced in those agents:

- **Polisher (Lever C v1) replaced with a no-op stub.** v1 polisher mutates
  `state.Narration.Scenes`; v2 has no `Scenes` field to write back. Per plan
  resolution P5, polisher v2 ships in D7. `internal/pipeline/agents/polisher.go`
  is now a 30-LOC stub returning nil; `polisher_test.go` deleted (D7 rewrites
  against `ActScript[]`).
- **`script.LegacyScenes()` consumers migrated:** `polisher.go` (stub),
  `visual_breakdowner.go`, `critic.go` (`validateMinorPolicyFindings`),
  `policy.go` (`MatchNarration`/`MinorSensitivePatterns.MatchNarration`),
  `internal/critic/rubricv2/scorer.go`, `internal/contract/v2/adapter.go`,
  `internal/pipeline/{resume,tts_track,image_track}.go`,
  `internal/service/scene_service.go`.
- **`PromptAssets.SegmenterTemplate` + `loadSegmenterTemplate`** added to
  `assets.go`; `prompts/agents/script_segmenter.tmpl` + `prompts.AgentScriptSegmenter`
  added to the embedded template registry; `prompt_lint_test.go` covers the
  v2 placeholder set.
- **`PipelineConfig.SegmenterModel="qwen-plus" / SegmenterProvider="dashscope"`**
  added to `internal/domain/config.go` defaults (DashScope-only per memory
  feedback_api_dashscope_only).
- **Test fixtures** (`testdata/contracts/golden_eval_fixture.*`,
  `testdata/golden/eval/*`, `testdata/fixtures/shadow_scenarios/*`) and
  inline test fixtures (`shadow_test.go`, `runner_test.go`,
  `quality-gate/main_test.go`, `smoke06_shadow_live_test.go`) converted from
  v1 scene shape to v2 acts shape (4 acts × 8 beats minimum).
- **v1 multi-beat-per-scene tests** in `visual_breakdowner_test.go`
  (`ShotCountMatchesNarrationBeats`, `PassesDescriptorsThroughVerbatim`)
  marked `t.Skip(...)` — multi-beat-per-scene shape was eliminated by v2;
  D2 (visual_breakdowner v2) reintroduces equivalent coverage against
  `BeatAnchor[]`.
- **rubricv2 test fixture (`goodScript`)** rebuilt as a `goodScriptBuilder`
  whose `setActID` reorders consecutive same-act scenes into discrete `Acts[]`
  groups, allowing twist-position regression to push revelation forward
  without rewriting every scoring assertion.

**Pre-existing failures left as-is** (confirmed via `git stash` reproduction
on the base commit `eb5a30d` before any D1 edits): `TestSettingsHandler_PutReturns409WhenIfMatchStale`,
`TestSettingsService_SaveWritesEffectiveAndDisk`, `TestSettingsService_SaveRejectsIfMatchMismatch`,
`TestSceneHandler_ListReviewItems_ReturnsPayload`. Out of D1 scope.

## Design Notes

The `LegacyScenes()` bridge is dead-layer risk that is justified only because D2–D6 progressively migrate downstream agents off `NarrationScene`. After D2/D4 ship, the bridge has zero callers and is deleted in the same PR as the last consumer. If D2–D6 stall, this bridge becomes durable cruft — flag in retrospective and re-plan. (Per Jay's "no dead layers" memory: bridge is acceptable only if its removal milestone holds.)

Stage 2 model is `qwen-plus` (offset arithmetic on fixed input, not creative density). If empirically unreliable, the Ask-First rule applies before any swap.

## Verification

**Commands:**
- `go build ./...` -- clean build.
- `go test ./...` and `go test -race ./internal/pipeline/agents/...` -- all green.
- Phase A SCP-049 dogfood via `cmd/pipeline serve` -- `state.Narration` v2 shape per acceptance criteria.

**Manual checks:**
- Inspect `state.Narration.Acts[0].Monologue` for SCP-049 dogfood -- ≥1100 runes, no closer-단정문 / No-CTA enforcement (those rules dropped per cycle-C revert), opening reflects A4 resolution (writer-chosen origin- or incident-first).

## Suggested Review Order

**Domain types — start here**

- New v2 types `ActScript` / `BeatAnchor` + `LegacyScenes()` bridge that downstream v1 consumers route through.
  [`narration.go:20`](../../internal/domain/narration.go#L20)

- `ActMonologueRuneCap` rescaled ~4× from v1's per-scene cap; comment block records calibration rationale.
  [`scenario.go:168`](../../internal/domain/scenario.go#L168)

- New `SegmenterModel`/`SegmenterProvider` defaults (`qwen-plus`/`dashscope`).
  [`config.go:9`](../../internal/domain/config.go#L9)

**Writer agent — two-stage rewrite**

- `NewWriter` signature now takes both `writerCfg` + `segmenterCfg`; cascade sequences stage 1 → stage 2 per act atomically.
  [`writer.go:78`](../../internal/pipeline/agents/writer.go#L78)

- Stage 1 (monologue) — qwen-max call with truncation/JSON/schema retry per cycle-C policy preserved.
  [`writer.go:181`](../../internal/pipeline/agents/writer.go#L181)

- Stage 2 (beats) — qwen-plus call; validator enforces 8–10 beats / monotonic non-overlapping in-range offsets.
  [`writer.go:288`](../../internal/pipeline/agents/writer.go#L288)

- Per-stage validators decoupled — stage-1 covers act_id/monologue/mood/key_points; stage-2 covers beat shape.
  [`writer.go:266`](../../internal/pipeline/agents/writer.go#L266)

**Stage prompts**

- Stage-1 monologue spec: drops closer-단정문, No-CTA, one-scene-one-visual-beat per plan carry/drop list.
  [`03_writing.md:1`](../../docs/prompts/scenario/03_writing.md)

- Stage-2 segmenter prompt: rune-offset slicing rules + 8–10 beat enforcement.
  [`03_segmenting.md:1`](../../docs/prompts/scenario/03_segmenting.md)

- Embedded mirrors of both prompts with the same v2 placeholder set.
  [`script_writer.tmpl:1`](../../prompts/agents/script_writer.tmpl)
  [`script_segmenter.tmpl:1`](../../prompts/agents/script_segmenter.tmpl)

**Schema / contract fixtures**

- v2 writer_output schema: 4 acts (strict), 8–10 beats per act, scene_count 32–40.
  [`writer_output.schema.json`](../../testdata/contracts/writer_output.schema.json)

- New writer_segmenting schema for stage-2 contract.
  [`writer_segmenting.schema.json`](../../testdata/contracts/writer_segmenting.schema.json)

**Polisher v1 retired (D7 reintroduces v2)**

- v1 polisher.go replaced with a no-op stub; tests deleted per plan resolution P5.
  [`polisher.go:1`](../../internal/pipeline/agents/polisher.go)

**Downstream `.LegacyScenes()` migration**

- Forbidden-term scanner reads via the bridge.
  [`policy.go:101`](../../internal/pipeline/agents/policy.go#L101)

- Visual breakdowner v1 (D2 will rewrite) — bridges through LegacyScenes; multi-beat-per-scene tests Skip-flagged.
  [`visual_breakdowner.go:58`](../../internal/pipeline/agents/visual_breakdowner.go#L58)

- Other consumers: critic, scene_service, resume, tts_track, image_track, contract/v2/adapter, rubricv2/scorer.
  [`critic.go:340`](../../internal/pipeline/agents/critic.go#L340)
  [`scene_service.go:916`](../../internal/service/scene_service.go#L916)

**Wiring + tests**

- serve.go wires `segmenterCfg` (qwen-plus, temp 0.0) into NewWriter.
  [`serve.go:411`](../../cmd/pipeline/serve.go#L411)

- Writer tests cover the spec's I/O matrix (happy path / out-of-range offsets / overlap / beat-count / truncation per stage / atomic stage-1+2 fail).
  [`writer_test.go:1`](../../internal/pipeline/agents/writer_test.go)
