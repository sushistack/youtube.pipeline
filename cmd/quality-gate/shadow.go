package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
)

const shadowWindow = 10

// ShadowGateResult carries the outcome of one Shadow quality-gate run.
type ShadowGateResult struct {
	Report   eval.ShadowReport
	HardFail bool // true only when false_rejections > 0
	SoftFail bool // true when Empty — no candidates, non-blocking
	Err      error
}

// runShadowGate calls RunShadow and enforces zero false-rejection regressions.
// ShadowReport.Empty is surfaced as a soft-fail (warning, exit 0) because CI
// environments do not carry production run history; this is expected and must
// not block PRs.
func runShadowGate(
	ctx context.Context,
	projectRoot string,
	source eval.ShadowSource,
	ev eval.Evaluator,
	now time.Time,
) ShadowGateResult {
	report, err := eval.RunShadow(ctx, projectRoot, source, ev, now, shadowWindow)
	if err != nil {
		return ShadowGateResult{Err: fmt.Errorf("shadow gate: run: %w", err)}
	}

	if report.Empty {
		return ShadowGateResult{Report: report, SoftFail: true}
	}

	hardFail := report.FalseRejections > 0
	return ShadowGateResult{Report: report, HardFail: hardFail}
}

// summary returns a markdown block suitable for $GITHUB_STEP_SUMMARY.
func (r ShadowGateResult) summary() string {
	if r.Err != nil {
		return fmt.Sprintf("## ❌ Shadow Quality Gate — ERROR\n\n```\n%v\n```\n\n", r.Err)
	}

	if r.SoftFail {
		return fmt.Sprintf(
			"## ⚠️ Shadow Quality Gate — SKIPPED (no candidates)\n\n"+
				"The shadow source returned zero eligible recent cases "+
				"(window=%d). This is expected in CI environments without "+
				"production run history. Shadow evaluation requires a "+
				"populated runs database; run the gate locally against a "+
				"real database to validate replay behaviour.\n\n",
			r.Report.Window,
		)
	}

	status := "✅ PASS"
	if r.HardFail {
		status = "❌ FAIL"
	}

	s := fmt.Sprintf("## %s Shadow Quality Gate\n\n", status)
	s += r.Report.SummaryLine() + "\n\n"
	s += "| Metric | Value |\n|--------|-------|\n"
	s += fmt.Sprintf("| Window | %d |\n", r.Report.Window)
	s += fmt.Sprintf("| Evaluated | %d |\n", r.Report.Evaluated)
	s += fmt.Sprintf("| False Rejections | %d |\n", r.Report.FalseRejections)
	s += "\n"

	if r.HardFail {
		s += shadowFailedScenesSummary(r.Report)
	} else {
		// Log all cases on pass too so the developer sees drift even when not failing.
		for _, res := range r.Report.Results {
			s += "- " + res.LogLine() + "\n"
		}
		s += "\n"
	}

	return s
}

// shadowFailedScenesSummary emits the Failed Scenes Summary for a Shadow
// regression, one entry per false-rejection case.
func shadowFailedScenesSummary(report eval.ShadowReport) string {
	s := "### Failed Scenes Summary\n\n"
	s += "| Gate | Run ID | Baseline Verdict | New Verdict | Score Delta | Retry Reason |\n"
	s += "|------|--------|-----------------|-------------|-------------|---------------|\n"
	for _, res := range report.Results {
		if !res.FalseRejection {
			continue
		}
		s += fmt.Sprintf("| shadow | %s | %s | %s | %+.2f | %s |\n",
			res.RunID,
			res.BaselineVerdict,
			res.NewVerdict,
			res.Diff.Overall,
			res.NewRetryReason,
		)
	}
	s += "\n> Re-run locally with: `go test ./internal/critic/eval -run TestShadow -v`\n\n"

	// Append the canonical per-case log lines for all cases (drift included).
	s += "#### All Cases\n\n```\n"
	for _, res := range report.Results {
		s += res.LogLine() + "\n"
	}
	s += "```\n\n"
	return s
}
