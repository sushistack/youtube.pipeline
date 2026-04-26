package pipeline_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// SMOKE-06 — Shadow Eval Against Live Run
// Regression guard for AI-5: when runs.scenario_path stores a run-relative
// path like "scenario.json", Shadow must resolve it against
// {outputDir}/{runID}/ rather than projectRoot.
func TestSmoke06_ShadowEvalAgainstLiveRunLayout(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	projectRoot := testutil.ProjectRoot(t)
	outputDir := t.TempDir()
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	run, err := store.Create(context.Background(), "049", outputDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	writeScenarioArtifact(t, outputDir, run.ID)

	if _, err := database.ExecContext(context.Background(), `
		UPDATE runs
		   SET status = 'completed',
		       stage = 'complete',
		       critic_score = 0.91,
		       scenario_path = 'scenario.json'
		 WHERE id = ?`,
		run.ID,
	); err != nil {
		t.Fatalf("seed completed run: %v", err)
	}

	report, err := eval.RunShadow(
		context.Background(),
		projectRoot,
		outputDir,
		eval.NewSQLiteShadowSource(database),
		smoke06Evaluator{},
		shadowNow,
		10,
	)
	if err != nil {
		t.Fatalf("RunShadow: %v", err)
	}
	if report.Evaluated != 1 {
		t.Fatalf("evaluated = %d, want 1", report.Evaluated)
	}
	if len(report.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(report.Results))
	}
	res := report.Results[0]
	if res.RunID != run.ID {
		t.Fatalf("run id = %s, want %s", res.RunID, run.ID)
	}
	if res.FalseRejection {
		t.Fatalf("unexpected false rejection: %+v", res)
	}
	// Verdict ∈ {pass,retry,accept_with_notes} — guards against an evaluator
	// returning "" or a new-taxonomy value that would silently absorb as drift.
	switch res.NewVerdict {
	case domain.CriticVerdictPass, domain.CriticVerdictRetry, domain.CriticVerdictAcceptWithNotes:
	default:
		t.Fatalf("verdict = %q, want one of pass/retry/accept_with_notes", res.NewVerdict)
	}
	if res.BaselineVerdict != domain.CriticVerdictPass {
		t.Fatalf("baseline verdict = %q, want pass", res.BaselineVerdict)
	}
	// Diff serializes {before, after, delta}: BaselineScore=0.91, NewOverallScore=92
	// → normalized 0.92, delta ≈ +0.01.
	if got, want := res.BaselineScore, 0.91; got != want {
		t.Fatalf("baseline score = %v, want %v", got, want)
	}
	if res.NewOverallScore != 92 {
		t.Fatalf("new overall score = %d, want 92", res.NewOverallScore)
	}
	if delta := res.Diff.Overall; delta < 0.005 || delta > 0.015 {
		t.Fatalf("overall diff = %v, want ≈ +0.01", delta)
	}
	// SummaryLine is the human-inspection output the UI/CI consumes.
	if want := "shadow eval: window=10 evaluated=1 false_rejections=0"; report.SummaryLine() != want {
		t.Fatalf("summary line = %q, want %q", report.SummaryLine(), want)
	}
}

var shadowNow = mustParseRFC3339("2026-04-25T00:00:00Z")

type smoke06Evaluator struct{}

func (smoke06Evaluator) Evaluate(_ context.Context, fixture eval.Fixture) (eval.VerdictResult, error) {
	return eval.VerdictResult{
		Verdict:      domain.CriticVerdictPass,
		OverallScore: 92,
		Provider:     "deepseek",
		Model:        "deepseek-chat",
	}, nil
}

func writeScenarioArtifact(t *testing.T, outputDir, runID string) {
	t.Helper()

	envelope := map[string]any{
		"run_id":      runID,
		"scp_id":      "SCP-049",
		"started_at":  "2026-04-25T00:00:00Z",
		"finished_at": "2026-04-25T00:01:00Z",
		"narration": map[string]any{
			"scp_id": "SCP-049",
			"title":  "Shadow smoke",
			"scenes": []map[string]any{
				{
					"scene_num":          1,
					"act_id":             "act_1",
					"narration":          "실험 장면입니다.",
					"fact_tags":          []map[string]any{},
					"mood":               "neutral",
					"entity_visible":     false,
					"location":           "containment chamber",
					"characters_present": []string{"researcher"},
					"color_palette":      "gray",
					"atmosphere":         "calm",
				},
				{
					"scene_num":          2,
					"act_id":             "act_1",
					"narration":          "관찰 로그를 검토합니다.",
					"fact_tags":          []map[string]any{},
					"mood":               "neutral",
					"entity_visible":     false,
					"location":           "control room",
					"characters_present": []string{"researcher"},
					"color_palette":      "gray",
					"atmosphere":         "calm",
				},
				{
					"scene_num":          3,
					"act_id":             "act_2",
					"narration":          "이상 징후를 기록합니다.",
					"fact_tags":          []map[string]any{},
					"mood":               "tense",
					"entity_visible":     false,
					"location":           "observation deck",
					"characters_present": []string{"researcher"},
					"color_palette":      "gray",
					"atmosphere":         "tense",
				},
				{
					"scene_num":          4,
					"act_id":             "act_2",
					"narration":          "봉인 절차를 진행합니다.",
					"fact_tags":          []map[string]any{},
					"mood":               "tense",
					"entity_visible":     false,
					"location":           "containment corridor",
					"characters_present": []string{"researcher"},
					"color_palette":      "gray",
					"atmosphere":         "tense",
				},
				{
					"scene_num":          5,
					"act_id":             "act_3",
					"narration":          "후속 분석을 시작합니다.",
					"fact_tags":          []map[string]any{},
					"mood":               "neutral",
					"entity_visible":     false,
					"location":           "analysis lab",
					"characters_present": []string{"researcher"},
					"color_palette":      "gray",
					"atmosphere":         "calm",
				},
				{
					"scene_num":          6,
					"act_id":             "act_3",
					"narration":          "결론을 정리합니다.",
					"fact_tags":          []map[string]any{},
					"mood":               "neutral",
					"entity_visible":     false,
					"location":           "analysis lab",
					"characters_present": []string{"researcher"},
					"color_palette":      "gray",
					"atmosphere":         "calm",
				},
				{
					"scene_num":          7,
					"act_id":             "act_4",
					"narration":          "문서를 마감합니다.",
					"fact_tags":          []map[string]any{},
					"mood":               "neutral",
					"entity_visible":     false,
					"location":           "records room",
					"characters_present": []string{"researcher"},
					"color_palette":      "gray",
					"atmosphere":         "calm",
				},
				{
					"scene_num":          8,
					"act_id":             "act_4",
					"narration":          "보관 절차를 완료합니다.",
					"fact_tags":          []map[string]any{},
					"mood":               "neutral",
					"entity_visible":     false,
					"location":           "records room",
					"characters_present": []string{"researcher"},
					"color_palette":      "gray",
					"atmosphere":         "calm",
				},
			},
			"metadata": map[string]any{
				"language":                "ko",
				"scene_count":             8,
				"writer_model":            "qwen-max",
				"writer_provider":         "dashscope",
				"prompt_template":         "v1",
				"format_guide_template":   "v1",
				"forbidden_terms_version": "v1",
			},
			"source_version": "v1-llm-writer",
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal scenario: %v", err)
	}
	dir := filepath.Join(outputDir, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenario.json"), data, 0o644); err != nil {
		t.Fatalf("write scenario.json: %v", err)
	}
}

func mustParseRFC3339(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
