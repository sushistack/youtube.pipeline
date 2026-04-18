package pipeline_test

import (
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestSnapshotDiff_NoChange(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	snap := domain.DecisionSnapshot{
		TotalScenes:   3,
		SceneStatuses: map[string]string{"0": "approved", "1": "approved", "2": "pending"},
	}
	got := pipeline.SnapshotDiff(snap, snap)
	if got != nil {
		t.Fatalf("expected nil for no-change, got %+v", got)
	}
}

func TestSnapshotDiff_OneSceneApproved(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	old := domain.DecisionSnapshot{
		SceneStatuses: map[string]string{"0": "approved", "1": "approved", "2": "approved", "3": "pending", "4": "pending"},
	}
	new := domain.DecisionSnapshot{
		SceneStatuses: map[string]string{"0": "approved", "1": "approved", "2": "approved", "3": "approved", "4": "pending"},
	}
	got := pipeline.SnapshotDiff(old, new)
	testutil.AssertEqual(t, len(got), 1)
	testutil.AssertEqual(t, got[0].Kind, pipeline.ChangeKindSceneStatusFlipped)
	testutil.AssertEqual(t, got[0].SceneID, "3")
	testutil.AssertEqual(t, got[0].Before, "pending")
	testutil.AssertEqual(t, got[0].After, "approved")
}

func TestSnapshotDiff_MultipleFlipsStableOrder(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	old := domain.DecisionSnapshot{
		SceneStatuses: map[string]string{"2": "pending", "5": "pending", "10": "pending"},
	}
	new := domain.DecisionSnapshot{
		SceneStatuses: map[string]string{"2": "approved", "5": "rejected", "10": "approved"},
	}
	got := pipeline.SnapshotDiff(old, new)
	testutil.AssertEqual(t, len(got), 3)
	// Numeric-aware order: 2, 5, 10 (NOT lex order "10", "2", "5").
	testutil.AssertEqual(t, got[0].SceneID, "2")
	testutil.AssertEqual(t, got[1].SceneID, "5")
	testutil.AssertEqual(t, got[2].SceneID, "10")
}

func TestSnapshotDiff_SceneAdded(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	old := domain.DecisionSnapshot{
		SceneStatuses: map[string]string{"0": "approved"},
	}
	new := domain.DecisionSnapshot{
		SceneStatuses: map[string]string{"0": "approved", "11": "pending"},
	}
	got := pipeline.SnapshotDiff(old, new)
	testutil.AssertEqual(t, len(got), 1)
	testutil.AssertEqual(t, got[0].Kind, pipeline.ChangeKindSceneAdded)
	testutil.AssertEqual(t, got[0].SceneID, "11")
	testutil.AssertEqual(t, got[0].Before, "")
	testutil.AssertEqual(t, got[0].After, "pending")
}

func TestSnapshotDiff_SceneRemoved(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	old := domain.DecisionSnapshot{
		SceneStatuses: map[string]string{"0": "approved", "7": "pending"},
	}
	new := domain.DecisionSnapshot{
		SceneStatuses: map[string]string{"0": "approved"},
	}
	got := pipeline.SnapshotDiff(old, new)
	testutil.AssertEqual(t, len(got), 1)
	testutil.AssertEqual(t, got[0].Kind, pipeline.ChangeKindSceneRemoved)
	testutil.AssertEqual(t, got[0].SceneID, "7")
	testutil.AssertEqual(t, got[0].Before, "pending")
	testutil.AssertEqual(t, got[0].After, "")
}

func TestAttachTimestamps_MatchesLastApprove(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	supersededID := int64(99)
	decisions := []*domain.Decision{
		{RunID: "r1", SceneID: strp("3"), DecisionType: "reject", SupersededBy: &supersededID, CreatedAt: "2026-01-01T00:00:00Z"},
		{RunID: "r1", SceneID: strp("3"), DecisionType: "approve", CreatedAt: "2026-01-02T00:00:00Z"},
		{RunID: "r1", SceneID: strp("3"), DecisionType: "approve", CreatedAt: "2026-01-03T00:00:00Z"},
	}
	changes := []pipeline.Change{
		{Kind: pipeline.ChangeKindSceneStatusFlipped, SceneID: "3", Before: "pending", After: "approved"},
	}
	got := pipeline.AttachTimestamps(changes, decisions)
	testutil.AssertEqual(t, len(got), 1)
	testutil.AssertEqual(t, got[0].Timestamp, "2026-01-03T00:00:00Z")
}

func TestAttachTimestamps_NoMatchingDecision(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	decisions := []*domain.Decision{
		{RunID: "r1", SceneID: strp("5"), DecisionType: "approve", CreatedAt: "2026-01-01T00:00:00Z"},
	}
	changes := []pipeline.Change{
		{Kind: pipeline.ChangeKindSceneStatusFlipped, SceneID: "3", Before: "pending", After: "approved"},
	}
	got := pipeline.AttachTimestamps(changes, decisions)
	testutil.AssertEqual(t, got[0].Timestamp, "")
}
