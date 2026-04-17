package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestInconsistencyReport_Error_Empty(t *testing.T) {
	r := InconsistencyReport{RunID: "scp-049-run-1", Stage: StageTTS}
	got := r.Error()
	if !strings.Contains(got, "consistent") {
		t.Errorf("empty report Error() = %q, want to contain 'consistent'", got)
	}
	if !strings.Contains(got, "scp-049-run-1") {
		t.Errorf("empty report Error() = %q, want RunID", got)
	}
	if !strings.Contains(got, "tts") {
		t.Errorf("empty report Error() = %q, want Stage", got)
	}
}

func TestInconsistencyReport_Error_WithMismatches(t *testing.T) {
	r := InconsistencyReport{
		RunID: "scp-049-run-1",
		Stage: StageTTS,
		Mismatches: []Mismatch{
			{Kind: "missing_file", Path: "tts/scene_01.wav"},
			{Kind: "missing_file", Path: "tts/scene_02.wav"},
		},
	}
	got := r.Error()
	for _, want := range []string{"inconsistency", "scp-049-run-1", "tts",
		"missing_file@tts/scene_01.wav", "missing_file@tts/scene_02.wav"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, want to contain %q", got, want)
		}
	}
}

func TestInconsistencyReport_WrapsValidation(t *testing.T) {
	// Resume returns the report wrapped via fmt.Errorf("%w: %s", ErrValidation, report.Error()).
	// Callers (API handler) rely on errors.Is identifying it as ErrValidation.
	r := InconsistencyReport{
		RunID: "scp-049-run-1", Stage: StageTTS,
		Mismatches: []Mismatch{{Kind: "missing_file", Path: "tts/scene_01.wav"}},
	}
	wrapped := fmt.Errorf("%w: %s", ErrValidation, r.Error())
	if !errors.Is(wrapped, ErrValidation) {
		t.Error("wrapped error should be errors.Is(ErrValidation)")
	}
}
