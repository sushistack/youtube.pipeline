package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// PairResult records the evaluation outcome for one Golden fixture pair.
type PairResult struct {
	Index       int    `json:"index"`
	NegVerdict  string `json:"neg_verdict"`
	PosVerdict  string `json:"pos_verdict"`
	NegMissed   bool   `json:"neg_missed,omitempty"`
	FalseReject bool   `json:"false_reject,omitempty"`
}

// Report is the outcome of a full Golden run.
//
// PerAct and Calibration are v2 (D5) additions. Both are emitted with
// omitempty so the v1 archive last_report snapshot under
// testdata/golden/eval/v1/last_report.json continues to round-trip cleanly
// — the v1 archive Report has only the four legacy fields + Pairs.
type Report struct {
	Recall           float64             `json:"recall"`
	TotalNegative    int                 `json:"total_negative"`
	DetectedNegative int                 `json:"detected_negative"`
	FalseRejects     int                 `json:"false_rejects"`
	Pairs            []PairResult        `json:"pairs,omitempty"`
	PerAct           *PerActAggregate    `json:"per_act,omitempty"`
	Calibration      *CalibrationSnapshot `json:"calibration,omitempty"`
}

// RunGolden evaluates all registered pairs and computes recall against the
// negative fixtures. On full success it updates the manifest with the run
// timestamp, prompt hash, and last_report.
func RunGolden(ctx context.Context, projectRoot string, evaluator Evaluator, now time.Time) (Report, error) {
	m, err := loadManifest(projectRoot)
	if err != nil {
		return Report{}, fmt.Errorf("load manifest: %w", err)
	}

	currentHash, err := CurrentCriticPromptHash(projectRoot)
	if err != nil {
		return Report{}, fmt.Errorf("hash critic prompt: %w", err)
	}

	if len(m.Pairs) == 0 {
		// An empty manifest is never a "successful run" — recall=0 from a
		// pair-less manifest is indistinguishable from a 100% false-rejection
		// run, which would cause downstream gates that key on `recall < X` to
		// misclassify both states. Surface this as ErrValidation rather than
		// silently writing a misleading last_report.
		return Report{}, fmt.Errorf("golden manifest has no pairs to evaluate: %w", domain.ErrValidation)
	}

	// Phase 1 — load every fixture pair and pre-compute per-act metrics
	// BEFORE invoking the evaluator. Per-act parsing is a JSON-shape gate
	// that costs nothing and catches stale v1-shape fixtures or hand-edited
	// corruption; failing fast here prevents a broken fixture in pair N from
	// burning evaluator budget on pairs 1..N-1 only to throw the work away.
	type loadedPair struct {
		entry          PairEntry
		pos, neg       Fixture
		posMet, negMet FixtureActReport
	}
	loaded := make([]loadedPair, 0, len(m.Pairs))
	for _, entry := range m.Pairs {
		pos, err := loadFixtureFile(projectRoot, entry.PositivePath)
		if err != nil {
			return Report{}, fmt.Errorf("pair %d positive: %w", entry.Index, err)
		}
		neg, err := loadFixtureFile(projectRoot, entry.NegativePath)
		if err != nil {
			return Report{}, fmt.Errorf("pair %d negative: %w", entry.Index, err)
		}
		posMet, err := computeFixtureActReport(pos)
		if err != nil {
			return Report{}, fmt.Errorf("pair %d positive per-act: %w", entry.Index, err)
		}
		negMet, err := computeFixtureActReport(neg)
		if err != nil {
			return Report{}, fmt.Errorf("pair %d negative per-act: %w", entry.Index, err)
		}
		loaded = append(loaded, loadedPair{
			entry: entry, pos: pos, neg: neg, posMet: posMet, negMet: negMet,
		})
	}

	// Phase 2 — drive the evaluator. By this point every fixture is known to
	// parse cleanly, so a downstream evaluator failure is a real evaluator
	// problem, not a fixture-shape problem.
	var report Report
	var fixtureReports []FixtureActReport
	for _, lp := range loaded {
		posResult, err := evaluator.Evaluate(ctx, lp.pos)
		if err != nil {
			return Report{}, fmt.Errorf("pair %d evaluate positive: %w", lp.entry.Index, err)
		}
		negResult, err := evaluator.Evaluate(ctx, lp.neg)
		if err != nil {
			return Report{}, fmt.Errorf("pair %d evaluate negative: %w", lp.entry.Index, err)
		}

		report.TotalNegative++
		negMissed := negResult.Verdict != "retry"
		falseReject := posResult.Verdict == "retry"
		if !negMissed {
			report.DetectedNegative++
		}
		if falseReject {
			report.FalseRejects++
		}
		report.Pairs = append(report.Pairs, PairResult{
			Index:       lp.entry.Index,
			NegVerdict:  negResult.Verdict,
			PosVerdict:  posResult.Verdict,
			NegMissed:   negMissed,
			FalseReject: falseReject,
		})
		fixtureReports = append(fixtureReports, lp.posMet, lp.negMet)
	}

	if report.TotalNegative > 0 {
		report.Recall = float64(report.DetectedNegative) / float64(report.TotalNegative)
	}

	if len(fixtureReports) > 0 {
		agg := AggregatePerAct(fixtureReports)
		report.PerAct = &agg
	}
	if len(report.Pairs) > 0 {
		cal := computeCalibration(report.Pairs)
		report.Calibration = &cal
	}

	ts := now.UTC().Truncate(time.Second)
	m.LastSuccessfulRunAt = &ts
	m.LastSuccessfulPromptHash = currentHash
	m.LastRefreshedAt = ts
	m.LastReport = &report

	if err := saveManifest(projectRoot, m); err != nil {
		return Report{}, fmt.Errorf("save manifest: %w", err)
	}

	return report, nil
}

func loadFixtureFile(projectRoot, relPath string) (Fixture, error) {
	path := filepath.Join(projectRoot, "testdata", "golden", relPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, fmt.Errorf("read fixture %s: %w", relPath, err)
	}
	var f Fixture
	if err := json.Unmarshal(data, &f); err != nil {
		return Fixture{}, fmt.Errorf("parse fixture %s: %w", relPath, err)
	}
	return f, nil
}
