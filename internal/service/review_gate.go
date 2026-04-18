package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

type ReviewGateSegmentStore interface {
	ListByRunID(ctx context.Context, runID string) ([]*domain.Episode, error)
}

type ReviewGateDecisionStore interface {
	PrepareBatchReview(ctx context.Context, runID string, sceneResults []db.SceneReviewUpdate, autoApprovals []db.AutoApprovalInput) (bool, error)
	OverrideMinorSafeguard(ctx context.Context, runID string, sceneIndex int, note string) error
}

type ReviewGateService struct {
	segments  ReviewGateSegmentStore
	decisions ReviewGateDecisionStore
}

func NewReviewGateService(segments ReviewGateSegmentStore, decisions ReviewGateDecisionStore) *ReviewGateService {
	return &ReviewGateService{segments: segments, decisions: decisions}
}

func (s *ReviewGateService) PrepareBatchReview(
	ctx context.Context,
	runID string,
	criticFindings []domain.MinorPolicyFinding,
	regexHits []domain.MinorRegexHit,
	threshold float64,
) (bool, error) {
	if threshold <= 0 || threshold >= 1 {
		return false, fmt.Errorf("prepare batch review: invalid threshold %v: %w", threshold, domain.ErrValidation)
	}
	segments, err := s.segments.ListByRunID(ctx, runID)
	if err != nil {
		return false, fmt.Errorf("prepare batch review: list segments: %w", err)
	}
	sceneCount := len(segments)
	for _, finding := range criticFindings {
		if finding.SceneNum < 1 || finding.SceneNum > sceneCount {
			return false, fmt.Errorf("prepare batch review: minor policy finding scene_num=%d out of range (1..%d): %w", finding.SceneNum, sceneCount, domain.ErrValidation)
		}
	}

	minorSignals := pipeline.MergeMinorSignals(regexHits, criticFindings)
	sceneResults := make([]db.SceneReviewUpdate, 0, len(segments))
	autoApprovals := make([]db.AutoApprovalInput, 0)
	for _, segment := range segments {
		result, err := pipeline.DecideSceneGate(pipeline.SceneGateInput{
			SceneIndex:      segment.SceneIndex,
			CriticScore:     segment.CriticScore,
			RegexTriggered:  len(minorSignals[segment.SceneIndex]) > 0,
			CriticTriggered: hasCriticFindingForScene(criticFindings, segment.SceneIndex+1),
		}, threshold)
		if err != nil {
			return false, fmt.Errorf("prepare batch review: scene %d: %w", segment.SceneIndex, err)
		}
		sceneResults = append(sceneResults, db.SceneReviewUpdate{
			SceneIndex:     segment.SceneIndex,
			ReviewStatus:   result.ReviewStatus,
			SafeguardFlags: result.SafeguardFlags,
			AutoApproved:   result.AutoApproved,
		})
		if result.AutoApproved && segment.CriticScore != nil {
			autoApprovals = append(autoApprovals, db.AutoApprovalInput{
				SceneIndex:   segment.SceneIndex,
				CriticScore:  *segment.CriticScore,
				Threshold:    threshold,
				ReviewStatus: result.ReviewStatus,
			})
		}
	}

	autoContinue, err := s.decisions.PrepareBatchReview(ctx, runID, sceneResults, autoApprovals)
	if err != nil {
		return false, fmt.Errorf("prepare batch review: %w", err)
	}
	return autoContinue, nil
}

func (s *ReviewGateService) OverrideMinorSafeguard(
	ctx context.Context,
	runID string,
	sceneIndex int,
	note string,
) error {
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("override minor safeguard: empty note: %w", domain.ErrValidation)
	}
	if err := s.decisions.OverrideMinorSafeguard(ctx, runID, sceneIndex, note); err != nil {
		return fmt.Errorf("override minor safeguard: %w", err)
	}
	return nil
}

func hasCriticFindingForScene(findings []domain.MinorPolicyFinding, sceneNum int) bool {
	for _, finding := range findings {
		if finding.SceneNum == sceneNum {
			return true
		}
	}
	return false
}
