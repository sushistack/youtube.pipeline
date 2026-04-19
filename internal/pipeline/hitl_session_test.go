package pipeline_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func strp(s string) *string { return &s }

// --- BuildSessionSnapshot ---

func TestBuildSessionSnapshot_EmptyInputs(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	snap := pipeline.BuildSessionSnapshot(nil, 0)
	testutil.AssertEqual(t, snap.TotalScenes, 0)
	testutil.AssertEqual(t, snap.ApprovedCount, 0)
	testutil.AssertEqual(t, snap.RejectedCount, 0)
	testutil.AssertEqual(t, snap.PendingCount, 0)
	if snap.SceneStatuses == nil {
		t.Fatalf("SceneStatuses must be non-nil empty map, got nil")
	}
	testutil.AssertEqual(t, len(snap.SceneStatuses), 0)
}

func TestBuildSessionSnapshot_ClassifiesPerSceneStatus(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	decisions := []*domain.Decision{
		{RunID: "r1", SceneID: strp("0"), DecisionType: "approve"},
		{RunID: "r1", SceneID: strp("1"), DecisionType: "approve"},
		{RunID: "r1", SceneID: strp("2"), DecisionType: "approve"},
		{RunID: "r1", SceneID: strp("3"), DecisionType: "approve"},
		{RunID: "r1", SceneID: strp("4"), DecisionType: "reject"},
	}
	snap := pipeline.BuildSessionSnapshot(decisions, 10)
	testutil.AssertEqual(t, snap.TotalScenes, 10)
	testutil.AssertEqual(t, snap.ApprovedCount, 4)
	testutil.AssertEqual(t, snap.RejectedCount, 1)
	testutil.AssertEqual(t, snap.PendingCount, 5)
	testutil.AssertEqual(t, snap.SceneStatuses["0"], "approved")
	testutil.AssertEqual(t, snap.SceneStatuses["4"], "rejected")
	testutil.AssertEqual(t, snap.SceneStatuses["9"], "waiting_for_review")
}

func TestBuildSessionSnapshot_SupersededDecisionsIgnored(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	supersededID := int64(99)
	decisions := []*domain.Decision{
		{RunID: "r1", SceneID: strp("0"), DecisionType: "reject", SupersededBy: &supersededID},
		{RunID: "r1", SceneID: strp("0"), DecisionType: "approve"}, // not superseded
	}
	snap := pipeline.BuildSessionSnapshot(decisions, 3)
	testutil.AssertEqual(t, snap.ApprovedCount, 1)
	testutil.AssertEqual(t, snap.RejectedCount, 0)
	testutil.AssertEqual(t, snap.SceneStatuses["0"], "approved")
}

func TestBuildSessionSnapshot_NullSceneIDIgnored(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	decisions := []*domain.Decision{
		{RunID: "r1", SceneID: nil, DecisionType: "approve"}, // run-level; skip
		{RunID: "r1", SceneID: strp("0"), DecisionType: "approve"},
	}
	snap := pipeline.BuildSessionSnapshot(decisions, 2)
	testutil.AssertEqual(t, snap.ApprovedCount, 1)
	testutil.AssertEqual(t, snap.PendingCount, 1)
	testutil.AssertEqual(t, snap.SceneStatuses["0"], "approved")
	testutil.AssertEqual(t, snap.SceneStatuses["1"], "waiting_for_review")
}

// --- NextSceneIndex ---

func TestNextSceneIndex_AllPending(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got := pipeline.NextSceneIndex(map[string]string{
		"0": "waiting_for_review", "1": "waiting_for_review", "2": "waiting_for_review",
	}, 3)
	testutil.AssertEqual(t, got, 0)
}

func TestNextSceneIndex_FirstPendingIsMiddle(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got := pipeline.NextSceneIndex(map[string]string{
		"0": "approved", "1": "approved", "2": "waiting_for_review", "3": "waiting_for_review",
	}, 4)
	testutil.AssertEqual(t, got, 2)
}

func TestNextSceneIndex_AllDecided(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got := pipeline.NextSceneIndex(map[string]string{
		"0": "approved", "1": "rejected", "2": "approved",
	}, 3)
	testutil.AssertEqual(t, got, 3)
}

func TestNextSceneIndex_HoleAtStart(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got := pipeline.NextSceneIndex(map[string]string{
		"0": "waiting_for_review", "1": "approved", "2": "approved",
	}, 3)
	testutil.AssertEqual(t, got, 0)
}

func TestBuildSessionSnapshot_TracksAutoApprovedAndWaitingForReview(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	decisions := []*domain.Decision{
		{RunID: "r1", SceneID: strp("0"), DecisionType: domain.DecisionTypeSystemAutoApproved},
		{RunID: "r1", SceneID: strp("1"), DecisionType: "reject"},
	}
	snap := pipeline.BuildSessionSnapshot(decisions, 3)
	testutil.AssertEqual(t, snap.ApprovedCount, 1)
	testutil.AssertEqual(t, snap.RejectedCount, 1)
	testutil.AssertEqual(t, snap.PendingCount, 1)
	testutil.AssertEqual(t, snap.SceneStatuses["0"], "approved")
	testutil.AssertEqual(t, snap.SceneStatuses["2"], "waiting_for_review")
}

func TestNextSceneIndex_SkipsAutoApprovedScenes(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got := pipeline.NextSceneIndex(map[string]string{
		"0": "approved",
		"1": "waiting_for_review",
		"2": "approved",
	}, 3)
	testutil.AssertEqual(t, got, 1)
}

func TestBuildSessionSnapshot_SkipAdvancesNextScene(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	decisions := []*domain.Decision{
		{RunID: "r1", SceneID: strp("0"), DecisionType: domain.DecisionTypeSkipAndRemember},
	}
	snap := pipeline.BuildSessionSnapshot(decisions, 2)
	testutil.AssertEqual(t, snap.SceneStatuses["0"], "skipped")
	testutil.AssertEqual(t, snap.SceneStatuses["1"], "waiting_for_review")
	// NextSceneIndex must skip past the skipped scene so the UI can
	// move forward even though segments.review_status is unchanged.
	got := pipeline.NextSceneIndex(snap.SceneStatuses, 2)
	testutil.AssertEqual(t, got, 1)
}

// --- SummaryString ---

func TestSummaryString_BatchReview(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// AC pins this exact string byte-for-byte.
	got := pipeline.SummaryString(
		"scp-049-run-1", domain.StageBatchReview, domain.StatusWaiting,
		4, 10, domain.DecisionSummary{ApprovedCount: 4, RejectedCount: 0, PendingCount: 6},
	)
	want := "Run scp-049-run-1: reviewing scene 5 of 10, 4 approved, 0 rejected"
	testutil.AssertEqual(t, got, want)
}

func TestSummaryString_ScenarioReview(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got := pipeline.SummaryString(
		"scp-049-run-1", domain.StageScenarioReview, domain.StatusWaiting,
		0, 1, domain.DecisionSummary{},
	)
	testutil.AssertEqual(t, got, "Run scp-049-run-1: reviewing scene 1 of 1, 0 approved, 0 rejected")
}

func TestSummaryString_FallbackForNonHITL(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got := pipeline.SummaryString(
		"scp-049-run-1", domain.StageWrite, domain.StatusFailed,
		0, 0, domain.DecisionSummary{},
	)
	testutil.AssertEqual(t, got, "Run scp-049-run-1: failed")
}

// --- UpsertSessionFromState ---

type fakeSessionStore struct {
	decisions  []*domain.Decision
	counts     pipeline.DecisionCounts
	upserted   *domain.HITLSession
	deletedRun string
	upsertErr  error
	listErr    error
	countsErr  error
	deleteErr  error
}

func (f *fakeSessionStore) ListByRunID(_ context.Context, _ string) ([]*domain.Decision, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.decisions, nil
}
func (f *fakeSessionStore) DecisionCountsByRunID(_ context.Context, _ string) (pipeline.DecisionCounts, error) {
	if f.countsErr != nil {
		return pipeline.DecisionCounts{}, f.countsErr
	}
	return f.counts, nil
}
func (f *fakeSessionStore) GetSession(_ context.Context, _ string) (*domain.HITLSession, error) {
	return nil, nil
}
func (f *fakeSessionStore) UpsertSession(_ context.Context, s *domain.HITLSession) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upserted = s
	return nil
}
func (f *fakeSessionStore) DeleteSession(_ context.Context, runID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedRun = runID
	return nil
}

func TestUpsertSessionFromState_LeavesHITL(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := &fakeSessionStore{}
	clk := clock.NewFakeClock(time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC))
	got, err := pipeline.UpsertSessionFromState(
		context.Background(), store, clk,
		"scp-049-run-1", domain.StageWrite, domain.StatusRunning,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil session (leaving HITL), got %+v", got)
	}
	testutil.AssertEqual(t, store.deletedRun, "scp-049-run-1")
	if store.upserted != nil {
		t.Fatalf("expected no upsert when leaving HITL, got %+v", store.upserted)
	}
}

func TestUpsertSessionFromState_BuildsAndUpserts(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := &fakeSessionStore{
		decisions: []*domain.Decision{
			{RunID: "r1", SceneID: strp("0"), DecisionType: "approve"},
			{RunID: "r1", SceneID: strp("1"), DecisionType: "approve"},
			{RunID: "r1", SceneID: strp("2"), DecisionType: "approve"},
		},
		counts: pipeline.DecisionCounts{Approved: 3, Rejected: 0, TotalScenes: 10},
	}
	clk := clock.NewFakeClock(time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC))
	got, err := pipeline.UpsertSessionFromState(
		context.Background(), store, clk,
		"r1", domain.StageBatchReview, domain.StatusWaiting,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected session, got nil")
	}
	testutil.AssertEqual(t, got.RunID, "r1")
	testutil.AssertEqual(t, got.Stage, domain.StageBatchReview)
	testutil.AssertEqual(t, got.SceneIndex, 3) // scenes 0,1,2 approved → next is 3
	testutil.AssertEqual(t, got.LastInteractionTimestamp, "2026-04-18T10:00:00Z")
	// Snapshot JSON should include "approved_count":3 and "total_scenes":10.
	if got.SnapshotJSON == "" || got.SnapshotJSON == "{}" {
		t.Fatalf("expected populated snapshot JSON, got %q", got.SnapshotJSON)
	}
	if store.upserted == nil {
		t.Fatalf("expected Upsert call, got nil")
	}
}

func TestUpsertSessionFromState_StoreErrorPropagates(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	boom := errors.New("boom")
	store := &fakeSessionStore{
		counts:    pipeline.DecisionCounts{TotalScenes: 5},
		upsertErr: boom,
	}
	clk := clock.NewFakeClock(time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC))
	_, err := pipeline.UpsertSessionFromState(
		context.Background(), store, clk,
		"r1", domain.StageBatchReview, domain.StatusWaiting,
	)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
}
