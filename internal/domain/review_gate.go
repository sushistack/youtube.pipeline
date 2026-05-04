package domain

// ReviewStatus is the dedicated scene-level review gate state.
type ReviewStatus string

const (
	ReviewStatusPending          ReviewStatus = "pending"
	ReviewStatusWaitingForReview ReviewStatus = "waiting_for_review"
	ReviewStatusAutoApproved     ReviewStatus = "auto_approved"
	ReviewStatusApproved         ReviewStatus = "approved"
	ReviewStatusRejected         ReviewStatus = "rejected"
)

var allReviewStatuses = [...]ReviewStatus{
	ReviewStatusPending,
	ReviewStatusWaitingForReview,
	ReviewStatusAutoApproved,
	ReviewStatusApproved,
	ReviewStatusRejected,
}

const (
	DecisionTypeApprove            = "approve"
	DecisionTypeReject             = "reject"
	DecisionTypeSkipAndRemember    = "skip_and_remember"
	DecisionTypeSystemAutoApproved = "system_auto_approved"
	DecisionTypeOverride           = "override"
	DecisionTypeUndo               = "undo"
	DecisionTypeDescriptorEdit     = "descriptor_edit"
	SafeguardFlagMinors            = "Safeguard Triggered: Minors"
)

// CommandKind identifies the operator action captured in context_snapshot for
// V1 undo. Only these five kinds are eligible for the undo stack.
const (
	CommandKindApprove             = "approve"
	CommandKindReject              = "reject"
	CommandKindSkip                = "skip"
	CommandKindApproveAllRemaining = "approve_all_remaining"
	CommandKindDescriptorEdit      = "descriptor_edit"
)

// UndoableCommandKinds is the set of decision_types that may be reversed by
// the undo service. System-generated and lifecycle decisions are excluded.
var UndoableCommandKinds = map[string]bool{
	DecisionTypeApprove:         true,
	DecisionTypeReject:          true,
	DecisionTypeSkipAndRemember: true,
	DecisionTypeDescriptorEdit:  true,
}

// IsPrePhaseC returns true when the run stage/status combination allows undo.
// Undo is blocked once Phase C (assemble) has started.
func IsPrePhaseC(stage Stage, status Status) bool {
	switch stage {
	case StageScenarioReview, StageCharacterPick, StageBatchReview:
		return status == StatusWaiting
	}
	return false
}

// MinorPolicyFinding is one minor-protection concern surfaced by the critic
// LLM. v2 keys findings on (ActID, RuneOffset) — both relative to
// NarrationScript.Acts[].Monologue — so a single act-level monologue carrying
// multiple findings can be addressed without re-fragmenting it into scenes.
//
// ActID matches NarrationScript.Acts[i].ActID (e.g. domain.ActIncident).
// RuneOffset is a half-open inclusive-on-the-left coordinate within that
// act's Monologue, in rune units; review_gate consumers translate it to a
// flat segments.scene_index via NarrationScript.BeatIndexAt(ActID, RuneOffset).
type MinorPolicyFinding struct {
	ActID      string `json:"act_id"`
	RuneOffset int    `json:"rune_offset"`
	Reason     string `json:"reason"`
}

// MinorRegexHit is the in-process counterpart of MinorPolicyFinding emitted
// by MinorSensitivePatterns.MatchNarration when a per-act monologue regex
// fires. ActID + RuneOffset have the same semantics as MinorPolicyFinding.
type MinorRegexHit struct {
	ActID      string
	RuneOffset int
	Pattern    string
}

func (s ReviewStatus) IsValid() bool {
	for _, v := range allReviewStatuses {
		if s == v {
			return true
		}
	}
	return false
}
