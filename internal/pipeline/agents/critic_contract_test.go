package agents

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestCriticPostReviewerSchema_AcceptsMinorPolicyFindings(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	v := mustValidator(t, "critic_post_reviewer.schema.json")
	var value any
	if err := decodeJSONResponse(string(testutil.LoadFixture(t, filepath.Join("contracts", "critic_post_reviewer.sample.json"))), &value); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := v.Validate(value); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestCriticPostReviewerSchema_RejectsUnknownActID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	err := validateMinorPolicyFindings(
		[]domain.MinorPolicyFinding{{ActID: "nonexistent_act", RuneOffset: 0, Reason: "미성년자가 위험에 노출됩니다."}},
		&domain.NarrationScript{Acts: []domain.ActScript{{
			ActID:     domain.ActIncident,
			Monologue: "한 줄.",
			Beats: []domain.BeatAnchor{{
				StartOffset: 0, EndOffset: 3,
				Mood: "calm", Location: "x", CharactersPresent: []string{"y"},
				ColorPalette: "z", Atmosphere: "w",
			}},
		}}},
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// TestCriticPostReviewerSchema_RejectsOutOfRangeRuneOffset locks in the
// rune_offset bounds check: a finding pointing past the end of the act's
// monologue must surface ErrValidation rather than be silently truncated.
func TestCriticPostReviewerSchema_RejectsOutOfRangeRuneOffset(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	err := validateMinorPolicyFindings(
		[]domain.MinorPolicyFinding{{ActID: domain.ActIncident, RuneOffset: 999, Reason: "미성년자가 위험에 노출됩니다."}},
		&domain.NarrationScript{Acts: []domain.ActScript{{
			ActID:     domain.ActIncident,
			Monologue: "한 줄.",
			Beats: []domain.BeatAnchor{{
				StartOffset: 0, EndOffset: 3,
				Mood: "calm", Location: "x", CharactersPresent: []string{"y"},
				ColorPalette: "z", Atmosphere: "w",
			}},
		}}},
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}
