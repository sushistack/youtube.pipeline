package db_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestRunStore_Create_IDFormat(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()

	run, err := store.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	testutil.AssertEqual(t, run.ID, "scp-049-run-1")
	testutil.AssertEqual(t, run.SCPID, "049")
	testutil.AssertEqual(t, run.Stage, domain.StagePending)
	testutil.AssertEqual(t, run.Status, domain.StatusPending)
}

func TestRunStore_Create_SequentialIDs(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		run, err := store.Create(ctx, "049", outDir)
		if err != nil {
			t.Fatalf("Create run %d: %v", i, err)
		}
		want := fmt.Sprintf("scp-049-run-%d", i)
		testutil.AssertEqual(t, run.ID, want)
	}
}

func TestRunStore_Create_SequentialPerSCPID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	// SCP 049 gets its own sequence.
	run1, _ := store.Create(ctx, "049", outDir)
	run2, _ := store.Create(ctx, "049", outDir)
	// SCP 173 starts from 1 independently.
	run3, _ := store.Create(ctx, "173", outDir)

	testutil.AssertEqual(t, run1.ID, "scp-049-run-1")
	testutil.AssertEqual(t, run2.ID, "scp-049-run-2")
	testutil.AssertEqual(t, run3.ID, "scp-173-run-1")
}

func TestRunStore_Create_OutputDirCreated(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()

	run, err := store.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	expected := filepath.Join(outDir, run.ID)
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Errorf("output dir %s was not created", expected)
	}
}

func TestRunStore_Get_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	_, err := store.Get(context.Background(), "scp-999-run-1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRunStore_List_Empty(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	runs, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	testutil.AssertEqual(t, len(runs), 0)
}

func TestRunStore_List_OrderedByCreatedAt(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	store.Create(ctx, "049", outDir)
	store.Create(ctx, "049", outDir)
	store.Create(ctx, "173", outDir)

	runs, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	testutil.AssertEqual(t, len(runs), 3)
	testutil.AssertEqual(t, runs[0].ID, "scp-049-run-1")
	testutil.AssertEqual(t, runs[1].ID, "scp-049-run-2")
	testutil.AssertEqual(t, runs[2].ID, "scp-173-run-1")
}

func TestRunStore_Cancel_Running(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)

	// Manually set status to running so cancel is valid.
	database.ExecContext(ctx, "UPDATE runs SET status = 'running' WHERE id = ?", run.ID)

	if err := store.Cancel(ctx, run.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	updated, _ := store.Get(ctx, run.ID)
	testutil.AssertEqual(t, updated.Status, domain.StatusCancelled)
}

func TestRunStore_Cancel_AlreadyTerminal(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)
	// pending status is not cancellable → ErrConflict

	err := store.Cancel(ctx, run.ID)
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("expected ErrConflict for pending run, got %v", err)
	}
}

func TestRunStore_Cancel_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	err := store.Cancel(context.Background(), "scp-999-run-1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRunStore_Cancel_RemovesHITLSession(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	decisions := db.NewDecisionStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)
	// Advance run into a paused HITL state and seed the session row.
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET stage = 'batch_review', status = 'waiting' WHERE id = ?`, run.ID); err != nil {
		t.Fatalf("seed stage/status: %v", err)
	}
	if err := decisions.UpsertSession(ctx, &domain.HITLSession{
		RunID: run.ID, Stage: domain.StageBatchReview, SceneIndex: 2,
		LastInteractionTimestamp: "2026-01-01T00:00:00Z", SnapshotJSON: `{}`,
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	// Sanity: session exists before cancel.
	if got, _ := decisions.GetSession(ctx, run.ID); got == nil {
		t.Fatalf("pre-cancel: expected session row, got nil")
	}

	if err := store.Cancel(ctx, run.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if got, _ := decisions.GetSession(ctx, run.ID); got != nil {
		t.Fatalf("expected session removed after Cancel, got %+v", got)
	}
}

func TestRunStore_SetStatus_Success(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)
	reason := "upstream_timeout"
	if err := store.SetStatus(ctx, run.ID, domain.StatusFailed, &reason); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	updated, _ := store.Get(ctx, run.ID)
	testutil.AssertEqual(t, updated.Status, domain.StatusFailed)
	if updated.RetryReason == nil || *updated.RetryReason != "upstream_timeout" {
		t.Errorf("RetryReason = %v, want upstream_timeout", updated.RetryReason)
	}
}

func TestRunStore_SetStatus_ClearsRetryReason(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)
	reason := "rate_limit"
	if err := store.SetStatus(ctx, run.ID, domain.StatusFailed, &reason); err != nil {
		t.Fatalf("SetStatus with reason: %v", err)
	}
	if err := store.SetStatus(ctx, run.ID, domain.StatusRunning, nil); err != nil {
		t.Fatalf("SetStatus clearing reason: %v", err)
	}
	updated, _ := store.Get(ctx, run.ID)
	testutil.AssertEqual(t, updated.Status, domain.StatusRunning)
	if updated.RetryReason != nil {
		t.Errorf("RetryReason = %v, want nil (cleared)", *updated.RetryReason)
	}
}

func TestRunStore_SetStatus_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	err := store.SetStatus(context.Background(), "scp-999-run-1", domain.StatusRunning, nil)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRunStore_SetSelectedCharacterID_PersistsValue(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)
	if err := store.SetCharacterQueryKey(ctx, run.ID, "scp-049"); err != nil {
		t.Fatalf("SetCharacterQueryKey: %v", err)
	}
	if err := store.SetSelectedCharacterID(ctx, run.ID, "scp-049#2"); err != nil {
		t.Fatalf("SetSelectedCharacterID: %v", err)
	}

	updated, err := store.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.SelectedCharacterID == nil || *updated.SelectedCharacterID != "scp-049#2" {
		t.Fatalf("SelectedCharacterID = %v, want scp-049#2", updated.SelectedCharacterID)
	}
}

func TestRunStore_Get_IncludesSelectedCharacterID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)
	if _, err := database.ExecContext(ctx,
		`UPDATE runs
		    SET character_query_key = 'scp-049',
		        selected_character_id = 'scp-049#1'
		  WHERE id = ?`,
		run.ID,
	); err != nil {
		t.Fatalf("seed character fields: %v", err)
	}

	got, err := store.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CharacterQueryKey == nil || *got.CharacterQueryKey != "scp-049" {
		t.Fatalf("CharacterQueryKey = %v, want scp-049", got.CharacterQueryKey)
	}
	if got.SelectedCharacterID == nil || *got.SelectedCharacterID != "scp-049#1" {
		t.Fatalf("SelectedCharacterID = %v, want scp-049#1", got.SelectedCharacterID)
	}
}

func TestRunStore_ApplyPhaseAResult_RoundTrip(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, err := store.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	retryReason := "weak_hook"
	score := 0.83
	scenarioPath := "scenario.json"
	if err := store.ApplyPhaseAResult(ctx, run.ID, domain.PhaseAAdvanceResult{
		Stage:        domain.StageScenarioReview,
		Status:       domain.StatusWaiting,
		RetryReason:  &retryReason,
		CriticScore:  &score,
		ScenarioPath: &scenarioPath,
	}); err != nil {
		t.Fatalf("ApplyPhaseAResult: %v", err)
	}

	updated, err := store.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	testutil.AssertEqual(t, updated.Stage, domain.StageScenarioReview)
	testutil.AssertEqual(t, updated.Status, domain.StatusWaiting)
	if updated.RetryReason == nil || *updated.RetryReason != retryReason {
		t.Fatalf("unexpected retry_reason: %v", updated.RetryReason)
	}
	if updated.CriticScore == nil {
		t.Fatal("expected critic_score persisted")
	}
	testutil.AssertFloatNear(t, *updated.CriticScore, score, 0.000001)
	if updated.ScenarioPath == nil || *updated.ScenarioPath != scenarioPath {
		t.Fatalf("unexpected scenario_path: %v", updated.ScenarioPath)
	}
}

func TestRunStore_IncrementRetryCount_Success(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)
	for i := 1; i <= 3; i++ {
		if err := store.IncrementRetryCount(ctx, run.ID); err != nil {
			t.Fatalf("IncrementRetryCount #%d: %v", i, err)
		}
	}
	updated, _ := store.Get(ctx, run.ID)
	testutil.AssertEqual(t, updated.RetryCount, 3)
}

func TestRunStore_IncrementRetryCount_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	err := store.IncrementRetryCount(context.Background(), "scp-999-run-1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRunStore_ResetForResume_Success(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)
	reason := "upstream_timeout"
	if err := store.SetStatus(ctx, run.ID, domain.StatusFailed, &reason); err != nil {
		t.Fatalf("seed SetStatus: %v", err)
	}

	if err := store.ResetForResume(ctx, run.ID, domain.StatusRunning); err != nil {
		t.Fatalf("ResetForResume: %v", err)
	}
	got, _ := store.Get(ctx, run.ID)
	testutil.AssertEqual(t, got.Status, domain.StatusRunning)
	if got.RetryReason != nil {
		t.Errorf("RetryReason = %v, want nil (cleared)", *got.RetryReason)
	}
	testutil.AssertEqual(t, got.RetryCount, 1)
}

func TestRunStore_ResetForResume_IncrementsFromExistingCount(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)
	// Seed retry_count=2.
	store.IncrementRetryCount(ctx, run.ID)
	store.IncrementRetryCount(ctx, run.ID)

	if err := store.ResetForResume(ctx, run.ID, domain.StatusWaiting); err != nil {
		t.Fatalf("ResetForResume: %v", err)
	}
	got, _ := store.Get(ctx, run.ID)
	testutil.AssertEqual(t, got.RetryCount, 3) // 2 + 1
	testutil.AssertEqual(t, got.Status, domain.StatusWaiting)
}

func TestRunStore_RecentCompletedRunsForMetrics_ReturnsWindow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "metrics_seed")
	store := db.NewRunStore(database)

	rows, err := store.RecentCompletedRunsForMetrics(context.Background(), 25)
	if err != nil {
		t.Fatalf("RecentCompletedRunsForMetrics: %v", err)
	}
	testutil.AssertEqual(t, len(rows), 25)
	testutil.AssertEqual(t, rows[0].ID, "scp-049-run-01")
	testutil.AssertEqual(t, rows[24].ID, "scp-049-run-25")
	testutil.AssertEqual(t, rows[0].Status, "completed")
	testutil.AssertEqual(t, rows[16].HumanOverride, true)
}

func TestRunStore_RecentCompletedRunsForMetrics_Empty(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	rows, err := store.RecentCompletedRunsForMetrics(context.Background(), 25)
	if err != nil {
		t.Fatalf("RecentCompletedRunsForMetrics: %v", err)
	}
	if rows != nil {
		t.Fatalf("expected nil slice, got %+v", rows)
	}
}

func TestRunStore_RecentCompletedRunsForMetrics_ValidationError(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	_, err := store.RecentCompletedRunsForMetrics(context.Background(), 0)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestRunStore_RecentCompletedRunsForMetrics_UsesIndex(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "metrics_seed")

	rows, err := database.Query(
		"EXPLAIN QUERY PLAN SELECT id, status, critic_score, human_override, retry_count, retry_reason, created_at FROM runs WHERE status = ? ORDER BY created_at DESC, id DESC LIMIT ?",
		"completed", 25,
	)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}
	planStr := plan.String()
	if !strings.Contains(planStr, "idx_runs_status_created_at") {
		t.Fatalf("expected idx_runs_status_created_at in plan, got:\n%s", planStr)
	}
	if strings.Contains(planStr, "SCAN runs") {
		t.Fatalf("unexpected full scan:\n%s", planStr)
	}
}

func TestRunStore_ResetForResume_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	err := store.ResetForResume(context.Background(), "scp-999-run-1", domain.StatusRunning)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRunStore_SetStatus_UpdatedAtAdvances(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)
	before := run.UpdatedAt

	// datetime('now') is second-precision; sleep >1s so trigger produces a
	// strictly greater timestamp. Needed to verify Migration 002 fires.
	time.Sleep(1100 * time.Millisecond)

	reason := "upstream_timeout"
	if err := store.SetStatus(ctx, run.ID, domain.StatusFailed, &reason); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	updated, _ := store.Get(ctx, run.ID)
	if updated.UpdatedAt <= before {
		t.Errorf("UpdatedAt did not advance: before=%q after=%q", before, updated.UpdatedAt)
	}
}

func TestRunStore_RecordStageObservation_AccumulatesColumns(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)

	reason := "rate_limit"
	score := 0.82
	obs := domain.StageObservation{
		Stage:         domain.StageWrite,
		DurationMs:    1500,
		TokenIn:       1000,
		TokenOut:      200,
		RetryCount:    1,
		RetryReason:   &reason,
		CriticScore:   &score,
		CostUSD:       0.05,
		HumanOverride: false,
	}
	if err := store.RecordStageObservation(ctx, run.ID, obs); err != nil {
		t.Fatalf("RecordStageObservation: %v", err)
	}
	if err := store.RecordStageObservation(ctx, run.ID, obs); err != nil {
		t.Fatalf("RecordStageObservation (2nd): %v", err)
	}

	got, _ := store.Get(ctx, run.ID)
	testutil.AssertEqual(t, got.DurationMs, int64(3000))
	testutil.AssertEqual(t, got.TokenIn, 2000)
	testutil.AssertEqual(t, got.TokenOut, 400)
	testutil.AssertEqual(t, got.RetryCount, 2)
	if got.CostUSD < 0.0999 || got.CostUSD > 0.1001 {
		t.Errorf("CostUSD: got %.4f want ~0.10", got.CostUSD)
	}
	if got.RetryReason == nil || *got.RetryReason != "rate_limit" {
		t.Errorf("RetryReason: got %v want rate_limit", got.RetryReason)
	}
	if got.CriticScore == nil || *got.CriticScore != 0.82 {
		t.Errorf("CriticScore: got %v want 0.82", got.CriticScore)
	}
}

func TestRunStore_RecordStageObservation_NullableOverwrite(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)

	// First call: leave RetryReason/CriticScore nil → NULL preserved.
	if err := store.RecordStageObservation(ctx, run.ID, domain.StageObservation{
		Stage:      domain.StageWrite,
		DurationMs: 500,
	}); err != nil {
		t.Fatalf("first record: %v", err)
	}
	got, _ := store.Get(ctx, run.ID)
	if got.RetryReason != nil {
		t.Errorf("RetryReason: expected nil, got %v", *got.RetryReason)
	}
	if got.CriticScore != nil {
		t.Errorf("CriticScore: expected nil, got %v", *got.CriticScore)
	}

	// Second call: set RetryReason + CriticScore non-nil → overwrites.
	reason := "timeout"
	score := 0.77
	if err := store.RecordStageObservation(ctx, run.ID, domain.StageObservation{
		Stage:       domain.StageWrite,
		RetryReason: &reason,
		CriticScore: &score,
	}); err != nil {
		t.Fatalf("second record: %v", err)
	}
	got, _ = store.Get(ctx, run.ID)
	if got.RetryReason == nil || *got.RetryReason != "timeout" {
		t.Errorf("RetryReason: got %v want timeout", got.RetryReason)
	}
	if got.CriticScore == nil || *got.CriticScore != 0.77 {
		t.Errorf("CriticScore: got %v want 0.77", got.CriticScore)
	}

	// Third call: nil pointers → COALESCE preserves prior non-null values.
	if err := store.RecordStageObservation(ctx, run.ID, domain.StageObservation{
		Stage: domain.StageCritic,
	}); err != nil {
		t.Fatalf("third record: %v", err)
	}
	got, _ = store.Get(ctx, run.ID)
	if got.RetryReason == nil || *got.RetryReason != "timeout" {
		t.Errorf("RetryReason after nil: got %v want timeout (preserved)", got.RetryReason)
	}
	if got.CriticScore == nil || *got.CriticScore != 0.77 {
		t.Errorf("CriticScore after nil: got %v want 0.77 (preserved)", got.CriticScore)
	}
}

func TestRunStore_RecordStageObservation_HumanOverrideSticky(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)

	// Set true once.
	if err := store.RecordStageObservation(ctx, run.ID, domain.StageObservation{
		Stage:         domain.StageWrite,
		HumanOverride: true,
	}); err != nil {
		t.Fatalf("first record: %v", err)
	}
	got, _ := store.Get(ctx, run.ID)
	if !got.HumanOverride {
		t.Fatal("expected HumanOverride=true after first record")
	}

	// Subsequent false does not revert.
	if err := store.RecordStageObservation(ctx, run.ID, domain.StageObservation{
		Stage:         domain.StageCritic,
		HumanOverride: false,
	}); err != nil {
		t.Fatalf("second record: %v", err)
	}
	got, _ = store.Get(ctx, run.ID)
	if !got.HumanOverride {
		t.Fatal("expected HumanOverride still true after false record")
	}
}

func TestRunStore_RecordStageObservation_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	err := store.RecordStageObservation(context.Background(), "ghost-run", domain.StageObservation{
		Stage: domain.StageWrite,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRunStore_RecordStageObservation_RejectsInvalid(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, _ := store.Create(ctx, "049", outDir)
	before, _ := store.Get(ctx, run.ID)

	err := store.RecordStageObservation(ctx, run.ID, domain.StageObservation{
		Stage:   domain.StageWrite,
		CostUSD: -0.50,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}

	after, _ := store.Get(ctx, run.ID)
	testutil.AssertEqual(t, after.CostUSD, before.CostUSD)
}

func TestAntiProgressFalsePositiveStats_EmptyDB(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	got, err := store.AntiProgressFalsePositiveStats(context.Background(), 50)
	if err != nil {
		t.Fatalf("stats on empty DB: %v", err)
	}
	testutil.AssertEqual(t, got.Total, 0)
	testutil.AssertEqual(t, got.OperatorOverride, 0)
	testutil.AssertEqual(t, got.Provisional, true)
}

func TestAntiProgressFalsePositiveStats_InvalidWindow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	for _, w := range []int{0, -1, -100} {
		_, err := store.AntiProgressFalsePositiveStats(context.Background(), w)
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("window=%d: expected ErrValidation, got %v", w, err)
		}
	}
}

func TestAntiProgressFalsePositiveStats_RollingWindow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "anti_progress_seed")
	store := db.NewRunStore(database)
	ctx := context.Background()

	cases := []struct {
		window          int
		wantTotal       int
		wantOverridden  int
		wantProvisional bool
	}{
		{50, 50, 0, false},
		{60, 60, 10, false},
		{100, 60, 10, true},
	}
	for _, tc := range cases {
		got, err := store.AntiProgressFalsePositiveStats(ctx, tc.window)
		if err != nil {
			t.Fatalf("window=%d: %v", tc.window, err)
		}
		if got.Total != tc.wantTotal || got.OperatorOverride != tc.wantOverridden || got.Provisional != tc.wantProvisional {
			t.Errorf("window=%d: got %+v, want Total=%d Overridden=%d Provisional=%v",
				tc.window, got, tc.wantTotal, tc.wantOverridden, tc.wantProvisional)
		}
	}
}

func TestAntiProgressFalsePositiveStats_IgnoresNonAntiProgressRows(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "anti_progress_seed")
	store := db.NewRunStore(database)

	// The fixture has 20 rate_limit rows + 5 NULL rows that must NOT be counted.
	got, err := store.AntiProgressFalsePositiveStats(context.Background(), 100)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	// Only the 60 anti_progress rows should be counted; the 25 decoys are excluded.
	testutil.AssertEqual(t, got.Total, 60)
}

func TestAntiProgressFalsePositiveStats_UsesCompositeIndex(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "anti_progress_seed")

	const explainQuery = `
EXPLAIN QUERY PLAN
SELECT COUNT(*), SUM(CASE WHEN human_override = 1 THEN 1 ELSE 0 END)
FROM (
    SELECT human_override
    FROM runs
    WHERE retry_reason = 'anti_progress'
    ORDER BY created_at DESC, id DESC
    LIMIT ?
);
`
	rows, err := database.QueryContext(context.Background(), explainQuery, 50)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}
	planStr := plan.String()
	// Migration 004 added idx_runs_retry_reason_created_at specifically for
	// this query shape. The planner must pick it up: a selective index seek
	// on retry_reason + index-ordered walk by created_at DESC.
	if !strings.Contains(planStr, "USING INDEX idx_runs_retry_reason_created_at") {
		t.Errorf("query plan must use idx_runs_retry_reason_created_at (migration 004); got plan:\n%s", planStr)
	}
	if strings.Contains(planStr, "SCAN runs") {
		t.Errorf("query plan must not full-scan runs; got plan:\n%s", planStr)
	}
}

func TestRunStore_ApplyCharacterPick_PersistsFrozenDescriptor(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()

	run, err := store.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`UPDATE runs SET stage = 'character_pick', status = 'waiting' WHERE id = ?`,
		run.ID,
	); err != nil {
		t.Fatalf("seed stage: %v", err)
	}

	descriptor := "appearance: plague doctor; environment: dim corridor"
	if err := store.ApplyCharacterPick(context.Background(),
		run.ID, "scp-049", "scp-049#3", &descriptor,
		domain.StageImage, domain.StatusRunning,
	); err != nil {
		t.Fatalf("ApplyCharacterPick: %v", err)
	}

	reloaded, err := store.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	testutil.AssertEqual(t, reloaded.Stage, domain.StageImage)
	testutil.AssertEqual(t, reloaded.Status, domain.StatusRunning)
	if reloaded.FrozenDescriptor == nil || *reloaded.FrozenDescriptor != descriptor {
		t.Fatalf("FrozenDescriptor = %v, want %q", reloaded.FrozenDescriptor, descriptor)
	}
	if reloaded.SelectedCharacterID == nil || *reloaded.SelectedCharacterID != "scp-049#3" {
		t.Fatalf("SelectedCharacterID = %v, want scp-049#3", reloaded.SelectedCharacterID)
	}
}

func TestRunStore_ApplyCharacterPick_NilDescriptorPreservesPriorValue(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()

	run, err := store.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`UPDATE runs SET stage = 'character_pick', status = 'waiting',
		 frozen_descriptor = 'pre-existing' WHERE id = ?`,
		run.ID,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := store.ApplyCharacterPick(context.Background(),
		run.ID, "scp-049", "scp-049#1", nil,
		domain.StageImage, domain.StatusRunning,
	); err != nil {
		t.Fatalf("ApplyCharacterPick: %v", err)
	}
	reloaded, err := store.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if reloaded.FrozenDescriptor == nil || *reloaded.FrozenDescriptor != "pre-existing" {
		t.Fatalf("nil descriptor must preserve prior value; got %v", reloaded.FrozenDescriptor)
	}
}

func TestRunStore_LatestFrozenDescriptorBySCPID_PrefersMostRecentAndExcludesCurrent(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	// Three runs for SCP-049: two with frozen_descriptor values, one is the
	// current run (must be excluded), plus one unrelated SCP.
	runOldest, err := store.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("Create oldest: %v", err)
	}
	runNewer, err := store.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("Create newer: %v", err)
	}
	runCurrent, err := store.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("Create current: %v", err)
	}
	runOther, err := store.Create(ctx, "050", outDir)
	if err != nil {
		t.Fatalf("Create other: %v", err)
	}

	// Prior runs must be status='completed' to count per AC-4; the test
	// explicitly seeds that predicate alongside the descriptor + updated_at
	// timestamp so ordering and completion-filter semantics are both covered.
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET frozen_descriptor = ?, status = 'completed', updated_at = ? WHERE id = ?`,
		"oldest-049", "2026-01-01 00:00:00", runOldest.ID,
	); err != nil {
		t.Fatalf("update oldest: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET frozen_descriptor = ?, status = 'completed', updated_at = ? WHERE id = ?`,
		"newer-049", "2026-03-01 00:00:00", runNewer.ID,
	); err != nil {
		t.Fatalf("update newer: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET frozen_descriptor = 'current-should-be-ignored' WHERE id = ?`,
		runCurrent.ID,
	); err != nil {
		t.Fatalf("update current: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET frozen_descriptor = 'other-scp', status = 'completed' WHERE id = ?`,
		runOther.ID,
	); err != nil {
		t.Fatalf("update other: %v", err)
	}

	got, err := store.LatestFrozenDescriptorBySCPID(ctx, "049", runCurrent.ID)
	if err != nil {
		t.Fatalf("LatestFrozenDescriptorBySCPID: %v", err)
	}
	if got == nil || *got != "newer-049" {
		t.Fatalf("got %v, want pointer to \"newer-049\"", got)
	}
}

func TestRunStore_LatestFrozenDescriptorBySCPID_ReturnsNilWhenNoPrior(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, err := store.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.LatestFrozenDescriptorBySCPID(ctx, "049", run.ID)
	if err != nil {
		t.Fatalf("LatestFrozenDescriptorBySCPID: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil pointer, got %q", *got)
	}
}

// TestRunStore_LatestFrozenDescriptorBySCPID_ExcludesNonCompletedRuns enforces
// AC-4's "most recent other *completed* run" predicate. A non-completed run
// (running/failed/cancelled) that happened to persist a frozen_descriptor via
// the character_pick flow must not surface as the prior-run prefill source.
func TestRunStore_LatestFrozenDescriptorBySCPID_ExcludesNonCompletedRuns(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	runFailed, err := store.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("Create failed run: %v", err)
	}
	runCancelled, err := store.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("Create cancelled run: %v", err)
	}
	runCurrent, err := store.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("Create current run: %v", err)
	}

	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET frozen_descriptor = 'failed-desc', status = 'failed' WHERE id = ?`,
		runFailed.ID,
	); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET frozen_descriptor = 'cancelled-desc', status = 'cancelled' WHERE id = ?`,
		runCancelled.ID,
	); err != nil {
		t.Fatalf("seed cancelled: %v", err)
	}

	got, err := store.LatestFrozenDescriptorBySCPID(ctx, "049", runCurrent.ID)
	if err != nil {
		t.Fatalf("LatestFrozenDescriptorBySCPID: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil (no completed prior), got %q", *got)
	}
}

func TestRunStore_UpdateOutputPath_UpdatesExistingRun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	outDir := t.TempDir()
	ctx := context.Background()

	run, err := store.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	outputPath := filepath.Join(outDir, run.ID, "output.mp4")
	err = store.UpdateOutputPath(ctx, run.ID, outputPath)
	if err != nil {
		t.Fatalf("UpdateOutputPath: %v", err)
	}

	// verify via raw query
	var stored string
	err = database.QueryRowContext(ctx,
		`SELECT output_path FROM runs WHERE id = ?`,
		run.ID).Scan(&stored)
	if err != nil {
		t.Fatalf("query output_path: %v", err)
	}
	if stored != outputPath {
		t.Errorf("output_path = %q, want %q", stored, outputPath)
	}
}

func TestRunStore_UpdateOutputPath_ReturnsNotFoundForMissingRun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	ctx := context.Background()

	err := store.UpdateOutputPath(ctx, "non-existent-run", "output.mp4")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestRunStore_UpdateOutputPath_ReturnsValidationErrorForEmptyRunID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)

	err := store.UpdateOutputPath(context.Background(), "", "output.mp4")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}
