package domain

const (
	ReviewSourceVersionV1 = "v1-reviewer-fact-check"

	ReviewIssueFactError           = "fact_error"
	ReviewIssueMissingFact         = "missing_fact"
	ReviewIssueDescriptorViolation = "descriptor_violation"
	ReviewIssueInventedContent     = "invented_content"
	ReviewIssueConsistencyIssue    = "consistency_issue"
)

type ReviewReport struct {
	OverallPass      bool               `json:"overall_pass"`
	CoveragePct      float64            `json:"coverage_pct"`
	Issues           []ReviewIssue      `json:"issues"`
	Corrections      []ReviewCorrection `json:"corrections"`
	ReviewerModel    string             `json:"reviewer_model"`
	ReviewerProvider string             `json:"reviewer_provider"`
	SourceVersion    string             `json:"source_version"`
}

type ReviewIssue struct {
	SceneNum    int    `json:"scene_num"`
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	Correction  string `json:"correction"`
}

type ReviewCorrection struct {
	SceneNum  int    `json:"scene_num"`
	Field     string `json:"field"`
	Original  string `json:"original"`
	Corrected string `json:"corrected"`
}
