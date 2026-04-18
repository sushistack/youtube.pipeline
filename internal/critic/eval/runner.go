package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Report is the outcome of a full Golden run.
type Report struct {
	Recall           float64 `json:"recall"`
	TotalNegative    int     `json:"total_negative"`
	DetectedNegative int     `json:"detected_negative"`
	FalseRejects     int     `json:"false_rejects"`
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

	var report Report
	for _, entry := range m.Pairs {
		pos, err := loadFixtureFile(projectRoot, entry.PositivePath)
		if err != nil {
			return Report{}, fmt.Errorf("pair %d positive: %w", entry.Index, err)
		}
		neg, err := loadFixtureFile(projectRoot, entry.NegativePath)
		if err != nil {
			return Report{}, fmt.Errorf("pair %d negative: %w", entry.Index, err)
		}

		posResult, err := evaluator.Evaluate(ctx, pos)
		if err != nil {
			return Report{}, fmt.Errorf("pair %d evaluate positive: %w", entry.Index, err)
		}
		negResult, err := evaluator.Evaluate(ctx, neg)
		if err != nil {
			return Report{}, fmt.Errorf("pair %d evaluate negative: %w", entry.Index, err)
		}

		report.TotalNegative++
		if negResult.Verdict == "retry" {
			report.DetectedNegative++
		}
		if posResult.Verdict == "retry" {
			report.FalseRejects++
		}
	}

	if report.TotalNegative > 0 {
		report.Recall = float64(report.DetectedNegative) / float64(report.TotalNegative)
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
