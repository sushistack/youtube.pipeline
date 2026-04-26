// Shadow replay for the Critic — Story 4.2.
//
// Golden is file-backed baseline governance (version-controlled fixtures,
// durable manifest). Shadow is recent-run replay: it reads the most recent
// passed runs from the live `runs` table, replays each canonical persisted
// Critic input through the injected Evaluator, and reports verdict/score
// drift. Shadow intentionally remains ephemeral — it writes no manifest, no
// testdata artifacts, and no CLI command in this story. CI/pass-fail
// enforcement is owned by Story 10.4.

package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// knownShadowVerdicts is the allow-list of Critic verdict strings the Shadow
// runner recognizes. Anything outside this set is treated as a regression
// signal in its own right (a buggy evaluator returning "" or a new-taxonomy
// value must not be silently absorbed as drift).
var knownShadowVerdicts = map[string]struct{}{
	domain.CriticVerdictPass:            {},
	domain.CriticVerdictRetry:           {},
	domain.CriticVerdictAcceptWithNotes: {},
}

// ScoreDiff captures the delta between a replayed Critic result and the
// baseline recorded when the run originally completed. Rubric deltas are
// computed only when the baseline artifact actually stores rubric
// sub-scores; otherwise they remain zero (current-data-model limitation —
// V1 persists the cumulative run-level score but not the post-writer
// rubric breakdown, and fabricating baseline numbers would mask real
// regressions).
type ScoreDiff struct {
	Overall            float64
	Hook               int
	FactAccuracy       int
	EmotionalVariation int
	Immersion          int
}

// ShadowResult is the per-case outcome of a Shadow replay.
type ShadowResult struct {
	RunID             string
	CreatedAt         string
	BaselineVerdict   string
	BaselineScore     float64
	NewVerdict        string
	NewRetryReason    string
	NewOverallScore   int
	NewCriticModel    string
	NewCriticProvider string
	Diff              ScoreDiff
	FalseRejection    bool
}

// ShadowReport aggregates replay results for the full window.
//
// Empty is true when the source returned zero eligible cases — this is
// NOT the same as "10 cases replayed with 0 regressions" and a CI
// enforcer must be able to tell the two apart. Story 10.4 will decide
// how to surface it; Shadow itself just records the signal.
type ShadowReport struct {
	Window          int
	Evaluated       int
	FalseRejections int
	Empty           bool
	Results         []ShadowResult
}

// RunShadow pulls the most recent passed cases from source and replays each
// one through evaluator, returning a full drift report. Regressions are
// data, not runtime errors — RunShadow returns the report even when
// false-rejection cases exist so callers can inspect and log them. Story
// 10.4 owns CI enforcement.
func RunShadow(
	ctx context.Context,
	projectRoot string,
	outputDir string,
	source ShadowSource,
	evaluator Evaluator,
	now time.Time,
	window int,
) (ShadowReport, error) {
	_ = now // reserved for future time-based filtering; V1 trusts source ordering.

	if window <= 0 {
		return ShadowReport{}, fmt.Errorf("shadow window must be > 0, got %d: %w", window, domain.ErrValidation)
	}
	if source == nil {
		return ShadowReport{}, fmt.Errorf("shadow source is nil: %w", domain.ErrValidation)
	}
	if evaluator == nil {
		return ShadowReport{}, fmt.Errorf("shadow evaluator is nil: %w", domain.ErrValidation)
	}

	cases, err := source.RecentPassedCases(ctx, window)
	if err != nil {
		return ShadowReport{}, fmt.Errorf("shadow recent passed cases: %w", err)
	}

	report := ShadowReport{Window: window, Empty: len(cases) == 0}
	for _, c := range cases {
		// Honor cancellation between candidates — long windows under a CI
		// timeout must not continue doing work past the budget.
		if err := ctx.Err(); err != nil {
			return ShadowReport{}, fmt.Errorf("shadow run cancelled after %d/%d cases: %w",
				report.Evaluated, len(cases), err)
		}

		fixture, err := LoadShadowInput(projectRoot, outputDir, c)
		if err != nil {
			return ShadowReport{}, fmt.Errorf("shadow load input for %s: %w", c.RunID, err)
		}

		verdict, err := evaluator.Evaluate(ctx, fixture)
		if err != nil {
			return ShadowReport{}, fmt.Errorf("shadow evaluate %s: %w", c.RunID, err)
		}

		res := buildShadowResult(c, verdict)
		if res.FalseRejection {
			report.FalseRejections++
		}
		report.Results = append(report.Results, res)
		report.Evaluated++
	}
	return report, nil
}

func buildShadowResult(c ShadowCase, v VerdictResult) ShadowResult {
	overallScore := v.OverallScore
	normalized := normalizeCriticScore(overallScore)

	// V1 regression signal: a previously-pass case now returns "retry". Both
	// sides of the comparison matter — without the baseline guard, a future
	// source that yields a non-pass baseline would have every retry
	// misclassified as a false rejection. accept_with_notes is logged drift,
	// not a false rejection here. Any verdict outside the known taxonomy is
	// also flagged so a buggy evaluator returning "" or a new value cannot
	// masquerade as a non-regression.
	_, known := knownShadowVerdicts[v.Verdict]
	falseRejection := c.BaselineVerdict == domain.CriticVerdictPass &&
		(v.Verdict == domain.CriticVerdictRetry || !known)

	return ShadowResult{
		RunID:             c.RunID,
		CreatedAt:         c.CreatedAt,
		BaselineVerdict:   c.BaselineVerdict,
		BaselineScore:     c.BaselineScore,
		NewVerdict:        v.Verdict,
		NewRetryReason:    v.RetryReason,
		NewOverallScore:   overallScore,
		NewCriticModel:    v.Model,
		NewCriticProvider: v.Provider,
		Diff: ScoreDiff{
			Overall: normalized - c.BaselineScore,
		},
		FalseRejection: falseRejection,
	}
}

// LoadShadowInput reads the persisted Phase A artifact referenced by c and
// converts it into the same Fixture shape Golden already feeds the
// evaluator. Path resolution is production-safe: absolute paths are read
// directly; anything else is resolved relative to projectRoot. No import of
// internal/testutil — the loader must be callable from production code.
//
// The narration payload is validated against writer_output.schema.json —
// the same schema Golden's ValidateFixture enforces — so a corrupted
// artifact fails loudly at load time instead of feeding noise into the
// evaluator.
func LoadShadowInput(projectRoot, outputDir string, c ShadowCase) (Fixture, error) {
	if c.ScenarioPath == "" {
		return Fixture{}, fmt.Errorf("shadow case %s: empty scenario_path: %w", c.RunID, domain.ErrValidation)
	}

	path := c.ScenarioPath
	if !filepath.IsAbs(path) {
		// Reject `..` segments before joining: a malicious or corrupted
		// runs.scenario_path containing `../../..` would otherwise escape
		// outputDir / projectRoot and read arbitrary files.
		cleaned := filepath.Clean(path)
		for _, seg := range strings.Split(filepath.ToSlash(cleaned), "/") {
			if seg == ".." {
				return Fixture{}, fmt.Errorf("shadow case %s: scenario_path %q contains parent traversal: %w",
					c.RunID, c.ScenarioPath, domain.ErrValidation)
			}
		}
		path = cleaned

		// Live-run layout first ({outputDir}/{runID}/<path>); fall back to
		// projectRoot only when the run-relative artifact is genuinely
		// missing. A real I/O error (permission denied, broken volume) must
		// surface — silently swapping in a projectRoot path would either fail
		// schema validation loudly or, worse, validate against an unrelated
		// file and produce a meaningless verdict diff.
		if outputDir != "" {
			runRelative := filepath.Join(outputDir, c.RunID, path)
			info, err := os.Stat(runRelative)
			switch {
			case err == nil && !info.IsDir():
				path = runRelative
			case err == nil && info.IsDir():
				return Fixture{}, fmt.Errorf("shadow case %s: scenario_path %q resolved to a directory: %w",
					c.RunID, runRelative, domain.ErrValidation)
			case errors.Is(err, fs.ErrNotExist):
				path = filepath.Join(projectRoot, path)
			default:
				return Fixture{}, fmt.Errorf("stat scenario artifact %s: %w", runRelative, err)
			}
		} else {
			path = filepath.Join(projectRoot, path)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, fmt.Errorf("read scenario artifact %s: %w", path, err)
	}

	var envelope struct {
		Narration json.RawMessage `json:"narration"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Fixture{}, fmt.Errorf("parse scenario artifact %s: %w", path, err)
	}
	// json.RawMessage for an explicit null is the 4-byte literal "null", so a
	// length check alone would let {"narration": null} through. Reject it
	// explicitly — the field must be present AND a real narration payload.
	if len(envelope.Narration) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Narration), []byte("null")) {
		return Fixture{}, fmt.Errorf("scenario artifact %s: missing or null narration field: %w", path, domain.ErrValidation)
	}

	if err := validateAgainstSchema(projectRoot, inputSchemaFile, envelope.Narration); err != nil {
		return Fixture{}, fmt.Errorf("scenario artifact %s narration: %w", path, err)
	}

	return Fixture{
		FixtureID:       c.RunID,
		Kind:            kindPositive,
		Checkpoint:      checkpointPostWriter,
		Input:           envelope.Narration,
		ExpectedVerdict: verdictPass,
		Category:        "shadow_replay",
	}, nil
}

// SummaryLine is the one-line "shadow eval: window=… evaluated=…
// false_rejections=…" string. Go suppresses test stdout/stderr on pass
// unless -v, so the canonical human-inspection command is `go test
// ./internal/critic/eval -run Shadow -v` and the logger is `t.Logf`. The
// formatting lives here so tests and any future CLI wrapper share one
// shape.
func (r ShadowReport) SummaryLine() string {
	return fmt.Sprintf("shadow eval: window=%d evaluated=%d false_rejections=%d",
		r.Window, r.Evaluated, r.FalseRejections)
}

// LogLine is the per-candidate drift line. retry_reason is appended only
// when present because a blank retry_reason= key would misleadingly
// suggest the case reported a reason.
func (res ShadowResult) LogLine() string {
	line := fmt.Sprintf(
		"shadow eval case: run_id=%s baseline=%.2f verdict=%s overall=%d diff=%+.2f false_rejection=%t",
		res.RunID, res.BaselineScore, res.NewVerdict,
		res.NewOverallScore, res.Diff.Overall, res.FalseRejection,
	)
	if res.NewRetryReason != "" {
		line = fmt.Sprintf("%s retry_reason=%s", line, res.NewRetryReason)
	}
	return line
}

func normalizeCriticScore(overallScore int) float64 {
	v := float64(overallScore) / 100.0
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
