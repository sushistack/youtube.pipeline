package domain_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestHITLSession_JSONRoundTrip(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	session := domain.HITLSession{
		RunID:                    "scp-049-run-1",
		Stage:                    domain.StageBatchReview,
		SceneIndex:               4,
		LastInteractionTimestamp: "2026-01-01T00:25:00Z",
		SnapshotJSON:             `{"total_scenes":10}`,
		CreatedAt:                "2026-01-01T00:00:00Z",
		UpdatedAt:                "2026-01-01T00:30:00Z",
	}
	raw, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got domain.HITLSession
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// SnapshotJSON is elided (json:"-"), so round-trip drops it.
	testutil.AssertEqual(t, got.RunID, session.RunID)
	testutil.AssertEqual(t, got.Stage, session.Stage)
	testutil.AssertEqual(t, got.SceneIndex, session.SceneIndex)
	testutil.AssertEqual(t, got.LastInteractionTimestamp, session.LastInteractionTimestamp)
	testutil.AssertEqual(t, got.CreatedAt, session.CreatedAt)
	testutil.AssertEqual(t, got.UpdatedAt, session.UpdatedAt)
	testutil.AssertEqual(t, got.SnapshotJSON, "")
}

func TestHITLSession_SnapshotJSONNotInJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	session := domain.HITLSession{
		RunID:        "scp-049-run-1",
		SnapshotJSON: `{"secret":"do-not-leak"}`,
	}
	raw, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "snapshot_json") || strings.Contains(text, "SnapshotJSON") {
		t.Fatalf("snapshot_json leaked into JSON output: %s", text)
	}
	if strings.Contains(text, "do-not-leak") {
		t.Fatalf("snapshot contents leaked into JSON output: %s", text)
	}
}

func TestDecisionSnapshot_EmptyIsValid(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var empty domain.DecisionSnapshot
	raw, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got domain.DecisionSnapshot
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	testutil.AssertEqual(t, got.TotalScenes, 0)
	testutil.AssertEqual(t, got.ApprovedCount, 0)
	testutil.AssertEqual(t, got.RejectedCount, 0)
	testutil.AssertEqual(t, got.PendingCount, 0)
	// SceneStatuses may be nil after unmarshal — that's acceptable;
	// BuildSessionSnapshot guarantees a non-nil empty map at the producer
	// boundary, not at this struct's zero-value.
}

func TestDecisionSummary_JSONFields(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	s := domain.DecisionSummary{ApprovedCount: 4, RejectedCount: 1, PendingCount: 5}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(raw)
	for _, key := range []string{`"approved_count":4`, `"rejected_count":1`, `"pending_count":5`} {
		if !strings.Contains(text, key) {
			t.Fatalf("want %s in JSON, got %s", key, text)
		}
	}
}
