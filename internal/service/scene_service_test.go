package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// fakeSegmentStore provides a minimal in-memory segment store for tests.
type fakeSegmentStore struct {
	scenes    []*domain.Episode
	updateErr error
}

func (f *fakeSegmentStore) ListByRunID(_ context.Context, _ string) ([]*domain.Episode, error) {
	return f.scenes, nil
}

func (f *fakeSegmentStore) UpdateNarration(_ context.Context, _ string, sceneIndex int, narration string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	for _, ep := range f.scenes {
		if ep.SceneIndex == sceneIndex {
			ep.Narration = &narration
			return nil
		}
	}
	return domain.ErrNotFound
}

func scenarioReviewRun(id string) *domain.Run {
	return &domain.Run{
		ID:     id,
		Stage:  domain.StageScenarioReview,
		Status: domain.StatusWaiting,
	}
}

func narrationPtr(s string) *string { return &s }

// ── ListScenes ────────────────────────────────────────────────────────────────

func TestSceneService_ListScenes_ReturnsScenes(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	segments := &fakeSegmentStore{scenes: []*domain.Episode{
		{SceneIndex: 0, Narration: narrationPtr("장면 0")},
		{SceneIndex: 1, Narration: narrationPtr("장면 1")},
	}}
	svc := service.NewSceneService(runs, segments)

	scenes, err := svc.ListScenes(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertEqual(t, len(scenes), 2)
	testutil.AssertEqual(t, *scenes[0].Narration, "장면 0")
}

func TestSceneService_ListScenes_ReturnsConflictWhenNotAtScenarioReview(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": {ID: "run-1", Stage: domain.StageResearch, Status: domain.StatusRunning},
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{})

	_, err := svc.ListScenes(context.Background(), "run-1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestSceneService_ListScenes_ReturnsNotFoundForMissingRun(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{})

	_, err := svc.ListScenes(context.Background(), "no-such")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// ── EditNarration ─────────────────────────────────────────────────────────────

func TestSceneService_EditNarration_UpdatesText(t *testing.T) {
	original := "원래 나레이션"
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	segments := &fakeSegmentStore{scenes: []*domain.Episode{
		{SceneIndex: 0, Narration: &original},
	}}
	svc := service.NewSceneService(runs, segments)

	err := svc.EditNarration(context.Background(), "run-1", 0, "새 나레이션")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertEqual(t, *segments.scenes[0].Narration, "새 나레이션")
}

func TestSceneService_EditNarration_ReturnsValidationErrorForEmptyNarration(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{})

	err := svc.EditNarration(context.Background(), "run-1", 0, "")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}

func TestSceneService_EditNarration_ReturnsConflictWhenNotAtScenarioReview(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": {ID: "run-1", Stage: domain.StageWrite, Status: domain.StatusRunning},
	}}
	svc := service.NewSceneService(runs, &fakeSegmentStore{})

	err := svc.EditNarration(context.Background(), "run-1", 0, "text")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestSceneService_EditNarration_ReturnsNotFoundForMissingScene(t *testing.T) {
	runs := &fakeRunStore{runs: map[string]*domain.Run{
		"run-1": scenarioReviewRun("run-1"),
	}}
	segments := &fakeSegmentStore{scenes: []*domain.Episode{}} // no scene index 0
	svc := service.NewSceneService(runs, segments)

	err := svc.EditNarration(context.Background(), "run-1", 0, "text")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
