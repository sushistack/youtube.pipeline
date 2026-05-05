---
title: 'SCP Cartoon Canonical Image Library'
type: 'feature'
created: '2026-05-05'
status: 'done'
baseline_commit: '0dd43e3c70ca609a1bc2952af2a74185214e050f'
context:
  - '{project-root}/internal/llmclient/comfyui/image.go'
  - '{project-root}/internal/pipeline/image_track.go'
  - '{project-root}/internal/service/character_service.go'
  - '{project-root}/web/src/components/production/CharacterPick.tsx'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Cast 단계에서 선택한 DDG 레퍼런스가 매 character-shot마다 ComfyUI image-edit으로 카툰화되기 때문에, 같은 SCP 캐릭터가 shot 사이에서 외형이 흔들린다. 같은 SCP를 다시 만들 때도 검색→픽→재생성을 반복해 비용·시간 낭비가 크다. 카툰 스타일 prompt prefix도 빠져있어 결과가 "kid-friendly Starcraft cartoon"이 아니라 사실적 SCP 그대로 나온다.

**Approach:** Cast 단계에서 레퍼런스 픽 직후 cartoon-style prompt prefix를 붙여 image-edit을 한 번 돌려 **canonical 카툰 이미지 1장**을 만들고, `scp_image_library` 테이블 + `{scp_image_dir}/{SCP_ID}/canonical.png` 파일에 저장한다. Phase B image_track은 character-shot에서 라이브러리 hit이 있으면 canonical 파일을 ref로 쓰고, 없으면 기존 DDG URL fallback. Cast 진입 시 라이브러리에 hit이 있으면 검색 폼을 건너뛰고 "재사용 / 재생성 / 다시 검색" 카드를 먼저 보여준다.

## Boundaries & Constraints

**Always:**
- canonical 파일과 DB row는 한 트랜잭션 의미로 일관 (파일 없는데 row만 있거나 그 반대 금지) — 파일은 atomic temp+rename, row는 그 이후 upsert
- prompt 합성 = `cartoon_style_prompt + "; " + frozen_descriptor`. cartoon_style_prompt는 `domain.PipelineConfig.CartoonStylePrompt`로 노출, config.yaml에서 override 가능
- 운영 토글 / 플래그는 모두 config.yaml로 (env 신규 항목 금지) — 메모리 `feedback_config_not_env.md` 준수
- canonical 생성은 별도 endpoint(`POST /api/runs/{id}/characters/canonical`). 단계머신은 건드리지 않으며 character_pick stage에 머문 채 호출됨
- 통합 테스트 우선: store는 실제 sqlite + tmpdir, image generator는 fake로 ImageEditRequest 입력값 검증
- Phase B의 character-shot reference 우선순위: (1) `scp_image_library` hit → 로컬 파일 base64 data URL, (2) miss → 기존 `FetchReferenceImageAsDataURL` 경로
- 정적 서빙 endpoint는 path traversal 방지 (`SCP_ID`를 `[A-Za-z0-9_-]+`로 제한, 최종 경로가 `scp_image_dir` prefix 안에 있는지 검증)

**Ask First:**
- (해당 사항 없음 — 모든 결정 확정)

**Never:**
- canonical 생성을 단계 status로 표현하지 않음 (`character_pick/generating` 같은 신규 status 추가 금지)
- canonical 생성 실패가 character_pick stage advance를 막지 않음 (운영자가 원하면 canonical 없이 기존 흐름으로 advance 가능 — 단, image_track은 그 경우 기존 DDG URL fallback)
- LoRA 추가 도입, 별도 LoRA 파일 셋업 금지 (이번 범위 외)
- 현재 unrelated diff (`web/src/index.css`, `ScenarioInspector.tsx`, `ProductionShell.tsx`) 건드리지 않음 — 메모리 `feedback_commit_scope.md` 준수
- DDG 검색 / `character_search_cache` / `runs.selected_character_id` 의미 변경 금지 (canonical은 별도 레이어)

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| canonical 신규 생성 | run에 selected_character_id + frozen_descriptor 있음, scp_image_library에 SCP_ID 없음 | image-edit 1회 호출, `scp_image_dir/{SCP_ID}/canonical.png` 파일 + DB row 생성 (version=1), 응답 200 | image-edit 실패 시 파일/row 모두 롤백 (atomic write로 partial 파일 남지 않음) |
| canonical 재생성 | 라이브러리 hit + body `{regenerate: true}` | 새 seed로 image-edit 재호출, 파일 덮어쓰기, row update (version+=1) | 동일 |
| canonical 멱등 조회 | 라이브러리 hit + body `{regenerate: false}` | 기존 row 그대로 응답 200, image-edit 호출 안 함 | N/A |
| GET canonical hit | 라이브러리 hit | 200 with record + image_url | N/A |
| GET canonical miss | 라이브러리 miss | 404 ErrNotFound | N/A |
| 정적 이미지 서빙 hit | 유효 SCP_ID + 파일 존재 | 200 image/png | N/A |
| 정적 이미지 서빙 traversal | SCP_ID에 `..` 또는 `/` 포함 | 400 VALIDATION_ERROR | N/A |
| Phase B character-shot, library hit | 라이브러리에 SCP_ID 있음 | image-edit ref = canonical 파일의 data URL | N/A |
| Phase B character-shot, library miss | 라이브러리 miss | image-edit ref = DDG URL → FetchReferenceImageAsDataURL (현재 동작 유지) | N/A |
| Cast 진입 시 hit | run의 SCP_ID 라이브러리에 있음 | UI: "재사용/재생성/다시검색" 카드 표시 | N/A |
| Cast 진입 시 miss | run의 SCP_ID 라이브러리에 없음 | UI: 기존 search → grid → descriptor 흐름 + descriptor 후 미리보기 단계 | N/A |
| canonical 생성 전 pick 시도 | 라이브러리 miss + canonical 미생성 | pick은 그대로 성공 (canonical은 optional) | image_track에서 fallback 경로 |

</frozen-after-approval>

## Code Map

- `migrations/019_scp_image_library.sql` -- 신규 테이블 + updated_at 트리거 (008/009 패턴)
- `internal/domain/types.go` -- `ScpImageRecord` 도메인 타입 신규
- `internal/domain/config.go` -- `CartoonStylePrompt`, `ScpImageDir`, `ScpCanonicalWidth`, `ScpCanonicalHeight` 필드 추가; `DefaultConfig()`에 기본값
- `internal/config/loader.go` -- 새 config 키 default 등록
- `internal/db/scp_image_library_store.go` -- 신규 store (Get / Upsert / Delete)
- `internal/db/scp_image_library_store_test.go` -- 라운드트립 + version increment + concurrent upsert
- `internal/service/scp_image_service.go` -- 신규 서비스 (`Generate`, `GetByRun`, `GetBySCPID`); `domain.ImageGenerator` + `ScpImageStore` 의존성
- `internal/service/scp_image_service_test.go` -- 통합 테스트 (실제 sqlite + fake ImageGenerator)
- `internal/api/handler_scp_image.go` -- `GET/POST /api/runs/{id}/characters/canonical` + `GET /api/scp_images/{scp_id}` 정적 서빙
- `internal/api/handler_scp_image_test.go` -- 200/404/400(traversal)/409(stage 부적합) 케이스
- `internal/api/routes.go` -- 신규 라우트 등록
- `internal/pipeline/image_track.go` -- `CanonicalImageResolver` 포트 추가, `runImageTrack`이 character-shot에서 우선 라이브러리 hit lookup; 신규 헬퍼 `LoadLocalImageAsDataURL`
- `internal/pipeline/image_track_test.go` -- canonical hit 시 Edit input.ReferenceImageURL이 라이브러리 파일에서 온 data URL인지 검증
- `cmd/pipeline/serve.go` -- store/service 와이어링, `ImageTrackConfig.CanonicalResolver` 주입
- `web/src/contracts/runContracts.ts` -- `scpCanonicalImageSchema` + `scpCanonicalImageResponseSchema`
- `web/src/lib/queryKeys.ts` -- `runs.canonical(run_id)` 추가
- `web/src/lib/apiClient.ts` -- `fetchScpCanonical`, `generateScpCanonical(run_id, regenerate)` 추가
- `web/src/components/production/CharacterPick.tsx` -- hit 분기 (재사용/재생성/다시검색 카드) + miss 흐름에 미리보기 단계 추가
- `web/src/components/production/CharacterPick.test.tsx` -- hit / miss / 재생성 클릭 흐름

## Tasks & Acceptance

**Execution:**
- [x] `migrations/019_scp_image_library.sql` -- 새 테이블 + 트리거 추가 -- 라이브러리 영속화의 진실의 원천 (스키마에 `source_candidate_id` 추가 — 재사용 시 /pick 재발행을 위해 필요)
- [x] `internal/domain/types.go` -- `ScpImageRecord{ScpID, FilePath, SourceRefURL, SourceQueryKey, SourceCandidateID, PromptUsed, Seed, Version, CreatedAt, UpdatedAt}` 추가 -- 도메인 단일 표현
- [x] `internal/domain/config.go` + `internal/config/loader.go` -- `CartoonStylePrompt`, `ScpImageDir`, `ScpCanonicalWidth=1280`, `ScpCanonicalHeight=720` (16:9)
- [x] `internal/db/scp_image_library_store.go` + 테스트 (5 tests)
- [x] `internal/service/scp_image_service.go` + 테스트 (8 tests). `GenerateCanonicalInput`로 candidate/descriptor 오버라이드 지원 — pre-pick 미리보기 흐름 가능
- [x] `internal/llmclient/comfyui/image.go` + `internal/domain/llm.go` -- `ImageEditRequest.Seed` (입력) + `ImageResponse.Seed` (실제 사용) 노출 -- canonical 멱등 재현 가능
- [x] `internal/pipeline/image_track.go` -- `CanonicalImageResolver` 포트 + `loadLocalImageAsDataURL` 헬퍼 + character-shot 분기 hit 우선
- [x] `internal/pipeline/image_track_test.go` -- canonical hit / miss 두 경로 검증 (2 새 tests)
- [x] `internal/api/handler_scp_image.go` + 테스트 (5 tests): 3개 엔드포인트 + traversal 방어
- [x] `internal/api/routes.go` -- 신규 라우트 등록 (3개: GET/POST canonical, GET static)
- [x] `cmd/pipeline/serve.go` + `cmd/pipeline/resume.go` -- ScpImage store/service + buildCanonicalImageGenerator + buildPhaseBRunner CanonicalResolver 주입
- [x] `web/src/contracts/runContracts.ts` + `web/src/lib/queryKeys.ts` + `web/src/lib/apiClient.ts` -- 타입/key/fetch 함수
- [x] `web/src/components/production/CharacterPick.tsx` + 테스트 (3 새 tests, total 13/13 pass) -- reuse/preview phases
- [ ] manual smoke -- ComfyUI 띄운 상태에서 end-to-end (사용자 직접 검증 필요)

**Acceptance Criteria:**
- Given 라이브러리에 SCP_ID 없음, when `POST /characters/canonical` 호출, then 파일이 atomic write로 생성되고 DB row가 version=1로 업서트, 응답 200에 `file_path`/`image_url`/`seed`/`prompt_used` 포함
- Given 라이브러리 hit, when `regenerate:true`로 재호출, then DB version이 +1되고 파일이 새 seed 결과로 덮어써지며, 실패 시 기존 파일/row가 그대로 유지된다
- Given Phase B character-shot 진입, when 라이브러리 hit, then `ImageEditRequest.ReferenceImageURL`이 라이브러리 파일의 base64 data URL이며 DDG fetcher는 호출되지 않는다
- Given Phase B character-shot 진입, when 라이브러리 miss, then 기존 동작(DDG URL → FetchReferenceImageAsDataURL) 그대로 작동한다
- Given Cast 진입 + 라이브러리 hit, when CharacterPick 렌더, then "재사용/재생성/다시검색" 카드만 표시되고 검색 폼은 표시되지 않는다 ("이대로 사용" 클릭 시 descriptor 단계로 직행)
- Given `GET /api/scp_images/../../etc/passwd`, then 400 VALIDATION_ERROR로 응답하고 디스크 접근은 일어나지 않는다
- Given canonical 생성 없이 운영자가 character pick을 진행, when Phase B character-shot 실행, then 라이브러리 miss 경로로 fallback되어 stage가 실패하지 않는다

## Spec Change Log

### 2026-05-05 — step-04 review patches (no loopback)

Three patch-level findings from step-04 review applied directly to the diff (no spec amendment, no code re-derivation):

1. **`IsValidSCPID` accepted bare `.` and other all-symbol strings.** Originally any non-`..` char set was allowed; with `scpID="."` `filepath.Join` produced `{root}/canonical.png`, escaping the per-SCP folder convention while still passing the containment check. Fix: require ≥1 alphanumeric char in `IsValidSCPID`. Regression cases `.`, `-`, `_`, `-_-` added to `TestScpImageService_IsValidSCPID`.
2. **File/row atomicity violated when Edit succeeds but Upsert fails.** Always rule says "파일/row 모두 롤백". Service was leaving the file on disk if the DB upsert errored. Fix: `os.Remove(absPath)` on Upsert error path inside `Generate`.
3. **Reuse-flow descriptor reconstruction was fragile.** FE's `extractFrozenFromPrompt` split on the first `"; "` to recover the operator's frozen descriptor, but the cartoon-style prompt is operator-tunable in `config.yaml` — any internal `"; "` in the style would corrupt the parse. Fix: persisted the raw `frozen_descriptor` as a discrete column on `scp_image_library` (migration 019), surfaced it through the API, and FE now reads the field directly; helper deleted.

**KEEP** (must survive re-derivation if the spec ever loops back):
- `domain.ImageEditRequest.Seed` + `ImageResponse.Seed` round-trip — lets ComfyUI seeds be persisted honestly without forcing dashscope to fake them
- `GenerateCanonicalInput` with optional `CandidateID` / `FrozenDescriptor` overrides — makes the pre-pick "preview before commit" miss flow possible without forcing stage advance
- `CanonicalImageResolver` as a dedicated port (not an extension of `CharacterResolver`) — keeps the priority order in image_track readable
- canonical-hit branch in image_track must not call `RefImageFetcher` — test asserts this with a fetcher that errors on call

## Design Notes

- `CanonicalImageResolver` 포트 분리: `CharacterResolver`를 확장하면 단계머신 의미가 흐려진다. 새 포트로 두면 `image_track.go`에서 `if canonical, err := cfg.CanonicalResolver.GetCanonical(ctx, run.SCPID); err == nil { ref = canonical } else if errors.Is(err, ErrNotFound) { fallback }` 구조가 명확.
- Atomic file write 패턴은 이미 `comfyui/image.go::writeFileAtomic`에 있음 — service는 동일 패턴 호출 (직접 reuse 또는 동일 패턴 자체 구현)
- `ScpImageRecord.FilePath`는 `scp_image_dir` 기준 상대경로(`SCP-049/canonical.png`)로 저장. 정적 서빙 핸들러가 `filepath.Join(scpImageDir, scpID, "canonical.png")` 후 `filepath.Clean` + prefix containment 검증
- canonical 생성 prompt 예시:
  ```
  Kid-friendly cartoon illustration, Starcraft-inspired stylized art, clean vector lines, vibrant colors,;
  Appearance: A tall, slender humanoid silhouette draped in heavy black robes...
  ```
- `regenerate:false` 멱등성: hit이면 image-edit 호출 자체를 skip → 비용 0. UI 재진입 / 페이지 새로고침에서 같은 결과 보장
- `image_url` 필드는 정적 서빙 endpoint URL (`/api/scp_images/SCP-049`)로 채움 — FE는 이걸 그대로 `<img src>`에 넣음

## Verification

**Commands:**
- `go build ./...` -- expected: 컴파일 성공
- `go test ./internal/db/... ./internal/service/... ./internal/api/... ./internal/pipeline/...` -- expected: 신규 테스트 + 기존 테스트 모두 PASS
- `cd web && pnpm test -- CharacterPick` -- expected: hit/miss/regenerate 시나리오 PASS
- `cd web && pnpm typecheck && pnpm lint` -- expected: 0 errors
- `make migrate-up` (또는 동등 커맨드) -- expected: 019 적용 후 schema 무결성 OK

**Manual checks:**
- ComfyUI 로컬(`http://127.0.0.1:8188`) 띄운 상태에서 SCP-049로 신규 run → Cast 진입 → 검색 → 픽 → descriptor 입력 → "Generate cartoon" → 약 60–70초 후 미리보기 카툰 이미지 표시 → "Confirm & continue" → image stage advance → Generate Assets → character-shot들이 라이브러리 canonical을 ref로 쓰는지 audit.log에서 확인
- 두 번째 SCP-049 run 생성 → Cast 진입 시 검색 폼 대신 "재사용/재생성/다시검색" 카드 표시 확인
- `scp_images/SCP-049/canonical.png`가 디스크에 존재하고 `scp_image_library` 테이블에 row 1개 존재 확인

## Suggested Review Order

**Schema and domain model**

- 신규 테이블 — version 증가, source_candidate_id, frozen_descriptor 컬럼이 Always 룰의 핵심
  [`019_scp_image_library.sql:1`](../../migrations/019_scp_image_library.sql#L1)

- ScpImageRecord — 라이브러리의 도메인 표현; FrozenDescriptor 분리는 step-04 patch
  [`types.go:172`](../../internal/domain/types.go#L172)

- ImageEditRequest/Response.Seed — ComfyUI seed가 결정적으로 propagate되는 단일 경로
  [`llm.go:18`](../../internal/domain/llm.go#L18)

**Cast-stage canonical lifecycle (entry point)**

- ScpImageService.Generate — 모든 비즈니스 invariant가 만나는 곳, hit/regenerate/atomic rollback이 한 함수에
  [`scp_image_service.go:Generate`](../../internal/service/scp_image_service.go#L132)

- IsValidSCPID — path traversal과 file collision 막는 단일 게이트
  [`scp_image_service.go:IsValidSCPID`](../../internal/service/scp_image_service.go#L260)

- HTTP handler — 3개 엔드포인트의 traversal 방어 + 정적 서빙 containment 검증
  [`handler_scp_image.go:1`](../../internal/api/handler_scp_image.go#L1)

**Phase B integration**

- runImageTrack의 reference 우선순위 분기 — canonical hit이 DDG fetcher를 우회하는 핵심 invariant
  [`image_track.go:222`](../../internal/pipeline/image_track.go#L222)

- loadLocalImageAsDataURL — 캐노니컬 파일을 ComfyUI Edit 입력 형식으로 변환
  [`image_track.go:418`](../../internal/pipeline/image_track.go#L418)

**Wiring**

- buildCanonicalImageGenerator — provider 분기로 별도 image client 인스턴스 구축
  [`serve.go:584`](../../cmd/pipeline/serve.go#L584)

- ScpImageService 주입 + image_track에 CanonicalResolver 전달
  [`serve.go:728`](../../cmd/pipeline/serve.go#L728)

- resume.go에서도 동일 와이어링 (Phase B retry 시 canonical 우선 유지)
  [`resume.go:75`](../../cmd/pipeline/resume.go#L75)

**Frontend HITL flow**

- CharacterPick 진입 시 canonical 조회 → reuse / search 분기, 그리고 derived phase 패턴
  [`CharacterPick.tsx:139`](../../web/src/components/production/CharacterPick.tsx#L139)

- 미스 흐름의 descriptor → preview → pick 순서 (canonical override가 pre-pick 미리보기를 가능하게 함)
  [`CharacterPick.tsx:264`](../../web/src/components/production/CharacterPick.tsx#L264)

- Reuse 카드 3-button 액션
  [`CharacterPick.tsx:323`](../../web/src/components/production/CharacterPick.tsx#L323)

- API 클라이언트 — generateScpCanonical override 시그니처
  [`apiClient.ts:421`](../../web/src/lib/apiClient.ts#L421)

**Tests (peripherals)**

- store 라운드트립 + version 증가 + validation
  [`scp_image_library_store_test.go:1`](../../internal/db/scp_image_library_store_test.go#L1)

- 통합 테스트 — Generate hit/miss/regenerate/rollback/refFetcher
  [`scp_image_service_test.go:1`](../../internal/service/scp_image_service_test.go#L1)

- canonical hit/miss 시 image_track의 ref 선택 invariant
  [`image_track_test.go:1117`](../../internal/pipeline/image_track_test.go#L1117)

- API handler 200/404/400 + traversal 케이스
  [`handler_scp_image_test.go:1`](../../internal/api/handler_scp_image_test.go#L1)

- React 테스트 13개 (10 기존 + 3 신규: reuse 카드, descriptor→preview→pick)
  [`CharacterPick.test.tsx:1`](../../web/src/components/production/CharacterPick.test.tsx#L1)
