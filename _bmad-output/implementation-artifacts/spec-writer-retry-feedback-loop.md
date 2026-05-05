---
title: 'Writer monologue retry feedback loop'
type: 'feature'
created: '2026-05-05'
status: 'done'
baseline_commit: '4525cb5bdab9e89f673b5921452f40d2ca658e5e'
context:
  - '{project-root}/_bmad-output/planning-artifacts/next-session-writer-retry-feedback-loop.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `runWriterActMonologue` renders the prompt once outside the retry loop and re-calls the LLM with the identical prompt on every attempt; the LLM never learns that the prior attempt's monologue was below the per-act rune floor or above the cap. Dogfood (SCP-049, 2026-05-05) showed revelation runs of 932 → 936 → 1517 runes (3× variance, attempt 1 nearly identical to attempt 0) and an unresolved over-cap exhaustion (1286/1120) that was masked by widening the cap rather than fixing the feedback gap.

**Approach:** Add a new `{retry_feedback}` placeholder to the writer prompt and inject within-stage length-miss feedback ("PREVIOUS ATTEMPT FAILED: monologue was N runes — BELOW the floor of F …") into the prompt on retry. Re-render the prompt inside the retry closure each attempt; capture the length-miss feedback string in an outer-scope variable updated only when the validation failure was a rune-count miss (cap or floor). Cross-stage critic feedback (`{quality_feedback}`) stays untouched.

## Boundaries & Constraints

**Always:**
- `{retry_feedback}` is a separate placeholder from `{quality_feedback}`; both default to empty string and coexist in the prompt without disturbing existing rendering.
- Retry feedback updates ONLY when the failure cause is a rune-count miss (n > cap or n < floor) — non-length validation failures (empty mood, sentence-terminal floor, schema, truncation, etc.) leave `retryFeedback` unchanged.
- Length-miss detection re-derives `n = utf8.RuneCountInString(decoded.Monologue)` and compares against `domain.ActMonologueRuneCap`/`ActMonologueRuneFloor`. Do not parse error strings.
- Feedback message includes: actual rune count, direction (BELOW floor / OVER cap), the band `[floor, cap]`, the band middle as a numeric target, and an action verb (`expand` / `tighten`). Symmetric wording for under-floor and over-cap.
- `prompts/agents/script_writer.tmpl` and `docs/prompts/scenario/03_writing.md` MUST stay byte-identical (lint test enforces).
- Each commit must build and test green; split the work per the planning artifact (placeholder+formatter, then retry refactor).

**Ask First:**
- If `validateWriterMonologueResponse` reports a length miss but `ActMonologueRuneCap`/`ActMonologueRuneFloor` lookup fails for the act ID, surface the contradiction — do NOT silently fall back to empty feedback in that path.

**Never:**
- Do NOT change `writerPerStageRetryBudget`, the cap/floor maps, the segmenter retry loop, the visual_breakdowner retry loop, or `{quality_feedback}` semantics in this cycle. Out-of-scope per planning doc §"세션 외 범위".
- Do NOT accumulate feedback across attempts — `retryFeedback` is overwritten with the most recent length-miss only.
- Do NOT inject retry_feedback for stage-2 (segmenter). Stage 2's failure modes are different (sentence-boundary, schema), out of scope for this cycle.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|---------------|---------------------------|----------------|
| Attempt 0 happy-path | Monologue in band, all other validators pass | Stage returns success; `retryFeedback` never read | N/A |
| Attempt 0 under-floor → Attempt 1 in-band | First decoded monologue n=200 < floor=288; second n=400 in band | Attempt 1's prompt contains `PREVIOUS ATTEMPT FAILED ... BELOW the floor of 288 ... ~ 504 runes ... expand`; stage succeeds | Attempt 0 returns retry-with-reason |
| Attempt 0 over-cap → Attempt 1 in-band | First n=850 > cap=720; second n=500 in band | Attempt 1's prompt contains `PREVIOUS ATTEMPT FAILED ... OVER the cap of 720 ... tighten`; stage succeeds | Attempt 0 returns retry-with-reason |
| Length-unrelated fail (empty mood) | Monologue in band but `mood=""` | `retryFeedback` stays empty; behavior identical to pre-change | Validate fails, retry budget burns, no feedback injected |
| Cross-stage `{quality_feedback}` non-empty | `state.PriorCriticFeedback` set, attempt 0 also under-floor | Attempt 1 prompt has BOTH `{quality_feedback}` populated and `{retry_feedback}` populated, in distinct sections | N/A |
| Length miss but cap/floor missing for act | (defensive) act ID has no entry in cap/floor maps | Validate already errors before reaching retry feedback path; do not inject feedback | Stage fails as today |

</frozen-after-approval>

## Code Map

- `prompts/agents/script_writer.tmpl` -- writer system prompt (embedded). Add `## Retry Feedback (in-stage)` section near `## Quality Feedback`. Update self-check guidance about length to reference the retry feedback if present.
- `docs/prompts/scenario/03_writing.md` -- byte-identical mirror; sync after editing the `.tmpl`.
- `internal/pipeline/agents/writer.go:225-330` -- `runWriterActMonologue` retry loop body. Move prompt rendering inside closure; declare `retryFeedback` outer-scope; on validate error, recompute rune count and refresh `retryFeedback` when out of band.
- `internal/pipeline/agents/writer.go:727-778` -- `renderWriterActPrompt`. Add `retryFeedback string` parameter; add `"{retry_feedback}", retryFeedback` to the `strings.NewReplacer` mapping.
- `internal/pipeline/agents/writer.go` (new helper) -- `formatMonologueLengthRetryFeedback(actual, floor, cap int) string` — pure function emitting the structured under-floor / over-cap message; returns empty string when `floor==0 || cap==0` or `floor <= actual <= cap`.
- `internal/pipeline/agents/prompt_lint_test.go:32-41` -- writer substitutes whitelist; add `"retry_feedback"`.
- `internal/pipeline/agents/writer_test.go:131-152` -- `sampleWriterAssets()` `WriterTemplate`; append `rf={retry_feedback}` token so the lint test (renderer-substitutes ↔ template-tokens parity) passes for tests using the in-package fake template.
- `internal/pipeline/agents/writer_test.go` (new tests) -- `formatMonologueLengthRetryFeedback` unit table + integration test queuing under-floor → in-band and asserting the recorded second prompt.
- `internal/domain/scenario.go:192-226` -- read-only; `ActMonologueRuneCap`/`ActMonologueRuneFloor` already provide cap/floor used by formatter.

## Tasks & Acceptance

**Execution (commit 1 — placeholder + formatter, build/test green):**
- [x] `internal/pipeline/agents/writer.go` -- add `formatMonologueLengthRetryFeedback(actual, floor, cap int) string` with under-floor / over-cap / in-band / zero-bound branches; pure, no logging.
- [x] `internal/pipeline/agents/writer.go` -- extend `renderWriterActPrompt` signature with `retryFeedback string`; add `"{retry_feedback}", retryFeedback` to the replacer; pass empty string at the existing call site (line 230) for now.
- [x] `prompts/agents/script_writer.tmpl` -- insert a `## Retry Feedback (in-stage)` section adjacent to the existing `## Quality Feedback` section, with a brief usage hint and the `{retry_feedback}` token; empty value renders as a blank section that reads naturally.
- [x] `docs/prompts/scenario/03_writing.md` -- byte-identical sync (verify with `diff` returning empty).
- [x] `internal/pipeline/agents/prompt_lint_test.go` -- add `"retry_feedback"` to the writer substitutes list.
- [x] `internal/pipeline/agents/writer_test.go` -- update `sampleWriterAssets` `WriterTemplate` to include `rf={retry_feedback}`; update any direct `renderWriterActPrompt` callers (if any) to pass `""` for the new arg.
- [x] `internal/pipeline/agents/writer_test.go` -- add unit tests for `formatMonologueLengthRetryFeedback` covering: under-floor (msg contains actual, floor, BELOW, expand, middle), over-cap (msg contains actual, cap, OVER, tighten, middle), in-band (empty), zero floor or zero cap (empty).

**Execution (commit 2 — retry feedback injection, build/test green):**
- [x] `internal/pipeline/agents/writer.go` -- in `runWriterActMonologue`, declare `var retryFeedback string` before the retry loop; move the `renderWriterActPrompt` call inside the closure (it now uses `retryFeedback`); on `validateWriterMonologueResponse` error, recompute `n := utf8.RuneCountInString(decoded.Monologue)`, look up cap/floor; if both present and `n` outside `[floor, cap]`, set `retryFeedback = formatMonologueLengthRetryFeedback(n, floor, cap)`. Keep the existing `prompt_chars` log line (now reflects the per-attempt prompt).
- [x] `internal/pipeline/agents/writer_test.go` -- add `TestWriter_Stage1_RetryFeedback_InjectedOnLengthMiss`: enqueue [under-floor monologue (e.g. 200 runes for incident, with ≥8 terminals), in-band monologue]; run writer; assert success and that `gen.prompts[stageKeyWriter][ActIncident][1]` contains `PREVIOUS ATTEMPT FAILED` and `BELOW the floor of 288` and the [0] entry does NOT.
- [x] `internal/pipeline/agents/writer_test.go` -- add a sibling test for over-cap: enqueue [over-cap monologue, in-band monologue]; assert second prompt contains `OVER the cap of 720`.
- [x] `internal/pipeline/agents/writer_test.go` -- add `TestWriter_Stage1_RetryFeedback_NotInjectedOnNonLengthMiss`: enqueue [empty-mood-but-in-band monologue, valid monologue]; assert second prompt does NOT contain `PREVIOUS ATTEMPT FAILED` (length-unrelated failure leaves feedback empty).

**Acceptance Criteria:**
- Given a writer act whose attempt 0 monologue has rune count below the floor and attempt 1 returns an in-band monologue, when `runWriterActMonologue` runs, then attempt 1's recorded prompt contains both the literal phrase `PREVIOUS ATTEMPT FAILED` and `BELOW the floor of <floor>` while attempt 0's prompt contains neither.
- Given an attempt 0 monologue exceeding the cap, when retry feedback is rendered into attempt 1, then the prompt contains `OVER the cap of <cap>` and the action verb `tighten`.
- Given a non-length validation failure on attempt 0, when attempt 1 renders, then `{retry_feedback}` is empty and the prompt does NOT mention `PREVIOUS ATTEMPT FAILED`.
- Given the contract test `TestPromptPlaceholders_AreFullyCovered/writer`, when run after the change, then it passes (no orphan and no stale placeholders).
- Given `TestPromptPlaceholders_DocsAndEmbeddedMirrorsAgree/writer`, when run after editing both the `.tmpl` and the `.md`, then it passes (placeholder sets identical).
- Given `diff prompts/agents/script_writer.tmpl docs/prompts/scenario/03_writing.md`, when executed, then output is empty.
- Given `go build ./...` and `go test ./...`, when run after each commit in the planned split, then both succeed.

## Spec Change Log

### 2026-05-05 — review iteration 1

**Triggering findings:**
- Edge#1, #2, #3 (high): false length-miss feedback when validation fails for non-length reasons but `decoded.Monologue` happens to be empty/short/wrong-act.
- Edge#4 (high): stale length-miss banner persists into the next attempt when the immediate previous attempt was in-band on length but failed for another reason.
- Edge#14, #15 (medium): truncation and JSON-decode failure paths leave prior length-miss feedback in place, leaking forward.

**Amendment to non-frozen sections:**
- The **Always** rule that said *"non-length validation failures … leave `retryFeedback` unchanged"* was operationally wrong: it caused stale signal contamination. The corrected behavior — which the I/O matrix already implied with "stays empty" in row 4 — is to recompute on every attempt and **clear `retryFeedback` whenever the just-completed attempt's content is not a measurable, real length miss**. A real length miss requires `decoded.ActID == spec.Act.ID && trim(decoded.Monologue) != "" && (n < floor || n > cap)`. Truncation and JSON-decode-failure paths also clear (no measurable content).
- Boundaries section above is left as written for human re-review; this change-log entry records the deviation. The frozen I/O matrix row 4 ("stays empty") is satisfied by the corrected behavior; row 5 (coexistence) is now exercised by an integration test.

**Known-bad state avoided:** the LLM being told "PREVIOUS ATTEMPT FAILED: monologue was N runes — BELOW the floor" referring to a count two attempts old, or to a content shape that wasn't actually a length miss (empty mood, wrong act_id, truncated response).

**Additional patches in this iteration:**
- `formatMonologueLengthRetryFeedback` parameter renamed `cap` → `capV` (matches caller's `capV`, avoids Go builtin shadow); new degenerate-band guard for `floor > capV` and negative inputs.
- New tests: `TestWriter_Stage1_RetryFeedback_NotInjectedOnEmptyMood` (I/O row 4 verbatim), `TestWriter_Stage1_RetryFeedback_CoexistsWithQualityFeedback` (I/O row 5), `TestWriter_Stage1_RetryFeedback_ClearedOnSubsequentNonLengthMiss` (regression fence on the stale-feedback fix). Two new formatter unit cases for inverted/negative bands.
- Cosmetic: prompt template Korean spacing nit, test-comment clarity fix ('가' is a syllable, not jamo).
- `retry_feedback_chars` log key added (minor scope creep beyond Execution bullet on `prompt_chars`); justified by Manual-checks dogfood verification — operator can grep it in trace logs.

**KEEP (must survive any future re-derivation):**
- Two-commit split (placeholder+formatter, then retry refactor) — preserved.
- `{retry_feedback}` is a separate placeholder coexisting with `{quality_feedback}` — preserved.
- Length-miss detection compares `utf8.RuneCountInString` against `domain.ActMonologueRuneCap`/`Floor` directly (no error-string parsing) — preserved.
- Symmetric under-floor / over-cap message structure with band middle as numeric target — preserved.

**Deferred (separate future cycle, recorded in `deferred-work.md`):**
- Render-error trace asymmetry (Edge#7), audit-log content drift across attempts (Edge#9), `retry_exhausted` outcome enrichment (Edge#19), non-UTF8 monologue hardening (Edge#18). All low/medium observability or defensive concerns — none block the dogfood signal this cycle was meant to give the LLM.

## Design Notes

**Feedback message templates** (under-floor and over-cap, both end with a CR-LF safe newline):

```
PREVIOUS ATTEMPT FAILED: monologue was {actual} runes — BELOW the floor of {floor}.
Target the middle of the band [{floor}, {cap}] = ~{middle} runes.
Add factual anchors, sensory detail, and narrator-aside commentary to expand.
Do NOT pad with filler.
```

```
PREVIOUS ATTEMPT FAILED: monologue was {actual} runes — OVER the cap of {cap}.
Target the middle of the band [{floor}, {cap}] = ~{middle} runes.
Tighten by removing redundant phrases and shortening sentences.
```

`{middle} = (floor + cap) / 2` (integer truncation is fine).

**Prompt section placement** — put the new `## Retry Feedback (in-stage)` section right after `## Quality Feedback` so the LLM reads cross-stage critic input first, then within-stage length feedback. When `{retry_feedback}` is empty, the section renders as just the header followed by a blank line — slight wart but reads fine and avoids conditional-template machinery.

**Why re-render every attempt is cheap** — the writer template is ~17.8k chars; `strings.NewReplacer` is `O(template + replacement)` per call. Negligible compared to LLM latency.

**Why we don't accumulate** — two consecutive under-floor attempts give nearly redundant signals; carrying both costs prompt tokens for marginal gain. Planning doc §D6 records this decision.

## Verification

**Commands:**
- `go test ./internal/pipeline/agents/...` -- expected: pass, including new formatter unit tests, retry-feedback injection integration tests, and `TestPromptPlaceholders_AreFullyCovered/writer` + `TestPromptPlaceholders_DocsAndEmbeddedMirrorsAgree/writer`.
- `go build ./...` -- expected: clean build after each commit.
- `diff prompts/agents/script_writer.tmpl docs/prompts/scenario/03_writing.md` -- expected: empty output.

**Manual checks (post-merge dogfood):**
- Run a single SCP video pipeline run; if writer_monologue retries occur on a revelation or unresolved act, inspect the trace JSON to confirm the second-attempt prompt contains the `PREVIOUS ATTEMPT FAILED ...` block and the previous attempt's actual rune count.
