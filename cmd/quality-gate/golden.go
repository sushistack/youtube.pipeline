package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
)

const goldenRecallThreshold = 0.80

// GoldenGateResult carries the outcome of one Golden quality-gate run.
type GoldenGateResult struct {
	Report     eval.Report
	PromptHash string
	Pass       bool
	Err        error
}

// runGoldenGate calls RunGolden and enforces the recall threshold. An error
// from RunGolden is itself a gate failure (broken fixtures, missing prompt
// file, manifest I/O failure).
func runGoldenGate(
	ctx context.Context,
	projectRoot string,
	ev eval.Evaluator,
	now time.Time,
) GoldenGateResult {
	hash, err := eval.CurrentCriticPromptHash(projectRoot)
	if err != nil {
		return GoldenGateResult{Err: fmt.Errorf("golden gate: hash prompt: %w", err)}
	}

	report, err := eval.RunGolden(ctx, projectRoot, ev, now)
	if err != nil {
		return GoldenGateResult{PromptHash: hash, Err: fmt.Errorf("golden gate: run: %w", err)}
	}

	pass := report.Recall >= goldenRecallThreshold
	return GoldenGateResult{Report: report, PromptHash: hash, Pass: pass}
}

// summary returns a markdown block suitable for $GITHUB_STEP_SUMMARY.
func (r GoldenGateResult) summary() string {
	if r.Err != nil {
		return fmt.Sprintf("## ❌ Golden Quality Gate — ERROR\n\n```\n%v\n```\n\n", r.Err)
	}

	status := "✅ PASS"
	if !r.Pass {
		status = "❌ FAIL"
	}

	s := fmt.Sprintf("## %s Golden Quality Gate\n\n", status)
	s += "| Metric | Value |\n|--------|-------|\n"
	s += fmt.Sprintf("| Recall | %.2f (threshold ≥ %.2f) |\n", r.Report.Recall, goldenRecallThreshold)
	s += fmt.Sprintf("| Total Negatives | %d |\n", r.Report.TotalNegative)
	s += fmt.Sprintf("| Detected Negatives | %d |\n", r.Report.DetectedNegative)
	s += fmt.Sprintf("| False Rejects | %d |\n", r.Report.FalseRejects)
	if r.PromptHash != "" {
		s += fmt.Sprintf("| Prompt Hash | `%s` |\n", r.PromptHash)
	}
	s += "\n"

	if !r.Pass {
		s += goldenFailedScenesSummary(r)
	}

	return s
}

// goldenFailedScenesSummary emits the Failed Scenes Summary for a Golden
// regression, listing each failing pair index with its expected vs actual verdict.
func goldenFailedScenesSummary(r GoldenGateResult) string {
	s := "### Failed Scenes Summary\n\n"

	var failed []eval.PairResult
	for _, pr := range r.Report.Pairs {
		if pr.NegMissed || pr.FalseReject {
			failed = append(failed, pr)
		}
	}
	if len(failed) > 0 {
		s += "| Gate | Pair Index | Expected Verdict | New Verdict | Issue |\n"
		s += "|------|-----------|-----------------|-------------|-------|\n"
		for _, pr := range failed {
			if pr.NegMissed {
				s += fmt.Sprintf("| golden | %d | retry | %s | negative not detected |\n",
					pr.Index, pr.NegVerdict)
			}
			if pr.FalseReject {
				s += fmt.Sprintf("| golden | %d | pass | %s | false rejection |\n",
					pr.Index, pr.PosVerdict)
			}
		}
		s += "\n"
	}

	s += "| Field | Value |\n|-------|-------|\n"
	s += "| Gate | golden |\n"
	s += fmt.Sprintf("| Recall | %.4f |\n", r.Report.Recall)
	s += fmt.Sprintf("| Expected Recall | ≥ %.2f |\n", goldenRecallThreshold)
	s += fmt.Sprintf("| Undetected Negatives | %d |\n",
		r.Report.TotalNegative-r.Report.DetectedNegative)
	s += fmt.Sprintf("| False Rejects | %d |\n", r.Report.FalseRejects)
	s += "\n> Re-run locally with: `go test ./internal/critic/eval -run TestGolden -v`\n\n"
	return s
}
