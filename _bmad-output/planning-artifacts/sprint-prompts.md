# Sprint Prompts — youtube.pipeline
# 각 스토리별 개발 → 코드리뷰 → 리뷰반영 3단계 프롬프트

---

## Epic 1: Project Foundation & Architecture Skeleton

---

### Epic 1 - Story 1.1 개발
`/bmad-create-story`
Go 모듈 + React SPA 전체 프로젝트 구조를 초기화하고 Makefile 빌드 체인을 구성한다.
- `cmd/pipeline/`, `internal/{service,domain,llmclient/{dashscope,deepseek,gemini},db,pipeline,hitl,web,clock,testutil}/`, `migrations/`, `testdata/{contracts,golden}/`, `web/`, `e2e/` 디렉토리 구조 정확히 일치
- Vite **7.3** (8.x 절대 불가), React 19.x, Tailwind 4.x, shadcn/ui CLI v4, Vitest ^4.1.4
- `make build` → web-build 먼저, go-build 후순; `make test`, `make dev` 모두 동작
- `.air.toml` Go 핫리로드, `e2e/playwright.config.ts` Chromium-only 플레이스홀더
- 이후 모든 스토리의 기반이 되므로 디렉토리 구조와 패키지명 오타 없이 정확하게

### Epic 1 - Story 1.1 코드 리뷰
`/bmad-code-review`
Story 1.1 구현 결과를 검토한다.
- `go build ./cmd/pipeline` 성공 여부, `cd web && npx vitest run` 0 tests 패스 여부
- Vite 버전이 7.3인지 (8.x 설치됐으면 즉시 지적)
- 디렉토리 구조가 Architecture 문서와 완전히 일치하는지
- Makefile target 순서(web-build → go-build)가 올바른지
- `.gitignore`에 `web/dist/`, `bin/`, `.env` 포함됐는지

### Epic 1 - Story 1.1 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- Vite 버전 불일치 시 `package.json` 다운그레이드 후 `npm install` 재실행
- 누락된 디렉토리 생성 (`.gitkeep` 포함)
- Makefile target 의존성 순서 수정
- `.gitignore` 누락 항목 추가

---

### Epic 1 - Story 1.2 개발
`/bmad-create-story`
SQLite 데이터베이스 스키마 자동 초기화 및 마이그레이션 러너를 구현한다.
- `internal/db/sqlite.go`: WAL 모드(PRAGMA journal_mode=wal), busy_timeout=5000, DB 파일 0600 권한(POSIX)
- `internal/db/migrate.go` ~50 LoC: `embed.FS` + `PRAGMA user_version` 기반 순수 Go 마이그레이션 러너(외부 툴 사용 금지)
- `migrations/001_init.sql`: `runs`, `decisions`, `segments` 3개 테이블, `segments`에 `UNIQUE(run_id, scene_index)` 제약
- 마이그레이션 멱등성 보장(이미 적용된 경우 에러 없이 스킵)
- Architecture DDL 컬럼 타입·기본값과 정확히 일치해야 함

### Epic 1 - Story 1.2 코드 리뷰
`/bmad-code-review`
Story 1.2 구현 결과를 검토한다.
- WAL 모드 설정 코드가 DB 오픈 직후 실행되는지 (커넥션 옵션 아닌 PRAGMA)
- migrate.go가 50 LoC 이하인지, 외부 마이그레이션 라이브러리 임포트 없는지
- `UNIQUE(run_id, scene_index)` 제약이 DDL에 실제로 존재하는지
- `sqlite_test.go`에 WAL 모드 확인, 권한 확인, 멱등성 테스트 모두 있는지
- `ncruces/go-sqlite3`(순수 Go, CGO 없음) 드라이버 사용 여부

### Epic 1 - Story 1.2 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- PRAGMA 설정 순서 보정 (WAL → busy_timeout → 마이그레이션)
- 누락된 테이블 컬럼 또는 제약 조건 추가
- migrate.go LoC 초과 시 리팩토링
- 누락 테스트 케이스 추가

---

### Epic 1 - Story 1.3 개발
`/bmad-create-story`
핵심 도메인 타입, 에러 분류 시스템, 프로바이더 인터페이스, Clock 추상화를 정의한다.
- `internal/domain/types.go`: 15개 Stage 상수(pending→complete), Run/Episode/NormalizedResponse 구조체, snake_case JSON 태그
- `internal/domain/errors.go`: 7개 센티넬 에러(ErrRateLimited, ErrUpstreamTimeout, ErrStageFailed, ErrValidation, ErrConflict, ErrCostCapExceeded, ErrNotFound), 각각 HTTP 상태코드 + retryable 플래그
- `internal/domain/`: TextGenerator, ImageGenerator(Generate+Edit), TTSSynthesizer 인터페이스; 모든 구현체는 `*http.Client` 생성자 주입(`http.DefaultClient` 사용 금지)
- `internal/clock/`: Clock 인터페이스, RealClock, FakeClock(결정적 시간 진행 지원)
- 인터페이스는 소비하는 패키지에서 정의(예외: domain/ 캐퍼빌리티 인터페이스)

### Epic 1 - Story 1.3 코드 리뷰
`/bmad-code-review`
Story 1.3 구현 결과를 검토한다.
- Stage 상수가 정확히 15개인지 (pending, research, structure, write, visual_break, review, critic, scenario_review, character_pick, image, tts, batch_review, assemble, metadata_ack, complete)
- ImageGenerator가 Generate와 Edit 메서드 둘 다 가지는지
- FakeClock이 결정적 시간 진행을 지원하는 테스트 API 제공하는지
- `http.DefaultClient` 직접 사용 코드가 없는지
- domain/ 패키지가 internal/ 내 다른 패키지를 임포트하지 않는지 (임포트 방향 규칙)

### Epic 1 - Story 1.3 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 누락 Stage 상수 추가
- ImageGenerator Edit 메서드 시그니처 수정
- FakeClock 시간 진행 API 추가
- 잘못된 임포트 방향 제거

---

### Epic 1 - Story 1.4 개발
`/bmad-create-story`
테스트 유틸리티와 외부 API 격리 레이어를 구축한다.
- `internal/testutil/assert.go`: `assertEqual[T any]` 제네릭 헬퍼, 불일치 시 `t.Helper()` + diff 출력
- `internal/testutil/fixture.go`: `LoadFixture(t, "contracts/pipeline_state.json")` → `testdata/` 기준 `[]byte` 반환
- `internal/testutil/nohttp.go`: 비-localhost URL HTTP 호출 즉시 차단("external HTTP call blocked in test: {url}")
- `web/src/test/setup.ts`: Vitest에서 비-localhost fetch 거부
- `CI=true`일 때 API 키 환경변수 존재 시 init panic("API keys must not be set in CI environment")
- `testdata/contracts/pipeline_state.json` 초기 픽스처, `testdata/golden/` 디렉토리 준비
- seed-able SQLite 스냅샷 픽스처 로더 (`LoadRunStateFixture`)

### Epic 1 - Story 1.4 코드 리뷰
`/bmad-code-review`
Story 1.4 구현 결과를 검토한다.
- nohttp 차단 메시지 정확성("external HTTP call blocked in test: {url}")
- CI lockout이 init()에서 실행되는지, TestMain이 아닌지
- assertEqual이 `t.Helper()`를 올바르게 호출하는지
- seed-able 픽스처 파일이 실제 `testdata/`에 체크인됐는지 (런타임 생성 아님)
- 3-layer 방어(생성자 주입, 차단 Transport, CI 환경 잠금) 모두 구현됐는지

### Epic 1 - Story 1.4 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- nohttp 메시지 형식 수정
- CI lockout 위치 이동 (init → 올바른 위치)
- `t.Helper()` 호출 위치 보정
- 누락된 픽스처 파일 생성 및 커밋

---

### Epic 1 - Story 1.5 개발
`/bmad-create-story`
`pipeline init`과 `pipeline doctor` CLI 명령을 구현한다.
- `pipeline init`: `./config.yaml`(모델 ID, 경로, 비용 캡, project-root 레이아웃), `.env` 템플릿(3개 API 키), SQLite 스키마 초기화, 출력 디렉토리 생성
- Viper 설정 계층: `.env`(시크릿) → `config.yaml`(비시크릿) → CLI 플래그
- `pipeline doctor`: 4개 검사 — API 키 존재, FS 경로 쓰기 가능, FFmpeg 바이너리(`ffmpeg -version`), Writer ≠ Critic 프로바이더(FR46)
- 각 검사는 `Check` 인터페이스 구현 + 레지스트리 등록으로 확장 가능
- Writer = Critic 설정 시: "Writer and Critic must use different LLM providers" 에러
- Cobra v2.5.1, Viper v1.11.0 사용

### Epic 1 - Story 1.5 코드 리뷰
`/bmad-code-review`
Story 1.5 구현 결과를 검토한다.
- Cobra/Viper 버전이 go.mod에서 pinned 됐는지 (v2.5.1, v1.11.0)
- doctor 검사 4개 모두 구현됐는지 (특히 Writer≠Critic)
- Check 인터페이스가 실제로 레지스트리 패턴으로 등록되는지
- Viper 설정 계층 우선순위가 올바른지 (env > yaml > default)
- init 실행 시 기존 파일 덮어쓰기 방지 로직 있는지

### Epic 1 - Story 1.5 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- go.mod 버전 고정
- 누락 doctor 검사 추가
- Check 인터페이스 레지스트리 패턴으로 리팩토링
- Viper 우선순위 수정

---

### Epic 1 - Story 1.6 개발
`/bmad-create-story`
CLI 렌더러와 출력 포맷팅(human-readable + JSON)을 구현한다.
- `Renderer` 인터페이스: `RenderSuccess(data)`, `RenderError(err)` 메서드
- Human renderer: 색상 코드 계층형 출력(green=success, amber=warning, red=error)
- JSON renderer: `{"version": 1, "data": {...}}` / `{"version": 1, "error": {"code": "...", "message": "...", "recoverable": true/false}}`
- 에러 코드는 Story 1.3의 7개 분류 매핑
- `--json` 플래그로 어느 CLI 명령에서도 전환 가능
- `pipeline doctor`와 `pipeline doctor --json` 모두 동작

### Epic 1 - Story 1.6 코드 리뷰
`/bmad-code-review`
Story 1.6 구현 결과를 검토한다.
- JSON envelope의 `version` 필드가 정수 1인지 (문자열 아님)
- 7개 에러 카테고리 모두 JSON error code로 매핑됐는지
- `--json` 플래그가 Cobra 글로벌 플래그로 등록됐는지 (각 커맨드마다 중복 아님)
- Human renderer ANSI 색상 코드가 실제 터미널에서 동작하는지 (Windows 고려)
- `recoverable` 필드가 Story 1.3 retryable 플래그와 일치하는지

### Epic 1 - Story 1.6 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- `version` 타입 수정 (int)
- 누락 에러 코드 매핑 추가
- `--json` 플래그 위치 이동 (글로벌)
- `recoverable`/`retryable` 일관성 수정

---

### Epic 1 - Story 1.7 개발
`/bmad-create-story`
GitHub Actions CI 파이프라인과 FR52-go E2E 스모크 테스트를 구축한다.
- `.github/workflows/ci.yml`: 4 jobs — `test-go`와 `test-web` 병렬, 이후 `test-e2e`와 `build` 순차
- Go 모듈 캐시, npm 캐시, Playwright Chromium 바이너리 캐시
- CI 환경에 API 키 미주입 (Layer 3 방어)
- `go-cleanarch` 또는 `depguard`로 레이어 임포트 린터 실행 (NFR-M4)
- `testdata/fr-coverage.json` 검증기: `grace: true` 모드(미매핑 FR 경고만)
- FR52-go: `go test -run E2E` — Phase A→B→C 전체 파이프라인을 mocked 프로바이더로 실행, output artifacts 존재 확인
- 전체 CI 벽시계 시간 ≤ 10분 (NFR-T6)

### Epic 1 - Story 1.7 코드 리뷰
`/bmad-code-review`
Story 1.7 구현 결과를 검토한다.
- test-go와 test-web이 실제로 병렬 실행되는지 (`needs` 없이 동일 수준)
- API 키가 CI secrets에도 등록 안 됐는지 확인
- layer-import 린터가 올바른 방향(api→service→domain/db/llmclient/pipeline/clock)을 검사하는지
- E2E 테스트가 실제 외부 API를 호출하지 않는지 (nohttp transport 사용)
- fr-coverage.json grace mode가 strict mode 활성화 포인트(Epic 6 이후)와 함께 주석에 명시됐는지

### Epic 1 - Story 1.7 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- CI jobs 병렬화 설정 수정
- E2E 테스트 외부 API 호출 격리
- layer-import 린터 방향 규칙 수정
- fr-coverage.json strict mode 전환 TODO 주석 추가

---

## Epic 2: Pipeline Lifecycle & State Machine

---

### Epic 2 - Story 2.1 개발
`/bmad-create-story`
파이프라인 상태머신을 순수 함수로 구현한다.
- `internal/pipeline/engine.go`: `NextStage(current Stage, event Event) → (Stage, error)` 순수 함수(DB 없음, 사이드 이펙트 없음)
- 15개 스테이지 전체 전이 매트릭스: 유효 전이 → 올바른 다음 스테이지, 무효 전이 → 에러
- HITL 대기 포인트(scenario_review, character_pick, batch_review, metadata_ack): status="waiting"
- 자동화 스테이지: status="running"
- `internal/pipeline/runner.go`에 Runner 인터페이스 정의 (engine ↔ run_service 순환 의존성 방지)

### Epic 2 - Story 2.1 코드 리뷰
`/bmad-code-review`
Story 2.1 구현 결과를 검토한다.
- NextStage가 진짜 순수 함수인지 (DB 접근, 파일 I/O, 글로벌 상태 없음)
- 15개 스테이지 × 모든 이벤트 조합 테스트가 exhaustive한지
- Runner 인터페이스가 `pipeline/runner.go`에 정의됐는지 (service 패키지 아님)
- HITL vs 자동화 스테이지 구분 로직이 상수 정의와 분리됐는지
- Go switch 문이 ~100 LoC 이내인지

### Epic 2 - Story 2.1 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 순수 함수 위반 코드 제거
- 누락 스테이지 전이 케이스 추가
- Runner 인터페이스 위치 이동
- 스테이지 분류 로직 분리

---

### Epic 2 - Story 2.2 개발
`/bmad-create-story`
파이프라인 런 생성, 취소, 조회 기능을 구현한다.
- `pipeline create scp-049`: runs 테이블 신규 행 생성, run_id 형식 `scp-{scp_id}-run-{순번}`, 출력 디렉토리 생성
- 순번은 같은 scp_id 기준 순차 증가 (전역 증가 아님)
- `pipeline cancel {run-id}`: 실행 중 런 status=cancelled, 진행 중 스테이지 failed
- `pipeline status`: 모든 런 목록 + 현재 스테이지/상태/타임스탬프
- `pipeline status {run-id}`: 스테이지별 상세 진행 현황
- REST API 뼈대(Epic 2의 home): `internal/api/routes.go`, 미들웨어 체인(`WithRequestID`, `WithRecover`, `WithCORS`, `WithRequestLog` via `Chain()`), 응답 envelope, 파이프라인 라이프사이클 6개 엔드포인트
- `pipeline serve` 명령 (localhost-only, `--dev` 플래그로 Vite 프록시)

### Epic 2 - Story 2.2 코드 리뷰
`/bmad-code-review`
Story 2.2 구현 결과를 검토한다.
- run_id 형식이 `scp-{scp_id}-run-{N}` 정확히 일치하는지
- 순번이 같은 scp_id 범위에서만 증가하는지 (전역 auto-increment 아님)
- 미들웨어 4개가 `Chain()` 함수로 조합되는지
- `pipeline serve`가 127.0.0.1에만 바인딩하는지 (NFR-S2)
- 6개 파이프라인 라이프사이클 엔드포인트 모두 구현됐는지 (`POST /api/runs`, `GET /api/runs`, `GET /api/runs/{id}`, `POST /api/runs/{id}/resume`, `POST /api/runs/{id}/cancel`, `GET /api/runs/{id}/status`)

### Epic 2 - Story 2.2 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- run_id 순번 로직 수정 (scp_id 범위 한정)
- 미들웨어 Chain() 함수 도입
- 서버 바인딩 127.0.0.1로 수정
- 누락 엔드포인트 추가

---

### Epic 2 - Story 2.3 개발
`/bmad-create-story`
스테이지 레벨 재시작과 아티팩트 생명주기를 구현한다.
- `pipeline resume {run-id}`: 마지막 성공 스테이지 이후부터 재시작, 완료된 스테이지 재실행 없음
- 재시작 진입 시 파일시스템 ↔ DB 상태 일관성 검증 (불일치 시 경고 + 확인 프롬프트)
- Phase B 재시작: 해당 런의 모든 segments 행 DELETE 후 재삽입 (clean slate)
- 실패 스테이지 부분 아티팩트 파일 디스크 정리
- 동일 입력으로 재시작 시 동일 스키마·스테이지 상태 진행 보장 (NFR-R1)

### Epic 2 - Story 2.3 코드 리뷰
`/bmad-code-review`
Story 2.3 구현 결과를 검토한다.
- Phase B 재시작 시 segments DELETE가 `WHERE run_id = ?`로 범위 한정됐는지 (다른 런 영향 없음)
- FS↔DB 일관성 검증이 resume 진입 전에 실행되는지 (스테이지 시작 후 아님)
- 아티팩트 파일 정리가 실패 스테이지에 국한되는지
- 재시작 멱등성 테스트가 integration으로 작성됐는지

### Epic 2 - Story 2.3 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- DELETE 범위 수정
- 일관성 검증 실행 순서 보정
- 아티팩트 정리 범위 수정
- 멱등성 테스트 추가

---

### Epic 2 - Story 2.4 개발
`/bmad-create-story`
스테이지별 옵저버빌리티 데이터와 비용 추적을 구현한다.
- 스테이지 완료(성공/실패) 시 runs 테이블 8개 컬럼 저장: duration_ms, token_in, token_out, retry_count, retry_reason(nullable), critic_score(nullable), cost_usd, human_override
- 스테이지 비용 캡 초과 시 즉시 hard-stop, `ErrCostCapExceeded`(retryable=false)
- 런 전체 비용 캡 초과 시 런 hard-stop (NFR-C2)
- HTTP 429 응답: 스테이지 상태 미진행, `retry_reason="rate_limit"` 기록 (NFR-P3)
- SQLite CLI로 진단 쿼리 가능 (NFR-O3), 롤링 윈도우 쿼리용 인덱스 (NFR-O4)

### Epic 2 - Story 2.4 코드 리뷰
`/bmad-code-review`
Story 2.4 구현 결과를 검토한다.
- 8개 컬럼 중 nullable 처리가 올바른지 (retry_reason, critic_score만 nullable)
- 비용 캡 체크가 스테이지 완료 후가 아닌 누적 중 실행되는지 (hard-stop 즉시성)
- 429 수신 시 스테이지 상태가 정말 진행 안 되는지 (stage status 불변 확인)
- FakeClock을 이용해 백오프 타이밍 테스트가 결정적으로 작성됐는지
- `migration/001_init.sql`에 NFR-O4 인덱스가 정의됐는지

### Epic 2 - Story 2.4 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- nullable 컬럼 타입 수정
- 비용 캡 체크 위치 이동 (누적 중 실시간 체크)
- 429 핸들러 스테이지 상태 진행 방지 수정
- NFR-O4 인덱스 마이그레이션에 추가

---

### Epic 2 - Story 2.5 개발
`/bmad-create-story`
안티-프로그레스 감지기(코사인 유사도 기반 재시도 루프 조기 종료)를 구현한다.
- 연속 재시도 출력 간 코사인 유사도 > 0.92(기본값)이면 루프 조기 종료 (FR8)
- `retry_reason="anti_progress"` 기록, 오퍼레이터 에스컬레이션("Retries producing similar output — human review required")
- 임계값이 설정으로 변경 가능
- 롤링 50-런 윈도우 오탐률 측정 캡처 (NFR-R2, V1.5 게이트 기준 ≤5%)
- 모든 테스트는 결정적 입력, LLM 호출 없음

### Epic 2 - Story 2.5 코드 리뷰
`/bmad-code-review`
Story 2.5 구현 결과를 검토한다.
- 코사인 유사도 계산 로직이 정확한지 (텍스트 임베딩 방식 확인)
- 임계값 0.92가 설정 파일에서 읽히는지 (하드코딩 아님)
- 50-런 윈도우 FP 측정이 실제로 DB에 저장되는지
- 테스트가 LLM을 전혀 호출하지 않는지 (deterministic 확인)

### Epic 2 - Story 2.5 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 코사인 유사도 계산 오류 수정
- 임계값 하드코딩 → 설정 기반으로 이동
- FP 측정값 DB 저장 추가
- LLM 호출 격리

---

### Epic 2 - Story 2.6 개발
`/bmad-create-story`
HITL 세션 일시정지/재개와 변경 diff 요약을 구현한다.
- 일시정지 상태를 DB에 내구적으로 저장 (run_id, stage, scene_index, last_interaction_timestamp)
- `GET /api/runs/{id}/status` 응답에 일시정지 위치 포함 (FR49)
- 재개 응답: run_id, current_stage, scene_index, total_scenes, decisions_summary(승인/거절/대기 수)
- 상태 요약 문자열: "Run scp-049-run-1: reviewing scene 5 of 10, 4 approved, 0 rejected"
- "무엇이 바뀌었나" diff 생성: T1 이후 변경된 항목, before/after 상태, 타임스탬프 (FR50)
- 변경 없으면 diff 없이 상태 요약만 표시

### Epic 2 - Story 2.6 코드 리뷰
`/bmad-code-review`
Story 2.6 구현 결과를 검토한다.
- 일시정지 상태가 프로세스 재시작 후에도 복구되는지 (NFR-R3)
- diff 생성이 `last_interaction_timestamp` 기준으로 올바르게 필터링하는지
- 변경 없는 경우 빈 diff(null/empty array)를 올바르게 처리하는지
- 상태 요약 문자열 형식이 스펙과 정확히 일치하는지

### Epic 2 - Story 2.6 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 일시정지 상태 영속성 보강
- diff 타임스탬프 필터 수정
- null diff 처리 수정
- 요약 문자열 형식 수정

---

### Epic 2 - Story 2.7 개발
`/bmad-create-story`
롤링 윈도우 파이프라인 메트릭 CLI 리포트를 구현한다.
- `pipeline metrics --window 25`: 5개 메트릭 계산 (자동화율, Critic 교정(Cohen's kappa), Critic 회귀 탐지율, 결함 탈출률, 재시작 멱등성)
- 각 메트릭: 현재 값, Day-90 목표, 통과/실패
- n < 25이면 "provisional" 라벨
- `--json` 플래그: `{"version": 1, "data": {"window": 25, "metrics": [...], "provisional": true/false}}`
- NFR-O4 인덱스 활용 (EXPLAIN QUERY PLAN으로 검증), 1000개 런에서 1초 이내

### Epic 2 - Story 2.7 코드 리뷰
`/bmad-code-review`
Story 2.7 구현 결과를 검토한다.
- 5개 메트릭 모두 구현됐는지 (자동화율, 교정, 탐지율, 탈출률, 멱등성)
- Cohen's kappa 계산 공식 정확성
- provisional 라벨 조건(n < window)이 올바른지
- SQL 쿼리가 인덱스를 사용하는지 (EXPLAIN QUERY PLAN)
- JSON 응답 구조가 스펙과 일치하는지

### Epic 2 - Story 2.7 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 누락 메트릭 구현
- kappa 계산 공식 수정
- provisional 조건 수정
- 인덱스 미사용 쿼리 최적화

---

## Epic 3: Scenario Generation & Basic Quality Gate (Phase A)

---

### Epic 3 - Story 3.1 개발
`/bmad-create-story`
Phase A 에이전트 체인 오케스트레이터와 파이프라인 러너를 구현한다.
- `internal/pipeline/runner.go`: Phase A 체인 순차 실행 — Researcher→Structurer→Writer→VisualBreakdowner→Reviewer→Critic
- `PipelineState`를 메모리 내 전달 (인터-에이전트 데이터)
- 에이전트 순수성 규칙: 에이전트는 순수 함수 (DB 없음, 사이드 이펙트 없음, HTTP 없음)
- 체인 완료 시 `scenario.json`을 런 출력 디렉토리에 저장
- `AgentFunc` 타입 시그니처 정의

### Epic 3 - Story 3.1 코드 리뷰
`/bmad-code-review`
Story 3.1 구현 결과를 검토한다.
- AgentFunc 시그니처가 PipelineState를 받고 반환하는지
- 에이전트가 DB/HTTP/파일IO를 직접 호출하지 않는지
- runner가 에이전트 실행 순서를 강제하는지
- scenario.json 저장이 runner 책임인지 (에이전트 아님)

### Epic 3 - Story 3.1 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- AgentFunc 시그니처 수정
- 에이전트 순수성 위반 코드 이동
- 실행 순서 강제 로직 보강

---

### Epic 3 - Story 3.2 개발
`/bmad-create-story`
Researcher와 Structurer 에이전트를 구현한다.
- Researcher: SCP ID를 받아 로컬 데이터 코퍼스에서 리서치 요약 생성 (FR9)
- Structurer: 리서치 요약 → 4막 내러티브 구조 생성 (FR10)
- 각 에이전트 출력은 JSON 스키마로 검증 (FR11)
- 로컬 코퍼스 접근은 파일 기반 (외부 API 없음)
- `testdata/contracts/`에 각 에이전트 출력 스키마 픽스처 추가

### Epic 3 - Story 3.2 코드 리뷰
`/bmad-code-review`
Story 3.2 구현 결과를 검토한다.
- Researcher가 실제로 로컬 코퍼스만 사용하는지 (외부 API 호출 없음)
- 4막 구조가 스키마로 강제되는지
- JSON 스키마 검증이 에이전트 반환 즉시 실행되는지 (runner에서 검증)
- 계약 픽스처가 `testdata/contracts/`에 추가됐는지

### Epic 3 - Story 3.2 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 외부 API 호출 제거
- 4막 스키마 강제 추가
- 검증 위치 수정
- 계약 픽스처 추가

---

### Epic 3 - Story 3.3 개발
`/bmad-create-story`
Writer 에이전트와 포스트-Writer Critic 체크포인트를 구현한다.
- Writer: 시나리오 구조 → 한국어 나레이션 생성, 금지어 목록 강제 적용 (FR48)
- 런타임 Writer ≠ Critic 프로바이더 검증 (FR12, defense-in-depth)
- 포스트-Writer 체크포인트: Critic 에이전트 호출 (FR13)
- Critic 사전 검사: JSON 스키마 검증 + 금지어 정규식 (FR25)
- Critic 판정: pass/retry/accept-with-notes + 루브릭 점수 (hook, 사실 정확도, 감정 변화, 몰입감)
- 금지어 목록은 오퍼레이터 편집 가능한 버전 관리 파일

### Epic 3 - Story 3.3 코드 리뷰
`/bmad-code-review`
Story 3.3 구현 결과를 검토한다.
- 금지어 목록이 하드코딩이 아닌 파일 로드인지 (NFR-M2)
- Writer ≠ Critic 검증이 런타임(runner 레벨)에서 실행되는지
- Critic 사전 검사(스키마 + 금지어)가 LLM 호출 전에 실행되는지
- Critic 판정 4개 루브릭 점수 모두 구현됐는지
- `retry_reason`이 Critic 결과에 따라 올바르게 설정되는지

### Epic 3 - Story 3.3 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 금지어 목록 하드코딩 → 파일 로드
- 런타임 검증 위치 수정
- Critic 사전 검사 순서 보정
- 루브릭 점수 누락 필드 추가

---

### Epic 3 - Story 3.4 개발
`/bmad-create-story`
VisualBreakdowner와 Reviewer 에이전트를 구현한다.
- VisualBreakdowner: 나레이션 + TTS 추정 시간 → 씬별 샷 브레이크다운 (1-5샷)
- 샷 수 공식: ≤8s→1, 8-15s→2, 15-25s→3, 25-40s→4, 40s+→5
- 각 샷: `visual_descriptor`(밀도 높은 텍스트), `estimated_duration_s`, `transition`(ken_burns|cross_dissolve|hard_cut)
- Frozen Descriptor가 모든 샷의 visual_descriptor 접두사로 전파
- 씬별 샷 수/전환 타입 오퍼레이터 오버라이드 가능 (`shot_overrides[scene_index]`)
- Reviewer: 사실 오류·일관성 문제 플래그 리뷰 리포트 생성
- 스키마 검증 (FR11): `Scenes[i].Shots[]` 배열, 각 요소 필수 필드

### Epic 3 - Story 3.4 코드 리뷰
`/bmad-code-review`
Story 3.4 구현 결과를 검토한다.
- Frozen Descriptor 접두사가 모든 샷에 verbatim으로 적용됐는지
- 샷 수 공식이 TTS 추정 시간을 기준으로 정확히 계산되는지
- 오버라이드가 scenario.json의 `shot_overrides` 키에 저장되는지
- 스키마 검증이 runner에서 Reviewer 반환 직후 실행되는지

### Epic 3 - Story 3.4 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- Frozen Descriptor 전파 로직 수정
- 샷 수 공식 경계값 수정
- 오버라이드 저장 키 수정
- 검증 위치 보정

---

### Epic 3 - Story 3.5 개발
`/bmad-create-story`
Phase A 완료와 포스트-Reviewer Critic 최종 체크포인트를 구현한다.
- 포스트-Reviewer Critic 두 번째 호출 (FR13): 전체 시나리오(비주얼 브레이크다운 포함) 평가
- 누적 품질 점수 생성
- Phase A 완료 시 최종 `scenario.json` 저장: 모든 에이전트 출력, 스키마, Critic 판정 포함 (NFR-M2)
- integration 테스트: Critic 체크포인트 2회, 최종 scenario.json 무결성

### Epic 3 - Story 3.5 코드 리뷰
`/bmad-code-review`
Story 3.5 구현 결과를 검토한다.
- Critic이 두 번 호출되는지 (포스트-Writer, 포스트-Reviewer) 로그/테스트로 확인
- scenario.json에 두 Critic 판정 결과가 모두 포함됐는지
- 최종 파일이 버전 관리 가능한 구조로 저장됐는지

### Epic 3 - Story 3.5 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- Critic 두 번 호출 로직 수정
- scenario.json 두 판정 결과 포함
- 저장 구조 정리

---

## Epic 4: Advanced Quality Infrastructure

---

### Epic 4 - Story 4.1 개발
`/bmad-create-story`
Golden eval 세트 거버넌스와 검증 시스템을 구현한다.
- `internal/critic/eval/` 디렉토리: 1:1 양성:음성 비율 강제
- `pipeline golden add --positive <path> --negative <path>`: 스키마 검증 후 수락, 비율 재검증
- `pipeline golden list`: 모든 픽스처 쌍과 인덱스·생성 타임스탬프 표시
- `testdata/golden/`에 단조 증가 인덱스로 버전 관리
- N일(기본 30일) 미갱신 시 "Staleness Warning" 발행
- Critic 프롬프트 변경 후 Golden 미실행 시 경고 (FR26)
- `go test ./internal/critic/eval -run Golden`: 탐지율(recall) 리포트

### Epic 4 - Story 4.1 코드 리뷰
`/bmad-code-review`
Story 4.1 구현 결과를 검토한다.
- 1:1 비율 강제가 pair 단위로 작동하는지 (개별 양성/음성 추가 불가)
- staleness 타임스탬프가 파일 기반으로 관리되는지 (DB 아님)
- Critic 프롬프트 변경 감지가 content hash 기반인지
- 픽스처가 `testdata/golden/`에 실제로 체크인됐는지

### Epic 4 - Story 4.1 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 비율 강제 로직 pair 단위로 수정
- staleness 관리 방식 수정
- 프롬프트 변경 감지 hash 기반 구현
- 픽스처 파일 커밋

---

### Epic 4 - Story 4.2 개발
`/bmad-create-story`
Shadow eval 러너를 구현한다.
- `go test ./internal/critic/eval -run Shadow`: 최근 N=10(설정 가능) 통과 씬을 runs 테이블에서 가져와 새 프롬프트로 재평가
- 결과 로깅: pass/fail + 점수 diff
- 거짓 거부(false-rejection) 회귀 감지

### Epic 4 - Story 4.2 코드 리뷰
`/bmad-code-review`
Story 4.2 구현 결과를 검토한다.
- N이 설정에서 읽히는지 (하드코딩 아님)
- SQLite에서 최근 N 통과 씬 쿼리가 올바른 인덱스를 사용하는지
- 점수 diff 출력이 명확한지 (어떤 씬이 왜 실패했는지)

### Epic 4 - Story 4.2 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- N 하드코딩 → 설정 기반
- 쿼리 최적화
- diff 출력 개선

---

### Epic 4 - Story 4.3 개발
`/bmad-create-story`
Cohen's kappa 교정 추적을 구현한다.
- `decisions` 테이블(오퍼레이터 행동)과 `runs` 테이블(Critic 점수)를 조인
- 롤링 25-런 윈도우 Cohen's kappa 계산
- n < 25이면 "provisional" 라벨
- kappa 값과 트렌드 데이터를 DB에 저장
- 순수 단위 테스트: 알려진 agreement/disagreement 데이터로 kappa 공식 검증

### Epic 4 - Story 4.3 코드 리뷰
`/bmad-code-review`
Story 4.3 구현 결과를 검토한다.
- Cohen's kappa 공식이 weighted/unweighted 중 어느 것인지 문서화됐는지
- 조인 쿼리가 올바른 runs/decisions 관계를 사용하는지
- provisional 조건이 window 파라미터 기반인지 (고정 25 아님)

### Epic 4 - Story 4.3 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- kappa 공식 문서화 추가
- 조인 쿼리 수정
- provisional 조건 파라미터 기반으로 수정

---

### Epic 4 - Story 4.4 개발
`/bmad-create-story`
미성년자 콘텐츠 보호막과 자동 승인 임계값을 구현한다.
- 미성년자 관련 민감 콘텐츠 감지(정규식 + LLM Critic 하위 검사): 다운스트림 자동화 차단, status=`waiting_for_review`, "Safeguard Triggered: Minors" 플래그
- 보호막 트리거 시 오퍼레이터 강제 노트 입력 후 Override 가능
- Critic 점수 > auto-approval 임계값이고 보호막 미트리거 시: status=`auto_approved`, decisions에 `system_auto_approved` 기록
- Golden 픽스처 세트에 "minors" 카테고리 known-fail 픽스처 최소 1개 추가 (NFR-T5)

### Epic 4 - Story 4.4 코드 리뷰
`/bmad-code-review`
Story 4.4 구현 결과를 검토한다.
- 보호막이 Critic 점수와 무관하게 차단하는지 (점수 높아도 차단)
- Override 시 노트(note)가 decisions 테이블에 기록되는지
- auto-approval 임계값이 설정에서 읽히는지
- Golden 픽스처에 "minors" 카테고리 파일이 실제 존재하는지

### Epic 4 - Story 4.4 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 보호막 점수 무관 차단 로직 수정
- Override 노트 기록 추가
- 임계값 설정화
- Golden 픽스처 파일 추가

---

## Epic 5: Media Generation (Phase B — Image & TTS)

---

### Epic 5 - Story 5.1 개발
`/bmad-create-story`
공통 Rate-Limiting과 지수 백오프 인프라를 구현한다. (Tier 1 크로스커팅 — 다른 Phase B 스토리보다 먼저 완료 필수)
- `golang.org/x/sync/semaphore`(동시성) + `golang.org/x/time/rate` 토큰 버킷(RPM) 이중 레이어
- DashScope 이미지·TTS 트랙이 동일한 limiter 인스턴스 공유 (같은 벤더 예산)
- 429/500 에러: 지수 백오프 + 지터, max 30s 지연, FakeClock으로 결정적 테스트
- AC-RL1: 결정적 백오프 타이밍 (FakeClock, 실제 sleep 없음)
- AC-RL2: 두 병렬 트랙 RPM 제한 ±5% 이내 준수
- AC-RL3: 30초 circuit-break, 고루틴 누수/데드락 없음

### Epic 5 - Story 5.1 코드 리뷰
`/bmad-code-review`
Story 5.1 구현 결과를 검토한다.
- DashScope 이미지와 TTS가 진짜로 동일한 limiter 인스턴스를 공유하는지 (복사본 아님)
- LLM 프로바이더들은 각자 별도 limiter를 가지는지
- FakeClock이 백오프 로직에서 실제 `time.Sleep` 대신 사용되는지
- circuit-break 후 고루틴이 정리되는지 (goroutine leak 테스트)

### Epic 5 - Story 5.1 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- limiter 인스턴스 공유 수정 (포인터 전달)
- LLM 프로바이더별 별도 limiter 분리
- FakeClock 교체
- goroutine leak 수정

---

### Epic 5 - Story 5.2 개발
`/bmad-create-story`
Phase B 병렬 미디어 생성 러너(errgroup)를 구현한다.
- `errgroup.Group` (NOT `errgroup.WithContext`) 사용: 한 트랙 실패가 다른 트랙 취소 금지 (FR16)
- 이미지 트랙 실패 + TTS 트랙 성공 시: 이미지 트랙 에러 옵저버빌리티 기록, 런 status=failed, TTS 아티팩트 보존
- 두 트랙 모두 완료 후 assembly 시작
- 벽시계 시간 캡처 (NFR-P4)
- Story 5.1 rate-limiter 인프라 사용

### Epic 5 - Story 5.2 코드 리뷰
`/bmad-code-review`
Story 5.2 구현 결과를 검토한다.
- `errgroup.WithContext` 사용 코드가 없는지 (`errgroup.Group`만 사용)
- 한 트랙 실패 시 다른 트랙이 계속 실행되는지 (test: 이미지 트랙 실패 주입 → TTS 완료 확인)
- TTS 아티팩트가 이미지 트랙 실패 후 삭제되지 않는지
- 두 트랙의 옵저버빌리티 행이 모두 DB에 기록되는지

### Epic 5 - Story 5.2 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- `errgroup.WithContext` → `errgroup.Group` 교체
- 트랙 독립성 보장 로직 수정
- 아티팩트 정리 로직에서 TTS 보호
- 옵저버빌리티 기록 추가

---

### Epic 5 - Story 5.3 개발
`/bmad-create-story`
캐릭터 레퍼런스 선택과 검색 결과 캐시를 구현한다.
- DuckDuckGo에서 10개 후보 이미지 검색, `CharacterGroup` 스키마로 오퍼레이터 제시
- 검색 결과 SQLite 기반 영속 캐시 (중복 외부 조회 방지, FR19)
- 선택된 캐릭터 ID를 런 상태에 `selected_character_id`로 저장
- Story 5.4에서 이 ID로 canonical 이미지 조회

### Epic 5 - Story 5.3 코드 리뷰
`/bmad-code-review`
Story 5.3 구현 결과를 검토한다.
- 캐시 테이블이 `001_init.sql` 또는 별도 마이그레이션에 정의됐는지
- 캐시 히트 시 외부 HTTP 호출이 실제로 없는지 (nohttp 테스트)
- `selected_character_id`가 runs 테이블 또는 연관 테이블에 저장되는지

### Epic 5 - Story 5.3 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 캐시 테이블 마이그레이션 추가
- 캐시 히트 경로 외부 호출 제거
- 캐릭터 ID 저장 위치 명확화

---

### Epic 5 - Story 5.4 개발
`/bmad-create-story`
Frozen Descriptor 전파와 샷별 이미지 생성(DashScope)을 구현한다.
- scenario.json의 샷 브레이크다운 읽기 (1-5샷/씬, 오퍼레이터 오버라이드 반영)
- 각 샷 이미지 프롬프트: Frozen Descriptor 접두사 + 샷 레벨 visual_descriptor
- 캐릭터 포함 샷: `qwen-image-edit`(레퍼런스 기반) + `selected_character_id`
- 비캐릭터 샷: 표준 이미지 생성
- 저장: `images/scene_{idx}/shot_{idx}.png`
- `segments.shots` JSON 배열 업데이트: image_path, duration_s, transition
- 10씬 × 평균 3샷 = ~30개 이미지

### Epic 5 - Story 5.4 코드 리뷰
`/bmad-code-review`
Story 5.4 구현 결과를 검토한다.
- Frozen Descriptor가 모든 샷에 verbatim 접두사로 붙는지 (수정 없음)
- 캐릭터 유무에 따른 Generate vs Edit 분기가 올바른지
- 디렉토리 구조 `images/scene_{idx}/shot_{idx}.png`가 정확히 구현됐는지
- segments.shots가 이미지 생성 직후 업데이트되는지 (일괄 업데이트 아님)

### Epic 5 - Story 5.4 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- Frozen Descriptor 수정 방지 로직 추가
- Generate/Edit 분기 수정
- 디렉토리 경로 수정
- segments 업데이트 타이밍 수정

---

### Epic 5 - Story 5.5 개발
`/bmad-create-story`
한국어 TTS와 정규식 기반 음역 처리(DashScope)를 구현한다.
- 나레이션 텍스트 전처리: 숫자("SCP-049"→"에스씨피-공사구"), 영어 단어("doctor"→"닥터") 한국어 음역
- DashScope qwen3-tts-flash 클라이언트로 한국어 음성 파일(MP3/WAV) 생성
- 음역 정규식 범위: 화폐, 날짜, 숫자, 일반 영어 단어
- Story 5.1 rate-limiter와 공유 DashScope limiter 사용

### Epic 5 - Story 5.5 코드 리뷰
`/bmad-code-review`
Story 5.5 구현 결과를 검토한다.
- 음역 정규식 테스트 커버리지가 화폐·날짜·숫자·영어 단어 4개 범주 모두 포함하는지
- TTS 호출 전에 반드시 음역이 먼저 실행되는지
- DashScope TTS가 Story 5.1의 공유 limiter를 사용하는지

### Epic 5 - Story 5.5 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 누락 음역 범주 정규식 추가
- 음역 실행 순서 보정
- limiter 공유 연결 수정

---

## Epic 6: Web UI — Design System & Application Shell

---

### Epic 6 - Story 6.1 개발
`/bmad-create-story`
Dark-only 테마 엔진과 글로벌 스타일링을 구현한다.
- `styles/tokens.css`: 16개 시맨틱 색상 토큰(RGB 트리플렛) — background(#0F1117)부터 destructive(#EF4444)까지
- Tailwind 테마 확장: CSS custom property 색상 토큰 참조
- Geist Sans + Geist Mono 번들(Go embed.FS 포함), 한국어 폴백 시스템 고딕 스택, font-display: swap
- 8레벨 타입 스케일(display 30px ~ caption 12px, 모두 rem)
- 4px 기본 스페이싱 스케일 (7개 명명 토큰: space-1~space-12)
- `data-density` 어포던스 밀도 시스템 (standard/elevated)
- dark: 접두사 없음, 라이트 모드 없음, 토글 없음

### Epic 6 - Story 6.1 코드 리뷰
`/bmad-code-review`
Story 6.1 구현 결과를 검토한다.
- 16개 토큰 모두 RGB 트리플렛(알파 컴포지팅용)으로 정의됐는지 (`#hex` 아님)
- `dark:` 접두사 사용 코드가 없는지
- Geist 폰트가 Go embed.FS에 번들됐는지 (CDN 아님)
- 8레벨 타입 스케일이 rem 단위인지 (px 아님)
- `data-density` attribute가 CSS custom property로 구현됐는지

### Epic 6 - Story 6.1 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- hex → RGB 트리플렛으로 변환
- `dark:` 접두사 제거
- 폰트 CDN → embed.FS로 이동
- px → rem 단위 변환

---

### Epic 6 - Story 6.2 개발
`/bmad-create-story`
SPA 아키텍처와 네비게이션 셸을 구현한다.
- CSS Grid 레이아웃: 사이드바(220px/48px) + 콘텐츠 창(flex-1, 최소 70%)
- Sidebar: 확장/축소, localStorage 상태 유지, <1280px 자동 축소
- 3-route SPA: `/production`, `/tuning`, `/settings` (React Router v7, BrowserRouter)
- 활성 탭 하이라이트, 페이지 전체 리로드 없는 탐색
- Zustand localStorage persist for UI state

### Epic 6 - Story 6.2 코드 리뷰
`/bmad-code-review`
Story 6.2 구현 결과를 검토한다.
- React Router v7 (`BrowserRouter + Routes`) 사용 확인 (v6 아님)
- Sidebar 상태가 Zustand localStorage persist로 저장되는지
- <1280px 자동 축소가 CSS media query로 구현됐는지 (JS resize 이벤트 아님)
- 콘텐츠 창의 최소 너비 70% 강제됐는지

### Epic 6 - Story 6.2 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- Router 버전 업그레이드
- 상태 지속성 Zustand로 이동
- CSS media query 방식으로 수정
- 최소 너비 제약 추가

---

### Epic 6 - Story 6.3 개발
`/bmad-create-story`
8-키 글로벌 키보드 단축키 엔진을 구현한다.
- 전역 훅: Enter, Esc, J, K, Tab, Ctrl+Z, Shift+Enter, S, 1-9/0 등록
- 입력 필드/텍스트에어리어 포커스 시 J/K 등 글로벌 단축키 억제 (입력 충돌 방지)
- UI에 키 레이블 항상 표시 (ActionBar 스타일)
- 커스텀 ESLint 규칙: Enter=primary, Esc=secondary 불변성 강제 (UX-DR52)

### Epic 6 - Story 6.3 코드 리뷰
`/bmad-code-review`
Story 6.3 구현 결과를 검토한다.
- input/textarea 포커스 감지가 이벤트 버블링을 사용하는지 (직접 ref 아님)
- ESLint 규칙이 실제로 빌드/lint를 실패시키는지 테스트됐는지
- 단축키 핸들러가 React 렌더링 사이클 외부에서 등록되는지 (이벤트 리스너 중복 방지)

### Epic 6 - Story 6.3 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 포커스 감지 방식 수정
- ESLint 규칙 테스트 추가
- 핸들러 등록 중복 방지

---

### Epic 6 - Story 6.4 개발
`/bmad-create-story`
Go embed.FS SPA 정적 파일 서빙을 구현한다.
- `embed.FS`로 `web/dist/` 번들 Go 바이너리에 포함
- Vite config `base: "./"` 설정
- `/api/*` 외 모든 경로 → `index.html` fallback (SPA catch-all)
- `--dev` 모드: Vite 개발 서버 프록시
- Makefile: `web-build` → `go-build` 의존성

### Epic 6 - Story 6.4 코드 리뷰
`/bmad-code-review`
Story 6.4 구현 결과를 검토한다.
- `/api/` 경로가 SPA catch-all에서 제외됐는지
- `embed.FS` 경로가 빌드 시 올바르게 해석되는지 (상대 경로 주의)
- `--dev` 모드 프록시 타겟이 Vite 기본 포트(5173)인지
- `web/dist/`가 `.gitignore`에 포함됐는지

### Epic 6 - Story 6.4 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- `/api/` catch-all 제외 로직 추가
- embed 경로 수정
- 프록시 포트 수정
- .gitignore 확인

---

### Epic 6 - Story 6.5 개발
`/bmad-create-story`
Playwright 스모크 테스트와 UX 테스팅 인프라를 구축한다 (FR52-web).
- Playwright Chromium only, 1 spec: SPA 로드, `/production` 렌더링, 기본 키보드 상호작용(Enter, Esc)
- `renderWithProviders.tsx`: MemoryRouter + QueryClient 래퍼
- Zod 스키마로 `testdata/contracts/` 픽스처 파싱 검증 (Go 언마샬 ↔ Zod 파싱 패리티)
- CSS custom property 검증: `getComputedStyle()`로 16개 토큰 비어있지 않음 확인
- ESLint keyboard invariance 규칙 동작 확인

### Epic 6 - Story 6.5 코드 리뷰
`/bmad-code-review`
Story 6.5 구현 결과를 검토한다.
- Playwright spec이 정확히 1개인지 (V1 제약)
- Zod 스키마가 Go 타입과 snake_case 필드명으로 일치하는지
- CSS 토큰 검증이 JSDOM에서 `getComputedStyle()`로 실제 작동하는지
- `renderWithProviders`가 QueryClient도 포함하는지

### Epic 6 - Story 6.5 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- Playwright spec 수 1개로 정리
- Zod 스키마 필드명 일치 수정
- CSS 토큰 검증 JSDOM 호환성 수정
- QueryClient wrapper 추가

---

## Epic 7: Production Tab — Scenario Review & Character Selection

---

### Epic 7 - Story 7.1 개발
`/bmad-create-story`
파이프라인 대시보드와 런 상태 컴포넌트(StatusBar/RunCard)를 구현한다.
- StatusBar: 스테이지 아이콘 + 이름 + 경과 시간 + 비용, 호버 시 런 ID + 비용 상세
- StageStepper: 6개 노드(pending/active/completed/failed), full(아이콘+레이블) / compact(아이콘만)
- RunCard: SCP ID(mono) + 런 번호 + 요약 + compact StageStepper + 상태 배지
- Critic 점수 ambient 비주얼 토큰 (UX-DR47): ≥80 green, 50-79 accent, <50 amber
- 사이드바 런 목록: 검색 가능, 상태 인식 RunCard
- `/api/runs/{id}/status` 5초 폴링

### Epic 7 - Story 7.1 코드 리뷰
`/bmad-code-review`
Story 7.1 구현 결과를 검토한다.
- Critic 점수 색상 토큰이 3개 범위(≥80/50-79/<50)로 올바르게 분기되는지
- StatusBar가 활성 런 없을 때 0px로 축소되는지
- 5초 폴링이 TanStack Query의 `refetchInterval`로 구현됐는지
- RunCard가 스테이지 상태에 따른 ARIA 속성을 가지는지

### Epic 7 - Story 7.1 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- Critic 점수 색상 범위 수정
- StatusBar idle 상태 0px 처리
- 폴링 구현 방식 수정
- ARIA 속성 추가

---

### Epic 7 - Story 7.2 개발
`/bmad-create-story`
시나리오 인스펙터와 인라인 편집기를 구현한다.
- 시나리오 리뷰 대기 포인트에서 나레이션 문단 표시
- 클릭 시 인라인 편집기(textarea) 전환, 편집 모드 시각 피드백(border/background)
- blur-save 또는 Enter 저장: "Saving..." 상태 표시 → API 퍼시스트
- 저장 실패 시 에러 메시지 + 원본 텍스트 복원
- Tab으로 편집 모드 진입, Ctrl+Z 되돌리기

### Epic 7 - Story 7.2 코드 리뷰
`/bmad-code-review`
Story 7.2 구현 결과를 검토한다.
- blur-save와 Enter 저장이 중복 API 호출 없이 처리되는지
- 저장 실패 시 낙관적 업데이트가 롤백되는지
- Tab 키가 편집 모드 진입에 사용되면서 포커스 이동과 충돌하지 않는지

### Epic 7 - Story 7.2 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 중복 저장 호출 방지 (debounce 또는 flag)
- 낙관적 업데이트 롤백 추가
- Tab 키 충돌 해결

---

### Epic 7 - Story 7.3 개발
`/bmad-create-story`
캐릭터 선택 인터페이스(후보 그리드)를 구현한다.
- CharacterGrid: 2×5 그리드, 1-9/0 키 직접 선택, Enter 확인, Esc 재검색
- 이미지 프리로드로 부드러운 렌더링
- Vision Descriptor 텍스트에어리어: 선택된 캐릭터 자동 추출 값 프리필
- 이전 런(동일 SCP ID)이 있으면 이전 descriptor 프리필 (UX-DR62)
- Tab → 편집 모드, blur-save, Ctrl+Z → 프리필 되돌리기
- 편집된 descriptor → Frozen Descriptor로 저장 (모든 이미지 프롬프트에 verbatim 사용)

### Epic 7 - Story 7.3 코드 리뷰
`/bmad-code-review`
Story 7.3 구현 결과를 검토한다.
- 이전 런 프리필이 같은 scp_id 기준으로 최신 런에서 가져오는지
- Frozen Descriptor가 확인 후 즉시 segments에 전파되는지 (나중에 아님)
- Ctrl+Z가 편집 내용만 되돌리고 그 이전 단계(선택 등)는 되돌리지 않는지

### Epic 7 - Story 7.3 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 이전 런 조회 쿼리 scp_id 필터 추가
- Frozen Descriptor 전파 타이밍 수정
- Ctrl+Z 범위 수정

---

### Epic 7 - Story 7.4 개발
`/bmad-create-story`
FailureBanner 단계별 실패 처리를 구현한다.
- FailureBanner: amber 왼쪽 border + 실패 메시지 + 비용 캡 상태 + "No work was lost"
- 재시도 가능 에러(429: orange) vs 치명적 에러(400: red) 시각 구분
- [Enter] Resume 버튼 → 백엔드 stage re-entry
- 재개 시 자동 닫힘, Esc 수동 닫힘

### Epic 7 - Story 7.4 코드 리뷰
`/bmad-code-review`
Story 7.4 구현 결과를 검토한다.
- 에러 타입(retryable vs fatal)이 API 응답에서 올바르게 파싱되는지
- FailureBanner가 ARIA role을 가지는지 (alert 또는 status)
- Resume 버튼이 `/api/runs/{id}/resume` POST를 호출하는지

### Epic 7 - Story 7.4 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 에러 타입 파싱 수정
- ARIA role 추가
- Resume API 엔드포인트 연결

---

### Epic 7 - Story 7.5 개발
`/bmad-create-story`
온보딩과 연속성 경험을 구현한다.
- 최초 실행 감지(localStorage)→ 3-모드 워크플로우 개요 온보딩 모달(1회 표시)
- Production 탭 진입 시 연속성 배너: "What changed since last session" 5초 자동 닫힘
- 상호작용 시 즉시 닫힘

### Epic 7 - Story 7.5 코드 리뷰
`/bmad-code-review`
Story 7.5 구현 결과를 검토한다.
- 온보딩 1회 표시가 localStorage 플래그로 보장되는지
- 연속성 배너 메시지가 Story 2.6 백엔드 diff에서 오는지 (하드코딩 아님)
- 5초 자동 닫힘이 interaction 시 취소되는지

### Epic 7 - Story 7.5 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- localStorage 플래그 추가
- 배너 메시지 백엔드 연결
- 자동 닫힘 interaction 취소 수정

---

## Epic 8: Batch Review & Decision Management

---

### Epic 8 - Story 8.1 개발
`/bmad-create-story`
Master-Detail 리뷰 레이아웃(SceneCard/DetailPanel)을 구현한다.
- 30:70 split: SceneCard 리스트 (썸네일 스트립 1-5) + DetailPanel (씬 클립/샷 갤러리, 전환 인디케이터)
- J/K 탐색, Focus-Follows-Selection
- 리제네레이션된 씬: Before/After diff 비주얼라이제이션 (나레이션 + 이미지), 버전 토글
- High-leverage 씬 (캐릭터 첫 등장, hook 씬, 막 경계 씬): "High-Leverage" 배지, 대기 큐 상단 정렬
- High-leverage DetailPanel: 큰 이미지 미리보기, 전체 Critic 점수 분류, "Why high-leverage" 주석

### Epic 8 - Story 8.1 코드 리뷰
`/bmad-code-review`
Story 8.1 구현 결과를 검토한다.
- High-leverage 씬 식별 로직이 3가지(캐릭터 첫 등장, hook, 막 경계) 모두 구현됐는지
- Before/After diff가 씬이 리제네레이션된 경우에만 표시되는지
- 30:70 split이 1024-1279px 뷰포트에서도 유지되는지 (UX-DR60)
- J/K가 빈 리스트에서 충돌하지 않는지 (K at first → wraps to last)

### Epic 8 - Story 8.1 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- High-leverage 식별 로직 보강
- diff 조건부 표시 수정
- 뷰포트 split 유지 수정
- J/K 경계 처리 추가

---

### Epic 8 - Story 8.2 개발
`/bmad-create-story`
씬 리뷰 액션과 AudioPlayer를 구현한다.
- AudioPlayer: 재생/일시정지(Space), Seekbar, 씬 변경 시 0:00 리셋
- Approve(Enter)/Reject(Esc)/Skip(S) 액션 → decisions 테이블 기록 + 다음 씬 자동 선택
- Skip = `skip_and_remember`: 씬 특성(Critic 점수, scene_index, 콘텐츠 플래그) 저장 → 미래 "Based on your past choices" 힌트용

### Epic 8 - Story 8.2 코드 리뷰
`/bmad-code-review`
Story 8.2 구현 결과를 검토한다.
- AudioPlayer가 씬 변경(J/K) 시 자동으로 리셋되는지
- Space 키가 AudioPlayer 재생 토글에 사용되면서 다른 단축키와 충돌하지 않는지
- `skip_and_remember` 패턴이 쿼리 가능한 구조로 저장되는지

### Epic 8 - Story 8.2 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- AudioPlayer 씬 변경 리셋 수정
- Space 키 충돌 해결
- `skip_and_remember` 저장 구조 수정

---

### Epic 8 - Story 8.3 개발
`/bmad-create-story`
의사결정 되돌리기와 Command Pattern을 구현한다.
- Ctrl+Z: 최근 결정 되돌리기(최대 10단계)
- V1 되돌리기 범위: 씬 승인, 씬 거절, 씬 스킵, 배치 "전체 승인"(하나의 액션), Vision Descriptor 텍스트 편집
- DB: decisions 테이블에 `superseded_by` FK로 되돌리기 행 삽입 (하드 삭제 없음)
- Phase C 렌더링 진입 전까지만 되돌리기 가능
- 되돌리기 후 포커스 복원

### Epic 8 - Story 8.3 코드 리뷰
`/bmad-code-review`
Story 8.3 구현 결과를 검토한다.
- 되돌리기가 DB에서 행을 삭제하지 않고 `superseded_by` 삽입으로 구현됐는지
- Phase C 진입 후 되돌리기 차단이 스테이지 상태 기반인지
- 10단계 스택 한도가 Zustand store에서 관리되는지
- 포커스 복원이 이전 SceneCard를 선택하는지

### Epic 8 - Story 8.3 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 하드 삭제 → `superseded_by` 삽입으로 수정
- Phase C 차단 조건 수정
- 스택 한도 관리 위치 수정
- 포커스 복원 로직 추가

---

### Epic 8 - Story 8.4 개발
`/bmad-create-story`
거절과 리제네레이션 플로우를 구현한다.
- Esc(거절) → 인라인 이유 프롬프트(모달 아님)
- 이유 구조적 유사성 경고: 같은 씬의 이전 실패 시도와 비교 (FR53)
- 백그라운드 리제네레이션 태스크 발송(max 2회 재시도)
- 2회 초과 시 수동 편집 또는 skip & flag 제안

### Epic 8 - Story 8.4 코드 리뷰
`/bmad-code-review`
Story 8.4 구현 결과를 검토한다.
- 거절 이유가 모달이 아닌 인라인으로 구현됐는지
- V1 유사도 정의(same SCP ID + same scene_index)로 이전 거절 비교하는지
- 2회 재시도 초과 상태가 씬 카드에 표시되는지

### Epic 8 - Story 8.4 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 모달 → 인라인 프롬프트로 교체
- 유사도 비교 로직 V1 스펙으로 수정
- 재시도 초과 상태 표시 추가

---

### Epic 8 - Story 8.5 개발
`/bmad-create-story`
배치 "전체 나머지 승인" 기능을 구현한다.
- Shift+Enter → 확인 다이얼로그 → 미검토 씬 전체 'Approved' 업데이트
- DB 잠금 방지: 50개 청크 단위 처리
- InlineConfirmPanel: push-up 60px, Enter/Esc, ARIA role="alertdialog", focus trap
- 전체 승인이 되돌리기 스택에서 단일 액션으로 처리

### Epic 8 - Story 8.5 코드 리뷰
`/bmad-code-review`
Story 8.5 구현 결과를 검토한다.
- 청크 처리가 실제로 50개 단위로 분할되는지
- InlineConfirmPanel에 focus trap이 구현됐는지 (ARIA alertdialog)
- 전체 승인이 되돌리기 스택에서 단일 항목으로 기록되는지

### Epic 8 - Story 8.5 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 청크 사이즈 파라미터화 (50)
- focus trap 구현
- 되돌리기 단일 항목 기록 수정

---

### Epic 8 - Story 8.6 개발
`/bmad-create-story`
의사결정 히스토리와 타임라인 뷰를 구현한다.
- TimelineView: 시간순 결정 목록 (타임스탬프, 타입, 이유)
- 100ms 디바운스 라이브 필터링 (결정 타입, 이유 검색)
- J/K 키보드 탐색
- 대규모 결정 세트에서 쿼리 성능 (인덱스 사용)

### Epic 8 - Story 8.6 코드 리뷰
`/bmad-code-review`
Story 8.6 구현 결과를 검토한다.
- 100ms 디바운스가 실제로 적용됐는지 (즉시 필터링 아님)
- decisions 테이블 쿼리가 NFR-O4 인덱스를 사용하는지
- J/K 탐색이 타임라인에서도 동작하는지 (SceneCard와 컨텍스트 분리)

### Epic 8 - Story 8.6 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 디바운스 타이밍 수정
- 쿼리 인덱스 활용 최적화
- J/K 컨텍스트 분리 구현

---

## Epic 9: Video Assembly & Compliance (Phase C)

---

### Epic 9 - Story 9.1 개발
`/bmad-create-story`
FFmpeg 2단계 어셈블리 엔진을 구현한다.
- **1단계 씬 클립**: 씬별 1-5 샷 이미지 + 전환(Ken Burns=zoompan, 크로스 디졸브=xfade 0.5s, 하드컷=직접 연결) + TTS 오디오 오버레이 → `clips/scene_{idx}.mp4` (H.264+AAC, 1080p)
- sync padding: 오디오-비디오 정렬 유지
- **2단계 최종 합성**: concat demuxer로 모든 씬 클립 → `output.mp4`, 총 시간 = 씬 클립 합산
- 1샷 씬(≤8s TTS): Ken Burns 단독 적용, 인터샷 전환 없음

### Epic 9 - Story 9.1 코드 리뷰
`/bmad-code-review`
Story 9.1 구현 결과를 검토한다.
- `xfade` 필터 duration이 0.5s로 고정됐는지
- Ken Burns `zoompan` 파라미터가 의도한 pan/zoom 효과를 내는지
- concat demuxer가 scene_index 순서로 씬을 처리하는지
- 갭/오버랩 없이 연결되는지 (output.mp4 총 시간 검증)

### Epic 9 - Story 9.1 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- xfade duration 고정 수정
- zoompan 파라미터 조정
- concat 순서 보정
- 총 시간 검증 테스트 추가

---

### Epic 9 - Story 9.2 개발
`/bmad-create-story`
메타데이터와 저작권 귀속 번들을 생성한다.
- 어셈블리 완료 후 `metadata.json`(YT-ready AI 공개 정보) 생성
- `manifest.json`: SCP 아티클 출처 URL, 저자 이름, 사용된 모든 SCP 구성요소 라이선스 체인 포함
- 사용된 모든 LLM 프로바이더 기록 (FR45, 비null 보장)

### Epic 9 - Story 9.2 코드 리뷰
`/bmad-code-review`
Story 9.2 구현 결과를 검토한다.
- metadata.json에 사용된 모든 모델 ID가 기록됐는지 (텍스트, 이미지, TTS 모두)
- manifest.json의 라이선스 체인이 SCP CC BY-SA 3.0을 커버하는지
- LLM 프로바이더 필드가 runs 테이블의 모든 관련 행에 비null인지

### Epic 9 - Story 9.2 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 모델 ID 기록 범위 확장
- CC BY-SA 3.0 라이선스 체인 추가
- null 프로바이더 필드 보강

---

### Epic 9 - Story 9.3 개발
`/bmad-create-story`
컴플라이언스 감사 로깅을 구현한다.
- 모든 미디어 생성/나레이션 작업: 프롬프트, 프로바이더, 비용, 차단된 ID 기록 → 런의 `audit.log` append
- 차단된 voice-ID(블록리스트) 요청: TTS 호출 전 차단, "Voice profile '{id}' is blocked" 에러, audit 로그 기록
- 오퍼레이터가 config.yaml 수정 후 resume 가능

### Epic 9 - Story 9.3 코드 리뷰
`/bmad-code-review`
Story 9.3 구현 결과를 검토한다.
- audit.log가 append 모드로 열리는지 (덮어쓰기 아님)
- voice-ID 차단이 TTS synthesizer 생성자 주입 레벨에서 일어나는지
- 블록리스트가 config.yaml에서 읽히는지 (하드코딩 아님)

### Epic 9 - Story 9.3 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 파일 오픈 모드 수정 (append)
- voice-ID 차단 위치 수정
- 블록리스트 설정화

---

### Epic 9 - Story 9.4 개발
`/bmad-create-story`
업로드 전 컴플라이언스 게이트를 구현한다.
- Phase C 완료 후 'Final Review' 대기 상태
- UI: 비디오 미리보기 + 메타데이터 체크리스트
- 오퍼레이터 명시적 확인 + 'Finalize' 클릭 전까지 `ready-for-upload` 상태 미설정 (NFR-L1 우회 경로 없음)
- 완료 보상 화면: 썸네일 + 5초 자동 재생 + 메타데이터 확인 + 다음 액션 CTA

### Epic 9 - Story 9.4 코드 리뷰
`/bmad-code-review`
Story 9.4 구현 결과를 검토한다.
- `ready-for-upload` 상태 전환에 오퍼레이터 확인이 반드시 필요한지 (API 레벨 강제)
- 메타데이터 체크리스트가 metadata.json + manifest.json 실제 존재 여부를 확인하는지
- 5초 자동 재생이 `autoPlay` 속성이 아닌 JS로 구현됐는지 (브라우저 정책 고려)

### Epic 9 - Story 9.4 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- API 레벨 확인 강제 추가
- 체크리스트 파일 존재 검증 추가
- 자동 재생 브라우저 정책 대응 수정

---

## Epic 10: Tuning, Settings & Operational Tooling

---

### Epic 10 - Story 10.1 개발
`/bmad-create-story`
설정 대시보드(LLM & 프로바이더 설정)를 구현한다.
- Settings 탭: 모델 ID, 비용 캡 등 config.yaml / .env 값 편집 및 검증 후 저장
- 활성 런 진행 중 변경: 다음 스테이지 진입 또는 새 런 시작까지 큐잉(현재 런 mid-stage 오염 방지)
- 현재 비용 사용량 대비 hard/soft 캡 시각 인디케이터

### Epic 10 - Story 10.1 코드 리뷰
`/bmad-code-review`
Story 10.1 구현 결과를 검토한다.
- 설정 변경이 config.yaml과 .env 중 올바른 파일에 저장되는지 (시크릿은 .env에만)
- 큐잉 로직이 실제로 현재 스테이지 완료를 기다리는지
- 비용 캡 인디케이터가 실시간 폴링 데이터를 사용하는지

### Epic 10 - Story 10.1 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 저장 대상 파일 분리 (시크릿 격리)
- 큐잉 로직 스테이지 완료 동기화
- 인디케이터 폴링 연결

---

### Epic 10 - Story 10.2 개발
`/bmad-create-story`
Tuning 탭(프롬프트 & 루브릭 랩)을 구현한다.
- Critic 프롬프트 편집기
- "Fast Feedback" 실행: 특정 스테이지를 10개 씬 샘플에 대해 실행
- 프롬프트 저장 시 자동 Shadow Eval 제안 (Epic 4 연계)
- 런에 프롬프트 버전 태깅(타임스탬프/short-sha) → 통계적 품질 비교
- Golden eval 러너 UI, Shadow eval 러너 UI, 픽스처 관리 (1:1 비율), calibration 뷰(kappa 트렌드)

### Epic 10 - Story 10.2 코드 리뷰
`/bmad-code-review`
Story 10.2 구현 결과를 검토한다.
- Fast Feedback이 10개 씬 샘플에만 실행되는지 (전체 런 아님)
- 프롬프트 버전이 runs 테이블에 저장되는지
- Shadow Eval 제안이 자동으로 뜨는지 (저장 후 즉시)
- Sequence 강제: Golden → Shadow → commit 순서가 UI에서 가이드되는지

### Epic 10 - Story 10.2 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- Fast Feedback 샘플 수 제한 추가
- 프롬프트 버전 runs 테이블 컬럼 추가
- Shadow Eval 자동 제안 트리거 수정
- Sequence 가이드 UI 추가

---

### Epic 10 - Story 10.3 개발
`/bmad-create-story`
데이터베이스 vacuum과 데이터 보존(소프트 아카이브)을 구현한다.
- `pipeline clean`: 설정된 보존 기간 초과 런/아티팩트 → Soft Archive (파일 삭제, DB 레코드 보존 + file path → NULL)
- 유휴 시간에 SQLite `VACUUM` 실행으로 DB 크기 최적화
- runs/segments 행은 영구 보존 (NFR-O2), 아티팩트 파일만 정리

### Epic 10 - Story 10.3 코드 리뷰
`/bmad-code-review`
Story 10.3 구현 결과를 검토한다.
- Soft Archive가 DB 레코드를 삭제하지 않고 파일 참조만 NULL로 설정하는지
- VACUUM이 파이프라인 활성 실행 중에 실행되지 않는지
- 보존 기간이 설정에서 읽히는지

### Epic 10 - Story 10.3 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- 레코드 삭제 로직 → NULL 업데이트로 수정
- VACUUM 실행 조건 수정
- 보존 기간 설정화

---

### Epic 10 - Story 10.4 개발
`/bmad-create-story`
Golden/Shadow CI 품질 게이트(Epic 1 CI 확장)를 구현한다.
- `internal/pipeline/agents/critic*` 또는 `testdata/golden/` 변경 PR 시 CI 자동 실행
- Golden eval: 탐지율 ≥ 80%
- Shadow eval: 최근 N=10 통과 씬에서 거짓 거부 회귀 제로
- 실패 시 "Failed Scenes Summary" diff 포함 CI 리포트 생성
- Epic 1 Story 1.7 CI에 추가 gate로 연결

### Epic 10 - Story 10.4 코드 리뷰
`/bmad-code-review`
Story 10.4 구현 결과를 검토한다.
- CI 트리거 조건이 `critic*` 파일과 `testdata/golden/` 경로 변경에 정확히 반응하는지
- 80% 탐지율 게이트가 분수 반올림 없이 정확한지
- 실패 시 artifact로 diff 파일이 생성되는지
- Epic 1 jobs와의 의존성 순서가 올바른지

### Epic 10 - Story 10.4 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- CI 트리거 경로 패턴 수정
- 탐지율 계산 수정
- 실패 artifact 생성 추가
- job 의존성 순서 수정

---

### Epic 10 - Story 10.5 개발
`/bmad-create-story`
JSON 데이터 내보내기(FR44)를 구현한다.
- `pipeline export --run-id scp-049-run-3 --type decisions`: `{output_dir}/{run-id}/export/decisions.json`, 버전 envelope `{"version":1,"data":[...]}`, 각 행에 superseded_by 포함
- `pipeline export --run-id scp-049-run-3 --type artifacts`: 아티팩트 메타데이터(상대 경로)
- `--format csv`: 동일 데이터 CSV 형식

### Epic 10 - Story 10.5 코드 리뷰
`/bmad-code-review`
Story 10.5 구현 결과를 검토한다.
- JSON envelope의 `version` 필드가 정수 1인지
- 아티팩트 경로가 절대 경로가 아닌 런 출력 디렉토리 기준 상대 경로인지
- CSV 출력 헤더 컬럼이 JSON 필드명과 일치하는지
- `superseded_by` null 값이 JSON에서 null로, CSV에서 빈 문자열로 처리되는지

### Epic 10 - Story 10.5 리뷰 반영
코드 리뷰에서 지적된 사항을 수정한다.
- `version` 타입 수정 (int)
- 경로 상대화 수정
- CSV 헤더 일치 수정
- null 처리 수정

---
*총 10 에픽, 48 스토리, 144 프롬프트*
