# Next session — Monologue-mode decoupling (Lever D)

Run with `/bmad-quick-dev`. **Do not fire this prompt until the dequeue
trigger below holds.** This is the architectural ceiling-break for the
writer-quality lever chain (P → B → E → C → **D**). It is the largest
and riskiest of the levers. Estimated 1–2 weeks of focused work.
Schema bump from v1 to v2. Every downstream agent affected.

---

## 2026-05-04 Pre-fire context — what the C cycle dogfood proved (read before firing)

The C cycle (Lever C polisher) shipped on 2026-05-03 (commit `c8aea99`).
A dogfood pass on SCP-049 with the polisher in line produced 6 example
runs (`docs/scenarios/SCP-049.example3.md` through `example8.md`).
Comparing those runs against `docs/exemplars/scp-049-hada.txt` proved
that **the gap between our output and the golden monologue is
architectural, not prompt-level**, which is exactly the trigger this
plan was queued for. The dequeue trigger (below) is satisfied.

**What the comparison showed:**

| Dimension | Golden (hada) | Cycle C output (ex8) |
|---|---|---|
| Form | Continuous voice-over, no scene breaks | 17 scene-bounded narrations, hard cuts |
| Opening | Origin/discovery as info ("프랑스 남부의 한 마을…") | Cold-open visual hook |
| Pacing | Information-led, every line a fact | Visual-led, scene padding (location/mood/characters) |
| D-class | Generalized ("연구원들") | Named (D-9982 / D-9341) |
| Closer | 의문문 + 댓글 호명 + "다음 영상에서 만나도록 하죠. 안녕!" | 단정문, no CTA |
| Length | ~5500 chars continuous | ~4250 chars across 17 scenes |

**Three patches in the C cycle were actively *anti-hada*** and must be
reverted as part of the D writer rework — the schema-bound v1 prompt
was forcing rules that golden itself violates:

1. **No-CTA closer rule** (added 2026-05-03 to `03_writing.md` after
   ex4-ex6 all leaked CTA on the closer). Golden ends with
   *"공사구에 대한 여러분들의 재미난 추측과 상상력을 댓글을 통해
   남겨주시면 감사하겠습니다…우리는 다음 영상에서 또 만나도록 하죠.
   안녕!"* — full CTA + viewer address + sign-off. Our patch
   forbade exactly this. **Drop the rule in v2.**
2. **Closer-must-be-단정문 rule** (existed pre-C). Golden closes
   with two consecutive 의문문: *"역병이란 무엇일까요?"* and
   *"좀비가 되어버리는 것일까요?"*. **Drop or invert in v2.**
3. **One-scene-one-visual-beat rule** (the entire architecture of
   `03_writing.md`). Schema disappears in v2 — beats become
   `BeatAnchor` slices into a single act monologue, not narration
   units. Rule moves with the schema.

**What survives the v1→v2 cut (do *not* re-derive from scratch):**

- **Lever B canon enforcement** — the `{containment_constraints}`
  template variable + "current-protocol forbidden actions must be
  past-framed" rule worked well across all 6 runs (only ex4 went
  implicit; ex5/ex6/ex7/ex8 all carried explicit time markers).
  Re-apply this rule to the v2 monologue prompt, scoped to
  monologue-level rather than per-scene.
- **Resume bug fix in `internal/pipeline/resume.go`** (post_writer
  retry was misclassified as "post_reviewer missing"). Lives in the
  engine, schema-agnostic — keep as is.
- **`visual_breakdowner` empty-content retry** (transient
  `domain.ErrStageFailed` is now retryable, was wrongly aborting on
  first miss). Visual_breakdowner survives in v2 with a different
  input shape but same retry policy — keep.
- **Writer per-act metadata gate** in `validateWriterActResponse`
  (forbids empty `characters_present`/`narration_beats` so per-act
  retry catches the LLM omission instead of the merged schema
  validator catching it too late). v2's stage-1 writer no longer has
  per-scene metadata, but the retry-budget-utilization principle
  carries — apply the same per-attempt validation pattern to v2's
  beat segmenter.
- **Polisher fallback-not-fail regime + read-only invariance + edit
  budget**. Concept survives. Re-implement against `ActScript[]`
  with per-act monologue rune-delta budget instead of per-scene.
  The 0.40 calibration data (see `narration.go` PolisherMaxEditRatio
  comment) is v1-specific — recalibrate for v2's larger units.

**What was wasted churn:**

- The CTA prompt patch and the existing closer-단정문 rule. ~30%
  of cycle C touch surface. Both were reactive to dogfood symptoms
  rather than to the underlying architectural mismatch.
- Per-scene rune cap tuning, multi-beat-compression policing,
  per-scene metadata expansion. v2 deletes the unit.

**The key process lesson:** the golden vs. dogfood comparison should
have been done **before** any reactive prompt patches in cycle C, not
after. A 2-minute side-by-side of `docs/exemplars/scp-049-hada.txt`
against the first dogfood output would have surfaced the architectural
gap directly. Subsequent prompt-level fixes were chasing symptoms of
the v1-vs-monologue schema mismatch, not closing real quality gaps.
**At D plan time and going forward: golden comparison is step 0 of
any quality-cycle, before agent-level patching.**

---

## Dequeue trigger — SATISFIED 2026-05-04

All three conditions hold:

1. ✅ Lever P / Lever B / Lever C all shipped and merged on `main`.
   C cycle commit: `c8aea99` (2026-05-03).
2. ✅ SCP-049 dogfood (ex3-ex8 in `docs/scenarios/`) compared
   against `docs/exemplars/scp-049-hada.txt`. The architectural
   gap is structural, not a quality-tuning gap — see the
   pre-fire context block above for the side-by-side.
3. ✅ Channel positioning confirmed by Jay (2026-05-04): the
   target identity is hada-style monologue, not cinematic SCP
   shorts. The cycle-C product (scene-bounded narration with
   visual breaks) is **not** what the channel ships.

**This plan is fire-ready.** Run `/bmad-quick-dev` against this
artifact at the next session start. Do NOT re-evaluate the trigger;
that work was completed in the cycle-C dogfood and is captured in
the pre-fire context block above.

---

## 2026-05-04 Open Question Resolutions (v1.1)

The 4 architectural questions surfaced by golden study and the 6
"Open questions for plan step" are resolved below. **These resolutions
are owned by Jay 2026-05-04 and supersede the open framings further
down the document; story specs implement them as-is.** Story
decomposition (see "Story Decomposition" section near the end) follows
from these resolutions.

| # | Question | Resolution |
|---|---|---|
| A1 | per-act vs whole-script monologue | **Option 1** — structurer keeps producing 4 acts as planning units; writer concatenates into a single monologue with explicit transition phrasing; no audible act-boundary cuts. Acts are internal-only scaffolding. |
| A2 | TTS audio continuity | **Try whole-script single TTS call first** (DashScope CosyVoice). If provider rejects ~5500-rune input, fall back to sentence-internal chunking with shared voice continuity params; act boundaries must NOT introduce audible cuts. |
| A3 | Beat segmenter scope | **Per-act, 4 calls, parallel-able** — direct corollary of A1. |
| A4 | Opening discipline | **No cold-open enforcement.** Writer picks origin-first or incident-first per SCP fit. v2 prompt explicitly drops the v1 incident-act cold-open hook rule. |
| P1 | v1→v2 migration | **Clean cut, no compat read path.** No production data; only fixtures + golden samples to rewrite. Compat shim = dead layer. |
| P2 | Beat count per act | **8–10 beats per act × 4 acts = 32–40 total.** First dogfood drives fine-tuning; treated as creative pacing tunable, not architectural. |
| P3 | Image-gen budget | **Defer until first dogfood measurement.** P2 sets the upper bound; cost is observed-then-adjusted, not pre-budgeted. |
| P4 | fact_tags propagation | **Direct per-beat mapping.** Researcher output unchanged; structurer attaches fact_tags per beat at segmentation time. Lever E (researcher fact density) NOT bundled into D. |
| P5 | Polisher v2 timing | **Skip first cycle.** D1–D6 ship without polisher in line. After v2 dogfood baseline measurements, polisher v2 lands as a separate cycle (D7) with calibration against v2 unit, not the v1 0.40 calibration which is being reverted in commit `f71565c`. |

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

## Cycle-C dogfood corrections — apply at plan time

These are concrete updates to the original plan based on side-by-side
study of `docs/exemplars/scp-049-hada.txt` (golden) vs. ex3-ex8
(cycle-C dogfood). Surface them when the plan step refines this
sketch.

### v1 writer prompt rules to **delete** in v2 (anti-hada)

`docs/prompts/scenario/03_writing.md` accumulated rules during
P/B/C cycles that fight the hada style. v2's writer prompt
(`docs/prompts/scenario/03_writing.md` rewrite, plus new
`03_segmenting.md`) must NOT carry these forward:

- **No-CTA closer rule** (added 2026-05-03). Golden ends with
  full CTA: *"댓글을 통해 남겨주시면 감사하겠습니다…우리는 다음
  영상에서 또 만나도록 하죠. 안녕!"* Drop the rule.
- **Closer-단정문 rule** (pre-existing). Golden closes with
  consecutive 의문문. Drop the rule, or invert it ("closer SHOULD
  pose 1-2 reflective questions about the SCP and address the
  viewer directly with a short sign-off").
- **One-scene-one-visual-beat rule + per-scene rune caps + scene
  count budgeting**. Schema disappears. The unit becomes the
  monologue (per-act or whole-script — see below) plus
  `BeatAnchor[]` segmentation. No per-scene narration budget.

### v1 writer prompt rules to **keep** (carry into v2)

- **Lever B canon enforcement** (`{containment_constraints}` block,
  past-tense framing for forbidden actions). 4 of 6 cycle-C runs
  hit explicit canon framing on the first try; the rule worked.
  Re-apply at the monologue prompt level — the constraint operates
  on text content, not on schema shape.
- **Korean 공포 유튜버 톤 guidance** (살리의방 / TheVolgun /
  TheRubber reference, ~합니다·~입니다 + 구어체 mix, 위키조 금지).
  Schema-agnostic. Keep verbatim.
- **Forbidden terms infrastructure** (`{forbidden_terms_section}`,
  `terms.MatchNarration`). Operates on text. Keep — but the v2
  forbidden-terms file (`docs/policy/forbidden_terms.ko.txt`) does
  NOT need the closer-CTA pseudo-block we considered earlier; that
  pattern is allowed now.

### Open architectural questions surfaced by golden study

These were **not** in the original plan and must be resolved before
spec-writing in the plan step:

1. **Per-act vs. whole-script monologue.** The original plan said
   `Act → ActMonologue × 4`. But hada golden reads as ONE continuous
   monologue with no internal act boundaries audible to the viewer.
   It has a structural arc (origin → ability → 049-2 → containment
   → closer) but the 4-act
   `incident/mystery/revelation/unresolved` taxonomy is a v1
   artifact that doesn't map to golden.

   Resolve at plan time:
   - **Option 1 (preserve 4-act internally):** structurer keeps
     producing 4 acts as planning units; writer concatenates
     them into a single monologue with explicit transition
     phrasing; TTS synthesizes the whole script as one block (or
     chunked under provider limits, but with no act-boundary
     audible cut). Acts become **internal-only** scaffolding.
   - **Option 2 (drop acts at the output level):** structurer
     produces a single ordered list of `key_points` + `mood_arc`
     with no act boundaries; writer treats it as one
     monologue plan; v2 schema has no `Acts[]`.
   - Recommend Option 1 for migration ease (structurer survives
     unchanged) and as defensive scaffolding against the writer
     producing structureless prose.

2. **TTS audio continuity.** Original plan said per-act TTS with
   concatenation only at act boundaries. In Option 1 above, the
   audio MUST be a single continuous voice-over — act-boundary
   pauses would re-introduce exactly the cut artifact this whole
   plan exists to remove. Either:
   - Synthesize the whole script in one TTS call (verify provider
     limit; DashScope CosyVoice is the current path).
   - If chunking is required, chunk on **sentence boundaries
     internal to acts**, not on act boundaries, and use the same
     voice continuity parameters across chunks so the seam is
     inaudible.

3. **Beat segmenter scope.** Original plan had per-act beat
   segmentation. If Option 1 above holds, the beat segmenter still
   operates per-act (4 calls, parallel-able), since each act's
   monologue text is the natural unit. If Option 2, the segmenter
   operates on the whole script (single call). Resolve with (1).

4. **Opening discipline.** Hada golden opens with origin/discovery
   info (*"프랑스 남부의 한 마을에서…"*) — NOT with a cold-open
   visual hook. v1's `incident` act forced cold-open hooks
   (*"눈을 감는 순간, 당신은 죽습니다"* style). v2 prompt should
   **not** force a cold-open — let the writer choose between
   origin-first and incident-first based on which fits the SCP.
   Probably most SCPs read better origin-first per hada style.

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

## Story Decomposition (v1.1)

D is treated as an epic. Each story below is a separate
`/bmad-quick-dev` session against its own spec file under
`_bmad-output/implementation-artifacts/`. Stories are sequential —
D2/D4 may fan-out after D1's schema lands, but no story can start
before D1.

| Story | Scope | Spec file |
|---|---|---|
| **D1 — domain types v2 + writer 2-stage** | NarrationScript v2 (`ActScript` / `BeatAnchor`), `NarrationScene` deletion, `writer.go` two-stage rewrite (monologue write → beat segment), `03_writing.md` rewrite + new `03_segmenting.md`, all `writer_test.go` per-act fan-out tests rewritten, contract fixtures rewritten, downstream stage shims so `go build ./...` and `go test ./...` stay green and a first end-to-end dogfood is reachable. | `spec-d1-domain-types-and-writer-v2.md` (this epic's first spec) |
| **D2 — visual_breakdowner v2** | Consume `ActScript` / `BeatAnchor`; emit `VisualAct` with rune-offset narration anchor per shot. `03_5_visual_breakdown.md` rewrite, contract fixtures rewrite. | `spec-d2-visual-breakdowner-v2.md` (created when D1 ships) |
| **D3 — TTS per-act/whole-script synthesis** | Spike CosyVoice ~2.5min limit. Single-call vs intra-act sentence-chunking decision documented. Audio assembler updates: act boundaries no longer add audible pauses. | `spec-d3-tts-monologue-synthesis.md` |
| **D4 — critic v2** | Consume `ActScript[]`. Per-scene checks become per-beat / per-act-paragraph. Forbidden-term scan unchanged. | `spec-d4-critic-v2.md` |
| **D5 — golden eval rubric refit** | Per-act monologue scoring. Golden sample reshape to v2. Verify v1 → v2 score comparability before claiming improvement. | `spec-d5-golden-eval-v2.md` |
| **D6 — state machine + resume** | Writer atomic (stage1+stage2 must both complete or neither persists). Resume cannot restart mid-writer. `phase_a` wiring updates. | `spec-d6-state-machine-resume-v2.md` |
| **D7 — polisher v2 (separate cycle)** | Operates on `ActScript[]` with per-act monologue rune-delta budget. Recalibrate ratio against v2 baseline. Read-only invariance + edit budget retained. | NOT in this epic — queued in `deferred-work.md` after D1–D6 dogfood per resolution P5. |

D7 is gated behind v2 baseline dogfood measurement.

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
