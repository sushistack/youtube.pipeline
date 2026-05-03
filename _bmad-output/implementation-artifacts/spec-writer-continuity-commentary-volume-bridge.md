---
title: 'Writer-stage quality lift: continuity + commentary + volume + cross-act bridge'
type: 'feature'
created: '2026-05-03'
status: 'done'
baseline_commit: '688f62797ea93cb28c4858002f978148eae4ce3e'
context:
  - '{project-root}/_bmad-output/planning-artifacts/next-session-scene-causal-linkage.md'
  - '{project-root}/_bmad-output/implementation-artifacts/spec-writer-per-act-fanout.md'
  - '{project-root}/_bmad-output/implementation-artifacts/spec-fewshot-exemplar-injection.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Pipeline-emitted narration sits at ~42% of golden-channel volume (~2300 vs ~5500 KR chars) and reads as four stitched scene capsules instead of one coherent video. Within an act, scene N+1 jumps without setup; final closers land on rhetorical questions; explainer/aside/numeric-fact narration modes the golden channel uses every video are absent; Acts 2/3/4 all receive Act-1's tail (not their actual prior act), so cross-act drift is real.

**Approach:** Combine three levers in one cycle — (P) writer-prompt rewrite enforcing within-act continuity, definite-state closer, narrator commentary modes, and numeric-fact surfacing; (P-cap) lift `ActNarrationRuneCap` values to close the volume gap; (B) `summarizePriorAct` cascade so each act receives its actual prior act's tail; (E) researcher → structurer prompt chain that propagates numeric facts into `key_points` so writer's Rule 6 has facts to surface. No schema changes; no new agents; one Go file edit, three prompt files edited.

## Boundaries & Constraints

**Always:**
- `domain.NarrationScript` / `domain.NarrationScene` / `domain.StructurerOutput.Acts[].KeyPoints` schemas unchanged.
- `summarizePriorAct` signature stays — only its caller and internal logic change to support the cascade.
- Lever B preserves the wall-clock budget of the per-act fan-out as much as possible: Act 1 → Act 2 stays serial; Act 3 receives Act 2's tail; Act 4 receives Act 3's tail. Acceptable additional serialization: at most one extra serial dependency edge (Act 2 → Act 3) — Acts 3 and 4 may run in parallel with each other but each must see its true predecessor's tail.
- The lifted caps in `ActNarrationRuneCap` must match the numbers stated in `03_writing.md` Pre-Output Self-Check; `prompt_lint_test` enforces placeholder coverage but does NOT pin cap numbers (verified at plan time), so no test churn there.
- Rule 5 (CTA closer) ships **without** `{channel_signature}` — empty/no template. The `unresolved` final scene MUST close on a definite-state closer (Rule 2). No CTA template is hardcoded.
- Lever E touches researcher (`01_research.md`) and structurer (`02_structure.md`) prompts only. No new placeholders, no schema fields. Numeric facts flow through the existing `key_points` array.
- All existing writer behaviors that survive — code-fence stripping, `ErrValidation` on JSON/schema/forbidden-term failures, no state mutation on failure, audit log emission, retry budget = 1 per act — continue to hold.

**Ask First:**
- If TTS pacing on dogfood at the new caps sounds rushed (>8.5 KR chars/sec), dial caps back ~15% and update both prompt and Go map.
- If Act 2 → Act 3 serialization breaches a wall-clock budget the user cares about, fall back to: each of Acts 2/3/4 receives Act-1 tail (current behavior), and document the regression to deferred work. Default: ship the cascade.

**Never:**
- Adding `{channel_signature}` placeholder to the writer template (Jay chose option (a) — empty default).
- New agent stages (polisher / smoother / etc — that's Lever C, separate prompt).
- Schema bumps to `NarrationScene`, `Act`, `StructurerOutput`, or `NarrationScript` (Lever D territory).
- Per-beat commentary as a standalone beat — beats drive image budget, commentary embeds in existing beats' narration.
- Touching critic, reviewer, or visual_breakdowner prompts/code.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Cascade happy path | Act 1 written → Act 2 receives Act-1 tail; Act 2 written → Acts 3 and 4 launched in parallel, Act 3 receives Act-2 tail, Act 4 receives Act-3 tail | All four acts emit valid responses; merged script has 10 scenes; cross-act transitions reflect each act's actual predecessor | N/A |
| Act 2 fails after retry | Act 2 writer call exhausts retry budget | Acts 3 and 4 NOT launched (no Act-2 tail to feed them); writer returns `ErrValidation`; `state.Narration` unchanged | Wrap as `writer: <err>: ErrValidation`; ctx propagation cancels in-flight Act-1-derived work |
| Act 3 fails after retry, Act 4 in flight | Act 3 launched first; Act 3 fails after retry; Act 4 still in flight | First error wins via errgroup; Act 4 ctx canceled; writer returns `ErrValidation`; state unchanged | Standard errgroup short-circuit |
| New Rule 1 violation in writer output | A scene's narration jumps without any of (a) physical, (b) causal, (c) bridge-token continuity | Writer prompt's Pre-Output Self-Check should catch most; if it slips through, no automated post-validator catches this — surfaces only at HITL spot-check | Out of scope: hard validator for continuity (would require semantic check; Lever C polisher is the right place) |
| Lifted cap exceeded by LLM | Scene's narration > new cap (e.g. mystery > 400 runes) | `validateWriterActResponse` rejects via `domain.ErrValidation`; per-act retry burns; if retry exhausts, writer returns ErrValidation | Existing path, no change |
| Researcher omits Numeric Facts section | Research packet has no numerics surfaced | Structurer's `key_points` lack numerics; writer's Rule 6 has nothing to surface; narration ships without numeric anchors | Soft fail: no automated check (researcher prompt is best-effort). Surfaces at HITL — accept for this cycle. |

</frozen-after-approval>

## Code Map

- `docs/prompts/scenario/03_writing.md` -- main writer prompt. Add Rule 1 (씬 간 연결), Rule 2 (closer discipline), Rule 4 (narrator commentary modes), Rule 6 (numeric fact injection); update cap numbers in §씬 단위 규칙 and §문장 & 페이싱 규칙; append corresponding Pre-Output Self-Check items.
- `docs/prompts/scenario/01_research.md` -- add a "Numeric Facts" required section (Lever E source).
- `docs/prompts/scenario/02_structure.md` -- add a directive: each non-incident act's `key_points` must include ≥2 entries that surface numerics from research's Numeric Facts section (Lever E propagation).
- `internal/domain/scenario.go` -- update `ActNarrationRuneCap` map: incident=120, mystery=400, revelation=520, unresolved=280. Update doc-comment with rationale.
- `internal/pipeline/agents/writer.go` -- restructure `NewWriter` orchestration so Acts 2 → {3,4} cascade per Lever B (see Design Notes); rewrite `summarizePriorAct` to accept *the actual prior act* not always Act 1; rename `priorActBeatRuneCap` if its semantics shift.
- `internal/pipeline/agents/writer_test.go` -- add `TestWriter_PerAct_PriorActSummary_Cascades` (Act 3 prompt contains Act 2 tail, Act 4 prompt contains Act 3 tail); update existing `TestWriter_PerAct_PriorActSummaryInjected` if its assertions assume Act-1-tail-everywhere; verify `TestWriter_PerAct_Concurrency` still holds under the new ordering (acceptable: max 2 in flight given Act 3, 4 parallel after Act 2).
- `internal/pipeline/agents/prompt_lint_test.go` -- read-only verify; no contract change since no new placeholders introduced.
- `testdata/contracts/writer_output.{schema,sample}.json` -- read-only; cap numbers don't appear in schema; no fixture rewrite.

## Tasks & Acceptance

**Execution:**
- [x] `internal/domain/scenario.go` -- update `ActNarrationRuneCap` to `{incident:120, mystery:400, revelation:520, unresolved:280}`; refresh doc-comment lines 168-181 with new rationale (volume parity vs golden, ≈70-80% wider than legacy).
- [x] `docs/prompts/scenario/03_writing.md` -- (a) update §씬 단위 규칙 cap-list (lines 55-59) to match new numbers; (b) update §문장 & 페이싱 규칙 cap reminder (line 98); (c) update Pre-Output Self-Check cap line (line 180); (d) add new subsection §씬 간 연결 규칙 between §씬 단위 규칙 and §필수 몰입 기법, mirroring the existing rule + ❌/✅ shape; whitelist bridge tokens: `"그리고 며칠 뒤"`, `"한편"`, `"그로부터 얼마 후"`, `"이후"`, `"같은 시각"`, `"다음 날"`; (e) add new subsection §Narrator Commentary Modes between §필수 몰입 기법 and §문장 & 페이싱 규칙 with the four modes (explainer / aside / speculation hook / numeric anchor) and act applicability (mystery / revelation / unresolved only — `incident` stays pure-impact); (f) replace Act 4 closer guidance at line 119: ❌ rhetorical question → ✅ definite-state closer; (g) add Rule 6 line under §콘텐츠 규칙: each non-incident act surfaces ≥2 numeric/ratio facts from `act_key_points`; (h) append four new Pre-Output Self-Check items per the planning artifact §Pre-Output Self-Check additions.
- [x] `docs/prompts/scenario/01_research.md` -- add a new required section between §4 and §5 titled "### 6. Numeric Facts": instruct the researcher to extract ≥6 numerics/ratios/counts from the source document, formatted as `- key: <fact> (source: <where>)`. Renumber existing §5 "Narrative Hooks" → §7 (or insert without renumber, plan-time choice — keep diff minimal).
- [x] `docs/prompts/scenario/02_structure.md` -- add directive after the JSON sample (around line 50) requiring each non-incident act's `key_points` to include ≥2 entries that surface numerics from research's "Numeric Facts" section. No JSON-shape change.
- [x] `internal/pipeline/agents/writer.go` -- restructure orchestration: Act 1 serial → Act 2 serial (depends on Act 1 tail) → Acts 3 and 4 parallel under errgroup (Act 3 receives Act 2 tail, Act 4 receives Act 3 tail; since Act 3 must finish before Act 4 has its tail, Act 4 also depends on Act 3 — see Design Notes for resolution). Rewrite `summarizePriorAct` to take the immediate prior act's `writerActResponse` (already its current parameter shape — only the *caller's choice of which response to pass* changes). Verify ctx cancellation propagates through all dependency edges.
- [x] `internal/pipeline/agents/writer_test.go` -- add `TestWriter_PerAct_PriorActSummary_Cascades`: capture Act 3's rendered prompt and assert it contains a substring of Act 2's last scene narration; capture Act 4's prompt and assert it contains a substring of Act 3's last scene narration. Add `TestWriter_PerAct_Cascade_FailsFastOnAct2`: inject Act 2 retry-exhausting failure, assert Acts 3 and 4 are never called (or short-circuit via ctx); existing concurrency test reviewed for compatibility with the new ordering.

**Acceptance Criteria:**
- Given a clean SCP-049 dogfood run after this spec lands, when the merged scenario is measured, then total KR chars across all scenes ≥4500 (≥82% of golden ~5500).
- Given the merged scenario, when an HITL reader tags every within-act adjacency as `physical | causal | bridged | jump`, then the count of `jump` adjacencies is 0.
- Given the merged scenario, when each non-incident act is read, then each commentary mode (explainer / aside / speculation hook / numeric anchor) appears at least twice across the scenario.
- Given the merged scenario, when the final scene of the `unresolved` act is read, then it ends on a declarative sentence (no rhetorical question).
- Given Acts 1 and 2 succeed and Act 2's last scene contains an identifiable narration tail, when Act 3's writer prompt is rendered, then it contains that tail (verified by unit test).
- Given Act 2 fails after retry exhaustion, when the writer runs, then Acts 3 and 4 are not invoked, `state.Narration` is unchanged, and the writer returns `domain.ErrValidation`.
- `go build ./...` succeeds; `go test ./internal/pipeline/agents/... ./internal/pipeline/...` is green; `go test -race ./internal/pipeline/agents/...` clean.

## Design Notes

**Cascade ordering (Lever B).** The current orchestration runs Act 1 serial → Acts 2/3/4 parallel, all three receiving Act-1's tail. The cascade requires each act to see its immediate predecessor. Pure cascade is fully serial (4× wall clock). Pragmatic compromise: Act 1 → Act 2 serial (already true), then Act 3 depends on Act 2's tail, Act 4 depends on Act 3's tail — also serial. Net: full serialization Acts 1→2→3→4, but each per-act call still parallelizes with image-side work in later stages. This is acceptable for narration quality; if wall-clock regression matters, the fallback (documented in Ask First) is to keep Acts 3/4 reading Act-1's tail and accept the partial drift.

**Why caps live in Go.** Validation at `validateWriterActResponse` ([writer.go:344-365](internal/pipeline/agents/writer.go#L344-L365)) reads from `domain.ActNarrationRuneCap`. The prompt's cap list is a hint to the LLM; the Go map is the enforcer. Both must change together — the planning artifact's claim that caps are prompt-only is incorrect.

**Why no `{channel_signature}` template.** Jay chose option (a). Empty CTA template means Rule 2's definite-state closer is the sole closer rule. If the channel sign-off becomes known later, a follow-up adds the placeholder — out of scope here.

**Lever E is best-effort.** No automated validator forces researcher to emit Numeric Facts or structurer to propagate them. The chain is prompt-only because this cycle's scope budget excludes adding `domain.NumericFact` schema. If the dogfood shows numeric anchors still missing, Lever C's polisher is the place to enforce (or a follow-up tightens with structured facts).

**Realistic ceiling.** Per planning artifact, prompt-only fixes top out at ~65-75% golden match. P+B+E together push toward the upper end of that range; the remaining gap is architectural (Lever C polish-pass for cross-act flow + closer; Lever D monologue mode for product identity). Both are queued as separate next-session prompts.

## Verification

**Commands:**
- `go build ./...` -- expected: success.
- `go test ./internal/pipeline/agents/...` -- expected: all writer tests green including `TestWriter_PerAct_PriorActSummary_Cascades` and `TestWriter_PerAct_Cascade_FailsFastOnAct2`.
- `go test -race ./internal/pipeline/agents/...` -- expected: clean (regression check on the new orchestration).
- `go test ./internal/pipeline/...` -- expected: phase_a / phase_a_integration green (writer slot is NoopAgent there; not affected by orchestration change).
- `go vet ./...` -- expected: clean.

**Manual checks:**
- Open `docs/prompts/scenario/03_writing.md` and verify the four new rule sections + four new Pre-Output items render coherently with the existing structure.
- Run a clean SCP-049 dogfood end-to-end. Diff the resulting `scenario.json` against `docs/scenarios/SCP-049.example2.md` for: total KR chars (≥4500), commentary mode coverage (≥2 per mode), within-act jump count (0), Act-4 closer (declarative).

## Suggested Review Order

**Cascade serialization (Lever B) — entry point**

- Serial loop replacing fan-out; each act passes its tail to the next
  [`writer.go:127`](../../internal/pipeline/agents/writer.go#L127)

- Tail condenser: bound to `priorActBeatRuneCap` runes, seeds Act 1 with ""
  [`writer.go:441`](../../internal/pipeline/agents/writer.go#L441)

**Volume caps (P-cap)**

- Go enforcer — both this map and the prompt must match; Go wins on violation
  [`scenario.go:179`](../../internal/domain/scenario.go#L179)

- Prompt cap list mirrors Go values (LLM hint, not enforcer)
  [`03_writing.md:55`](../../docs/prompts/scenario/03_writing.md#L55)

**Continuity & commentary rules (P)**

- 씬 간 연결 규칙: within-act (a)/(b)/(c) continuity + bridge-token whitelist
  [`03_writing.md:76`](../../docs/prompts/scenario/03_writing.md#L76)

- Narrator Commentary Modes (explainer / aside / speculation / numeric anchor)
  [`03_writing.md:120`](../../docs/prompts/scenario/03_writing.md#L120)

- Act 4 closer discipline: ❌ rhetorical → ✅ definite-state
  [`03_writing.md:166`](../../docs/prompts/scenario/03_writing.md#L166)

- Rule 6: ≥2 numeric facts per non-incident act from `act_key_points`
  [`03_writing.md:177`](../../docs/prompts/scenario/03_writing.md#L177)

**Numeric fact propagation (Lever E)**

- Researcher §5: ≥6 numerics extracted from source, no fabrication
  [`01_research.md:61`](../../docs/prompts/scenario/01_research.md#L61)

- Structurer Rule 9: propagate ≥2 per non-incident act into `key_points`
  [`02_structure.md:60`](../../docs/prompts/scenario/02_structure.md#L60)

**Tests**

- Cascade chain: Act 3 prompt contains Act 2 tail; Act 4 contains Act 3 tail
  [`writer_test.go:208`](../../internal/pipeline/agents/writer_test.go#L208)

- Serial invariant: maxInFlight=1 AND totalCalls=4 (both assertions required)
  [`writer_test.go:311`](../../internal/pipeline/agents/writer_test.go#L311)

- Fail-fast: Act 2 retry exhaustion → Acts 3/4 never invoked
  [`writer_test.go:336`](../../internal/pipeline/agents/writer_test.go#L336)
