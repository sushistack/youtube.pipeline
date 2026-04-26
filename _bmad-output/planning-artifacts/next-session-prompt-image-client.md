# 다음 세션 프롬프트 — DashScope Image Client 구현 + Phase B 이미지 트랙 wiring

> 이 파일은 새 세션에서 그대로 복붙해서 시작용 프롬프트로 쓸 수 있도록 작성됨.
> 필요시 `세션 시작 컨텍스트` 섹션 위만 잘라서 붙이면 됨.

---

## 세션 시작 컨텍스트

`youtube.pipeline` 저장소의 Phase B 이미지 트랙은 오케스트레이션은 완성돼 있지만 (`internal/pipeline/image_track.go`), 실제 DashScope 이미지 클라이언트가 빠져 있어 [cmd/pipeline/serve.go:112-114](cmd/pipeline/serve.go#L112-L114)에서 no-op stub으로 wiring 돼 있다. 결과적으로 모든 run에서 이미지가 0장 생성됨. 이번 세션에서 이걸 끝까지 마무리한다.

**결정된 방향:** Story 5.4의 마무리. 새 패키지 `internal/llmclient/dashscope/image.go`에서 `domain.ImageGenerator` (Generate + Edit) 구현체 작성 → `cmd/pipeline/serve.go::buildPhaseBRunner`에서 stub 제거 후 실제 wiring. 테스트 우선 (외부 HTTP은 `testutil.BlockExternalHTTP(t)`로 차단, 인라인 페이크).

**세션 외 범위 (지금 건드리지 말 것):**
- 프론트 변경 (별 세션에서 처리됨 — Cancel 버튼, audio 정적 라우트)
- Phase A/C, Phase B의 TTS 트랙
- Story 5.1~5.3의 limiter / character resolver — 그대로 재사용

---

## 사전 학습 자료 (읽고 시작)

순서대로 훑고 시작:

1. **메모리 파일 (전부)** — `/home/jay/.claude/projects/-mnt-work-projects-youtube-pipeline/memory/MEMORY.md` 색인 보고 관련 항목 읽기. 특히:
   - `feedback_pipeline_rigor.md` — 파이프라인/상태머신 꼼꼼하게
   - `feedback_api_dashscope_only.md` — Qwen은 DashScope, SiliconFlow 금지
   - `feedback_no_dead_layers.md` — Vision/Growth로 미루지 말 것
   - `feedback_meta_principles.md` — 테스트 중심, 정직 timeline
2. [_bmad-output/implementation-artifacts/5-4-frozen-descriptor-propagation-per-shot-image-generation.md](_bmad-output/implementation-artifacts/5-4-frozen-descriptor-propagation-per-shot-image-generation.md) — 스토리 ACs (status: done이지만 클라이언트 wiring 빠짐)
3. [internal/pipeline/image_track.go](internal/pipeline/image_track.go) — 호출자 측이 어떤 인터페이스로 부르는지 정확히 확인
4. [internal/llmclient/dashscope/tts.go](internal/llmclient/dashscope/tts.go) — 가장 가까운 참고 구현. **중요한 패턴:**
   - HTTP client 주입 (절대 `http.DefaultClient` 금지)
   - URL/key region 분리 (Intl vs CN)
   - 외부 audio URL을 받아서 `OutputPath`로 다운로드 + 파일 작성
   - 구조체 필드: 응답 unmarshal → 정규화 → 다운로드 → cost/duration 계산
5. [internal/llmclient/dashscope/text.go](internal/llmclient/dashscope/text.go) — limiter / 에러 분류 / 재시도 합성 패턴
6. [internal/domain/llm.go](internal/domain/llm.go) — `ImageGenerator` interface, `ImageRequest`, `ImageEditRequest`, `ImageResponse` 시그니처
7. [internal/pipeline/image_track_test.go](internal/pipeline/image_track_test.go) — `fakeImageGen`이 어떤 동작을 흉내내는지 (Generate + Edit이 OutputPath에 파일을 써야 함)
8. [cmd/pipeline/serve.go](cmd/pipeline/serve.go) — `buildPhaseBRunner` 함수 + `dynamicPhaseBExecutor`. 둘 다 손봐야 함

---

## 구현 범위 (체크리스트)

### 1. `internal/llmclient/dashscope/image.go` 신규 파일

DashScope qwen-image API 두 개 엔드포인트를 지원:
- **Generate (text-to-image)**: 모델 `qwen-image` (또는 `qwen-image-plus`) — 텍스트 프롬프트로 이미지 생성
- **Edit (reference-based)**: 모델 `qwen-image-edit` — 캐릭터 참조 이미지 + 프롬프트 → 일관성 있는 이미지

설계 요점:
- 단일 `ImageClient` 구조체가 `domain.ImageGenerator` (Generate + Edit) 둘 다 구현
- `ImageClientConfig`: `APIKey`, `Endpoint` (region-aware, 기본은 Intl), `Limiter *llmclient.CallLimiter`, `Logger *slog.Logger`
- 생성자 `NewImageClient(*http.Client, ImageClientConfig) (*ImageClient, error)` — TTS/Text와 동일 가드 (nil http client, empty API key 등 → `domain.ErrValidation`)
- 두 메서드 모두 다음 시퀀스:
  1. 요청 body 구성 (DashScope text2image 스펙: `model`, `input.prompt`, `parameters.size` "1024*1024" 형식, Edit는 `input.ref_imgs` 배열 추가)
  2. POST → JSON 응답 → 비동기 task ID 받음 (DashScope text2image는 비동기 jobs API임 — TTS와 다름!)
  3. `GET /api/v1/tasks/{task_id}` 폴링 (status: PENDING → RUNNING → SUCCEEDED/FAILED)
  4. SUCCEEDED 시 응답에서 image URL 추출 → 다운로드 → `req.OutputPath`에 작성
  5. `domain.ImageResponse` 채워서 반환 (`ImagePath`, `Provider="dashscope"`, `Model`, `CostUSD` 추정값, `DurationMs`)
- HTTP 상태 분류는 [retry.go](internal/llmclient/retry.go) 패턴 따름 (429/5xx → 재시도, 4xx → 영구실패). 재시도는 호출자 (image_track의 `Limiter.Do` + `WithRetry`)에서 합성하므로 **클라이언트 자체는 retry/limiter 미보유**.
- 비용 추정: DashScope qwen-image 공식 가격 기준으로 상수 (예: `costPerImage = 0.02` USD). 출처 주석 달기.
- 응답 다운로드는 TTS의 `audioDownloadLimit` 패턴 — `imageDownloadLimit = 50 << 20` 정도로 캡 + `io.LimitReader`.
- 파일 작성은 atomic-ish: temp 파일 → rename. (TTS는 직접 쓰지만 이미지는 Story 5.4 spec이 멱등성을 명시했으니 더 깔끔하게.)

폴링 정책: 메소드 내부에서 작은 백오프 루프 (예: 2s → 4s → 8s, 최대 60s). `ctx`로 cancel 가능. 상한은 const로.

### 2. `internal/llmclient/dashscope/image_test.go` 신규 테스트

`testutil.BlockExternalHTTP(t)` 필수. `httptest.Server`로 fake DashScope 흉내. 최소 케이스:

- `TestImageClient_Generate_HappyPath` — Generate 호출 → submit + 1회 폴링 → SUCCEEDED → 다운로드 → 파일 존재 + ImageResponse 정상
- `TestImageClient_Edit_PassesReferenceURL` — Edit 요청 body의 `input.ref_imgs` 가 그대로 전달되는지
- `TestImageClient_Generate_PollUntilSucceeded` — PENDING 2회 후 SUCCEEDED, 백오프 동작
- `TestImageClient_TaskFailedSurfacesError` — FAILED 응답 시 typed 에러 (`domain.ErrValidation` 또는 별도 sentinel)
- `TestImageClient_HTTP5xxIsRetryable` — 재시도 가능 분류
- `TestImageClient_HTTP4xxIsTerminal` — 영구실패 분류
- `TestImageClient_DownloadCapEnforced` — 다운로드 응답이 캡 초과 시 거부
- `TestImageClient_CostsAccumulate` — CostUSD 계산 정확
- `TestImageClient_RejectsNilHTTPClient` / `TestImageClient_RejectsEmptyAPIKey` — 생성자 가드

테스트 작성 순서: 실패 → 통과 (TDD). 한 번에 다 짜지 말고 케이스 하나씩.

### 3. `internal/domain/config.go` — Image Edit Model 분리

현재 `PipelineConfig`에는 `ImageModel` 하나밖에 없음. Generate와 Edit 모델은 다르므로 (qwen-image vs qwen-image-edit), 추가 필드 필요:

```go
ImageEditModel string `yaml:"image_edit_model" mapstructure:"image_edit_model"`
```

기본값:
- `ImageModel`: `"qwen-image"` (현재 `"qwen-max-vl"`은 잘못된 default — VL은 vision-LLM이지 이미지 생성기 아님; 이번에 고침)
- `ImageEditModel`: `"qwen-image-edit"`

`config_test.go`, `settings_files.go` (107~110라인 부근), `loader.go`, `service/settings_files.go` 등 관련 곳 모두 추가:
- `grep -rn "ImageModel" internal/` 으로 영향받는 곳 다 수정
- 테스트 fixture (testdata 안의 yaml 등)에도 `image_edit_model` 추가

### 4. `cmd/pipeline/serve.go` — wiring

[cmd/pipeline/serve.go:65-123](cmd/pipeline/serve.go#L65-L123) `buildPhaseBRunner` 시그니처 확장:

```go
func buildPhaseBRunner(
    cfg domain.PipelineConfig,
    dashScopeAPIKey string,
    limiterFactory *llmclient.ProviderLimiterFactory,
    runStore *db.RunStore,
    segStore *db.SegmentStore,
    characterResolver pipeline.CharacterResolver,  // NEW
    logger *slog.Logger,
) (*pipeline.PhaseBRunner, error)
```

내부에서:
1. `dashscope.NewImageClient` 생성 (region-aware endpoint는 TTS와 동일하게 `cfg.DashScopeRegion` 사용; image용 endpoint 상수 따로 정의 — text2image는 `https://dashscope-intl.aliyuncs.com/api/v1/services/aigc/text2image/image-synthesis` 라인)
2. `pipeline.NewImageTrack(pipeline.ImageTrackConfig{...})` 호출 — 모든 의존성 주입:
   - `OutputDir`, `Provider="dashscope"`, `GenerateModel: cfg.ImageModel`, `EditModel: cfg.ImageEditModel`
   - `Width: 1024`, `Height: 1024` (스펙에 명시되지 않았다면 cfg에 추가)
   - `Images: imageClient` (방금 만든 클라이언트)
   - `CharacterResolver: characterResolver`
   - `Shots: segStore`
   - `Limiter: limiterFactory.DashScopeImage()`
   - `Clock`, `Logger`, `AuditLogger`
3. 결과를 `pipeline.NewPhaseBRunner` 첫 인자 (현재는 `imageTrackStub`)에 넘김. **stub 코드 완전 삭제.**

`dynamicPhaseBExecutor`도 손봐야 함 — `Run` 메서드가 `buildPhaseBRunner`를 호출할 때 `characterResolver`를 어디서 가져올지 결정 필요. `service.CharacterService`가 이미 `GetSelectedCandidate`를 구현하므로 [internal/service/character_service.go:254](internal/service/character_service.go#L254), 그걸 executor 구조체에 필드로 추가하고 `runServe`에서 주입.

```go
type dynamicPhaseBExecutor struct {
    settings          *service.SettingsService
    runStore          *db.RunStore
    segStore          *db.SegmentStore
    characterResolver pipeline.CharacterResolver  // NEW
    logger            *slog.Logger
    limiterFactory    *llmclient.ProviderLimiterFactory
}
```

`runServe()`에서 wiring 시 `characterSvc`를 그대로 넘기면 됨 (구조적으로 인터페이스 만족).

### 5. 통합 테스트 / regression

- `cmd/pipeline/serve_test.go` (있다면) 또는 신규 `serve_phase_b_wiring_test.go` — `buildPhaseBRunner`가 nil 아닌 PhaseBRunner를 만드는지, image track이 stub이 아닌지 확인.
- `internal/pipeline/image_track_test.go`는 이미 `fakeImageGen` 사용 — 변경 불필요.
- `go test ./...` 전체 통과 확인.

### 6. 운영 검증

서버 실제 실행:
```
./startup.sh dev
```
브라우저 → 새 run 생성 → Phase A 진행 → character_pick → 이미지 생성 단계에서:
- 로그에 `image track shot` 라인이 매 shot마다 떠야 함 (Provider: dashscope, cost_usd > 0)
- `{outputDir}/{runID}/images/scene_XX/shot_XX.png` 파일 실제 생성됨
- BatchReview 화면에서 이미지가 보임 (단, 이미지 정적 서빙 라우트도 필요할 수 있음 — 별도 세션에서 audio처럼 뚫음. 지금 세션 범위는 **생성**까지)

만약 이미지 정적 서빙 라우트가 없어 UI가 404를 받는다면, 같은 세션에서 [internal/api/handler_media.go](internal/api/handler_media.go) 파일에 `Image` 핸들러 추가 + `routes.go`에 `GET /api/runs/{id}/scenes/{idx}/shots/{shot}/image` 등록 + `handler_scene.go`의 응답 변환에서 `image_path` 도 URL로 재작성. 이건 audio와 같은 패턴이라 30분 내로 가능.

---

## 제약 / 절대 금지

- 이미지 생성에 **DashScope만** 사용. SiliconFlow / Replicate / 다른 제공자 금지 (`feedback_api_dashscope_only.md`).
- HTTP client는 절대 `http.DefaultClient` 사용 금지. 모든 클라이언트는 `*http.Client`를 생성자로 주입받음.
- Vision/Growth 미루기 금지 — 이번 세션에서 wiring까지 끝내야 함. stub 한 줄 남기고 끝내지 말 것 (`feedback_no_dead_layers.md`).
- 테스트 없이 production 코드 만들지 말 것. 외부 HTTP은 `testutil.BlockExternalHTTP(t)`로 막고 `httptest.Server`로 fake (`feedback_meta_principles.md`).
- 파이프라인 / 상태머신 변경 시 검증된 외부 사례 (TTS 클라이언트 패턴)를 그대로 모방 (`feedback_pipeline_rigor.md`).
- 커밋 범위 엄수 — 이미지 클라이언트 + wiring 외 파일 편집 금지 (`feedback_commit_scope.md`).

---

## 완료 정의 (Definition of Done)

- [ ] `internal/llmclient/dashscope/image.go` 가 `domain.ImageGenerator` 구현
- [ ] `internal/llmclient/dashscope/image_test.go` 가 BlockExternalHTTP + httptest로 9+ 케이스 커버
- [ ] `domain.PipelineConfig.ImageEditModel` 추가 + 기본값 + config 로딩/저장 모두 통과
- [ ] `cmd/pipeline/serve.go` 의 `imageTrackStub` 제거, 실제 image track wiring
- [ ] `dynamicPhaseBExecutor` 가 `CharacterResolver` 주입 받음
- [ ] `go test ./...` 전체 통과
- [ ] `./startup.sh dev` 로 실제 새 run 돌려 이미지 파일이 출력 디렉토리에 생성되는 것 육안 확인
- [ ] 커밋 메시지: `feat(image-track): wire DashScope qwen-image client into Phase B` 류 (정직 timeline — Story 5.4 마무리 표시)

---

## 시작 인사 (그대로 사용 가능)

> Phase B 이미지 트랙이 [cmd/pipeline/serve.go:112-114](cmd/pipeline/serve.go#L112-L114)에서 no-op stub으로 wiring돼 있어서 모든 run에서 이미지가 안 만들어지는 상태. 이번 세션에서 DashScope `qwen-image` / `qwen-image-edit` 클라이언트를 새로 만들어서 stub 제거하고 끝까지 wiring 한다. 위의 [next-session-prompt-image-client.md](_bmad-output/planning-artifacts/next-session-prompt-image-client.md) 의 사전 학습 자료부터 순서대로 읽고, 구현 범위 체크리스트 따라가자. 테스트 먼저, 외부 HTTP은 `testutil.BlockExternalHTTP(t)` + `httptest.Server`. 시작 전에 메모리 파일 (`MEMORY.md` 색인 + feedback 6개)부터 확인.
