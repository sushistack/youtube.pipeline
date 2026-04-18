package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestMigration003_IndexesCreated confirms Migration 003 produced the three
// NFR-O4 indexes. Query sqlite_master directly — this is the authoritative
// check that the migration file was picked up by the embed.FS runner.
func TestMigration003_IndexesCreated(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)

	wanted := map[string]bool{
		"idx_runs_created_at":        false,
		"idx_runs_status_created_at": false,
		"idx_runs_stage":             false,
	}
	rows, err := database.Query(
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='runs'`,
	)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if _, ok := wanted[name]; ok {
			wanted[name] = true
		}
	}
	for idx, found := range wanted {
		if !found {
			t.Errorf("Migration 003 index missing: %s", idx)
		}
	}
}

// rollingWindowQueries is the canonical set of queries that NFR-O4 claims
// will run without full-table scans. Each is exercised via EXPLAIN QUERY PLAN
// and asserted to pick an index.
var rollingWindowQueries = []struct {
	name  string
	sql   string
	args  []any
	index string // which index we expect (substring match)
}{
	{
		name:  "recent_runs_by_date",
		sql:   "SELECT id, stage, status, cost_usd FROM runs WHERE created_at > ? ORDER BY created_at DESC",
		args:  []any{"2020-01-01"},
		index: "idx_runs",
	},
	{
		name:  "failure_count_in_window",
		sql:   "SELECT COUNT(*) FROM runs WHERE status = ? AND created_at > ?",
		args:  []any{"failed", "2020-01-01"},
		index: "idx_runs_status_created_at",
	},
	{
		name:  "stage_histogram_in_window",
		sql:   "SELECT stage, COUNT(*) FROM runs WHERE created_at > ? GROUP BY stage",
		args:  []any{"2020-01-01"},
		index: "idx_runs",
	},
	{
		name:  "metrics_recent_completed_runs",
		sql:   "SELECT id, status, critic_score, human_override, retry_count, retry_reason, created_at FROM runs WHERE status = ? ORDER BY created_at DESC, id DESC LIMIT ?",
		args:  []any{"completed", 25},
		index: "idx_runs_status_created_at",
	},
	{
		name:  "metrics_decisions_by_run",
		sql:   "SELECT scene_id, decision_type FROM decisions WHERE run_id = ? AND superseded_by IS NULL AND decision_type IN ('approve','reject') AND scene_id IS NOT NULL",
		args:  []any{"scp-049-run-1"},
		index: "idx_decisions_run_id_type",
	},
	{
		name:  "metrics_auto_passed_scenes",
		sql:   "SELECT COUNT(*) FROM segments WHERE run_id = ? AND critic_score >= ?",
		args:  []any{"scp-049-run-1", 0.70},
		index: "sqlite_autoindex_segments_1",
	},
}

func TestRollingWindowQueries_UseIndexes(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "observability_seed")

	for _, q := range rollingWindowQueries {
		t.Run(q.name, func(t *testing.T) {
			rows, err := database.Query("EXPLAIN QUERY PLAN "+q.sql, q.args...)
			if err != nil {
				t.Fatalf("explain: %v", err)
			}
			defer rows.Close()

			var plan strings.Builder
			for rows.Next() {
				var id, parent, notused int
				var detail string
				if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
					t.Fatalf("scan plan row: %v", err)
				}
				plan.WriteString(detail)
				plan.WriteString("\n")
			}
			planStr := plan.String()
			if !strings.Contains(planStr, "USING INDEX") &&
				!strings.Contains(planStr, "USING COVERING INDEX") {
				t.Errorf("query %s did not use an index. plan:\n%s", q.name, planStr)
			}
			if (strings.Contains(planStr, "SCAN runs") || strings.Contains(planStr, "SCAN decisions")) &&
				!strings.Contains(planStr, "USING INDEX") &&
				!strings.Contains(planStr, "USING COVERING INDEX") {
				t.Errorf("query %s performed a full scan. plan:\n%s", q.name, planStr)
			}
			if !strings.Contains(planStr, q.index) {
				t.Logf("query %s plan:\n%s (expected %s substring)", q.name, planStr, q.index)
			}
		})
	}
}

// TestMigration005_DecisionsIndexesCreated confirms Migration 005 produced the
// decisions-table indexes required by Story 2.7.
func TestMigration005_DecisionsIndexesCreated(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)

	wanted := map[string]bool{
		"idx_decisions_run_id_type": false,
		"idx_decisions_created_at":  false,
	}
	rows, err := database.Query(
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='decisions'`,
	)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if _, ok := wanted[name]; ok {
			wanted[name] = true
		}
	}
	for idx, found := range wanted {
		if !found {
			t.Errorf("Migration 005 index missing: %s", idx)
		}
	}
}

// TestSeedFixture_Distribution verifies the observability_seed fixture
// produced the expected row counts. Downstream tests rely on these numbers;
// if the fixture drifts, catch it here.
func TestSeedFixture_Distribution(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "observability_seed")
	ctx := context.Background()

	type bucket struct {
		label string
		query string
		args  []any
		want  int
	}
	for _, b := range []bucket{
		{"total", "SELECT COUNT(*) FROM runs", nil, 60},
		{"completed", "SELECT COUNT(*) FROM runs WHERE status=?", []any{"completed"}, 42},
		{"failed", "SELECT COUNT(*) FROM runs WHERE status=?", []any{"failed"}, 9},
		{"cancelled", "SELECT COUNT(*) FROM runs WHERE status=?", []any{"cancelled"}, 6},
		{"running", "SELECT COUNT(*) FROM runs WHERE status=?", []any{"running"}, 3},
		{"human_override", "SELECT COUNT(*) FROM runs WHERE human_override=1", nil, 3},
		{"low_critic_score", "SELECT COUNT(*) FROM runs WHERE critic_score IS NOT NULL AND critic_score < 0.7", nil, 5},
	} {
		var got int
		if err := database.QueryRowContext(ctx, b.query, b.args...).Scan(&got); err != nil {
			t.Fatalf("%s: %v", b.label, err)
		}
		testutil.AssertEqual(t, got, b.want)
	}
}
