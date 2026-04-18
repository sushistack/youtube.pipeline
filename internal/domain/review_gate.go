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
	DecisionTypeSystemAutoApproved = "system_auto_approved"
	DecisionTypeOverride           = "override"
	SafeguardFlagMinors            = "Safeguard Triggered: Minors"
)

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
