package domain

const (
	// CriticSourceVersionV2 / CriticSourceVersionPostReviewerV2 mark the
	// monologue-mode critic emission. v1 (per-scene scene_num findings) is
	// gone after D4 — the bridge died with the last consumer.
	CriticSourceVersionV2             = "v2-critic-post-writer"
	CriticSourceVersionPostReviewerV2 = "v2-critic-post-reviewer"

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

// CriticSceneNote is one act-level improvement note emitted by the critic
// LLM. v2 keys notes on ActID (act-paragraph granularity per the D plan
// "per-beat or per-act-paragraph" rule); per-beat notes were rejected as
// over-dense for HITL surfacing. RuneOffset is optional and points into
// ActScript.Monologue when the LLM wants to anchor a note to a specific
// span; consumers MAY ignore it.
type CriticSceneNote struct {
	ActID      string `json:"act_id"`
	RuneOffset int    `json:"rune_offset,omitempty"`
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
