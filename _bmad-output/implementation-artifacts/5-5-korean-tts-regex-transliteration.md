# Story 5.5: Korean TTS with Regex Transliteration (DashScope)

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want Korean TTS generated with correct pronunciation of numbers and English terms,
so that the narration sounds natural and professional.

## Prerequisites

**Stories 5.1–5.4 are hard dependencies for this story.** Story 5.5 completes the Phase B Media Generation epic by closing the remaining gap: the TTS track. It must consume the shared DashScope limiter/retry infrastructure from Story 5.1, run inside the Phase B parallel execution boundary established in Story 5.2, and preserve the clean-slate Phase B resume semantics that Stories 5.2 and 5.4 already honor.

- reuse the shared DashScope limiter instance for BOTH image and TTS contention via `ProviderLimiterFactory.DashScopeTTS()` which returns the same `*CallLimiter` pointer as `DashScopeImage()` ([internal/llmclient/limiter.go:171-174](internal/llmclient/limiter.go#L171-L174)); do not allocate a story-local limiter
- wrap provider calls in `llmclient.WithRetry(...)` so retryable errors (rate_limit / timeout / stage_failed) are backoff-retried through `clock.Clock.Sleep` and observed via `pipeline.Recorder.RecordRetry` ([internal/llmclient/retry.go:88-98](internal/llmclient/retry.go#L88-L98), [internal/pipeline/observability.go:113-120](internal/pipeline/observability.go#L113-L120))
- plug the TTS track into the existing `pipeline.PhaseBRunner` via `NewPhaseBRunner(images, tts, recorder, clk, logger, assemble)` ([internal/pipeline/phase_b.go:87-109](internal/pipeline/phase_b.go#L87-L109)) — the runner already expects a non-nil `TTSTrack` and already asserts non-cancelling errgroup parallelism
- mirror the Story 5.4 image track shape: `TTSTrackConfig` struct + `NewTTSTrack(cfg) (TTSTrack, error)` constructor ([internal/pipeline/image_track.go:40-94](internal/pipeline/image_track.go#L40-L94)); do NOT invent a second orchestration pattern for Phase B
- preserve clean-slate Phase B resume: `pipeline.CleanStageArtifacts(runDir, StageTTS)` already removes `{runDir}/tts/` ([internal/pipeline/artifact.go:26-27](internal/pipeline/artifact.go#L26-L27)) and `SegmentStore.ClearTTSArtifactsByRunID` already NULLs the DB columns ([internal/db/segment_store.go:169-188](internal/db/segment_store.go#L169-L188)) — the new write path must stay compatible with both

**Current codebase gap to close deliberately:** the domain interface `TTSSynthesizer`, the config field `TTSModel`, the DB columns `segments.tts_path` / `tts_duration_ms`, and the Phase B runner's `TTSTrack` slot all exist today. What is missing: (a) a DashScope qwen3-tts-flash client implementing `TTSSynthesizer`, (b) a Korean regex transliteration engine, (c) the Phase B TTS track orchestrator that loads scenario narration, transliterates, calls the provider, writes files, and persists segment metadata, and (d) a `SegmentStore` method to persist `tts_path` + `tts_duration_ms` symmetrical to `UpsertImageShots`.

- transliteration lives at the pipeline utility layer (not inside the DashScope client) so it is provider-agnostic and unit-testable without HTTP
- DashScope plumbing (HTTP client, endpoint, response mapping, retryable-error wrapping) belongs under `internal/llmclient/dashscope/`
- orchestration belongs in `internal/pipeline/`
- new persistence method belongs in `internal/db/`

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, use inline fakes, and keep the module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0. No testify, no gomock, no real sleeps, no wall-clock assertions — use `clock.FakeClock` and `testutil.AssertEqual[T]` / `testutil.AssertJSONEq` per the repository conventions already applied in Stories 5.1–5.4.

1. **AC-TRANSLITERATION-ENGINE-COVERS-REGEX-SCOPE:** a deterministic Korean regex transliteration engine converts narration text to Korean orthography across four required scopes: numbers (including the SCP-ID pattern), common English words, currency, and dates.

   Required outcome:
   - `"SCP-049"` → `"에스씨피-공사구"` (digit-by-digit Korean reading of the ID number, letters read as Korean syllables)
   - `"doctor"` → `"닥터"`, and other common English words covered by a table-driven rule set
   - currency tokens (e.g. `"$100"`, `"100 USD"`) are converted to Korean reading (e.g. "백 달러")
   - date-like tokens (e.g. `"1998"`, `"1998년"`, `"1998-11-04"`) are converted to a spoken Korean form suitable for TTS
   - the engine is pure, deterministic, and idempotent: `Transliterate(Transliterate(x)) == Transliterate(x)` for inputs already containing only Korean orthography

   Rules:
   - the engine runs on the full narration string of a single scene; it must not mutate adjacent Korean characters or insert visible rule markers
   - rules are composable and order is explicit (e.g. SCP-ID pattern runs before the generic number pattern, currency before bare numbers)
   - rules are table-driven for the English-word and currency-unit dictionaries so new entries require data changes, not control-flow edits
   - the engine does not silently drop unrecognized English tokens; either it converts them via a documented fallback (e.g. Hangul-letter spelling) or it leaves them untouched — the chosen policy must be tested
   - the engine never calls a remote service and never touches the filesystem

   Tests (in `internal/pipeline/transliteration_test.go` or the equivalent engine package):
   - `TestTransliterate_SCPIDReadingDigitByDigit` — `"SCP-049"` → `"에스씨피-공사구"`
   - `TestTransliterate_CommonEnglishWords` — table-driven dictionary coverage (`"doctor"`→`"닥터"` and at least 3 more)
   - `TestTransliterate_CurrencyTokens` — at least `"$100"` and `"100 USD"` forms
   - `TestTransliterate_DateTokens` — at least one year-only and one full-date form
   - `TestTransliterate_MixedSentence` — a realistic Korean narration sentence with embedded English + numbers transliterates every token and leaves Korean text untouched
   - `TestTransliterate_Idempotent` — applying the transliterator twice equals applying it once

2. **AC-TRANSLITERATION-APPLIED-BEFORE-SYNTHESIS:** the TTS track transliterates each scene's narration BEFORE calling the `TTSSynthesizer`; the provider never receives raw untransliterated text, and the original narration is preserved untouched in `scenario.json` and `segments.narration`.

   Required outcome:
   - for each scene, the TTS track reads `state.Narration.Scenes[i].Narration` (canonical source per [internal/domain/narration.go:16-27](internal/domain/narration.go#L16-L27)) and applies the transliteration engine to produce the text passed in `domain.TTSRequest.Text`
   - `segments.narration` and the persisted `scenario.json` are NOT overwritten with the transliterated form
   - the transliterated string is observable in tests via a fake `TTSSynthesizer` that records received `TTSRequest.Text`

   Rules:
   - transliteration is orchestration-layer logic, not provider-layer — it must not live inside the DashScope client
   - if `state.Narration` is nil or a scene's `Narration` is empty, surface a typed validation error (`domain.ErrValidation`) rather than calling the provider with empty text
   - the fake TTS provider in tests asserts that received text contains zero ASCII digits and zero Latin-alphabet word characters for at least one representative mixed-input scene

   Tests:
   - `TestTTSTrack_TransliteratesNarrationBeforeSynthesize` — fake TTS records received text, assert it equals `Transliterate(originalNarration)`
   - `TestTTSTrack_PreservesOriginalNarrationInScenarioAndSegments` — assert raw narration in `state.Narration` and persisted `segments.narration` is unchanged after the track runs
   - `TestTTSTrack_EmptyNarrationFailsValidation` — nil `state.Narration` or empty `Narration` string returns a `domain.ErrValidation`-wrapped error

3. **AC-DASHSCOPE-QWEN3-TTS-FLASH-CLIENT:** a new DashScope client under `internal/llmclient/dashscope/` implements `domain.TTSSynthesizer` for qwen3-tts-flash, accepts `*http.Client` via its constructor (API isolation Layer 1), and returns a populated `domain.TTSResponse` with a real on-disk audio file.

   Required outcome:
   - constructor signature follows the project pattern: `func NewTTSClient(httpClient *http.Client, cfg TTSClientConfig) (*TTSClient, error)` where `TTSClientConfig` carries at minimum the endpoint/region, API key source, and default voice identifier
   - `Synthesize(ctx, req)` issues a single POST to the DashScope qwen3-tts endpoint, writes the returned audio bytes to `req` 's target path, and returns `domain.TTSResponse{AudioPath, DurationMs, Model, Provider: "dashscope", CostUSD}`
   - the client MUST NOT use `http.DefaultClient`; rejecting a nil `*http.Client` at construction time is the preferred guardrail
   - HTTP status mapping lives in this package: 429 → wrap `domain.ErrRateLimited`, 5xx/timeout → wrap `domain.ErrUpstreamTimeout` or `domain.ErrStageFailed` consistent with the retry taxonomy in [internal/llmclient/retry.go:28-45](internal/llmclient/retry.go#L28-L45), 4xx client errors → wrap `domain.ErrValidation` (non-retryable)
   - extend `domain.TTSRequest` only if strictly necessary (e.g., add a `Format` or `OutputPath` field) and justify the addition in code comments; current fields are `Text`, `Model`, `Voice` ([internal/domain/llm.go:46-51](internal/domain/llm.go#L46-L51))

   Rules:
   - the client does not own retry, backoff, or rate-limiting logic; those are composed by the TTS track via `CallLimiter.Do` + `llmclient.WithRetry`
   - the client does not own transliteration; it accepts whatever `TTSRequest.Text` it is given
   - model ID is never hard-coded in business logic (NFR-M1) — `TTSRequest.Model` is populated by the TTS track from `cfg.TTSModel` at call time
   - audio file output directory must be created by the caller (the TTS track) before invoking the client; the client writes only the file

   Tests (`internal/llmclient/dashscope/tts_test.go`):
   - `TestTTSClient_ConstructorRejectsNilHTTPClient`
   - `TestTTSClient_Synthesize_SuccessWritesAudioAndReturnsDuration` — use `httptest.Server` inside `testutil.BlockExternalHTTP` with a permit to assert the POST body carries the supplied text/model/voice and the saved file contains the returned bytes
   - `TestTTSClient_Synthesize_MapsRateLimitTo_ErrRateLimited` — 429 response → `errors.Is(err, domain.ErrRateLimited)` is true
   - `TestTTSClient_Synthesize_MapsServerErrorToRetryable` — 5xx → classified as `domain.ErrUpstreamTimeout` or `domain.ErrStageFailed` (pick and document one)
   - `TestTTSClient_Synthesize_MapsClientErrorTo_ErrValidation` — 4xx (except 429) → non-retryable

4. **AC-TTS-TRACK-USES-SHARED-DASHSCOPE-LIMITER-AND-RETRY:** the Phase B TTS track gates every provider call through the shared DashScope limiter and the deterministic retry helper from Story 5.1, records retry observability through `pipeline.Recorder.RecordRetry`, and never cancels the image track.

   Required outcome:
   - the TTS track wraps each `Synthesize` call in `limiter.Do(ctx, fn)` where `limiter` is the `*llmclient.CallLimiter` returned by `factory.DashScopeTTS()` — the same pointer as `DashScopeImage()`
   - inside that closure, `llmclient.WithRetry(ctx, clk, maxRetries, fn, onRetry)` wraps the actual `Synthesize` call; the `onRetry` callback invokes `recorder.RecordRetry(ctx, runID, domain.StageTTS, reason)`
   - on TTS track failure, the returned error is surfaced but the runner does NOT cancel the image track context — Story 5.2's non-cancelling errgroup invariant is preserved automatically by using `PhaseBRunner`
   - cost accumulation is recorded via `result.Observation.CostUSD` (summed per scene) so `PhaseBRunner.recordObservation` persists total TTS cost per run

   Rules:
   - the TTS track MUST receive a `Limiter` (the structural interface already defined at [internal/pipeline/image_track.go:36-38](internal/pipeline/image_track.go#L36-L38)) rather than constructing one itself
   - the TTS track MUST share the DashScope limiter pointer with the image track — production wiring pulls both from the same `ProviderLimiterFactory`
   - do NOT call `recorder.RecordRetry` directly from the DashScope client; retry observability is a pipeline-layer concern
   - retry exhaustion surfaces the classified error (`ErrRateLimited`/`ErrUpstreamTimeout`/`ErrStageFailed`) so the `PhaseBRunner` and downstream resume logic see a retryable-looking failure

   Tests (in `internal/pipeline/tts_track_test.go`):
   - `TestTTSTrack_UsesSharedDashScopeLimiter` — inject a fake `Limiter` that records call counts; assert every `Synthesize` goes through `limiter.Do`
   - `TestTTSTrack_RetriesOn429AndRecordsRetry` — fake TTS returns `domain.ErrRateLimited` once then succeeds; assert `recorder.RecordRetry` was called with `stage=tts, reason="rate_limit"` and retry timing driven by `FakeClock`
   - `TestTTSTrack_NonRetryableErrorSurfacesImmediately` — e.g. `domain.ErrValidation` returns without retry
   - `TestPhaseBRunner_TTSFailureDoesNotCancelImageTrack` — pairs the new TTS track with a slow fake image track and asserts image context is not canceled when TTS errs (may extend existing `internal/pipeline/phase_b_test.go` patterns)

5. **AC-PER-SCENE-TTS-ARTIFACT-CANONICAL-PATH:** each generated TTS audio file is written to `{runDir}/tts/scene_{idx}.wav` (or `.mp3` if configured), one audio file per scene, matching the artifact convention already baked into `CleanStageArtifacts` and existing test fixtures.

   Required outcome:
   - the TTS track creates `{runDir}/tts/` before the first call (mirror [internal/pipeline/image_track.go:143-146](internal/pipeline/image_track.go#L143-L146))
   - scene index formatting is `scene_%02d` and matches the 1-based scene numbering used by the image track (`scene_01`, `scene_02`, …) — see [internal/pipeline/image_track.go:165](internal/pipeline/image_track.go#L165) and existing fixtures `testdata/fixtures/failed_at_tts.sql`
   - file extension is taken from a config field (default `wav`); the DashScope client does not hard-code it
   - on re-run after a clean-slate Phase B resume, the path pattern is stable and overwrites deterministically

   Rules:
   - relative paths stored in `segments.tts_path` (e.g. `"tts/scene_01.wav"`) — NOT absolute paths, consistent with the Story 5.4 image track contract and [testdata/fixtures/failed_at_tts.sql:14-16](testdata/fixtures/failed_at_tts.sql#L14-L16)
   - do NOT store audio files outside the `tts/` subdirectory; other subtrees are owned by other stages
   - if the provider returns an empty/zero-byte body, surface a typed error instead of persisting an unplayable file

   Tests:
   - `TestTTSTrack_WritesAudioToSceneCanonicalPaths` — assert `tts/scene_01.wav`, `tts/scene_02.wav`, … exist after a multi-scene run
   - `TestTTSTrack_StoresRelativePathInSegments` — assert `segments.tts_path` begins with `"tts/"` and does not contain the run directory absolute prefix
   - `TestTTSTrack_RerunOverwritesDeterministically` — after a clean-slate resume, same scene writes to the same relative path

6. **AC-SEGMENTS-TTS-METADATA-PERSISTED:** after each scene's TTS synthesis, `segments.tts_path` and `segments.tts_duration_ms` are persisted via a new, symmetrical `SegmentStore` method that preserves image shot data already written by Story 5.4.

   Required outcome:
   - add a new `SegmentStore.UpsertTTSArtifact(ctx, runID, sceneIndex, ttsPath string, ttsDurationMs int64) error` (or an equivalently named symmetric partner to `UpsertImageShots` — see [internal/db/segment_store.go:200-231](internal/db/segment_store.go#L200-L231))
   - upsert semantics: insert a row if `(run_id, scene_index)` does not exist; otherwise UPDATE only the TTS columns and leave `shots`, `shot_count`, `narration`, `clip_path`, `critic_*`, `review_status`, `safeguard_flags` untouched
   - the method accepts integer milliseconds (matching the `tts_duration_ms INTEGER` column; see [migrations/001_init.sql](migrations/001_init.sql)) and validates non-empty `runID`, non-negative `sceneIndex`, non-empty `ttsPath`, non-negative `ttsDurationMs`
   - the domain `Episode` struct's `TTSPath *string` and `TTSDurationMs *int` fields are already present ([internal/domain/types.go:160-168](internal/domain/types.go#L160-L168)); this story populates them, does not redefine them

   Rules:
   - do NOT overload `UpsertImageShots` to also handle TTS — the asymmetry between "write per-scene JSON array" and "write two scalar columns" is real and mixing them invites regressions
   - do NOT write observability (retries, cost) through this method — that belongs to `pipeline.Recorder`
   - the method must be safe to call on a run whose segments rows were just cleared by `ClearTTSArtifactsByRunID` (the resume clean-slate path)
   - no schema migration is required; the columns already exist

   Tests (`internal/db/segment_store_test.go`):
   - `TestSegmentStore_UpsertTTSArtifact_InsertsWhenSegmentMissing` — fresh run, no existing segments row, upsert creates one with TTS fields populated and default status
   - `TestSegmentStore_UpsertTTSArtifact_UpdatesOnlyTTSColumnsWhenSegmentExists` — pre-insert a row with non-nil `shots`/`shot_count`/`narration`; upsert TTS; assert image and narration fields survive
   - `TestSegmentStore_UpsertTTSArtifact_RejectsInvalidInput` — empty `runID`, negative `sceneIndex`, empty `ttsPath`, negative duration → `domain.ErrValidation`
   - `TestSegmentStore_UpsertTTSArtifact_CoexistsWithUpsertImageShots` — image+TTS upserted in either order produces a row with both sets of fields intact

7. **AC-MODEL-ID-AND-VOICE-ARE-CONFIG-DRIVEN:** no DashScope TTS model identifier or voice identifier is hard-coded in business logic; both flow from `cfg.TTSModel` and a configured voice ID through the TTS track into `TTSRequest.Model` / `TTSRequest.Voice`.

   Required outcome:
   - `cfg.TTSModel` ([internal/domain/config.go:14](internal/domain/config.go#L14)) is the sole source of the model identifier passed to `TTSSynthesizer.Synthesize`; default remains `"qwen3-tts-flash-2025-09-18"` ([internal/domain/config.go:72](internal/domain/config.go#L72))
   - if the current `PipelineConfig` does not yet expose a voice identifier, add a single `TTSVoice string` field with a documented default, rather than encoding a voice literal inside the DashScope client
   - `ripgrep` / `grep` over `internal/` excluding `*_test.go` returns no literal `"qwen3-tts"` or `"qwen-tts"` outside `internal/domain/config.go`

   Rules:
   - config additions must follow the existing `yaml` + `mapstructure` tag convention already used for `TTSModel`
   - tests may hard-code a fake model string (e.g. `"fake-tts"`) since that value is a test fixture, not product configuration
   - NFR-M1 compliance is asserted structurally (via the forbidden-literal grep style) AND behaviorally (the DashScope client test passes through the model string unchanged)

   Tests:
   - `TestTTSTrack_PassesConfiguredModelAndVoiceToProvider` — inject `cfg.TTSModel="fake-tts"` and a fake voice; assert the fake `TTSSynthesizer` receives matching `TTSRequest.Model` / `TTSRequest.Voice`
   - a structural assertion in `internal/pipeline/tts_track_test.go` (or a dedicated lint-style test) proving no hard-coded qwen3 literal leaks from the track implementation

8. **AC-PHASE-B-INTEGRATION-AND-NO-REGRESSION:** the TTS track integrates cleanly with the existing Phase B runner, preserves the non-cancelling errgroup invariant, and leaves all pre-existing tests green under `go test ./... -race`.

   Required outcome:
   - `pipeline.NewPhaseBRunner` wired with the real TTS track still satisfies every existing test in `internal/pipeline/phase_b_test.go` (the tests that use inline fakes continue to pass, and at least one new integration-style test exercises the real track with a fake provider)
   - the TTS track populates `TTSTrackResult{Observation, Artifacts}` such that `PhaseBRunner.recordObservation` persists per-track cost/duration consistent with how the image track does today (see [internal/pipeline/phase_b.go:138-150](internal/pipeline/phase_b.go#L138-L150))
   - existing TTS-side resume paths continue to work: `resume.go`'s `clearFailedPhaseBTrack` branch for `StageTTS` ([internal/pipeline/resume.go:355-366](internal/pipeline/resume.go#L355-L366)) drives `ClearTTSArtifactsByRunID`, and `cleanFailedPhaseBTrack` removes the `tts/` directory; the new write path must accept that as valid pre-state and successfully re-populate fields on the next run

   Rules:
   - do NOT introduce per-scene resume semantics for TTS in V1 — Phase B remains stage-level clean-slate per Story 2.3 and Story 5.2 decisions
   - do NOT change the `TTSTrack` type signature ([internal/pipeline/phase_b.go:20](internal/pipeline/phase_b.go#L20)) — the runner is already locked to it
   - do NOT move transliteration, rate-limiting, or retry orchestration into `cmd/pipeline/` wiring code; those concerns belong to the `internal/pipeline/` layer and are tested there
   - `go test ./... -race` and `go build ./...` must both succeed; any new dependencies are reflected in `go.mod` / `go.sum`

   Tests:
   - `TestPhaseBRunner_RealTTSTrack_WithFakeProvider_CompletesAllScenes` — end-to-end wiring test using fake `TTSSynthesizer`, fake `Limiter`, real transliteration, real persistence against an in-memory SQLite
   - existing `TestPhaseBRunner_*` tests continue to pass unmodified
   - `TestResume_PhaseBRegenerationRebuildsTTSAfterFailure` — pair with Story 5.4's image-resume test (or extend an existing resume test) to prove TTS persistence is rebuilt cleanly after a simulated failure

---

## Tasks / Subtasks

- [x] **T1: Korean regex transliteration engine** (AC: #1, #2)
  - [x] Add `internal/pipeline/transliteration.go` with a `Transliterate(text string) string` function (or a `Transliterator` struct if rule composition benefits from state).
  - [x] Implement ordered rule set: SCP-ID pattern → currency → dates → generic numbers → English-word dictionary.
  - [x] Add table-driven English word dictionary (at least `"doctor"→"닥터"` plus common SCP-narration terms).
  - [x] Add currency-unit dictionary (at least USD, KRW) and a date reader covering year-only and ISO-date forms.
  - [x] Add `internal/pipeline/transliteration_test.go` with the six tests listed in AC-1.

- [x] **T2: DashScope qwen3-tts-flash client** (AC: #3, #7)
  - [x] Add `internal/llmclient/dashscope/tts.go` implementing `domain.TTSSynthesizer`, accepting `*http.Client` via constructor.
  - [x] Map HTTP status codes to the canonical retry taxonomy (ErrRateLimited / ErrUpstreamTimeout / ErrStageFailed / ErrValidation).
  - [x] Write audio response bytes to `req`'s target path; return populated `domain.TTSResponse`.
  - [x] Add `internal/llmclient/dashscope/tts_test.go` using `httptest.Server` inside `testutil.BlockExternalHTTP` — cover success, 429, 5xx, and 4xx cases.
  - [x] Decide and document `TTSRequest` / `TTSResponse` field additions (e.g. `Format`, `OutputPath`) — extend `internal/domain/llm.go` only if necessary.

- [x] **T3: SegmentStore TTS upsert method** (AC: #6)
  - [x] Add `SegmentStore.UpsertTTSArtifact(ctx, runID, sceneIndex, ttsPath, ttsDurationMs) error` in `internal/db/segment_store.go`.
  - [x] Use `INSERT ... ON CONFLICT(run_id, scene_index) DO UPDATE SET tts_path=?, tts_duration_ms=?` to preserve image/narration/review columns.
  - [x] Validate inputs and return `domain.ErrValidation` for bad values.
  - [x] Add tests in `internal/db/segment_store_test.go` covering insert, update, asymmetric coexistence with `UpsertImageShots`, and invalid-input rejection.

- [x] **T4: Phase B TTS track orchestrator** (AC: #2, #4, #5, #7, #8)
  - [x] Add `internal/pipeline/tts_track.go` with `TTSTrackConfig` and `NewTTSTrack(cfg) (TTSTrack, error)`, mirroring `NewImageTrack`.
  - [x] Iterate `state.Narration.Scenes`, apply `Transliterate`, call `limiter.Do(ctx, ...)` wrapping `llmclient.WithRetry(...)` wrapping `tts.Synthesize(ctx, req)`.
  - [x] Write audio to `{runDir}/tts/scene_%02d.{wav|mp3}`, capture duration + cost.
  - [x] Persist per-scene metadata via `SegmentStore.UpsertTTSArtifact`; track cost on `TTSTrackResult.Observation`.
  - [x] Wire `onRetry` callback to `recorder.RecordRetry(ctx, runID, domain.StageTTS, reason)`.
  - [x] Add `internal/pipeline/tts_track_test.go` covering the tests enumerated in AC-2, AC-4, AC-5, and AC-7.

- [x] **T5: Config + wiring** (AC: #7)
  - [x] Add `TTSVoice string` (or equivalent) field to `PipelineConfig` with a documented default if the DashScope qwen3-tts API requires a voice identifier.
  - [x] Update `internal/config/loader.go` (and defaults in `domain.DefaultConfig`) if new config fields were introduced.
  - [x] Wire production construction in `cmd/pipeline/serve.go` (or the equivalent entry point) so the real TTS track is passed to `NewPhaseBRunner` with the shared DashScope limiter.

- [x] **T6: Integration, resume, and regression tests** (AC: #8)
  - [x] Add at least one integration-style test that builds the real TTS track with a fake `TTSSynthesizer` + in-memory SQLite and drives a multi-scene Phase B run end-to-end.
  - [x] Add or extend a resume test proving that after `cleanFailedPhaseBTrack` + `clearFailedPhaseBTrack` for `StageTTS`, the next run rebuilds `tts/` files and `segments.tts_path` / `tts_duration_ms` deterministically.
  - [x] Run `go test ./... -race` and `go build ./...`; fix any regressions.
  - [x] Verify `grep -r '"qwen3-tts' internal/ --include='*.go' --exclude='*_test.go'` returns only matches in `internal/domain/config.go`.

## Dev Notes

### Epic Intent and Story Boundary

- Epic 5 closes FR14–FR19. Story 5.5 specifically owns FR15 (Korean TTS with numeric/English-term transliteration) and the TTS half of FR16 (parallel image + TTS tracks, shared DashScope budget). [Source: `_bmad-output/planning-artifacts/epics.md` Epic 5 / Story 5.5 lines 1471–1484]
- The Phase B runner, clean-slate resume semantics, shared limiter factory, retry taxonomy, and image track were delivered by Stories 5.1, 5.2, 5.3, and 5.4. Story 5.5 must slot into those contracts, not re-open them. [Source: `_bmad-output/implementation-artifacts/5-1-common-rate-limiting-exponential-backoff.md`, `5-2-parallel-media-generation-runner.md`, `5-4-frozen-descriptor-propagation-per-shot-image-generation.md`]
- Epic 5 explicitly routes TTS through DashScope only (no SiliconFlow) per the standing product decision. [Source: `_bmad-output/planning-artifacts/architecture.md` line 176]

### Architecture Alignment

- `architecture.md` names rate-limit coordination a Tier 1 cross-cutting concern for Phase B; DashScope image and TTS share the same limiter budget. [Source: `_bmad-output/planning-artifacts/architecture.md` lines 191–196, 863–865]
- The segments schema already reserves `tts_path TEXT, tts_duration_ms INTEGER`, and Phase B resume semantics are clean-slate DELETE+reinsert. [Source: `_bmad-output/planning-artifacts/architecture.md` lines 511–547]
- The artifact file layout specifies `tts/scene_NN.wav` under the run's output tree. [Source: `_bmad-output/planning-artifacts/architecture.md` lines 810–813]
- The retry helper with clock injection is already canonical; TTS must reuse it. [Source: `_bmad-output/planning-artifacts/architecture.md` lines 867–886]

### PRD and Product Alignment

- PRD defines TTS track as: scenario refinement (numerals + English → Korean) → DashScope qwen3-tts-flash → one audio per scene, running in parallel with images. [Source: `_bmad-output/planning-artifacts/prd.md` lines 335–344]
- TTS stage cost is explicitly the cheapest — clean-slate re-runs on failure are acceptable. [Source: `_bmad-output/planning-artifacts/prd.md` lines 521–527]
- NFR-M1 requires model identifiers (including TTS model) to be configuration-driven; `qwen3-tts-flash-2025-09-18` is called out as an example. [Source: `_bmad-output/planning-artifacts/prd.md` lines 1443, 972]
- FR47 (blocked voice-ID list) is a future concern owned by a different slice; Story 5.5 must NOT hard-code a voice literal inside the client so FR47 enforcement can layer on later without refactoring.
- FR15 is the functional contract for this story: "numerals and English terms transliterated to Korean orthography prior to synthesis." [Source: `_bmad-output/planning-artifacts/prd.md` line 1265; `epics.md` line 37]

### Existing Code to Extend, Not Replace

- `internal/domain/llm.go` already defines `TTSRequest` / `TTSResponse` / `TTSSynthesizer` ([internal/domain/llm.go:46-79](internal/domain/llm.go#L46-L79)); extend the request struct only if a required field is missing.
- `internal/domain/config.go` already exposes `TTSModel` and `CostCapTTS` ([internal/domain/config.go:14,33,72,83](internal/domain/config.go#L14)); reuse them instead of inventing parallel config.
- `internal/domain/narration.go` is the canonical Korean narration contract (`NarrationScene.Narration` is the per-scene text to transliterate).
- `internal/llmclient/limiter.go` exposes `ProviderLimiterFactory.DashScopeTTS()` which returns the SAME `*CallLimiter` pointer as `DashScopeImage()` ([internal/llmclient/limiter.go:171-172](internal/llmclient/limiter.go#L171-L172)); this is the production wiring handle for the shared DashScope budget.
- `internal/llmclient/retry.go` exposes `WithRetry(ctx, clk, maxRetries, fn, onRetry)` and `RetryReasonFor(err)`; reuse both verbatim ([internal/llmclient/retry.go:28-45,88-98](internal/llmclient/retry.go#L28-L98)).
- `internal/pipeline/observability.go` exposes `Recorder.RecordRetry(ctx, runID, stage, reason)` — the onRetry callback target ([internal/pipeline/observability.go:113-120](internal/pipeline/observability.go#L113-L120)).
- `internal/pipeline/phase_b.go` defines `TTSTrack func(ctx, PhaseBRequest) (TTSTrackResult, error)` and already asserts non-nil TTS track in `PhaseBRunner.Run` ([internal/pipeline/phase_b.go:20,112](internal/pipeline/phase_b.go#L20)); the TTS track implementation must match this signature exactly.
- `internal/pipeline/image_track.go` is the structural template for the new TTS track: `ImageTrackConfig` / `NewImageTrack(cfg) (ImageTrack, error)` / `runImageTrack` inner function + a narrow `Limiter` interface already shared with callers.
- `internal/pipeline/artifact.go` already removes `{runDir}/tts/` on `StageTTS` cleanup ([internal/pipeline/artifact.go:26-27](internal/pipeline/artifact.go#L26-L27)).
- `internal/pipeline/resume.go` already wires `StageTTS` into both `cleanFailedPhaseBTrack` (filesystem) and `clearFailedPhaseBTrack` (DB) paths, and already invokes `SegmentStore.ClearTTSArtifactsByRunID` ([internal/pipeline/resume.go:344-366](internal/pipeline/resume.go#L344-L366)) — do not duplicate.
- `internal/db/segment_store.go` already has `ClearTTSArtifactsByRunID` and `UpsertImageShots`; this story adds the symmetric `UpsertTTSArtifact`.
- `internal/clock/clock.go` has `Clock` / `RealClock` / `FakeClock` with `Advance`, `Sleep`, and `PendingSleepers` — use these for deterministic timing tests instead of wall-clock sleeps.
- `internal/testutil/` exposes `BlockExternalHTTP`, `AssertEqual[T]`, `AssertJSONEq`, `CaptureLog`, and fixture helpers; use them everywhere per the repo convention.

### File Structure Notes

- Expected new files:
  - `internal/pipeline/transliteration.go` — Korean regex transliteration engine
  - `internal/pipeline/transliteration_test.go`
  - `internal/pipeline/tts_track.go` — Phase B TTS track orchestrator
  - `internal/pipeline/tts_track_test.go`
  - `internal/llmclient/dashscope/tts.go` — qwen3-tts-flash HTTP client
  - `internal/llmclient/dashscope/tts_test.go`
- Expected existing files to extend:
  - `internal/domain/llm.go` — only if `TTSRequest` or `TTSResponse` needs a new field (e.g. `Format`, `OutputPath`)
  - `internal/domain/config.go` — add `TTSVoice` if needed; update `DefaultConfig`
  - `internal/config/loader.go` — if new config fields were added
  - `internal/db/segment_store.go` — add `UpsertTTSArtifact`
  - `internal/db/segment_store_test.go` — add `UpsertTTSArtifact` tests
  - `cmd/pipeline/serve.go` (or equivalent) — wire the real TTS track into production `PhaseBRunner` construction
- No new migration is required; `segments.tts_path` and `segments.tts_duration_ms` already exist in `migrations/001_init.sql`.

### Testing Requirements

- Every new test calls `testutil.BlockExternalHTTP(t)` and uses inline fakes.
- Transliteration engine tests are pure string-in / string-out — no DB, no HTTP, no clock.
- DashScope client tests use `httptest.Server` (localhost is allowed by `BlockExternalHTTP`).
- TTS track tests use a fake `TTSSynthesizer` that records received `TTSRequest.Text` so AC-2 can be asserted without HTTP.
- Retry timing tests drive `clock.FakeClock` via `Advance` + `PendingSleepers` — zero real sleeps in tests.
- At least one integration-style test wires the real track together with a real `SegmentStore` backed by an in-memory SQLite (`testutil.DB` or equivalent) to prove AC-8.
- `go test ./... -race` and `go build ./...` are both required to pass before marking the story done.

### Open Implementation Tensions

- **Voice-ID surface:** the DashScope qwen3-tts-flash API expects a voice identifier (Korean voice presets). Whether that belongs in `TTSRequest.Voice` only (current field, populated by the TTS track from config) or also in a `PipelineConfig.TTSVoice` field is an implementation judgment call. The AC requires the value to be config-driven, not the exact location — pick the cleanest place and document the choice in code comments.
- **Audio format decision:** architecture calls out both `.wav` (in the tree diagram) and MP3/WAV (in the epics scope). Default to `.wav` to match existing fixtures (`testdata/fixtures/failed_at_tts.sql`, `internal/pipeline/artifact_test.go`); expose format as config if the qwen3-tts API requires a request-side selection.
- **English fallback policy:** not every English word in a narration will be in the dictionary. Choose one of (a) Hangul-letter spelling via a deterministic rule, (b) leave-untouched passthrough with a logged warning, or (c) hard error. Whichever is chosen must be unit-tested and must not crash Phase B.
- **Transliteration placement (pipeline vs. agent):** `internal/pipeline/agents/policy.go` contains analogous regex-scanning logic for forbidden terms. Placing the transliterator at the `internal/pipeline/` layer (alongside `image_track.go`) keeps it closer to the TTS track consumer and avoids polluting the agent package with a single-consumer utility. Revisit if a second consumer appears.
- **FR47 (blocked voice-ID) scope:** FR47 is explicitly not in this story. But the shape of the voice parameter now sets up whether FR47 can be added as a pre-call guard without refactoring. Keep the voice ID on the request so a future blocklist check can live in the TTS track between config read and limiter call.

### References

- Epic definition and AC-RL constraints: [epics.md](_bmad-output/planning-artifacts/epics.md) Epic 5 / Story 5.5 lines 1471–1484; Epic 5 scope lines 444–463; AC-RL reference line 638.
- Architecture constraints (segments schema, resume semantics, artifact tree, shared limiter, retry helper): [architecture.md](_bmad-output/planning-artifacts/architecture.md) lines 191–196, 511–547, 695–735, 789–822, 847–886.
- PRD differentiator, model-id policy (NFR-M1), TTS workflow, voice-likeness guardrails: [prd.md](_bmad-output/planning-artifacts/prd.md) lines 335–344, 521–527, 755–782, 972, 1265, 1443.
- UX scenario edit propagation to TTS: [ux-design-specification.md](_bmad-output/planning-artifacts/ux-design-specification.md) UX-DR61.
- Prior story contexts: [5-1-common-rate-limiting-exponential-backoff.md](_bmad-output/implementation-artifacts/5-1-common-rate-limiting-exponential-backoff.md), [5-2-parallel-media-generation-runner.md](_bmad-output/implementation-artifacts/5-2-parallel-media-generation-runner.md), [5-4-frozen-descriptor-propagation-per-shot-image-generation.md](_bmad-output/implementation-artifacts/5-4-frozen-descriptor-propagation-per-shot-image-generation.md).
- Sprint prompt shorthand: [sprint-prompts.md](_bmad-output/planning-artifacts/sprint-prompts.md) Epic 5 Story 5.5 note.

## Dev Agent Record

### Context Reference

- Epic source: `_bmad-output/planning-artifacts/epics.md` (Epic 5, Story 5.5)
- Architecture source: `_bmad-output/planning-artifacts/architecture.md` (Tier 1 rate-limit coordination, segments schema, Phase B artifact tree, retry helper)
- PRD source: `_bmad-output/planning-artifacts/prd.md` (FR15, FR16, NFR-M1, TTS workflow, voice-likeness policy)
- Sprint prompt source: `_bmad-output/planning-artifacts/sprint-prompts.md` (Epic 5 — Story 5.5)
- Prior story contexts: `5-1-...md`, `5-2-...md`, `5-3-...md`, `5-4-...md`

### Missing Context at Story Creation Time

- No `project-context.md` file is present in the repository.
- No existing transliteration code (regex or otherwise) exists in the repo; the dictionary tables and rule ordering will be chosen during implementation.
- `testdata/` does not yet contain a synthetic Korean narration fixture with mixed English + numeric tokens; dev should add one inline or via a small fixture file when writing the mixed-sentence test (AC-1).

### Implementation Plan

- Build the pure transliteration engine first; prove AC-1 in isolation.
- Add `SegmentStore.UpsertTTSArtifact` second; it has the narrowest contract and unblocks the track tests.
- Add the DashScope client third; verify HTTP error taxonomy with httptest.
- Add the TTS track fourth; compose limiter + retry + transliteration + client + store; cover AC-2 through AC-5 and AC-7 with inline fakes.
- Wire production construction in `cmd/pipeline/serve.go` last; extend an integration-style Phase B test to prove AC-8.
- Keep every delay test on `clock.FakeClock`; keep every HTTP test inside `testutil.BlockExternalHTTP` + `httptest.Server`.

### Completion Notes

- Story file created for Epic 5 Story 5.5 per explicit user request.
- Scope anchored to the current repository state: domain TTS interface, config field, DB columns, Phase B runner's TTS slot, clean-slate resume paths, and shared DashScope limiter all exist; this story fills the remaining gap with transliteration engine, DashScope qwen3-tts-flash client, TTS track orchestrator, and `UpsertTTSArtifact` persistence method.
- Guardrails added for Story 5.1 limiter + retry reuse, Story 5.2 non-cancelling errgroup invariant, and Story 5.4 track-config shape parity.

## File List

- `_bmad-output/implementation-artifacts/5-5-korean-tts-regex-transliteration.md`
