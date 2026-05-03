package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

var gateTestNow = time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)

// ── test evaluators ───────────────────────────────────────────────────────────

// controlledEvaluator returns overridden verdicts by fixture ID; falls back to
// deflt when set, otherwise defaulting negative→retry, positive→pass.
type controlledEvaluator struct {
	override map[string]eval.VerdictResult
	deflt    eval.VerdictResult
}

func (e *controlledEvaluator) Evaluate(_ context.Context, f eval.Fixture) (eval.VerdictResult, error) {
	if e.override != nil {
		if v, ok := e.override[f.FixtureID]; ok {
			return v, nil
		}
	}
	if e.deflt.Verdict != "" {
		return e.deflt, nil
	}
	if f.Kind == "negative" {
		return eval.VerdictResult{Verdict: "retry"}, nil
	}
	return eval.VerdictResult{Verdict: "pass"}, nil
}

type errorEvaluator struct{ msg string }

func (e errorEvaluator) Evaluate(_ context.Context, _ eval.Fixture) (eval.VerdictResult, error) {
	return eval.VerdictResult{}, fmt.Errorf("%s", e.msg)
}

// passEvaluator always returns "pass" — used to make all Shadow cases
// pass (no false rejections).
type passEvaluator struct{}

func (passEvaluator) Evaluate(_ context.Context, _ eval.Fixture) (eval.VerdictResult, error) {
	return eval.VerdictResult{Verdict: "pass", OverallScore: 80}, nil
}

// retryEvaluator always returns "retry" — triggers false rejection for every
// case that had a "pass" baseline.
type retryEvaluator struct{}

func (retryEvaluator) Evaluate(_ context.Context, _ eval.Fixture) (eval.VerdictResult, error) {
	return eval.VerdictResult{Verdict: "retry", RetryReason: "weak_hook", OverallScore: 40}, nil
}

// ── test shadow source ────────────────────────────────────────────────────────

type staticShadowSource struct{ cases []eval.ShadowCase }

func (s *staticShadowSource) RecentPassedCases(_ context.Context, limit int) ([]eval.ShadowCase, error) {
	if limit >= len(s.cases) {
		return s.cases, nil
	}
	return s.cases[:limit], nil
}

// ── golden gate root helpers ──────────────────────────────────────────────────

// buildGoldenRoot creates a self-contained temp root with pairs golden pairs
// and all schema/prompt files required by RunGolden.
func buildGoldenRoot(t *testing.T, pairs int) string {
	t.Helper()
	realRoot := testutil.ProjectRoot(t)
	tmp := t.TempDir()

	mkdirAll(t, tmp, filepath.Join("testdata", "golden", "eval"))
	mkdirAll(t, tmp, filepath.Join("testdata", "contracts"))
	mkdirAll(t, tmp, filepath.Join("docs", "prompts", "scenario"))

	for _, schema := range []string{
		"golden_eval_fixture.schema.json",
		"writer_output.schema.json",
		"golden_eval_manifest.schema.json",
	} {
		copyFileRel(t, realRoot, tmp, filepath.Join("testdata", "contracts", schema))
	}
	copyFileRel(t, realRoot, tmp, filepath.Join("docs", "prompts", "scenario", "critic_agent.md"))

	writeEmptyManifest(t, tmp)
	for i := 1; i <= pairs; i++ {
		addGoldenPair(t, tmp, i)
	}
	return tmp
}

func mkdirAll(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
		t.Fatalf("mkdirAll %s: %v", rel, err)
	}
}

func copyFileRel(t *testing.T, srcRoot, dstRoot, rel string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(srcRoot, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if err := os.WriteFile(filepath.Join(dstRoot, rel), data, 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func writeEmptyManifest(t *testing.T, root string) {
	t.Helper()
	m := map[string]any{
		"version":          1,
		"next_index":       1,
		"last_refreshed_at": "2026-04-18T10:00:00Z",
		"pairs":            []any{},
	}
	writeJSON(t, filepath.Join(root, "testdata", "golden", "eval", "manifest.json"), m)
}

// addGoldenPair writes one positive + one negative fixture and appends the
// entry to manifest.json.
func addGoldenPair(t *testing.T, root string, idx int) {
	t.Helper()
	dirName := fmt.Sprintf("%06d", idx)
	dir := filepath.Join(root, "testdata", "golden", "eval", dirName)
	mkdirAll(t, root, filepath.Join("testdata", "golden", "eval", dirName))

	posID := fmt.Sprintf("gate-pos-%03d", idx)
	negID := fmt.Sprintf("gate-neg-%03d", idx)

	writeJSON(t, filepath.Join(dir, "positive.json"), map[string]any{
		"fixture_id":       posID,
		"kind":             "positive",
		"checkpoint":       "post_writer",
		"input":            json.RawMessage(minimalNarrationJSON()),
		"expected_verdict": "pass",
		"category":         "known_pass",
	})
	writeJSON(t, filepath.Join(dir, "negative.json"), map[string]any{
		"fixture_id":       negID,
		"kind":             "negative",
		"checkpoint":       "post_writer",
		"input":            json.RawMessage(minimalNarrationJSON()),
		"expected_verdict": "retry",
		"category":         "fact_error",
	})

	// Append pair to manifest.
	manifestPath := filepath.Join(root, "testdata", "golden", "eval", "manifest.json")
	var raw map[string]any
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	pairs, _ := raw["pairs"].([]any)
	pairs = append(pairs, map[string]any{
		"index":         idx,
		"created_at":    "2026-04-18T10:00:00Z",
		"positive_path": fmt.Sprintf("eval/%s/positive.json", dirName),
		"negative_path": fmt.Sprintf("eval/%s/negative.json", dirName),
	})
	raw["pairs"] = pairs
	raw["next_index"] = idx + 1
	writeJSON(t, manifestPath, raw)
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// minimalNarrationJSON returns a valid writer_output payload as raw JSON bytes.
func minimalNarrationJSON() []byte {
	scenes := make([]map[string]any, 8)
	for i := range scenes {
		scenes[i] = map[string]any{
			"scene_num":          i + 1,
			"act_id":             fmt.Sprintf("act_%d", (i/2)+1),
			"narration":          "테스트 나레이션입니다.",
			"narration_beats":    []string{"테스트 나레이션입니다."},
			"fact_tags":          []any{},
			"mood":               "neutral",
			"entity_visible":     false,
			"location":           "격리실",
			"characters_present": []string{"연구원"},
			"color_palette":      "gray",
			"atmosphere":         "calm",
		}
	}
	payload := map[string]any{
		"scp_id": "SCP-TEST",
		"title":  "테스트 시나리오",
		"scenes": scenes,
		"metadata": map[string]any{
			"language":                "ko",
			"scene_count":             8,
			"writer_model":            "deepseek-v4-flash",
			"writer_provider":         "deepseek",
			"prompt_template":         "v1-scenario-writer",
			"format_guide_template":   "v1-format-guide",
			"forbidden_terms_version": "v1",
		},
		"source_version": "v1-llm-writer",
	}
	b, _ := json.Marshal(payload)
	return b
}

// ── shadow root helper ────────────────────────────────────────────────────────

// buildShadowRoot creates a temp root with the writer_output schema and N
// scenario artifacts, returning the root and the ShadowCase slice.
func buildShadowRoot(t *testing.T, runIDs ...string) (string, []eval.ShadowCase) {
	t.Helper()
	realRoot := testutil.ProjectRoot(t)
	tmp := t.TempDir()
	mkdirAll(t, tmp, filepath.Join("testdata", "contracts"))
	copyFileRel(t, realRoot, tmp, filepath.Join("testdata", "contracts", "writer_output.schema.json"))

	var cases []eval.ShadowCase
	for _, id := range runIDs {
		rel := filepath.Join("testdata", "shadow_test", id, "scenario.json")
		abs := filepath.Join(tmp, rel)
		mkdirAll(t, tmp, filepath.Dir(rel))
		if err := os.WriteFile(abs, shadowScenarioJSON(id), 0o644); err != nil {
			t.Fatalf("write scenario %s: %v", id, err)
		}
		cases = append(cases, eval.ShadowCase{
			RunID:           id,
			CreatedAt:       "2026-04-18 09:00:00",
			ScenarioPath:    rel,
			BaselineScore:   0.82,
			BaselineVerdict: "pass",
		})
	}
	return tmp, cases
}

func shadowScenarioJSON(runID string) []byte {
	scenes := make([]map[string]any, 8)
	for i := range scenes {
		scenes[i] = map[string]any{
			"scene_num":          i + 1,
			"act_id":             fmt.Sprintf("act_%d", (i/2)+1),
			"narration":          "테스트 나레이션입니다.",
			"narration_beats":    []string{"테스트 나레이션입니다."},
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
		"run_id":      runID,
		"scp_id":      "SCP-SHADOW",
		"started_at":  "2026-04-18T09:00:00Z",
		"finished_at": "2026-04-18T09:10:00Z",
		"narration": map[string]any{
			"scp_id": "SCP-SHADOW",
			"title":  "shadow test",
			"scenes": scenes,
			"metadata": map[string]any{
				"language":                "ko",
				"scene_count":             8,
				"writer_model":            "deepseek-v4-flash",
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

// ── Golden gate tests ─────────────────────────────────────────────────────────

// TestGoldenGate_RecallAboveThresholdPasses verifies recall ≥ 0.80 → Pass=true.
// 2 pairs, both negatives detected → recall = 1.0.
func TestGoldenGate_RecallAboveThresholdPasses(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := buildGoldenRoot(t, 2)

	result := runGoldenGate(context.Background(), root, &controlledEvaluator{}, gateTestNow)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !result.Pass {
		t.Errorf("expected Pass=true for recall=%.2f, got false", result.Report.Recall)
	}
	if result.Report.Recall < goldenRecallThreshold {
		t.Errorf("recall %.4f below threshold %.2f", result.Report.Recall, goldenRecallThreshold)
	}
}

// TestGoldenGate_RecallBelowThresholdFails verifies recall < 0.80 → Pass=false.
// 5 pairs; override all negatives to return "pass" (missed detection) → recall = 0.
func TestGoldenGate_RecallBelowThresholdFails(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := buildGoldenRoot(t, 5)

	// Force negatives to return "pass" — the gate should miss all of them.
	ev := &controlledEvaluator{
		override: map[string]eval.VerdictResult{
			"gate-neg-001": {Verdict: "pass"},
			"gate-neg-002": {Verdict: "pass"},
			"gate-neg-003": {Verdict: "pass"},
			"gate-neg-004": {Verdict: "pass"},
			"gate-neg-005": {Verdict: "pass"},
		},
	}
	result := runGoldenGate(context.Background(), root, ev, gateTestNow)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Pass {
		t.Errorf("expected Pass=false for recall=0, got true")
	}
	if result.Report.Recall >= goldenRecallThreshold {
		t.Errorf("expected recall < %.2f, got %.4f", goldenRecallThreshold, result.Report.Recall)
	}
}

// TestGoldenGate_ExactThresholdPasses verifies recall = 0.80 exactly → Pass=true.
// 5 negatives, 4 detected → recall = 0.80 (boundary condition).
func TestGoldenGate_ExactThresholdPasses(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := buildGoldenRoot(t, 5)

	// Let 4 of 5 negatives be detected (one missed).
	ev := &controlledEvaluator{
		override: map[string]eval.VerdictResult{
			"gate-neg-005": {Verdict: "pass"}, // missed
		},
	}
	result := runGoldenGate(context.Background(), root, ev, gateTestNow)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !result.Pass {
		t.Errorf("recall=0.80 must pass gate, got Pass=false (recall=%.4f)", result.Report.Recall)
	}
}

// TestGoldenGate_JustBelowThresholdFails verifies recall < 0.80 → Pass=false.
// With 5 pairs and 2 missed negatives, 3/5 detected → recall = 0.60 < 0.80.
func TestGoldenGate_JustBelowThresholdFails(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := buildGoldenRoot(t, 5)

	// 3 out of 5 negatives detected (2 missed) → recall = 0.60 < 0.80.
	ev := &controlledEvaluator{
		override: map[string]eval.VerdictResult{
			"gate-neg-004": {Verdict: "pass"},
			"gate-neg-005": {Verdict: "pass"},
		},
	}
	result := runGoldenGate(context.Background(), root, ev, gateTestNow)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Pass {
		t.Errorf("recall=0.60 must fail gate, got Pass=true")
	}
}

// TestGoldenGate_EvaluatorErrorFails verifies an evaluator error → gate failure.
func TestGoldenGate_EvaluatorErrorFails(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := buildGoldenRoot(t, 1)

	result := runGoldenGate(context.Background(), root, errorEvaluator{"critic offline"}, gateTestNow)
	if result.Err == nil {
		t.Fatal("expected error from errorEvaluator, got nil")
	}
	if result.Pass {
		t.Error("gate must fail when evaluator errors")
	}
}

// TestGoldenGate_PromptHashIncludedInSummary verifies the step summary contains
// a prompt hash.
func TestGoldenGate_PromptHashIncludedInSummary(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := buildGoldenRoot(t, 1)

	result := runGoldenGate(context.Background(), root, &controlledEvaluator{}, gateTestNow)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.PromptHash == "" {
		t.Error("expected non-empty PromptHash in gate result")
	}
	summary := result.summary()
	if !strings.Contains(summary, result.PromptHash) {
		t.Errorf("step summary missing prompt hash %q", result.PromptHash)
	}
}

// TestGoldenGate_FailedScenesSummaryOnRegression verifies the Failed Scenes
// Summary section appears when the gate fails.
func TestGoldenGate_FailedScenesSummaryOnRegression(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := buildGoldenRoot(t, 2)

	ev := &controlledEvaluator{
		override: map[string]eval.VerdictResult{
			"gate-neg-001": {Verdict: "pass"},
			"gate-neg-002": {Verdict: "pass"},
		},
	}
	result := runGoldenGate(context.Background(), root, ev, gateTestNow)
	if result.Pass {
		t.Fatal("expected gate failure for this test to be meaningful")
	}
	summary := result.summary()
	if !strings.Contains(summary, "Failed Scenes Summary") {
		t.Error("summary missing 'Failed Scenes Summary' section on regression")
	}
	for _, want := range []string{"golden", "Recall", "Expected Recall"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q", want)
		}
	}
}

// TestGoldenGate_ErrorSummaryContainsErrorText verifies error details appear in
// the step summary.
func TestGoldenGate_ErrorSummaryContainsErrorText(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := buildGoldenRoot(t, 1)

	result := runGoldenGate(context.Background(), root, errorEvaluator{"llm_error_XYZ"}, gateTestNow)
	summary := result.summary()
	if !strings.Contains(summary, "ERROR") {
		t.Error("summary missing ERROR marker")
	}
	if !strings.Contains(summary, "llm_error_XYZ") {
		t.Error("summary missing original error text")
	}
}

// ── Shadow gate tests ─────────────────────────────────────────────────────────

// TestShadowGate_ZeroFalseRejectionsPasses verifies false_rejections=0 → HardFail=false.
func TestShadowGate_ZeroFalseRejectionsPasses(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root, cases := buildShadowRoot(t, "run-01", "run-02", "run-03")
	src := &staticShadowSource{cases: cases}

	result := runShadowGate(context.Background(), root, src, passEvaluator{}, gateTestNow)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.HardFail {
		t.Error("expected HardFail=false with zero false rejections")
	}
	if result.SoftFail {
		t.Error("expected SoftFail=false (candidates present)")
	}
}

// TestShadowGate_FalseRejectionsFail verifies false_rejections>0 → HardFail=true.
func TestShadowGate_FalseRejectionsFail(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root, cases := buildShadowRoot(t, "run-01", "run-02")
	src := &staticShadowSource{cases: cases}

	result := runShadowGate(context.Background(), root, src, retryEvaluator{}, gateTestNow)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !result.HardFail {
		t.Errorf("expected HardFail=true, got false (false_rejections=%d)", result.Report.FalseRejections)
	}
}

// TestShadowGate_EmptyWindowSoftFails verifies ShadowReport.Empty → SoftFail=true, HardFail=false.
func TestShadowGate_EmptyWindowSoftFails(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root, _ := buildShadowRoot(t) // no scenarios
	src := &staticShadowSource{cases: nil}

	result := runShadowGate(context.Background(), root, src, passEvaluator{}, gateTestNow)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !result.SoftFail {
		t.Error("expected SoftFail=true for empty candidate set")
	}
	if result.HardFail {
		t.Error("expected HardFail=false for empty candidate set")
	}
}

// TestShadowGate_EmptySummaryExplainsNoData verifies the step summary surfaces the
// zero-candidates situation with actionable text.
func TestShadowGate_EmptySummaryExplainsNoData(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root, _ := buildShadowRoot(t)
	src := &staticShadowSource{cases: nil}

	result := runShadowGate(context.Background(), root, src, passEvaluator{}, gateTestNow)
	summary := result.summary()
	for _, want := range []string{"SKIPPED", "zero eligible", "window="} {
		if !strings.Contains(summary, want) {
			t.Errorf("empty shadow summary missing %q", want)
		}
	}
}

// TestShadowGate_AcceptWithNotesDoesNotFail verifies accept_with_notes drift is
// logged but does not cause HardFail.
func TestShadowGate_AcceptWithNotesDoesNotFail(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root, cases := buildShadowRoot(t, "run-01")
	src := &staticShadowSource{cases: cases}

	awne := &controlledEvaluator{deflt: eval.VerdictResult{
		Verdict: "accept_with_notes", OverallScore: 75,
	}}
	result := runShadowGate(context.Background(), root, src, awne, gateTestNow)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.HardFail {
		t.Error("accept_with_notes must not cause HardFail")
	}
}

// TestShadowGate_FailedScenesSummaryOnRegression verifies the Failed Scenes
// Summary table appears when HardFail=true, including run ID and verdict fields.
func TestShadowGate_FailedScenesSummaryOnRegression(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root, cases := buildShadowRoot(t, "run-fail-01", "run-fail-02")
	src := &staticShadowSource{cases: cases}

	result := runShadowGate(context.Background(), root, src, retryEvaluator{}, gateTestNow)
	if !result.HardFail {
		t.Fatal("expected HardFail for this test to be meaningful")
	}
	summary := result.summary()
	for _, want := range []string{
		"Failed Scenes Summary",
		"run-fail-01",
		"run-fail-02",
		"shadow",
		"pass",  // BaselineVerdict
		"retry", // NewVerdict
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("shadow failure summary missing %q", want)
		}
	}
}

// TestShadowGate_NullSourceProducesEmpty exercises the production CI path
// where no database is available.
func TestShadowGate_NullSourceProducesEmpty(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root, _ := buildShadowRoot(t)

	result := runShadowGate(context.Background(), root, nullShadowSource{}, passEvaluator{}, gateTestNow)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !result.SoftFail {
		t.Error("nullShadowSource must produce SoftFail=true")
	}
}

// ── watchedPaths documentation test ──────────────────────────────────────────

// TestWatchedPaths_Documentation is a fixture-backed verification that the
// paths listed as CI trigger inputs are present in the repository. If any path
// moves, this test surfaces the need to update the CI workflow.
//
// Negative case: unrelated web-only paths (web/**) are NOT in the watched set,
// so web-only PRs do not require the quality gate (when path filtering is
// active in ci.yml).
func TestWatchedPaths_Documentation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)

	// These must exist in the repo — the CI workflow watches them.
	watchedPaths := []string{
		filepath.Join("internal", "critic"),
		filepath.Join("docs", "prompts", "scenario", "critic_agent.md"),
		filepath.Join("testdata", "golden"),
		filepath.Join(".github", "workflows", "ci.yml"),
	}
	for _, rel := range watchedPaths {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("watched path %q not found: %v (update CI workflow if path moved)", rel, err)
		}
	}

	// Negative: web/ is not in watched paths — changes there must NOT
	// automatically require the quality gate.
	unwatchedPaths := []string{
		filepath.Join("web", "src"),
		filepath.Join("web", "package.json"),
	}
	// Verify no watched path is under an unwatched prefix (would mean web-only
	// changes falsely trigger the quality gate).
	for _, u := range unwatchedPaths {
		for _, w := range watchedPaths {
			if strings.HasPrefix(w, u) {
				t.Errorf("watched path %q is under unwatched prefix %q — web-only changes would trigger gate", w, u)
			}
		}
	}
}
