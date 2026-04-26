package domain

const (
	CriticSourceVersionV1             = "v1-critic-post-writer"
	CriticSourceVersionPostReviewerV1 = "v1-critic-post-reviewer"

	CriticVerdictPass            = "pass"
	CriticVerdictRetry           = "retry"
	CriticVerdictAcceptWithNotes = "accept_with_notes"

	CriticCheckpointPostWriter   = "post_writer"
	CriticCheckpointPostReviewer = "post_reviewer"
)

var CriticRubricWeights = map[string]float64{
	"hook":                0.25,
	"fact_accuracy":       0.25,
	"emotional_variation": 0.25,
	"immersion":           0.25,
}

type CriticOutput struct {
	PostWriter   *CriticCheckpointReport `json:"post_writer,omitempty"`
	PostReviewer *CriticCheckpointReport `json:"post_reviewer,omitempty"`
}

type CriticCheckpointReport struct {
	Checkpoint          string               `json:"checkpoint"`
	Verdict             string               `json:"verdict"`
	RetryReason         string               `json:"retry_reason,omitempty"`
	OverallScore        int                  `json:"overall_score"`
	Rubric              CriticRubricScores   `json:"rubric"`
	Feedback            string               `json:"feedback"`
	SceneNotes          []CriticSceneNote    `json:"scene_notes"`
	MinorPolicyFindings []MinorPolicyFinding `json:"minor_policy_findings,omitempty"`
	Precheck            CriticPrecheck       `json:"precheck"`
	CriticModel         string               `json:"critic_model"`
	CriticProvider      string               `json:"critic_provider"`
	SourceVersion       string               `json:"source_version"`
}

type CriticRubricScores struct {
	Hook               int `json:"hook"`
	FactAccuracy       int `json:"fact_accuracy"`
	EmotionalVariation int `json:"emotional_variation"`
	Immersion          int `json:"immersion"`
}

type CriticSceneNote struct {
	SceneNum   int    `json:"scene_num"`
	Issue      string `json:"issue"`
	Suggestion string `json:"suggestion"`
}

type CriticPrecheck struct {
	SchemaValid       bool     `json:"schema_valid"`
	ForbiddenTermHits []string `json:"forbidden_term_hits"`
	ShortCircuited    bool     `json:"short_circuited"`
}

// PersistedCriticReport is a critic_reports row hydrated for callers (UI,
// diagnostic queries). attempt_number is the 1-indexed retry attempt;
// CreatedAt is the SQLite-formatted timestamp (UTC, no zone suffix).
type PersistedCriticReport struct {
	RunID         string
	AttemptNumber int
	Report        CriticCheckpointReport
	CreatedAt     string
}

// PersistedNarrationAttempt is a narration_attempts row hydrated for callers.
// Narration carries the full script the critic evaluated, including act_id,
// scene metadata, and source_version.
type PersistedNarrationAttempt struct {
	RunID         string
	AttemptNumber int
	Narration     *NarrationScript
	CreatedAt     string
}
