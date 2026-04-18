package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

type CalibrationMetricsReader interface {
	RecentCompletedRunsForMetrics(ctx context.Context, window int) ([]db.RunMetricsRow, error)
	KappaPairsForRuns(ctx context.Context, runIDs []string, threshold float64) ([]db.KappaPair, error)
	LatestDecisionIDForRuns(ctx context.Context, runIDs []string) (int, error)
}

type CalibrationSnapshotWriter interface {
	UpsertCriticCalibrationSnapshot(ctx context.Context, sourceKey string, snap domain.CriticCalibrationSnapshot) error
	RecentCriticCalibrationTrend(ctx context.Context, windowSize int, limit int) ([]domain.CriticCalibrationTrendPoint, error)
}

type CalibrationService struct {
	metrics   CalibrationMetricsReader
	snapshots CalibrationSnapshotWriter
	clk       clock.Clock
}

func NewCalibrationService(metrics CalibrationMetricsReader, snapshots CalibrationSnapshotWriter, clk clock.Clock) *CalibrationService {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &CalibrationService{metrics: metrics, snapshots: snapshots, clk: clk}
}

func (s *CalibrationService) RefreshCriticCalibration(
	ctx context.Context,
	window int,
	calibrationThreshold float64,
) (*domain.CriticCalibrationSnapshot, error) {
	if window <= 0 {
		return nil, fmt.Errorf("refresh critic calibration: window %d must be > 0: %w", window, domain.ErrValidation)
	}
	if math.IsNaN(calibrationThreshold) || math.IsInf(calibrationThreshold, 0) || calibrationThreshold < 0 || calibrationThreshold > 1 {
		return nil, fmt.Errorf("refresh critic calibration: calibration threshold %g must be in [0, 1]: %w", calibrationThreshold, domain.ErrValidation)
	}

	runs, err := s.metrics.RecentCompletedRunsForMetrics(ctx, window)
	if err != nil {
		return nil, fmt.Errorf("refresh critic calibration: recent completed runs: %w", err)
	}

	snap := domain.CriticCalibrationSnapshot{
		WindowSize:           window,
		WindowCount:          len(runs),
		Provisional:          len(runs) < window,
		CalibrationThreshold: calibrationThreshold,
		ComputedAt:           s.clk.Now().UTC().Format(time.RFC3339),
	}
	if len(runs) > 0 {
		snap.WindowEndRunID = runs[0].ID
		snap.WindowStartRunID = runs[len(runs)-1].ID
	}

	runIDs := make([]string, len(runs))
	for i, run := range runs {
		runIDs[i] = run.ID
	}

	latestDecisionID, err := s.metrics.LatestDecisionIDForRuns(ctx, runIDs)
	if err != nil {
		return nil, fmt.Errorf("refresh critic calibration: latest decision id: %w", err)
	}
	snap.LatestDecisionID = latestDecisionID

	pairs, err := s.metrics.KappaPairsForRuns(ctx, runIDs, calibrationThreshold)
	if err != nil {
		return nil, fmt.Errorf("refresh critic calibration: kappa pairs: %w", err)
	}

	a, b, c, d := kappaCounts(pairs)
	snap.AgreementYesYes = a
	snap.DisagreementYesNo = b
	snap.DisagreementNoYes = c
	snap.AgreementNoNo = d

	kappa, ok, reason := CohensKappa(a, b, c, d)
	if ok {
		snap.Kappa = &kappa
	} else {
		snap.Reason = reason
	}

	sourceKey := calibrationSourceKey(window, calibrationThreshold, snap.WindowEndRunID, latestDecisionID, len(runs))
	if err := s.snapshots.UpsertCriticCalibrationSnapshot(ctx, sourceKey, snap); err != nil {
		return nil, fmt.Errorf("refresh critic calibration: persist snapshot: %w", err)
	}
	return &snap, nil
}

func calibrationSourceKey(window int, threshold float64, latestRunID string, latestDecisionID int, windowCount int) string {
	return "window=" + strconv.Itoa(window) +
		"|threshold=" + strconv.FormatFloat(threshold, 'f', -1, 64) +
		"|run=" + latestRunID +
		"|decision=" + strconv.Itoa(latestDecisionID) +
		"|count=" + strconv.Itoa(windowCount)
}
