package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// fakeShadowSource is the inline source used by Shadow unit tests. Tests
// pre-populate cases; production uses the SQLite-backed adapter in
// internal/db.
type fakeShadowSource struct {
	cases    []ShadowCase
	calls    int
	forceErr error
}

func (f *fakeShadowSource) RecentPassedCases(_ context.Context, limit int) ([]ShadowCase, error) {
	f.calls++
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	// Guard matches the production adapter's contract — negative limit is
	// never legal. RunShadow validates before calling, so this is reachable
	// only when a test bypasses validation; returning a slice expression with
	// a negative index would panic and mask the test's real intent.
	if limit < 0 {
		return nil, fmt.Errorf("fakeShadowSource: negative limit %d: %w", limit, domain.ErrValidation)
	}
	if limit > len(f.cases) {
		return f.cases, nil
	}
	return f.cases[:limit], nil
}

// fakeShadowEvaluator returns a verdict keyed by fixture ID (= run ID).
// The default verdict is "pass" with overall=90 so happy-path tests need
// only override the cases they care about.
type fakeShadowEvaluator struct {
	override map[string]VerdictResult
	calls    int
}

func (f *fakeShadowEvaluator) Evaluate(_ context.Context, fixture Fixture) (VerdictResult, error) {
	f.calls++
	if v, ok := f.override[fixture.FixtureID]; ok {
		return v, nil
	}
	return VerdictResult{Verdict: domain.CriticVerdictPass, OverallScore: 90}, nil
}

// newShadowTestRoot writes a scenario artifact at testdata/shadow_test/<id>/scenario.json
// under a fresh temp directory AND copies the writer_output schema so
// LoadShadowInput's new narration-schema validation has something to read.
func newShadowTestRoot(t *testing.T, runIDs ...string) string {
	t.Helper()
	root := t.TempDir()
	copyContractsSchema(t, root, inputSchemaFile)
	for _, id := range runIDs {
		writeShadowScenarioAt(t, root, filepath.Join("testdata", "shadow_test", id, "scenario.json"))
	}
	return root
}

// copyContractsSchema mirrors a single contracts file from the real
// project root into the temp root at the same relative path so the eval
// package's schema loader can find it without importing testutil into
// production.
func copyContractsSchema(t *testing.T, root, schemaFile string) {
	t.Helper()
	realRoot := testutil.ProjectRoot(t)
	src := filepath.Join(realRoot, "testdata", "contracts", schemaFile)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read real schema %s: %v", src, err)
	}
	dstDir := filepath.Join(root, "testdata", "contracts")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dstDir, err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, schemaFile), data, 0o644); err != nil {
		t.Fatalf("write schema %s: %v", schemaFile, err)
	}
}

// writeShadowScenarioAt writes a minimal-but-valid scenario.json at
// filepath.Join(root, rel).
func writeShadowScenarioAt(t *testing.T, root, rel string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", abs, err)
	}
	if err := os.WriteFile(abs, shadowScenarioJSON(), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
}

// shadowScenarioJSON returns a scenario.json payload with an 8-scene
// narration matching writer_output.schema.json — the exact Fixture.Input
// shape Golden already feeds the evaluator.
func shadowScenarioJSON() []byte {
	scenes := make([]map[string]any, 8)
	for i := range scenes {
		scenes[i] = map[string]any{
			"scene_num":          i + 1,
			"act_id":             fmt.Sprintf("act_%d", (i/2)+1),
			"narration":          "테스트 나레이션입니다.",
			"fact_tags":          []any{},
			"mood":               "neutral",
			"entity_visible":     false,
			"location":           "격리실",
			"characters_present": []string{"연구원"},
			"color_palette":      "gray",
			"atmosphere":         "calm",
		}
	}
	envelope := map[string]any{
		"run_id":      "shadow-test",
		"scp_id":      "SCP-SHADOW",
		"started_at":  "2026-04-16T09:00:00Z",
		"finished_at": "2026-04-16T09:10:00Z",
		"narration": map[string]any{
			"scp_id": "SCP-SHADOW",
			"title":  "shadow test",
			"scenes": scenes,
			"metadata": map[string]any{
				"language":                "ko",
				"scene_count":             8,
				"writer_model":            "deepseek-chat",
				"writer_provider":         "deepseek",
				"prompt_template":         "v1",
				"format_guide_template":   "v1",
				"forbidden_terms_version": "v1",
			},
			"source_version": "v1-llm-writer",
		},
	}
	b, _ := json.Marshal(envelope)
	return b
}

// ── LoadShadowInput -----------------------------------------------------

func TestLoadShadowInput_AbsolutePath(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// Artifact at an absolute path OUTSIDE projectRoot; projectRoot only
	// provides the writer_output schema for narration validation.
	artifactHome := t.TempDir()
	abs := filepath.Join(artifactHome, "shadow-abs", "scenario.json")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, shadowScenarioJSON(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	projectRoot := t.TempDir()
	copyContractsSchema(t, projectRoot, inputSchemaFile)

	f, err := LoadShadowInput(projectRoot, projectRoot, ShadowCase{
		RunID:        "scp-shadow-run-abs",
		ScenarioPath: abs,
	})
	if err != nil {
		t.Fatalf("LoadShadowInput: %v", err)
	}
	testutil.AssertEqual(t, "scp-shadow-run-abs", f.FixtureID)
	testutil.AssertEqual(t, kindPositive, f.Kind)
	testutil.AssertEqual(t, checkpointPostWriter, f.Checkpoint)
	testutil.AssertEqual(t, verdictPass, f.ExpectedVerdict)
	testutil.AssertEqual(t, "shadow_replay", f.Category)
}

func TestLoadShadowInput_RejectsNullNarration(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := t.TempDir()
	copyContractsSchema(t, root, inputSchemaFile)
	rel := filepath.Join("testdata", "shadow_test", "null-narration", "scenario.json")
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Field is present but explicitly null — json.RawMessage captures the
	// 4-byte literal "null". A len==0 check alone would accept it, which is
	// why we guard with an explicit bytes.Equal.
	if err := os.WriteFile(abs, []byte(`{"narration": null}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadShadowInput(root, root, ShadowCase{
		RunID:        "null-narration",
		ScenarioPath: rel,
	})
	if err == nil {
		t.Fatal("expected error for {\"narration\": null}, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestLoadShadowInput_RejectsSchemaViolatingNarration(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := t.TempDir()
	copyContractsSchema(t, root, inputSchemaFile)
	rel := filepath.Join("testdata", "shadow_test", "bad-shape", "scenario.json")
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Narration is a JSON object but missing every required writer_output
	// field. Without schema validation the evaluator would receive garbage
	// and return arbitrary verdicts — masking a regression as drift.
	if err := os.WriteFile(abs, []byte(`{"narration": {"nope": true}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadShadowInput(root, root, ShadowCase{
		RunID:        "bad-shape",
		ScenarioPath: rel,
	})
	if err == nil {
		t.Fatal("expected schema-violation error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestLoadShadowInput_ProjectRelativePath(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := newShadowTestRoot(t, "scp-shadow-run-rel")

	rel := filepath.Join("testdata", "shadow_test", "scp-shadow-run-rel", "scenario.json")
	f, err := LoadShadowInput(root, root, ShadowCase{
		RunID:        "scp-shadow-run-rel",
		ScenarioPath: rel,
	})
	if err != nil {
		t.Fatalf("LoadShadowInput: %v", err)
	}
	testutil.AssertEqual(t, "scp-shadow-run-rel", f.FixtureID)
	// Fixture.Input must be the narration payload exactly — ReusesGoldenFixtureShape
	// confirms the schema matches.
	var narration map[string]any
	if err := json.Unmarshal(f.Input, &narration); err != nil {
		t.Fatalf("unmarshal narration: %v", err)
	}
	if narration["source_version"] != "v1-llm-writer" {
		t.Errorf("expected source_version=v1-llm-writer, got %v", narration["source_version"])
	}
}

func TestLoadShadowInput_RejectsParentTraversal(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := newShadowTestRoot(t)

	// A scenario_path with `..` segments must be rejected before any join,
	// even if a file happens to exist at the resolved location: a malicious
	// or corrupted runs.scenario_path could otherwise read arbitrary files.
	for _, traversal := range []string{
		"../escape.json",
		"foo/../../bar.json",
		"..",
	} {
		_, err := LoadShadowInput(root, root, ShadowCase{
			RunID:        "evil-run",
			ScenarioPath: traversal,
		})
		if err == nil {
			t.Fatalf("LoadShadowInput accepted traversal path %q", traversal)
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("path %q: expected ErrValidation, got %v", traversal, err)
		}
	}
}

func TestLoadShadowInput_InvalidJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	rel := filepath.Join("testdata", "shadow_test", "broken", "scenario.json")
	abs := filepath.Join(tmp, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Invalid JSON must surface as a hard error. Silent skipping would hide
	// the regression signal Shadow exists to detect.
	_, err := LoadShadowInput(tmp, tmp, ShadowCase{
		RunID:        "broken",
		ScenarioPath: rel,
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoadShadowInput_ReusesGoldenFixtureShape(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := newShadowTestRoot(t, "scp-shadow-run-shape")

	rel := filepath.Join("testdata", "shadow_test", "scp-shadow-run-shape", "scenario.json")
	f, err := LoadShadowInput(root, root, ShadowCase{
		RunID:        "scp-shadow-run-shape",
		ScenarioPath: rel,
	})
	if err != nil {
		t.Fatalf("LoadShadowInput: %v", err)
	}

	// The whole point of AC-REPLAY-INPUT-REUSE: a Shadow fixture is bit-for-bit
	// a Golden post-writer Fixture. Validate it against the Golden fixture
	// schema so any drift between the two is a test failure.
	realRoot := testutil.ProjectRoot(t)
	envelope, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := validateAgainstSchema(realRoot, fixtureSchemaFile, envelope); err != nil {
		t.Fatalf("shadow fixture does not match Golden envelope schema: %v", err)
	}
	if err := validateAgainstSchema(realRoot, inputSchemaFile, []byte(f.Input)); err != nil {
		t.Fatalf("shadow fixture Input field does not match writer_output schema: %v", err)
	}
}

// ── RunShadow -----------------------------------------------------------

func TestRunShadow_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := newShadowTestRoot(t, "scp-run-01", "scp-run-02", "scp-run-03")

	src := &fakeShadowSource{cases: []ShadowCase{
		shadowCase("scp-run-01", 0.85),
		shadowCase("scp-run-02", 0.82),
		shadowCase("scp-run-03", 0.77),
	}}
	ev := &fakeShadowEvaluator{}

	report, err := RunShadow(context.Background(), root, root, src, ev, shadowTestNow, 10)
	if err != nil {
		t.Fatalf("RunShadow: %v", err)
	}
	testutil.AssertEqual(t, 10, report.Window)
	testutil.AssertEqual(t, 3, report.Evaluated)
	testutil.AssertEqual(t, 0, report.FalseRejections)
	testutil.AssertEqual(t, 1, src.calls) // source called exactly once.
	testutil.AssertEqual(t, 3, ev.calls)  // each candidate replayed exactly once.
	testutil.AssertEqual(t, 3, len(report.Results))
}

func TestRunShadow_CountsFalseRejections(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := newShadowTestRoot(t, "scp-run-01", "scp-run-02", "scp-run-03")

	src := &fakeShadowSource{cases: []ShadowCase{
		shadowCase("scp-run-01", 0.85),
		shadowCase("scp-run-02", 0.82),
		shadowCase("scp-run-03", 0.77),
	}}
	// Two of three previously-passed cases now return "retry" — this is the
	// exact condition Story 4.2 treats as a false-rejection regression.
	ev := &fakeShadowEvaluator{override: map[string]VerdictResult{
		"scp-run-01": {Verdict: domain.CriticVerdictRetry, RetryReason: "weak_hook", OverallScore: 62},
		"scp-run-02": {Verdict: domain.CriticVerdictRetry, RetryReason: "fact_error", OverallScore: 55},
	}}

	report, err := RunShadow(context.Background(), root, root, src, ev, shadowTestNow, 10)
	if err != nil {
		t.Fatalf("RunShadow: %v", err)
	}
	testutil.AssertEqual(t, 2, report.FalseRejections)

	// Non-retry cases are explicitly NOT flagged as false rejections.
	for _, res := range report.Results {
		switch res.RunID {
		case "scp-run-01", "scp-run-02":
			if !res.FalseRejection {
				t.Errorf("%s: expected FalseRejection=true", res.RunID)
			}
		case "scp-run-03":
			if res.FalseRejection {
				t.Errorf("%s: expected FalseRejection=false (still passes)", res.RunID)
			}
		}
	}
}

func TestRunShadow_AcceptWithNotesIsNotFalseRejection(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := newShadowTestRoot(t, "scp-run-01")

	src := &fakeShadowSource{cases: []ShadowCase{shadowCase("scp-run-01", 0.85)}}
	ev := &fakeShadowEvaluator{override: map[string]VerdictResult{
		// accept_with_notes is drift worth logging but not a regression
		// failure for this story — Story 10.4 decides CI behavior.
		"scp-run-01": {Verdict: domain.CriticVerdictAcceptWithNotes, OverallScore: 78},
	}}

	report, err := RunShadow(context.Background(), root, root, src, ev, shadowTestNow, 10)
	if err != nil {
		t.Fatalf("RunShadow: %v", err)
	}
	testutil.AssertEqual(t, 0, report.FalseRejections)
	testutil.AssertEqual(t, false, report.Results[0].FalseRejection)
	testutil.AssertEqual(t, domain.CriticVerdictAcceptWithNotes, report.Results[0].NewVerdict)
}

func TestRunShadow_OverallDiffUsesNormalizedScore(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := newShadowTestRoot(t, "scp-run-01")

	src := &fakeShadowSource{cases: []ShadowCase{shadowCase("scp-run-01", 0.81)}}
	ev := &fakeShadowEvaluator{override: map[string]VerdictResult{
		// new overall=62 → normalized 0.62; baseline 0.81 → diff -0.19.
		"scp-run-01": {Verdict: domain.CriticVerdictRetry, RetryReason: "weak_hook", OverallScore: 62},
	}}

	report, err := RunShadow(context.Background(), root, root, src, ev, shadowTestNow, 10)
	if err != nil {
		t.Fatalf("RunShadow: %v", err)
	}
	testutil.AssertFloatNear(t, report.Results[0].Diff.Overall, -0.19, 1e-9)
	testutil.AssertEqual(t, 62, report.Results[0].NewOverallScore)
}

func TestRunShadow_EmptyResultSetMarksEmpty(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := newShadowTestRoot(t)

	// Source returns zero eligible cases. Without the Empty flag, a CI
	// enforcer cannot distinguish this from "10 cases replayed with no
	// regressions" — both look like false_rejections=0.
	src := &fakeShadowSource{cases: nil}
	ev := &fakeShadowEvaluator{}

	report, err := RunShadow(context.Background(), root, root, src, ev, shadowTestNow, 10)
	if err != nil {
		t.Fatalf("RunShadow: %v", err)
	}
	testutil.AssertEqual(t, 0, report.Evaluated)
	testutil.AssertEqual(t, 0, report.FalseRejections)
	testutil.AssertEqual(t, true, report.Empty)
}

func TestRunShadow_NonEmptyResultSetLeavesEmptyFalse(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := newShadowTestRoot(t, "scp-run-01")

	src := &fakeShadowSource{cases: []ShadowCase{shadowCase("scp-run-01", 0.85)}}
	ev := &fakeShadowEvaluator{}

	report, err := RunShadow(context.Background(), root, root, src, ev, shadowTestNow, 10)
	if err != nil {
		t.Fatalf("RunShadow: %v", err)
	}
	testutil.AssertEqual(t, false, report.Empty)
}

func TestRunShadow_UnknownVerdictFlaggedAsFalseRejection(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := newShadowTestRoot(t, "scp-run-01", "scp-run-02")

	src := &fakeShadowSource{cases: []ShadowCase{
		shadowCase("scp-run-01", 0.85),
		shadowCase("scp-run-02", 0.82),
	}}
	// An evaluator that forgets to set Verdict (zero value "") OR produces a
	// new-taxonomy value must not be silently absorbed as non-regression
	// drift — that defeats Shadow's entire reason for existing.
	ev := &fakeShadowEvaluator{override: map[string]VerdictResult{
		"scp-run-01": {Verdict: "", OverallScore: 0},
		"scp-run-02": {Verdict: "hard_fail", OverallScore: 30},
	}}

	report, err := RunShadow(context.Background(), root, root, src, ev, shadowTestNow, 10)
	if err != nil {
		t.Fatalf("RunShadow: %v", err)
	}
	testutil.AssertEqual(t, 2, report.FalseRejections)
	for _, res := range report.Results {
		if !res.FalseRejection {
			t.Errorf("%s with verdict %q must be flagged as false rejection", res.RunID, res.NewVerdict)
		}
	}
}

func TestRunShadow_HonorsContextCancellation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := newShadowTestRoot(t, "scp-run-01", "scp-run-02", "scp-run-03")

	src := &fakeShadowSource{cases: []ShadowCase{
		shadowCase("scp-run-01", 0.85),
		shadowCase("scp-run-02", 0.82),
		shadowCase("scp-run-03", 0.77),
	}}
	ev := &fakeShadowEvaluator{}

	// Cancel BEFORE the loop starts so the first iteration observes it. Long
	// windows under a CI timeout must not continue doing work past the
	// budget.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RunShadow(ctx, root, root, src, ev, shadowTestNow, 10)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled in chain, got %v", err)
	}
}

func TestRunShadow_RejectsInvalidWindow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	src := &fakeShadowSource{}
	ev := &fakeShadowEvaluator{}

	_, err := RunShadow(context.Background(), "", "", src, ev, shadowTestNow, 0)
	if err == nil {
		t.Fatal("expected validation error for window=0")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}

	_, err = RunShadow(context.Background(), "", "", src, ev, shadowTestNow, -1)
	if err == nil {
		t.Fatal("expected validation error for negative window")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}

	// Source must not have been invoked when validation fails.
	testutil.AssertEqual(t, 0, src.calls)
}

// ── Logging (AC-VERBOSE-LOGGING-FOR-GO-TEST) ----------------------------

func TestShadow_ReportLogsSummary(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	report := ShadowReport{Window: 10, Evaluated: 10, FalseRejections: 1}
	line := report.SummaryLine()

	for _, want := range []string{
		"shadow eval:",
		"window=10",
		"evaluated=10",
		"false_rejections=1",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("summary line missing %q: %s", want, line)
		}
	}
	t.Log(line)
}

func TestShadow_ReportLogsPerCaseDiff(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	res := ShadowResult{
		RunID:           "scp-049-run-12",
		BaselineScore:   0.81,
		NewVerdict:      domain.CriticVerdictRetry,
		NewOverallScore: 62,
		NewRetryReason:  "weak_hook",
		Diff:            ScoreDiff{Overall: -0.19},
		FalseRejection:  true,
	}
	line := res.LogLine()

	for _, want := range []string{
		"shadow eval case:",
		"run_id=scp-049-run-12",
		"baseline=0.81",
		"verdict=retry",
		"overall=62",
		"diff=-0.19",
		"false_rejection=true",
		"retry_reason=weak_hook",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("case line missing %q: %s", want, line)
		}
	}
	t.Log(line)

	// Passing case with no retry reason must NOT emit a blank retry_reason= key.
	pass := ShadowResult{
		RunID:          "scp-049-run-13",
		BaselineScore:  0.85,
		NewVerdict:     domain.CriticVerdictPass,
		Diff:           ScoreDiff{Overall: 0.01},
		FalseRejection: false,
	}
	if strings.Contains(pass.LogLine(), "retry_reason=") {
		t.Errorf("passing case should omit retry_reason key, got: %s", pass.LogLine())
	}
}

// ── helpers -------------------------------------------------------------

var shadowTestNow = time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)

// shadowCase builds a ShadowCase whose scenario_path points at the
// newShadowTestRoot layout, so RunShadow tests resolve the artifact
// without having to spell out the relative path at every call site.
func shadowCase(runID string, baseline float64) ShadowCase {
	return ShadowCase{
		RunID:           runID,
		CreatedAt:       "2026-04-18 09:00:00",
		ScenarioPath:    filepath.Join("testdata", "shadow_test", runID, "scenario.json"),
		BaselineScore:   baseline,
		BaselineVerdict: "pass",
	}
}
