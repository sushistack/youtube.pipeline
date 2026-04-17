package domain_test

import (
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestNewStageObservation_ZeroValued(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	obs := domain.NewStageObservation(domain.StageWrite)
	testutil.AssertEqual(t, obs.Stage, domain.StageWrite)
	if !obs.IsZero() {
		t.Fatalf("expected IsZero=true, got %+v", obs)
	}
}

func TestStageObservation_Validate_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	reason := "rate_limit"
	score := 0.82
	obs := domain.StageObservation{
		Stage:         domain.StageCritic,
		DurationMs:    1500,
		TokenIn:       1000,
		TokenOut:      250,
		RetryCount:    1,
		RetryReason:   &reason,
		CriticScore:   &score,
		CostUSD:       0.02,
		HumanOverride: false,
	}
	if err := obs.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestStageObservation_Validate_RejectsUnknownStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	obs := domain.StageObservation{Stage: domain.Stage("mystery")}
	err := obs.Validate()
	if err == nil {
		t.Fatal("expected ErrValidation, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected errors.Is ErrValidation, got %v", err)
	}
}

func TestStageObservation_Validate_RejectsNegatives(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	base := domain.NewStageObservation(domain.StageWrite)
	cases := []struct {
		name string
		mut  func(*domain.StageObservation)
	}{
		{"duration", func(o *domain.StageObservation) { o.DurationMs = -1 }},
		{"token_in", func(o *domain.StageObservation) { o.TokenIn = -1 }},
		{"token_out", func(o *domain.StageObservation) { o.TokenOut = -5 }},
		{"retry_count", func(o *domain.StageObservation) { o.RetryCount = -2 }},
		{"cost_usd", func(o *domain.StageObservation) { o.CostUSD = -0.01 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := base
			tc.mut(&obs)
			err := obs.Validate()
			if err == nil {
				t.Fatal("expected ErrValidation, got nil")
			}
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("expected errors.Is ErrValidation, got %v", err)
			}
		})
	}
}

func TestStageObservation_IsZero_NonZeroFields(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	reason := "timeout"
	score := 0.5
	cases := []struct {
		name string
		mut  func(*domain.StageObservation)
	}{
		{"duration", func(o *domain.StageObservation) { o.DurationMs = 1 }},
		{"token_in", func(o *domain.StageObservation) { o.TokenIn = 1 }},
		{"token_out", func(o *domain.StageObservation) { o.TokenOut = 1 }},
		{"retry_count", func(o *domain.StageObservation) { o.RetryCount = 1 }},
		{"retry_reason", func(o *domain.StageObservation) { o.RetryReason = &reason }},
		{"critic_score", func(o *domain.StageObservation) { o.CriticScore = &score }},
		{"cost_usd", func(o *domain.StageObservation) { o.CostUSD = 0.01 }},
		{"human_override", func(o *domain.StageObservation) { o.HumanOverride = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := domain.NewStageObservation(domain.StageWrite)
			tc.mut(&obs)
			if obs.IsZero() {
				t.Fatalf("expected IsZero=false for %s, got true", tc.name)
			}
		})
	}
}
