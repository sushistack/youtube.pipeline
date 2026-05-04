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

// PrepareBatchReview translates v2 critic + regex findings (act_id +
// rune_offset) to flat scene_index gate decisions using the run's
// NarrationScript. The script is required because v2 findings are anchored
// to act monologues, not to scene_num — translation lives at this boundary
// rather than inside review_gate's per-segment loop so a missing script
// surfaces as ErrValidation up front.
//
// Out-of-range findings (act_id absent from script, or rune_offset outside
// any beat's range) are dropped silently by MergeMinorSignals — they
// represent stale findings or LLM offsets that escaped the validator and
// would otherwise blow up here. The critic-side validator
// (validateMinorPolicyFindings) is the authoritative gate; this method's
// out-of-range silence preserves resume forward-progress against historical
// reports.
func (s *ReviewGateService) PrepareBatchReview(
	ctx context.Context,
	runID string,
	script *domain.NarrationScript,
	criticFindings []domain.MinorPolicyFinding,
	regexHits []domain.MinorRegexHit,
	threshold float64,
) (bool, error) {
	if threshold <= 0 || threshold >= 1 {
		return false, fmt.Errorf("prepare batch review: invalid threshold %v: %w", threshold, domain.ErrValidation)
	}
	if script == nil {
		return false, fmt.Errorf("prepare batch review: %w: narration script is nil", domain.ErrValidation)
	}
	segments, err := s.segments.ListByRunID(ctx, runID)
	if err != nil {
		return false, fmt.Errorf("prepare batch review: list segments: %w", err)
	}
	criticByScene := make(map[int]bool, len(criticFindings))
	for _, finding := range criticFindings {
		idx := script.BeatIndexAt(finding.ActID, finding.RuneOffset) - 1
		if idx >= 0 {
			criticByScene[idx] = true
		}
	}

	minorSignals := pipeline.MergeMinorSignals(script, regexHits, criticFindings)
	sceneResults := make([]db.SceneReviewUpdate, 0, len(segments))
	autoApprovals := make([]db.AutoApprovalInput, 0)
	for _, segment := range segments {
		result, err := pipeline.DecideSceneGate(pipeline.SceneGateInput{
			SceneIndex:      segment.SceneIndex,
			CriticScore:     segment.CriticScore,
			RegexTriggered:  len(minorSignals[segment.SceneIndex]) > 0,
			CriticTriggered: criticByScene[segment.SceneIndex],
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
