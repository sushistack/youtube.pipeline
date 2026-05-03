# Next session — Scene causal linkage + narrator commentary + volume lift

Run with `/bmad-quick-dev`. Single planning artifact bundling the **writer-stage
quality gap** surfaced after the visual_breakdowner Goal 2 cycle (`151c691`)
landed. Two distinct symptoms, one prompt file at the root of both:

1. **Scene-to-scene jumps within an act** — comparing
   [`docs/scenarios/SCP-049.example.md`](../../docs/scenarios/SCP-049.example.md)
   vs [`docs/scenarios/SCP-049.example2.md`](../../docs/scenarios/SCP-049.example2.md):
   both share the same defects (locations appear without setup, scenes
   feel like jump-cuts, final scene closes on rhetorical questions).
   fewshot (`4e924e9`) lifted local tone but cannot fix what the
   writer prompt never asked for.
2. **~30% gap to golden dataset, both quality AND quantity** — vs.
   [`docs/exemplars/scp-049-hada.txt`](../../docs/exemplars/scp-049-hada.txt)
   and siblings. Pipeline emits ~2300 KR chars; golden ~5500 chars
   (≈42% volume ratio). Pipeline is missing entire narrative modes the
   golden channel relies on every video: explainer commentary,
   conversational asides, numeric facts, channel-signature CTA closer.

Both fixable in the **writer prompt** ([`docs/prompts/scenario/03_writing.md`](../../docs/prompts/scenario/03_writing.md))
without code or schema changes. Bundle into one spec.

---

## Plan-step recommendation: ONE spec, two rules + one cap lift

This used to be a 4-spec menu. Collapsed:

- **Within-act continuity rule** + **closer discipline** → one prompt section.
- **Per-act cap lift** + **commentary modes section** + **CTA closer template**
  → one prompt section.
- **Cross-act bridge** (the only thing that needs Go code / serialization
  changes) → not in scope here. Add to
  [`deferred-work.md`](../implementation-artifacts/deferred-work.md)
  with the dequeue trigger: "after this cycle's dogfood, if Act N→N+1
  jumps still feel jarring."

Net: one spec, one file edit (writer prompt), zero schema bump, zero
code change. Estimated ~2-3 hours to build + dogfood.

---

## Current state on main (read first)

Baseline: `151c691 feat(agents): visual_breakdowner Goal 2 + retry helper + hardening sweep`.

Relevant new infrastructure from that commit:

- **`NarrationScene.NarrationBeats []string`** is now part of the writer
  schema ([`internal/domain/narration.go:22-31`](../../internal/domain/narration.go#L22-L31)).
  Each beat = one downstream visual_breakdowner shot (1:1 mapping).
  Min 1 beat per scene; richer scenes typically 2-4. **This matters
  for Spec D**: commentary can either embed inside an existing beat's
  narration *or* live as a non-rendering aside in `narration` text
  outside the beats. Decide at plan time — lean toward embedding,
  because each beat already drives an image and asides shouldn't
  inflate the image budget.
- **`runWithRetry[T]` helper** exists at
  [`internal/pipeline/agents/retry.go`](../../internal/pipeline/agents/retry.go).
  Irrelevant to this cycle but means writer.go retry plumbing is now
  uniform across stages.
- **Per-act narration caps stay in the prompt** at
  [`03_writing.md:55-59`](../../docs/prompts/scenario/03_writing.md#L55-L59),
  not in Go code. Verified by skimming the diff in `151c691` —
  writer.go's cap validation reads from prompt-defined limits.
  Cap lift is therefore a one-line numeric edit.
- **Pre-Output Self-Check** at
  [`03_writing.md:171-181`](../../docs/prompts/scenario/03_writing.md#L171-L181)
  is the natural place to add new check items.
- **`prior_act_summary`** at
  [`writer.go:summarizePriorAct`](../../internal/pipeline/agents/writer.go)
  still passes only Act 1's tail (mood-only) to Acts 2/3/4. This is
  the cross-act bridge problem; **deferred** for now.

Latest dogfood (SCP-049, before this cycle): visual_breakdowner stable
end-to-end, reviewer non-deterministic but generally passes. The writer
output itself is what this cycle is targeting.

---

## Quality + quantity gap vs golden dataset

### Volume

| Channel | Total KR chars | per-minute (10-min target) |
|---|---|---|
| Golden ([`scp-049-hada.txt`](../../docs/exemplars/scp-049-hada.txt)) | ~5500 | ~550 |
| Pipeline ([`example2.md`](../../docs/scenarios/SCP-049.example2.md)) | ~2300 | ~230 |
| **Gap** | **~3200 chars** | **~58% short** |

Root cause: per-act caps `incident=100 / mystery=220 / revelation=320 / unresolved=180`
were tuned for shot-driven imagery, not voice-over density. To hit
golden volume, equivalent caps would be roughly
**`incident=120 / mystery=400 / revelation=520 / unresolved=280`**
(≈70-80% wider). Validate against TTS pacing on dogfood — at ~7-8
KR chars/sec, mystery-cap=400 ≈ 50sec which fits the existing
35-50sec atmospheric scene target.

Volume gap is **NOT** addressable by adding more scenes (would multiply
image-gen cost). Only by lifting per-scene caps.

### Narrative-mode gap (qualitative)

Patterns the golden channel uses every video, **0 instances** in pipeline:

| Mode | Golden example | Pipeline status |
|---|---|---|
| Explainer / 설명조 | "이 녀석은 상식을 아득히 뛰어넘는 수준의 ~인데요" | Absent |
| Conversational aside | "아니 ~ 라고 생각하실 수도 있지만" | Absent |
| Numeric / ratio fact | "신체의 87%가 파괴되거나 썩은 상태에서도" | Absent |
| Speculation hook (mid-stream) | "과연 ~일까요?" mid-video | Only as closer |
| CTA closer | "댓글로 ~. 다음 영상에서 또 만나죠. 안녕!" | Absent |
| Sequential information stacking | reveals are cumulative across scenes | Scenes are independent capsules |

### Architectural ceiling (state explicitly to user)

- Golden = single-narrator monologue over stock/anime imagery.
- Pipeline = scene-bounded narration paired with per-shot generated imagery.
- These are **different products**. Pipeline can match golden's *tone*
  and *information density* but cannot reproduce seamless monologue —
  every scene boundary is a hard cut by design.
- Realistic ceiling for prompt-only fixes: **65-75% match** to golden
  on combined quality+volume. Going past that requires decoupling
  narration from scene boundaries (architectural change) OR accepting
  a different product identity (cinematic SCP shorts vs. hada-style
  explainers).
- **Surface this to the user before approving the spec.** If the user
  wants 90%+ match, the answer involves architectural work, not
  prompt iteration.

---

## Spec — Writer prompt: continuity + closer + commentary + volume

Single spec. Edit one file: [`docs/prompts/scenario/03_writing.md`](../../docs/prompts/scenario/03_writing.md).

### Rule 1 — Within-act scene continuity

Within a single act, every scene after the first MUST be a direct
consequence of the prior scene, anchored by:
- (a) **physical continuation**: same location, same actors, time
  advances < ~30 sec in-story; OR
- (b) **causal continuation**: a definite trigger in scene N causes
  scene N+1's action; OR
- (c) **explicit bridge token**: opening with one of the allowed
  connectors that mark legitimate jumps.

Allowed bridge tokens (whitelist): `"그리고 며칠 뒤"`, `"한편"`,
`"그로부터 얼마 후"`, `"이후"`, `"같은 시각"`, `"다음 날"`. Reject
weak transitions like bare `"그리고"` / `"그러더니"` as bridges —
they don't mark a jump.

Add to writer prompt as a new **씬 간 연결 규칙** subsection adjacent to
the existing **씬 단위 규칙** block ([lines 49-84](../../docs/prompts/scenario/03_writing.md#L49-L84)).
Mirror its shape: rule + ❌ bad example + ✅ good example.

### Rule 2 — Closer discipline (Act 4 unresolved)

- Final scene of the entire video MUST close on a **definite state**
  (concrete image, specific reading, held silence) — NOT on a
  rhetorical question.
- Replace current Act 4 guidance ([line 119](../../docs/prompts/scenario/03_writing.md#L119))
  with: ❌ "이게 치료일까요, 저주일까요?" → ✅ "그날 049의 펜은 멈추지 않았습니다."
- The CTA closer (Rule 4 below) **is** the replacement for the
  rhetorical question. They're not separate rules.

### Rule 3 — Lift per-act narration caps

[Lines 55-59](../../docs/prompts/scenario/03_writing.md#L55-L59):

```
incident:    100 → 120
mystery:     220 → 400
revelation:  320 → 520
unresolved:  180 → 280
```

Update the matching Pre-Output Self-Check item ([line 178](../../docs/prompts/scenario/03_writing.md#L178))
to the new values. Also update the
[`prompt_lint_test.go`](../../internal/pipeline/agents/prompt_lint_test.go)
if it pins the cap numbers (verify at plan time).

### Rule 4 — Narrator commentary modes

Add a new **Narrator Commentary Modes** subsection between
**필수 몰입 기법** ([line 86](../../docs/prompts/scenario/03_writing.md#L86))
and **문장 & 페이싱 규칙** ([line 96](../../docs/prompts/scenario/03_writing.md#L96)).

Mandate at least one commentary line per **non-incident** scene from a
defined menu:
- **Explainer**: "이 녀석은 ~인데요", "그게 어느 정도인 거하니"
- **Aside**: "아니 ~ 라고 생각하실 수도 있지만"
- **Speculation hook**: "과연 ~일까요?" (mid-stream, not just closer)
- **Numeric anchor**: "무려 ~%가" / "총 ~번 중 ~번" (must be from
  research packet's `fact_tags`, no inventing)

Acts: applies to `mystery` / `revelation` / `unresolved`. The `incident`
act stays pure-impact (no commentary — preserves hook discipline).

**Where the commentary lives in the schema**: embed it inside an
existing beat's `narration` text. Do NOT create a separate beat for
commentary alone — beats drive image generation, and a commentary aside
has no obvious visual.

### Rule 5 — CTA closer

Final scene of the `unresolved` act MUST end with a CTA template.
First-cut: parameterize via `{channel_signature}` in the prompt template,
default empty. **Do NOT bake hada's signature** ("우리는 다음 영상에서
또 만나도록 하죠. 안녕!") — that's hada's, not the user's. Surface
the question to the user during plan step: "what's your channel
sign-off?" If unknown, ship with empty default and the rule reduces
to "definite-state closer" only (Rule 2).

### Rule 6 — Numeric fact injection

Companion to Rule 4's numeric-anchor mode: at least 2 scenes per non-incident
act must surface a number/ratio/count from `fact_references`. Add as a
new line in **콘텐츠 규칙** ([line 121-126](../../docs/prompts/scenario/03_writing.md#L121-L126)).

### Pre-Output Self-Check additions

Append to [the existing checklist](../../docs/prompts/scenario/03_writing.md#L171-L181):

- [ ] 각 씬 N+1은 씬 N과 (a) 무대 연속, (b) 인과 연속, (c) 허용된 연결어 중 하나로 이어진다
- [ ] `unresolved` act의 마지막 씬은 의문문이 아닌 단정문으로 끝난다 (CTA 포함 시 제외)
- [ ] `mystery`/`revelation`/`unresolved` 각 씬당 commentary 모드 (explainer/aside/speculation/numeric) 중 1개 이상 포함
- [ ] 비-incident act당 숫자/비율 fact 2회 이상 등장

---

## Acceptance signals

Run a clean SCP-049 dogfood. Measure:

1. **Volume**: total KR chars in merged scenario ≥4500 (target ~82% of
   golden). Current ~2300.
2. **Mode coverage**: each commentary mode appears ≥2 times across the
   scenario. CTA closer present (or definite-state closer if
   `{channel_signature}` empty).
3. **Connectivity**: tag every within-act adjacency as
   `physical | causal | bridged | jump`. Target: **0 jumps within any act**.
4. **HITL spot-check**: read end-to-end. Does the result feel like a
   real channel narration vs. a sequence of dramatic captions?
   Subjective but the only honest test.
5. **TTS sanity**: synth one act at the new caps. If pacing sounds
   rushed, dial caps back ~15%.

---

## Out of scope

- **Cross-act bridge** (Acts 2/3/4 receiving prior act's actual tail).
  Code/serialization surgery. Add to
  [`deferred-work.md`](../implementation-artifacts/deferred-work.md)
  with trigger: "after this cycle, if Act N→N+1 jumps still feel jarring."
- **Per-beat commentary as separate beat**. Rejected above — beats
  drive image budget. Commentary embeds in existing beats' narration.
- **Channel-signature template selection**. Spec parameterizes
  `{channel_signature}`; user fills it post-spec.
- **Reviewer non-determinism** / **TTS pacing tuning beyond cap
  validation** / **adding more fewshot exemplars** / **architectural
  monologue-mode pipeline**.

---

## Reference — what fewshot fixed (and what it didn't)

Spot diff between [`example.md`](../../docs/scenarios/SCP-049.example.md)
(pre-fewshot) and [`example2.md`](../../docs/scenarios/SCP-049.example2.md)
(post-fewshot, commit `4e924e9`):

✅ Closed by fewshot:
- Opening concreteness ("은빛 조명 아래" → "커다란 소의 사체 위로")
- Named anchors (anonymous "D" → "D-9982")
- Surgical sequence decomposition (1 squashed scene → 3 distinct beats)
- Verb intensity / sensory density

❌ Unchanged — this cycle's target:
- Within-act scene-to-scene causal linkage → **Rule 1**
- Final-scene closer discipline (rhetorical question endings) → **Rule 2**
- Volume gap (~42% of golden) → **Rule 3**
- Missing narrator commentary modes → **Rule 4**
- Missing CTA closer → **Rule 5**
- Missing numeric facts → **Rule 6**
- Cross-act narrative bridging → **deferred**

Realistic ceiling after this cycle: **65-75% match** to golden on
combined quality+volume. >75% requires architectural change.
