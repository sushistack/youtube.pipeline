package domain

type CriticCalibrationSnapshot struct {
	WindowSize           int      `json:"window_size"`
	WindowCount          int      `json:"window_count"`
	Provisional          bool     `json:"provisional"`
	CalibrationThreshold float64  `json:"calibration_threshold"`
	Kappa                *float64 `json:"kappa,omitempty"`
	Reason               string   `json:"reason,omitempty"`
	AgreementYesYes      int      `json:"agreement_yes_yes"`
	DisagreementYesNo    int      `json:"disagreement_yes_no"`
	DisagreementNoYes    int      `json:"disagreement_no_yes"`
	AgreementNoNo        int      `json:"agreement_no_no"`
	WindowStartRunID     string   `json:"window_start_run_id,omitempty"`
	WindowEndRunID       string   `json:"window_end_run_id,omitempty"`
	LatestDecisionID     int      `json:"latest_decision_id,omitempty"`
	ComputedAt           string   `json:"computed_at"`
}

type CriticCalibrationTrendPoint struct {
	ComputedAt  string   `json:"computed_at"`
	WindowCount int      `json:"window_count"`
	Provisional bool     `json:"provisional"`
	Kappa       *float64 `json:"kappa,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}
