package agents

import (
	"fmt"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

func runPostWriterPrecheck(
	script *domain.NarrationScript,
	validator *Validator,
	terms *ForbiddenTerms,
) (domain.CriticPrecheck, error) {
	if script == nil {
		return domain.CriticPrecheck{}, fmt.Errorf("critic precheck: %w: narration is nil", domain.ErrValidation)
	}
	if validator == nil {
		return domain.CriticPrecheck{}, fmt.Errorf("critic precheck: %w: validator is nil", domain.ErrValidation)
	}
	if terms == nil {
		return domain.CriticPrecheck{}, fmt.Errorf("critic precheck: %w: forbidden terms are nil", domain.ErrValidation)
	}

	if err := validator.Validate(script); err != nil {
		return domain.CriticPrecheck{
			SchemaValid:       false,
			ForbiddenTermHits: []string{},
			ShortCircuited:    true,
		}, nil
	}

	hits := terms.MatchNarration(script)
	if len(hits) > 0 {
		return domain.CriticPrecheck{
			SchemaValid:       true,
			ForbiddenTermHits: flattenForbiddenTermHits(hits),
			ShortCircuited:    true,
		}, nil
	}

	return domain.CriticPrecheck{
		SchemaValid:       true,
		ForbiddenTermHits: []string{},
		ShortCircuited:    false,
	}, nil
}

func runPostReviewerPrecheck(
	state *PipelineState,
	cfg TextAgentConfig,
	writerValidator *Validator,
	visualValidator *Validator,
	reviewValidator *Validator,
	terms *ForbiddenTerms,
) (domain.CriticPrecheck, *domain.CriticCheckpointReport, error) {
	switch {
	case state == nil:
		return domain.CriticPrecheck{}, nil, fmt.Errorf("critic precheck: %w: state is nil", domain.ErrValidation)
	case state.Narration == nil:
		return domain.CriticPrecheck{}, nil, fmt.Errorf("critic precheck: %w: narration is nil", domain.ErrValidation)
	case state.VisualScript == nil:
		return domain.CriticPrecheck{}, nil, fmt.Errorf("critic precheck: %w: visual breakdown is nil", domain.ErrValidation)
	case state.Review == nil:
		return domain.CriticPrecheck{}, nil, fmt.Errorf("critic precheck: %w: review is nil", domain.ErrValidation)
	case writerValidator == nil:
		return domain.CriticPrecheck{}, nil, fmt.Errorf("critic precheck: %w: writer validator is nil", domain.ErrValidation)
	case visualValidator == nil:
		return domain.CriticPrecheck{}, nil, fmt.Errorf("critic precheck: %w: visual validator is nil", domain.ErrValidation)
	case reviewValidator == nil:
		return domain.CriticPrecheck{}, nil, fmt.Errorf("critic precheck: %w: review validator is nil", domain.ErrValidation)
	case terms == nil:
		return domain.CriticPrecheck{}, nil, fmt.Errorf("critic precheck: %w: forbidden terms are nil", domain.ErrValidation)
	}

	precheck := domain.CriticPrecheck{
		SchemaValid:       true,
		ForbiddenTermHits: []string{},
		ShortCircuited:    false,
	}

	if err := writerValidator.Validate(state.Narration); err != nil {
		precheck.SchemaValid = false
		precheck.ShortCircuited = true
		report := buildPostReviewerShortCircuitReport("schema_validation_failed", "내레이션 스키마 검증에 실패해 최종 비평을 진행하지 않았습니다. 작성 결과 형식을 먼저 수정해야 합니다.", cfg)
		return precheck, &report, nil
	}
	if err := visualValidator.Validate(*state.VisualScript); err != nil {
		precheck.SchemaValid = false
		precheck.ShortCircuited = true
		report := buildPostReviewerShortCircuitReport("schema_validation_failed", "비주얼 브레이크다운 스키마 검증에 실패해 최종 비평을 진행하지 않았습니다. 장면 분해 결과를 먼저 수정해야 합니다.", cfg)
		return precheck, &report, nil
	}
	if err := reviewValidator.Validate(*state.Review); err != nil {
		precheck.SchemaValid = false
		precheck.ShortCircuited = true
		report := buildPostReviewerShortCircuitReport("schema_validation_failed", "리뷰 결과 스키마 검증에 실패해 최종 비평을 진행하지 않았습니다. 리뷰 출력을 먼저 수정해야 합니다.", cfg)
		return precheck, &report, nil
	}

	hits := terms.MatchNarration(state.Narration)
	if len(hits) > 0 {
		precheck.ForbiddenTermHits = flattenForbiddenTermHits(hits)
		precheck.ShortCircuited = true
		report := buildPostReviewerShortCircuitReport("forbidden_terms_detected", "금지어 정책에 걸린 표현이 남아 있어 최종 비평을 진행하지 않았습니다. 해당 표현을 교체한 뒤 다시 작성해야 합니다.", cfg)
		return precheck, &report, nil
	}

	if !state.Review.OverallPass {
		precheck.ShortCircuited = true
		report := buildPostReviewerShortCircuitReport("review_failed", "사실성 검토가 통과되지 않아 최종 비평을 진행하지 않았습니다. 리뷰 수정 사항을 반영한 뒤 다시 작성해야 합니다.", cfg)
		return precheck, &report, nil
	}

	return precheck, nil, nil
}

func DeriveRetryReason(scores domain.CriticRubricScores) string {
	type candidate struct {
		score  int
		reason string
	}
	candidates := []candidate{
		{scores.Hook, "weak_hook"},
		{scores.FactAccuracy, "fact_accuracy"},
		{scores.EmotionalVariation, "emotional_variation"},
		{scores.Immersion, "immersion"},
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.score < best.score {
			best = candidate
		}
	}
	return best.reason
}

func buildPrecheckRetryReport(precheck domain.CriticPrecheck, cfg TextAgentConfig) domain.CriticCheckpointReport {
	reason := "schema_validation_failed"
	feedback := "작성 결과가 스키마를 통과하지 못했습니다. 누락된 필드와 형식을 먼저 수정한 뒤 다시 작성해야 합니다."
	if len(precheck.ForbiddenTermHits) > 0 {
		reason = "forbidden_terms_detected"
		feedback = "금지어 정책에 걸린 표현이 포함되어 있어 비평 단계를 진행하지 않았습니다. 해당 장면 표현을 교체한 뒤 다시 작성해야 합니다."
	}
	return domain.CriticCheckpointReport{
		Checkpoint:     domain.CriticCheckpointPostWriter,
		Verdict:        domain.CriticVerdictRetry,
		RetryReason:    reason,
		OverallScore:   0,
		Rubric:         domain.CriticRubricScores{},
		Feedback:       feedback,
		SceneNotes:     []domain.CriticSceneNote{},
		Precheck:       precheck,
		CriticModel:    cfg.Model,
		CriticProvider: cfg.Provider,
		SourceVersion:  domain.CriticSourceVersionV1,
	}
}

func buildPostReviewerShortCircuitReport(reason, feedback string, cfg TextAgentConfig) domain.CriticCheckpointReport {
	return domain.CriticCheckpointReport{
		Checkpoint:     domain.CriticCheckpointPostReviewer,
		Verdict:        domain.CriticVerdictRetry,
		RetryReason:    reason,
		OverallScore:   0,
		Rubric:         domain.CriticRubricScores{},
		Feedback:       feedback,
		SceneNotes:     []domain.CriticSceneNote{},
		Precheck:       domain.CriticPrecheck{},
		CriticModel:    cfg.Model,
		CriticProvider: cfg.Provider,
		SourceVersion:  domain.CriticSourceVersionPostReviewerV1,
	}
}

func flattenForbiddenTermHits(hits []ForbiddenTermHit) []string {
	items := make([]string, 0, len(hits))
	for _, hit := range hits {
		items = append(items, fmt.Sprintf("scene %d: %s", hit.SceneNum, hit.Pattern))
	}
	return items
}

func formatForbiddenTermHits(hits []ForbiddenTermHit) string {
	return "forbidden terms matched: " + strings.Join(flattenForbiddenTermHits(hits), ", ")
}
