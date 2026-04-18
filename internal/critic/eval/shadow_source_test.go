package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestRecentPassedCases_UsesConfiguredWindow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "shadow_eval_seed")
	src := NewSQLiteShadowSource(database)

	cases, err := src.RecentPassedCases(context.Background(), 5)
	if err != nil {
		t.Fatalf("RecentPassedCases: %v", err)
	}
	testutil.AssertEqual(t, 5, len(cases))

	// Recency ordering: newest first — seed rows are spaced -1..-12 days so
	// run-01 must precede run-05.
	testutil.AssertEqual(t, "scp-shadow-run-01", cases[0].RunID)
	testutil.AssertEqual(t, "scp-shadow-run-05", cases[4].RunID)

	// BaselineVerdict is always "pass" in V1 — the selection predicate is the
	// 0.70 operational threshold.
	for _, c := range cases {
		testutil.AssertEqual(t, "pass", c.BaselineVerdict)
		if c.BaselineScore < 0.70 {
			t.Errorf("baseline %f < 0.70 for %s; selection predicate leaked a below-threshold row",
				c.BaselineScore, c.RunID)
		}
	}
}

func TestRecentPassedCases_CompletedOnly(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "shadow_eval_seed")
	src := NewSQLiteShadowSource(database)

	cases, err := src.RecentPassedCases(context.Background(), 20)
	if err != nil {
		t.Fatalf("RecentPassedCases: %v", err)
	}

	// 12 eligible rows total; the three decoys (failed / below-threshold /
	// null scenario) must be absent even though limit exceeds the eligible
	// count.
	testutil.AssertEqual(t, 12, len(cases))
	for _, c := range cases {
		if strings.HasPrefix(c.RunID, "scp-shadow-decoy-") {
			t.Errorf("decoy row leaked into results: %s", c.RunID)
		}
	}
}

func TestRecentPassedCases_RejectsMissingScenarioPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "shadow_eval_seed")
	src := NewSQLiteShadowSource(database)

	cases, err := src.RecentPassedCases(context.Background(), 20)
	if err != nil {
		t.Fatalf("RecentPassedCases: %v", err)
	}

	// The NULL-scenario decoy would slip through a naive query. The WHERE
	// clause rejects it explicitly so every returned case carries a
	// replayable path. Silent skipping would mask regressions (Shadow
	// cannot distinguish "no false rejections" from "half the sample did
	// not replay").
	for _, c := range cases {
		if c.ScenarioPath == "" {
			t.Errorf("%s returned with empty ScenarioPath", c.RunID)
		}
		if c.RunID == "scp-shadow-decoy-null-scenario" {
			t.Errorf("null-scenario decoy leaked into results")
		}
	}
}

func TestRecentPassedCases_RejectsInvalidLimit(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "shadow_eval_seed")
	src := NewSQLiteShadowSource(database)

	_, err := src.RecentPassedCases(context.Background(), 0)
	if err == nil {
		t.Fatal("expected validation error for limit=0")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}

	_, err = src.RecentPassedCases(context.Background(), -1)
	if err == nil {
		t.Fatal("expected validation error for negative limit")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestRecentPassedCases_QueryUsesIndex(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "shadow_eval_seed")

	rows, err := database.Query(
		`EXPLAIN QUERY PLAN `+recentPassedCasesSQL, CriticPassThreshold, 10)
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

	// Migration 003 created idx_runs_status_created_at; the Shadow query
	// seeks by status='completed' and then walks created_at DESC, which is
	// exactly that index's shape. A plain "SCAN runs" without any INDEX use
	// is a regression — AND a drift to any other index (e.g., a bare
	// created_at index that has to re-filter by status) is also a regression
	// because it loses the selectivity the composite index gives us.
	if !strings.Contains(planStr, "USING INDEX") &&
		!strings.Contains(planStr, "USING COVERING INDEX") {
		t.Errorf("Shadow recency query did not use an index. plan:\n%s", planStr)
	}
	if !strings.Contains(planStr, "idx_runs_status_created_at") {
		t.Errorf("Shadow recency query must use idx_runs_status_created_at. plan:\n%s", planStr)
	}
}
