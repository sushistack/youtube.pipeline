# SQLite CLI 진단 쿼리 (NFR-O3 / NFR-O4)

오퍼레이터용 기본 진단 세트. 외부 도구 없이 `sqlite3 ~/.youtube-pipeline/pipeline.db` 한 줄이면 끝납니다.

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

## 관련 설정

- `cost_cap_per_run` (`~/.youtube-pipeline/config.yaml`) — 런 전체 비용 서킷 브레이커 (NFR-C2).
- 스테이지별 캡: `cost_cap_research`, `cost_cap_write`, `cost_cap_image`, `cost_cap_tts`, `cost_cap_assemble` (NFR-C1).
