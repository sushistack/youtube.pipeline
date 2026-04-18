package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// fakeEvaluator implements Evaluator for testing.
// Returns "retry" for negative fixtures and "pass" for positive by default.
type fakeEvaluator struct {
	// override maps fixture_id to a forced VerdictResult.
	override map[string]VerdictResult
}

func (f *fakeEvaluator) Evaluate(_ context.Context, fixture Fixture) (VerdictResult, error) {
	if f.override != nil {
		if v, ok := f.override[fixture.FixtureID]; ok {
			return v, nil
		}
	}
	if fixture.Kind == "negative" {
		return VerdictResult{Verdict: "retry"}, nil
	}
	return VerdictResult{Verdict: "pass"}, nil
}

func TestRunGolden_RecallHappy(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupRootWithSeedPairs(t)

	report, err := RunGolden(context.Background(), root, &fakeEvaluator{}, testNow)
	if err != nil {
		t.Fatalf("RunGolden: %v", err)
	}
	testutil.AssertEqual(t, 2, report.TotalNegative)
	testutil.AssertEqual(t, 2, report.DetectedNegative)
	testutil.AssertEqual(t, 0, report.FalseRejects)
	if report.Recall != 1.0 {
		t.Errorf("expected recall=1.0, got %f", report.Recall)
	}
}

func TestRunGolden_CountsFalseRejects(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupRootWithSeedPairs(t)

	// Force positive fixtures to return "retry" (false reject).
	ev := &fakeEvaluator{
		override: map[string]VerdictResult{
			"scp-173-pass-001": {Verdict: "retry"},
			"scp-173-pass-002": {Verdict: "retry"},
		},
	}
	report, err := RunGolden(context.Background(), root, ev, testNow)
	if err != nil {
		t.Fatalf("RunGolden: %v", err)
	}
	testutil.AssertEqual(t, 2, report.FalseRejects)
}

func TestRunGolden_UpdatesManifestOnSuccess(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupRootWithSeedPairs(t)

	runAt := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	_, err := RunGolden(context.Background(), root, &fakeEvaluator{}, runAt)
	if err != nil {
		t.Fatalf("RunGolden: %v", err)
	}

	m, err := loadManifest(root)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if m.LastSuccessfulRunAt == nil {
		t.Fatal("expected LastSuccessfulRunAt to be set after successful run")
	}
	if !m.LastSuccessfulRunAt.Equal(runAt.UTC()) {
		t.Errorf("LastSuccessfulRunAt = %v, want %v", *m.LastSuccessfulRunAt, runAt.UTC())
	}
	if m.LastSuccessfulPromptHash == "" {
		t.Error("expected LastSuccessfulPromptHash to be set")
	}
	if m.LastReport == nil {
		t.Fatal("expected LastReport to be set")
	}
}

func TestGolden_LocalReport_PersistsToManifest(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupRootWithSeedPairs(t)

	report, err := RunGolden(context.Background(), root, &fakeEvaluator{}, testNow)
	if err != nil {
		t.Fatalf("RunGolden: %v", err)
	}

	m, err := loadManifest(root)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if m.LastReport == nil {
		t.Fatal("LastReport not persisted to manifest")
	}
	testutil.AssertEqual(t, report.TotalNegative, m.LastReport.TotalNegative)
	testutil.AssertEqual(t, report.DetectedNegative, m.LastReport.DetectedNegative)
	testutil.AssertEqual(t, report.FalseRejects, m.LastReport.FalseRejects)
}

// TestGolden is the on-demand test target: go test ./internal/critic/eval -run TestGolden -v
func TestGolden(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)

	report, err := RunGolden(context.Background(), root, &fakeEvaluator{}, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunGolden against real project root: %v", err)
	}

	t.Logf("Golden run complete — recall=%.2f total_negative=%d detected=%d false_rejects=%d",
		report.Recall, report.TotalNegative, report.DetectedNegative, report.FalseRejects)
}

// setupRootWithSeedPairs creates a self-contained temp root with 2 pairs.
func setupRootWithSeedPairs(t *testing.T) string {
	t.Helper()
	root := setupTestRoot(t)

	for i := 1; i <= 2; i++ {
		dirName := fmt.Sprintf("%06d", i)
		dir := filepath.Join(root, "testdata", "golden", "eval", dirName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir pair %d: %v", i, err)
		}

		posFixture := Fixture{
			FixtureID:       fmt.Sprintf("scp-173-pass-%03d", i),
			Kind:            "positive",
			Checkpoint:      "post_writer",
			Input:           minimalNarrationInput(),
			ExpectedVerdict: "pass",
			Category:        "known_pass",
		}
		negFixture := Fixture{
			FixtureID:       fmt.Sprintf("scp-173-fact-error-%03d", i),
			Kind:            "negative",
			Checkpoint:      "post_writer",
			Input:           minimalNarrationInput(),
			ExpectedVerdict: "retry",
			Category:        "fact_error",
		}

		writeFixtureFile(t, dir, "positive.json", posFixture)
		writeFixtureFile(t, dir, "negative.json", negFixture)
		appendManifestPair(t, root, i, dirName)
	}
	return root
}

func writeFixtureFile(t testing.TB, dir, name string, f Fixture) {
	t.Helper()
	data, err := marshalIndented(f)
	if err != nil {
		t.Fatalf("marshal fixture %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
}

func appendManifestPair(t testing.TB, root string, idx int, dirName string) {
	t.Helper()
	m, err := loadManifest(root)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	m.Pairs = append(m.Pairs, PairEntry{
		Index:        idx,
		CreatedAt:    testNow,
		PositivePath: fmt.Sprintf("eval/%s/positive.json", dirName),
		NegativePath: fmt.Sprintf("eval/%s/negative.json", dirName),
	})
	m.NextIndex = idx + 1
	if err := saveManifest(root, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}
}

// minimalNarrationInput returns a json.RawMessage with a valid NarrationScript.
// Uses a hardcoded minimal payload without schema validation (runner tests do not
// exercise ValidateFixture).
func minimalNarrationInput() json.RawMessage {
	scenes := make([]map[string]interface{}, 8)
	for i := range scenes {
		scenes[i] = map[string]interface{}{
			"scene_num":          i + 1,
			"act_id":             fmt.Sprintf("act_%d", i+1),
			"narration":          "테스트 나레이션입니다.",
			"fact_tags":          []interface{}{},
			"mood":               "neutral",
			"entity_visible":     false,
			"location":           "격리실",
			"characters_present": []string{"연구원"},
			"color_palette":      "gray",
			"atmosphere":         "calm",
		}
	}
	payload := map[string]interface{}{
		"scp_id": "SCP-TEST",
		"title":  "테스트 시나리오",
		"scenes": scenes,
		"metadata": map[string]interface{}{
			"language":               "ko",
			"scene_count":            8,
			"writer_model":           "deepseek-chat",
			"writer_provider":        "deepseek",
			"prompt_template":        "v1-scenario-writer",
			"format_guide_template":  "v1-format-guide",
			"forbidden_terms_version": "v1",
		},
		"source_version": "v1-llm-writer",
	}
	b, _ := json.Marshal(payload)
	return b
}
