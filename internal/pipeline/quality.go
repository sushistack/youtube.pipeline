package pipeline

import (
	"fmt"
	"math"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
)

func ComputePhaseAQuality(
	postWriter *domain.CriticCheckpointReport,
	postReviewer *domain.CriticCheckpointReport,
) (agents.PhaseAQualitySummary, error) {
	if postWriter == nil {
		return agents.PhaseAQualitySummary{}, fmt.Errorf("compute phase a quality: %w: post_writer critic is nil", domain.ErrValidation)
	}
	if postReviewer == nil {
		return agents.PhaseAQualitySummary{}, fmt.Errorf("compute phase a quality: %w: post_reviewer critic is nil", domain.ErrValidation)
	}

	verdict := postReviewer.Verdict
	switch verdict {
	case domain.CriticVerdictPass, domain.CriticVerdictRetry, domain.CriticVerdictAcceptWithNotes:
	default:
		return agents.PhaseAQualitySummary{}, fmt.Errorf("compute phase a quality: %w: unknown final_verdict %q", domain.ErrValidation, verdict)
	}

	weighted := 0.40*float64(postWriter.OverallScore) + 0.60*float64(postReviewer.OverallScore)
	return agents.PhaseAQualitySummary{
		PostWriterScore:   postWriter.OverallScore,
		PostReviewerScore: postReviewer.OverallScore,
		CumulativeScore:   int(math.Floor(weighted + 0.5)),
		FinalVerdict:      verdict,
	}, nil
}

func NormalizeCriticScore(overallScore int) float64 {
	v := float64(overallScore) / 100.0
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
