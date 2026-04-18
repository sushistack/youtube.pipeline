package agents

// PhaseAQualitySummary is the deterministic final quality summary stored in
// the canonical Phase A carrier once Phase A fully completes.
type PhaseAQualitySummary struct {
	PostWriterScore   int    `json:"post_writer_score"`
	PostReviewerScore int    `json:"post_reviewer_score"`
	CumulativeScore   int    `json:"cumulative_score"`
	FinalVerdict      string `json:"final_verdict"`
}

// ContractRef points to a checked-in schema file using a repo-relative path
// plus the SHA-256 digest of the raw file bytes.
type ContractRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// PhaseAContractManifest records the schema set that defines the final
// authoritative Phase A artifact.
type PhaseAContractManifest struct {
	ResearchSchema           ContractRef `json:"research_schema"`
	StructureSchema          ContractRef `json:"structure_schema"`
	WriterSchema             ContractRef `json:"writer_schema"`
	VisualBreakdownSchema    ContractRef `json:"visual_breakdown_schema"`
	ReviewSchema             ContractRef `json:"review_schema"`
	CriticPostWriterSchema   ContractRef `json:"critic_post_writer_schema"`
	CriticPostReviewerSchema ContractRef `json:"critic_post_reviewer_schema"`
	PhaseAStateSchema        ContractRef `json:"phase_a_state_schema"`
}
