package db_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
