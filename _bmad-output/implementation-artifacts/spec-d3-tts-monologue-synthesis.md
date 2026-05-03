---
title: 'D3 — TTS per-act / whole-script monologue synthesis (no audible act-boundary cuts)'
type: 'feature'
created: '2026-05-04'
status: 'draft'
context:
  - '_bmad-output/planning-artifacts/next-session-monologue-mode-decoupling.md'
  - '_bmad-output/implementation-artifacts/spec-d1-domain-types-and-writer-v2.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** v1 TTS synthesizes per-scene audio and concatenates with scene-boundary pauses, which is the audible scene-cut artifact the D epic exists to eliminate. After D1 lands, the monologue is one continuous text per act (and structurally one text across the whole script per A1 resolution), so per-scene synthesis is wrong-shape input.

**Approach:** Per plan resolution A2: try DashScope CosyVoice **single-call whole-script synthesis** first against the merged monologue (`acts[0].Monologue + " " + acts[1].Monologue + …`). If the provider rejects ~5500-rune input (rate limit, max-input-length), fall back to **intra-act sentence-boundary chunking** with shared voice continuity params so seams are inaudible. Act boundaries MUST NOT introduce audible pauses regardless of which path is taken — that is the whole point of D.

## Boundaries & Constraints

**Always:**
- DashScope CosyVoice is the only TTS provider (per `feedback_api_dashscope_only.md`). No SiliconFlow, no other Qwen channels.
- Act boundaries produce zero audible pause in the final assembled audio. (Spike validates by listening test — see Verification.)
- If chunking is required, chunks split on **Korean sentence-final punctuation** ("다.", "요.", "니다.", "?", "!" etc.) inside an act's monologue, NOT on act/beat boundaries.
- Chunking, if used, must reuse identical voice / speed / pitch params across chunks so the seam is inaudible.
- Reuse cycle-C 5-min HTTP timeout (commit `2ef1d3c`). Per-call audio at golden volume runs ~2.5min; 5-min ceiling has headroom.

**Ask First:**
- If single-call synthesis fails ≥3× on identical input (provider rejects whole script), HALT and confirm chunking strategy before implementing.
- If the listening test detects an audible seam at any sentence/act boundary, HALT — do not ship "almost continuous" audio.
- If DashScope rolls out a different long-form synthesis endpoint (`cosyvoice-v2`, etc.), HALT before adopting.

**Never:**
- No real-time / streaming TTS (out of scope per plan).
- No per-scene synthesis (deletes D's reason for existing).
- No multi-language support — Korean only (per plan).
- No beat-anchor-driven audio cuts. Beats are visual-only; audio is continuous.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected | Error Handling |
|---|---|---|---|
| Single-call synthesis succeeds | merged whole-script monologue ≤ provider input ceiling | one audio file per run; total duration ≈ rune_count / golden pacing | N/A |
| Single-call rejected (max-input) | provider 4xx | fall back to intra-act sentence chunking with shared voice params | log fallback reason |
| Chunked synthesis, all chunks succeed | `[]chunk` from sentence splitter | concatenated audio with inaudible seams | N/A |
| One chunk fails after retries | exhaust | whole TTS stage fails atomically; partial audio NOT persisted | `ErrStageFailed` |
| Provider rate-limit (429) | 429 with Retry-After | honor Retry-After; if absent, exponential backoff | retry → escalate |

</frozen-after-approval>

## Code Map

- `internal/pipeline/agents/tts.go` (path may be `tts/` package — confirm during planning) -- replace per-scene synthesis with whole-script-first / chunked-fallback path.
- `internal/pipeline/agents/tts_test.go` -- rewrite end-to-end; mock DashScope responses for both single-call success and chunked-fallback paths.
- `internal/audio/assembler.go` (or wherever audio concatenation lives) -- act-boundary pause insertion REMOVED; rely on continuous monologue input.
- `cmd/pipeline/serve.go` -- TTS config update if max_input length / new endpoint params surface.
- `docs/prompts/...` -- N/A (TTS doesn't use LLM prompts).
- Korean sentence splitter -- new helper if not already present (`internal/text/koreansplit.go` or similar). Splits on KR sentence-final punctuation; preserves trailing whitespace for seam continuity.

## Tasks & Acceptance

**Execution:**
- [ ] Provider spike: hit DashScope CosyVoice with a 5500-rune Korean input (use SCP-049 D1 dogfood monologue). Measure: success / rejection / latency. Document result in spec change log.
- [ ] `internal/text/koreansplit.go` -- KR sentence splitter (only if spike says chunking needed).
- [ ] `internal/pipeline/agents/tts.go` -- whole-script-first path; chunked fallback (only if spike says needed).
- [ ] `internal/audio/assembler.go` -- remove act-boundary pause insertion.
- [ ] `internal/pipeline/agents/tts_test.go` -- rewrite end-to-end, both paths.
- [ ] `cmd/pipeline/serve.go` -- TTS config alignment.
- [ ] Unit-test every row of the I/O matrix.

**Acceptance Criteria:**
- Given an SCP-049 dogfood post-D1/D2, when TTS runs, then exactly one audio file is produced for the run with duration ≥ act-monologue-rune-count / golden-pacing-rate.
- Given the same audio, when listened at v1's known act-boundary timestamps, then there is no audible pause / voice-change / seam.
- Given `go test ./...` and `go test -race ./...`, then all green.
- Given a forced single-call rejection (test fixture), when TTS runs, then chunked fallback produces the same total duration with inaudible seams.

## Design Notes

The spike-first execution order is deliberate. The plan flags this risk explicitly: "Verify the TTS provider handles 2.5-minute synthesis in one call. If not, intra-act chunking becomes necessary." Implementing chunking before knowing whether it's needed is dead-layer risk. Spike result drives the actual implementation scope.

If single-call works, the chunker (`koreansplit.go`) is not implemented in this spec — only stubbed for future use if provider behavior changes.

## Verification

**Commands:**
- `go build ./...` + `go test ./...` + `go test -race ./internal/pipeline/agents/...`
- Phase A SCP-049 dogfood through TTS; inspect generated audio file.

**Manual checks:**
- HITL listening test: blind-listen v2 audio at v1's act-boundary timestamps. Acceptance: no audible cut. (Per plan acceptance signal #3.)
- HITL listening test against `docs/exemplars/scp-049-hada.txt` audio reference (if available): time-to-distinguish ≥20s for v2 dogfood (plan acceptance signal #5 final form). Sub-20s but ≥5s indicates D shipped but not fully delivered; <5s = retro the lever.
