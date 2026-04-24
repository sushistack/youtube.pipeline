package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// criticPromptRelPath is the canonical editable Critic prompt file.
// Kept in sync with internal/critic/eval.criticPromptRelPath — the eval
// package already owns the hash helper; duplicating the path here avoids a
// one-symbol export just for the prompt I/O surface.
const criticPromptRelPath = "docs/prompts/scenario/critic_agent.md"

// goldenFreshnessThresholdDays is the default staleness window. Matches the
// value baked into existing Golden runner tests.
const goldenFreshnessThresholdDays = 14

// fastFeedbackCorpusRelPath is the deterministic 10-sample corpus path.
// Story 10.2 AC-4 version-controls this file; the runner fails loudly if
// the count is not exactly fastFeedbackSampleCount.
const (
	fastFeedbackCorpusRelPath = "testdata/fixtures/fast_feedback/corpus.json"
	fastFeedbackSampleCount   = 10
)

// defaultCalibrationWindow and defaultCalibrationLimit are the fall-through
// values when the UI does not supply them via query parameters.
const (
	defaultCalibrationWindow = 20
	defaultCalibrationLimit  = 30
)

// CalibrationReader is the narrow interface the Tuning service needs from
// the calibration store. Defining it here keeps the import direction
// consumer → implementor and lets tests inject a fake without pulling in
// the full *db.CalibrationStore.
type CalibrationReader interface {
	RecentCriticCalibrationTrend(ctx context.Context, windowSize int, limit int) ([]domain.CriticCalibrationTrendPoint, error)
}

// ShadowSource is aliased locally so callers only import service types.
type ShadowSource = eval.ShadowSource

// PromptVersionProvider is the read-only surface RunService consumes to
// stamp newly-created runs with the active prompt version. TuningService
// implements it via ActivePromptVersion.
type PromptVersionProvider interface {
	ActivePromptVersion() *db.PromptVersionTag
}

// TuningService is the Story 10.2 service boundary. It owns prompt file
// I/O, wraps RunGolden/RunShadow, mediates fixture pair writes, and reads
// calibration trends. The Evaluator is intentionally injected: Story 10.2
// defines the API surface and mechanics, but a real Critic-backed
// evaluator is a separate concern and may be stubbed for V1.
type TuningService struct {
	projectRoot string
	evaluator   eval.Evaluator
	shadow      ShadowSource
	shadowWindow int
	calibration CalibrationReader
	clk         clock.Clock

	mu         sync.Mutex
	lastSaved  *db.PromptVersionTag
	lastSavedAt time.Time
}

// TuningServiceOptions groups constructor inputs. shadowWindow ≤ 0 falls
// back to 20, matching the existing `shadow_eval_window` config default.
type TuningServiceOptions struct {
	ProjectRoot  string
	Evaluator    eval.Evaluator
	ShadowSource ShadowSource
	ShadowWindow int
	Calibration  CalibrationReader
	Clock        clock.Clock
}

// NewTuningService constructs a TuningService. projectRoot, evaluator, and
// calibration are required; the Shadow source may be nil if the deployment
// has not wired Shadow yet (the endpoint will fail loudly in that case).
//
// Hydration on startup: when a prompt file already exists on disk, synthesize
// a PromptVersionTag from its current hash so AC-3 stamping keeps working
// across server restarts even if the operator has not re-saved the prompt in
// this process. Without hydration, new runs created after a restart would
// silently drop to NULL prompt_version/hash until the next explicit save.
func NewTuningService(opts TuningServiceOptions) *TuningService {
	clk := opts.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	window := opts.ShadowWindow
	if window <= 0 {
		window = 20
	}
	svc := &TuningService{
		projectRoot:  opts.ProjectRoot,
		evaluator:    opts.Evaluator,
		shadow:       opts.ShadowSource,
		shadowWindow: window,
		calibration:  opts.Calibration,
		clk:          clk,
	}
	svc.hydrateActivePromptVersion()
	return svc
}

// hydrateActivePromptVersion seeds lastSaved from the prompt file on disk.
// Uses a best-effort background context for gitShortSHA resolution; if the
// file is missing or unreadable, lastSaved stays nil and stamping falls back
// to the "no prompt saved this session" path, matching pre-hydration
// behavior.
func (s *TuningService) hydrateActivePromptVersion() {
	path := filepath.Join(s.projectRoot, criticPromptRelPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	now := s.clk.Now().UTC()
	hash := sha256Hex(raw)
	sha := gitShortSHA(context.Background(), s.projectRoot)
	s.lastSaved = &db.PromptVersionTag{Version: versionTag(now, sha), Hash: hash}
	s.lastSavedAt = now
}

// GetCriticPrompt reads the canonical prompt file and returns its contents
// plus the metadata the Tuning UI needs to display current version.
// Missing file is a hard error — Story 10.2 treats the prompt as a
// required artifact, not an optional one.
func (s *TuningService) GetCriticPrompt(ctx context.Context) (CriticPromptEnvelope, error) {
	path := filepath.Join(s.projectRoot, criticPromptRelPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		return CriticPromptEnvelope{}, fmt.Errorf("read critic prompt %s: %w", path, err)
	}
	hash := sha256Hex(raw)

	s.mu.Lock()
	saved := s.lastSaved
	savedAt := s.lastSavedAt
	s.mu.Unlock()

	var env CriticPromptEnvelope
	env.Body = string(raw)
	env.PromptHash = hash

	if saved != nil && saved.Hash == hash {
		env.VersionTag = saved.Version
		env.GitShortSHA = extractShortSHA(saved.Version)
		env.SavedAt = savedAt.UTC().Format(time.RFC3339)
	} else {
		env.GitShortSHA = gitShortSHA(ctx, s.projectRoot)
	}
	return env, nil
}

// SaveCriticPrompt overwrites the canonical prompt file with body and
// returns the metadata envelope for the new version. A trailing newline is
// appended if missing; otherwise body is preserved verbatim. Save also
// publishes the new PromptVersionTag so subsequent run creates can stamp
// it on the runs row (AC-3).
//
// The write is atomic and serialized: the whole operation runs under s.mu so
// concurrent saves do not interleave bytes on disk or produce a lastSaved
// hash that disagrees with the file contents, and the payload is staged to
// a sibling temp file + os.Rename so a mid-write crash cannot leave the
// canonical prompt file truncated.
func (s *TuningService) SaveCriticPrompt(ctx context.Context, body string) (CriticPromptEnvelope, error) {
	if strings.TrimSpace(body) == "" {
		return CriticPromptEnvelope{}, fmt.Errorf("critic prompt body is empty: %w", domain.ErrValidation)
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	path := filepath.Join(s.projectRoot, criticPromptRelPath)

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := atomicWriteFile(path, []byte(body), 0o644); err != nil {
		return CriticPromptEnvelope{}, fmt.Errorf("write critic prompt %s: %w", path, err)
	}
	now := s.clk.Now().UTC()
	hash := sha256Hex([]byte(body))
	sha := gitShortSHA(ctx, s.projectRoot)
	tag := versionTag(now, sha)

	s.lastSaved = &db.PromptVersionTag{Version: tag, Hash: hash}
	s.lastSavedAt = now

	return CriticPromptEnvelope{
		Body:        body,
		SavedAt:     now.Format(time.RFC3339),
		PromptHash:  hash,
		GitShortSHA: sha,
		VersionTag:  tag,
	}, nil
}

// atomicWriteFile writes data to a sibling temp file in the target directory
// and atomically renames it to path. The sibling-directory placement keeps
// the rename on the same filesystem so os.Rename is guaranteed atomic on the
// platforms the pipeline runs on.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}

// ActivePromptVersion returns the most recently saved prompt version tag,
// or nil if the process has not saved a prompt since startup. Safe for
// concurrent use from RunService.Create.
func (s *TuningService) ActivePromptVersion() *db.PromptVersionTag {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastSaved == nil {
		return nil
	}
	// Return a copy so callers cannot mutate the internal record.
	v := *s.lastSaved
	return &v
}

// GoldenState aggregates the manifest-backed pair list, freshness, and
// last-report values used by the Golden + Fixture Management sections.
func (s *TuningService) GoldenState(_ context.Context) (TuningGoldenState, error) {
	pairs, err := eval.ListPairs(s.projectRoot)
	if err != nil {
		return TuningGoldenState{}, fmt.Errorf("list golden pairs: %w", err)
	}
	out := TuningGoldenState{
		Pairs:     make([]TuningGoldenPair, 0, len(pairs)),
		PairCount: len(pairs),
	}
	for _, p := range pairs {
		out.Pairs = append(out.Pairs, TuningGoldenPair{
			Index:        p.Index,
			CreatedAt:    p.CreatedAt,
			PositivePath: p.PositivePath,
			NegativePath: p.NegativePath,
		})
	}
	fresh, err := eval.EvaluateFreshness(s.projectRoot, s.clk.Now(), goldenFreshnessThresholdDays)
	if err != nil {
		return TuningGoldenState{}, fmt.Errorf("golden freshness: %w", err)
	}
	warnings := fresh.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	out.Freshness = TuningGoldenFreshness{
		Warnings:          warnings,
		DaysSinceRefresh:  fresh.DaysSinceRefresh,
		PromptHashChanged: fresh.PromptHashChanged,
		CurrentPromptHash: fresh.CurrentPromptHash,
	}
	if report := manifestLastReport(s.projectRoot); report != nil {
		out.LastReport = report
	}
	return out, nil
}

// RunGolden delegates to eval.RunGolden and maps the result into the API
// type. Propagates evaluator/manifest errors verbatim so handlers can
// classify them via domain.Classify.
func (s *TuningService) RunGolden(ctx context.Context) (TuningGoldenReport, error) {
	if s.evaluator == nil {
		return TuningGoldenReport{}, fmt.Errorf("run golden: %w: evaluator not configured", domain.ErrValidation)
	}
	r, err := eval.RunGolden(ctx, s.projectRoot, s.evaluator, s.clk.Now())
	if err != nil {
		return TuningGoldenReport{}, fmt.Errorf("run golden: %w", err)
	}
	return TuningGoldenReport{
		Recall:           r.Recall,
		TotalNegative:    r.TotalNegative,
		DetectedNegative: r.DetectedNegative,
		FalseRejects:     r.FalseRejects,
	}, nil
}

// AddGoldenPair materializes two uploaded fixture files into a new pair
// directory and refreshes the manifest. Both inputs are required — AC-5
// forbids a single-fixture add path.
func (s *TuningService) AddGoldenPair(_ context.Context, positive, negative []byte) (TuningGoldenPair, error) {
	if len(positive) == 0 {
		return TuningGoldenPair{}, fmt.Errorf("add golden pair: positive fixture required: %w", domain.ErrValidation)
	}
	if len(negative) == 0 {
		return TuningGoldenPair{}, fmt.Errorf("add golden pair: negative fixture required: %w", domain.ErrValidation)
	}

	// eval.AddPair reads source paths, not byte slices. Materialize the
	// uploaded bytes into a temp file so the existing validation/normalization
	// pipeline (schema + kind check) applies unchanged. Temp files are
	// removed after AddPair returns regardless of outcome.
	posPath, posCleanup, err := writeTempFixture(positive, "positive-*.json")
	if err != nil {
		return TuningGoldenPair{}, fmt.Errorf("add golden pair: %w", err)
	}
	defer posCleanup()
	negPath, negCleanup, err := writeTempFixture(negative, "negative-*.json")
	if err != nil {
		return TuningGoldenPair{}, fmt.Errorf("add golden pair: %w", err)
	}
	defer negCleanup()

	meta, err := eval.AddPair(s.projectRoot, posPath, negPath, s.clk.Now())
	if err != nil {
		return TuningGoldenPair{}, fmt.Errorf("add golden pair: %w", err)
	}
	return TuningGoldenPair{
		Index:        meta.Index,
		CreatedAt:    meta.CreatedAt,
		PositivePath: meta.PositivePath,
		NegativePath: meta.NegativePath,
	}, nil
}

// RunShadow delegates to eval.RunShadow using the configured window.
// Regressions remain part of the returned report; only transport-level
// errors surface as errors.
func (s *TuningService) RunShadow(ctx context.Context) (TuningShadowReport, error) {
	if s.evaluator == nil {
		return TuningShadowReport{}, fmt.Errorf("run shadow: %w: evaluator not configured", domain.ErrValidation)
	}
	if s.shadow == nil {
		return TuningShadowReport{}, fmt.Errorf("run shadow: %w: shadow source not configured", domain.ErrValidation)
	}
	report, err := eval.RunShadow(ctx, s.projectRoot, s.shadow, s.evaluator, s.clk.Now(), s.shadowWindow)
	if err != nil {
		return TuningShadowReport{}, fmt.Errorf("run shadow: %w", err)
	}
	out := TuningShadowReport{
		Window:          report.Window,
		Evaluated:       report.Evaluated,
		FalseRejections: report.FalseRejections,
		Empty:           report.Empty,
		SummaryLine:     report.SummaryLine(),
		Results:         make([]TuningShadowResultRow, 0, len(report.Results)),
	}
	for _, r := range report.Results {
		out.Results = append(out.Results, TuningShadowResultRow{
			RunID:           r.RunID,
			CreatedAt:       r.CreatedAt,
			BaselineVerdict: r.BaselineVerdict,
			BaselineScore:   r.BaselineScore,
			NewVerdict:      r.NewVerdict,
			NewRetryReason:  r.NewRetryReason,
			NewOverallScore: r.NewOverallScore,
			OverallDiff:     r.Diff.Overall,
			FalseRejection:  r.FalseRejection,
		})
	}
	if tag := s.ActivePromptVersion(); tag != nil {
		out.VersionTag = tag.Version
	}
	return out, nil
}

// FastFeedback runs the deterministic 10-sample Critic evaluation pass
// and returns an aggregated report. Evaluator errors short-circuit the
// run — a single bad sample fails the whole call so the operator sees a
// clean failure instead of partial data that looks like a pass.
//
// The sample count is enforced at load time: AC-4 requires exactly
// fastFeedbackSampleCount samples in deterministic order, never fewer.
func (s *TuningService) FastFeedback(ctx context.Context) (FastFeedbackReport, error) {
	if s.evaluator == nil {
		return FastFeedbackReport{}, fmt.Errorf("fast feedback: %w: evaluator not configured", domain.ErrValidation)
	}
	fixtures, err := loadFastFeedbackCorpus(s.projectRoot)
	if err != nil {
		return FastFeedbackReport{}, fmt.Errorf("fast feedback: %w", err)
	}

	start := s.clk.Now()
	out := FastFeedbackReport{
		SampleCount: len(fixtures),
		Samples:     make([]FastFeedbackSample, 0, len(fixtures)),
	}
	for _, f := range fixtures {
		if err := ctx.Err(); err != nil {
			return FastFeedbackReport{}, fmt.Errorf("fast feedback cancelled: %w", err)
		}
		verdict, err := s.evaluator.Evaluate(ctx, f)
		if err != nil {
			return FastFeedbackReport{}, fmt.Errorf("fast feedback evaluate %s: %w", f.FixtureID, err)
		}
		switch verdict.Verdict {
		case domain.CriticVerdictPass:
			out.PassCount++
		case domain.CriticVerdictRetry:
			out.RetryCount++
		case domain.CriticVerdictAcceptWithNotes:
			out.AcceptNotesCount++
		}
		out.Samples = append(out.Samples, FastFeedbackSample{
			FixtureID:    f.FixtureID,
			Verdict:      verdict.Verdict,
			RetryReason:  verdict.RetryReason,
			OverallScore: verdict.OverallScore,
		})
	}
	out.DurationMs = s.clk.Now().Sub(start).Milliseconds()
	if tag := s.ActivePromptVersion(); tag != nil {
		out.VersionTag = tag.Version
	}
	return out, nil
}

// loadFastFeedbackCorpus reads and decodes the deterministic 10-sample
// corpus. Count is enforced strictly — AC-4 treats "fewer than 10
// samples" as a loud failure rather than a silent short corpus.
func loadFastFeedbackCorpus(projectRoot string) ([]eval.Fixture, error) {
	path := filepath.Join(projectRoot, fastFeedbackCorpusRelPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fast feedback corpus %s: %w", path, err)
	}
	var fixtures []eval.Fixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		return nil, fmt.Errorf("parse fast feedback corpus %s: %w", path, err)
	}
	if len(fixtures) != fastFeedbackSampleCount {
		return nil, fmt.Errorf(
			"fast feedback corpus must have exactly %d samples, got %d: %w",
			fastFeedbackSampleCount, len(fixtures), domain.ErrValidation,
		)
	}
	return fixtures, nil
}

// Calibration reads the persisted kappa trend. Both window and limit are
// clamped to sane positives so a careless query parameter does not
// short-circuit the DB read.
func (s *TuningService) Calibration(ctx context.Context, window, limit int) (TuningCalibration, error) {
	if s.calibration == nil {
		return TuningCalibration{}, fmt.Errorf("calibration: %w: store not configured", domain.ErrValidation)
	}
	if window <= 0 {
		window = defaultCalibrationWindow
	}
	if limit <= 0 {
		limit = defaultCalibrationLimit
	}
	points, err := s.calibration.RecentCriticCalibrationTrend(ctx, window, limit)
	if err != nil {
		return TuningCalibration{}, fmt.Errorf("calibration trend: %w", err)
	}
	out := TuningCalibration{
		Window: window,
		Limit:  limit,
		Points: make([]TuningCalibrationPoint, 0, len(points)),
	}
	for _, p := range points {
		out.Points = append(out.Points, TuningCalibrationPoint{
			ComputedAt:  p.ComputedAt,
			WindowCount: p.WindowCount,
			Provisional: p.Provisional,
			Kappa:       p.Kappa,
			Reason:      p.Reason,
		})
	}
	if n := len(out.Points); n > 0 {
		latest := out.Points[n-1]
		out.Latest = &latest
	}
	return out, nil
}

// sha256Hex returns the lowercase hex encoding of SHA-256(data).
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

// gitShortSHA resolves the short SHA of the current HEAD, falling back to
// "nogit" when git is unavailable or the working copy is not a repo. The
// context timeout prevents a hanging `git` from stalling the save.
func gitShortSHA(ctx context.Context, projectRoot string) string {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "-C", projectRoot, "rev-parse", "--short=7", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "nogit"
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "nogit"
	}
	return sha
}

// versionTag formats the canonical Story 10.2 version tag:
//
//	<utc-timestamp>-<git_short_sha>, e.g. 20260424T031522Z-f6b34b6
func versionTag(now time.Time, sha string) string {
	return fmt.Sprintf("%s-%s", now.UTC().Format("20060102T150405Z"), sha)
}

// extractShortSHA recovers the sha portion of a version tag. When the tag
// is malformed, returns the full tag to avoid lying about what was stored.
func extractShortSHA(tag string) string {
	if i := strings.LastIndex(tag, "-"); i >= 0 && i+1 < len(tag) {
		return tag[i+1:]
	}
	return tag
}

// writeTempFixture writes data into a temp file whose name matches pattern.
// The returned cleanup fn removes the file and is safe to call via defer.
func writeTempFixture(data []byte, pattern string) (string, func(), error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp fixture: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, fmt.Errorf("write temp fixture: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", func() {}, fmt.Errorf("close temp fixture: %w", err)
	}
	return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
}

// manifestLastReport reads the raw manifest and returns the embedded
// last_report if present. Errors are swallowed intentionally — a missing
// or malformed manifest should not turn the GET /golden call into a 500
// when the rest of the payload is still computable.
func manifestLastReport(projectRoot string) *TuningGoldenReport {
	raw, err := os.ReadFile(filepath.Join(projectRoot, "testdata", "golden", "eval", "manifest.json"))
	if err != nil {
		return nil
	}
	var m struct {
		LastReport *struct {
			Recall           float64 `json:"recall"`
			TotalNegative    int     `json:"total_negative"`
			DetectedNegative int     `json:"detected_negative"`
			FalseRejects     int     `json:"false_rejects"`
		} `json:"last_report"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	if m.LastReport == nil {
		return nil
	}
	return &TuningGoldenReport{
		Recall:           m.LastReport.Recall,
		TotalNegative:    m.LastReport.TotalNegative,
		DetectedNegative: m.LastReport.DetectedNegative,
		FalseRejects:     m.LastReport.FalseRejects,
	}
}
