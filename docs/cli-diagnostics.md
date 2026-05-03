# SQLite CLI 진단 쿼리 (NFR-O3 / NFR-O4)

오퍼레이터용 기본 진단 세트. 외부 도구 없이 `sqlite3 ./pipeline.db` (project-root 레이아웃) 한 줄이면 끝납니다.

쿼리는 모두 `runs` 테이블 단일 스캔 또는 `idx_runs_*` 인덱스를 사용합니다 (NFR-O4).
Migration 003에서 추가된 인덱스:

- `idx_runs_created_at` — 롤링 윈도우 쿼리의 기본 접근 경로
- `idx_runs_status_created_at` — 상태 필터 + 날짜 윈도우 결합
- `idx_runs_stage` — `GROUP BY stage` 집계

각 쿼리는 `internal/db/diagnostic_query_test.go`가 실제로 실행하면서 검증합니다.
스키마가 바뀌어 JOIN이 필요해지면 해당 테스트가 깨져서 이 문서를 갱신하라고 강제합니다.

## 1. 최근 실패한 런 10개

```sql
SELECT id, stage, retry_count, retry_reason, cost_usd
  FROM runs
 WHERE status = 'failed'
 ORDER BY updated_at DESC
 LIMIT 10;
```

## 2. 오늘 지출한 총 비용

```sql
SELECT SUM(cost_usd) FROM runs WHERE created_at > date('now', 'start of day');
```

`NULL`이면 오늘 생성된 런이 없다는 뜻 — 정상 상태.

## 3. 90일 롤링 실패율

```sql
SELECT status, COUNT(*)
  FROM runs
 WHERE created_at > date('now', '-90 days')
 GROUP BY status;
```

Day-90 게이트 판정에 직접 사용 (NFR-R2 / NFR-R4 트래킹).

## 4. 스테이지별 비용 / 평균 소요 시간 (지난 7일)

```sql
SELECT stage, SUM(cost_usd) AS total, AVG(duration_ms) AS avg_ms
  FROM runs
 WHERE created_at > date('now', '-7 days')
 GROUP BY stage
 ORDER BY total DESC;
```

어느 스테이지가 예산/시간을 태우는지 즉시 확인.

## 5. 저품질 (critic < 0.7) 런 최근 20개

```sql
SELECT id, critic_score
  FROM runs
 WHERE critic_score IS NOT NULL AND critic_score < 0.7
 ORDER BY updated_at DESC
 LIMIT 20;
```

Critic 임계값 튜닝 (Story 2.5 / 4.x) 베이스라인.

## 6. Anti-progress 통계 (최근 50건, NFR-R2)

```sql
SELECT COUNT(*) AS total,
       SUM(CASE WHEN human_override = 1 THEN 1 ELSE 0 END) AS op_overridden
  FROM (
      SELECT human_override FROM runs
       WHERE retry_reason = 'anti_progress'
       ORDER BY created_at DESC
       LIMIT 50
  );
```

V1은 측정만 — `≤5%` FP 게이트는 V1.5부터 적용됩니다.
`op_overridden / total` 은 **오퍼레이터 오버라이드 비율**이며 V1의 FP 프록시입니다 (V1.5에서 정식 ground-truth 신호로 교체 예정).

## 관련 설정

- `cost_cap_per_run` (`./config.yaml`) — 런 전체 비용 서킷 브레이커 (NFR-C2).
- 스테이지별 캡: `cost_cap_research`, `cost_cap_write`, `cost_cap_image`, `cost_cap_tts`, `cost_cap_assemble` (NFR-C1).
- `anti_progress_threshold` — 연속 재시도 출력 간 코사인 유사도 상한 (기본 0.92, FR8 / NFR-R2). 1.0이면 탐지기 비활성화.

## 7. HITL 세션 일시정지 검사 (Story 2.6, FR49 + FR50)

런이 HITL 체크포인트에서 일시정지된 경우 (`status='waiting'` + stage ∈
{scenario_review, character_pick, batch_review, metadata_ack}), 시스템은
`hitl_sessions` 테이블에 스냅샷을 내구적으로 저장합니다. 이 스냅샷은
정확한 재개 위치 + "마지막 상호작용 이후 무엇이 바뀌었는지" diff 계산에
사용됩니다.

일시정지된 런 조회:

```
pipeline status <run-id>
```

일시정지 상태 직접 조회:

```sql
SELECT run_id, stage, scene_index, last_interaction_timestamp
  FROM hitl_sessions
 WHERE run_id = 'scp-049-run-1';
```

응답의 `changes_since_last_interaction` 배열은 T1 이후 바뀐 것이 없으면
비어 있습니다. 예기치 못한 변경이 보이면 동일 런을 다른 프로세스가
수정하고 있지 않은지 확인하세요 — V1은 단일 오퍼레이터 툴이며
동시 writer는 버그입니다.

## 8. Pipeline metrics (Story 2.7, FR29 / NFR-O4)

`pipeline metrics --window 25` 는 최근 완료된 런만 집계해서 Day-90 게이트용
5개 지표를 보여줍니다. `provisional` 은 완료 런이 윈도우보다 적다는 뜻이고,
`unavailable` 은 아직 데이터가 없다는 뜻입니다. V1에서는 회귀 탐지율과 재개
idempotency를 DB에서 계산하지 못하므로 `--regression-rate`, `--idempotency-rate`
파일 플래그로 주입합니다.

```text
Pipeline metrics — rolling window: 25 (25 completed runs)

METRIC                       VALUE        TARGET     STATUS
---------------------------  -----------  ---------  ----------
Automation rate              72.0%        ≥ 80%      ✗ fail
Critic calibration (kappa)   0.71         ≥ 0.70     ✓ pass
Critic regression detection  82.0%        ≥ 80%      ✓ pass
Defect escape rate           5.0%         ≤ 5%       ✓ pass
Stage-level resume idempot.  100.0%       ≥ 100%     ✓ pass

Generated at: 2026-04-18T12:34:56Z
```

```sql
-- completed-run window (idx_runs_status_created_at)
SELECT id, status, critic_score, human_override, retry_count, retry_reason, created_at
  FROM runs WHERE status = 'completed'
 ORDER BY created_at DESC, id DESC LIMIT 25;

-- dominant operator verdict inputs (idx_decisions_run_id_type)
SELECT scene_id, decision_type FROM decisions
 WHERE run_id = ? AND superseded_by IS NULL
   AND decision_type IN ('approve','reject') AND scene_id IS NOT NULL;

-- auto-passed scenes (sqlite_autoindex_segments_1)
SELECT COUNT(*) FROM segments WHERE run_id = ? AND critic_score >= 0.70;
```

V1.5 deferred work: automation rate는 run-level sticky bit 기반이고, kappa도
scene-level critic verdict가 아니라 run-level proxy입니다. 자세한 후속 항목은
`_bmad-output/implementation-artifacts/deferred-work.md` 의 Story 2.7 섹션을
참조하세요.
