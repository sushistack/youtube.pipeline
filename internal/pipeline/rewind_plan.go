package pipeline

import (
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// StageNodeKey identifies one of the four work-phase nodes in the run-detail
// stepper (Story / Cast / Media / Cut). Mirrors the frontend formatters.ts
// STAGE_NODE_KEYS that operators click. Rewind takes this key (not raw backend
// Stage values) so the API stays stable if backend stages get renamed or
// regrouped.
type StageNodeKey string

const (
	StageNodeScenario  StageNodeKey = "scenario"  // Story
	StageNodeCharacter StageNodeKey = "character" // Cast
	StageNodeAssets    StageNodeKey = "assets"    // Media
	StageNodeAssemble  StageNodeKey = "assemble"  // Cut
)

// IsRewindable reports whether k is one of the four operator-clickable
// rewind targets. The pending and complete lifecycle keys are intentionally
// excluded — they are not work phases.
func (k StageNodeKey) IsRewindable() bool {
	switch k {
	case StageNodeScenario, StageNodeCharacter, StageNodeAssets, StageNodeAssemble:
		return true
	}
	return false
}

// RewindTarget returns the backend Stage that the rewind re-enters at the
// start of for the given stepper node. Each stepper node groups several
// backend stages; rewind always lands at the *first* stage of the group so
// the operator re-experiences the work-phase from its beginning.
//
// Mapping (artifact §"Stepper → backend stage mapping"):
//
//	scenario  → research          (Phase A entry)
//	character → character_pick    (HITL Cast pick)
//	assets    → image             (Phase B entry — image+tts run in parallel)
//	assemble  → assemble          (Phase C entry)
func RewindTarget(node StageNodeKey) (domain.Stage, error) {
	switch node {
	case StageNodeScenario:
		return domain.StageResearch, nil
	case StageNodeCharacter:
		return domain.StageCharacterPick, nil
	case StageNodeAssets:
		return domain.StageImage, nil
	case StageNodeAssemble:
		return domain.StageAssemble, nil
	}
	return "", fmt.Errorf("invalid stage node %q: %w", node, domain.ErrValidation)
}

// stageOrder is a 0-based pipeline-position index into domain.AllStages().
// Used for O(1) "is X strictly before Y?" comparisons. Pinned by tests to
// stay in sync with the AllStages() array; if AllStages() is reordered the
// regression is caught by TestRewind_StageOrderMatchesAllStages.
var stageOrder = func() map[domain.Stage]int {
	all := domain.AllStages()
	m := make(map[domain.Stage]int, len(all))
	for i, s := range all {
		m[s] = i
	}
	return m
}()

// stageStrictlyBefore reports whether a is strictly earlier in pipeline order
// than b. Returns false when either stage is unknown (defensive).
func stageStrictlyBefore(a, b domain.Stage) bool {
	ai, aok := stageOrder[a]
	bi, bok := stageOrder[b]
	if !aok || !bok {
		return false
	}
	return ai < bi
}

// stageAtOrAfter reports whether a is at the same position or later than b.
// Inverse of stageStrictlyBefore (excluding unknowns).
func stageAtOrAfter(a, b domain.Stage) bool {
	ai, aok := stageOrder[a]
	bi, bok := stageOrder[b]
	if !aok || !bok {
		return false
	}
	return ai >= bi
}

// CanRewind validates that a run currently at currentStage may be rewound to
// the given node. Rule: target's entry Stage must be strictly earlier than
// currentStage in pipeline order. Equal or later targets are rejected with
// ErrConflict — the frontend hides the affordance, but the server is the
// authority.
//
// Returns the resolved target Stage on success.
func CanRewind(currentStage domain.Stage, node StageNodeKey) (domain.Stage, error) {
	target, err := RewindTarget(node)
	if err != nil {
		return "", err
	}
	if !stageStrictlyBefore(target, currentStage) {
		return "", fmt.Errorf(
			"rewind target %s (node %s) is not strictly before current stage %s: %w",
			target, node, currentStage, domain.ErrConflict,
		)
	}
	return target, nil
}

// decisionStageBucket maps a decision_type string to the work-phase Stage
// that "produced" it. Used to decide which decision rows to delete on
// rewind. Unknown types map to the empty Stage — defensive: rewind keeps
// rows of unknown provenance rather than silently deleting audit history.
//
// Mapping rationale:
//   - approve / reject / skip_and_remember / system_auto_approved /
//     override / undo: scene-level review acts that exist only after
//     batch_review is reachable. Bucket = batch_review.
//   - descriptor_edit: an operator edit applied during the character_pick
//     HITL gate. Bucket = character_pick.
//
// Scenario-review approval, character pick, and metadata ack are not
// recorded as decision rows (they live as runs-row stage transitions),
// so they need no bucket.
func decisionStageBucket(decisionType string) domain.Stage {
	switch decisionType {
	case domain.DecisionTypeApprove,
		domain.DecisionTypeReject,
		domain.DecisionTypeSkipAndRemember,
		domain.DecisionTypeSystemAutoApproved,
		domain.DecisionTypeOverride,
		domain.DecisionTypeUndo:
		return domain.StageBatchReview
	case domain.DecisionTypeDescriptorEdit:
		return domain.StageCharacterPick
	}
	return ""
}

// shouldDeleteDecisionOnRewind reports whether a decision belonging to
// `bucket` should be removed when rewinding to `target`. Empty bucket
// (unknown type) is preserved.
//
// The rule is "bucket is at or after target": rewinding to target erases
// every artifact produced from target's stage onward, so any decision tied
// to that stage or a later one must go.
func shouldDeleteDecisionOnRewind(bucket, target domain.Stage) bool {
	if bucket == "" {
		return false
	}
	return stageAtOrAfter(bucket, target)
}

// allDecisionTypesToDeleteOnRewind enumerates every decision_type whose
// bucket is at or after target. Returns nil when nothing qualifies (the
// caller should skip the DELETE entirely in that case).
//
// Hardcoded list rather than reflection: the set of decision types is a
// stable domain-level enum, and an explicit list survives renames better
// than runtime introspection.
func allDecisionTypesToDeleteOnRewind(target domain.Stage) []string {
	all := []string{
		domain.DecisionTypeApprove,
		domain.DecisionTypeReject,
		domain.DecisionTypeSkipAndRemember,
		domain.DecisionTypeSystemAutoApproved,
		domain.DecisionTypeOverride,
		domain.DecisionTypeUndo,
		domain.DecisionTypeDescriptorEdit,
	}
	out := make([]string, 0, len(all))
	for _, t := range all {
		if shouldDeleteDecisionOnRewind(decisionStageBucket(t), target) {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// RewindPlan describes every effect a Rewind must apply to bring a run
// from its current state back to a chosen stepper node's entry. Computed
// purely from the StageNodeKey by PlanRewind; consumed by the orchestration
// in Engine.Rewind. Tests assert the plan content for each node; the
// orchestration tests assert the plan is faithfully executed.
type RewindPlan struct {
	// Node is the operator-facing stepper key the rewind targets.
	Node StageNodeKey
	// Target is the backend Stage at which the run will re-enter the
	// pipeline; equals RewindTarget(Node).
	Target domain.Stage
	// FinalStage is what runs.stage will be set to after cleanup. May
	// differ from Target — rewind to scenario lands at pending so the
	// operator clicks "Start run" rather than auto-restarting Phase A.
	FinalStage domain.Stage
	// FinalStatus is what runs.status will be set to.
	FinalStatus domain.Status

	// DB segment-row effects.
	DeleteSegments      bool
	ClearImageArtifacts bool
	ClearTTSArtifacts   bool
	ClearClipPaths      bool

	// runs-row column resets. ClearScenarioPath also nulls scenario_path.
	// ClearCharacterPick clears selected_character_id, frozen_descriptor,
	// and character_query_key together (one logical "operator pick").
	ClearScenarioPath  bool
	ClearCharacterPick bool
	ClearOutputPath    bool
	ClearCriticScore   bool

	// DecisionTypesToDelete enumerates which decision_type rows to remove.
	// Nil/empty means no DELETE on decisions table.
	DecisionTypesToDelete []string

	// Filesystem effects (relative to the per-run output directory).
	FSRemoveScenario  bool
	FSRemoveImages    bool
	FSRemoveTTS       bool
	FSRemoveClips     bool
	FSRemoveOutputMP4 bool
	FSRemoveMetadata  bool
	FSRemoveManifest  bool
}

// PlanRewind builds the full set of effects required to rewind to the
// given stepper node. The plan is pure: same input → same output.
// Tests anchor the per-node expectations in TestPlanRewind_*.
func PlanRewind(node StageNodeKey) (RewindPlan, error) {
	target, err := RewindTarget(node)
	if err != nil {
		return RewindPlan{}, err
	}
	p := RewindPlan{
		Node:                  node,
		Target:                target,
		DecisionTypesToDelete: allDecisionTypesToDeleteOnRewind(target),
	}
	switch node {
	case StageNodeScenario:
		// Restart from the very beginning. The operator's next action is
		// "Start run" (POST /api/runs/{id}/advance from pending/pending),
		// which the existing UI surface already exposes.
		p.FinalStage = domain.StagePending
		p.FinalStatus = domain.StatusPending
		p.DeleteSegments = true
		p.ClearScenarioPath = true
		p.ClearCharacterPick = true
		p.ClearOutputPath = true
		p.ClearCriticScore = true
		p.FSRemoveScenario = true
		p.FSRemoveImages = true
		p.FSRemoveTTS = true
		p.FSRemoveClips = true
		p.FSRemoveOutputMP4 = true
		p.FSRemoveMetadata = true
		p.FSRemoveManifest = true
	case StageNodeCharacter:
		// Re-enter at Cast HITL. Phase A artifacts (scenario.json + segments
		// narration text) are preserved; Phase B/C outputs are wiped.
		p.FinalStage = domain.StageCharacterPick
		p.FinalStatus = domain.StatusWaiting
		p.ClearImageArtifacts = true
		p.ClearTTSArtifacts = true
		p.ClearClipPaths = true
		p.ClearCharacterPick = true
		p.ClearOutputPath = true
		p.FSRemoveImages = true
		p.FSRemoveTTS = true
		p.FSRemoveClips = true
		p.FSRemoveOutputMP4 = true
		p.FSRemoveMetadata = true
		p.FSRemoveManifest = true
	case StageNodeAssets:
		// Re-enter at the Media manual gate (image/waiting → "Generate Assets").
		// Character pick is preserved (operator already chose).
		p.FinalStage = domain.StageImage
		p.FinalStatus = domain.StatusWaiting
		p.ClearImageArtifacts = true
		p.ClearTTSArtifacts = true
		p.ClearClipPaths = true
		p.ClearOutputPath = true
		p.FSRemoveImages = true
		p.FSRemoveTTS = true
		p.FSRemoveClips = true
		p.FSRemoveOutputMP4 = true
		p.FSRemoveMetadata = true
		p.FSRemoveManifest = true
	case StageNodeAssemble:
		// Re-enter at the Cut manual gate (assemble/waiting → "Generate Video").
		// Per-scene image/tts artifacts are inputs to Phase C — preserved.
		p.FinalStage = domain.StageAssemble
		p.FinalStatus = domain.StatusWaiting
		p.ClearClipPaths = true
		p.ClearOutputPath = true
		p.FSRemoveClips = true
		p.FSRemoveOutputMP4 = true
		p.FSRemoveMetadata = true
		p.FSRemoveManifest = true
	}
	return p, nil
}
