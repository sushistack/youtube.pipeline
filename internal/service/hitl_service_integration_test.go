package service_test

import (
	"context"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestIntegration_BuildStatus_PausedNoChanges exercises the full BuildStatus
// flow against a real SQLite DB. The fixture's snapshot_json matches the
// live state exactly, so the FR50 diff must be empty.
func TestIntegration_BuildStatus_PausedNoChanges(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "paused_at_batch_review")
	runs := db.NewRunStore(database)
	decisions := db.NewDecisionStore(database)
	logger, _ := testutil.CaptureLog(t)
	svc := service.NewHITLService(runs, decisions, logger)

	got, err := svc.BuildStatus(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if got.Run == nil || got.Run.ID != "scp-049-run-1" {
		t.Fatalf("Run mismatch: %+v", got.Run)
	}
	if got.PausedPosition == nil {
		t.Fatalf("expected PausedPosition, got nil")
	}
	testutil.AssertEqual(t, got.PausedPosition.SceneIndex, 2)
	testutil.AssertEqual(t, got.PausedPosition.LastInteractionTimestamp, "2026-01-01T00:25:00Z")
	testutil.AssertEqual(t, got.PausedPosition.Stage, domain.StageBatchReview)

	if got.DecisionsSummary == nil {
		t.Fatalf("expected DecisionsSummary")
	}
	testutil.AssertEqual(t, got.DecisionsSummary.ApprovedCount, 2)
	testutil.AssertEqual(t, got.DecisionsSummary.RejectedCount, 0)
	testutil.AssertEqual(t, got.DecisionsSummary.PendingCount, 1)

	// With totalScenes=3 and scene_index=2 (from session), summary text
	// "scene 3 of 3" (1-indexed). Session captured snapshot before scene 2
	// was decided — current state still has it pending, so no changes.
	want := "Run scp-049-run-1: reviewing scene 3 of 3, 2 approved, 0 rejected"
	testutil.AssertEqual(t, got.Summary, want)

	if got.ChangesSince != nil {
		t.Fatalf("expected nil ChangesSince (snapshot == live), got %+v", got.ChangesSince)
	}
}

// TestIntegration_BuildStatus_PausedWithChanges exercises FR50: the snapshot
// shows scene 2 as pending, but live decisions approved it at T2 > T1.
func TestIntegration_BuildStatus_PausedWithChanges(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.LoadRunStateFixture(t, "paused_with_changes")
	runs := db.NewRunStore(database)
	decisions := db.NewDecisionStore(database)
	logger, _ := testutil.CaptureLog(t)
	svc := service.NewHITLService(runs, decisions, logger)

	got, err := svc.BuildStatus(context.Background(), "scp-049-run-2")
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if got.PausedPosition == nil {
		t.Fatalf("expected PausedPosition")
	}
	testutil.AssertEqual(t, got.DecisionsSummary.ApprovedCount, 3)
	testutil.AssertEqual(t, got.DecisionsSummary.RejectedCount, 0)
	testutil.AssertEqual(t, got.DecisionsSummary.PendingCount, 0)

	if len(got.ChangesSince) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(got.ChangesSince), got.ChangesSince)
	}
	ch := got.ChangesSince[0]
	testutil.AssertEqual(t, ch.Kind, pipeline.ChangeKindSceneStatusFlipped)
	testutil.AssertEqual(t, ch.SceneID, "2")
	testutil.AssertEqual(t, ch.Before, "pending")
	testutil.AssertEqual(t, ch.After, "approved")
	testutil.AssertEqual(t, ch.Timestamp, "2026-01-02T00:45:00Z")

	// Summary uses live NextSceneIndex (P8): all 3 scenes approved → index=3 (past-end)
	// → "scene 4 of 3" signals review complete.
	want := "Run scp-049-run-2: reviewing scene 4 of 3, 3 approved, 0 rejected"
	testutil.AssertEqual(t, got.Summary, want)
}

// TestIntegration_BuildStatus_NonHITLRun verifies that non-HITL runs do NOT
// populate PausedPosition/Summary/ChangesSince; DecisionsSummary is still
// returned (zero-valued when no segments/decisions exist).
func TestIntegration_BuildStatus_NonHITLRun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, stage, status) VALUES ('r-run', 'scp', 'write', 'running')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	runs := db.NewRunStore(database)
	decisions := db.NewDecisionStore(database)
	logger, _ := testutil.CaptureLog(t)
	svc := service.NewHITLService(runs, decisions, logger)

	got, err := svc.BuildStatus(context.Background(), "r-run")
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if got.PausedPosition != nil {
		t.Fatalf("expected no PausedPosition, got %+v", got.PausedPosition)
	}
	if got.Summary != "" {
		t.Fatalf("expected empty Summary, got %q", got.Summary)
	}
	if got.ChangesSince != nil {
		t.Fatalf("expected no ChangesSince, got %+v", got.ChangesSince)
	}
	// DecisionsSummary is always populated (D1: 0/0/0 for zero counts).
	if got.DecisionsSummary == nil {
		t.Fatalf("expected DecisionsSummary non-nil, got nil")
	}
	testutil.AssertEqual(t, got.DecisionsSummary.ApprovedCount, 0)
	testutil.AssertEqual(t, got.DecisionsSummary.RejectedCount, 0)
	testutil.AssertEqual(t, got.DecisionsSummary.PendingCount, 0)
}
