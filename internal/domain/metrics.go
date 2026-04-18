package domain

// MetricID enumerates the five Day-90 pipeline metrics.
// Stored as string for stable JSON output; do not reorder.
type MetricID string

const (
	MetricAutomationRate            MetricID = "automation_rate"
	MetricCriticCalibration         MetricID = "critic_calibration"
	MetricCriticRegressionDetection MetricID = "critic_regression_detection"
	MetricDefectEscapeRate          MetricID = "defect_escape_rate"
	MetricResumeIdempotency         MetricID = "resume_idempotency"
)

// MetricComparator describes the direction of the target comparison.
type MetricComparator string

const (
	ComparatorGTE MetricComparator = "gte"
	ComparatorLTE MetricComparator = "lte"
)

var (
	metricOrder = []MetricID{
		MetricAutomationRate,
		MetricCriticCalibration,
		MetricCriticRegressionDetection,
		MetricDefectEscapeRate,
		MetricResumeIdempotency,
	}
	metricLabels = map[MetricID]string{
		MetricAutomationRate:            "Automation rate",
		MetricCriticCalibration:         "Critic calibration (kappa)",
		MetricCriticRegressionDetection: "Critic regression detection",
		MetricDefectEscapeRate:          "Defect escape rate",
		MetricResumeIdempotency:         "Stage-level resume idempot.",
	}
)

// Metric is one row in the metrics report.
type Metric struct {
	ID          MetricID         `json:"id"`
	Label       string           `json:"label"`
	Value       *float64         `json:"value"`
	Target      float64          `json:"target"`
	Comparator  MetricComparator `json:"comparator"`
	Pass        bool             `json:"pass"`
	Unavailable bool             `json:"unavailable"`
	Reason      string           `json:"reason,omitempty"`
}

// MetricsReport is the CLI payload envelope data.
type MetricsReport struct {
	Window      int      `json:"window"`
	WindowCount int      `json:"window_count"`
	Provisional bool     `json:"provisional"`
	Metrics     []Metric `json:"metrics"`
	GeneratedAt string   `json:"generated_at"`
}

// MetricIDs returns the stable external order for metrics rendering and JSON.
func MetricIDs() []MetricID {
	out := make([]MetricID, len(metricOrder))
	copy(out, metricOrder)
	return out
}

// Label returns the stable human label for a MetricID.
func Label(id MetricID) string {
	return metricLabels[id]
}
