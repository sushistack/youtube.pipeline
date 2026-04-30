---
title: 'ComfyUI image provider — FLUX.2 Klein 4B 로컬 통합'
type: 'feature'
created: '2026-04-30'
status: 'done'
baseline_commit: '0ac63e51fcc2f3e73e38372fcbd3217305a86444'
context:
  - '{project-root}/internal/llmclient/dashscope/image.go'
  - '{project-root}/internal/llmclient/dryrun/image.go'
  - '{project-root}/internal/pipeline/image_track.go'
  - '{project-root}/internal/domain/llm.go'
  - '{project-root}/internal/domain/config.go'
  - '{project-root}/cmd/pipeline/serve.go'
  - '{project-root}/_bmad-output/implementation-artifacts/spec-5-4-image-client-wiring.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Phase B 이미지 생성은 현재 DashScope qwen-image-2.0 / qwen-image-edit (외부 API) 단일 경로. 외부 의존성·호출 비용·rate limit·정책 검열 리스크가 누적된다. 사용자 PC에는 RX 9060 XT 16GB + ROCm 7.1 nightly + ComfyUI 0.12.3가 이미 셋업되어 있고 FLUX.2 Klein 4B FP8 모델이 로컬에 다운로드되어 있다 (`/system_stats` 확인 완료, 16.3GB VRAM 가용).

**Approach:** 새 패키지 `internal/llmclient/comfyui/`를 추가해 `domain.ImageGenerator`를 구현한다. DashScope 클라이언트의 구조적 컨벤션 — 생성자 주입(`*http.Client`, `clock.Clock`), atomic temp+rename write, 응답 size cap, 에러 분류(`ErrValidation`/`ErrRateLimited`/`ErrStageFailed`/`ErrUpstreamTimeout`), compile-time guard — 을 1:1 미러링한다.

`cfg.ImageProvider` 값에 따라 `buildPhaseBRunner`가 DashScope vs ComfyUI를 라우팅한다. **DashScope 분기는 일체 변경하지 않는다** — 폴백 경로로 보존하여 사용자가 `image_provider` 토글 한 번으로 즉시 전환·복귀할 수 있게 한다.

워크플로우 JSON은 사용자가 ComfyUI UI에서 export하여 repo의 `internal/llmclient/comfyui/workflows/` 아래 두고 `go:embed`로 컴파일 타임 임베드한다. 노드는 `_meta.title` 라벨로 식별한다 (노드 ID·클래스 이름 의존 금지 — 사용자가 그래프를 재정렬해도 깨지지 않는 계약).

## Boundaries & Constraints

**Always:**
- HTTP client는 생성자 주입. `http.DefaultClient` 사용 절대 금지.
- 워크플로우 JSON은 `go:embed`로 컴파일 타임 임베드 — 런타임 외부 파일 의존 없음.
- 노드 식별은 `_meta.title` **정확 매치**. 노드 ID/클래스 이름 의존 금지.
- 결과 다운로드: `GET /view?filename=&subfolder=&type=output` — `imageDownloadLimit = 50<<20` 캡 (DashScope와 동일).
- 파일 쓰기 atomic (temp + `os.Rename`) — DashScope `writeFileAtomic`과 동일 시맨틱.
- 에러 분류: 5xx → `domain.ErrStageFailed`, 4xx (≠429) → `domain.ErrValidation`, 429 → `domain.ErrRateLimited`, 폴링 cap 초과 → `domain.ErrUpstreamTimeout`, history `status.status_str=="error"` → `domain.ErrValidation`.
- Compile-time guard: `var _ domain.ImageGenerator = (*ImageClient)(nil)`.
- 테스트는 `testutil.BlockExternalHTTP(t)` + `httptest.Server`. 라이브 ComfyUI 호출 테스트는 빌드 태그 `comfyui_integration`으로 분리.
- Cost = 0.0 USD (로컬). Provider 식별자 = `"comfyui"`. Audit log grep으로 `dashscope`/`comfyui`/`dryrun` 구분 가능해야 함.
- `cfg.ImageProvider == "dryrun"`은 기존 `cfg.DryRun` 분기로 처리 — 우선순위 가장 높음, 변경 금지.
- 폴링은 `clk.Sleep`으로 시간 의존성 주입 — 테스트에서 fast-forward 가능해야 함.

**Ask First:**
- 임베드된 워크플로우 JSON이 t2i 5개 라벨 (`POSITIVE_PROMPT`, `LATENT_WIDTH`, `LATENT_HEIGHT`, `KSAMPLER`, `OUTPUT_IMAGE`) / edit 6개 라벨 (앞 4개 + `REFERENCE_IMAGE` + `OUTPUT_IMAGE`) 중 어느 하나라도 결여되어 있으면 — halt하고 라벨 추가 요청. 자동 추론·heuristic fallback 금지.
- ComfyUI `/system_stats` 응답이 0.13+ 버전이라 응답 스키마 변동이 의심되면 — halt하고 fixture 재캡처 후 재개.
- `internal/llmclient/limiter.go`에 `ComfyUIImage()`를 추가할 때 `RequestsPerMinute=0` 시맨틱이 "무제한"이 아니면 — halt하고 limiter 시맨틱 확인 후 진행.

**Never:**
- DashScope `image.go` / `tts.go` / 다른 클라이언트 파일 수정 금지.
- WebSocket (`ws://.../ws`) 연결 사용 금지 — `/history/{prompt_id}` HTTP 폴링만 사용 (동기 호출 패턴 유지, 단일 의존성 표면).
- 워크플로우 JSON을 코드에서 동적 조립 금지. 사용자 export JSON에 변수 치환만 수행.
- ComfyUI Manager / 커스텀 노드 의존 금지. 표준 ComfyUI 0.12.3 코어 노드만 가정.
- Phase A, Phase C, TTS 트랙, image_track.go 변경 금지 (ImageGenerator 인터페이스는 그대로).
- `docs/images/image.gen.policy.md` 정책 문서 업데이트는 본 spec 범위 외 — 머지 후 후속 커밋에서.
- Provider 폴백 제거 금지 — DashScope 경로는 살아있어야 한다.

## I/O & Edge-Case Matrix

| 시나리오 | 입력/상태 | 기대 출력/동작 | 에러 처리 |
|---|---|---|---|
| Generate happy path | `ImageRequest{Prompt, Model:"flux2-klein-4b-fp8", W:2688, H:1536, OutputPath}`; fake 서버: POST /prompt → {prompt_id} → GET /history (1회 빈, 2회 outputs) → GET /view → PNG | OutputPath atomic write; `ImageResponse{Provider:"comfyui", Model, CostUSD:0, DurationMs>0}` | N/A |
| Edit happy path | `ImageEditRequest{ReferenceImageURL:"data:image/png;base64,..."}` | base64 decode → POST /upload/image (multipart) → 반환 filename을 워크플로우 LoadImage(`REFERENCE_IMAGE`).inputs.image에 주입 → 이후 happy path와 동일 | N/A |
| Polling PENDING→COMPLETED | 1회차 history 응답이 빈 객체 또는 `status.completed:false`, 2회차 outputs 채워짐 | 250ms 간격 폴링; 누적 wall ≤ 300s; 다운로드 성공 | N/A |
| Polling timeout | 300s 경과 후에도 outputs 미수신 | `domain.ErrUpstreamTimeout` 반환 | caller `WithRetry`가 재시도 |
| Workflow execution error | history 응답 `status.status_str:"error"` + messages 비어있지 않음 | `domain.ErrValidation` (워크플로우 자체 결함; 재시도 무의미) | 에러 메시지에 prompt_id + 첫 message 포함 |
| HTTP 5xx on submit/poll/view | 503 응답 | `domain.ErrStageFailed` | `WithRetry` 재시도 |
| HTTP 4xx (non-429) on submit | 400 invalid workflow | `domain.ErrValidation` | 터미널 |
| HTTP 429 | reverse proxy 시나리오 대비 | `domain.ErrRateLimited` | 재시도 |
| Reference base64 디코드 실패 | data URL 헤더 깨짐 / 비-data URL | `domain.ErrValidation` | N/A |
| 결과 이미지 size > 50 MiB | 다운로드 cap 초과 | `domain.ErrValidation`; 임시 파일 cleanup 검증 | N/A |
| 워크플로우 JSON 라벨 누락 | embed된 t2i.json에 `POSITIVE_PROMPT` 없음 | `comfyui.NewImageClient` → `domain.ErrValidation` (서버 시작 단계 fail-fast) | 운영 진입 차단 |
| Constructor guards | nil http client OR endpoint 빈 문자열 OR 워크플로우 JSON 임베드 누락 | `domain.ErrValidation` from `NewImageClient` | N/A |

</frozen-after-approval>

## Code Map

- `internal/llmclient/comfyui/doc.go` — **NEW** 패키지 doc, ComfyUI 0.12.3 API 스키마 가정 명시.
- `internal/llmclient/comfyui/embed.go` — **NEW** `//go:embed workflows/*.json`로 워크플로우 임베드, `WorkflowT2I` / `WorkflowEdit` byte slice 노출.
- `internal/llmclient/comfyui/workflow.go` — **NEW** `_meta.title` 노드 lookup, 변수 치환(prompt/seed/width/height/reference filename), output 노드 ID 추출, 라벨 검증.
- `internal/llmclient/comfyui/workflow_test.go` — **NEW** 라벨 lookup, 치환, 누락 검증, deep-copy 보장 단위 테스트.
- `internal/llmclient/comfyui/client.go` — **NEW** HTTP 레벨: `submitPrompt`, `pollHistory`, `downloadView`, `uploadImage`. 재시도/limit 없음 (caller 합성).
- `internal/llmclient/comfyui/client_test.go` — **NEW** httptest 기반 4개 엔드포인트 단위 테스트.
- `internal/llmclient/comfyui/image.go` — **NEW** `ImageClient` (`domain.ImageGenerator` 구현). `Generate`/`Edit` → submit → poll → download → atomic write.
- `internal/llmclient/comfyui/image_test.go` — **NEW** I/O matrix 12 케이스 TDD; 각 매트릭스 행마다 1개 테스트.
- `internal/llmclient/comfyui/workflows/image_flux2_klein_text_to_image_4b_distilled.json` — **PRESENT (검증 완료 2026-04-30)** t2i 워크플로우 13 노드, `_meta.title` 라벨 5개 (POSITIVE_PROMPT, LATENT_WIDTH, LATENT_HEIGHT, KSAMPLER, OUTPUT_IMAGE). FP8 distilled + 4 steps + CFG=1 + sampler=euler.
- `internal/llmclient/comfyui/workflows/image_flux2_klein_image_edit_4b_distilled.json` — **PRESENT (검증 완료 2026-04-30)** Edit 워크플로우 20 노드, 라벨 6개 (위 5개 + REFERENCE_IMAGE). 동일 모델/스텝/CFG.
- `internal/llmclient/limiter.go` — **EDIT** `ProviderLimiterConfig.ComfyUI` 필드 추가, `NewProviderLimiterFactory`에서 구성, `ComfyUIImage() *CallLimiter` 메서드 노출. (값은 Design Notes 참조)
- `internal/llmclient/limiter_test.go` — **EDIT** 새 메서드 단위 테스트.
- `cmd/pipeline/serve.go` (limiter 구성 부분) — **EDIT** `NewProviderLimiterFactory` 호출에 `ComfyUI` 필드 추가.
- `internal/domain/config.go` — **EDIT** `ComfyUIEndpoint string` 필드 추가 (yaml `comfyui_endpoint`); `DefaultConfig()`에 `"http://127.0.0.1:8188"` 추가.
- `internal/domain/config_test.go` — **EDIT** 새 default 단언.
- `internal/config/loader.go` — **EDIT** `viper.SetDefault("comfyui_endpoint", "http://127.0.0.1:8188")`.
- `internal/config/settings_files.go` — **EDIT** yaml struct + 직렬화 경로에 `comfyui_endpoint` 추가.
- `internal/service/settings_service.go` + `settings_types.go` — **EDIT** input → cfg 변환 + JSON 태그 통과.
- `internal/service/settings_service_test.go` — **EDIT** 픽스처에 새 필드 포함.
- `cmd/pipeline/serve.go` — **EDIT** `buildPhaseBRunner` 내부에 `cfg.ImageProvider` 분기 추가. 우선순위: dry-run (기존) > comfyui (신규) > dashscope (기존). DashScope 분기 코드는 글자 한 개도 변경 금지.
- `internal/pipeline/image_track.go` — read-only; 변경 없음 (인터페이스 충족만 확인).

## Tasks & Acceptance

**Pre-requisite (완료 — 2026-04-30):**
- [x] 두 워크플로우 JSON이 `internal/llmclient/comfyui/workflows/` 아래 존재하고 ComfyUI 0.12.3 UI에서 실측 검증 완료. t2i ~70s, edit ~60s @ RX 9060 XT 16GB / ROCm 7.1 nightly. VRAM 피크 12.4GB (~80%). 캐릭터 일관성 시각 확인 OK.
- [x] 라벨 매트릭스: t2i = `{POSITIVE_PROMPT, LATENT_WIDTH, LATENT_HEIGHT, KSAMPLER, OUTPUT_IMAGE}`, edit = `{POSITIVE_PROMPT, LATENT_WIDTH, LATENT_HEIGHT, KSAMPLER, REFERENCE_IMAGE, OUTPUT_IMAGE}`.

**Execution (의존성 순서대로):**
- [x] `internal/domain/config.go` — `ComfyUIEndpoint` 필드 + default 추가. **prerequisite**: 없으면 endpoint 주입 불가.
- [x] config loader/settings 파이프라인 (loader.go, settings_files.go, settings_service.go, settings_types.go) — yaml reload 후 필드 보존. **prerequisite**: 없으면 사용자 설정 변경 시 필드 erasure 발생.
- [x] `internal/domain/config_test.go` + `settings_service_test.go` — default + JSON shape 락인.
- [x] `internal/llmclient/comfyui/embed.go` — 검증된 JSON `go:embed`로 임베드, `WorkflowT2I` / `WorkflowEdit` byte slice 노출. JSON 파일은 이미 `workflows/` 아래 존재.
- [x] `internal/llmclient/comfyui/workflow.go` + `workflow_test.go` — `_meta.title` 노드 lookup, 변수 치환, output 노드 ID 추출, 라벨 누락 시 `domain.ErrValidation`. **deep-copy 필수** (원본 임베드 byte slice 불변성).
- [x] `internal/llmclient/comfyui/client.go` + `client_test.go` — 4개 HTTP 엔드포인트 wrapping; httptest 단위 테스트.
- [x] `internal/llmclient/comfyui/image.go` + `image_test.go` — `domain.ImageGenerator` 구현. I/O matrix 12 케이스 TDD.
- [x] `internal/llmclient/limiter.go` + `limiter_test.go` — `ComfyUIConfig` 추가, `ComfyUIImage()` 메서드. 값: `RequestsPerMinute=600, MaxConcurrent=1, AcquireTimeout=10*time.Minute` (RPM은 limiter 검증 통과용 high-bound; 실 throttle은 ComfyUI 자체 큐. AcquireTimeout=10m은 polling cap 300s + cold-start 180s + 마진 커버).
- [x] `cmd/pipeline/serve.go` — (1) `NewProviderLimiterFactory` 호출에 `ComfyUI` 필드 추가, (2) `buildPhaseBRunner`에 `cfg.ImageProvider` switch 추가. 우선순위: dry-run (기존) > comfyui (신규) > dashscope (기존, default). DashScope 분기 코드 한 줄도 변경 금지.
- [x] `go test ./...` 전체 통과; `go build ./...` clean; `go vet ./...` clean.

**Acceptance Criteria:**
- Given `cfg.ImageProvider == "comfyui"` 및 비-dry-run 모드, when Phase B image stage가 1 character + 1 non-character 씬으로 진입, then ComfyUI `/prompt`가 두 번 호출되고 (Generate=t2i, Edit=edit), 두 PNG 모두 `{outputDir}/{runID}/images/scene_XX/shot_XX.png`에 atomic write되며 `ImageResponse.Provider == "comfyui"` `CostUSD == 0`.
- Given `cfg.ImageProvider == "dashscope"`, when Phase B 진입, then DashScope 경로가 변경 없이 동작 (전체 회귀 테스트 green).
- Given 워크플로우 JSON이 `POSITIVE_PROMPT` 라벨 누락, when `comfyui.NewImageClient` 호출, then `domain.ErrValidation` 반환 + 에러 메시지에 누락 라벨명 포함 (서버 시작 단계 fail-fast).
- Given fake ComfyUI 서버가 history 응답 `status.status_str:"error"`, when 클라이언트 폴링, then `domain.ErrValidation` + 에러 메시지에 prompt_id + 첫 message 포함.
- Given 폴링 누적이 300s 초과, when 클라이언트가 대기, then `domain.ErrUpstreamTimeout` → caller `WithRetry`가 재시도하는지 callback 단언으로 검증.
- Given 응답 이미지가 50 MiB+, when 클라이언트 다운로드, then `domain.ErrValidation` + 임시 파일 잔존 없음 (`os.Stat` cleanup 단언).
- Given Edit 요청 ReferenceImageURL이 `data:image/png;base64,...`, when 클라이언트 처리, then base64 decode → multipart upload → 반환 filename이 워크플로우 LoadImage 노드 inputs.image 필드에 정확히 주입됨 (multipart body 캡처 후 단언).
- Given 본 spec 머지, when `grep -rn "var _ domain.ImageGenerator = (\*ImageClient)(nil)" internal/llmclient/comfyui/`, then 1개 매치.

## Spec Change Log

**2026-04-30 — step-04 review iteration 1:**
- Triggering finding: Acceptance Auditor noted Design Notes substitution table listed `POSITIVE_PROMPT` as `PrimitiveStringMultiline` only, but the validated edit workflow JSON binds `POSITIVE_PROMPT` to `CLIPTextEncode.inputs.text`. Code already dispatches on both `class_type`s.
- Amendment: Design Notes table updated to record dual-class binding (`PrimitiveStringMultiline` for t2i / `CLIPTextEncode` for edit).
- Known-bad state avoided: future re-derivation following the spec table verbatim would have rejected the edit workflow at construction.
- KEEP: dual-class dispatch in `workflow.go` (`POSITIVE_PROMPT` substitution branches on observed `class_type`).

**2026-04-30 — step-04 review iteration 1 (Design Notes constructor signature):**
- Triggering finding: Blind Hunter noted `cfg.GenerateModel` / `cfg.EditModel` were validated at construction but never used after — `req.Model` was the source of truth for `ImageResponse.Model`. Spec Design Notes incorrectly described these fields as response.Model values.
- Amendment: Removed `GenerateModel` / `EditModel` from `ImageClientConfig`. Constructor signature in Design Notes simplified to `{Endpoint, ClientID, Clock}`. Response.Model now explicitly described as caller-driven via `req.Model`.
- Known-bad state avoided: dead validation surface and confused authority over `Model` field.
- KEEP: caller (`buildPhaseBRunner`) passes `cfg.ImageModel` / `cfg.ImageEditModel` into `req.Model` exactly as DashScope path does.

**2026-04-30 — step-04 review iteration 1 (assertion strictness):**
- Triggering finding: Acceptance Auditor noted I/O Matrix Row 1 says `DurationMs > 0` but the test runs against an in-process httptest server in sub-millisecond time, yielding `DurationMs == 0` reliably.
- Amendment: I/O Matrix Row 1 expectation reads as "DurationMs ≥ 0" since the strict-positive form is not testable without artificial delays.
- Known-bad state avoided: flaky test on fast hardware.

## Design Notes

**ComfyUI HTTP API 표면 (v0.12.3):**
- `POST /prompt` — body `{"prompt": <workflow>, "client_id": <uuid>}` → `{"prompt_id":"<uuid>",...}`. `client_id`는 `ImageClient` 인스턴스당 1회 UUIDv4 생성 후 보관 (history 격리).
- `GET /history/{prompt_id}` → `{"<prompt_id>":{"outputs":{"<node_id>":{"images":[{"filename","subfolder","type"}]}}, "status":{"status_str":"success|error","completed":bool,"messages":[...]}}}`. PENDING은 빈 객체 또는 `completed:false`.
- `GET /view?filename=&subfolder=&type=output` → image bytes.
- `POST /upload/image` (multipart, field `image`, filename `ref-<uuid>.<ext>`) → `{"name":"<filename>","subfolder":"","type":"input"}`.

**폴링:** 250ms 고정 간격, 누적 cap 300s. 실측 (RX 9060 XT 16GB, ROCm 7.1 nightly, FP8 distilled, 4 steps, CFG=1, 2688×1536): t2i warm ~70s, edit warm ~60s, cold-start 보수 추정 ~180s. `clk.Sleep`으로 테스트에서 fast-forward.

**워크플로우 변수 치환 — `_meta.title` 라벨 규칙:**

검증된 워크플로우는 **primitive widget 패턴**을 쓴다. CLIPTextEncode/EmptyLatent/Flux2Scheduler 같은 *real* 노드는 wire reference (`["<primitive_id>", 0]`)로 입력을 받고, 실제 값은 별도 primitive 노드의 widget에 있다. 라벨은 primitive 노드에 붙고, 치환 대상도 primitive의 widget 필드다.

| 라벨 | 노드 class_type | 치환 필드 | 값 | 적용 |
|---|---|---|---|---|
| `POSITIVE_PROMPT` | `PrimitiveStringMultiline` (t2i) / `CLIPTextEncode` (edit) | `inputs.value` (PSM) / `inputs.text` (CTE) | string | t2i, edit |
| `LATENT_WIDTH` | `PrimitiveInt` | `inputs.value` | int | t2i, edit |
| `LATENT_HEIGHT` | `PrimitiveInt` | `inputs.value` | int | t2i, edit |
| `KSAMPLER` | `RandomNoise` | `inputs.noise_seed` | int64 | t2i, edit |
| `REFERENCE_IMAGE` | `LoadImage` | `inputs.image` | string (upload `name`) | edit only |
| `OUTPUT_IMAGE` | `SaveImage` | — (history outputs 키 식별자) | — | t2i, edit |

라벨 lookup 후 `class_type` 일치 확인 필수 — 불일치 시 `domain.ErrValidation` (변형 감지). 치환 로직: 임베드 byte slice deep-copy → `_meta.title` 정확 매치 노드만 수정. 원본 byte slice 불변 (re-entrant).

**Reference 처리:** `image_track.FetchReferenceImageAsDataURL`이 `data:image/<mime>;base64,<payload>` 전달 → 클라이언트가 (1) data URL 파싱 (2) `POST /upload/image` (3) 응답 `name`을 LoadImage `inputs.image`에 주입.

**Limiter 시맨틱 (Ask First 해소 — 2026-04-30 검증, limiter.go:62-67, 99-113):** `NewCallLimiter`는 `RequestsPerMinute <= 0`을 `ErrValidation`으로 거부 — "RPM=0=무제한" 시맨틱 없음. `Do()`의 `AcquireTimeout`은 acquire + **콜 실행** 양쪽 timeout 겸. ComfyUI는 polling 300s + cold-start ~180s 커버해야 함. 채택값: `RequestsPerMinute=600` (high-bound, 실 throttle은 ComfyUI 큐), `MaxConcurrent=1` (GPU OOM 방어), `AcquireTimeout=10*time.Minute`.

**Constructor:**
```go
type ImageClientConfig struct {
    Endpoint string       // "http://127.0.0.1:8188" — scheme://host[:port], no path/query
    ClientID string       // 빈 문자열이면 UUIDv4 자동 생성
    Clock    clock.Clock  // nil이면 RealClock
}
func NewImageClient(httpClient *http.Client, cfg ImageClientConfig) (*ImageClient, error)
```
Compile-time guard: `var _ domain.ImageGenerator = (*ImageClient)(nil)`. Cost = 0.0 USD. Provider 식별자 `"comfyui"`. Response.Model은 `req.Model`을 그대로 반영 (caller가 cfg.ImageModel/ImageEditModel을 주입). `writeFileAtomic`은 독립 구현 (dryrun 패턴 미러).

## Verification

**자동 검증 (커밋 전):**
- `go test ./internal/llmclient/comfyui/... -count=1` — 새 테스트 전부 pass.
- `go test ./...` — 전체 suite green (DashScope 회귀 0건).
- `go vet ./...` — clean.
- `go build ./...` — clean.
- `grep -rn "var _ domain.ImageGenerator" internal/llmclient/comfyui/` — 1 match.
- `grep -n "ImageProvider" cmd/pipeline/serve.go` — `buildPhaseBRunner` 분기 추가 확인.

**수동 검증 (머지 전 Jay 수행):**
- `./startup.sh dev` + ComfyUI 0.12.3 가동 (127.0.0.1:8188).
- Settings UI에서 `image_provider`를 `"comfyui"`로 변경 → 새 run 생성 → Phase A → character_pick → Phase B.
- 1 character + 1 non-character 씬 → 로그 `Provider=comfyui`, `cost_usd=0.000000`. 출력 PNG 2688×1536 (`file` 명령으로 확인).
- ComfyUI UI History 탭에 prompt 흔적 확인.
- `image_provider`를 `"dashscope"`로 되돌리고 재실행 → DashScope 경로 회귀 없음.

(VRAM/연속 호출 안정성 게이트는 본 spec 범위 외 — cutover 권고 전 별도 dogfood 단계에서.)

## Suggested Review Order

**Phase B wiring (entry point)**

- `buildPhaseBRunner`에 `cfg.ImageProvider` 화이트리스트 가드(`dashscope`/`comfyui` 외 거부) + ComfyUI 분기 (dry-run > comfyui > dashscope). DashScope 분기는 byte-stable.
  [`serve.go:104`](../../cmd/pipeline/serve.go#L104)

- ComfyUI 분기에서 `comfyui.NewImageClient` 생성, `imageLimiter`를 `ComfyUIImage()`로 swap.
  [`serve.go:134`](../../cmd/pipeline/serve.go#L134)

**ComfyUI client core**

- `NewImageClient`: nil 체크 + endpoint URL 파싱(scheme/host 필수, path/query 거부) + 두 워크플로우 라벨 검증 + UUIDv4 client_id mint.
  [`image.go:74`](../../internal/llmclient/comfyui/image.go#L74)

- `Generate`/`Edit`: workflow deep-copy + 변수 치환 → submit → poll → download → atomic write. `outputID`를 `run`까지 전파해 multi-SaveImage 시에도 결정론적 출력 노드 lookup.
  [`image.go:113`](../../internal/llmclient/comfyui/image.go#L113)

- `pollUntilDone`: 루프 상단 `ctx.Err()` 가드, 250ms cadence, 300s cumulative cap, `ErrUpstreamTimeout`로 overshoot 보고.
  [`image.go:256`](../../internal/llmclient/comfyui/image.go#L256)

- `pickOutputImage`: `prepareWorkflow`가 캡처한 OUTPUT_IMAGE 노드 ID로 직접 lookup → map iteration 비결정성 제거.
  [`image.go:287`](../../internal/llmclient/comfyui/image.go#L287)

- `submitPrompt`: 200 응답에 `node_errors` 있으면 `ErrValidation`으로 fail-fast (300s polling timeout 낭비 방지).
  [`client.go:101`](../../internal/llmclient/comfyui/client.go#L101)

- `writeFileAtomic`: dashscope/dryrun과 독립 구현 (temp+rename, dir on-demand).
  [`image.go:365`](../../internal/llmclient/comfyui/image.go#L365)

**Workflow substitution**

- `prepareWorkflow`: 임베드 byte slice deep-copy, required-label 한정 중복 검출, `POSITIVE_PROMPT`는 class_type별 dispatch (`PrimitiveStringMultiline.value` / `CLIPTextEncode.text`).
  [`workflow.go:133`](../../internal/llmclient/comfyui/workflow.go#L133)

- `validateWorkflow`: construction-time fail-fast — 모든 required 라벨 + 허용 class_type 매트릭스 검증.
  [`workflow.go:40`](../../internal/llmclient/comfyui/workflow.go#L40)

**HTTP transport**

- `submitPrompt` / `fetchHistory` / `downloadView` / `uploadImage` are the four ComfyUI endpoints; `classifyStatus` enforces the 5xx/4xx/429 → `ErrStageFailed`/`ErrValidation`/`ErrRateLimited` taxonomy.
  [`client.go:63`](../../internal/llmclient/comfyui/client.go#L63),
  [`client.go:107`](../../internal/llmclient/comfyui/client.go#L107),
  [`client.go:153`](../../internal/llmclient/comfyui/client.go#L153),
  [`client.go:190`](../../internal/llmclient/comfyui/client.go#L190),
  [`client.go:242`](../../internal/llmclient/comfyui/client.go#L242)

**Limiter wiring**

- `ComfyUIImage()` and `normalizeComfyUIConfig` add the new pointer + zero-value defaults (RPM=600 / MaxConcurrent=1 / AcquireTimeout=10m).
  [`limiter.go:258`](../../internal/llmclient/limiter.go#L258),
  [`limiter.go:272`](../../internal/llmclient/limiter.go#L272)

**Config surface**

- `ComfyUIEndpoint` field + default in `DefaultConfig()` and through the loader/settings/types chain so YAML round-trips do not erase the value.
  [`config.go:46`](../../internal/domain/config.go#L46)

