package pipeline

import (
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestComputePhaseAQuality_WeightedAverage(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	got, err := ComputePhaseAQuality(
		&domain.CriticCheckpointReport{OverallScore: 80},
		&domain.CriticCheckpointReport{OverallScore: 90, Verdict: domain.CriticVerdictPass},
	)
	if err != nil {
		t.Fatalf("ComputePhaseAQuality: %v", err)
	}
	testutil.AssertEqual(t, got.CumulativeScore, 86)
	testutil.AssertEqual(t, got.PostWriterScore, 80)
	testutil.AssertEqual(t, got.PostReviewerScore, 90)
	testutil.AssertEqual(t, got.FinalVerdict, domain.CriticVerdictPass)
}

func TestComputePhaseAQuality_RoundsHalfUp(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	got, err := ComputePhaseAQuality(
		&domain.CriticCheckpointReport{OverallScore: 81},
		&domain.CriticCheckpointReport{OverallScore: 84, Verdict: domain.CriticVerdictAcceptWithNotes},
	)
	if err != nil {
		t.Fatalf("ComputePhaseAQuality: %v", err)
	}
	testutil.AssertEqual(t, got.CumulativeScore, 83)
}

func TestComputePhaseAQuality_RejectsNilCheckpoint(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	_, err := ComputePhaseAQuality(nil, &domain.CriticCheckpointReport{})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestNormalizeCriticScore(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	testutil.AssertFloatNear(t, NormalizeCriticScore(87), 0.87, 0.000001)
}
