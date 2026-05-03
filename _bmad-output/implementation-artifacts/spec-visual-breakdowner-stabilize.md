---
title: 'Visual breakdowner: per-scene retry budget + transition contract tightening'
type: 'bugfix'
created: '2026-05-03'
status: 'done'
context: []
baseline_commit: '856a7bb60ce478ed8dbc0191c79cec2e16ed331f'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Every Phase A dogfood since `v1.2-roles` shipped fails at `visual breakdowner: scene N shot 1 invalid transition "": validation error`. The LLM drops the `transition` field on roughly 1-of-16 scenes per run, and the stage has zero per-scene retry budget so a single drop kills the run. Writer succeeds; only this stage blocks dogfood end-to-end.

**Approach:** Mirror writer's per-call retry pattern in visual_breakdowner — 1 retry per scene on JSON-decode or schema-validation failure, then surface the original error. Tighten the prompt so `transition` is explicitly mandatory and the allowed enum is impossible to miss. Add a negative-then-positive test that proves retry recovers. Same cycle removes the temporary `writer_debug.json` patch (`388bb03`) since scenario.json will once again be reliably written end-to-end.

## Boundaries & Constraints

**Always:**
- Per-scene retry budget = 1 (mirrors `writerPerActRetryBudget`). On second failure, return the last attempt's error wrapping `domain.ErrValidation`.
- Retry triggers on JSON-decode OR schema-validation failure. Generator-layer errors (network, context) propagate immediately and do NOT consume the retry, mirroring writer.
- Retries reuse the **same** prompt — no quality-feedback augmentation.
- Logging mirrors writer's key/value shape: `Info("visual breakdowner attempt start"|"complete"|"retry")` with `run_id`, `scene_num`, `attempt`, and (on retry) `reason="json_decode"|"schema_validation"`. No-op when `cfg.Logger == nil`.
- Prompt rules section makes `transition` non-skippable: an explicit "MUST be present" rule plus a standalone, prominent enum line (`ken_burns`/`cross_dissolve`/`hard_cut`).
- `BuildFrozenDescriptor`, `ShotCountForDuration`, `EnsureFrozenPrefix`, and the per-scene `errgroup` fan-out stay untouched.

**Ask First:**
- If the audit-log path needs to change (it currently logs once before validation, so a retry would emit twice), confirm whether duplicate audit entries on retry are acceptable before duplicating.
- If the prompt edit touches anything beyond the transition contract — frozen-prefix verbatim rule, output-contract example, allowed-enum surface — STOP. That belongs to deferred Goal 2.

**Never:**
- Decode-time fallback to a default transition. Silent defaults hide LLM regressions and bias drops to the most boring transition.
- Retry budget > 1. One retry catches transient drops; more masks broken prompts.
- Touching `internal/api/`, `internal/service/`, `web/`, image-gen, or TTS.
- Changing `ShotCountForDuration`, beat alignment, `narration_beats`, or `EnsureFrozenPrefix` semantics — deferred Goal 2.
- Modifying writer prompt or fewshot exemplars.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|---------------|---------------------------|----------------|
| Happy path | LLM returns valid shots first try | Scene built; per-scene call count == 1; no retry log | N/A |
| Empty `"transition":""` then valid | First response has empty transition; second is valid | Retry triggers; scene built from second; per-scene call count == 2; one retry log with `reason="schema_validation"` | N/A |
| Disallowed enum (`"zoom"`) then valid | First has invalid transition; second is valid | Same as above | N/A |
| Both attempts fail validation | Two invalid responses | Stage returns `domain.ErrValidation`-wrapped error from last attempt; `state.VisualBreakdown` unchanged | Wrap and return |
| Both attempts JSON-decode fail | Two non-JSON responses | Stage returns wrapped JSON-decode error from last attempt | Wrap and return |
| Transport error first call | `gen.Generate` returns network error | Propagate immediately; retry budget unconsumed (call count == 1) | Return error as-is |
| Context cancelled mid-retry | Caller cancels between attempts | Goroutine returns `ctx.Err()`; errgroup unwinds | Return ctx error |

</frozen-after-approval>

## Code Map

- `internal/pipeline/agents/visual_breakdowner.go` -- runner. Wrap the `gen.Generate` + decode + validate block (lines ~103-132) in a retry loop following [writer.go:234-334](internal/pipeline/agents/writer.go#L234-L334).
- `internal/pipeline/agents/writer.go` -- reference for the retry pattern and logger shape (`writerPerActRetryBudget` constant at line 63).
- `docs/prompts/scenario/03_5_visual_breakdown.md` -- prompt template. Tighten the Rules section (lines 48-56) only. Do NOT touch the frozen-prefix rule (line 52) or output example (lines 38-46) — Goal 2.
- `internal/pipeline/agents/visual_breakdowner_test.go` -- add retry tests. Existing `sequenceTextGenerator` (line 283) routes by parsing "Scene N" from the prompt; extend it to support a per-scene response queue (multiple responses popped in order for the same scene number) or add a sibling generator.
- `internal/pipeline/phase_a.go` -- cleanup. Delete `writeWriterDebug` (lines ~489-513) and its call site (lines ~256-265, including comment block). Verify imports stay valid.

## Tasks & Acceptance

**Execution:**
- [x] `internal/pipeline/agents/visual_breakdowner.go` -- add `visualBreakdownerPerSceneRetryBudget = 1` const; refactor the per-scene goroutine body to a `for attempt := 0; attempt <= visualBreakdownerPerSceneRetryBudget; attempt++` loop. Retry on JSON-decode and schema-validation failures only; transport errors propagate. Mirror writer's `cfg.Logger.Info` shape.
- [x] `docs/prompts/scenario/03_5_visual_breakdown.md` -- tighten Rules so `transition` is explicitly mandatory; the allowed enum (`ken_burns`/`cross_dissolve`/`hard_cut`) appears as a standalone prominent line. Keep all other rules and the output-contract example unchanged.
- [x] `internal/pipeline/agents/visual_breakdowner_test.go` -- add three tests: `RetriesOnEmptyTransition`, `RetriesOnInvalidTransition`, `GivesUpAfterOneRetry`. First two: feed two responses per scene (invalid then valid); assert success + per-scene call count == 2 + one retry log. Third: feed two invalid responses; assert `errors.Is(err, domain.ErrValidation)` + per-scene call count == 2. Use a per-scene response queue.
- [x] `internal/pipeline/phase_a.go` -- delete `writeWriterDebug` and its invocation block. Drop newly-unused imports if any.

**Acceptance Criteria:**
- Given a scene whose first LLM response has `transition: ""`, when visual_breakdowner runs, then it retries once and completes when the second response is valid.
- Given a scene whose first LLM response has an invalid enum (`"zoom"`), when visual_breakdowner runs, then it retries once and completes when the second response is valid.
- Given a scene where both attempts fail validation, when visual_breakdowner runs, then it returns an error wrapping `domain.ErrValidation` and `state.VisualBreakdown` is unchanged.
- Given a transport error on the first call, when visual_breakdowner runs, then the error propagates without consuming the retry (per-scene call count == 1).
- Given the cleanup is complete, when `grep -rn "writer_debug\|writeWriterDebug" internal/` runs, then it returns zero matches.
- Given `go test ./internal/pipeline/...` runs, then it passes.

## Spec Change Log

## Verification

**Commands:**
- `go build ./...` -- expected: clean build.
- `go test ./internal/pipeline/agents/... -run VisualBreakdowner` -- expected: all tests pass including the three new retry tests.
- `go test ./internal/pipeline/...` -- expected: pass.
- `grep -rn "writer_debug\|writeWriterDebug" internal/` -- expected: zero matches.

**Manual checks:**
- Resume the failing SCP-049 dogfood from the writer-completed run; confirm visual_breakdowner reaches the end of all scenes with at most one "visual breakdowner retry" log line per scene (zero is the target after prompt tightening).

## Suggested Review Order

**Retry mechanism (entry point)**

- Const + loop bound — single source of truth for "one retry per scene".
  [`visual_breakdowner.go:27`](../../internal/pipeline/agents/visual_breakdowner.go#L27)

- Goroutine body now delegates to the helper; errgroup fan-out unchanged.
  [`visual_breakdowner.go:98`](../../internal/pipeline/agents/visual_breakdowner.go#L98)

- Retry helper — mirrors `runWriterAct`; transport errors propagate, decode/schema failures retry.
  [`visual_breakdowner.go:119`](../../internal/pipeline/agents/visual_breakdowner.go#L119)

- The retry loop itself, with per-attempt logging keyed by `scene_num`/`attempt`/`reason`.
  [`visual_breakdowner.go:137`](../../internal/pipeline/agents/visual_breakdowner.go#L137)

**Prompt contract (root-cause prevention)**

- New presence rule — drops/empty strings are now an explicit contract violation.
  [`03_5_visual_breakdown.md:53`](../../docs/prompts/scenario/03_5_visual_breakdown.md#L53)

- Enum line elevated to a standalone prominent rule.
  [`03_5_visual_breakdown.md:54`](../../docs/prompts/scenario/03_5_visual_breakdown.md#L54)

**Test coverage**

- Empty-transition recovery — proves retry actually fires and the second response wins.
  [`visual_breakdowner_test.go:239`](../../internal/pipeline/agents/visual_breakdowner_test.go#L239)

- Invalid-enum recovery — same shape, different failure mode.
  [`visual_breakdowner_test.go:260`](../../internal/pipeline/agents/visual_breakdowner_test.go#L260)

- Transport error path — propagates without consuming retry budget (call count == 1).
  [`visual_breakdowner_test.go:278`](../../internal/pipeline/agents/visual_breakdowner_test.go#L278)

- Retry-exhausted boundary — two failures → `ErrValidation`, state untouched.
  [`visual_breakdowner_test.go:305`](../../internal/pipeline/agents/visual_breakdowner_test.go#L305)

- Test-only generators — per-scene FIFO queue and per-scene error injection.
  [`visual_breakdowner_test.go:429`](../../internal/pipeline/agents/visual_breakdowner_test.go#L429)

**Cleanup (debug-dump retirement)**

- Dump call site removed from the run loop now that scenario.json is reliable end-to-end.
  [`phase_a.go:251`](../../internal/pipeline/phase_a.go#L251)
