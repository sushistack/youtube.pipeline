package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestHumanRenderer_RunOutput_NotPausedOmitsSummary(t *testing.T) {
	var buf bytes.Buffer
	r := NewHumanRenderer(&buf)
	r.RenderSuccess(&RunOutput{
		ID:     "scp-049-run-1",
		SCPID:  "049",
		Stage:  "write",
		Status: "running",
	})
	got := buf.String()
	if strings.Contains(got, "Summary:") {
		t.Fatalf("non-HITL output should not contain Summary:, got: %s", got)
	}
	if strings.Contains(got, "Changes since") {
		t.Fatalf("non-HITL output should not contain Changes since, got: %s", got)
	}
}

func TestHumanRenderer_RunOutput_PausedShowsSummaryAndChanges(t *testing.T) {
	var buf bytes.Buffer
	r := NewHumanRenderer(&buf)
	r.RenderSuccess(&RunOutput{
		ID:     "scp-049-run-2",
		SCPID:  "049",
		Stage:  "batch_review",
		Status: "waiting",
		PausedPosition: &PausedPositionOutput{
			Stage:                    "batch_review",
			SceneIndex:               2,
			LastInteractionTimestamp: "2026-01-02T00:25:00Z",
		},
		DecisionsSummary: &DecisionSummaryOutput{ApprovedCount: 3, RejectedCount: 0, PendingCount: 0},
		Summary:          "Run scp-049-run-2: reviewing scene 3 of 3, 3 approved, 0 rejected",
		ChangesSince: []ChangeOutput{
			{Kind: "scene_status_flipped", SceneID: "2", Before: "pending", After: "approved", Timestamp: "2026-01-02T00:45:00Z"},
		},
	})
	got := buf.String()
	for _, want := range []string{
		"Summary: Run scp-049-run-2: reviewing scene 3 of 3, 3 approved, 0 rejected",
		"Changes since last interaction (2026-01-02T00:25:00Z)",
		"scene 2: pending \u2192 approved (at 2026-01-02T00:45:00Z)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("want %q in output, got: %s", want, got)
		}
	}
}

func TestJSONRenderer_RunOutput_PausedFieldsPresent(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)
	r.RenderSuccess(&RunOutput{
		ID:     "scp-049-run-2",
		Stage:  "batch_review",
		Status: "waiting",
		PausedPosition: &PausedPositionOutput{
			Stage:                    "batch_review",
			SceneIndex:               2,
			LastInteractionTimestamp: "2026-01-02T00:25:00Z",
		},
		DecisionsSummary: &DecisionSummaryOutput{ApprovedCount: 3, RejectedCount: 0, PendingCount: 0},
		Summary:          "Run scp-049-run-2: reviewing scene 3 of 3, 3 approved, 0 rejected",
		ChangesSince: []ChangeOutput{
			{Kind: "scene_status_flipped", SceneID: "2", Before: "pending", After: "approved", Timestamp: "2026-01-02T00:45:00Z"},
		},
	})
	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, _ := json.Marshal(env.Data)
	text := string(data)
	for _, want := range []string{
		`"paused_position"`,
		`"decisions_summary"`,
		`"summary":"Run scp-049-run-2: reviewing scene 3 of 3, 3 approved, 0 rejected"`,
		`"changes_since_last_interaction"`,
		`"kind":"scene_status_flipped"`,
		`"before":"pending"`,
		`"after":"approved"`,
		`"timestamp":"2026-01-02T00:45:00Z"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("want %s in JSON, got: %s", want, text)
		}
	}
}

func TestJSONRenderer_RunOutput_NotPausedOmitsHITLFields(t *testing.T) {
	var buf bytes.Buffer
	r := NewJSONRenderer(&buf)
	r.RenderSuccess(&RunOutput{
		ID:     "scp-049-run-1",
		Stage:  "write",
		Status: "running",
	})
	text := buf.String()
	for _, banned := range []string{
		`"paused_position"`,
		`"decisions_summary"`,
		`"summary"`,
		`"changes_since_last_interaction"`,
	} {
		if strings.Contains(text, banned) {
			t.Fatalf("non-HITL output should not contain %s, got: %s", banned, text)
		}
	}
}
