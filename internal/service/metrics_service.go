package service

import (
	"context"
	"fmt"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// MetricsStore is the narrow persistence surface required by MetricsService.
type MetricsStore interface {
	RecentCompletedRunsForMetrics(ctx context.Context, window int) ([]db.RunMetricsRow, error)
	KappaPairsForRuns(ctx context.Context, runIDs []string, threshold float64) ([]db.KappaPair, error)
	DefectEscapeInRuns(ctx context.Context, runIDs []string, threshold float64) (db.DefectEscape, error)
}

// MetricsService computes the Day-90 pipeline metrics report.
type MetricsService struct {
	store MetricsStore
	clk   clock.Clock
}

// NewMetricsService constructs a MetricsService.
func NewMetricsService(store MetricsStore, clk clock.Clock) *MetricsService {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &MetricsService{store: store, clk: clk}
}

// Report returns the rolling-window metrics report.
func (s *MetricsService) Report(
	ctx context.Context,
	window int,
	calibrationThreshold float64,
	regressionDetection *float64,
	idempotencyRate *float64,
) (*domain.MetricsReport, error) {
	if window <= 0 {
		return nil, fmt.Errorf("metrics report: window %d must be > 0: %w", window, domain.ErrValidation)
	}

	runs, err := s.store.RecentCompletedRunsForMetrics(ctx, window)
	if err != nil {
		return nil, fmt.Errorf("metrics report: recent completed runs: %w", err)
	}

	report := &domain.MetricsReport{
		Window:      window,
		WindowCount: len(runs),
		Provisional: len(runs) < window,
		GeneratedAt: s.clk.Now().UTC().Format(time.RFC3339),
	}

	if len(runs) == 0 {
		report.Metrics = []domain.Metric{
			unavailableMetric(domain.MetricAutomationRate, 0.80, domain.ComparatorGTE, "no completed runs in window"),
			unavailableMetric(domain.MetricCriticCalibration, 0.70, domain.ComparatorGTE, "no completed runs in window"),
			unavailableMetric(domain.MetricCriticRegressionDetection, 0.80, domain.ComparatorGTE, "no completed runs in window"),
			unavailableMetric(domain.MetricDefectEscapeRate, 0.05, domain.ComparatorLTE, "no completed runs in window"),
			unavailableMetric(domain.MetricResumeIdempotency, 1.0, domain.ComparatorGTE, "no completed runs in window"),
		}
		return report, nil
	}

	runIDs := make([]string, len(runs))
	overrideCount := 0
	for i, run := range runs {
		runIDs[i] = run.ID
		if run.HumanOverride {
			overrideCount++
		}
	}

	kappaPairs, err := s.store.KappaPairsForRuns(ctx, runIDs, calibrationThreshold)
	if err != nil {
		return nil, fmt.Errorf("metrics report: kappa pairs: %w", err)
	}
	defectEscape, err := s.store.DefectEscapeInRuns(ctx, runIDs, calibrationThreshold)
	if err != nil {
		return nil, fmt.Errorf("metrics report: defect escape: %w", err)
	}

	automationValue := float64(len(runs)-overrideCount) / float64(len(runs))

	a, b, c, d := kappaCounts(kappaPairs)
	kappaValue, kappaOK, kappaReason := CohensKappa(a, b, c, d)

	var metrics []domain.Metric
	metrics = append(metrics, valueMetric(
		domain.MetricAutomationRate, automationValue, 0.80, domain.ComparatorGTE,
	))
	if !kappaOK {
		metrics = append(metrics, unavailableMetric(
			domain.MetricCriticCalibration, 0.70, domain.ComparatorGTE, kappaReason,
		))
	} else {
		metrics = append(metrics, valueMetric(
			domain.MetricCriticCalibration, kappaValue, 0.70, domain.ComparatorGTE,
		))
	}
	if regressionDetection == nil {
		metrics = append(metrics, unavailableMetric(
			domain.MetricCriticRegressionDetection, 0.80, domain.ComparatorGTE, "not provided via --regression-rate",
		))
	} else {
		metrics = append(metrics, valueMetric(
			domain.MetricCriticRegressionDetection, *regressionDetection, 0.80, domain.ComparatorGTE,
		))
	}
	if defectEscape.AutoPassedScenes == 0 {
		metrics = append(metrics, unavailableMetric(
			domain.MetricDefectEscapeRate, 0.05, domain.ComparatorLTE, "no auto-passed scenes",
		))
	} else {
		metrics = append(metrics, valueMetric(
			domain.MetricDefectEscapeRate,
			float64(defectEscape.EscapedScenes)/float64(defectEscape.AutoPassedScenes),
			0.05,
			domain.ComparatorLTE,
		))
	}
	if idempotencyRate == nil {
		metrics = append(metrics, unavailableMetric(
			domain.MetricResumeIdempotency, 1.0, domain.ComparatorGTE, "not provided via --idempotency-rate",
		))
	} else {
		metrics = append(metrics, valueMetric(
			domain.MetricResumeIdempotency, *idempotencyRate, 1.0, domain.ComparatorGTE,
		))
	}

	report.Metrics = metrics
	return report, nil
}

func kappaCounts(pairs []db.KappaPair) (a, b, c, d int) {
	for _, pair := range pairs {
		switch {
		case pair.CriticPass && pair.OperatorApprove:
			a++
		case pair.CriticPass && !pair.OperatorApprove:
			b++
		case !pair.CriticPass && pair.OperatorApprove:
			c++
		default:
			d++
		}
	}
	return a, b, c, d
}

func unavailableMetric(id domain.MetricID, target float64, comparator domain.MetricComparator, reason string) domain.Metric {
	return domain.Metric{
		ID:          id,
		Label:       domain.Label(id),
		Target:      target,
		Comparator:  comparator,
		Pass:        false,
		Unavailable: true,
		Reason:      reason,
	}
}

func valueMetric(id domain.MetricID, value, target float64, comparator domain.MetricComparator) domain.Metric {
	pass := value >= target
	if comparator == domain.ComparatorLTE {
		pass = value <= target
	}
	return domain.Metric{
		ID:          id,
		Label:       domain.Label(id),
		Value:       &value,
		Target:      target,
		Comparator:  comparator,
		Pass:        pass,
		Unavailable: false,
	}
}
