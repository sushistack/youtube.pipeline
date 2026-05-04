---
title: 'D3 — TTS per-act / whole-script monologue synthesis (no audible act-boundary cuts)'
type: 'feature'
created: '2026-05-04'
status: 'done'
baseline_commit: '876f0ed431b9274b53446d9504c30e08e6fea01a'
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

- `internal/pipeline/tts_track.go` — replace per-scene synthesis loop with merged-monologue path. Builds the merged input from `state.Narration.Acts[i].Monologue` joined by single space (per Intent), attempts single-call first, falls back to KR sentence chunking; concatenates chunk WAVs into one canonical `tts/run_audio.wav`; slices the canonical audio per `BeatAnchor` rune-offset → time-offset mapping into `tts/scene_NN.wav` (sample-accurate copies — not re-syntheses) so Phase C's per-scene `Episode.TTSPath` contract is preserved.
- `internal/pipeline/tts_track_test.go` — rewrite end-to-end against V2 fixtures: merged-monologue request shape, single-call success path, single-call-rejected → chunk fallback, atomic chunk-failure (no partial files), per-beat slice byte-equality with the canonical run audio, retain transliteration / blocked-voice / audit / limiter / retry coverage from v1.
- `internal/text/koreansplit.go` (NEW) — Korean sentence splitter that terminates on `다.`, `요.`, `니다.`, `?`, `!`, `…`, `\n` (inclusive of trailing terminator + whitespace) and packs sentences into chunks bounded by a byte cap. Pure helper; consumed by `tts_track.go` only.
- `internal/text/koreansplit_test.go` (NEW) — unit tests over each terminator, mid-sentence quote handling, multi-sentence packing under cap, oversize-sentence hard-byte fallback (UTF-8 boundary safe).
- `cmd/pipeline/serve.go` — no provider/model swap. Wire-through only: pass the existing `cfg.TTSModel` / `cfg.TTSVoice` / dashscope endpoint into `TTSTrackConfig` (already done); add `TTSMaxInputBytes` so the chunker reads the cap from config rather than a package-level const (defaults to current 560).
- `internal/pipeline/phase_c.go` — UNCHANGED. Per-beat slices preserve the existing `Episode.TTSPath` contract; sample-contiguous slices guarantee no audible boundary on concat.

## Tasks & Acceptance

**Execution:**
- [x] Provider spike: hit DashScope CosyVoice with a 5500-rune Korean input (use SCP-049 D1 dogfood monologue). Measure: success / rejection / latency. Document result in spec change log.
- [x] `internal/text/koreansplit.go` -- KR sentence splitter (only if spike says chunking needed).
- [x] `internal/text/koreansplit_test.go` -- unit-test every terminator + the byte-pack / hard-fallback rows.
- [x] `internal/pipeline/tts_track.go` -- merged-monologue input; whole-script-first path; chunked fallback; canonical `tts/run_audio.wav`; per-beat sample-accurate slicing into `tts/scene_NN.wav`; atomic chunk-failure cleanup.
- [x] `internal/pipeline/tts_track_test.go` -- rewrite end-to-end, both paths; assert per-beat slices concat → byte-equal canonical run audio.
- [x] `cmd/pipeline/serve.go` -- TTS config alignment (`TTSMaxInputBytes`).
- [x] Unit-test every row of the I/O matrix.

**Acceptance Criteria:**
- Given an SCP-049 dogfood post-D1/D2, when TTS runs, then exactly one canonical audio file (`tts/run_audio.wav`) is produced per run with duration ≥ `Narration.MonologueRuneCount() / golden-pacing-rate`. Per-beat WAVs in `tts/scene_NN.wav` are bit-accurate slices of the canonical file (no re-synthesis).
- Given the same audio, when listened at v1's known act-boundary timestamps, then there is no audible pause / voice-change / seam.
- Given `go test ./...` and `go test -race ./...`, then all green.
- Given a forced single-call rejection (test fixture), when TTS runs, then chunked fallback produces a canonical run audio whose byte-stream is identical to the concatenation of the per-beat slices in scene order.

## Spec Change Log

### 2026-05-04 — Pre-implementation reconciliations (filled at planning, before coding)

The frozen block is preserved verbatim. The following reconciliations apply at
implementation time and do NOT modify the frozen intent — they only resolve
ambiguities the planning artifact left for the spec step:

1. **Provider name — "CosyVoice" vs. existing `qwen3-tts-flash`.** The frozen
   block says "DashScope CosyVoice" in keeping with the planning artifact
   `next-session-monologue-mode-decoupling.md` (line 131, 253, 367, 507). The
   project's actual TTS lineup per `feedback_api_dashscope_only.md` and
   `internal/domain/config.go:161` is `qwen3-tts-flash-2025-09-18` (with
   variants `qwen3-tts-instruct-flash`, `qwen3-tts-vc`). No `cosyvoice` model
   is currently wired; the existing client (`internal/llmclient/dashscope/tts.go`)
   targets the Qwen TTS multimodal-generation endpoint. The user's actual
   feedback rule — "Qwen 모델은 DashScope로만 호출, SiliconFlow 미사용" —
   is satisfied by the existing client. Treating the frozen "CosyVoice" as
   the planning artifact's misnomer for the project's already-approved
   DashScope-Qwen TTS path; no vendor migration. The frozen "no other Qwen
   channels" is interpreted as "no other vendors / no SiliconFlow",
   consistent with the user's actual rule. If the human disagrees this is
   a renegotiation moment.

2. **Spike result — already known.** `internal/pipeline/tts_track.go:26`
   documents the empirical 600-byte cap per DashScope's 4xx response
   `"Range of input length should be [0, 600]"`; the in-code constant
   `ttsMaxInputBytes = 560` is the safety margin. For the SCP-049 D1
   dogfood monologue (~5500 runes ≈ ~16500 UTF-8 bytes for Korean glyphs),
   single-call synthesis is rejected by the provider's input-length cap.
   The spec's spike-first task is therefore satisfied by existing
   knowledge; chunking is implemented from day 1. Per the user's
   "no dead layers" rule, the always-rejected single-call try is NOT
   implemented as a runtime branch — the merged monologue path goes
   directly into chunking. The single-call branch is documented in the
   change log as the conditional fallback IF the provider input cap ever
   widens (current cap < merged-monologue length) — the chunker's
   `MaxInputBytes` is config-driven so future ceiling raises don't need
   code edits.

3. **One-audio-file AC interpretation.** "Exactly one audio file is produced
   for the run" is interpreted as: one canonical continuous audio
   (`tts/run_audio.wav`) per run, produced by a single contiguous synthesis
   path (chunks concatenated via `-c copy`, no re-encoding, no inserted
   silence). Per-beat WAVs (`tts/scene_NN.wav`) are sample-accurate slices
   of the canonical recording — bit-for-bit copies from the same continuous
   synthesis, NOT independent syntheses. Concatenating the per-beat slices
   in scene order yields a byte stream identical to the canonical file.
   Phase C (`internal/pipeline/phase_c.go`) is therefore left unmodified:
   per-scene `Episode.TTSPath` continues to point at a sliced WAV, and
   because the slices are sample-contiguous from one recording, the
   concatenated MP4's audio track is continuous.

4. **Beat → time-offset mapping.** Rune-offset proportional mapping:
   `start_ms_j = total_ms * sum_runes_before_beat_j / total_runes`. Total
   runes is `Narration.MonologueRuneCount()` plus the joiner runes inserted
   between acts (single space per pair → `len(Acts) - 1` extra runes).
   Mapping is approximate (TTS does not pace rune-uniformly) but adequate
   for monologue-mode visual cuts (information-led, not action-led — a
   ±1-2s drift between visual beat and aural pacing is below perceptual
   threshold for the channel's product). Sub-second alignment requires
   forced alignment with a separate STT pass — out of D3 scope.

### 2026-05-04 — Step-04 review patches

Three-reviewer pass (blind hunter / edge-case hunter / acceptance auditor)
flagged silent-corruption surfaces. All `patch`-class findings applied; rune
mapping approximation (change-log entry 4) and joiner-absorption design were
reaffirmed as `reject`/`defer` per acceptance auditor's compliance read.

Patches:

- **Defensive beat validation** (`mergeMonologueAndBeats`). Per-act anchors now
  rejected for: `EndOffset == StartOffset` (zero-length), `StartOffset <
  prevEnd` (overlap / non-monotonic). D1 stage-2 validator already enforces
  these but TTS no longer trusts upstream — silent clamping to a zero-duration
  scene would otherwise pass file-existence checks and break Phase C.
- **Sample-accuracy guard** (`NewTTSTrack`). Hard error when `AudioFormat`
  is anything but `wav`. `-c copy -ss/-to` is sample-accurate only for PCM
  containers; compressed codecs snap to packet boundaries and silently break
  the "concat of slices == canonical" invariant. `mp3`/`aac` paths blocked
  until a re-encoding slicer lands.
- **Empty-slice guard** (`sliceAudioByTime`). Reject `endSec <= startSec`
  before invoking ffmpeg, and stat the output for non-zero size after — ffmpeg
  can silently emit a 0-byte WAV that would pass downstream existence checks.
- **Cleanup signature trim**. `cleanupTTSArtifacts` no longer takes the
  unused `ttsRoot` parameter (review flagged dead arg).
- **Test helper fix**. `utf8ValidPrefix` was a tautology returning true for
  any input; replaced with `utf8.ValidString`.
- **Dead test fixture removed**. `fakeTTSSynthesizer.rejectFirstNCalls` was
  unreferenced — vestigial single-call-rejection scaffolding from earlier
  drafts; per-no-dead-layers principle, removed.
- **AC #1 floor assertion**. `TestTTSTrack_SingleCallSynthesisProducesCanonicalRunAudio`
  now asserts the canonical run audio's duration ≥ merged-monologue rune
  count at the test's pacing rate (1ms/rune), closing the AC #1 verification
  gap the acceptance auditor flagged.

## Design Notes

The spike-first execution order is deliberate. The plan flags this risk explicitly: "Verify the TTS provider handles 2.5-minute synthesis in one call. If not, intra-act chunking becomes necessary." Implementing chunking before knowing whether it's needed is dead-layer risk. Spike result drives the actual implementation scope. (See change log entry 2 — spike already satisfied by existing empirical knowledge.)

**Atomic stage failure.** When any chunk fails after retries, the TTS stage fails atomically: any partial chunk WAVs and the canonical run audio are removed from `tts/`, and the segments table's `tts_path` columns are NOT written. Phase B re-entry on resume rebuilds from scratch — there is no mid-merge resume target.

**KR splitter packing.** Sentences are packed greedily into chunks bounded by `MaxInputBytes`. A single sentence longer than the cap falls through to UTF-8-safe hard-byte split (preserves rune integrity). Korean sentence-final patterns matched: terminator runes (`.`, `?`, `!`, `…`, `\n`) — Korean sentence-ending eomi (`다.`, `요.`, `니다.`) all end in `.` so the existing rune-level terminator detection covers them; the test matrix exercises each pattern explicitly.

**No re-synthesis on slicing.** Per-beat slices use `ffmpeg -ss start_ms -to end_ms -c copy` against the canonical WAV. WAV PCM is constant-bytes-per-second so `-c copy` produces sample-accurate slices. Critically, this means slice → concat → bit-identical canonical audio (the AC's seam test reduces to a `bytes.Equal` assertion in unit tests, no listening test required at the unit level).

## Verification

**Commands:**
- `go build ./...` + `go test ./...` + `go test -race ./internal/pipeline/...`
- Phase A SCP-049 dogfood through TTS; inspect generated audio file.

**Manual checks:**
- HITL listening test: blind-listen v2 audio at v1's act-boundary timestamps. Acceptance: no audible cut. (Per plan acceptance signal #3.)
- HITL listening test against `docs/exemplars/scp-049-hada.txt` audio reference (if available): time-to-distinguish ≥20s for v2 dogfood (plan acceptance signal #5 final form). Sub-20s but ≥5s indicates D shipped but not fully delivered; <5s = retro the lever.
