package service

import (
	"context"
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// SceneReader is the read surface SceneService needs from the segment store.
type SceneReader interface {
	ListByRunID(ctx context.Context, runID string) ([]*domain.Episode, error)
}

// NarrationUpdater is the write surface SceneService needs for narration edits.
type NarrationUpdater interface {
	UpdateNarration(ctx context.Context, runID string, sceneIndex int, narration string) error
}

// SceneService implements scenario scene list and narration edit business logic.
type SceneService struct {
	runs     RunStore
	segments interface {
		SceneReader
		NarrationUpdater
	}
}

// NewSceneService constructs a SceneService.
func NewSceneService(runs RunStore, segments interface {
	SceneReader
	NarrationUpdater
}) *SceneService {
	return &SceneService{runs: runs, segments: segments}
}

// ListScenes returns all segments for a run in scene_index order.
// Returns CONFLICT if the run is not currently paused at scenario_review.
func (s *SceneService) ListScenes(ctx context.Context, runID string) ([]*domain.Episode, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("scene list: %w", err)
	}
	if run.Stage != domain.StageScenarioReview || run.Status != domain.StatusWaiting {
		return nil, fmt.Errorf("scene list: run is not paused at scenario_review: %w", domain.ErrConflict)
	}
	scenes, err := s.segments.ListByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("scene list: %w", err)
	}
	return scenes, nil
}

// EditNarration updates the narration text for a specific scene.
// Returns CONFLICT if the run is not currently paused at scenario_review.
// Returns NOT_FOUND if the scene index does not exist.
// Returns VALIDATION_ERROR if narration is empty.
func (s *SceneService) EditNarration(ctx context.Context, runID string, sceneIndex int, narration string) error {
	if narration == "" {
		return fmt.Errorf("edit narration: %w: narration is required", domain.ErrValidation)
	}
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return fmt.Errorf("edit narration: %w", err)
	}
	if run.Stage != domain.StageScenarioReview || run.Status != domain.StatusWaiting {
		return fmt.Errorf("edit narration: run is not paused at scenario_review: %w", domain.ErrConflict)
	}
	if err := s.segments.UpdateNarration(ctx, runID, sceneIndex, narration); err != nil {
		return fmt.Errorf("edit narration: %w", err)
	}
	return nil
}
