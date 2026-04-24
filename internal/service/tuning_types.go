package service

import "time"

// CriticPromptEnvelope is the response body returned by the Tuning prompt
// GET/PUT endpoints. It mirrors the canonical editable prompt file along
// with the metadata required for run-level version tagging.
type CriticPromptEnvelope struct {
	Body        string `json:"body"`
	SavedAt     string `json:"saved_at"`
	PromptHash  string `json:"prompt_hash"`
	GitShortSHA string `json:"git_short_sha"`
	VersionTag  string `json:"version_tag"`
}

// TuningGoldenPair is the manifest-backed fixture pair surfaced to the UI.
type TuningGoldenPair struct {
	Index        int       `json:"index"`
	CreatedAt    time.Time `json:"created_at"`
	PositivePath string    `json:"positive_path"`
	NegativePath string    `json:"negative_path"`
}

// TuningGoldenFreshness summarises staleness advisories for the Golden set.
type TuningGoldenFreshness struct {
	Warnings          []string `json:"warnings"`
	DaysSinceRefresh  int      `json:"days_since_refresh"`
	PromptHashChanged bool     `json:"prompt_hash_changed"`
	CurrentPromptHash string   `json:"current_prompt_hash"`
}

// TuningGoldenReport mirrors eval.Report as a JSON-stable API type.
type TuningGoldenReport struct {
	Recall           float64 `json:"recall"`
	TotalNegative    int     `json:"total_negative"`
	DetectedNegative int     `json:"detected_negative"`
	FalseRejects     int     `json:"false_rejects"`
}

// TuningGoldenState is the aggregated payload returned by GET /api/tuning/golden.
type TuningGoldenState struct {
	Pairs      []TuningGoldenPair    `json:"pairs"`
	PairCount  int                   `json:"pair_count"`
	Freshness  TuningGoldenFreshness `json:"freshness"`
	LastReport *TuningGoldenReport   `json:"last_report,omitempty"`
}

// TuningCalibrationPoint is one entry in the kappa trend. Kappa is optional
// — unavailable kappa is a first-class state, not an error.
type TuningCalibrationPoint struct {
	ComputedAt  string   `json:"computed_at"`
	WindowCount int      `json:"window_count"`
	Provisional bool     `json:"provisional"`
	Kappa       *float64 `json:"kappa,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

// TuningCalibration is the full response body for GET /api/tuning/calibration.
// Points are returned oldest → newest, matching RecentCriticCalibrationTrend.
type TuningCalibration struct {
	Window int                      `json:"window"`
	Limit  int                      `json:"limit"`
	Points []TuningCalibrationPoint `json:"points"`
	Latest *TuningCalibrationPoint  `json:"latest,omitempty"`
}

// TuningShadowResultRow is a single replayed case in the Shadow response.
// Field names match ShadowResult but the struct lives here so the API
// contract is decoupled from the internal package shape.
type TuningShadowResultRow struct {
	RunID           string  `json:"run_id"`
	CreatedAt       string  `json:"created_at"`
	BaselineVerdict string  `json:"baseline_verdict"`
	BaselineScore   float64 `json:"baseline_score"`
	NewVerdict      string  `json:"new_verdict"`
	NewRetryReason  string  `json:"new_retry_reason,omitempty"`
	NewOverallScore int     `json:"new_overall_score"`
	OverallDiff     float64 `json:"overall_diff"`
	FalseRejection  bool    `json:"false_rejection"`
}

// TuningShadowReport is the full report body returned by POST /api/tuning/shadow/run.
type TuningShadowReport struct {
	Window          int                     `json:"window"`
	Evaluated       int                     `json:"evaluated"`
	FalseRejections int                     `json:"false_rejections"`
	Empty           bool                    `json:"empty"`
	SummaryLine     string                  `json:"summary_line"`
	Results         []TuningShadowResultRow `json:"results"`
	VersionTag      string                  `json:"version_tag,omitempty"`
}

// FastFeedbackSample is one deterministic 10-sample result.
type FastFeedbackSample struct {
	FixtureID    string `json:"fixture_id"`
	Verdict      string `json:"verdict"`
	RetryReason  string `json:"retry_reason,omitempty"`
	OverallScore int    `json:"overall_score"`
}

// FastFeedbackReport is the full response for POST /api/tuning/fast-feedback.
type FastFeedbackReport struct {
	SampleCount      int                  `json:"sample_count"`
	PassCount        int                  `json:"pass_count"`
	RetryCount       int                  `json:"retry_count"`
	AcceptNotesCount int                  `json:"accept_with_notes_count"`
	DurationMs       int64                `json:"duration_ms"`
	VersionTag       string               `json:"version_tag,omitempty"`
	Samples          []FastFeedbackSample `json:"samples"`
}
