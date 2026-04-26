---
title: 'Story 5.4 wrap — wire DashScope qwen-image client into Phase B'
type: 'feature'
created: '2026-04-26'
status: 'done'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/5-4-frozen-descriptor-propagation-per-shot-image-generation.md'
  - '{project-root}/internal/pipeline/image_track.go'
  - '{project-root}/internal/llmclient/dashscope/tts.go'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Phase B image track is fully orchestrated but its `domain.ImageGenerator` is wired to a no-op stub at [cmd/pipeline/serve.go:89-93](cmd/pipeline/serve.go#L89-L93). Every run produces zero images. Story 5.4 was marked done without a real client.

**Approach:** Build a single `internal/llmclient/dashscope/image.go` package that implements `domain.ImageGenerator` (`Generate` + `Edit`) against DashScope's async text2image jobs API, mirroring the TTS client's structural conventions. Replace the stub in `buildPhaseBRunner` with the real client and thread a `CharacterResolver` (already implemented by `service.CharacterService`) through the executor. Add an `ImageEditModel` config field so Generate (`qwen-image`) and Edit (`qwen-image-edit`) can use distinct model IDs.

## Boundaries & Constraints

**Always:**
- Provider is **DashScope only** for images (per `feedback_api_dashscope_only.md`); no SiliconFlow/Replicate.
- HTTP client must be injected via constructor — never `http.DefaultClient`.
- Image client owns: HTTP request shaping, async polling, response download (size-capped), file write. Image client does **not** own: retry, rate-limit, concurrency — caller (image_track) composes those via existing `Limiter.Do` + `WithRetry`.
- Tests must use `testutil.BlockExternalHTTP(t)` + `httptest.Server`; no live network.
- File write must be atomic (temp file → `os.Rename`) so partial-failure runs are safely re-runnable per Story 5.4 idempotency spec.
- Region-aware endpoint: default Intl, override via `cfg.DashScopeRegion == "cn"`, mirroring TTS constructor logic.
- Errors classified via existing `domain.ErrRateLimited` (429), `domain.ErrStageFailed` (5xx), `domain.ErrValidation` (4xx + bad request shape) so `WithRetry` can decide.
- Stub `imageTrackStub` is **deleted** entirely — no fallback path left behind (per `feedback_no_dead_layers.md`).

**Ask First:**
- If `service.CharacterService.GetSelectedCandidate` does not structurally satisfy `pipeline.CharacterResolver` after this change, halt and confirm before introducing an adapter shim.
- If polling consistently exceeds the hard cap (60s) on real qwen-image jobs in dev verification, halt and confirm before raising the constant.

**Never:**
- Image static-serving HTTP route (no `handler_media.go` on `main`; defer to a follow-up session that lands together with the audio static route).
- `phase_c_metadata.go` ImageEditModel propagation (metadata bundle uses Generate model only; do not expand scope).
- Touching files already modified on `wip/in-flight-pre-image-client` outside this spec's Code Map.
- Touching Phase A, Phase C, or the Phase B TTS track.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Generate happy path | `ImageRequest{Prompt, Model: "qwen-image", Width:1024, Height:1024, OutputPath}`; fake server returns task → SUCCEEDED → image bytes | File written atomically at OutputPath; `ImageResponse{ImagePath=OutputPath, Provider:"dashscope", Model, CostUSD>0, DurationMs>0}` | N/A |
| Edit with reference URL | `ImageEditRequest{ReferenceImageURL, Model:"qwen-image-edit", ...}` | POST body contains `input.ref_imgs:[ReferenceImageURL]`; otherwise same as Generate | N/A |
| Async poll PENDING→RUNNING→SUCCEEDED | Task status returns PENDING twice then SUCCEEDED | Backs off 2s, 4s; final download succeeds; total wall ≤ 60s | N/A |
| Task FAILED | Polling returns `task_status:"FAILED"` with reason | Returns wrapped `domain.ErrValidation` (terminal — qwen-image FAILED is not transient) | error string includes task_id + reason |
| HTTP 429 on submit | Submit returns 429 | Returns `domain.ErrRateLimited` | Caller's WithRetry retries |
| HTTP 5xx on submit or poll | 500/503 | Returns `domain.ErrStageFailed` | Caller's WithRetry retries |
| HTTP 4xx (non-429) on submit | 400 with bad prompt | Returns `domain.ErrValidation` | Terminal; not retried |
| Download exceeds cap | Image stream > 50 MiB | Returns `domain.ErrValidation`; no partial file remains | Temp file cleaned up |
| Polling times out | 60s elapses, task still RUNNING | Returns `domain.ErrUpstreamTimeout` | Caller's WithRetry retries |
| Constructor guards | nil http client OR empty APIKey | Returns `domain.ErrValidation` from `NewImageClient` | N/A |

</frozen-after-approval>

## Code Map

- `internal/llmclient/dashscope/image.go` -- **NEW** `ImageClient` struct implementing `domain.ImageGenerator`; submit + poll + download + atomic write.
- `internal/llmclient/dashscope/image_test.go` -- **NEW** 10 test cases driven by `httptest.Server` + `testutil.BlockExternalHTTP`.
- `internal/llmclient/dashscope/tts.go` -- read-only reference (constructor guards, region endpoint, download cap, error classification).
- `internal/domain/config.go` -- add `ImageEditModel` field; fix `ImageModel` default from `"qwen-max-vl"` (a vision-LLM, wrong) to `"qwen-image"`; add `"qwen-image-edit"` default for new field.
- `internal/domain/config_test.go` -- extend default-config assertions to include `ImageEditModel`.
- `internal/config/loader.go` -- add Viper `SetDefault("image_edit_model", "qwen-image-edit")`.
- `internal/config/settings_files.go` -- add `ImageEditModel` to file I/O yaml struct (around lines 109 + 387).
- `internal/service/settings_service.go` -- pass-through `ImageEditModel` in input → cfg conversion (around lines 308, 358, 393).
- `internal/service/settings_types.go` -- add JSON tag for `image_edit_model`.
- `internal/service/settings_service_test.go` -- include new field in fixture maps (lines ~84, 208).
- `internal/pipeline/image_track.go` -- read-only; confirm `ImageTrackConfig`, `CharacterResolver`, output-path conventions match implementation.
- `cmd/pipeline/serve.go` -- replace `imageTrackStub` with real client + `pipeline.NewImageTrack`; extend `buildPhaseBRunner` and `dynamicPhaseBExecutor` to thread `pipeline.CharacterResolver`.
- `internal/pipeline/image_track_test.go` -- unchanged (uses `fakeImageGen`); referenced for contract.

## Tasks & Acceptance

**Execution:**
- [ ] `internal/domain/config.go` -- add `ImageEditModel string` (yaml/mapstructure `image_edit_model`); fix `ImageModel` default to `"qwen-image"`; default `ImageEditModel` to `"qwen-image-edit"` -- prerequisite for client wiring.
- [ ] `internal/config/loader.go` + `internal/config/settings_files.go` -- propagate the new field through Viper defaults and yaml file I/O -- otherwise reload-after-edit erases the field.
- [ ] `internal/service/settings_service.go` + `settings_types.go` -- expose `ImageEditModel` to settings API/UI plumbing -- consistency with all other model fields.
- [ ] Test updates in `internal/domain/config_test.go` + `internal/service/settings_service_test.go` -- lock in defaults + JSON shape.
- [ ] `internal/llmclient/dashscope/image.go` -- new `ImageClient`, `ImageClientConfig`, `NewImageClient(*http.Client, ImageClientConfig) (*ImageClient, error)`; method bodies do submit-poll-download-atomic-write (see Design Notes for shape).
- [ ] `internal/llmclient/dashscope/image_test.go` -- TDD against the I/O matrix; one test per matrix row (10 cases).
- [ ] `cmd/pipeline/serve.go` -- (a) extend `buildPhaseBRunner` with `characterResolver pipeline.CharacterResolver` parameter, (b) instantiate `dashscope.NewImageClient` with region-aware endpoint, (c) build `pipeline.NewImageTrack(...)` with all dependencies, (d) delete `imageTrackStub` block, (e) add `characterResolver` field to `dynamicPhaseBExecutor` and forward through `Run`, (f) in `runServe` pass `characterSvc` (already implements `GetSelectedCandidate`) into the executor.
- [ ] `go test ./...` end-to-end pass; `go build ./...` clean.

**Acceptance Criteria:**
- Given a fresh run that reaches the image stage with one character scene and one non-character scene, when Phase B runs, then `Image.Edit` is invoked for the character shot and `Image.Generate` for the non-character shot, both writing PNG files at `{outputDir}/{runID}/images/scene_XX/shot_XX.png`.
- Given the image client receives an HTTP 429 on submit, when it returns, then the error wraps `domain.ErrRateLimited` and the outer `WithRetry` retries (verified via callback assertion in test).
- Given a fake DashScope task transitions PENDING→RUNNING→SUCCEEDED across two polls, when the client polls, then the wall-clock backoff sums ≥ 2s+4s and ≤ 60s, and the final image is written atomically (no partial file ever observable on disk).
- Given the spec is fully landed, when `grep -n "imageTrackStub" cmd/pipeline/serve.go` runs, then it returns no matches.
- Given default config with no `image_edit_model` set, when the pipeline loads, then `cfg.ImageEditModel == "qwen-image-edit"` and `cfg.ImageModel == "qwen-image"`.

## Spec Change Log

## Design Notes

**DashScope text2image is async** (unlike TTS). Two endpoints:
- Submit: `POST {endpoint}/api/v1/services/aigc/text2image/image-synthesis` with header `X-DashScope-Async: enable` → returns `{output:{task_id}}`.
- Poll: `GET {endpoint}/api/v1/tasks/{task_id}` → `{output:{task_status, results:[{url}]}}`.

`endpoint` is region-aware: Intl `https://dashscope-intl.aliyuncs.com`, CN `https://dashscope.aliyuncs.com`. Mirror TTS region selection.

Body shape (Generate vs Edit):
```jsonc
{
  "model": "qwen-image",                      // or "qwen-image-edit"
  "input": {
    "prompt": "...",
    "ref_imgs": ["https://..."]               // Edit only
  },
  "parameters": { "size": "1024*1024", "n": 1 }
}
```

Polling: `2s, 4s, 8s, 16s, 30s` (cap each step at 30s; cumulative cap 60s) — driven by `clk.Sleep` so test can fast-forward. On `task_status:"FAILED"` return `ErrValidation`; on cumulative > 60s return `ErrUpstreamTimeout`.

Atomic write: `os.WriteFile(tmp, bytes, 0o644)` then `os.Rename(tmp, OutputPath)`. Caller (image_track.go) is responsible for parent dirs (already does `os.MkdirAll`).

Cost estimate: `costPerImage = 0.02` USD constant — annotate source URL in a one-line comment so future Jay can audit.

Download cap: `imageDownloadLimit = 50 << 20` (50 MiB) wrapping `io.LimitReader`, mirroring TTS's `audioDownloadLimit`.

`buildPhaseBRunner` shape after change:
```go
func buildPhaseBRunner(
    cfg domain.PipelineConfig,
    dashScopeAPIKey string,
    limiterFactory *llmclient.ProviderLimiterFactory,
    runStore *db.RunStore,
    segStore *db.SegmentStore,
    characterResolver pipeline.CharacterResolver,
    logger *slog.Logger,
) (*pipeline.PhaseBRunner, error)
```

## Verification

**Commands:**
- `go test ./internal/llmclient/dashscope/... -run Image -count=1` -- expected: all new tests pass.
- `go test ./...` -- expected: full suite green (including `image_track_test.go`, settings tests).
- `go vet ./...` -- expected: clean.
- `grep -n "imageTrackStub" cmd/pipeline/serve.go` -- expected: no output.

**Manual checks (if no CLI):**
- `./startup.sh dev` → create new run via UI → progress through Phase A and character_pick → observe logs for `image track shot` lines (Provider=`dashscope`, `cost_usd>0`) and verify `{outputDir}/{runID}/images/scene_XX/shot_XX.png` files exist on disk for at least one character + one non-character shot.

## Suggested Review Order

**Client core**

- New DashScope text2image client — submit + async poll + atomic write entry point.
  [`image.go:88`](../../internal/llmclient/dashscope/image.go#L88)

- Job-runner skeleton — error taxonomy and elapsed-time observation in one place.
  [`image.go:213`](../../internal/llmclient/dashscope/image.go#L213)

- Poll loop — sleep-first cadence with cumulative `pollMaxWall` cap surfacing `ErrUpstreamTimeout`.
  [`image.go:298`](../../internal/llmclient/dashscope/image.go#L298)

- HTTP status taxonomy — 429/5xx/4xx mapping shared across submit + poll phases.
  [`image.go:401`](../../internal/llmclient/dashscope/image.go#L401)

- Atomic temp+rename write — Story 5.4 idempotency contract enforced at the bytes layer.
  [`image.go:425`](../../internal/llmclient/dashscope/image.go#L425)

**Phase B wiring**

- `buildPhaseBRunner` replaces the no-op stub with real image client + image track.
  [`serve.go:86`](../../cmd/pipeline/serve.go#L86)

- Region-aware endpoint selection mirroring TTS for Intl/CN parity.
  [`serve.go:65`](../../cmd/pipeline/serve.go#L65)

- `dynamicPhaseBExecutor` carries `CharacterResolver` so Phase B regenerations resolve consistently.
  [`serve.go:138`](../../cmd/pipeline/serve.go#L138)

- `runServe` constructs `characterSvc` before the executor so the resolver wires through.
  [`serve.go:281`](../../cmd/pipeline/serve.go#L281)

- Resume path threads its own `characterSvc` so resume-after-failure produces the same Edit calls.
  [`resume.go:69`](../../cmd/pipeline/resume.go#L69)

**Config surface**

- `ImageEditModel` added to `PipelineConfig`; old `qwen-max-vl` (vision-LLM, not generator) replaced.
  [`config.go:24`](../../internal/domain/config.go#L24)

- New defaults `qwen-image` + `qwen-image-edit` lock the routing model split into the schema.
  [`config.go:113`](../../internal/domain/config.go#L113)

- Viper default plus yaml file struct keep reload-after-edit from erasing the field.
  [`loader.go:41`](../../internal/config/loader.go#L41)
  [`settings_files.go:110`](../../internal/config/settings_files.go#L110)

- Settings service plumbs the field through validation, normalize, and apply paths.
  [`settings_service.go:309`](../../internal/service/settings_service.go#L309)

**Tests**

- 12 cases cover the I/O matrix end-to-end; all use `BlockExternalHTTP` + `httptest.Server`.
  [`image_test.go:194`](../../internal/llmclient/dashscope/image_test.go#L194)

- Poll timeout case drives `FakeClock` so the 60s wall is asserted deterministically.
  [`image_test.go:380`](../../internal/llmclient/dashscope/image_test.go#L380)

- Settings handler + service fixtures updated for the new required field.
  [`handler_settings_test.go:58`](../../internal/api/handler_settings_test.go#L58)
  [`settings_service_test.go:85`](../../internal/service/settings_service_test.go#L85)
