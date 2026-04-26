# Post-Epic Quality Prompts

Epic 1~10 완료 후 "실제로 동작하는지 / 뭐가 깨졌는지 / UI 포함 E2E 커버 어디까지 할지" 확인용 프롬프트 순서.
각 스텝은 이전 스텝 산출물을 입력으로 쓰므로 **순서대로** 실행할 것.

---

## Step 1 — 현재 상태 지도 그리기 (retrospective)

**목적**: epic 10개 돌아보며 "done / deferred / 미검증"을 분리. deferred-work.md와 실제 코드 상태를 대조해서 risk map 생성.

**프롬프트**:

```
/bmad-retrospective

Epic 1~10 전체에 대한 post-epic 리뷰를 진행한다.
아래 입력을 참고해서 "실제 동작 상태 맵"을 만들어줘:

- _bmad-output/planning-artifacts/epics.md — 원래 계획된 scope
- _bmad-output/implementation-artifacts/deferred-work.md — epic별 미뤄둔 항목 (420줄, 전 epic 포함)
- 실제 코드 상태 (src/, apps/, pkg/ 등 실제 구현)
- 최근 커밋 히스토리 (git log --oneline -50)

산출물:
1. Epic별로 "구현 완료 + 테스트됨" / "구현됐으나 미검증" / "deferred로 빠진 항목" / "E2E에서 아직 안 돌려본 부분" 4분류
2. 전체 파이프라인 E2E가 현재 돌아갈지에 대한 리스크 평가 (high/medium/low)
3. UI(React SPA) 쪽 미테스트 영역 목록
4. 다음 단계(테스트 전략 수립)에서 Test Architect가 집중해야 할 "tea-focus areas" 섹션 — Step 2 입력으로 쓸 것
```

**기대 산출물 파일**: `_bmad-output/retrospective/epic-1-10-status-map.md` (또는 retrospective skill이 만드는 위치)

---

## Step 2 — Test Architect 전략 수립 (bmad-tea)

**목적**: Step 1 risk map을 Murat(Test Architect)에게 전달해서 quality strategy 수립. 어디를 어떤 레벨(unit/integration/E2E/smoke)로 테스트할지, NFR 어디서 걸리는지.

**Step 1 완료 후 프롬프트**:

```
/bmad-tea

Murat, epic 1~10 전체 구현이 완료됐는데 deferred 항목도 많고 UI E2E 커버가 부족한 상태.
Step 1 retrospective 산출물을 기반으로 quality strategy를 수립해줘.

입력:
- Step 1 산출물 (retrospective risk map + tea-focus areas)
- _bmad-output/implementation-artifacts/deferred-work.md
- _bmad-output/planning-artifacts/architecture.md (NFR 제약 확인용)
- 현재 테스트 코드 상태 (testdata/, *_test.go, apps/*/tests/)

원하는 답:
1. 파이프라인 E2E smoke 범위 제안 (golden path: 스크립트 → 씬 → 이미지 → 오디오 → 비디오 → 업로드 전 gate까지)
2. UI E2E 범위 제안 (critical user flow — dashboard → scenario inspector → review → approve/reject)
3. deferred-work.md 항목 중 "quality gate 통과 전 반드시 처리해야 할 것" 필터링 (우선순위 P0/P1/P2)
4. NFR 리스크 (퍼포먼스, 안정성, 데이터 무결성) 어디서 걸릴 가능성이 높은지
5. 다음 스텝에서 쓸 구체적 test design input — Step 3용
```

---

## Step 3 — E2E Test Design (bmad-testarch-test-design)

**목적**: Murat이 정한 E2E 범위를 실행 가능한 테스트 계획으로 전환. 파이프라인 전체 + UI critical flow.

**Step 2 완료 후 프롬프트**:

```
/bmad-testarch-test-design

Step 2 (bmad-tea)에서 정한 E2E 범위를 실제 테스트 케이스로 설계해줘.

입력:
- Step 2 quality strategy 산출물
- 현재 Playwright 인프라 (Epic 6.5에서 세팅된 것)
- testdata/golden/ 구조

원하는 답:
1. 파이프라인 E2E 테스트 케이스 목록 (given/when/then, fixture, 예상 실행 시간)
2. UI E2E 테스트 케이스 목록 (Playwright 기반, critical flow 위주)
3. 각 케이스의 우선순위와 예상 구현 공수
4. 테스트 데이터 전략 (golden fixture, mock 경계, 외부 API는 어떻게)
```

---

## Step 3.5 — CI hygiene unblock (CP-3 + CP-4)

**목적**: Step 3 test design 에서 P0 11개 시나리오가 CI 에서 실제로 녹색이 되려면 먼저 풀어야 하는 CI plumbing 2건. Step 4 돌리기 전 필수. 반나절 이내로 끝나는 작은 변경만 모아놓음.

**왜 Step 4 직전에 필요한가**:

- CP-3 미해결이면 `test-e2e` 가 `continue-on-error: true` 로 실패를 숨김 + 루트 `e2e/smoke.spec.ts` 가 `test.todo()` → Step 4에서 만든 UI E2E 10개가 CI 신호로 잡히지 않음
- CP-4 미해결이면 `test-go` 에 ffmpeg 이 없어 Phase C 관련 테스트(SMOKE-02 등)가 조용히 skip 됨 + `go-version: '1.25.7'` 은 존재하지 않는 버전이라 `actions/setup-go` 가 임의로 resolve 함

**Step 3 완료 후 프롬프트**:

```
/bmad-quick-dev

Step 3 test design 의 §6.6 CI Wiring Prerequisites 중 CP-3 과 CP-4 를 한 커밋(또는 2개 인접 커밋)으로 처리해줘. Step 4 (`/bmad-qa-generate-e2e-tests`) 를 돌리기 전 반드시 필요한 CI plumbing 이다.

입력:
- _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md §6.6
- _bmad-output/test-artifacts/quality-strategy-2026-04-25.md §5.5
- .github/workflows/ci.yml
- e2e/smoke.spec.ts (루트, test.todo() 상태)
- web/e2e/smoke.spec.ts (실제로 동작하는 spec 들)

변경 범위 (이 파일들만, 다른 코드 건드리지 말 것):

1. CP-3 — test-e2e job 정리
   - .github/workflows/ci.yml line 46 `continue-on-error: true` 제거
   - .github/workflows/ci.yml 의 test-e2e job 이 루트 e2e/ 대신 web/e2e/ 를 실행하도록 repoint
     (cd e2e → cd web, cache-dependency-path 와 install 경로 포함)
   - 루트 e2e/ 디렉토리는 제거 OR 남겨둘지 판단 — Jay 메모리 feedback_commit_scope.md 에 따라, 이번 커밋 범위는 CI hygiene 만이므로 루트 e2e/ 는 삭제하지 말고 별도 cleanup 커밋으로 미루기
     (단 루트 e2e/smoke.spec.ts 는 test.todo() 한 줄이라 영향 없음)

2. CP-4 — test-go job 에 ffmpeg + go-version 수정
   - .github/workflows/ci.yml test-go job 에 `Run Go tests` 스텝 직전에 apt-get install ffmpeg 스텝 추가:
       - name: Install ffmpeg
         run: sudo apt-get update && sudo apt-get install -y ffmpeg
   - ci.yml 전체에서 `go-version: '1.25.7'` 3군데 (line 16, 116, 138) 를 실제 존재하는 최신 1.25.x 로 교체
     (actions/setup-go 는 semver 부분 매칭 지원 — `go-version: '1.25'` 로 쓰면 최신 1.25.x 자동 선택되어 안전. 확정된 패치 버전이 필요하면 https://go.dev/dl/ 확인해서 정확한 1.25.x 핀)

검증:
- 로컬에서 `yamllint .github/workflows/ci.yml` 또는 GitHub Actions syntax validator 로 문법 확인
- 커밋 전 ci.yml diff 를 Jay 에게 보여주고 승인 받기 (CI 변경은 blast radius 가 커서 확인 필요)
- 가능하면 test-go job 이 로컬 ffmpeg 설치된 환경에서 `go test ./internal/pipeline/...` 이 skip 없이 도는지 한 번만 검증

원하는 답:
1. ci.yml 수정 diff (두 CP 합쳐서 한 번에 또는 2개 인접 커밋)
2. 루트 e2e/ 폴더 cleanup 은 별도 follow-up 으로 미뤘다는 메모
3. 커밋 메시지 초안 — Jay 의 커밋 스타일(간결, 변경 이유 위주) 따라서
4. Step 4 (`/bmad-qa-generate-e2e-tests`) 에 넘어가도 되는지 GO/NO-GO
```

**기대 산출물**:
- `.github/workflows/ci.yml` 수정 (1 파일, diff 최소)
- 커밋 1~2개 (CP-3 + CP-4 scope-clean)
- Step 4 진행 GO 사인

---

## Step 4 — UI E2E 실구현 — 준비된 7개 (bmad-qa-generate-e2e-tests)

**목적**: Step 3 test design 의 UI E2E 10개 중, Step 3.5 이후 **블로커가 풀린 7개**만 Playwright 파일로 생성. 나머지 3개(UI-E2E-05/06/07)는 각 블로커 해제 후 Step 6 에서 백필.

**범위**:

| 대상 | 우선순위 | 블로커 상태 |
|------|----------|-------------|
| UI-E2E-01 SPA Smoke | P0 | Step 3.5 CP-3 완료 후 가능 |
| UI-E2E-02 Inline Narration Edit | P0 | 없음 |
| UI-E2E-03 Character → Vision | P0 | 없음 |
| UI-E2E-04 Batch Review Chord | P0 | 없음 |
| UI-E2E-08 Settings | P1 | 없음 |
| UI-E2E-09 Inventory Search | P1 | 없음 |
| UI-E2E-10 FailureBanner Resume | P1 | 없음 |

**제외 (Step 6 백필 대상)**: UI-E2E-05 (AI-3 `>=` 통일 대기), UI-E2E-06 (AI-4 FR23 hard gate 대기), UI-E2E-07 (CP-5 Story 10-2 FULL 대기)

**Step 3.5 완료 후 프롬프트**:

```
/bmad-qa-generate-e2e-tests

Step 3 test design §5 의 UI E2E 중 블로커 없는 7개(UI-E2E-01/02/03/04/08/09/10)를 Playwright 파일로 생성해줘. 나머지 3개(05/06/07)는 블로커 해제 후 Step 6 에서 별도로 처리하므로 이번 스코프에 포함 금지.

입력:
- _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md §5 (UI E2E 케이스별 Given/When/Then + fixture + 공수)
- web/playwright.config.ts (Chromium-only, port 4173, serve:e2e webServer)
- web/e2e/smoke.spec.ts, web/e2e/new-run-creation.spec.ts (기존 패턴 — pageerror/console 가드, getByRole, Continue to workspace 온보딩 처리)

원하는 답:
1. 각 케이스별 .spec.ts (7개 파일; 대략 web/e2e/ 루트)
2. 공용 페이지오브젝트 4개 (web/e2e/po/ 아래)
   - ProductionShell (01, 02, 03, 06 대비 placeholder, 09, 10)
   - BatchReviewShell (04; 05 대비 placeholder 지양 — 해당 케이스 미생성)
   - TuningShell (07 은 이번 제외 — 스캐폴드 불필요)
   - SettingsShell (08)
   실제 사용 안 하는 페이지오브젝트는 만들지 말 것
3. Per-test zustand reset fixture (deferred 6-5 대응)
4. 로컬 실행 명령어 (`cd web && npx playwright test`) + CI 연동 (.github/workflows/ci.yml test-e2e job 은 Step 3.5 에서 이미 cd web 으로 repoint 됨)

범위 제한:
- 제외된 UI-E2E-05/06/07 를 위한 스캐폴드/TODO 코멘트/테스트 skip 파일 생성 금지 (scope leak 방지)
- Step 3 test design 범위 밖의 flow 는 절대 건드리지 말 것
```

**기대 산출물**:
- `web/e2e/*.spec.ts` 7개 (기존 `smoke.spec.ts`/`new-run-creation.spec.ts` 와 공존)
- `web/e2e/po/` 3개 페이지오브젝트 (ProductionShell/BatchReviewShell/SettingsShell)
- 로컬에서 `cd web && npx playwright test` 7개 모두 녹색

**✅ 실행 완료 (2026-04-25)**:

- **커밋**:
  - `5faa267` epic 1-10 UI E2E Round 1 — UI-E2E-01/02/03/04/08/09/10 specs (메인 산출물)
  - `78f7bf6` test setup — URL|Request typeguard fix for tsc -b (선행 — webServer build 가 막혀서 e2e 실행 자체 불가했음)
- **검증**: `cd web && npx playwright test` → **14/15 green**. 새로 만든 7 spec (11 tests) 전부 통과.
- **본 Step 실행 중 발견된 사전 결함 fix 동봉**:
  - `78f98fa` TimelineView empty-state heading wrapper — 빈 DB 에서 `<h2>Timeline</h2>` 가 안 렌더되어 `smoke.spec.ts:45` 가 항상 red 였음. 본문에서 4개 분기 모두 헤더 wrapper 적용.
  - `31cec0b` Playwright DB wipe on boot + `workers=1` — Playwright 워커 간 `.tmp/playwright/pipeline.db` 공유로 인해 prior 세션 run 이 GET /api/runs 에 잔존, fallback selection race 유발. webServer 부팅 시 DB rm + 단일 워커 강제로 격리.
- **잔여 결함 (Step 5 Story 11-6 으로 트래킹)**:
  - `new-run-creation.spec.ts` (기존 spec) — `Sidebar.handleNewRunSuccess` 의 `set_search_params` 가 dialog close + invalidateQueries 사이에서 reactive update race. main 에서도 동일 fail. **production 코드 race 라 test infra 가 아닌 별도 스토리로 처리**.

---

## Step 4.5 — 파이프라인 E2E 실구현 — 준비된 4개 (bmad-quick-dev)

**목적**: Step 3 test design 의 파이프라인 E2E 8개 중 **블로커 없는 4개**(Go integration)를 구현. `/bmad-qa-generate-e2e-tests` 는 Playwright/UI 전용이라, Go 통합 테스트는 `/bmad-quick-dev` 로 처리하는 게 정석.

**진행 가능 시점**: Step 4 완료 직후 즉시. CP-4 ffmpeg 은 커밋 `7e07c29` 에서 landed, Round 1 의 DB 격리/`workers=1` fix(`31cec0b`) 도 e2e infra 안정화 완료. 추가 블로커 없음.

**추정 (Jay 솔로)**: **10~16h ≈ 1.5~2 dev-day**.
- 실제 테스트 함수 작성: 7~11h (SMOKE-02 3-5h + SMOKE-04 2-3h + SMOKE-07 2-3h + SMOKE-08 2-3h)
- `testdata/e2e/scp-049-seed/` fixture 패키지: 3~5h ← **fixture 가 거의 절반 비용**. 한 번 만들면 SMOKE-01/03/05/06 (Step 6 백필) 에서도 그대로 재사용되는 자산이라 선투자 가치는 있음.

**권장 시작 순서**: **SMOKE-02 first** (test-design §13 의 "10× faster than SMOKE-01, orthogonal to CP-1" 권장 — fixture 만들면서 곧장 검증 가능). 이후 SMOKE-08 → SMOKE-07 → SMOKE-04 순. SMOKE-04 의 CP-1 의존 여부는 시작 직전에 재검증할 것.

**범위**:

| 대상 | 우선순위 | 파일 | 블로커 |
|------|----------|------|--------|
| SMOKE-02 Phase A→B→C Handoff | P0 | `internal/pipeline/smoke02_phase_handoff_test.go` (신규) | Step 3.5 CP-4 완료 후 가능 |
| SMOKE-04 Cost Cap Circuit Breaker | P0 | `internal/pipeline/smoke04_cost_cap_test.go` (신규) | 없음 |
| SMOKE-07 Compliance Gate | P1 | `internal/server/handler_ack_test.go` (기존 파일 확장) | 없음 |
| SMOKE-08 Export Round-Trip + CSV Guard | P1 | `internal/pipeline/smoke08_export_test.go` (신규) | 없음 |

**제외 (Step 6 백필 대상)**: SMOKE-01/03 (CP-1 Engine.Advance 대기), SMOKE-05 (Phase C 하드닝 대기), SMOKE-06 (AI-5 + CP-5 대기)

**Step 4 완료 후 (병렬 가능) 프롬프트**:

```
/bmad-quick-dev

Step 3 test design §4 의 파이프라인 E2E 8개 중 블로커 없는 4개(SMOKE-02/04/07/08)를 Go 통합 테스트로 구현해줘. 나머지 4개(01/03/05/06)는 Step 5 블로커 해제 후 Step 6 에서 처리하므로 이번 스코프에 포함 금지.

입력:
- _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md §4 (SMOKE-02/04/07/08 각각 Given/When/Then + fixture)
- internal/pipeline/e2e_test.go (기존 mockTextGenerator/mockImageGenerator/mockTTSSynthesizer + testutil.BlockExternalHTTP 재사용)
- testdata/golden/eval/ (기존 골든; 불변; --dry-run 강제)

원하는 답:
1. SMOKE-02: internal/pipeline/smoke02_phase_handoff_test.go 신규
   - testdata/e2e/scp-049-seed/scenario.json 선행 생성 필요 (Step 3 §6.1 canonical seed)
   - phase_b.Run + phase_c.Assemble 실행, 실제 ffmpeg 사용
   - 런타임 ≤ 15초
2. SMOKE-04: internal/pipeline/smoke04_cost_cap_test.go 신규
   - fixture.SeedRunAtCost 헬퍼 (신규 내부 헬퍼) + errors.Is(err, ErrCostCapExceeded)
   - 런타임 ≤ 5초
3. SMOKE-07: internal/server/handler_ack_test.go 확장 (기존 파일 수정)
   - 사전 POST /api/runs/{id}/upload → 409 어설션
   - POST /api/runs/{id}/ack-metadata → runs.stage=ready_for_upload
   - 런타임 ≤ 3초
4. SMOKE-08: internal/pipeline/smoke08_export_test.go 신규
   - 주입 payload: 씬 나레이션 '=SUM(A1)' 포함
   - 동일 export 2회 → sha256 동일 (idempotent) + CSV 인젝션 가드
   - 런타임 ≤ 5초

공용 인프라:
- testdata/e2e/scp-049-seed/ (SMOKE-02 에서 사용; 이번 스코프에서 생성)
  - raw.txt, scenario.json, responses/images/*.png (256x256 solid color), responses/tts/*.wav (1초 무음), expected-manifest.json
  - 약 5MB; .gitignore 하지 말 것
- internal/pipeline/fi/ 는 SMOKE-05 전용이므로 이번 스코프에서 제외

범위 제한:
- 제외된 SMOKE-01/03/05/06 관련 파일/스캐폴드 절대 생성 금지
- Engine.Advance 근처 코드 건드리지 말 것 (CP-1 스토리 영역)
- go-version/ffmpeg CI 변경은 Step 3.5 에서 완료됐으므로 이 프롬프트에서 중복 변경 금지

검증:
- 로컬에서 `CGO_ENABLED=0 go test ./internal/pipeline/... ./internal/server/... -run 'SMOKE_0[2478]|HandlerAck' -count=1 -timeout=60s` 4개 모두 녹색
- testutil.BlockExternalHTTP 로 외부 HTTP 시도 시 panic 확인
```

**기대 산출물**:
- Go 테스트 파일 3개 신규 + 1개 확장
- `testdata/e2e/scp-049-seed/` 시드 번들
- `go test ./internal/pipeline/... ./internal/server/...` 에서 4개 녹색

---

## Step 5 — 블로커 해제 스토리 5건 (bmad-create-story × 5)

**목적**: Step 3 test design §10 Dependencies 표의 블로커 5건을 stories/ 에 정식 스토리로 생성. 각 스토리는 Jay 가 이후 `/bmad-dev-story` 로 개별 처리. 이 스텝 자체는 테스트를 만들지 않고 **"블로커를 풀 수 있는 구현 스토리의 준비"** 를 한다.

**생성 대상**:

| 스토리 후보 | 풀리는 블로커 | 연쇄 효과 (Step 6 에서 해제될 E2E) |
|-------------|---------------|-------------------------------------|
| Story 11-1 Engine.Advance 완전 와이어링 | CP-1 | SMOKE-01, SMOKE-03 + `TestE2E_FullPipeline` unskip |
| Story 11-2 DeepSeek 어댑터 + Tuning 서피스 FULL | CP-5 Story 10-2 (+ AI-5 `LoadShadowInput` 포함) | UI-E2E-07, SMOKE-06 |
| Story 11-3 RetryExhausted `>=` 통일 (3-site) | AI-3 | UI-E2E-05 |
| Story 11-4 FR23 Compliance Gate 핸들러 | AI-4 | UI-E2E-06 |
| Story 11-5 Phase C 하드닝 (metadata+manifest 원자성, xfade<0.5s 가드, probe=0 가드) | R-04/R-09/R-10 | SMOKE-05 |
| Story 11-6 ProductionShell URL race fix (Sidebar `handleNewRunSuccess` 의 `set_search_params` 가 dialog close + invalidateQueries 사이에서 reactive update race — 새 run 생성 직후 URL `?run=` 이 셋되지 않아 fallback selection 으로 빠짐) | (Round 1 검증에서 발견된 사전 결함) | `new-run-creation.spec.ts` 회복 (현재 14/15 → 15/15) |

**프롬프트 (5회 반복, 스토리 단위로)**:

```
/bmad-create-story

Step 3 test design §10 Dependencies 와 Step 2 quality strategy §3 P0 목록 기반으로 아래 1개 블로커를 해제하는 구현 스토리를 만들어줘.

대상: [Story 11-X — 위 표에서 하나 선택]

입력:
- _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md §10, §12
- _bmad-output/test-artifacts/quality-strategy-2026-04-25.md §3 P0
- _bmad-output/implementation-artifacts/deferred-work.md (해당 스토리의 deferred 항목)
- _bmad-output/implementation-artifacts/epic-1-10-retro-2026-04-24.md

원하는 답:
1. acceptance criteria (해당 블로커 해제의 verification criteria 그대로 반영)
2. 영향받는 파일 목록
3. "이 스토리가 완료되면 언블록되는 E2E 시나리오" 명시 (Step 6 입력)
4. 예상 공수 (Jay 솔로 기준)
```

**진행 순서 추천** (공수 · 위험도 고려):
1. **Story 11-6 (URL race, 가장 작음 — 클라이언트 한 곳 fix)** → 11-3 (AI-3) → 11-4 (AI-4) → 11-5 (Phase C 하드닝) → 11-1 (CP-1) → 11-2 (CP-5, 가장 큼)
2. 각 스토리 완료 직후 Step 6 에서 해당 E2E 백필
3. Jay 의 커밋 scope 원칙 (feedback_commit_scope.md) 에 따라 **스토리당 1 커밋**, 섞지 말 것

**기대 산출물**:
- `_bmad-output/implementation-artifacts/stories/11-1-engine-advance.md` 등 5개 스토리 파일
- `_bmad-output/implementation-artifacts/sprint-status.yaml` 에 Epic 11 항목 5개 추가 (tracking)

---

## Step 6 — 남은 E2E 백필 (Step 4 / Step 4.5 재실행)

**목적**: Step 5 의 블로커 스토리가 하나씩 랜딩될 때마다 해제된 E2E 케이스를 백필. Step 4/4.5 의 동일 프롬프트를 **스코프만 바꿔** 재실행.

**백필 매핑**:

| 랜딩된 스토리 | 재실행할 스텝 | 생성 대상 |
|---------------|---------------|-----------|
| Story 11-6 (URL race) | Step 4 재실행 불요 — 기존 `new-run-creation.spec.ts` 자동 회복; `cd web && npx playwright test` 로 15/15 확인만 | (신규 spec 없음, 기존 spec 회복) |
| Story 11-3 (AI-3) | Step 4 재실행 | UI-E2E-05 Retry Exhausted |
| Story 11-4 (AI-4) | Step 4 재실행 | UI-E2E-06 ComplianceGate Ack |
| Story 11-5 (Phase C 하드닝) | Step 4.5 재실행 | SMOKE-05 Metadata+Manifest 원자성 |
| Story 11-1 (CP-1) | Step 4.5 재실행 | SMOKE-01 Full Pipeline, SMOKE-03 Resume + `TestE2E_FullPipeline` unskip |
| Story 11-2 (CP-5) | Step 4 + Step 4.5 재실행 | UI-E2E-07 Tuning, SMOKE-06 Shadow Live |

**프롬프트 템플릿** (랜딩 건마다):

```
/bmad-qa-generate-e2e-tests   (UI 대상인 경우)
또는
/bmad-quick-dev                (파이프라인 대상인 경우)

Step 5 Story 11-X 가 완료되어 블로커가 해제됐다. 해당 블로커에 걸려있던 E2E 케이스를 백필해줘.

대상: [UI-E2E-0X 또는 SMOKE-0X — 위 매핑 표 참고]

입력:
- _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md §4/§5 해당 케이스
- Step 5 Story 11-X 구현 diff (git log 로 확인)
- 이미 만들어진 Step 4/4.5 산출물 (page object, 시드, fixture 재사용)

원하는 답:
1. 해당 케이스 .spec.ts 또는 _test.go 추가
2. 공용 page object / fixture 재사용 (신규 생성 지양)
3. 로컬 녹색 확인

범위 제한:
- 이번 백필 대상 외 다른 케이스 건드리지 말 것
- Step 4/4.5 에서 이미 만들어진 파일 수정은 strict하게 필요한 경우만
```

**기대 산출물 (최종)**: Step 6 이 전부 끝난 시점에 18개 E2E 모두 CI 녹색.

---

## Step 7 — Traceability + ship gate (bmad-testarch-trace)

**목적**: 커버리지 gap 최종 확인 + V1 ship 결정. Step 1~6 다 돌고 나서 "요구사항 대비 테스트 매핑" 완성 + quality gate 판정.

**Step 6 최종 백필 완료 후 프롬프트**:

```
/bmad-testarch-trace

Epic 1~11 전체 요구사항 대비 테스트 커버리지 traceability matrix 를 만들고 V1 ship gate 판정을 내려줘.

입력:
- _bmad-output/planning-artifacts/prd.md
- _bmad-output/planning-artifacts/epics.md
- _bmad-output/test-artifacts/quality-strategy-2026-04-25.md §5.7 (traceability seed)
- _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md §8 (quality gate criteria)
- Step 4/4.5/6 실제 테스트 파일 전체
- deferred-work.md 최종 상태

원하는 답:
1. FR1–FR53 → 테스트 매핑 매트릭스 (SMOKE-01..08, UI-E2E-01..10 별)
2. NFR-C1/C2/C3, NFR-R1..R4, NFR-P3/P4, NFR-A1/A2, NFR-T1 → 테스트 매핑
3. 커버리지 gap 목록 (요구사항 있으나 테스트 없음)
4. 최종 quality gate 판정: **PASS / CONCERNS / FAIL / WAIVED** (Step 3 §8 기준)
   - PASS: 11 P0 + 7 P1 모두 녹색 + P0 deferred 12건 모두 closed
   - CONCERNS: P0 녹색이지만 일부 P1 OPEN
   - FAIL: P0 중 하나라도 red OR P0 deferred 미해결
   - WAIVED: Jay 가 expiry 명시 일괄 waiver
5. 만약 CONCERNS/FAIL 이면 ship 전 반드시 처리할 item 리스트
```

**기대 산출물**: `_bmad-output/test-artifacts/traceability-matrix-v1.md` + ship decision.

---

## Step 8a — Dogfood pass (수동, Jay 1인)

**목적**: Step 7 PASS 는 *기술적* ship gate. Step 8a 는 *사용성* ship gate. CI 녹색이랑 별개로 "Jay 가 운영자 입장에서 실제로 SCP 영상을 처음부터 끝까지 만들 수 있는가" 검증.

**왜 자동화 못 하는가**: Dogfood 의 가치는 "뭘 찾는지 모르는 상태에서 발견" 하는 데 있음. assertion 으로 미리 적을 수 있는 건 이미 fix 끝난 상태. 자동화 = 이미 아는 걸 검증, dogfood = 모르는 걸 발견. 카테고리가 다름. (자동화로 보강 가능한 부분은 Step 8b 에서 별도 처리.)

**진행 시점**: Step 7 PASS 직후. 두 사이클 권장.

**1차 dogfood run (반나절)**:

```
실제 SCP raw 텍스트 1편 골라서 (예: 가장 짧은 SCP-049 정도) UI 만으로 끝까지 가본다.

코스:
1. New Run 다이얼로그 → 시나리오 생성
2. ProductionShell 에서 씬별 검토·인라인 편집
3. 캐릭터 → Vision 매핑
4. Batch Review (이미지/오디오 ack/reject)
5. Phase C 조립 → 메타데이터 ack
6. Compliance Gate 통과 → 업로드 직전까지

매 화면마다 자문:
- "처음 보는 사람한테 다음 액션이 뭔지 명확한가?"
- "복사·라벨이 모호하지 않은가?"
- "로딩·에러 상태가 보이는가?"
- "실제 길이 텍스트에서 레이아웃 안 깨지는가?"
- "이거 짜증나는가?"

발견 항목 전부 dogfood-checklist.md 에 기록.

```

**분류 후 처리**:

| 분류 | 정의 | 처리 |
|------|------|------|
| P0 | 다음 단계로 못 넘어가는 블로커 | 즉시 fix (Story 12-X) |
| P1 | 동작은 하지만 이상함 (복사 모호, 로딩 부재 등) | Epic 12 묶어서 처리 |
| P2 | cosmetic (정렬, 색, 마이크로 인터랙션) | 백로그 |

**2차 dogfood run** (Story 12-X 랜딩 후): 1차 발견 항목 회복 확인 + 처음 보는 거 한 번 더. 보통 두 사이클이면 "Jay 혼자 운영 가능" 수준 도달. 그래도 새로운 P0 가 나오면 3차.

**기대 산출물**:
- `_bmad-output/dogfood/dogfood-run-1-2026-MM-DD.md` (발견 항목 카탈로그)
- Epic 12 — Dogfood Findings (Story 12-1, 12-2, ... 형태로 P0/P1 처리)
- 2차 run 결과 메모

**참고**: 이 스텝은 BMad skill 로 돌리는 게 아니라 Jay 가 직접 UI 를 사용. 발견된 결함만 이후 `/bmad-quick-dev` 또는 `/bmad-create-story` 로 처리.

---

## Step 8b — Dogfood 보강 자동화 (visual regression + nightly real-data + trace 녹화)

**목적**: Step 8a 를 **대체하는 게 아니라 보강**. (1) dogfood 가 잡는 항목 중 자동화 가능한 카테고리를 선제적으로 cover, (2) dogfood 발견 항목을 회귀 테스트로 자산화하는 변환 비용을 낮춤, (3) 외부 API drift 를 사람 손 안 거치고 잡음.

**진행 시점**: Step 8a 1차 run 직후 (1차에서 어떤 종류의 결함이 잡히는지 확인 후 자동화 우선순위 결정). Step 7 와 병렬도 가능하지만, 1차 dogfood 결과를 본 뒤가 ROI 명확.

**3개 자산**:

### 8b-1 Visual regression (Playwright `toHaveScreenshot()`)

- 대상: critical 화면 4~5개 — ProductionShell scene list, BatchReview grid, Compliance Gate, Settings, Inventory
- 베이스라인: 실제 길이의 시나리오 텍스트 (짧은 한 줄 ~ 긴 단락) + 한국어/특수문자 포함 fixture 로 캡처
- 트리거: PR CI 에서 `web/e2e/visual/*.spec.ts` 실행, diff 검출 시 PR comment 로 노출
- 잡히는 결함: 실제 텍스트 길이로 인한 레이아웃 깨짐, 폰트 fallback, overflow truncation 누락 — **dogfood 발견 항목의 약 30% 선제 cover** 가 목표

### 8b-2 Nightly real-data smoke

- 대상: SMOKE-01 (Full Pipeline) 의 mock 풀린 변종 1개. 실제 DeepSeek/Qwen DashScope 호출, 실제 ffmpeg.
- 스케줄: 1회/일 cron (`/schedule` 로 별도 구성 가능)
- 정책: **flaky 허용** — CI gating 안 함, 실패 시 issue 자동 생성만. 외부 API 변동을 noise 로 묻지 않고 issue 화 하는 게 목적.
- 잡히는 결함: 프롬프트 drift, 외부 API 스키마/응답 형태 변경, rate limit 정책 변동, 실제 응답 길이/모양으로만 드러나는 파서 결함

### 8b-3 Playwright trace 녹화 워크플로우

- Jay 가 Step 8a dogfood 진행 시 `PWDEBUG=1 npx playwright codegen http://localhost:4173` 또는 manual trace recording 사용
- 발견된 버그 자리에서 trace 저장 → 해당 trace 를 `web/e2e/dogfood-finding-XX.spec.ts` 로 변환
- 효과: dogfood 의 **회귀 자산화 비용을 사실상 0에 가깝게** 낮춤. 발견 → 자동 회귀 cycle 단축.

**프롬프트** (Step 8a 1차 끝나고):

```
/bmad-quick-dev

Step 8a 1차 dogfood 결과(_bmad-output/dogfood/dogfood-run-1-2026-MM-DD.md)를 보고 Step 8b 자동화 자산 3개를 구성해줘.

입력:
- _bmad-output/dogfood/dogfood-run-1-2026-MM-DD.md (1차 발견 항목)
- web/playwright.config.ts
- web/e2e/po/ (기존 페이지오브젝트 재사용)
- _bmad-output/test-artifacts/test-design-epic-1-10-2026-04-25.md §4 SMOKE-01

원하는 답:
1. 8b-1 — web/e2e/visual/*.spec.ts (4~5개) + 한국어/긴 텍스트 fixture + CI 와이어링
2. 8b-2 — internal/pipeline/nightly_real_data_test.go (build tag `//go:build nightly`) + GitHub Actions cron workflow + 실패 시 issue 자동 생성 (gh CLI)
3. 8b-3 — docs/dogfood-trace-workflow.md (Jay 가 따라 할 수 있는 trace 녹화 → spec 변환 절차)

범위 제한:
- Step 8a 결과에서 정말로 자동화 가치 있는 것만 선별. 그냥 "할 수 있어서 한다" 식 금지.
- 외부 비용 발생하는 nightly 는 dry-run cap 명시 (1일 1회 cap, 월 비용 추정치 명시)
```

**기대 산출물**:
- `web/e2e/visual/*.spec.ts` + 베이스라인 PNG
- `internal/pipeline/nightly_real_data_test.go` + `.github/workflows/nightly-real-data.yml`
- `docs/dogfood-trace-workflow.md`

**경고**: 8b 는 dogfood 의 **보강**이지 대체가 아님. 8a 1차에서 "이거 자동화하면 좋겠다" 가 명확히 나온 항목만 8b 에 포함. 미리 가상으로 자동화 항목 추가하지 말 것 (over-engineering).

---

## 메모

- 순서는 **Step 1 → 2 → 3 → 3.5 → 4 ∥ 4.5 → 5 → 6 → 7 → 8a → 8b**. Step 4 와 Step 4.5 는 병렬 가능 (다른 파일 영역). Step 8a 와 8b 는 8a 1차 → 8b → 8a 2차 순.
- Step 5 는 5개 스토리를 **순차적으로** 진행 (Jay 솔로). 각 스토리 랜딩 직후 해당 Step 6 백필 1건을 바로 돌리면 리듬이 좋음.
- 모든 스텝은 커밋 scope 원칙(feedback_commit_scope.md) 엄격 준수 — 섞지 말 것.
- deferred-work.md 는 모든 스텝에서 공통 입력. Step 5 스토리가 랜딩될 때마다 관련 deferred 항목 체크하여 줄여나가기.
- **Step 7 PASS 판정 = V1 ship 가능의 *기술적* 조건**. **Step 8a 두 사이클 통과 = V1 ship 가능의 *사용성* 조건**. 둘 다 충족돼야 실제 ship.
- Step 8b 는 V1 ship 의 필수 게이트는 아님 (Step 8a 만으로도 ship 가능). 다만 V1.1 부터의 회귀 비용을 낮추는 **운영 자산**.


### 권장 루프

dogfood run (끊지 말고 끝까지)
  └─ 발견마다 → 5초 메모 (어느 화면, 뭐가 이상했는지, 심각도 느낌)
  └─ 진짜 P0 (다음 화면으로 아예 못 가는 blocker) → 그 자리에서 최소 fix 후 재개
  └─ 나머지는 전부 "계속"

run 끝나고
  └─ 메모 → P0/P1/P2 분류
  └─ P0 → Story 12-X 즉시
  └─ P1 → Epic 12 묶음
  └─ 2차 run: P0 lands 된 뒤
메모 형식 (간단할수록 좋음)

### # Dogfood Run 1 — 2026-04-25

[P0] BatchReview — Reject 후 다음 씬으로 못 넘어감, 버튼 disable 안 풀림
[P1] ProductionShell — 씬 4에서 텍스트 overflow, 말줄임 없이 잘림
[P1] Compliance Gate — "왜 거절됐는지" 설명 없음, 그냥 빨간색만
[P2] Settings — 저장 완료 토스트가 너무 빨리 사라짐
딱 이 형식이면 충분. 각 항목당 10~20초. run 끝나고 GitHub issue 로 올리거나 deferred-work.md 에 추가.

### 실제 루프

dogfood 진행
  └─ P0 blocker → 고치고 그 화면부터 재개
  └─ P1/P2 → 메모하고 계속
run 끝나고 → P1/P2 분류 후 Epic 12 묶음
그게 전부.