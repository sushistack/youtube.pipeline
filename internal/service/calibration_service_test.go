package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type calibrationMetricsStub struct {
	runs             []db.RunMetricsRow
	pairs            []db.KappaPair
	latestDecisionID int
}

func (s calibrationMetricsStub) RecentCompletedRunsForMetrics(_ context.Context, _ int) ([]db.RunMetricsRow, error) {
	return s.runs, nil
}

func (s calibrationMetricsStub) KappaPairsForRuns(_ context.Context, _ []string, _ float64) ([]db.KappaPair, error) {
	return s.pairs, nil
}

func (s calibrationMetricsStub) LatestDecisionIDForRuns(_ context.Context, _ []string) (int, error) {
	return s.latestDecisionID, nil
}

type calibrationSnapshotRecorder struct {
	sourceKey string
	snap      domain.CriticCalibrationSnapshot
	calls     int
}

func (r *calibrationSnapshotRecorder) UpsertCriticCalibrationSnapshot(_ context.Context, sourceKey string, snap domain.CriticCalibrationSnapshot) error {
	r.sourceKey = sourceKey
	r.snap = snap
	r.calls++
	return nil
}

func (r *calibrationSnapshotRecorder) RecentCriticCalibrationTrend(_ context.Context, _ int, _ int) ([]domain.CriticCalibrationTrendPoint, error) {
	return nil, nil
}

func TestCalibrationService_Refresh_PersistsSnapshot(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	recorder := &calibrationSnapshotRecorder{}
	svc := NewCalibrationService(calibrationMetricsStub{
		runs: []db.RunMetricsRow{
			{ID: "run-2"},
			{ID: "run-1"},
		},
		pairs: []db.KappaPair{
			{CriticPass: true, OperatorApprove: true},
			{CriticPass: true, OperatorApprove: false},
			{CriticPass: false, OperatorApprove: false},
		},
		latestDecisionID: 12,
	}, recorder, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)))

	got, err := svc.RefreshCriticCalibration(context.Background(), 25, 0.70)
	if err != nil {
		t.Fatalf("RefreshCriticCalibration: %v", err)
	}

	testutil.AssertEqual(t, recorder.calls, 1)
	testutil.AssertEqual(t, recorder.sourceKey, "window=25|threshold=0.7|run=run-2|decision=12|count=2")
	testutil.AssertEqual(t, got.WindowSize, 25)
	testutil.AssertEqual(t, got.WindowCount, 2)
	testutil.AssertEqual(t, got.Provisional, true)
	testutil.AssertEqual(t, got.WindowStartRunID, "run-1")
	testutil.AssertEqual(t, got.WindowEndRunID, "run-2")
	testutil.AssertEqual(t, got.LatestDecisionID, 12)
	testutil.AssertEqual(t, got.ComputedAt, "2026-04-18T12:34:56Z")
	testutil.AssertEqual(t, got.AgreementYesYes, 1)
	testutil.AssertEqual(t, got.DisagreementYesNo, 1)
	testutil.AssertEqual(t, got.DisagreementNoYes, 0)
	testutil.AssertEqual(t, got.AgreementNoNo, 1)
	if got.Kappa == nil {
		t.Fatal("expected persisted kappa value")
	}
	testutil.AssertFloatNear(t, *got.Kappa, 0.39999999999999997, 1e-9)
}

func TestCalibrationService_Refresh_ProvisionalWhenShort(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	recorder := &calibrationSnapshotRecorder{}
	svc := NewCalibrationService(calibrationMetricsStub{
		runs: []db.RunMetricsRow{{ID: "run-1"}},
		pairs: []db.KappaPair{
			{CriticPass: true, OperatorApprove: true},
		},
		latestDecisionID: 7,
	}, recorder, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)))

	got, err := svc.RefreshCriticCalibration(context.Background(), 25, 0.70)
	if err != nil {
		t.Fatalf("RefreshCriticCalibration: %v", err)
	}

	testutil.AssertEqual(t, got.Provisional, true)
	testutil.AssertEqual(t, got.WindowCount, 1)
	testutil.AssertEqual(t, got.WindowStartRunID, "run-1")
	testutil.AssertEqual(t, got.WindowEndRunID, "run-1")
}

func TestCalibrationService_Refresh_DegeneratePersistsReason(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	recorder := &calibrationSnapshotRecorder{}
	svc := NewCalibrationService(calibrationMetricsStub{
		runs: []db.RunMetricsRow{
			{ID: "run-2"},
			{ID: "run-1"},
		},
		pairs: []db.KappaPair{
			{CriticPass: true, OperatorApprove: true},
			{CriticPass: true, OperatorApprove: true},
		},
		latestDecisionID: 4,
	}, recorder, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)))

	got, err := svc.RefreshCriticCalibration(context.Background(), 25, 0.70)
	if err != nil {
		t.Fatalf("RefreshCriticCalibration: %v", err)
	}

	if got.Kappa != nil {
		t.Fatal("expected nil kappa for degenerate matrix")
	}
	testutil.AssertEqual(t, got.Reason, "degenerate — no variance to calibrate against")
	testutil.AssertEqual(t, got.AgreementYesYes, 2)
	testutil.AssertEqual(t, got.DisagreementYesNo, 0)
	testutil.AssertEqual(t, got.DisagreementNoYes, 0)
	testutil.AssertEqual(t, got.AgreementNoNo, 0)
}

func TestCalibrationService_Refresh_RejectsInvalidThreshold(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	recorder := &calibrationSnapshotRecorder{}
	svc := NewCalibrationService(calibrationMetricsStub{}, recorder, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)))

	cases := []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.01, 1.01}
	for _, threshold := range cases {
		_, err := svc.RefreshCriticCalibration(context.Background(), 25, threshold)
		if err == nil {
			t.Fatalf("threshold %g: expected validation error, got nil", threshold)
		}
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("threshold %g: expected domain.ErrValidation, got %v", threshold, err)
		}
	}
	testutil.AssertEqual(t, recorder.calls, 0)
}

func TestCalibrationService_Refresh_NoCompletedRunsStillPersists(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	recorder := &calibrationSnapshotRecorder{}
	svc := NewCalibrationService(calibrationMetricsStub{}, recorder, clock.NewFakeClock(time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)))

	got, err := svc.RefreshCriticCalibration(context.Background(), 25, 0.70)
	if err != nil {
		t.Fatalf("RefreshCriticCalibration: %v", err)
	}

	testutil.AssertEqual(t, recorder.calls, 1)
	testutil.AssertEqual(t, recorder.sourceKey, "window=25|threshold=0.7|run=|decision=0|count=0")
	testutil.AssertEqual(t, got.WindowCount, 0)
	testutil.AssertEqual(t, got.Provisional, true)
	testutil.AssertEqual(t, got.Reason, "no paired observations")
	testutil.AssertEqual(t, got.WindowStartRunID, "")
	testutil.AssertEqual(t, got.WindowEndRunID, "")
	if got.Kappa != nil {
		t.Fatal("expected nil kappa when no completed runs exist")
	}
}
