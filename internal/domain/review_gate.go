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

type MinorPolicyFinding struct {
	SceneNum int    `json:"scene_num"`
	Reason   string `json:"reason"`
}

type MinorRegexHit struct {
	SceneNum int
	Pattern  string
}

func (s ReviewStatus) IsValid() bool {
	for _, v := range allReviewStatuses {
		if s == v {
			return true
		}
	}
	return false
}
