package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type metricsTestStore struct {
	*db.RunStore
	*db.DecisionStore
}

func TestMetricsService_Report_FullWindow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "metrics_seed")
	clk := clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC))
	svc := NewMetricsService(metricsTestStore{
		RunStore:      db.NewRunStore(database),
		DecisionStore: db.NewDecisionStore(database),
	}, clk)
	regression := 0.82
	idempotency := 1.0

	report, err := svc.Report(context.Background(), 25, 0.70, &regression, &idempotency)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}

	testutil.AssertEqual(t, report.Window, 25)
	testutil.AssertEqual(t, report.WindowCount, 25)
	testutil.AssertEqual(t, report.Provisional, false)
	testutil.AssertEqual(t, report.GeneratedAt, "2026-04-18T12:34:56Z")
	testutil.AssertEqual(t, len(report.Metrics), 5)

	testutil.AssertFloatNear(t, *report.Metrics[0].Value, 0.72, 1e-9)
	testutil.AssertEqual(t, report.Metrics[0].Pass, false)
	testutil.AssertFloatNear(t, *report.Metrics[1].Value, 0.714828897338403, 1e-9)
	testutil.AssertEqual(t, report.Metrics[1].Pass, true)
	testutil.AssertFloatNear(t, *report.Metrics[2].Value, 0.82, 1e-9)
	testutil.AssertEqual(t, report.Metrics[2].Pass, true)
	testutil.AssertFloatNear(t, *report.Metrics[3].Value, 0.05, 1e-9)
	testutil.AssertEqual(t, report.Metrics[3].Pass, true)
	testutil.AssertFloatNear(t, *report.Metrics[4].Value, 1.0, 1e-9)
	testutil.AssertEqual(t, report.Metrics[4].Pass, true)
}

func TestMetricsService_Report_ProvisionalWhenShort(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "metrics_seed")
	svc := NewMetricsService(metricsTestStore{
		RunStore:      db.NewRunStore(database),
		DecisionStore: db.NewDecisionStore(database),
	}, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)))

	report, err := svc.Report(context.Background(), 30, 0.70, nil, nil)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}

	testutil.AssertEqual(t, report.WindowCount, 25)
	testutil.AssertEqual(t, report.Provisional, true)
	if report.Metrics[0].Value == nil {
		t.Fatal("automation metric should still be computed for a short window")
	}
}

func TestMetricsService_Report_HandlesEmptyDB(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	svc := NewMetricsService(metricsTestStore{
		RunStore:      db.NewRunStore(database),
		DecisionStore: db.NewDecisionStore(database),
	}, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)))

	report, err := svc.Report(context.Background(), 25, 0.70, nil, nil)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}

	testutil.AssertEqual(t, report.WindowCount, 0)
	testutil.AssertEqual(t, report.Provisional, true)
	for _, metric := range report.Metrics {
		testutil.AssertEqual(t, metric.Unavailable, true)
		testutil.AssertEqual(t, metric.Pass, false)
		testutil.AssertEqual(t, metric.Reason, "no completed runs in window")
		if metric.Value != nil {
			t.Fatalf("metric %s should have nil value when unavailable", metric.ID)
		}
	}
}

func TestMetricsService_Report_DegenerateKappa_Unavailable(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, stage, status, critic_score, created_at, updated_at) VALUES
		  ('r1', '049', 'complete', 'completed', 0.90, '2026-04-18T00:00:00Z', '2026-04-18T00:00:00Z'),
		  ('r2', '049', 'complete', 'completed', 0.95, '2026-04-18T00:01:00Z', '2026-04-18T00:01:00Z')`); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO segments (run_id, scene_index, critic_score) VALUES
		  ('r1', 0, 0.90), ('r2', 0, 0.95)`); err != nil {
		t.Fatalf("seed segments: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (run_id, scene_id, decision_type, created_at) VALUES
		  ('r1', '0', 'approve', '2026-04-18T00:00:10Z'),
		  ('r2', '0', 'approve', '2026-04-18T00:01:10Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	svc := NewMetricsService(metricsTestStore{
		RunStore:      db.NewRunStore(database),
		DecisionStore: db.NewDecisionStore(database),
	}, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)))

	report, err := svc.Report(context.Background(), 25, 0.70, nil, nil)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}

	kappa := report.Metrics[1]
	testutil.AssertEqual(t, kappa.Unavailable, true)
	testutil.AssertEqual(t, kappa.Reason, "degenerate — no variance to calibrate against")
	if kappa.Value != nil {
		t.Fatal("degenerate kappa must not emit a numeric value")
	}
}

func TestMetricsService_Report_ValidationOnZeroWindow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	svc := NewMetricsService(metricsTestStore{
		RunStore:      db.NewRunStore(database),
		DecisionStore: db.NewDecisionStore(database),
	}, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)))

	_, err := svc.Report(context.Background(), 0, 0.70, nil, nil)
	if err == nil {
		t.Fatal("expected validation error for zero window")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// TestMetricsService_Report_Performance1000Runs verifies NFR-O4: rolling-window
// metrics must complete in under 1 second for 1000 completed runs (PRD §Technical
// Success). A 5-second ceiling is used as a CI-safe assertion.
func TestMetricsService_Report_Performance1000Runs(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)

	// Insert 1000 completed runs with no decisions or segments.
	// Metrics will be partial (kappa/defect-escape unavailable) but the DB I/O
	// path — including idx_runs_status_created_at — is fully exercised.
	for i := range 1000 {
		id := fmt.Sprintf("perf-run-%04d", i)
		_, err := database.Exec(
			`INSERT INTO runs (id, scp_id, status, critic_score, human_override, retry_count, created_at)
             VALUES (?, 'scp-perf', 'completed', NULL, 0, 0, datetime('2026-01-01', ?))`,
			id, fmt.Sprintf("-%d seconds", i),
		)
		if err != nil {
			t.Fatalf("seed run %s: %v", id, err)
		}
	}

	svc := NewMetricsService(metricsTestStore{
		RunStore:      db.NewRunStore(database),
		DecisionStore: db.NewDecisionStore(database),
	}, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)))

	start := time.Now()
	report, err := svc.Report(context.Background(), 25, 0.70, nil, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if report.WindowCount != 25 {
		t.Fatalf("expected window_count=25, got %d", report.WindowCount)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Report on 1000-run DB took %v, exceeds 5s CI ceiling (NFR-O4 target: <1s on operator machine)", elapsed)
	}
}
