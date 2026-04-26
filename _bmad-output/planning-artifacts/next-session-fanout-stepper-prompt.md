---
purpose: BMAD-quick-dev launch prompt for the next session
created: 2026-04-25
follows: spec-production-master-detail (commit f35582a, branch feat/production-master-detail)
status: ready-to-invoke
---

# How to use this file

Next session, paste the body below (everything inside the `--- /bmad-quick-dev ARGUMENTS ---` fences) into:

```
/bmad-quick-dev
```

It is self-contained — references the prior spec for context but does not assume
any conversation memory.

---

--- /bmad-quick-dev ARGUMENTS ---

Production 페이지 상단 stepper를 **n8n-style fan-out / fan-in 워크플로 시각화**로 확장한다.
현재는 6단계 직선 (Pending → Scenario → Character → Assets → Assemble → Complete) — `web/src/components/shared/StageStepper.tsx`. 운영자가 "지금 어디서 무엇이 동시에 돌고 있고 어디가 막혔는지"를 한눈에 못 봄.

## 직전 작업 컨텍스트 (변경 불필요, 컨텍스트로만 활용)
- 직전 commit: `f35582a feat(production): master-detail split refactor (Direction B)` on branch `feat/production-master-detail`
- 직전 spec: `_bmad-output/implementation-artifacts/spec-production-master-detail.md` — 4-zone 레이아웃, ProductionAppHeader가 stepper의 home, StatusBar에도 compact 변형이 있음
- 현재 `StageStepper`는 `variant: 'full' | 'compact'` 두 모드. 본 작업의 thin 모드는 이 둘 중 하나에 매핑되거나 유지됨

## 사전 audit (먼저 실행 — 백엔드 신호 유무가 결정타)

다음을 **순서대로** 확인하고, 결과를 plan 단계에서 명시 보고:

1. **Status payload 스키마** — `web/src/contracts/runContracts.ts`의 `runStatusResponseSchema`/`runStatusPayload`가 현재 어떤 필드를 노출하는지
2. **Per-agent progress (scenario stage)** — scenario 단계의 sub-agent (corpus retriever / outline writer / scene writer / critic 등) 각각의 진행 상태가 status에 emit되는가? `internal/api/handler_run.go`, `internal/pipeline/agents/*.go`, `internal/critic/eval/runtime_evaluator.go` 확인
3. **Per-modality progress (assets stage)** — TTS done/total, 이미지 done/total이 분리되어 emit되는가? assets stage 핸들러 확인
4. **Per-scene progress** — `10/32` 같은 카운터에 매핑할 scenes_total / scenes_done 류 필드가 status에 있는가? 없다면 review_items 길이로 derive 가능한지
5. **SSE stream 스키마** — `web/src/hooks/useRunStatus.ts`의 `/api/runs/{id}/status/stream` 핸들러가 어떤 이벤트 타입을 emit하는지 (SSE는 dirty 상태로 main에 미반영 — `useRunStatus.ts` diff 또는 main 비교 필요)

**결정 분기:**
- (A) 신호가 다 있다 → 순수 프론트 작업 (DAG 렌더 + 노드 mapping)
- (B) 일부 누락 → spec에 백엔드 확장 task 포함 (단, DashScope만 사용 / no dead layers — 진짜 필요한 신호만)
- (C) 거의 없음 → 본 task 보류하고 별도 backend epic으로 분리 권고

## UX 의도 (Direction B 후속 — 구체 mockup 없음, 본 결과로 처음 결정)

### Thin mode (default)
- 현재 6노드 직선과 동일 또는 살짝 압축
- 단계당 status dot + label
- 헤더 height ~48-56px 유지 (현재와 비슷)

### Expanded mode (사용자 토글)
- Stage 안의 fan-out을 펼침
- **Scenario**: corpus → outline → scene-writer × N → critic 같은 sub-agent들이 각각 노드. 동시 실행은 horizontal parallel rails로
- **Assets**: TTS 노드 + Image 노드를 parallel로 분기, 다시 Assemble로 수렴
- 각 fan-out 노드:
  - status dot (idle / running / done / failed)
  - per-scene 진행도 (예: `Image 18/32`, `TTS 12/32`) — 데이터 있을 때
  - hover/tap → 상세 (실패 사유, 재시도 횟수, 직전 latency)
- 단계 간 connector line (svg path) — 완료 구간은 채워짐, 활성 구간은 dashed pulsing

### 토글 메커니즘
- Header 우측 끝에 expand/collapse 버튼 (chevron-down / chevron-up)
- 상태는 `useUIStore`에 persist (다음 방문 시 유지). 키 이름 후보: `stage_stepper_expanded: boolean`
- `prefers-reduced-motion`이면 fan-out 펼침 애니메이션 생략

## 변경 범위 (P0)

1. **Pre-audit 결과를 spec에 적시** (Decision Notes 섹션)
2. **`StageStepper` 분기 또는 신규 `FanoutStepper`** — 기존 컴포넌트를 `variant: 'thin' | 'expanded'`로 확장하든, peer 컴포넌트를 추가하고 ProductionAppHeader에서 토글하든 결정. dead-layer 회피 — 두 컴포넌트로 분리할 거면 명확한 이유가 있어야 함.
3. **`useUIStore`에 expand 상태 persist** (zustand persist 패턴 기존 코드 참고: `production_last_seen`, `sidebar_collapsed`)
4. **Per-scene/per-modality 카운터 노출** — 신호가 있는 만큼만. 없으면 deferred-work에 명시.
5. **CSS** — `web/src/index.css`에 `.stage-stepper--expanded`, `.stage-stepper__rail`, `.stage-stepper__connector` 등. SVG connector path는 가능하면 인라인.
6. **테스트 (vitest)** — thin 모드 회귀 + expanded 모드 fan-out 렌더 + 카운터 표시 + persist 토글
7. **선택**: 이 stepper가 StatusBar의 compact stepper와 동기화되는지 (둘 다 같은 파이프라인을 보여주므로 의미 없는 중복일 수도 있음 — UX 결정 필요)

## 프로젝트 컨텍스트 / 제약

- 프론트: React 19 + TypeScript + Vitest + Playwright. 직전 commit 직후 main에 미반영된 dirty 다수 (`useRunStatus.ts` SSE / `NewRunPanel.tsx` portal / `fixtures.ts` formatter / cmd/* / internal/* / tuning/* 등) — 이 dirty들은 본 작업 외 epic이므로 절대 손대지 말 것
- 작업 시작 전 `git status` 로 baseline 확인. 가능하면 `feat/production-master-detail` 브랜치를 main에 머지 후 새 feature 브랜치로 시작 (사용자에게 확인)
- **memory 피드백 (그대로 적용)**:
  - **commit scope 엄격성**: 이 task 범위 외 파일·라인 수정 금지. mixed 파일은 uncommitted로 둠.
  - **no dead layers**: thin/expanded를 future-proof하게 추상화하지 말 것. 진짜 필요한 한 컴포넌트로 끝낼 수 있으면 그게 정답.
  - **test-driven**: vitest 패턴 (`StageStepper.test.tsx` 참고). 신규 컴포넌트면 신규 .test.tsx 동반.
  - **유저 1급 사용성**: 토글이 실제 동작해야 함 (mockup 흉내 X). 키보드 토글 단축키 한 개 정도 (예: `Shift+P`로 pipeline view 토글) 검토.
- **외부 API**: DashScope만 사용. backend 확장이 들어가면 새 외부 API 도입 금지.
- **백엔드 손댈 일 생기면**: pre-audit (B) 분기일 때만. 가능하면 status 응답 스키마 확장 + SSE 이벤트 1-2개 추가로 끝내고, 큰 리팩터는 별도 epic으로.

## 산출물

- 수정/신규 컴포넌트, 테스트, (필요 시) 백엔드 status 스키마 확장 + 컨트랙트 동기화
- 모든 vitest 테스트 통과
- typecheck 통과
- (가능하면) dev server에서 expanded 모드 토글 + fan-out 렌더 + 카운터 갱신 수동 확인
- 마지막에 변경 요약: 어떤 파일이 어떻게 바뀌었는지, deferred-work에 추가된 항목 (특히 누락 신호), 다음 task 후보

--- /end ARGUMENTS ---

# Optional pre-flight (사용자 결정 사항)

다음 항목은 다음 세션 시작 시 사용자에게 묻고 진행:

1. **브랜치 전략**: `feat/production-master-detail`을 먼저 main에 머지할지, 그 위에서 새 브랜치를 딸지, 아니면 현재 브랜치 그 자리에서 이어갈지
2. **압박 일정 여부**: thin 모드 회귀 안전성이 최우선이라면 expanded는 별도 후속 commit으로 분리
3. **백엔드 확장 허용 여부**: pre-audit 결과 (B) 분기로 갈 때 같은 세션에서 같이 갈지, 별 세션으로 미룰지
