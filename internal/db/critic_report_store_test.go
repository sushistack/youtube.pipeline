package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestCriticReportStore_InsertAndList(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewCriticReportStore(database)

	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "scp-049-run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	report := domain.CriticCheckpointReport{
		Checkpoint:     domain.CriticCheckpointPostReviewer,
		Verdict:        domain.CriticVerdictRetry,
		RetryReason:    "emotional_variation",
		OverallScore:   65,
		Rubric:         domain.CriticRubricScores{Hook: 80, FactAccuracy: 75, EmotionalVariation: 50, Immersion: 55},
		Feedback:       "감정 변주가 약합니다. Scene 3을 톤다운하고 Scene 5를 강하게.",
		SceneNotes:     []domain.CriticSceneNote{{SceneNum: 3, Issue: "단조로움", Suggestion: "정적 추가"}},
		Precheck:       domain.CriticPrecheck{SchemaValid: true, ForbiddenTermHits: []string{}, ShortCircuited: false},
		CriticModel:    "gemini-2.5-pro",
		CriticProvider: "gemini",
		SourceVersion:  domain.CriticSourceVersionPostReviewerV1,
	}

	if err := store.InsertCriticReport(context.Background(), "scp-049-run-1", 1, report); err != nil {
		t.Fatalf("InsertCriticReport: %v", err)
	}

	got, err := store.ListCriticReportsByRun(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("ListCriticReportsByRun: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	rec := got[0]
	if rec.RunID != "scp-049-run-1" || rec.AttemptNumber != 1 {
		t.Errorf("RunID/AttemptNumber = %q/%d, want scp-049-run-1/1", rec.RunID, rec.AttemptNumber)
	}
	if rec.Report.Verdict != domain.CriticVerdictRetry {
		t.Errorf("Verdict = %q, want retry", rec.Report.Verdict)
	}
	if rec.Report.RetryReason != "emotional_variation" {
		t.Errorf("RetryReason = %q, want emotional_variation", rec.Report.RetryReason)
	}
	if rec.Report.OverallScore != 65 {
		t.Errorf("OverallScore = %d, want 65", rec.Report.OverallScore)
	}
	if rec.Report.Rubric.EmotionalVariation != 50 {
		t.Errorf("Rubric.EmotionalVariation = %d, want 50", rec.Report.Rubric.EmotionalVariation)
	}
	if len(rec.Report.SceneNotes) != 1 || rec.Report.SceneNotes[0].SceneNum != 3 {
		t.Errorf("SceneNotes round-trip failed: %+v", rec.Report.SceneNotes)
	}
	if rec.Report.Feedback != "감정 변주가 약합니다. Scene 3을 톤다운하고 Scene 5를 강하게." {
		t.Errorf("Feedback round-trip failed: %q", rec.Report.Feedback)
	}
	if rec.CreatedAt == "" {
		t.Errorf("CreatedAt is empty")
	}
}

func TestCriticReportStore_InsertCriticReport_Validation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewCriticReportStore(database)

	report := domain.CriticCheckpointReport{Checkpoint: "post_writer", Verdict: "pass"}

	if err := store.InsertCriticReport(context.Background(), "", 1, report); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("empty run_id: err = %v, want ErrValidation", err)
	}
	if err := store.InsertCriticReport(context.Background(), "run-1", 0, report); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("attempt 0: err = %v, want ErrValidation", err)
	}
}

func TestCriticReportStore_NarrationAttempt_RoundTrip(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewCriticReportStore(database)

	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "scp-049-run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	monologue := "그 의사는 도착한 모든 곳에서 사람들을 치료한다고 말했습니다."
	narration := &domain.NarrationScript{
		SCPID: "049",
		Title: "Plague Doctor",
		Acts: []domain.ActScript{
			{
				ActID:     domain.ActIncident,
				Monologue: monologue,
				Mood:      "ominous",
				KeyPoints: []string{},
				Beats: []domain.BeatAnchor{{
					StartOffset:       0,
					EndOffset:         len([]rune(monologue)),
					Mood:              "ominous",
					Location:          "Site-19 cell",
					CharactersPresent: []string{"SCP-049"},
					EntityVisible:     true,
					ColorPalette:      "muted gray, candlelight amber",
					Atmosphere:        "claustrophobic dread",
					FactTags:          []domain.FactTag{},
				}},
			},
		},
		Metadata:      domain.NarrationMetadata{},
		SourceVersion: domain.NarrationSourceVersionV2,
	}

	if err := store.InsertNarrationAttempt(context.Background(), "scp-049-run-1", 2, narration); err != nil {
		t.Fatalf("InsertNarrationAttempt: %v", err)
	}

	got, err := store.ListNarrationAttemptsByRun(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("ListNarrationAttemptsByRun: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	rec := got[0]
	if rec.AttemptNumber != 2 {
		t.Errorf("AttemptNumber = %d, want 2", rec.AttemptNumber)
	}
	if rec.Narration == nil || rec.Narration.SCPID != "049" {
		t.Fatalf("Narration round-trip failed: %+v", rec.Narration)
	}
	if len(rec.Narration.Acts) != 1 || rec.Narration.Acts[0].ActID != domain.ActIncident {
		t.Errorf("Act round-trip failed: %+v", rec.Narration.Acts)
	}
}

func TestCriticReportStore_ListEmpty(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewCriticReportStore(database)

	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "scp-049-run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	reports, err := store.ListCriticReportsByRun(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("ListCriticReportsByRun: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("len(reports) = %d, want 0", len(reports))
	}

	attempts, err := store.ListNarrationAttemptsByRun(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("ListNarrationAttemptsByRun: %v", err)
	}
	if len(attempts) != 0 {
		t.Errorf("len(attempts) = %d, want 0", len(attempts))
	}
}
