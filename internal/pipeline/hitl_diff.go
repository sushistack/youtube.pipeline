package pipeline

import (
	"sort"
	"strconv"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// ChangeKind enumerates the kinds of changes the FR50 diff can report.
type ChangeKind string

const (
	// ChangeKindSceneStatusFlipped reports a scene whose review status
	// differs between T1 (old snapshot) and T2 (current state), e.g.
	// pending → approved.
	ChangeKindSceneStatusFlipped ChangeKind = "scene_status_flipped"
	// ChangeKindSceneAdded reports a scene that exists in the new snapshot
	// but not in the old (typical cause: regeneration completed after T1).
	ChangeKindSceneAdded ChangeKind = "scene_added"
	// ChangeKindSceneRemoved reports a scene that existed at T1 but no
	// longer exists (unusual; defensive — e.g., a destructive cleanup).
	ChangeKindSceneRemoved ChangeKind = "scene_removed"
)

// Change is one line in the FR50 diff output.
type Change struct {
	Kind      ChangeKind `json:"kind"`
	SceneID   string     `json:"scene_id"`
	Before    string     `json:"before,omitempty"`
	After     string     `json:"after,omitempty"`
	Timestamp string     `json:"timestamp,omitempty"`
}

// SnapshotDiff returns the ordered list of Changes between oldSnap (state
// at last interaction T1) and newSnap (current state). Returns nil if the
// snapshots are equal. Ordering: by scene_id ascending with numeric-aware
// comparison (so "2" comes before "10" when both parse as ints); lexicographic
// fallback if either side is non-numeric.
//
// Pure function: no side effects. The caller is responsible for populating
// Change.Timestamp via AttachTimestamps using the live decisions list.
func SnapshotDiff(oldSnap, newSnap domain.DecisionSnapshot) []Change {
	// Union of scene_ids across both snapshots.
	ids := make(map[string]struct{})
	for id := range oldSnap.SceneStatuses {
		ids[id] = struct{}{}
	}
	for id := range newSnap.SceneStatuses {
		ids[id] = struct{}{}
	}

	var changes []Change
	for id := range ids {
		oldStatus, oldHas := oldSnap.SceneStatuses[id]
		newStatus, newHas := newSnap.SceneStatuses[id]
		switch {
		case oldHas && newHas:
			if oldStatus != newStatus {
				changes = append(changes, Change{
					Kind:    ChangeKindSceneStatusFlipped,
					SceneID: id,
					Before:  oldStatus,
					After:   newStatus,
				})
			}
		case !oldHas && newHas:
			changes = append(changes, Change{
				Kind:    ChangeKindSceneAdded,
				SceneID: id,
				After:   newStatus,
			})
		case oldHas && !newHas:
			changes = append(changes, Change{
				Kind:    ChangeKindSceneRemoved,
				SceneID: id,
				Before:  oldStatus,
			})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	sortChangesByNumericSceneID(changes)
	return changes
}

// AttachTimestamps annotates a []Change with timestamps sourced from the
// given decisions list. Matching rule: the most recent non-superseded
// decision for the change's SceneID whose decision_type corresponds to
// change.After ("approve" → "approved", "reject" → "rejected"). If no
// match, Change.Timestamp is left empty.
//
// Input mutation: returns a fresh slice; does not modify the argument.
func AttachTimestamps(changes []Change, decisions []*domain.Decision) []Change {
	if len(changes) == 0 {
		return nil
	}
	out := make([]Change, len(changes))
	copy(out, changes)
	for i := range out {
		wantType := decisionTypeForStatus(out[i].After)
		if wantType == "" {
			continue
		}
		// Scan decisions in reverse to pick the most recent match (input
		// is ordered by created_at ASC per DecisionStore.ListByRunID).
		for j := len(decisions) - 1; j >= 0; j-- {
			d := decisions[j]
			if d == nil || d.SceneID == nil || d.SupersededBy != nil {
				continue
			}
			if *d.SceneID != out[i].SceneID {
				continue
			}
			if d.DecisionType != wantType {
				continue
			}
			out[i].Timestamp = d.CreatedAt
			break
		}
	}
	return out
}

func decisionTypeForStatus(status string) string {
	switch status {
	case "approved":
		return "approve"
	case "rejected":
		return "reject"
	}
	return ""
}

// sortChangesByNumericSceneID sorts changes by SceneID with numeric-aware
// comparison: parse both sides as int; if both parse, compare as ints;
// otherwise fall back to lexicographic. V1 scene_ids are always numeric
// (0..totalScenes-1 stringified), so the lexicographic fallback is defensive.
func sortChangesByNumericSceneID(changes []Change) {
	sort.SliceStable(changes, func(i, j int) bool {
		ai, aerr := strconv.Atoi(changes[i].SceneID)
		bi, berr := strconv.Atoi(changes[j].SceneID)
		if aerr == nil && berr == nil {
			return ai < bi
		}
		return changes[i].SceneID < changes[j].SceneID
	})
}
