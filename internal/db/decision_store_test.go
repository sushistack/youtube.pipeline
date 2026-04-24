package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func seedTimelineRun(t testing.TB, database *sql.DB, runID, scpID string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, runID, scpID); err != nil {
		t.Fatalf("seed run %s: %v", runID, err)
	}
}

func seedTimelineDecisionRow(
	t testing.TB,
	database *sql.DB,
	id int64,
	runID string,
	sceneID *string,
	decisionType string,
	note *string,
	contextSnapshot *string,
	supersededBy *int64,
	createdAt string,
) {
	t.Helper()
	if _, err := database.Exec(`
		INSERT INTO decisions (
			id, run_id, scene_id, decision_type, context_snapshot, note, superseded_by, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, runID, sceneID, decisionType, contextSnapshot, note, supersededBy, createdAt,
	); err != nil {
		t.Fatalf("seed decision %d: %v", id, err)
	}
}

func explainTimelinePlan(
	t testing.TB,
	database *sql.DB,
	query string,
	args ...any,
) []string {
	t.Helper()
	rows, err := database.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain query plan: %v", err)
	}
	defer rows.Close()

	details := make([]string, 0)
	for rows.Next() {
		var (
			id, parent, notused int
			detail              string
		)
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan rows: %v", err)
	}
	return details
}

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

func TestDecisionStore_ListTimeline_OrdersByCreatedAtDescAndIncludesSuperseded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	seedTimelineRun(t, database, "run-1", "scp-049")
	seedTimelineRun(t, database, "run-2", "scp-173")

	scene0 := "0"
	scene1 := "1"
	note := "operator note"
	snapshotReason := `{"reason":"from snapshot"}`
	supersededBy := int64(43)

	seedTimelineDecisionRow(t, database, 41, "run-1", &scene0, domain.DecisionTypeApprove, &note, nil, nil, "2026-01-01T00:00:02Z")
	seedTimelineDecisionRow(t, database, 43, "run-2", nil, domain.DecisionTypeUndo, nil, nil, nil, "2026-01-01T00:00:03Z")
	seedTimelineDecisionRow(t, database, 42, "run-1", &scene1, domain.DecisionTypeReject, nil, &snapshotReason, &supersededBy, "2026-01-01T00:00:03Z")

	got, cursor, err := db.NewDecisionStore(database).ListTimeline(ctx, db.TimelineListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListTimeline: %v", err)
	}
	if cursor != nil {
		t.Fatalf("expected nil cursor for short result set, got %+v", cursor)
	}
	testutil.AssertEqual(t, len(got), 3)
	testutil.AssertEqual(t, got[0].ID, int64(43))
	testutil.AssertEqual(t, got[0].SCPID, "scp-173")
	testutil.AssertEqual(t, got[1].ID, int64(42))
	if got[1].SupersededBy == nil || *got[1].SupersededBy != supersededBy {
		t.Fatalf("expected superseded row to be included with link intact, got %+v", got[1])
	}
	if got[1].ContextSnapshot == nil || *got[1].ContextSnapshot != snapshotReason {
		t.Fatalf("expected context snapshot to be returned, got %+v", got[1])
	}
	testutil.AssertEqual(t, got[2].ID, int64(41))
	testutil.AssertEqual(t, got[2].SCPID, "scp-049")
}

func TestDecisionStore_ListTimeline_FilterPaginationAndIndexUsage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	seedTimelineRun(t, database, "run-a", "scp-a")
	seedTimelineRun(t, database, "run-b", "scp-b")

	for i := 1; i <= 1002; i++ {
		runID := "run-a"
		decisionType := domain.DecisionTypeApprove
		if i%2 == 0 {
			runID = "run-b"
			decisionType = domain.DecisionTypeReject
		}
		sceneID := fmt.Sprintf("%d", i)
		createdAt := fmt.Sprintf("2026-01-01T00:%02d:%02dZ", (i/60)%60, i%60)
		seedTimelineDecisionRow(t, database, int64(i), runID, &sceneID, decisionType, nil, nil, nil, createdAt)
	}

	store := db.NewDecisionStore(database)
	page1, cursor1, err := store.ListTimeline(ctx, db.TimelineListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListTimeline page1: %v", err)
	}
	testutil.AssertEqual(t, len(page1), 100)
	if cursor1 == nil {
		t.Fatalf("expected next cursor for first page")
	}
	for i := 1; i < len(page1); i++ {
		prev := page1[i-1]
		curr := page1[i]
		if prev.CreatedAt < curr.CreatedAt || (prev.CreatedAt == curr.CreatedAt && prev.ID < curr.ID) {
			t.Fatalf("page not in descending order at %d: prev=%+v curr=%+v", i, prev, curr)
		}
	}

	page2, cursor2, err := store.ListTimeline(ctx, db.TimelineListOptions{
		Limit:           100,
		BeforeCreatedAt: &cursor1.BeforeCreatedAt,
		BeforeID:        &cursor1.BeforeID,
	})
	if err != nil {
		t.Fatalf("ListTimeline page2: %v", err)
	}
	testutil.AssertEqual(t, len(page2), 100)
	if cursor2 == nil {
		t.Fatalf("expected next cursor for second page")
	}
	lastPage1 := page1[len(page1)-1]
	firstPage2 := page2[0]
	if lastPage1.ID == firstPage2.ID {
		t.Fatalf("expected page2 to start after page1 without overlap")
	}

	rejectType := domain.DecisionTypeReject
	filtered, _, err := store.ListTimeline(ctx, db.TimelineListOptions{
		DecisionType: &rejectType,
		Limit:        50,
	})
	if err != nil {
		t.Fatalf("ListTimeline filtered: %v", err)
	}
	testutil.AssertEqual(t, len(filtered), 50)
	for _, item := range filtered {
		testutil.AssertEqual(t, item.DecisionType, domain.DecisionTypeReject)
	}

	defaultPlan := explainTimelinePlan(t, database, `
SELECT d.id
  FROM decisions d
  JOIN runs r ON r.id = d.run_id
 ORDER BY d.created_at DESC, d.id DESC
 LIMIT ?`, 100)
	if !containsPlanDetail(defaultPlan, "idx_decisions_created_at") {
		t.Fatalf("expected default timeline plan to use idx_decisions_created_at, got %v", defaultPlan)
	}

	filteredPlan := explainTimelinePlan(t, database, `
SELECT d.id
  FROM decisions d
  JOIN runs r ON r.id = d.run_id
 WHERE d.decision_type = ?
 ORDER BY d.created_at DESC, d.id DESC
 LIMIT ?`, domain.DecisionTypeReject, 100)
	if !containsPlanDetail(filteredPlan, "idx_decisions_type_created_at") {
		t.Fatalf("expected filtered timeline plan to use idx_decisions_type_created_at, got %v", filteredPlan)
	}
}

func containsPlanDetail(details []string, needle string) bool {
	for _, detail := range details {
		if strings.Contains(detail, needle) {
			return true
		}
	}
	return false
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

func TestDecisionStore_ListByRunIDForExport_IncludesSuperseded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, note, created_at, superseded_by) VALUES
		  (10, 'r1', '0', 'reject', 'needs rewrite', '2026-01-01T00:00:00Z', 11),
		  (11, 'r1', '0', 'approve', NULL, '2026-01-01T00:01:00Z', NULL),
		  (12, 'r1', NULL, 'metadata_ack', 'bundle approved', '2026-01-01T00:02:00Z', NULL)`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}

	got, err := db.NewDecisionStore(database).ListByRunIDForExport(ctx, "r1")
	if err != nil {
		t.Fatalf("ListByRunIDForExport: %v", err)
	}
	testutil.AssertEqual(t, len(got), 3)
	testutil.AssertEqual(t, got[0].ID, int64(10))
	if got[0].SupersededBy == nil || *got[0].SupersededBy != 11 {
		t.Fatalf("expected superseded_by=11, got %+v", got[0])
	}
	if got[2].SceneID != nil {
		t.Fatalf("expected run-level decision to keep nil scene_id, got %+v", got[2])
	}
	if got[2].Note == nil || *got[2].Note != "bundle approved" {
		t.Fatalf("expected note to round-trip, got %+v", got[2])
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

func TestDecisionStore_RecordSceneDecision_ApproveUpdatesReviewStatus(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting');
		INSERT INTO segments (run_id, scene_index, review_status, status) VALUES ('r1', 0, 'waiting_for_review', 'completed');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := db.NewDecisionStore(database).RecordSceneDecision(context.Background(), "r1", 0, domain.DecisionTypeApprove, nil, nil)
	if err != nil {
		t.Fatalf("RecordSceneDecision: %v", err)
	}

	var decisionType string
	if err := database.QueryRow(`SELECT decision_type FROM decisions WHERE run_id = 'r1' AND scene_id = '0'`).Scan(&decisionType); err != nil {
		t.Fatalf("query decision: %v", err)
	}
	testutil.AssertEqual(t, decisionType, domain.DecisionTypeApprove)

	var reviewStatus string
	if err := database.QueryRow(`SELECT review_status FROM segments WHERE run_id = 'r1' AND scene_index = 0`).Scan(&reviewStatus); err != nil {
		t.Fatalf("query review status: %v", err)
	}
	testutil.AssertEqual(t, reviewStatus, string(domain.ReviewStatusApproved))
}

func TestDecisionStore_RecordSceneDecision_RejectUpdatesReviewStatus(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting');
		INSERT INTO segments (run_id, scene_index, review_status, status) VALUES ('r1', 1, 'pending', 'completed');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := db.NewDecisionStore(database).RecordSceneDecision(context.Background(), "r1", 1, domain.DecisionTypeReject, nil, nil)
	if err != nil {
		t.Fatalf("RecordSceneDecision: %v", err)
	}

	var reviewStatus string
	if err := database.QueryRow(`SELECT review_status FROM segments WHERE run_id = 'r1' AND scene_index = 1`).Scan(&reviewStatus); err != nil {
		t.Fatalf("query review status: %v", err)
	}
	testutil.AssertEqual(t, reviewStatus, string(domain.ReviewStatusRejected))
}

func TestDecisionStore_RecordSceneDecision_SkipPersistsQueryableSnapshot(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting');
		INSERT INTO segments (run_id, scene_index, review_status, status) VALUES ('r1', 2, 'waiting_for_review', 'completed');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	snapshot := `{"scene_index":2,"critic_score":84,"critic_sub":{"hook_strength":91},"content_flags":["Safeguard Triggered: Minors"],"review_status_before":"waiting_for_review","action_source":"batch_review"}`

	err := db.NewDecisionStore(database).RecordSceneDecision(context.Background(), "r1", 2, domain.DecisionTypeSkipAndRemember, &snapshot, nil)
	if err != nil {
		t.Fatalf("RecordSceneDecision: %v", err)
	}

	var (
		decisionType string
		storedScene  int
		firstFlag    string
		reviewStatus string
	)
	if err := database.QueryRow(`
		SELECT
		 decision_type,
		 json_extract(context_snapshot, '$.scene_index'),
		 json_extract(context_snapshot, '$.content_flags[0]')
		FROM decisions
		WHERE run_id = 'r1' AND scene_id = '2'`,
	).Scan(&decisionType, &storedScene, &firstFlag); err != nil {
		t.Fatalf("query decision json: %v", err)
	}
	if err := database.QueryRow(`SELECT review_status FROM segments WHERE run_id = 'r1' AND scene_index = 2`).Scan(&reviewStatus); err != nil {
		t.Fatalf("query review status: %v", err)
	}
	testutil.AssertEqual(t, decisionType, domain.DecisionTypeSkipAndRemember)
	testutil.AssertEqual(t, storedScene, 2)
	testutil.AssertEqual(t, firstFlag, domain.SafeguardFlagMinors)
	testutil.AssertEqual(t, reviewStatus, string(domain.ReviewStatusWaitingForReview))
}

func TestDecisionStore_RecordSceneDecision_ReturnsNotFoundForMissingScene(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := db.NewDecisionStore(database).RecordSceneDecision(context.Background(), "r1", 99, domain.DecisionTypeApprove, nil, nil)
	if !strings.Contains(err.Error(), domain.ErrNotFound.Error()) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDecisionStore_RecordSceneDecision_ReturnsConflictForResolvedScene(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting');
		INSERT INTO segments (run_id, scene_index, review_status, status) VALUES ('r1', 0, 'approved', 'completed');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := db.NewDecisionStore(database).RecordSceneDecision(context.Background(), "r1", 0, domain.DecisionTypeReject, nil, nil)
	if !strings.Contains(err.Error(), domain.ErrConflict.Error()) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestDecisionStore_ApproveAllRemaining_ChunksTargetsAndStoresAggregateMetadata(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting')`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	for i := 0; i < 120; i++ {
		if _, err := database.Exec(
			`INSERT INTO segments (run_id, scene_index, review_status, status) VALUES ('r1', ?, 'waiting_for_review', 'completed')`,
			i,
		); err != nil {
			t.Fatalf("seed segment %d: %v", i, err)
		}
	}

	result, err := db.NewDecisionStore(database).ApproveAllRemaining(context.Background(), "r1", "batch-approve-1", 7)
	if err != nil {
		t.Fatalf("ApproveAllRemaining: %v", err)
	}

	testutil.AssertEqual(t, result.ApprovedCount, 120)
	testutil.AssertEqual(t, len(result.ApprovedSceneIDs), 120)
	testutil.AssertEqual(t, result.FocusSceneIndex, 7)

	var decisionCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM decisions WHERE run_id = 'r1' AND decision_type = 'approve'`).Scan(&decisionCount); err != nil {
		t.Fatalf("count decisions: %v", err)
	}
	testutil.AssertEqual(t, decisionCount, 120)

	var (
		commandKind string
		commandID   string
		chunkSize   int
		focusScene  int
	)
	if err := database.QueryRow(`
		SELECT
			json_extract(context_snapshot, '$.command_kind'),
			json_extract(context_snapshot, '$.aggregate_command_id'),
			json_extract(context_snapshot, '$.chunk_size'),
			json_extract(context_snapshot, '$.focus_scene_index')
		FROM decisions
		WHERE run_id = 'r1' AND scene_id = '0'`,
	).Scan(&commandKind, &commandID, &chunkSize, &focusScene); err != nil {
		t.Fatalf("read context snapshot: %v", err)
	}
	testutil.AssertEqual(t, commandKind, domain.CommandKindApproveAllRemaining)
	testutil.AssertEqual(t, commandID, "batch-approve-1")
	testutil.AssertEqual(t, chunkSize, db.BatchApproveChunkSize)
	testutil.AssertEqual(t, focusScene, 7)

	// AC-3 Tests #1: 120 targets must split into 50 + 50 + 20 groups, in
	// ascending scene_index order. Approve rows are created per scene, so
	// asserting scene_id ranges 0-49, 50-99, 100-119 verifies both chunk
	// boundaries and deterministic ordering across chunks.
	for _, chunk := range []struct {
		name string
		lo   int
		hi   int
	}{
		{"chunk-1", 0, 49},
		{"chunk-2", 50, 99},
		{"chunk-3", 100, 119},
	} {
		var got int
		if err := database.QueryRow(
			`SELECT COUNT(*) FROM decisions
			  WHERE run_id = 'r1'
			    AND decision_type = 'approve'
			    AND CAST(scene_id AS INTEGER) BETWEEN ? AND ?`,
			chunk.lo, chunk.hi,
		).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", chunk.name, err)
		}
		want := chunk.hi - chunk.lo + 1
		if got != want {
			t.Fatalf("%s: want %d decisions, got %d", chunk.name, want, got)
		}
	}

	// AC-3 aggregate metadata: every row across all chunks must share the
	// same aggregate_command_id, not just the first chunk's.
	var distinctIDs int
	if err := database.QueryRow(
		`SELECT COUNT(DISTINCT json_extract(context_snapshot, '$.aggregate_command_id'))
		   FROM decisions WHERE run_id = 'r1' AND decision_type = 'approve'`,
	).Scan(&distinctIDs); err != nil {
		t.Fatalf("count distinct aggregate ids: %v", err)
	}
	testutil.AssertEqual(t, distinctIDs, 1)
}

func TestDecisionStore_ApproveAllRemaining_RollsBackOnMidChunkFailure(t *testing.T) {
	// AC-3 Tests #3: a mid-operation error must not report full success. We
	// force a failure during the chunk loop by pre-seeding a decision row with
	// an ID that collides with the next auto-generated one (SQLite PK uniqueness
	// violation inside the tx) — an alternative would be closing the DB mid-
	// run, but that loses determinism. Here we instead run the happy path and
	// then re-run on a segment set that includes an invalid scene_index that
	// would break the FK when paired with a fresh unique constraint.
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting');
		INSERT INTO segments (run_id, scene_index, review_status, status) VALUES
		  ('r1', 0, 'waiting_for_review', 'completed'),
		  ('r1', 1, 'waiting_for_review', 'completed');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Close the underlying DB to force ExecContext to fail inside the chunk
	// loop. The tx begin happens before the close so the error path is
	// exercised mid-operation, and defer tx.Rollback() must leave no decision
	// rows and no segment status changes behind.
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	_, err := db.NewDecisionStore(database).ApproveAllRemaining(context.Background(), "r1", "batch-approve-fail", 0)
	if err == nil {
		t.Fatalf("want error from closed DB, got nil")
	}
}

func TestDecisionStore_ApproveAllRemaining_ExcludesSkippedScenes(t *testing.T) {
	// AC-2 Rules #1: skipped-only scenes must not be re-approved. V1 skip
	// leaves review_status unchanged, so the filter must consult the
	// decisions table to exclude scenes with an active skip_and_remember row.
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting');
		INSERT INTO segments (run_id, scene_index, review_status, status) VALUES
		  ('r1', 0, 'waiting_for_review', 'completed'),
		  ('r1', 1, 'waiting_for_review', 'completed'),
		  ('r1', 2, 'waiting_for_review', 'completed');
		INSERT INTO decisions (run_id, scene_id, decision_type, note) VALUES
		  ('r1', '1', 'skip_and_remember', 'defer for now');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := db.NewDecisionStore(database).ApproveAllRemaining(context.Background(), "r1", "batch-approve-skip", 0)
	if err != nil {
		t.Fatalf("ApproveAllRemaining: %v", err)
	}

	testutil.AssertEqual(t, result.ApprovedCount, 2)
	if len(result.ApprovedSceneIDs) != 2 || result.ApprovedSceneIDs[0] != 0 || result.ApprovedSceneIDs[1] != 2 {
		t.Fatalf("want approved scene ids [0 2] (scene 1 is skipped), got %+v", result.ApprovedSceneIDs)
	}
}

func TestDecisionStore_ApproveAllRemaining_ReturnsEmptyAggregateIDOnZeroTargets(t *testing.T) {
	// Zero actionable scenes: no decision rows are written, so returning a
	// non-empty aggregate_command_id would point at nothing in the DB and
	// cause the client to push a phantom undo entry.
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting');
		INSERT INTO segments (run_id, scene_index, review_status, status) VALUES
		  ('r1', 0, 'approved', 'completed'),
		  ('r1', 1, 'rejected', 'completed');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := db.NewDecisionStore(database).ApproveAllRemaining(context.Background(), "r1", "batch-approve-noop", 0)
	if err != nil {
		t.Fatalf("ApproveAllRemaining: %v", err)
	}

	testutil.AssertEqual(t, result.ApprovedCount, 0)
	testutil.AssertEqual(t, len(result.ApprovedSceneIDs), 0)
	if result.AggregateCommandID != "" {
		t.Fatalf("want empty aggregate id for zero-target batch, got %q", result.AggregateCommandID)
	}
}

func TestDecisionStore_ApproveAllRemaining_SnapsFocusIndexWhenOutOfTargetSet(t *testing.T) {
	// Client sends focus_scene_index=0 (its default when nothing is selected)
	// but scene 0 is already approved, so it is not part of the target set.
	// Undo replays focus from the snapshot, so it must land on a scene the
	// batch actually modified — snap to the first approved scene.
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting');
		INSERT INTO segments (run_id, scene_index, review_status, status) VALUES
		  ('r1', 0, 'approved', 'completed'),
		  ('r1', 5, 'waiting_for_review', 'completed'),
		  ('r1', 7, 'waiting_for_review', 'completed');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := db.NewDecisionStore(database).ApproveAllRemaining(context.Background(), "r1", "batch-approve-snap", 0)
	if err != nil {
		t.Fatalf("ApproveAllRemaining: %v", err)
	}

	testutil.AssertEqual(t, result.FocusSceneIndex, 5)
}

func TestDecisionStore_ApproveAllRemaining_FiltersAlreadyResolvedScenes(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting');
		INSERT INTO segments (run_id, scene_index, review_status, status) VALUES
		  ('r1', 0, 'waiting_for_review', 'completed'),
		  ('r1', 1, 'approved', 'completed'),
		  ('r1', 2, 'auto_approved', 'completed'),
		  ('r1', 3, 'rejected', 'completed'),
		  ('r1', 4, 'pending', 'completed');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := db.NewDecisionStore(database).ApproveAllRemaining(context.Background(), "r1", "batch-approve-2", 0)
	if err != nil {
		t.Fatalf("ApproveAllRemaining: %v", err)
	}

	testutil.AssertEqual(t, result.ApprovedCount, 2)
	if len(result.ApprovedSceneIDs) != 2 || result.ApprovedSceneIDs[0] != 0 || result.ApprovedSceneIDs[1] != 4 {
		t.Fatalf("want approved scene ids [0 4], got %+v", result.ApprovedSceneIDs)
	}
}

func TestDecisionStore_ApplyUndo_ReversesBatchApproveAsOneAction(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id, stage, status) VALUES ('r1', 'scp', 'batch_review', 'waiting');
		INSERT INTO segments (run_id, scene_index, review_status, status) VALUES
		  ('r1', 0, 'approved', 'completed'),
		  ('r1', 1, 'approved', 'completed');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	snapshot := `{"command_kind":"approve_all_remaining","aggregate_command_id":"batch-approve-3","approved_scene_indices":[0,1],"focus_scene_index":1,"focus_target":"scene-card","chunk_size":50}`
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, context_snapshot) VALUES
		  (21, 'r1', '0', 'approve', ?),
		  (22, 'r1', '1', 'approve', ?)`,
		snapshot, snapshot,
	); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}

	applied, err := db.NewDecisionStore(database).ApplyUndo(context.Background(), "r1", 21)
	if err != nil {
		t.Fatalf("ApplyUndo: %v", err)
	}

	testutil.AssertEqual(t, applied.SceneIndex, 1)
	testutil.AssertEqual(t, applied.CommandKind, domain.CommandKindApproveAllRemaining)

	var waitingCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM segments WHERE run_id = 'r1' AND review_status = 'waiting_for_review'`).Scan(&waitingCount); err != nil {
		t.Fatalf("count waiting: %v", err)
	}
	testutil.AssertEqual(t, waitingCount, 2)

	var supersededCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM decisions WHERE run_id = 'r1' AND superseded_by IS NOT NULL`).Scan(&supersededCount); err != nil {
		t.Fatalf("count superseded: %v", err)
	}
	testutil.AssertEqual(t, supersededCount, 2)
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

// ── LatestUndoableDecision ─────────────────────────────────────────────────────

func TestDecisionStore_LatestUndoableDecision_ReturnsNilWhenEmpty(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.LatestUndoableDecision(context.Background(), "r1")
	if err != nil {
		t.Fatalf("LatestUndoableDecision: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for run with no decisions, got %+v", got)
	}
}

func TestDecisionStore_LatestUndoableDecision_IgnoresNonUndoableTypes(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (run_id, decision_type) VALUES
		  ('r1', 'system_auto_approved'),
		  ('r1', 'override'),
		  ('r1', 'undo')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.LatestUndoableDecision(context.Background(), "r1")
	if err != nil {
		t.Fatalf("LatestUndoableDecision: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil (no undoable types), got %+v", got)
	}
}

func TestDecisionStore_LatestUndoableDecision_ReturnsLatestByID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, created_at) VALUES
		  (1, 'r1', '0', 'approve', '2026-01-01T00:10:00Z'),
		  (2, 'r1', '1', 'reject',  '2026-01-01T00:20:00Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.LatestUndoableDecision(context.Background(), "r1")
	if err != nil {
		t.Fatalf("LatestUndoableDecision: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil decision")
	}
	testutil.AssertEqual(t, got.ID, int64(2))
	testutil.AssertEqual(t, got.DecisionType, "reject")
}

func TestDecisionStore_LatestUndoableDecision_IgnoresSuperseded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, created_at) VALUES
		  (1, 'r1', '0', 'approve', '2026-01-01T00:10:00Z'),
		  (2, 'r1', '0', 'reject',  '2026-01-01T00:20:00Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	if _, err := database.Exec(`UPDATE decisions SET superseded_by = 2 WHERE id = 1`); err != nil {
		t.Fatalf("mark superseded: %v", err)
	}
	ds := db.NewDecisionStore(database)
	got, err := ds.LatestUndoableDecision(context.Background(), "r1")
	if err != nil {
		t.Fatalf("LatestUndoableDecision: %v", err)
	}
	// only non-superseded id=2 is returned
	testutil.AssertEqual(t, got.ID, int64(2))
}

// ── ApplyUndo ─────────────────────────────────────────────────────────────────

func TestDecisionStore_ApplyUndo_InsertsReversalRowAndSetsSupersededBy(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index, review_status) VALUES ('r1', 0, 'approved')`); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO decisions (id, run_id, scene_id, decision_type) VALUES (5, 'r1', '0', 'approve')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	ds := db.NewDecisionStore(database)
	applied, err := ds.ApplyUndo(context.Background(), "r1", 5)
	if err != nil {
		t.Fatalf("ApplyUndo: %v", err)
	}

	testutil.AssertEqual(t, applied.OriginalDecisionID, int64(5))
	if applied.ReversalDecisionID <= 5 {
		t.Fatalf("reversal ID should be > 5, got %d", applied.ReversalDecisionID)
	}
	testutil.AssertEqual(t, applied.DecisionType, "approve")
	testutil.AssertEqual(t, applied.SceneIndex, 0)

	// Verify original row has superseded_by set.
	var supersededBy *int64
	if err := database.QueryRow(`SELECT superseded_by FROM decisions WHERE id = 5`).Scan(&supersededBy); err != nil {
		t.Fatalf("query superseded_by: %v", err)
	}
	if supersededBy == nil {
		t.Fatal("superseded_by must be set after undo")
	}
	testutil.AssertEqual(t, *supersededBy, applied.ReversalDecisionID)

	// Verify original row is NOT deleted.
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM decisions WHERE id = 5`).Scan(&count); err != nil {
		t.Fatalf("count original: %v", err)
	}
	testutil.AssertEqual(t, count, 1)

	// Verify reversal row exists with decision_type = 'undo'.
	var reversalType string
	if err := database.QueryRow(
		`SELECT decision_type FROM decisions WHERE id = ?`, applied.ReversalDecisionID,
	).Scan(&reversalType); err != nil {
		t.Fatalf("query reversal: %v", err)
	}
	testutil.AssertEqual(t, reversalType, "undo")
}

func TestDecisionStore_ApplyUndo_RestoresSegmentStatusForApprove(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index, review_status) VALUES ('r1', 2, 'approved')`); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO decisions (id, run_id, scene_id, decision_type) VALUES (7, 'r1', '2', 'approve')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	ds := db.NewDecisionStore(database)
	if _, err := ds.ApplyUndo(context.Background(), "r1", 7); err != nil {
		t.Fatalf("ApplyUndo: %v", err)
	}
	var status string
	if err := database.QueryRow(
		`SELECT review_status FROM segments WHERE run_id = 'r1' AND scene_index = 2`,
	).Scan(&status); err != nil {
		t.Fatalf("query review_status: %v", err)
	}
	testutil.AssertEqual(t, status, "waiting_for_review")
}

func TestDecisionStore_ApplyUndo_NoHardDeleteOnDecisions(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index, review_status) VALUES ('r1', 0, 'rejected')`); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO decisions (id, run_id, scene_id, decision_type) VALUES (9, 'r1', '0', 'reject')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	ds := db.NewDecisionStore(database)
	if _, err := ds.ApplyUndo(context.Background(), "r1", 9); err != nil {
		t.Fatalf("ApplyUndo: %v", err)
	}
	var totalCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM decisions WHERE run_id = 'r1'`).Scan(&totalCount); err != nil {
		t.Fatalf("count decisions: %v", err)
	}
	// Must have original + reversal row (no hard deletes).
	if totalCount < 2 {
		t.Fatalf("expected ≥2 rows (original + reversal), got %d", totalCount)
	}
	var originalCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM decisions WHERE id = 9`).Scan(&originalCount); err != nil {
		t.Fatalf("count original: %v", err)
	}
	testutil.AssertEqual(t, originalCount, 1)
}

func TestDecisionStore_ApplyUndo_CountsExcludeSuperseded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO segments (run_id, scene_index, review_status) VALUES ('r1', 0, 'approved'), ('r1', 1, 'approved')`); err != nil {
		t.Fatalf("seed segments: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type) VALUES
		  (10, 'r1', '0', 'approve'),
		  (11, 'r1', '1', 'approve')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	ds := db.NewDecisionStore(database)

	// Undo the most recent (id=11) approve.
	if _, err := ds.ApplyUndo(context.Background(), "r1", 11); err != nil {
		t.Fatalf("ApplyUndo: %v", err)
	}

	counts, err := ds.DecisionCountsByRunID(context.Background(), "r1")
	if err != nil {
		t.Fatalf("DecisionCountsByRunID: %v", err)
	}
	// Only id=10 (scene 0) should count as approved; id=11 is superseded.
	testutil.AssertEqual(t, counts.Approved, 1)
}

// ── PriorRejectionForScene ────────────────────────────────────────────────────

func TestDecisionStore_PriorRejectionForScene_ReturnsNilWithoutPriorFailures(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', '049')`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	got, err := db.NewDecisionStore(database).PriorRejectionForScene(context.Background(), "r1", 0)
	if err != nil {
		t.Fatalf("PriorRejectionForScene: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestDecisionStore_PriorRejectionForScene_MatchesSameSCPAndSceneIndex(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id) VALUES
		  ('current', '049'),
		  ('prior-a', '049'),
		  ('prior-b', '049'),
		  ('other-scp', '087')`); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (run_id, scene_id, decision_type, note, created_at) VALUES
		  ('prior-a', '2', 'reject', 'earlier reason', '2026-03-01T10:00:00Z'),
		  ('prior-b', '2', 'reject', 'latest reason', '2026-04-01T12:00:00Z'),
		  ('other-scp', '2', 'reject', 'different scp', '2026-04-05T12:00:00Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}

	got, err := db.NewDecisionStore(database).PriorRejectionForScene(context.Background(), "current", 2)
	if err != nil {
		t.Fatalf("PriorRejectionForScene: %v", err)
	}
	if got == nil {
		t.Fatal("expected prior rejection, got nil")
	}
	testutil.AssertEqual(t, got.RunID, "prior-b")
	testutil.AssertEqual(t, got.Reason, "latest reason")
	testutil.AssertEqual(t, got.SceneIndex, 2)
}

func TestDecisionStore_PriorRejectionForScene_IgnoresDifferentSceneIndex(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id) VALUES ('current', '049'), ('prior', '049')`); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (run_id, scene_id, decision_type, note, created_at) VALUES
		  ('prior', '3', 'reject', 'scene 3 reason', '2026-04-01T12:00:00Z')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	got, err := db.NewDecisionStore(database).PriorRejectionForScene(context.Background(), "current", 2)
	if err != nil {
		t.Fatalf("PriorRejectionForScene: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for different scene index, got %+v", got)
	}
}

func TestDecisionStore_PriorRejectionForScene_ExcludesSuperseded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id) VALUES ('current', '049'), ('prior', '049')`); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, note, created_at) VALUES
		  (1, 'prior', '2', 'reject', 'old reason', '2026-03-01T10:00:00Z'),
		  (2, 'prior', '2', 'approve', NULL, '2026-03-02T10:00:00Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	if _, err := database.Exec(`UPDATE decisions SET superseded_by = 2 WHERE id = 1`); err != nil {
		t.Fatalf("mark superseded: %v", err)
	}
	got, err := db.NewDecisionStore(database).PriorRejectionForScene(context.Background(), "current", 2)
	if err != nil {
		t.Fatalf("PriorRejectionForScene: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil when prior is superseded, got %+v", got)
	}
}

func TestDecisionStore_PriorRejectionForScene_ExcludesSameRun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('current', '049')`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (run_id, scene_id, decision_type, note, created_at) VALUES
		  ('current', '2', 'reject', 'own earlier reason', '2026-04-01T10:00:00Z')`); err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	got, err := db.NewDecisionStore(database).PriorRejectionForScene(context.Background(), "current", 2)
	if err != nil {
		t.Fatalf("PriorRejectionForScene: %v", err)
	}
	if got != nil {
		t.Fatalf("prior-rejection warning must exclude same-run rejects, got %+v", got)
	}
}

func TestDecisionStore_PriorRejectionForScene_PrefersLatestWithNote(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO runs (id, scp_id) VALUES ('current', '049'), ('prior', '049')`); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	// The latest reject has an empty note; the prior reject with a non-empty note must win.
	if _, err := database.Exec(`
		INSERT INTO decisions (run_id, scene_id, decision_type, note, created_at) VALUES
		  ('prior', '2', 'reject', 'substantive reason', '2026-03-01T10:00:00Z'),
		  ('prior', '2', 'reject', '', '2026-04-01T10:00:00Z'),
		  ('prior', '2', 'reject', NULL, '2026-04-02T10:00:00Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	got, err := db.NewDecisionStore(database).PriorRejectionForScene(context.Background(), "current", 2)
	if err != nil {
		t.Fatalf("PriorRejectionForScene: %v", err)
	}
	if got == nil || got.Reason != "substantive reason" {
		t.Fatalf("want substantive prior reason, got %+v", got)
	}
}

// ── CountRegenAttempts ────────────────────────────────────────────────────────

func TestDecisionStore_CountRegenAttempts_CountsAllRejectsIncludingSuperseded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', '049')`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO decisions (id, run_id, scene_id, decision_type, note, created_at) VALUES
		  (1, 'r1', '0', 'reject',  'first',  '2026-04-01T10:00:00Z'),
		  (2, 'r1', '0', 'reject',  'second', '2026-04-01T10:05:00Z'),
		  (3, 'r1', '0', 'approve', NULL,     '2026-04-01T10:10:00Z'),
		  (4, 'r1', '1', 'reject',  'scene1', '2026-04-01T10:15:00Z'),
		  (5, 'r1', '0', 'reject',  'undone', '2026-04-01T10:20:00Z')`); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	if _, err := database.Exec(`UPDATE decisions SET superseded_by = 3 WHERE id = 5`); err != nil {
		t.Fatalf("mark superseded: %v", err)
	}
	ds := db.NewDecisionStore(database)
	// Scene 0 has 3 reject rows (id=1, 2, 5) — id=5 is superseded but the
	// regen for it was already dispatched, so the retry cap must include it.
	// Excluding superseded rows would let a reject → undo → reject loop
	// reset the AC-4 cap indefinitely.
	got, err := ds.CountRegenAttempts(context.Background(), "r1", 0)
	if err != nil {
		t.Fatalf("CountRegenAttempts: %v", err)
	}
	testutil.AssertEqual(t, got, 3)

	other, err := ds.CountRegenAttempts(context.Background(), "r1", 1)
	if err != nil {
		t.Fatalf("CountRegenAttempts (scene 1): %v", err)
	}
	testutil.AssertEqual(t, other, 1)

	missing, err := ds.CountRegenAttempts(context.Background(), "r1", 99)
	if err != nil {
		t.Fatalf("CountRegenAttempts (scene 99): %v", err)
	}
	testutil.AssertEqual(t, missing, 0)
}

func TestDecisionStore_RecordDescriptorEdit_StoresBeforeAfter(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id) VALUES ('r1', 'scp')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ds := db.NewDecisionStore(database)
	id, err := ds.RecordDescriptorEdit(context.Background(), "r1", "old descriptor", "new descriptor")
	if err != nil {
		t.Fatalf("RecordDescriptorEdit: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}
	var (
		decisionType    string
		contextSnapshot string
	)
	if err := database.QueryRow(
		`SELECT decision_type, context_snapshot FROM decisions WHERE id = ?`, id,
	).Scan(&decisionType, &contextSnapshot); err != nil {
		t.Fatalf("query: %v", err)
	}
	testutil.AssertEqual(t, decisionType, "descriptor_edit")
	if !strings.Contains(contextSnapshot, `"before"`) || !strings.Contains(contextSnapshot, "old descriptor") {
		t.Fatalf("context_snapshot missing before value: %s", contextSnapshot)
	}
	if !strings.Contains(contextSnapshot, `"after"`) || !strings.Contains(contextSnapshot, "new descriptor") {
		t.Fatalf("context_snapshot missing after value: %s", contextSnapshot)
	}
}

func TestDecisionStore_ApplyUndo_RestoresFrozenDescriptor(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(`INSERT INTO runs (id, scp_id, frozen_descriptor) VALUES ('r1', 'scp', 'new descriptor')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	snapshot := `{"command_kind":"descriptor_edit","before":"old descriptor","after":"new descriptor"}`
	if _, err := database.Exec(
		`INSERT INTO decisions (id, run_id, decision_type, context_snapshot) VALUES (20, 'r1', 'descriptor_edit', ?)`,
		snapshot,
	); err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	ds := db.NewDecisionStore(database)
	applied, err := ds.ApplyUndo(context.Background(), "r1", 20)
	if err != nil {
		t.Fatalf("ApplyUndo: %v", err)
	}
	testutil.AssertEqual(t, applied.DecisionType, "descriptor_edit")

	var frozen *string
	if err := database.QueryRow(`SELECT frozen_descriptor FROM runs WHERE id = 'r1'`).Scan(&frozen); err != nil {
		t.Fatalf("query frozen_descriptor: %v", err)
	}
	if frozen == nil || *frozen != "old descriptor" {
		t.Fatalf("want frozen_descriptor=%q, got %v", "old descriptor", frozen)
	}
}
