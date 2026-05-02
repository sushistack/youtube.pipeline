
# Pending 상태 캐시 패널 + drop_caches 옵션 추가

## 배경

`youtube.pipeline` 저장소 (Go 백엔드 + React/TS 프론트). Phase A의 결정론적
agent들(researcher, structurer)은 출력을 디스크 캐시(`research_cache.json`,
`structure_cache.json`)에 저장하고, `tryLoadCache`가 다음 실행에서 이를 재사용함.

현재는 캐시를 무효화하려면 운영자가 터미널에서 `rm -rf {outputDir}/{run_id}/*.json`을
직접 입력해야 함. 1인 운영자(Jay)가 결정론 로직을 자주 만지는 워크플로에서 UX
불일치 + 오타 리스크가 있음. 나머지 운영(stepper rewind, advance, scenario
review)은 모두 UI 기반인데 캐시 처리만 터미널인 상태.

`tryLoadCache`의 SourceVersion 자동 무효화는 이미 들어가 있음
(`internal/pipeline/phase_a.go`의 `tryLoadCache`). 이 작업은 그것과 보완 관계 —
UI에서 명시적 토글로 캐시를 즉석에서 무효화하는 경로를 제공.

## 목표

Run이 `pending` 상태일 때 (Start run 버튼이 보이는 화면), 해당 run에 어떤
캐시가 디스크에 있는지 시각화하고, 각 캐시를 "유지" 또는 "삭제"로 토글한 뒤
Start run 클릭 시 선택대로 처리한 후 Phase A를 시작.

## 스펙

### 1. 백엔드 — 신규 엔드포인트 `GET /api/runs/{id}/cache`

응답:

```json
{
  "caches": [
    {
      "stage": "research",
      "path": "scp-049-run-1/research_cache.json",
      "size_bytes": 1234,
      "modified_at": "2026-05-02T10:23:45Z",
      "source_version": "v1-deterministic"
    },
    {
      "stage": "structure",
      "path": "scp-049-run-1/structure_cache.json",
      "size_bytes": 567,
      "modified_at": "2026-05-02T10:23:46Z",
      "source_version": "v1-deterministic"
    }
  ]
}
```

요구사항:

- Stage 종류: `research`, `structure` (필수). `scenario` (선택 — `scenario.json`
  존재 시 포함)
- `{outputDir}/{run_id}/` 아래에서 파일 존재 시에만 항목 추가
- `source_version`은 JSON 부분 unmarshal로 추출 (실패 시 빈 문자열)
- Run이 존재하지 않으면 404, 정상이면 200 + 빈 배열도 허용

위치 가이드:

- 라우트 등록: `internal/api/routes.go:46` 근처에 scene 라우트들과 함께
- 핸들러: 신규 또는 적절한 기존 핸들러에 메서드 추가 (`SceneHandler` 또는
  `RunHandler` 중 의미상 가까운 곳)
- 디렉터리 해석은 기존 `internal/service/scene_service.go`의 `s.outputDir`
  패턴 참조

### 2. 백엔드 — `POST /api/runs/{id}/advance` 바디 확장

기존 동작 보존하면서 옵션 바디 추가:

```json
{ "drop_caches": ["research", "structure"] }
```

- 빈 바디 또는 `drop_caches` 누락 시 기존 동작 그대로
- 들어온 stage들의 캐시 파일을 `engine.Advance` 호출 **이전**에 삭제
- 알 수 없는 stage 이름은 400 ValidationError로 거부
- 파일이 없는 경우 에러 아님 (idempotent delete)

핸들러: `internal/api/handler_run.go:377` `Advance()` 메서드.

### 3. 프론트엔드 — Pending 카드의 캐시 패널

위치: `web/src/components/shells/ProductionShell.tsx:217-274`
`renderPendingDetail()` 내부, "Run created. It has not started yet..." 안내문과
"Start run" 버튼 사이.

요구사항:

- 신규 hook `useRunCache(run_id)` (`web/src/hooks/useRunCache.ts`) — React Query
  로 `GET /api/runs/{id}/cache` 호출. 기존 `useRunScenes` 패턴 참조
- 캐시 0개면 섹션 자체를 렌더링하지 않음 (현재 깔끔한 화면 유지)
- 캐시 1개 이상 시:
  - 섹션 헤더: "Cached artifacts" (eyebrow 스타일)
  - 각 캐시 행: 체크박스 (default checked = "유지"), stage 라벨,
    `source_version`, `modified_at` (사람이 읽을 수 있는 상대 또는 절대 시각)
  - 체크 해제 = "삭제 후 재생성"
- "Start run" 클릭 시 체크 해제된 stage들을 `drop_caches`로 advance 호출

`apiClient.ts`의 `advanceRun` 시그니처 확장:

```ts
export function advanceRun(
  run_id: string,
  options?: { drop_caches?: string[] },
)
```

- 옵션이 있으면 바디에 포함, 없으면 빈 바디 (기존 호출 호환)

### 4. 테스트

**백엔드:**

- 신규 cache 핸들러: 캐시 존재 / 부재 / run-not-found 케이스
- Advance 핸들러: `drop_caches` 동작 — 파일이 실제로 삭제되는지, 알 수 없는
  stage는 거절되는지, 빈 바디는 기존 동작인지
- 기존 패턴은 `internal/api/handler_run_test.go` 참고 (`testutil`만 사용,
  testify 금지)

**프론트엔드:**

- `web/src/components/shells/ProductionShell.test.tsx:485`의 pending state
  테스트를 확장 또는 별건으로:
  - 캐시 0개: 섹션이 안 보임
  - 캐시 N개: 행 N개 렌더링, 체크 토글, Start run 클릭 시 올바른 `drop_caches`로
    호출되는지 검증
- MSW 또는 기존 mock 패턴 따라가기

## 범위 외 (절대 손대지 말 것)

- 실행 중(running) 상태의 Restart 버튼 — 별도 작업
- `tryLoadCache` 또는 `SourceVersionV1` 변경 — 이미 완료됨
- 결정론 agent 로직 자체 (`researcher.go`, `structurer.go`) — 건드리지 않음
- Schema 파일 (`testdata/contracts/*.schema.json`) 버전 bump — 별건

## 컨벤션

- Go: `writeJSON`, `writeError`, `writeDomainError` 헬퍼 + `domain.Err*`
  센티넬 사용. 핸들러 테스트는 `httptest` + 테이블 드리븐
- TS: snake_case 변수, 기존 React Query 키 패턴 (`queryKeys.runs.*`),
  strict TypeScript
- 모든 변경은 단일 논리적 커밋으로 묶일 수 있게 (out-of-scope 파일 편집 금지)

## 검증

- `go test ./internal/api/... ./internal/pipeline/...` 통과
- 프론트 테스트 통과 (프로젝트 표준 명령)
- 수동: 프로덕션 셸 띄워서 pending run에 캐시가 있을 때 패널이 보이고, 체크
  해제 후 Start run 시 해당 캐시가 삭제되고 새로 생성되는지 확인
