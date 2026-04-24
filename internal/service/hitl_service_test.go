package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// --- Fakes ---

type fakeRunStore struct {
	runs map[string]*domain.Run
}

func (f *fakeRunStore) Create(_ context.Context, _, _ string) (*domain.Run, error) {
	return nil, errors.New("unused")
}
func (f *fakeRunStore) CreateWithPromptVersion(_ context.Context, _, _ string, _ *db.PromptVersionTag) (*domain.Run, error) {
	return nil, errors.New("unused")
}
func (f *fakeRunStore) Get(_ context.Context, id string) (*domain.Run, error) {
	r, ok := f.runs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return r, nil
}
func (f *fakeRunStore) List(_ context.Context) ([]*domain.Run, error) {
	return nil, errors.New("unused")
}
func (f *fakeRunStore) Cancel(_ context.Context, _ string) error { return errors.New("unused") }
func (f *fakeRunStore) MarkComplete(_ context.Context, _ string) error { return errors.New("unused") }

type fakeDecisionReader struct {
	decisions []*domain.Decision
	counts    db.DecisionCounts
	session   *domain.HITLSession
	listErr   error
	countsErr error
	sessErr   error
}

func (f *fakeDecisionReader) ListByRunID(_ context.Context, _ string) ([]*domain.Decision, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.decisions, nil
}
func (f *fakeDecisionReader) DecisionCountsByRunID(_ context.Context, _ string) (db.DecisionCounts, error) {
	if f.countsErr != nil {
		return db.DecisionCounts{}, f.countsErr
	}
	return f.counts, nil
}
func (f *fakeDecisionReader) GetSession(_ context.Context, _ string) (*domain.HITLSession, error) {
	if f.sessErr != nil {
		return nil, f.sessErr
	}
	return f.session, nil
}

func strp(s string) *string { return &s }

// --- Tests ---

func TestHITLService_BuildStatus_NotPaused(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"r1": {ID: "r1", SCPID: "049", Stage: domain.StageWrite, Status: domain.StatusRunning},
	}}
	decisions := &fakeDecisionReader{counts: db.DecisionCounts{}}
	logger, _ := testutil.CaptureLog(t)
	svc := service.NewHITLService(runs, decisions, logger)

	got, err := svc.BuildStatus(context.Background(), "r1")
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if got.PausedPosition != nil {
		t.Fatalf("expected no PausedPosition, got %+v", got.PausedPosition)
	}
	if got.ChangesSince != nil {
		t.Fatalf("expected no ChangesSince, got %+v", got.ChangesSince)
	}
	if got.Summary != "" {
		t.Fatalf("expected empty Summary for non-HITL, got %q", got.Summary)
	}
	// DecisionsSummary is always populated (D1: spec main AC wins).
	if got.DecisionsSummary == nil {
		t.Fatalf("expected DecisionsSummary non-nil, got nil")
	}
	testutil.AssertEqual(t, got.DecisionsSummary.ApprovedCount, 0)
	testutil.AssertEqual(t, got.DecisionsSummary.RejectedCount, 0)
	testutil.AssertEqual(t, got.DecisionsSummary.PendingCount, 0)
}

func TestHITLService_BuildStatus_PausedWithNoChanges(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"r1": {ID: "r1", Stage: domain.StageBatchReview, Status: domain.StatusWaiting},
	}}
	// Live state matches the stored snapshot.
	decisions := &fakeDecisionReader{
		decisions: []*domain.Decision{
			{RunID: "r1", SceneID: strp("0"), DecisionType: "approve"},
			{RunID: "r1", SceneID: strp("1"), DecisionType: "approve"},
		},
		counts: db.DecisionCounts{Approved: 2, Rejected: 0, TotalScenes: 3},
		session: &domain.HITLSession{
			RunID: "r1", Stage: domain.StageBatchReview, SceneIndex: 2,
			LastInteractionTimestamp: "2026-01-01T00:25:00Z",
			SnapshotJSON:             `{"total_scenes":3,"approved_count":2,"rejected_count":0,"pending_count":1,"scene_statuses":{"0":"approved","1":"approved","2":"waiting_for_review"}}`,
		},
	}
	logger, _ := testutil.CaptureLog(t)
	svc := service.NewHITLService(runs, decisions, logger)

	got, err := svc.BuildStatus(context.Background(), "r1")
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if got.PausedPosition == nil {
		t.Fatalf("expected PausedPosition, got nil")
	}
	testutil.AssertEqual(t, got.PausedPosition.SceneIndex, 2)
	if got.ChangesSince != nil {
		t.Fatalf("expected nil ChangesSince (no changes), got %+v", got.ChangesSince)
	}
	if got.Summary == "" {
		t.Fatalf("expected non-empty Summary")
	}
}

func TestHITLService_BuildStatus_PausedWithChanges(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"r1": {ID: "r1", Stage: domain.StageBatchReview, Status: domain.StatusWaiting},
	}}
	// Old snapshot had scene "2" as "pending"; current live state approved
	// it at T2 (after T1). Expect a scene_status_flipped change.
	decisions := &fakeDecisionReader{
		decisions: []*domain.Decision{
			{RunID: "r1", SceneID: strp("0"), DecisionType: "approve", CreatedAt: "2026-01-02T00:15:00Z"},
			{RunID: "r1", SceneID: strp("1"), DecisionType: "approve", CreatedAt: "2026-01-02T00:25:00Z"},
			{RunID: "r1", SceneID: strp("2"), DecisionType: "approve", CreatedAt: "2026-01-02T00:45:00Z"},
		},
		counts: db.DecisionCounts{Approved: 3, Rejected: 0, TotalScenes: 3},
		session: &domain.HITLSession{
			RunID: "r1", Stage: domain.StageBatchReview, SceneIndex: 2,
			LastInteractionTimestamp: "2026-01-02T00:25:00Z",
			SnapshotJSON:             `{"total_scenes":3,"approved_count":2,"rejected_count":0,"pending_count":1,"scene_statuses":{"0":"approved","1":"approved","2":"waiting_for_review"}}`,
		},
	}
	logger, _ := testutil.CaptureLog(t)
	svc := service.NewHITLService(runs, decisions, logger)

	got, err := svc.BuildStatus(context.Background(), "r1")
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if len(got.ChangesSince) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(got.ChangesSince), got.ChangesSince)
	}
	ch := got.ChangesSince[0]
	testutil.AssertEqual(t, ch.Kind, pipeline.ChangeKindSceneStatusFlipped)
	testutil.AssertEqual(t, ch.SceneID, "2")
	testutil.AssertEqual(t, ch.Before, "waiting_for_review")
	testutil.AssertEqual(t, ch.After, "approved")
	testutil.AssertEqual(t, ch.Timestamp, "2026-01-02T00:45:00Z")
}

func TestHITLService_BuildStatus_HITLStateButNoSession(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"r1": {ID: "r1", Stage: domain.StageBatchReview, Status: domain.StatusWaiting},
	}}
	decisions := &fakeDecisionReader{
		counts: db.DecisionCounts{Approved: 0, Rejected: 0, TotalScenes: 5},
		// session intentionally nil
	}
	logger, buf := testutil.CaptureLog(t)
	svc := service.NewHITLService(runs, decisions, logger)

	got, err := svc.BuildStatus(context.Background(), "r1")
	if err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if got.PausedPosition != nil {
		t.Fatalf("expected PausedPosition nil when session row missing, got %+v", got.PausedPosition)
	}
	if got.Summary == "" {
		t.Fatalf("expected Summary computed from live state")
	}
	if got.ChangesSince != nil {
		t.Fatalf("expected ChangesSince nil when no session snapshot, got %+v", got.ChangesSince)
	}
	if !strings.Contains(buf.String(), "hitl session row missing") {
		t.Fatalf("expected Warn log about missing session, got: %s", buf.String())
	}
}

func TestHITLService_BuildStatus_RunNotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runs := &fakeRunStore{runs: map[string]*domain.Run{}}
	decisions := &fakeDecisionReader{}
	logger, _ := testutil.CaptureLog(t)
	svc := service.NewHITLService(runs, decisions, logger)

	_, err := svc.BuildStatus(context.Background(), "nope")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected wrapped ErrNotFound, got %v", err)
	}
}

func TestHITLService_BuildStatus_CorruptSnapshotFallsBack(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"r1": {ID: "r1", Stage: domain.StageBatchReview, Status: domain.StatusWaiting},
	}}
	decisions := &fakeDecisionReader{
		decisions: []*domain.Decision{},
		counts:    db.DecisionCounts{TotalScenes: 3},
		session: &domain.HITLSession{
			RunID: "r1", Stage: domain.StageBatchReview, SceneIndex: 0,
			LastInteractionTimestamp: "2026-01-01T00:00:00Z",
			SnapshotJSON:             `NOT VALID JSON {`,
		},
	}
	logger, buf := testutil.CaptureLog(t)
	svc := service.NewHITLService(runs, decisions, logger)

	got, err := svc.BuildStatus(context.Background(), "r1")
	if err != nil {
		t.Fatalf("BuildStatus should not error on corrupt snapshot: %v", err)
	}
	if got.ChangesSince != nil {
		t.Fatalf("expected nil ChangesSince on corrupt snapshot, got %+v", got.ChangesSince)
	}
	if !strings.Contains(buf.String(), "hitl snapshot unmarshal failed") {
		t.Fatalf("expected Warn log about corrupt snapshot, got: %s", buf.String())
	}
}
