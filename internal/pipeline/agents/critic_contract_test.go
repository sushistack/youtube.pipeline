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

func TestCriticPostReviewerSchema_RejectsOutOfRangeSceneNum(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	err := validateMinorPolicyFindings(
		[]domain.MinorPolicyFinding{{SceneNum: 99, Reason: "미성년자가 위험에 노출됩니다."}},
		&domain.NarrationScript{Scenes: []domain.NarrationScene{{SceneNum: 1}}},
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}
