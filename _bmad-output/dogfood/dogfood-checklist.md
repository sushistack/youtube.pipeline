# UI Dogfood Checklist

**운영자 관점** 체크리스트. CI 녹색 확인용이 아니라 "Jay 가 실제로 SCP 영상을 만들 수 있는가" 검증용.

## 사용법

1. 이 파일을 에디터에 열어두고 UI 조작과 병행
2. 통과: `- [x]` 로 체크
3. 이상 발견: 체크하지 말고 줄 끝에 `← P0 / P1 / P2: 메모` 기록
4. P0 (다음 단계로 못 넘어가는 blocker) → 그 자리에서 고치고 재개
5. P1/P2 → 계속 진행 후 세션 끝나고 Epic 12 로 묶기

**심각도 기준**
- `P0` — 다음 단계로 진행 불가 (blocker)
- `P1` — 진행은 되나 이상함 (복사 모호, 상태 표시 부재, 레이아웃 깨짐 등)
- `P2` — cosmetic (정렬, 색, 애니메이션, 토스트 타이밍 등)

---

## Phase −1 — 사전 준비 (Setup)

> 모든 명령어는 레포 루트(`/mnt/work/projects/youtube.pipeline`) 기준. 별도 명시 없으면 기본 셸은 `zsh`.

### 사전 빌드 / 의존성

- [x] `cd web && npm install` — 의존성 설치 (최초 1회 또는 package.json 변경 시)
- [x] `~/.youtube-pipeline/config.yaml` 존재 — 없으면 `go run ./cmd/pipeline init` 으로 생성
- [x] `~/.youtube-pipeline/.env` 에 필수 키 (DashScope, OpenAI 등) 채워져 있음
- [ ] (prod 모드 검증 시에만) `make build` 또는 `go build -o bin/pipeline ./cmd/pipeline` 으로 바이너리 빌드

### 서버 기동 — `./startup.sh` 로 통일

> **주의**: `make dev` 는 `air` (Go hot reloader) 의존이 있어 air 미설치 환경에서 실패합니다. dogfood 세션에서는 `./startup.sh` 를 표준으로 사용합니다.

**A. 통합 dev 모드 (기본) — `./startup.sh dev`**
- [x] 레포 루트에서 `./startup.sh dev` 실행
- [x] `[startup] Starting Vite dev server on http://127.0.0.1:5173` 로그 확인
- [x] `[startup] Starting Go server on http://127.0.0.1:8080 (proxying frontend to Vite)` 로그 확인
- [x] Go 서버 로그에 `Listening on ...` 출력 확인
- [x] 브라우저에서 `http://127.0.0.1:8080` 접속 → SPA 로드, 콘솔 에러 없음 (Go 서버가 Vite 로 프록시)

**B. prod 빌드 모드 (배포 동등 검증) — `./startup.sh prod`**
- [x] `./startup.sh prod` 실행 → `npm run build` 후 embedded SPA 를 Go 서버가 직접 서빙
- [x] `http://127.0.0.1:8080` 접속 → 빌드된 SPA 로드 확인

**환경 변수 (선택)**: `APP_PORT`, `VITE_PORT`, `DATA_DIR`, `DB_PATH`, `OUTPUT_DIR` — 기본값은 [startup.sh:55-60](startup.sh#L55-L60) 참고

### 클린 상태 확보

- [x] DevTools → Application → Local Storage 에서 `youtube-pipeline-ui` 키 삭제 (OnboardingModal 재현용)
- [x] DevTools 콘솔 비어있음 (페이지 로드 직후 에러/경고 0개)

---

## Phase 0 — 앱 시작 & 기본 탐색

### 최초 방문

- [x] `http://localhost:5173` 접속 시 OnboardingModal 표시됨 ([web/src/components/shared/OnboardingModal.tsx](web/src/components/shared/OnboardingModal.tsx))
- [x] 모달 제목 **"Know where to work next"** 표시
- [x] **"Continue to workspace"** 버튼 클릭 → 모달 닫힘
- [x] 페이지 새로고침 → 모달 재표시 안 됨 (localStorage `youtube-pipeline-ui.onboarding_dismissed=true`)
- [x] `Esc` 키 → 모달 닫힘 (대체 동작)

### 사이드바 & 네비게이션

- [x] 사이드바 brand 영역에 `youtube.pipeline` 라벨 표시 ([Sidebar.tsx:110](web/src/components/shared/Sidebar.tsx#L110))
- [x] nav 항목 3개: **Production / Tuning / Settings** 표시
- [x] Production 클릭 → URL `/production`
- [x] Tuning 클릭 → URL `/tuning`
- [x] Settings 클릭 → URL `/settings`
- [x] 활성 탭 시각적으로 구분됨 (선택 상태 강조)
- [x] 사이드바 collapse 토글 → 사이드바 접힘
- [x] 접힌 상태 expand 토글 → 다시 펼쳐짐
- [x] 뷰포트 1024px 로 좁히기 → 사이드바 강제 collapse 동작

---

## Phase 1 — New Run 생성

### NewRunPanel 진입 (모달)

> Production 라우트(`/production`)에 있어야 New Run 트리거가 표시됨.
> NewRunPanel 은 사이드바 인라인이 아니라 **화면 중앙에 띄우는 모달** 형태 (backdrop + 중앙 정렬).

- [x] 사이드바 상단 **"+ New Run"** 버튼 보임 ([Sidebar.tsx:148-160](web/src/components/shared/Sidebar.tsx#L148-L160), aria-label `"Create a new pipeline run"`)
- [x] 사이드바 collapsed 상태에서도 **"+"** 아이콘 버튼만 컴팩트하게 표시 (라벨 숨김, 사이드바 폭 안에 정렬)
- [x] **"+ New Run"** 클릭 → 화면 중앙에 모달 열림 (backdrop 어두워짐, 제목 **"Create a new pipeline run"**)
- [x] 모달 열리면 body 스크롤 잠김 (배경 스크롤 안 됨)
- [x] 모달 열리면 SCP ID 입력 필드에 자동 포커스
- [x] 입력 필드 placeholder **"049"**
- [x] 모달 외부(backdrop) 클릭 → 모달 닫힘, 포커스가 사이드바 **"+ New Run"** 으로 복귀

### SCP ID 입력 & 유효성 검사

- [x] 빈 상태에서 **"Create"** 버튼 비활성화 ([NewRunPanel.tsx](web/src/components/production/NewRunPanel.tsx))
- [x] 유효하지 않은 문자 입력 (예: `SCP 049` 공백 포함, 또는 `abc`) → 인라인 에러 메시지 표시
- [x] 유효한 값 입력 (예: `049` 또는 `SCP-049`) → 에러 사라지고 **"Create"** 활성화
- [x] **"Create"** 클릭 → 버튼 라벨이 **"Creating…"** 으로 바뀌고 disabled
- [x] Submit 성공 → 모달 닫힘, URL `?run=<RUN_ID>` 로 업데이트, 사이드바 Run inventory 에 신규 RunCard(컴팩트 형태) 표시
- [x] StageStepper 가 `scenario_review` 단계를 강조 표시 (waiting 상태)

### 취소 & 에러

- [x] **"Cancel"** 버튼 또는 `Esc` 키 → 모달 닫힘, 포커스가 사이드바 **"+ New Run"** 으로 복귀
- [x] backdrop 클릭 → 모달 닫힘 (Cancel 과 동일 동작), 단 Submit 진행 중일 때는 닫히지 않음
- [x] `Tab` / `Shift+Tab` → 포커스가 모달 내부에 갇힘 (focus trap)
- [x] Go 서버 종료(`Ctrl+C`) 후 Submit → 모달 안에 에러 메시지 표시 (현재 카피: **"Couldn't reach the server. Check that `pipeline serve` is running, then retry."** — [NewRunPanel.tsx:46-54](web/src/components/production/NewRunPanel.tsx#L46-L54)) · DevTools Network 탭에서 POST `/api/runs` 가 `(failed) net::ERR_CONNECTION_REFUSED` 로 끝나는지 동시 확인
- [x] 동일 SCP ID 재제출 → **새 run 시퀀스 가 추가로 생성됨** (`scp-049-run-2`, `-run-3` …). 백엔드 spec — 동일 SCP 에 대해 여러 run 허용 ([run_store.go:91-105](internal/db/run_store.go#L91-L105)). 사이드바 Run inventory 에 신규 카드가 쌓이는지만 확인하면 됨

---

## Phase 2 — Scenario 단계 (ScenarioInspector)

> Run 이 `scenario_review` stage 에 진입한 상태에서 테스트
> 진행이 안 되면 pending 카드 최상단의 **"Start run"** 버튼 클릭 → `POST /api/runs/<RUN_ID>/resume` 호출 (UI 단독으로 stage 진입 가능, 터미널 불필요)

### Pending → Scenario 진입 (Start run 버튼)

- [x] New Run 생성 직후 pending 카드(aria-label `"Pending run guidance"`)에 **"Start run"** 버튼이 카드 최상단에 강조 색으로 표시 (border + accent 배경)
- [x] 버튼 클릭 → 라벨이 **"Starting…"** 으로 전환되며 disabled
- [x] `POST /api/runs/<RUN_ID>/resume` 200 응답 → 카드 사라지고 ScenarioInspector 자동 렌더 (DevTools Network 탭에서 확인)
- [x] StageStepper 가 `scenario_review` 단계 (waiting 상태) 강조로 갱신
- [x] 실패 시 (서버 다운/에러) 카드 안에 **"Start failed: …"** 메시지 표시, 재클릭 가능

### 씬 목록

- [ ] section aria-label `"Scenario narration review"`, eyebrow **"Scenario review"** 표시 ([ScenarioInspector.tsx:41-43](web/src/components/production/ScenarioInspector.tsx#L41-L43))
- [ ] 씬 목록 렌더링 — "N scenes" 헤더 표시
- [ ] 씬 수가 많을 때 (10개+) 레이아웃 깨짐 없음
- [ ] 긴 나레이션 텍스트 — overflow truncation 또는 wrap 자연스럽게 처리
- [ ] 한국어·특수문자 포함 텍스트 정상 표시
- [ ] 로딩 중 **"Loading scenes…"** 표시 ([ScenarioInspector.tsx:17](web/src/components/production/ScenarioInspector.tsx#L17))
- [ ] 에러 시 **"Failed to load scenes. Try refreshing."** 표시 ([ScenarioInspector.tsx:25](web/src/components/production/ScenarioInspector.tsx#L25))

### 인라인 편집 (InlineNarrationEditor)

- [ ] 씬 클릭 → 편집 모드 진입 (textarea 포커스, aria-label `"Narration for scene N"`)
- [ ] `Tab` → 편집 모드 진입 (키보드만으로 가능, aria-hint 에 "Press Tab, Enter, or click to edit" 명시됨)
- [ ] 편집 중 텍스트 수정 후 `Enter` → 저장, 편집 종료
- [ ] `Shift+Enter` → 줄바꿈 (저장 아님)
- [ ] `Ctrl+Z` → 변경사항 원래대로 되돌아감
- [ ] 저장 직후 페이지 새로고침 → 변경 내용 유지 확인 (DB 반영)
- [ ] 다른 씬 클릭 시 현재 편집 중인 씬 자동 저장 또는 취소 여부 명확
- [ ] 저장 실패 시 에러 표시 — DevTools Network 탭에서 `PATCH /api/runs/<RUN_ID>/scenes/<idx>` 응답을 throttle/offline 으로 강제 실패시켜 검증

### StageStepper (모든 단계 공통)

- [ ] StageStepper 에서 현재 단계 강조 표시
- [ ] 완료된 단계 체크 표시
- [ ] compact / full 두 variant 모두 가독성 확인 (Production 헤더와 사이드바 RunCard 양쪽)

---

## Phase 3 — Character 단계 (CharacterPick)

> Run 이 `character_pick` stage 에 진입한 상태에서 테스트

### 캐릭터 검색 & 그리드

- [ ] 캐릭터 검색 입력 필드 표시 및 동작
- [ ] 검색어 입력 → 후보 목록 필터링
- [ ] 후보 그리드 표시 (data-testid `character-grid`, [CharacterPick.tsx:80](web/src/components/production/CharacterPick.tsx#L80))
- [ ] 셀 data-testid `character-grid-cell-0` ~ `character-grid-cell-9` 까지 노출
- [ ] 후보 없을 때 empty state 명확
- [ ] 숫자 키 `1`–`9` → 그리드 위치 0–8 선택, `0` → 위치 9 선택
- [ ] `Enter` → 선택 확정
- [ ] `Escape` → 검색으로 복귀

### VisionDescriptorEditor

- [ ] 캐릭터 선택 후 Vision Descriptor 입력 화면 진입
- [ ] 기존 descriptor prefill 있으면 자동 채워짐 (DB 에 저장된 마지막 값)
- [ ] 텍스트 입력 후 저장 → 다음 캐릭터로 진행
- [ ] 여러 캐릭터 있을 때 순서대로 처리 흐름 명확 (진행 상황 indicator 보임)
- [ ] 마지막 캐릭터 완료 후 다음 단계로 자동 이동 또는 명확한 안내 메시지

---

## Phase 4 — Assets 단계 (BatchReview)

> Run 이 `assets_review` stage 에 진입한 상태에서 테스트
> ActionBar 단축키는 [BatchReview.tsx:707-776](web/src/components/production/BatchReview.tsx#L707-L776) 에 정의

### 리뷰 항목 목록

- [ ] 리뷰 항목 목록 표시 (이미지 + 오디오 쌍)
- [ ] High-leverage 항목이 먼저 정렬되어 표시
- [ ] SceneCard — 이미지 썸네일 표시 (broken image 아이콘 없음)
- [ ] SceneCard — 씬 인덱스·나레이션 요약 표시
- [ ] `waiting_for_review` / `pending` 상태와 처리 완료 항목 시각적 구분

### 오디오 재생 (AudioPlayer)

- [ ] AudioPlayer 재생 버튼 클릭 → 오디오 재생
- [ ] 재생 중 타임라인 진행 표시
- [ ] 재생 완료 → 정지 상태로 복귀
- [ ] 오디오 파일 없는 항목 → 에러 표시 또는 disabled 처리

### 개별 항목 결정 (ActionBar 라벨 그대로 표기)

- [ ] **`[Enter] Approve`** 버튼 또는 Enter 키 → 항목 승인
- [ ] **`[Esc] Reject`** 버튼 또는 Esc 키 → RejectComposer 열림
- [ ] `J` → 다음 항목 (review-next)
- [ ] `K` → 이전 항목 (review-prev)
- [ ] **`[S] Skip`** 버튼 또는 S 키 → 현재 항목 skip
- [ ] **`[Ctrl+Z] Undo`** 버튼 또는 Ctrl+Z → 마지막 결정 undo, 항목 상태 원복
- [ ] **`[Tab] Edit`** 버튼은 disabled 상태 확인 (현재 미구현, P2 cosmetic 으로만 분류)

### RejectComposer

- [ ] Reject 트리거 시 RejectComposer 열림
- [ ] 거절 사유 입력 필드 표시 및 자동 포커스
- [ ] 사유 입력 후 확정 → 항목 rejected 상태로 변경
- [ ] RejectComposer 열린 상태에서 J/K/S 단축키 비활성화 확인

### Regen

- [ ] Rejected 항목에 Regen 버튼 표시
- [ ] Regen 클릭 → 재생성 요청, 로딩 표시
- [ ] Regen 2회 후 버튼 비활성화 (max 2 regen per scene)
- [ ] Regen 이후 새 이미지/오디오로 업데이트

### 재시도 소진(Retry-exhausted) 처리

- [ ] 소진 시 **"Skip & flag"** 버튼 노출 ([BatchReview.tsx:686](web/src/components/production/BatchReview.tsx#L686) 부근)
- [ ] 클릭 → 항목이 skip 처리되고 manifest 에 flag 기록

### DetailPanel

- [ ] 항목 클릭 → DetailPanel 열림 (이미지 원본 크기 / 나레이션 전문)
- [ ] DetailPanel 닫기 가능 (X 버튼 또는 Esc)

### 일괄 처리

- [ ] **`[Shift+Enter] Approve All Remaining`** 버튼 표시 시점 (1개 이상 actionable 남은 경우, [BatchReview.tsx:743](web/src/components/production/BatchReview.tsx#L743))
- [ ] Shift+Enter 또는 버튼 클릭 → InlineConfirmPanel 열림 (메시지 **"This will approve …"** + 카운트)
- [ ] 확인 → 모든 남은 항목 승인, 다음 단계 진행
- [ ] 취소 → 원래 상태 복귀
- [ ] 모든 항목 처리 후 다음 단계로 자동 이동 또는 명확한 안내

---

## Phase 5 — Assemble / Compliance Gate (ComplianceGate)

> Run 이 `assemble` stage 에 진입한 상태에서 테스트
> 컴포넌트: [web/src/components/production/ComplianceGate.tsx](web/src/components/production/ComplianceGate.tsx)

### 메타데이터 표시

- [ ] 패널 제목 **"Pre-Upload Compliance Gate"**
- [ ] 영상 제목 표시
- [ ] 소스 URL 표시
- [ ] 작성자 표시
- [ ] 라이선스 표시
- [ ] 로딩 중 skeleton 상태 표시
- [ ] 메타데이터 로드 실패 시 에러 표시 (Network 탭에서 `/api/runs/<RUN_ID>/metadata` 강제 실패시켜 확인)

### 영상 미리보기

- [ ] `<video>` 엘리먼트 표시 (src `/api/runs/<RUN_ID>/video`)
- [ ] 재생 → **5초 도달 시 자동 정지** (timeupdate 핸들러, [ComplianceGate.tsx:38-49](web/src/components/production/ComplianceGate.tsx#L38-L49))
- [ ] 영상 파일 없을 때 fallback 표시
- [ ] 재생/정지 컨트롤 동작

### 체크리스트 (8개 항목)

각 라벨은 메타데이터 값을 인라인으로 채워서 표시. 라벨 형식 그대로 확인.

- [ ] **"Title confirmed: {title}"** — 제목 값 반영하여 표시
- [ ] **"AI disclosure — Narration: {AI|Human}"** — 체크 가능
- [ ] **"AI disclosure — Imagery: {AI|Human}"** — 체크 가능
- [ ] **"AI disclosure — TTS: {AI|Human}"** — 체크 가능
- [ ] **"Models logged: {model1, model2, …}"** — 체크 가능
- [ ] **"Source URL confirmed: {url}"** — 체크 가능
- [ ] **"Author confirmed: {author}"** — 체크 가능
- [ ] **"License: {license}"** — 체크 가능
- [ ] 미체크 항목 1개라도 있으면 Ack 버튼 비활성화
- [ ] 8개 모두 체크 → Ack 버튼 활성화

### Ack 제출

- [ ] **"Acknowledge & Complete"** 버튼 클릭 → 라벨 **"Finalising…"** 로 전환 후 `ready_for_upload` stage 진입 ([ComplianceGate.tsx:209](web/src/components/production/ComplianceGate.tsx#L209))
- [ ] 완료 후 라벨 **"Acknowledged"** 표시 (또는 즉시 CompletionReward 화면 전환)
- [ ] 이미 `ready_for_upload` 상태에서 재진입 시 409 에러 처리 또는 이미 완료 안내
- [ ] 네트워크 에러 시 에러 메시지 표시 (DevTools 로 `/api/runs/<RUN_ID>/ack` POST 차단 후 검증)

---

## Phase 6 — Completion (CompletionReward)

> Run 이 `complete` / `ready_for_upload` stage 에 진입한 상태
> 컴포넌트: [web/src/components/production/CompletionReward.tsx](web/src/components/production/CompletionReward.tsx)

- [ ] 화면 제목 **"Ready for upload"** 표시
- [ ] **"Compliance metadata summary"** 캡션이 붙은 테이블 표시 (Title / Source / Author / License 행)
- [ ] 최종 영상 미리보기 — **5초 자동 정지** 동일 동작 ([CompletionReward.tsx:20-30](web/src/components/production/CompletionReward.tsx#L20-L30))
- [ ] 업로드 명령어 (CLI 커맨드) 표시
- [ ] **"Copy command"** 버튼 클릭 → 클립보드 복사 ([ProductionShell.tsx:349](web/src/components/shells/ProductionShell.tsx#L349)) — 다른 에디터에 paste 로 검증
- [ ] 복사 후 토스트/배너 피드백 명확
- [ ] **"Start Next SCP"** 버튼 표시 ([CompletionReward.tsx:107](web/src/components/production/CompletionReward.tsx#L107)) → 클릭 시 NewRunPanel 으로 즉시 진입
- [ ] 사이드바 Run inventory 에 해당 Run 이 `complete` 상태로 표시

---

## Phase E — Error & Recovery

### FailureBanner (실패한 Run)

> 컴포넌트: [web/src/components/shared/FailureBanner.tsx](web/src/components/shared/FailureBanner.tsx)

- [ ] Run 실패 시 FailureBanner 표시 (eyebrow **"Pipeline failed"**, [FailureBanner.tsx:79](web/src/components/shared/FailureBanner.tsx#L79))
- [ ] 실패 메시지 — rate_limit 시 "DashScope rate limit" 메시지
- [ ] 실패 메시지 — 일반 실패 시 "Stage failed" 메시지
- [ ] **"Resume"** 버튼 클릭 → 라벨 **"Resuming..."** 로 전환, Run 재시작, 배너 닫힘 ([FailureBanner.tsx:106](web/src/components/shared/FailureBanner.tsx#L106))
- [ ] Resume 실패 시 **"Resume failed: {error}"** 표시
- [ ] Dismiss(X) 버튼 (aria-label `"Dismiss failure banner"`) → 배너 닫힘

### ContinuityBanner (재접속 시)

- [ ] 앱 재접속 후 Run 상태가 바뀌어 있을 때 ContinuityBanner 표시
- [ ] 배너에 변경 사항 요약 메시지 표시
- [ ] 5초 후 자동 사라짐
- [ ] 사용자 상호작용(클릭 등) 시 즉시 사라짐
- [ ] 편집 중인 입력 필드 상호작용 시 자동 dismiss 안 됨

### StatusBar (진행 중인 Run)

> 컴포넌트: [web/src/components/shared/StatusBar.tsx](web/src/components/shared/StatusBar.tsx) (aria-label `"Live telemetry"`)

- [ ] Run 진행 중 StatusBar 표시 (하단 또는 헤더)
- [ ] hover / focus 시 확장 — 소요 시간, 비용 표시
- [ ] Run 완료 후 StatusBar 사라짐

---

## Phase 7 — 사이드바 인벤토리 (복수 Run 환경)

> Run 여러 개 만든 뒤 테스트
> Sidebar 의 **"Run inventory"** 섹션 ([Sidebar.tsx:206](web/src/components/shared/Sidebar.tsx#L206))

- [ ] 복수 Run 목록 정렬 — 진행 중 Run 우선, 완료된 Run 하단
- [ ] RunCard (사이드바 컴팩트 모드) — SCP ID, Run 시퀀스, status 배지, freshness/cost 푸터만 표시 (StageStepper · 요약 · critic 칩은 숨김)
- [ ] RunCard — status 별 시각적 구분 (running / failed / complete)
- [ ] 카드 높이가 한 줄 가까운 컴팩트 형태로 유지됨 (사이드바 폭에 맞춰 좁아도 깨지지 않음)
- [ ] 검색 입력 placeholder **"Search runs"** 표시 ([Sidebar.tsx:217](web/src/components/shared/Sidebar.tsx#L217))
- [ ] 검색 → ID 로 필터링
- [ ] 검색 → SCP ID 로 필터링
- [ ] 검색 → stage 로 필터링 (예: `scenario_review`, `assets_review`, `assemble`)
- [ ] 검색 결과 없을 때 empty state 표시
- [ ] RunCard 클릭 → URL `?run=<RUN_ID>` 업데이트, 해당 Run 내용 메인 영역에 표시
- [ ] 다른 Run 선택 시 이전 Run 의 패널 닫힘

---

## Phase 8 — Settings (SettingsShell)

> 라우트 `/settings`. 컴포넌트: [web/src/components/shells/SettingsShell.tsx](web/src/components/shells/SettingsShell.tsx)

### 프로바이더 & 모델 설정 (ProviderConfigPanel)

- [ ] eyebrow **"Provider controls"** 표시 ([ProviderConfigPanel.tsx:25](web/src/components/settings/ProviderConfigPanel.tsx#L25))
- [ ] writer_provider 드롭다운 → 옵션 선택 가능
- [ ] writer_model 필드 표시 및 입력
- [ ] critic_provider 드롭다운
- [ ] critic_model 필드
- [ ] image_provider / image_model
- [ ] tts_provider / tts_model / tts_voice / tts_audio_format
- [ ] dashscope_region 필드
- [ ] writer_provider == critic_provider 선택 시 validation 에러 표시
- [ ] 필수 필드 비어있을 때 저장 시도 → 에러 표시

### 비용 상한 설정

- [ ] cost_cap_research 숫자 입력
- [ ] cost_cap_write / cost_cap_image / cost_cap_tts / cost_cap_assemble / cost_cap_per_run
- [ ] 음수 또는 0 입력 시 validation 처리

### Secret Fields (SecretFieldsPanel)

- [ ] eyebrow **"Secret storage"** 표시 ([SecretFieldsPanel.tsx:31](web/src/components/settings/SecretFieldsPanel.tsx#L31))
- [ ] API 키 입력 필드 표시 (마스킹 처리)
- [ ] 키 입력 후 저장 → 기존 키 마스킹된 상태로 유지
- [ ] **Clear** 버튼/링크로 기존 secret 제거 가능
- [ ] 빈 값으로 저장하면 기존 secret 유지 (안내문 명시됨, [SecretFieldsPanel.tsx:40](web/src/components/settings/SecretFieldsPanel.tsx#L40))

### 저장 & 초기화

- [ ] 변경사항 있을 때 QueuedChangeBanner 표시
- [ ] **Save** 클릭 → 저장 성공 피드백 (토스트 또는 배너)
- [ ] **Reset** 클릭 → 변경사항 초기화 확인 프롬프트 또는 즉시 되돌림

### BudgetIndicator & TimelineView

- [ ] BudgetIndicator eyebrow **"Budget telemetry"**, 제목 **"Spend against cap"** ([BudgetIndicator.tsx:22](web/src/components/settings/BudgetIndicator.tsx#L22))
- [ ] 상태 pill: **"Safe"** / **"Near cap"** / **"Exceeded"** 중 하나 표시 ([BudgetIndicator.tsx:29](web/src/components/settings/BudgetIndicator.tsx#L29))
- [ ] dt 라벨: **"Current spend"**, **"Soft cap"**, **"Hard cap"**
- [ ] TimelineView — 지출 이력 그래프/목록 표시
- [ ] 데이터 없는 경우 empty state 표시

---

## Phase 9 — Tuning (TuningShell)

> 라우트 `/tuning`. 컴포넌트: [web/src/components/shells/TuningShell.tsx](web/src/components/shells/TuningShell.tsx) (eyebrow **"Prompt and rubric lab"**)

### Critic Prompt (CriticPromptSection)

- [ ] textarea aria-label **"Critic prompt body"** 노출 ([CriticPromptSection.tsx:101](web/src/components/tuning/CriticPromptSection.tsx#L101))
- [ ] 현재 Critic 프롬프트 텍스트 prefill 됨
- [ ] 프롬프트 수정 후 저장 → SaveRecommendationBanner 표시 (versionTag 포함)
- [ ] SaveRecommendationBanner — **"Run Shadow"** 링크 클릭 → ShadowEvalSection 으로 스크롤
- [ ] Banner Dismiss → 사라짐

### FastFeedback

- [ ] FastFeedbackSection 표시
- [ ] 단일 씬 입력 후 fast feedback 실행
- [ ] 결과에 duration (`{N} ms`) 노출 ([FastFeedbackSection.tsx:62](web/src/components/tuning/FastFeedbackSection.tsx#L62))
- [ ] 결과 표시 (pass/fail + 코멘트)

### Golden Eval (GoldenEvalSection)

- [ ] 제목 **"Golden Eval"** 표시
- [ ] **"Run Golden eval"** 버튼 클릭 → 라벨 **"Running…"** 으로 전환 ([GoldenEvalSection.tsx:64](web/src/components/tuning/GoldenEvalSection.tsx#L64))
- [ ] 통과 시 → 결과 라인 (예: `recall 100.0% · detected N/M negatives · 0 false rejects`) 표시, Shadow Eval 활성화 ([GoldenEvalSection.tsx:73-84](web/src/components/tuning/GoldenEvalSection.tsx#L73-L84))
- [ ] 실패 시 (`false_rejects > 0`) → 실패 결과 + 재실행 가능 상태, Shadow 비활성 유지

### Shadow Eval (ShadowEvalSection)

- [ ] 제목 **"Shadow Eval"** 표시
- [ ] Golden 통과 전: 안내 문구 **"Golden must pass this session before Shadow can run."** 표시, 버튼 disabled ([ShadowEvalSection.tsx:63](web/src/components/tuning/ShadowEvalSection.tsx#L63))
- [ ] Golden 통과 후: **"Run Shadow eval"** 버튼 활성화 ([ShadowEvalSection.tsx:73](web/src/components/tuning/ShadowEvalSection.tsx#L73))
- [ ] 클릭 → 라벨 **"Running…"** 으로 전환, 결과 표시
- [ ] 결과에 critic provider/model + 결과 테이블 노출
- [ ] **페이지 새로고침 시 session gate 초기화 → Shadow 다시 비활성화** (React state 기반, 의도된 동작)

### Calibration & Fixture Management

- [ ] CalibrationSection — 트렌드 데이터 표시 (aria-label `"Calibration trend oldest to newest"`) 또는 empty state
- [ ] FixtureManagementSection — fixture 목록 표시 (aria-label `"Registered fixture pairs"`)
- [ ] **"Positive fixture"** / **"Negative fixture"** 라벨 노출, 파일 input 동작
- [ ] fixture 추가/삭제 동작

---

## 글로벌 단축키 체크

> 키맵 정의: [web/src/lib/keyboardShortcuts.ts](web/src/lib/keyboardShortcuts.ts). 모든 키는 입력 필드/모달 안에서는 비활성화되어야 함.

| 단축키 | 컨텍스트 | 기대 동작 | 동작 확인 | 비고 |
|--------|----------|-----------|-----------|------|
| `Esc` | OnboardingModal | 모달 닫힘 | | |
| `Esc` | NewRunPanel | 패널 닫고 포커스 복귀 | | |
| `Tab` | ScenarioInspector | 씬 편집 모드 진입 | | InlineNarrationEditor |
| `Enter` | InlineNarrationEditor | 저장 + 편집 종료 | | |
| `Shift+Enter` | InlineNarrationEditor | 줄바꿈 (저장 안 함) | | |
| `Ctrl+Z` | InlineNarrationEditor | 변경 되돌리기 | | |
| `1`–`9`, `0` | CharacterPick | 그리드 위치 0–9 선택 | | |
| `Enter` | CharacterPick | 선택 확정 | | |
| `Esc` | CharacterPick | 검색으로 복귀 | | |
| `Enter` | BatchReview | 현재 항목 Approve | | RejectComposer 열린 동안 비활성 |
| `Esc` | BatchReview | Reject (Composer 열기) | | |
| `J` | BatchReview | 다음 항목 | | |
| `K` | BatchReview | 이전 항목 | | |
| `S` | BatchReview | 현재 항목 Skip | | |
| `Shift+Enter` | BatchReview | Approve All Remaining (확인 패널) | | |
| `Ctrl+Z` | BatchReview | 마지막 결정 Undo | | |

---

## 세션 결과 요약

**실행 일자**: ____-__-__
**실행 환경**: [ ] `make dev` / [ ] `npm run dev` + `pipeline serve --dev` / [ ] `vite preview` + 빌드 바이너리
**브라우저**: [ ] Chrome / [ ] Firefox / [ ] Safari — 버전: ____
**테스트 SCP ID**: ____________

| 분류 | 건수 | 항목 |
|------|------|------|
| P0 (blocker) | | |
| P1 (이상하지만 진행 가능) | | |
| P2 (cosmetic) | | |
| 통과 | | |

**Epic 12 생성 여부**: [ ] 완료 — 링크: ____________

**다음 액션**:
-
-
