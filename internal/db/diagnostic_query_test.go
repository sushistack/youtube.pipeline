package db_test

import (
	"context"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestDiagnosticQueries_NFR_O3 pins the "operator-friendly CLI queries" that
// NFR-O3 promises: every query is answerable via a raw SQL string against
// the canonical schema — no JOINs, no JSON1, no post-processing. If a future
// schema change forces a JOIN, this test breaks and `docs/cli-diagnostics.md`
// must be re-authored.
//
// Queries are copied verbatim from docs/cli-diagnostics.md — keep them in
// sync. If you modify one, update the other.
func TestDiagnosticQueries_NFR_O3(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "observability_seed")
	ctx := context.Background()

	t.Run("recent_failures", func(t *testing.T) {
		rows, err := database.QueryContext(ctx,
			`SELECT id, stage, retry_count, retry_reason, cost_usd
			   FROM runs
			  WHERE status='failed'
			  ORDER BY updated_at DESC
			  LIMIT 10`)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		if n == 0 {
			t.Fatal("expected at least one failed run, got 0")
		}
		if n > 10 {
			t.Fatalf("LIMIT 10 violated, got %d", n)
		}
	})

	t.Run("todays_spend", func(t *testing.T) {
		var sum *float64
		err := database.QueryRowContext(ctx,
			`SELECT SUM(cost_usd) FROM runs WHERE created_at > date('now', 'start of day')`,
		).Scan(&sum)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		// No assertion on value — just that the query is legal SQL
		// and returns a scalar. NULL is fine when no runs today.
		_ = sum
	})

	t.Run("rolling_90d_failure_rate", func(t *testing.T) {
		rows, err := database.QueryContext(ctx,
			`SELECT status, COUNT(*)
			   FROM runs
			  WHERE created_at > date('now','-90 days')
			  GROUP BY status`)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		sawFailed := false
		sawCompleted := false
		for rows.Next() {
			var status string
			var count int
			if err := rows.Scan(&status, &count); err != nil {
				t.Fatalf("scan: %v", err)
			}
			if status == "failed" && count > 0 {
				sawFailed = true
			}
			if status == "completed" && count > 0 {
				sawCompleted = true
			}
		}
		if !sawFailed || !sawCompleted {
			t.Errorf("expected both failed and completed rows in 90d window, got failed=%v completed=%v", sawFailed, sawCompleted)
		}
	})

	t.Run("per_stage_cost_breakdown", func(t *testing.T) {
		rows, err := database.QueryContext(ctx,
			`SELECT stage, SUM(cost_usd) AS total, AVG(duration_ms) AS avg_ms
			   FROM runs
			  WHERE created_at > date('now','-7 days')
			  GROUP BY stage
			  ORDER BY total DESC`)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		seen := 0
		for rows.Next() {
			var stage string
			var total, avgMs float64
			if err := rows.Scan(&stage, &total, &avgMs); err != nil {
				t.Fatalf("scan: %v", err)
			}
			seen++
		}
		if seen == 0 {
			t.Fatal("expected at least one stage in 7d window")
		}
	})

	t.Run("critic_score_health", func(t *testing.T) {
		rows, err := database.QueryContext(ctx,
			`SELECT id, critic_score
			   FROM runs
			  WHERE critic_score IS NOT NULL AND critic_score < 0.7
			  ORDER BY updated_at DESC
			  LIMIT 20`)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		// Fixture has 5 low-critic-score rows.
		if n != 5 {
			t.Errorf("critic_score<0.7 count: got %d want 5", n)
		}
	})
}
