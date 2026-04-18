package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMetric_JSONShape(t *testing.T) {
	report := MetricsReport{
		Window:      25,
		WindowCount: 0,
		Provisional: true,
		Metrics: []Metric{
			{ID: MetricAutomationRate, Label: Label(MetricAutomationRate), Target: 0.80, Comparator: ComparatorGTE, Unavailable: true},
			{ID: MetricCriticCalibration, Label: Label(MetricCriticCalibration), Target: 0.70, Comparator: ComparatorGTE, Unavailable: true},
			{ID: MetricCriticRegressionDetection, Label: Label(MetricCriticRegressionDetection), Target: 0.80, Comparator: ComparatorGTE, Unavailable: true},
			{ID: MetricDefectEscapeRate, Label: Label(MetricDefectEscapeRate), Target: 0.05, Comparator: ComparatorLTE, Unavailable: true},
			{ID: MetricResumeIdempotency, Label: Label(MetricResumeIdempotency), Target: 1.0, Comparator: ComparatorGTE, Unavailable: true},
		},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	for _, key := range []string{"window", "window_count", "provisional", "metrics", "generated_at"} {
		if _, ok := doc[key]; !ok {
			t.Fatalf("expected key %q in marshaled report", key)
		}
	}

	rawMetrics, ok := doc["metrics"].([]any)
	if !ok {
		t.Fatalf("metrics type = %T, want []any", doc["metrics"])
	}
	if len(rawMetrics) != 5 {
		t.Fatalf("len(metrics) = %d, want 5", len(rawMetrics))
	}
}

func TestLabel_AllMetricIDs(t *testing.T) {
	want := []MetricID{
		MetricAutomationRate,
		MetricCriticCalibration,
		MetricCriticRegressionDetection,
		MetricDefectEscapeRate,
		MetricResumeIdempotency,
	}
	got := MetricIDs()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MetricIDs() = %v, want %v", got, want)
	}
	for _, id := range got {
		if Label(id) == "" {
			t.Fatalf("Label(%q) is empty", id)
		}
	}
}
