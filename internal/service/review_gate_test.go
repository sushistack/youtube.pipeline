package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type fakeReviewGateSegments struct {
	items []*domain.Episode
	err   error
}

func (f *fakeReviewGateSegments) ListByRunID(_ context.Context, _ string) ([]*domain.Episode, error) {
	return f.items, f.err
}

type fakeReviewGateDecisions struct {
	autoContinue bool
	prepareErr   error
	overrideErr  error
	lastResults  []db.SceneReviewUpdate
}

func (f *fakeReviewGateDecisions) PrepareBatchReview(_ context.Context, _ string, sceneResults []db.SceneReviewUpdate, _ []db.AutoApprovalInput) (bool, error) {
	f.lastResults = sceneResults
	return f.autoContinue, f.prepareErr
}

func (f *fakeReviewGateDecisions) OverrideMinorSafeguard(_ context.Context, _ string, _ int, _ string) error {
	return f.overrideErr
}

func TestOverrideMinorSafeguard_RejectsEmptyNote(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	svc := service.NewReviewGateService(&fakeReviewGateSegments{}, &fakeReviewGateDecisions{})
	err := svc.OverrideMinorSafeguard(context.Background(), "r1", 0, "   ")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestPrepareBatchReview_AutoApprovalThresholdFromService(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	score := 0.91
	segments := &fakeReviewGateSegments{items: []*domain.Episode{
		{RunID: "r1", SceneIndex: 0, CriticScore: &score},
	}}
	decisions := &fakeReviewGateDecisions{autoContinue: true}
	svc := service.NewReviewGateService(segments, decisions)
	autoContinue, err := svc.PrepareBatchReview(context.Background(), "r1", nil, nil, 0.85)
	if err != nil {
		t.Fatalf("PrepareBatchReview: %v", err)
	}
	testutil.AssertEqual(t, autoContinue, true)
	testutil.AssertEqual(t, decisions.lastResults[0].ReviewStatus, domain.ReviewStatusAutoApproved)
}

func TestBatchReviewPreparation_NoManualScenesCanAutoContinue(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	score := 0.91
	segments := &fakeReviewGateSegments{items: []*domain.Episode{
		{RunID: "r1", SceneIndex: 0, CriticScore: &score},
	}}
	decisions := &fakeReviewGateDecisions{autoContinue: true}
	svc := service.NewReviewGateService(segments, decisions)
	autoContinue, err := svc.PrepareBatchReview(context.Background(), "r1", nil, nil, 0.85)
	if err != nil {
		t.Fatalf("PrepareBatchReview: %v", err)
	}
	testutil.AssertEqual(t, autoContinue, true)
}

func TestOverrideMinorSafeguard_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	svc := service.NewReviewGateService(&fakeReviewGateSegments{}, &fakeReviewGateDecisions{})
	if err := svc.OverrideMinorSafeguard(context.Background(), "r1", 1, "운영자 검토 후 허용"); err != nil {
		t.Fatalf("OverrideMinorSafeguard: %v", err)
	}
}

func TestOverrideMinorSafeguard_RejectsNonSafeguardedScene(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	svc := service.NewReviewGateService(&fakeReviewGateSegments{}, &fakeReviewGateDecisions{overrideErr: domain.ErrConflict})
	err := svc.OverrideMinorSafeguard(context.Background(), "r1", 1, "메모")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestPrepareBatchReview_RejectsOutOfRangeMinorFinding(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	segments := &fakeReviewGateSegments{items: []*domain.Episode{{RunID: "r1", SceneIndex: 0}}}
	svc := service.NewReviewGateService(segments, &fakeReviewGateDecisions{})
	_, err := svc.PrepareBatchReview(context.Background(), "r1",
		[]domain.MinorPolicyFinding{{SceneNum: 2, Reason: "범위 초과"}},
		[]domain.MinorRegexHit{},
		0.85,
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}
