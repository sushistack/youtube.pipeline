package eval

import (
	"context"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestIntegration_Shadow_ReplaysRecentPassedCases walks the full
// DB → artifact → evaluator path against real SQLite with the
// shadow_eval_seed fixture. Window=5 so the integration covers both the
// ordering predicate and the window limit.
func TestIntegration_Shadow_ReplaysRecentPassedCases(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "shadow_eval_seed")
	root := testutil.ProjectRoot(t)

	src := NewSQLiteShadowSource(database)
	ev := &integrationEvaluator{
		defaultVerdict: VerdictResult{Verdict: domain.CriticVerdictPass, OverallScore: 88},
	}

	report, err := RunShadow(context.Background(), root, src, ev, shadowTestNow, 5)
	if err != nil {
		t.Fatalf("RunShadow integration: %v", err)
	}
	testutil.AssertEqual(t, 5, report.Window)
	testutil.AssertEqual(t, 5, report.Evaluated)
	testutil.AssertEqual(t, 0, report.FalseRejections)

	// Window=5 must pick the 5 newest eligible rows in created_at DESC order.
	testutil.AssertEqual(t, "scp-shadow-run-01", report.Results[0].RunID)
	testutil.AssertEqual(t, "scp-shadow-run-05", report.Results[4].RunID)
}

// TestIntegration_Shadow_DetectsFalseRejectionRegression feeds an evaluator
// that flips one previously passed case to "retry" and verifies the report
// counts it as a false rejection. accept_with_notes on another case must
// NOT raise the false-rejection count (per AC-SHADOW-REPORT-AND-DIFFS).
func TestIntegration_Shadow_DetectsFalseRejectionRegression(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "shadow_eval_seed")
	root := testutil.ProjectRoot(t)

	src := NewSQLiteShadowSource(database)
	ev := &integrationEvaluator{
		defaultVerdict: VerdictResult{Verdict: domain.CriticVerdictPass, OverallScore: 88},
		byRunID: map[string]VerdictResult{
			// run-01 regresses — this is the hard signal Shadow exists to catch.
			"scp-shadow-run-01": {
				Verdict:      domain.CriticVerdictRetry,
				RetryReason:  "weak_hook",
				OverallScore: 55,
			},
			// run-02 drifts to accept_with_notes — logged but NOT a false rejection.
			"scp-shadow-run-02": {
				Verdict:      domain.CriticVerdictAcceptWithNotes,
				OverallScore: 74,
			},
		},
	}

	report, err := RunShadow(context.Background(), root, src, ev, shadowTestNow, 10)
	if err != nil {
		t.Fatalf("RunShadow integration: %v", err)
	}
	testutil.AssertEqual(t, 1, report.FalseRejections)

	var found01, found02 bool
	for _, res := range report.Results {
		if res.RunID == "scp-shadow-run-01" {
			found01 = true
			testutil.AssertEqual(t, true, res.FalseRejection)
			testutil.AssertEqual(t, "weak_hook", res.NewRetryReason)
		}
		if res.RunID == "scp-shadow-run-02" {
			found02 = true
			testutil.AssertEqual(t, false, res.FalseRejection)
			testutil.AssertEqual(t, domain.CriticVerdictAcceptWithNotes, res.NewVerdict)
		}
	}
	if !found01 || !found02 {
		t.Fatalf("expected both run-01 and run-02 in report, found01=%t found02=%t", found01, found02)
	}
}

// TestIntegration_Shadow_LogsScoreDiffs runs the full pipeline and confirms
// the summary + per-case log lines capture the drift numbers a human
// reviewer would need under `go test -run Shadow -v`.
func TestIntegration_Shadow_LogsScoreDiffs(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "shadow_eval_seed")
	root := testutil.ProjectRoot(t)

	src := NewSQLiteShadowSource(database)
	ev := &integrationEvaluator{
		defaultVerdict: VerdictResult{Verdict: domain.CriticVerdictPass, OverallScore: 84},
		byRunID: map[string]VerdictResult{
			"scp-shadow-run-03": {
				Verdict:      domain.CriticVerdictRetry,
				RetryReason:  "fact_error",
				OverallScore: 60,
			},
		},
	}

	report, err := RunShadow(context.Background(), root, src, ev, shadowTestNow, 5)
	if err != nil {
		t.Fatalf("RunShadow integration: %v", err)
	}

	summary := report.SummaryLine()
	t.Log(summary)
	if !strings.Contains(summary, "evaluated=5") {
		t.Errorf("summary missing evaluated=5: %s", summary)
	}
	if !strings.Contains(summary, "false_rejections=1") {
		t.Errorf("summary missing false_rejections=1: %s", summary)
	}

	sawRegression := false
	for _, res := range report.Results {
		line := res.LogLine()
		t.Log(line)
		if res.RunID == "scp-shadow-run-03" {
			sawRegression = true
			if !strings.Contains(line, "false_rejection=true") {
				t.Errorf("run-03 line missing false_rejection=true: %s", line)
			}
			if !strings.Contains(line, "retry_reason=fact_error") {
				t.Errorf("run-03 line missing retry_reason=fact_error: %s", line)
			}
			// diff = 0.60 - 0.81 = -0.21 (baseline from seed row -3 days)
			if !strings.Contains(line, "diff=-0.21") {
				t.Errorf("run-03 line missing diff=-0.21: %s", line)
			}
		}
	}
	if !sawRegression {
		t.Error("expected scp-shadow-run-03 in integration report")
	}
}

// integrationEvaluator is the inline fake evaluator used by integration
// tests. No external HTTP, no gomock/testify — matches the project's
// TestXxx_CaseName + inline-fakes convention.
type integrationEvaluator struct {
	defaultVerdict VerdictResult
	byRunID        map[string]VerdictResult
}

func (e *integrationEvaluator) Evaluate(_ context.Context, f Fixture) (VerdictResult, error) {
	if v, ok := e.byRunID[f.FixtureID]; ok {
		return v, nil
	}
	return e.defaultVerdict, nil
}
