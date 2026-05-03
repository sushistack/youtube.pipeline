# Next session — Monologue-mode decoupling (Lever D)

Run with `/bmad-quick-dev`. **Do not fire this prompt until the dequeue
trigger below holds.** This is the architectural ceiling-break for the
writer-quality lever chain (P → B → E → C → **D**). It is the largest
and riskiest of the levers. Estimated 1–2 weeks of focused work.
Schema bump from v1 to v2. Every downstream agent affected.

---

## Dequeue trigger (read this first)

Fire when **all** hold after the P+B+E and C cycles ship:

1. Lever P (`spec-writer-continuity-commentary-volume-bridge.md`),
   Lever B (within same spec), and Lever C
   (`next-session-whole-scenario-polish-pass.md`) have all shipped
   and merged on `main`.
2. A clean SCP-049 dogfood after C has been HITL-evaluated against
   the golden dataset (`docs/exemplars/scp-049-hada.txt` and siblings).
3. The HITL evaluation concludes that the **product identity gap**
   — not just the volume or transition gap — is still blocking. Specific
   signals:
   - Scenario reads as well-stitched scene-bounded narration but
     still NOT as continuous monologue. Each scene boundary is
     audible at TTS time as a hard cut even when the prose flows.
   - Information density per minute remains visibly below golden
     even after P-cap tuning, because scene-bounded narration
     enforces an "image-driven" rhythm rather than a "voice-driven"
     rhythm.
   - Channel positioning has been confirmed (by Jay) to require the
     hada-style monologue identity. If the channel ships fine as
     "cinematic SCP shorts," **do not fire D** — it is the wrong
     product fix.

If the channel has stabilized as a different (cinematic-shorts)
product, **delete this prompt** rather than firing it. Do not let
"because we wrote a prompt for it" be the reason to take on schema
v2 churn.

---

## What "monologue-mode" means

Today's pipeline:

```
Act → SceneBudget scenes
Each scene: narration (≤cap runes) + narration_beats[] (1-N strings)
Each beat → 1 visual_breakdowner shot → 1 image
TTS: concat scenes' narration with scene-boundary pauses
```

Golden channel (hada):

```
Act → single continuous monologue (~5500 KR chars total)
Imagery: stock/anime cuts timed to monologue beats, NOT to narration boundaries
TTS: one continuous voice-over per video, scene cuts are visual-only
```

**Monologue-mode for our pipeline:**

```
Act → ActMonologue (single continuous KR text, ~act_cap runes)
ActMonologue → segmented into BeatAnchors[] (offsets into the monologue text)
Each BeatAnchor → 1 visual_breakdowner shot → 1 image
TTS: synth ActMonologue as one block per act; scene boundaries become
     visual-only metadata (image cuts), not voice-over cuts
```

The schema bump separates **what is said** (monologue) from **what
is shown** (beats), which the v1 schema conflates by giving each
scene its own `narration` field.

---

## Spec sketch (refine at plan time)

### Schema (v1 → v2)

New `domain` types:

```go
// ActScript replaces NarrationScene as the act-level narration unit.
type ActScript struct {
    ActID      string         `json:"act_id"`
    Monologue  string         `json:"monologue"`  // continuous KR text
    Beats      []BeatAnchor   `json:"beats"`      // segmentation
    Mood       string         `json:"mood"`
    KeyPoints  []string       `json:"key_points"`
}

// BeatAnchor anchors one visual shot to a slice of the monologue.
type BeatAnchor struct {
    StartOffset       int      `json:"start_offset"`        // rune offset
    EndOffset         int      `json:"end_offset"`
    Mood              string   `json:"mood"`
    Location          string   `json:"location"`
    CharactersPresent []string `json:"characters_present"`
    EntityVisible     bool     `json:"entity_visible"`
    ColorPalette      string   `json:"color_palette"`
    Atmosphere        string   `json:"atmosphere"`
    FactTags          []FactTag `json:"fact_tags"`
}

// NarrationScript v2.
type NarrationScript struct {
    SCPID         string            `json:"scp_id"`
    Title         string            `json:"title"`
    Acts          []ActScript       `json:"acts"`
    Metadata      NarrationMetadata `json:"metadata"`
    SourceVersion string            `json:"source_version"` // "v2-monologue"
}
```

`NarrationScene` is deleted. Migrations: this is a clean cut. There's
no production data to migrate; the only "data" is fixtures and golden
eval samples.

### Writer rework

Two-stage writer per act:

**Stage 1 (per-act monologue write):**
- Inputs: act synopsis, key points, prior-act monologue tail (Lever B
  cascade extends naturally — pass `prior_act.Monologue` tail), quality
  feedback.
- Output: `{ "monologue": "..." }`.
- One LLM call per act. Acts 2/3/4 still parallel under errgroup.

**Stage 2 (per-act beat segmentation):**
- Inputs: the just-written monologue, structurer's act intent.
- Output: `{ "beats": [BeatAnchor, ...] }`.
- One LLM call per act. Can run in parallel with Stage 1 of the next
  act (pipeline-style).

Acts merged into `state.Narration` (now `NarrationScript` v2).

Decision points:
- Do Stage 1 + Stage 2 share a model? Recommend Stage 1 = qwen-max
  (creative density), Stage 2 = qwen-plus (structured segmentation).
- Beat count per act: lift from current SceneBudget × 1-N beats.
  Recommend 8–12 beats per act, ~2 sec per beat at golden pacing.

### visual_breakdowner rework

`visual_breakdowner` previously took `NarrationScene` and emitted
`VisualScene { Shots []VisualShot }`. v2 takes `ActScript` and emits
`VisualAct { Shots []VisualShot }` where each shot's
`narration_anchor` carries the rune-offset slice from the
corresponding `BeatAnchor`.

Files: `internal/pipeline/agents/visual_breakdowner.go`,
`docs/prompts/scenario/03_5_visual_breakdown.md`,
`testdata/contracts/visual_breakdown.{schema,sample}.json`.

### TTS rework

TTS previously synthesized per-scene audio and concatenated.
v2 synthesizes per-act audio (one block) and concatenates only at
act boundaries.

Files: TTS agent (path TBD, likely `internal/pipeline/agents/tts.go`
or similar), audio assembly stage, FFmpeg two-stage assembler
(`spec 9-1-ffmpeg-two-stage-assembly-engine.md` documents the
existing engine).

Risk: per-act audio is longer (~2.5 minutes per act at golden volume).
Verify the TTS provider (DashScope CosyVoice or whichever current)
handles 2.5-minute synthesis in one call. If not, intra-act chunking
becomes necessary — chunk on punctuation + sentence boundaries inside
the monologue, NOT on beat anchors.

### Critic / polisher / golden eval

- **Critic**: rewrite to read `ActScript` instead of `NarrationScene[]`.
  The critic's per-scene checks become per-beat or per-act-paragraph
  checks. Forbidden-term scan still operates on `monologue` text.
- **Polisher (Lever C, if shipped)**: rewrite to operate on
  `ActScript[]`. The diff-budget validator measures monologue rune
  delta per act, not per scene.
- **Golden eval** (`spec 4-2-shadow-eval-runner.md`): the golden
  scoring rubric is currently per-scene. Rewrite to score per-act
  monologue. This may reveal that golden samples themselves were
  scored against a v1-shaped rubric — verify before claiming the
  new scores are comparable.

### State machine + resume

Stages: `researcher → structurer → writer (stage1+2) → polisher →
critic → visual_breakdowner → tts → assembler → ...`. Resume
boundaries: writer-stage1 output (monologues only, beats absent) is
**not a valid stage artifact** — resume cannot restart from the
mid-writer point. Either treat writer as atomic (both stages must
complete or neither persists) or persist the intermediate
monologue-only state with an explicit `writer_stage1_complete` flag.
Recommend atomic.

---

## Open questions for plan step

1. **v1 → v2 migration strategy.** No production data to migrate.
   But: golden eval fixtures, all sample fixtures
   (`testdata/contracts/*.sample.json`), all integration test fixtures,
   and existing exemplars need rewriting. Is there value in maintaining
   a v1-compat read path during transition? Probably no — a clean cut
   keeps the codebase one-shape. Confirm at plan time.

2. **Beat count per act vs total.** The current pipeline targets
   ~10 scenes total (`target_scene_count=10` per
   `spec-writer-per-act-fanout.md`). v2 makes scene the wrong unit.
   Do we target ~30–40 beats total (8–10 per act × 4 acts), or fewer
   (more held imagery per beat)? This is a creative pacing decision,
   not a code decision. Resolve with a HITL spike.

3. **Image-gen budget.** Current pipeline's image budget scales with
   scene count × beats-per-scene. v2 may inflate or shrink this
   depending on (2). Re-estimate cost per dogfood before committing.

4. **fact_tags propagation.** Currently `fact_tags` are per-scene.
   In v2 they're per-beat. Verify the researcher / structurer fact
   pipeline maps cleanly to per-beat anchors. Lever E (researcher
   fact density) becomes more important here — beats need denser
   fact distribution to drive monologue numeric anchors.

5. **Polisher (C) interaction with D.** If C shipped before D, the
   polisher operates on `NarrationScript` v1. After D, it must
   re-operate on v2. Plan polisher v2 alongside D, or skip polisher
   for v2's first cycle and reintroduce it later.

6. **Reviewer / HITL UI.** Frontend dashboard
   (`spec 7-1-pipeline-dashboard-run-status.md` and
   `spec 8-1-master-detail-review-layout.md`) renders per-scene
   detail. v2 makes that wrong. Frontend rewrite may be a separate
   coordinated cycle — do NOT bundle into the agent-side schema bump.

---

## Touch surface (preview)

This is illustrative — real touch surface is broader.

- `internal/domain/narration.go` — full rewrite, NarrationScene
  deleted, ActScript / BeatAnchor added, schema version bumped.
- `internal/domain/scenario.go` — `ActNarrationRuneCap` becomes
  `ActMonologueRuneCap`, values rescaled to act-monologue scale
  (likely 4× current per-scene caps).
- `internal/pipeline/agents/writer.go` — two-stage rewrite. Per-act
  fan-out preserved.
- `internal/pipeline/agents/writer_test.go` — full rewrite of all
  per-act fan-out tests.
- `docs/prompts/scenario/03_writing.md` — major rewrite as
  monologue-spec; introduce stage 2 prompt file
  `docs/prompts/scenario/03_segmenting.md`.
- `internal/pipeline/agents/visual_breakdowner.go` and tests —
  consume ActScript.
- `docs/prompts/scenario/03_5_visual_breakdown.md` — major rewrite.
- `internal/pipeline/agents/critic.go` and tests.
- `internal/pipeline/agents/polisher.go` and tests (if C shipped).
- `internal/pipeline/agents/tts*.go` — per-act monologue synthesis.
- `internal/pipeline/runner.go` and state_machine wiring.
- `testdata/contracts/*.{schema,sample}.json` — every contract
  fixture.
- `internal/pipeline/phase_a_test.go`,
  `phase_a_integration_test.go` — large updates.
- `_bmad-output/implementation-artifacts/4-2-shadow-eval-runner.md`
  refresh — golden eval rubric refit.
- Frontend (Phase B/C dashboards) — coordinated rewrite, separate
  cycle, **do not bundle**.

---

## Acceptance signals

1. `go test ./...` and `go test -race ./...` clean.
2. End-to-end SCP-049 dogfood produces a `NarrationScript` v2 with
   4 acts, total monologue rune count ≥4500 (Lever P target was
   ≥4500 for v1; v2 should match or exceed).
3. TTS synthesizes per-act monologue without intra-act audio cuts.
   Listening test: at act-internal scene boundaries (where v1 had a
   pause), v2 has continuous voice.
4. Visual cut count is independent of monologue length. A 2.5-minute
   act monologue can carry anywhere from 6 to 14 visual cuts
   without the monologue rewriting itself.
5. **HITL listening test against golden**: blind-listen to a v2 dogfood
   act and a golden hada act. Time-to-distinguish should be ≥20 sec
   (i.e. they sound similar enough that a casual listener takes
   >20 sec to spot the AI version). If <5 sec, D didn't deliver —
   architectural change without quality return.
6. Golden eval rubric refit and rerun. Score on v2 should improve
   over v1 final-cycle score by ≥15 absolute points (whatever the
   rubric's scale is). If <5, D was wasted churn.

---

## Out of scope for this cycle

- **Frontend / dashboard rewrite**. Coordinated separately so the
  agent-side schema bump can land standalone behind a feature flag.
- **Image generation prompt assembly redesign**. Beats still drive
  shots; only the *anchor mechanism* changes (offset slice vs scene
  boundary). The image-prompt builder stays the same.
- **Multi-language support**. Korean-only.
- **Real-time / streaming TTS**. Batch synthesis stays.
- **Critic feedback loop redesign**. Critic still runs once; its
  signature changes but its semantics don't.

---

## Reference — what's being given up

The v1 schema's per-scene granularity has real virtues:

- **Resume granularity**: a v1 run that dies mid-writer can resume
  per-act; v2 makes act atomic but loses any sub-act granularity.
- **Critic surfacing**: per-scene critic feedback maps cleanly to
  HITL review UI; per-beat feedback in v2 is denser and harder to
  display.
- **Image-gen retry isolation**: a single shot regen in v1 doesn't
  perturb adjacent narration. In v2 it doesn't either, IF beat
  anchors stay stable across regens. Verify.

These tradeoffs are the cost of closing the architectural ceiling.
If the dequeue trigger fails (HITL says v1 product is fine), do not
pay them.
