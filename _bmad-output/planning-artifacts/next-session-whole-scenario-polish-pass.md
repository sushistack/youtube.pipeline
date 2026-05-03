# Next session — Whole-scenario polish-pass agent (Lever C)

Run with `/bmad-quick-dev`. **Do not fire this prompt until the dequeue
trigger below holds.** Designed as Lever C in the writer-quality lever
chain (P → B → E → **C** → D). P+B+E precede this; D follows only if C
is insufficient.

---

## Dequeue trigger (read this first)

Fire when **all** hold after the P+B+E cycle dogfoods:

1. P+B+E spec (`spec-writer-continuity-commentary-volume-bridge.md`)
   has shipped and merged on `main`.
2. A clean SCP-049 dogfood has been run end-to-end through writer.
3. HITL spot-check on the merged scenario produces **at least one**
   of the following observations:
   - Cross-act transitions still feel jumpy despite `summarizePriorAct`
     cascade (Lever B did not fully close the Act N→N+1 gap).
   - Final closer (Act 4 unresolved) still reads as scene-bounded
     rather than scenario-bounded — the closer "ends a scene," not
     "ends a video."
   - Within-act commentary modes from Lever P land but the scenario
     overall reads as 4 stitched mini-narrations rather than one
     coherent ~10-minute piece.
4. Volume gap is no longer the gating concern (≥75% of golden volume
   has been achieved by P alone; if not, fix volume first via P-cap
   tuning rather than reaching for C).

If the trigger does not hold, **defer further** — do not fire the
spec just because the prompt file exists.

---

## What "polish-pass" means

A new agent stage inserted **after** the writer (per-act fan-out) and
**before** critic. It reads the entire merged `NarrationScript`
(all 4 acts, all scenes), and returns a rewritten script with the
**same schema** but with three targeted edits:

1. **Cross-act transition rewriting** — the first scene of each act
   (except Act 1) is rewritten to land naturally from the prior act's
   final scene. The agent has full visibility into both acts at once,
   which `summarizePriorAct` (B) only approximates via a 240-rune tail.
2. **Closer rewrite** — the final scene of `unresolved` is rewritten
   to land as a video-closer (definite-state + optional CTA), not a
   scene-closer.
3. **Connective-tissue smoothing** — within-act adjacent scenes whose
   continuity rule (P, Rule 1) was satisfied only by allowed bridge
   tokens get their bridge softened or replaced if a more concrete
   physical/causal continuation is available given full-script context.

**Schema is unchanged.** `NarrationScene` shape, `NarrationBeats`,
all metadata fields stay. Only `narration` text and `narration_beats`
text within affected scenes are rewritten.

---

## Spec sketch (refine at plan time)

### New agent: `polisher`

- Location: `internal/pipeline/agents/polisher.go`.
- Signature: `func NewPolisher(gen domain.TextGenerator, cfg TextAgentConfig, prompts PromptAssets, validator *Validator, terms *ForbiddenTerms) AgentFunc`.
- One LLM call over the full merged script. No fan-out.
- Reads `state.Narration` (set by writer), produces a rewritten
  `NarrationScript`, replaces `state.Narration` on success.
- Per-call retry budget = 1 (mirrors writer per-act retry pattern,
  routed through `runWithRetry` from `internal/pipeline/agents/retry.go`).

### Prompt file: `docs/prompts/scenario/03_5_polish.md`

Structure mirrors `03_writing.md`:
- Inputs section: full `NarrationScript` JSON + Lever P's continuity
  rules (so polisher and writer share the same continuity contract).
- Edit-budget rule: polisher MUST NOT rewrite more than ~25% of any
  scene's narration. This is a smoothing pass, not a rewrite. Caps
  per scene enforced server-side via diff-rune-count check (see
  Validation below).
- Output: same `NarrationScript` JSON shape.

### Validation

- `internal/pipeline/agents/polisher.go` runs the existing
  `validator.Validate(script)` after the polish call.
- **New diff-budget validator**: for each scene, compute the
  rune-distance between pre-polish narration and post-polish narration.
  If any scene's edit ratio exceeds 25%, return `ErrValidation` with
  a clear message — polisher overstepped its smoothing mandate.
- Run `terms.MatchNarration` once on the polished script.

### State machine wiring

- New stage between `writer` and `critic`. Stage ID: `polisher`.
- Files affected:
  - `internal/pipeline/state_machine.go` (or wherever stage transitions
    are declared) — add `polisher` between `writer` and `critic`.
  - `internal/pipeline/runner.go` — slot wiring.
  - State persistence / resume logic — verify `polisher` can resume
    cleanly if the run dies after writer but before polisher emits.
    `state.Narration` from writer is the artifact; polisher overwrites
    on success.
- Audit logger: emit `text_generation` audit entry per polish call,
  matching the writer pattern (see `writer.go:295-306`).

### Config

- Polisher uses its own `TextAgentConfig`. Defaults: same model as
  writer, same provider, `MaxTokens` should be ≥ writer's (it emits
  the full script in one shot — likely 8k+ tokens). DashScope
  `qwen-max` may be the right tier here, NOT `qwen-plus`.
- Add to project config schema (`config.yaml` agent block).

### Concurrency

- Single LLM call. No `Concurrency` field needed beyond the inherited
  `TextAgentConfig.Concurrency` (which polisher will leave at default
  / unused).

---

## Open questions for plan step

1. **Slot position.** `writer → polisher → critic` (recommended) vs.
   `writer → critic → polisher` (lets critic feedback drive polish).
   Recommendation: polisher BEFORE critic — critic should review the
   final shipping artifact. If polish lands new defects, critic catches
   them; if polish runs after critic, critic's feedback is stale.

2. **Failure mode.** If polisher fails after retry, do we (a) fall
   back to writer's output unchanged and continue, or (b) fail the
   run? Recommendation: **(a) fall back with a `polisher_failed`
   audit event**. Polish is a quality lift, not a correctness gate.
   The writer output is already valid by construction.

3. **Diff-budget threshold (25%).** Empirical guess. May need tuning
   after first dogfood. Make it `domain.PolisherMaxEditRatio` constant,
   not a magic number.

4. **TTS reuse risk.** If polisher rewrites narration but downstream
   image-gen has already cached image prompts using pre-polish
   `narration_beats` text, there's a stale-cache risk. Verify image-gen
   stage runs AFTER polisher (it should — visual_breakdowner is post-
   writer / post-polisher in the pipeline).

5. **Cost.** One extra full-script LLM call per run (~8k input,
   ~5k output tokens at golden volume). Estimate $0.03–0.10 per run
   depending on model tier. Acceptable for a personal channel
   pipeline; document in audit log.

6. **Determinism for golden eval.** If golden eval reruns include
   polish, the eval's golden scoring drifts. Decide whether
   `golden_eval` runs polisher or pins to pre-polish narration.

---

## Touch surface (preview)

- `internal/pipeline/agents/polisher.go` — new file, ~200 LOC.
- `internal/pipeline/agents/polisher_test.go` — new file, tests:
  Happy / SchemaViolation / EditBudgetViolation / ForbiddenTerms /
  PolishFailedFallback / DoesNotMutateOnFailure.
- `docs/prompts/scenario/03_5_polish.md` — new prompt file.
- `internal/pipeline/runner.go` — slot wiring.
- `internal/pipeline/state_machine.go` (path may vary) — add stage.
- `internal/pipeline/phase_a_test.go` / `phase_a_integration_test.go` —
  thread the polisher slot through; prefer `agents.NoopAgent()` for
  paths that don't exercise polish behavior.
- `internal/domain/scenario.go` — add `PolisherMaxEditRatio` constant
  if used. No schema change.
- `config.yaml` schema and example — polisher agent block.
- `internal/pipeline/agents/retry.go` — already has `runWithRetry`,
  reuse without modification.

---

## Acceptance signals

1. `go test ./...` and `go test -race ./internal/pipeline/agents/...`
   clean.
2. Polisher slots into the pipeline cleanly: a clean SCP-049 dogfood
   run shows `audit.log` entries for `stage=polisher` with exactly
   one entry per run.
3. **Diff visualisation**: produce a side-by-side of pre-polish and
   post-polish narration for the dogfood run. The Act-N → Act-N+1
   first-scene rewrites must be visible. The Act-4 closer must be
   rewritten. Within-act bodies should be largely unchanged
   (≤25% rune diff per scene).
4. **HITL spot-check**: read end-to-end. Does the scenario now feel
   like ONE video vs. 4 stitched acts?
5. Polish-failed fallback path verified by injecting a generator
   error and confirming the run completes with writer's output
   unchanged + a `polisher_failed` audit event.
6. Cost delta from a clean dogfood within ±20% of estimate. If polish
   is consistently >$0.15 per run, revisit max_tokens or model tier.

---

## Out of scope for this cycle

- **Schema bumps**. Polish is text-only edits to existing fields.
  If you find yourself wanting a new field, that's Lever D territory.
- **Per-scene polish (multiple LLM calls)**. Single full-script call
  is the architectural simplification that makes this lever cheap.
- **Polish-driven critic re-run loop**. Critic runs once, on polished
  output. No feedback cycle.
- **Multi-pass polish** (smooth → tighten → final). One pass.
- **Image-gen / TTS integration changes**. Polish stays in narration
  scope.
- **Lever D (monologue-mode decoupling)**. Different prompt file
  (`next-session-monologue-mode-decoupling.md`).

---

## Reference — why C exists separately from B

Lever B (`summarizePriorAct` cascade) gives Acts 2/3/4 a 240-rune
tail of the prior act. That fixes the cross-act drift but not the
**polish** of the transition itself: the writer for Act N still
writes its first scene's opening line WITHOUT seeing Act N-1's
final scene's actual narration arc. B is a hint; C is a rewrite.

Similarly, B does nothing for the final closer because there's no
"Act 5" — the Act 4 writer call has no downstream feedback. Only a
post-merge agent that sees the whole arc can reliably rewrite the
closer to land as a video-ender.

This is why C cannot be replaced by tuning B harder.
