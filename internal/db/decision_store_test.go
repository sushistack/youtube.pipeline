package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestDecisionStore_ListByRunID_OrdersByCreatedAtAsc(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Insert out of order so we verify ORDER BY (not insertion order).
	if _, err := database.Exec(`
		INSERT INTO decisions (run_id, scene_id, decision_type, created_at) VALUES
		  ('r1', '2', 'approve', '2026-01-01T00:30:00Z'),
		  ('r1', '0', 'approve', '2026-01-01T00:10:00Z'),
		  ('r1', '1', 'reject',  '2026-01-01T00:20:00Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.ListByRunID(ctx, "r1")
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	testutil.AssertEqual(t, len(got), 3)
	testutil.AssertEqual(t, *got[0].SceneID, "0")
	testutil.AssertEqual(t, *got[1].SceneID, "1")
	testutil.AssertEqual(t, *got[2].SceneID, "2")
}

func TestDecisionStore_ListByRunID_ExcludesSuperseded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Insert 3 decisions; mark one superseded.
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, created_at) VALUES
		  (1, 'r1', '0', 'reject',  '2026-01-01T00:10:00Z'),
		  (2, 'r1', '0', 'approve', '2026-01-01T00:20:00Z'),
		  (3, 'r1', '1', 'approve', '2026-01-01T00:30:00Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	if _, err := database.Exec(`UPDATE decisions SET superseded_by = 2 WHERE id = 1`); err != nil {
		t.Fatalf("mark superseded: %v", err)
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.ListByRunID(context.Background(), "r1")
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	testutil.AssertEqual(t, len(got), 2) // only id=2 and id=3 remain
	for _, d := range got {
		if d.ID == 1 {
			t.Fatalf("superseded decision id=1 should be excluded, got: %+v", d)
		}
	}
}

func TestDecisionStore_ListByRunID_EmptyRun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r-empty', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.ListByRunID(context.Background(), "r-empty")
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty run, got %+v", got)
	}
}

func TestDecisionStore_GetSession_NotPaused(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ds := db.NewDecisionStore(database)
	sess, err := ds.GetSession(context.Background(), "r1")
	if err != nil {
		t.Fatalf("GetSession unexpected error: %v", err)
	}
	if sess != nil {
		t.Fatalf("expected nil session for run with no hitl_sessions row, got %+v", sess)
	}
}

func TestDecisionStore_GetSession_RoundTrip(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ds := db.NewDecisionStore(database)
	want := &domain.HITLSession{
		RunID:                    "r1",
		Stage:                    domain.StageBatchReview,
		SceneIndex:               4,
		LastInteractionTimestamp: "2026-01-01T00:25:00Z",
		SnapshotJSON:             `{"total_scenes":10,"approved_count":4}`,
	}
	if err := ds.UpsertSession(context.Background(), want); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	got, err := ds.GetSession(context.Background(), "r1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got == nil {
		t.Fatalf("expected session, got nil")
	}
	testutil.AssertEqual(t, got.RunID, want.RunID)
	testutil.AssertEqual(t, got.Stage, want.Stage)
	testutil.AssertEqual(t, got.SceneIndex, want.SceneIndex)
	testutil.AssertEqual(t, got.LastInteractionTimestamp, want.LastInteractionTimestamp)
	testutil.AssertEqual(t, got.SnapshotJSON, want.SnapshotJSON)
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Fatalf("created_at/updated_at should auto-populate, got %+v", got)
	}
}

func TestDecisionStore_UpsertSession_UpdatesExisting(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ds := db.NewDecisionStore(database)
	first := &domain.HITLSession{
		RunID: "r1", Stage: domain.StageBatchReview, SceneIndex: 0,
		LastInteractionTimestamp: "2026-01-01T00:10:00Z", SnapshotJSON: `{}`,
	}
	if err := ds.UpsertSession(context.Background(), first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	var updatedAtBefore string
	if err := database.QueryRow("SELECT updated_at FROM hitl_sessions WHERE run_id = 'r1'").Scan(&updatedAtBefore); err != nil {
		t.Fatalf("read updated_at before: %v", err)
	}

	// SQLite datetime resolution is 1 second — sleep to guarantee advancement.
	time.Sleep(1100 * time.Millisecond)

	second := &domain.HITLSession{
		RunID: "r1", Stage: domain.StageBatchReview, SceneIndex: 4,
		LastInteractionTimestamp: "2026-01-01T00:25:00Z", SnapshotJSON: `{"approved":4}`,
	}
	if err := ds.UpsertSession(context.Background(), second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got, err := ds.GetSession(context.Background(), "r1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	testutil.AssertEqual(t, got.SceneIndex, 4)
	testutil.AssertEqual(t, got.LastInteractionTimestamp, "2026-01-01T00:25:00Z")
	testutil.AssertEqual(t, got.SnapshotJSON, `{"approved":4}`)
	if got.UpdatedAt == updatedAtBefore {
		t.Errorf("updated_at should advance after upsert: before=%q after=%q", updatedAtBefore, got.UpdatedAt)
	}
}

func TestDecisionStore_UpsertSession_OrphanFKFails(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	ds := db.NewDecisionStore(database)
	err := ds.UpsertSession(context.Background(), &domain.HITLSession{
		RunID: "nonexistent", Stage: domain.StageBatchReview, SceneIndex: 0,
		LastInteractionTimestamp: "2026-01-01T00:00:00Z", SnapshotJSON: `{}`,
	})
	if err == nil {
		t.Fatalf("expected FK violation for nonexistent run, got nil")
	}
}

func TestDecisionStore_DeleteSession_NoOpOnMissing(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	ds := db.NewDecisionStore(database)
	if err := ds.DeleteSession(context.Background(), "nonexistent"); err != nil {
		t.Fatalf("DeleteSession should no-op on missing, got %v", err)
	}
}

func TestDecisionStore_DeleteSession_RemovesRow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ds := db.NewDecisionStore(database)
	if err := ds.UpsertSession(context.Background(), &domain.HITLSession{
		RunID: "r1", Stage: domain.StageBatchReview, SceneIndex: 0,
		LastInteractionTimestamp: "2026-01-01T00:00:00Z", SnapshotJSON: `{}`,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := ds.DeleteSession(context.Background(), "r1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err := ds.GetSession(context.Background(), "r1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected deleted, got %+v", got)
	}
}

func TestDecisionStore_LatestDecisionIDForRuns_MaxNonSupersededApproveReject(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id) VALUES
		  ('r1', 'scp'),
		  ('r2', 'scp')`); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, created_at, superseded_by) VALUES
		  (1, 'r1', '0', 'approve', '2026-01-01T00:00:00Z', NULL),
		  (2, 'r1', '1', 'reject',  '2026-01-01T00:01:00Z', 4),
		  (3, 'r1', NULL, 'metadata_ack', '2026-01-01T00:02:00Z', NULL),
		  (4, 'r2', '2', 'approve', '2026-01-01T00:03:00Z', NULL),
		  (5, 'r2', '3', 'reject',  '2026-01-01T00:04:00Z', NULL)`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}

	got, err := db.NewDecisionStore(database).LatestDecisionIDForRuns(context.Background(), []string{"r1", "r2"})
	if err != nil {
		t.Fatalf("LatestDecisionIDForRuns: %v", err)
	}

	testutil.AssertEqual(t, got, 5)
}

func TestDecisionStore_LatestDecisionIDForRuns_EmptyRunIDs(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, created_at, superseded_by) VALUES
		  (1, 'r1', '0', 'approve', '2026-01-01T00:00:00Z', NULL)`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}

	ds := db.NewDecisionStore(database)
	got, err := ds.LatestDecisionIDForRuns(context.Background(), nil)
	if err != nil {
		t.Fatalf("LatestDecisionIDForRuns nil: %v", err)
	}
	testutil.AssertEqual(t, got, 0)

	got, err = ds.LatestDecisionIDForRuns(context.Background(), []string{})
	if err != nil {
		t.Fatalf("LatestDecisionIDForRuns empty slice: %v", err)
	}
	testutil.AssertEqual(t, got, 0)
}

func TestDecisionStore_LatestDecisionIDForRuns_AllSuperseded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, created_at, superseded_by) VALUES
		  (1, 'r1', '0', 'approve', '2026-01-01T00:00:00Z', 2),
		  (2, 'r1', '0', 'reject',  '2026-01-01T00:01:00Z', 3),
		  (3, 'r1', '0', 'approve', '2026-01-01T00:02:00Z', 4),
		  (4, 'r1', '0', 'reject',  '2026-01-01T00:03:00Z', 1)`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}

	got, err := db.NewDecisionStore(database).LatestDecisionIDForRuns(context.Background(), []string{"r1"})
	if err != nil {
		t.Fatalf("LatestDecisionIDForRuns: %v", err)
	}
	testutil.AssertEqual(t, got, 0)
}

func TestDecisionStore_DecisionCounts_EmptyRun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.DecisionCountsByRunID(context.Background(), "r1")
	if err != nil {
		t.Fatalf("DecisionCounts: %v", err)
	}
	testutil.AssertEqual(t, got.Approved, 0)
	testutil.AssertEqual(t, got.Rejected, 0)
	testutil.AssertEqual(t, got.TotalScenes, 0)
}

func TestDecisionStore_DecisionCounts_AllPending(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index) VALUES ('r1', ?)`, i); err != nil {
			t.Fatalf("seed segment %d: %v", i, err)
		}
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.DecisionCountsByRunID(context.Background(), "r1")
	if err != nil {
		t.Fatalf("DecisionCounts: %v", err)
	}
	testutil.AssertEqual(t, got.Approved, 0)
	testutil.AssertEqual(t, got.Rejected, 0)
	testutil.AssertEqual(t, got.TotalScenes, 10)
}

func TestDecisionStore_DecisionCounts_MixedStates(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index) VALUES ('r1', ?)`, i); err != nil {
			t.Fatalf("seed segment: %v", err)
		}
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (run_id, scene_id, decision_type, created_at) VALUES
		  ('r1', '0', 'approve', '2026-01-01T00:00:00Z'),
		  ('r1', '1', 'approve', '2026-01-01T00:00:00Z'),
		  ('r1', '2', 'approve', '2026-01-01T00:00:00Z'),
		  ('r1', '3', 'approve', '2026-01-01T00:00:00Z'),
		  ('r1', '4', 'reject',  '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.DecisionCountsByRunID(context.Background(), "r1")
	if err != nil {
		t.Fatalf("DecisionCounts: %v", err)
	}
	testutil.AssertEqual(t, got.Approved, 4)
	testutil.AssertEqual(t, got.Rejected, 1)
	testutil.AssertEqual(t, got.TotalScenes, 10)
}

func TestDecisionStore_DecisionCounts_DedupesSupersededRejections(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index) VALUES ('r1', ?)`, i); err != nil {
			t.Fatalf("seed segment: %v", err)
		}
	}
	// Scene 0: reject superseded by approve (V1 undo flow).
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, created_at) VALUES
		  (1, 'r1', '0', 'reject',  '2026-01-01T00:00:00Z'),
		  (2, 'r1', '0', 'approve', '2026-01-01T00:01:00Z')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := database.Exec(`UPDATE decisions SET superseded_by = 2 WHERE id = 1`); err != nil {
		t.Fatalf("mark superseded: %v", err)
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.DecisionCountsByRunID(context.Background(), "r1")
	if err != nil {
		t.Fatalf("DecisionCounts: %v", err)
	}
	testutil.AssertEqual(t, got.Approved, 1)
	testutil.AssertEqual(t, got.Rejected, 0)
	testutil.AssertEqual(t, got.TotalScenes, 10)
}

func TestDecisionStore_DecisionCounts_IgnoresNullSceneID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index) VALUES ('r1', 0)`); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	// A run-level decision (no scene_id) must not count as a scene-level approve.
	if _, err := database.Exec(`
		INSERT INTO decisions (run_id, scene_id, decision_type, created_at) VALUES
		  ('r1', NULL, 'approve', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.DecisionCountsByRunID(context.Background(), "r1")
	if err != nil {
		t.Fatalf("DecisionCounts: %v", err)
	}
	testutil.AssertEqual(t, got.Approved, 0)
	testutil.AssertEqual(t, got.Rejected, 0)
	testutil.AssertEqual(t, got.TotalScenes, 1)
}

func TestDecisionCountsByRunID_AutoApprovedNotPending(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index) VALUES ('r1', ?)`, i); err != nil {
			t.Fatalf("seed segment: %v", err)
		}
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (run_id, scene_id, decision_type, created_at) VALUES
		  ('r1', '0', 'system_auto_approved', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	got, err := db.NewDecisionStore(database).DecisionCountsByRunID(context.Background(), "r1")
	if err != nil {
		t.Fatalf("DecisionCountsByRunID: %v", err)
	}
	testutil.AssertEqual(t, got.Approved, 1)
	testutil.AssertEqual(t, got.Rejected, 0)
	testutil.AssertEqual(t, got.TotalScenes, 2)
}

func TestPrepareBatchReview_AutoApprovedSceneRecorded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting')`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index, critic_score) VALUES ('r1', 0, 0.91)`); err != nil {
		t.Fatalf("seed segment: %v", err)
	}

	autoContinue, err := db.NewDecisionStore(database).PrepareBatchReview(ctx, "r1",
		[]db.SceneReviewUpdate{
			{SceneIndex: 0, ReviewStatus: domain.ReviewStatusAutoApproved, AutoApproved: true},
		},
		[]db.AutoApprovalInput{{SceneIndex: 0, CriticScore: 0.91, Threshold: 0.85}},
	)
	if err != nil {
		t.Fatalf("PrepareBatchReview: %v", err)
	}
	testutil.AssertEqual(t, autoContinue, true)

	var (
		decisionType  string
		sceneID       string
		reviewStatus  string
		stage         string
		status        string
		humanOverride int
	)
	if err := database.QueryRow(`SELECT decision_type, scene_id FROM decisions WHERE run_id = 'r1'`).Scan(&decisionType, &sceneID); err != nil {
		t.Fatalf("select decision: %v", err)
	}
	if err := database.QueryRow(`SELECT review_status FROM segments WHERE run_id = 'r1' AND scene_index = 0`).Scan(&reviewStatus); err != nil {
		t.Fatalf("select segment: %v", err)
	}
	if err := database.QueryRow(`SELECT human_override FROM runs WHERE id = 'r1'`).Scan(&humanOverride); err != nil {
		t.Fatalf("select run: %v", err)
	}
	if err := database.QueryRow(`SELECT stage, status FROM runs WHERE id = 'r1'`).Scan(&stage, &status); err != nil {
		t.Fatalf("select run stage: %v", err)
	}
	testutil.AssertEqual(t, decisionType, domain.DecisionTypeSystemAutoApproved)
	testutil.AssertEqual(t, sceneID, "0")
	testutil.AssertEqual(t, reviewStatus, string(domain.ReviewStatusAutoApproved))
	testutil.AssertEqual(t, stage, string(domain.StageAssemble))
	testutil.AssertEqual(t, status, string(domain.StatusRunning))
	testutil.AssertEqual(t, humanOverride, 0)
}

func TestPrepareBatchReview_AutoApprovalIdempotentOnRerun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting')`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index, critic_score) VALUES ('r1', 0, 0.91)`); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	store := db.NewDecisionStore(database)
	results := []db.SceneReviewUpdate{
		{SceneIndex: 0, ReviewStatus: domain.ReviewStatusAutoApproved, AutoApproved: true},
	}
	autoApprovals := []db.AutoApprovalInput{{SceneIndex: 0, CriticScore: 0.91, Threshold: 0.85}}
	if _, err := store.PrepareBatchReview(ctx, "r1", results, autoApprovals); err != nil {
		t.Fatalf("PrepareBatchReview #1: %v", err)
	}
	if _, err := store.PrepareBatchReview(ctx, "r1", results, autoApprovals); err != nil {
		t.Fatalf("PrepareBatchReview #2: %v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM decisions WHERE run_id = 'r1' AND decision_type = 'system_auto_approved'`).Scan(&count); err != nil {
		t.Fatalf("count decisions: %v", err)
	}
	testutil.AssertEqual(t, count, 1)
}

func TestPrepareBatchReview_SystemDecisionDoesNotFlipHumanOverride(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, stage, status, human_override) VALUES ('r1', 'scp', 'batch_review', 'waiting', 0)`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index, critic_score) VALUES ('r1', 0, 0.91)`); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	if _, err := db.NewDecisionStore(database).PrepareBatchReview(ctx, "r1",
		[]db.SceneReviewUpdate{{SceneIndex: 0, ReviewStatus: domain.ReviewStatusAutoApproved, AutoApproved: true}},
		[]db.AutoApprovalInput{{SceneIndex: 0, CriticScore: 0.91, Threshold: 0.85}},
	); err != nil {
		t.Fatalf("PrepareBatchReview: %v", err)
	}
	var humanOverride int
	if err := database.QueryRow(`SELECT human_override FROM runs WHERE id = 'r1'`).Scan(&humanOverride); err != nil {
		t.Fatalf("select run: %v", err)
	}
	testutil.AssertEqual(t, humanOverride, 0)
}

func TestOverrideMinorSafeguard_SetsRunHumanOverride(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, human_override) VALUES ('r1', 'scp', 0)`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO segments (run_id, scene_index, review_status, safeguard_flags)
		VALUES ('r1', 0, 'waiting_for_review', '["Safeguard Triggered: Minors"]')`); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	store := db.NewDecisionStore(database)
	if err := store.OverrideMinorSafeguard(ctx, "r1", 0, "운영자 판단으로 허용"); err != nil {
		t.Fatalf("OverrideMinorSafeguard: %v", err)
	}
	var (
		reviewStatus  string
		humanOverride int
		decisionType  string
		note          string
	)
	if err := database.QueryRow(`SELECT review_status FROM segments WHERE run_id = 'r1' AND scene_index = 0`).Scan(&reviewStatus); err != nil {
		t.Fatalf("select segment: %v", err)
	}
	if err := database.QueryRow(`SELECT human_override FROM runs WHERE id = 'r1'`).Scan(&humanOverride); err != nil {
		t.Fatalf("select run: %v", err)
	}
	if err := database.QueryRow(`SELECT decision_type, note FROM decisions WHERE run_id = 'r1' AND scene_id = '0'`).Scan(&decisionType, &note); err != nil {
		t.Fatalf("select decision: %v", err)
	}
	testutil.AssertEqual(t, reviewStatus, string(domain.ReviewStatusApproved))
	testutil.AssertEqual(t, humanOverride, 1)
	testutil.AssertEqual(t, decisionType, domain.DecisionTypeOverride)
	testutil.AssertEqual(t, note, "운영자 판단으로 허용")
}

func TestDecisionStore_KappaPairsForRuns_FiltersSupersededAndNonScene(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, status, critic_score) VALUES ('r1', 'scp', 'completed', 0.85)`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, created_at) VALUES
		  (1, 'r1', '0', 'reject',  '2026-01-01T00:00:00Z'),
		  (2, 'r1', '0', 'approve', '2026-01-01T00:01:00Z'),
		  (3, 'r1', '1', 'reject',  '2026-01-01T00:02:00Z'),
		  (4, 'r1', '2', 'approve', '2026-01-01T00:03:00Z'),
		  (5, 'r1', NULL, 'approve','2026-01-01T00:04:00Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	if _, err := database.Exec(`UPDATE decisions SET superseded_by = 2 WHERE id = 1`); err != nil {
		t.Fatalf("mark superseded: %v", err)
	}

	ds := db.NewDecisionStore(database)
	got, err := ds.KappaPairsForRuns(context.Background(), []string{"r1"}, 0.70)
	if err != nil {
		t.Fatalf("KappaPairsForRuns: %v", err)
	}
	testutil.AssertEqual(t, len(got), 1)
	testutil.AssertEqual(t, got[0].CriticPass, true)
	testutil.AssertEqual(t, got[0].OperatorApprove, true)
}

func TestDecisionStore_KappaPairsForRuns_EmptyRunIDs(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	ds := db.NewDecisionStore(database)
	got, err := ds.KappaPairsForRuns(context.Background(), nil, 0.70)
	if err != nil {
		t.Fatalf("KappaPairsForRuns: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil slice, got %+v", got)
	}
}

func TestDecisionStore_DefectEscapeInRuns_CountsOnlyAutoPassedRejects(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, status, critic_score) VALUES ('r1', 'scp', 'completed', 0.85)`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO segments (run_id, scene_index, critic_score) VALUES
		  ('r1', 0, 0.80),
		  ('r1', 1, 0.90),
		  ('r1', 2, 0.60)`); err != nil {
		t.Fatalf("seed segments: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, created_at) VALUES
		  (1, 'r1', '0', 'reject',  '2026-01-01T00:00:00Z'),
		  (2, 'r1', '1', 'approve', '2026-01-01T00:01:00Z'),
		  (3, 'r1', '2', 'reject',  '2026-01-01T00:02:00Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}

	ds := db.NewDecisionStore(database)
	got, err := ds.DefectEscapeInRuns(context.Background(), []string{"r1"}, 0.70)
	if err != nil {
		t.Fatalf("DefectEscapeInRuns: %v", err)
	}
	testutil.AssertEqual(t, got.AutoPassedScenes, 2)
	testutil.AssertEqual(t, got.EscapedScenes, 1)
}

func TestDecisionStore_DefectEscapeInRuns_EmptyRunIDs(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	ds := db.NewDecisionStore(database)
	got, err := ds.DefectEscapeInRuns(context.Background(), nil, 0.70)
	if err != nil {
		t.Fatalf("DefectEscapeInRuns: %v", err)
	}
	testutil.AssertEqual(t, got.AutoPassedScenes, 0)
	testutil.AssertEqual(t, got.EscapedScenes, 0)
}

func TestDecisionStore_KappaPairsForRuns_UsesIndex(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, status, critic_score) VALUES ('r1', 'scp', 'completed', 0.85)`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO decisions (run_id, scene_id, decision_type, created_at) VALUES ('r1', '0', 'approve', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}

	rows, err := database.Query(`
		EXPLAIN QUERY PLAN
		SELECT d.run_id, d.decision_type, r.critic_score, COUNT(*)
		  FROM decisions d
		  JOIN runs r ON r.id = d.run_id
		 WHERE d.run_id IN (?)
		   AND d.scene_id IS NOT NULL
		   AND d.decision_type IN ('approve', 'reject')
		   AND d.superseded_by IS NULL
		   AND r.critic_score IS NOT NULL
		 GROUP BY d.run_id, d.decision_type, r.critic_score
		 ORDER BY d.run_id ASC, d.decision_type ASC`, "r1")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()

	plan := explainPlan(t, rows)
	if !strings.Contains(plan, "idx_decisions_run_id_type") {
		t.Fatalf("expected idx_decisions_run_id_type in plan, got:\n%s", plan)
	}
	if strings.Contains(plan, "SCAN decisions") {
		t.Fatalf("unexpected full scan:\n%s", plan)
	}
}

func TestDecisionStore_DefectEscapeInRuns_UsesIndex(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, status, critic_score) VALUES ('r1', 'scp', 'completed', 0.85)`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index, critic_score) VALUES ('r1', 0, 0.80)`); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO decisions (run_id, scene_id, decision_type, created_at) VALUES ('r1', '0', 'reject', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}

	rows, err := database.Query(`
		EXPLAIN QUERY PLAN
		SELECT COUNT(*),
		       SUM(
		           CASE
		               WHEN EXISTS (
		                   SELECT 1
		                     FROM decisions d
		                    WHERE d.run_id = s.run_id
		                      AND d.scene_id = CAST(s.scene_index AS TEXT)
		                      AND d.decision_type = 'reject'
		                      AND d.superseded_by IS NULL
		               ) THEN 1
		               ELSE 0
		           END
		       )
		  FROM segments s
		 WHERE s.run_id IN (?)
		   AND s.critic_score >= ?`, "r1", 0.70)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()

	plan := explainPlan(t, rows)
	if !strings.Contains(plan, "sqlite_autoindex_segments_1") {
		t.Fatalf("expected segments autoindex in plan, got:\n%s", plan)
	}
	if !strings.Contains(plan, "idx_decisions_run_id_type") {
		t.Fatalf("expected idx_decisions_run_id_type in plan, got:\n%s", plan)
	}
}

func TestDecisionStore_KappaPairsForRuns_TieBreaksToReject(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	ctx := context.Background()
	database := testutil.NewTestDB(t)

	// Seed: run r1 with critic_score=0.80 (passes threshold), equal approves and rejects.
	// Tie must conservatively break to reject (OperatorApprove=false).
	_, err := database.Exec(`INSERT INTO runs (id, scp_id, status, critic_score) VALUES ('r1', 'scp', 'completed', 0.80)`)
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
	for _, row := range []struct{ dt, scene string }{
		{"approve", "0"},
		{"approve", "1"},
		{"reject", "2"},
		{"reject", "3"},
	} {
		if _, err := database.Exec(
			`INSERT INTO decisions (run_id, decision_type, scene_id) VALUES ('r1', ?, ?)`,
			row.dt, row.scene,
		); err != nil {
			t.Fatalf("seed decision %s/%s: %v", row.dt, row.scene, err)
		}
	}

	store := db.NewDecisionStore(database)
	pairs, err := store.KappaPairsForRuns(ctx, []string{"r1"}, 0.70)
	if err != nil {
		t.Fatalf("KappaPairsForRuns: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].OperatorApprove {
		t.Fatal("tie (2 approves vs 2 rejects) must conservatively break to reject (OperatorApprove=false)")
	}
}

func explainPlan(t *testing.T, rows interface {
	Next() bool
	Scan(...any) error
}) string {
	t.Helper()
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
	return plan.String()
}
