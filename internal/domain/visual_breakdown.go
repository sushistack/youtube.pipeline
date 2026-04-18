package domain

const (
	VisualBreakdownSourceVersionV1 = "v1-visual-breakdown"
	ShotFormulaVersionV1           = "tts-duration-v1"

	TransitionKenBurns      = "ken_burns"
	TransitionCrossDissolve = "cross_dissolve"
	TransitionHardCut       = "hard_cut"
)

type VisualBreakdownOutput struct {
	SCPID            string                  `json:"scp_id"`
	Title            string                  `json:"title"`
	FrozenDescriptor string                  `json:"frozen_descriptor"`
	Scenes           []VisualBreakdownScene  `json:"scenes"`
	ShotOverrides    map[int]ShotOverride    `json:"shot_overrides"`
	Metadata         VisualBreakdownMetadata `json:"metadata"`
	SourceVersion    string                  `json:"source_version"`
}

type VisualBreakdownScene struct {
	SceneNum              int          `json:"scene_num"`
	ActID                 string       `json:"act_id"`
	Narration             string       `json:"narration"`
	EstimatedTTSDurationS float64      `json:"estimated_tts_duration_s"`
	ShotCount             int          `json:"shot_count"`
	Shots                 []VisualShot `json:"shots"`
}

type VisualShot struct {
	ShotIndex          int     `json:"shot_index"`
	VisualDescriptor   string  `json:"visual_descriptor"`
	EstimatedDurationS float64 `json:"estimated_duration_s"`
	Transition         string  `json:"transition"`
}

type ShotOverride struct {
	ShotCount  *int    `json:"shot_count,omitempty"`
	Transition *string `json:"transition,omitempty"`
}

type VisualBreakdownMetadata struct {
	VisualBreakdownModel    string `json:"visual_breakdown_model"`
	VisualBreakdownProvider string `json:"visual_breakdown_provider"`
	PromptTemplate          string `json:"prompt_template"`
	ShotFormulaVersion      string `json:"shot_formula_version"`
}
