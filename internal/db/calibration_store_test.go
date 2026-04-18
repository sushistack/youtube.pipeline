package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestCalibrationStore_UpsertCriticCalibrationSnapshot_IdempotentBySourceKey(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	store := db.NewCalibrationStore(database)

	firstKappa := 0.71
	secondKappa := 0.82
	if err := store.UpsertCriticCalibrationSnapshot(ctx, "window=25|threshold=0.7|run=r2|decision=9|count=2", domain.CriticCalibrationSnapshot{
		WindowSize:           25,
		WindowCount:          2,
		Provisional:          true,
		CalibrationThreshold: 0.70,
		Kappa:                &firstKappa,
		AgreementYesYes:      3,
		DisagreementYesNo:    1,
		DisagreementNoYes:    0,
		AgreementNoNo:        2,
		WindowStartRunID:     "r1",
		WindowEndRunID:       "r2",
		LatestDecisionID:     9,
		ComputedAt:           "2026-04-18T12:34:56Z",
	}); err != nil {
		t.Fatalf("UpsertCriticCalibrationSnapshot(first): %v", err)
	}
	if err := store.UpsertCriticCalibrationSnapshot(ctx, "window=25|threshold=0.7|run=r2|decision=9|count=2", domain.CriticCalibrationSnapshot{
		WindowSize:           25,
		WindowCount:          2,
		Provisional:          true,
		CalibrationThreshold: 0.70,
		Kappa:                &secondKappa,
		AgreementYesYes:      4,
		DisagreementYesNo:    0,
		DisagreementNoYes:    0,
		AgreementNoNo:        2,
		WindowStartRunID:     "r1",
		WindowEndRunID:       "r2",
		LatestDecisionID:     9,
		ComputedAt:           "2026-04-18T12:35:56Z",
	}); err != nil {
		t.Fatalf("UpsertCriticCalibrationSnapshot(second): %v", err)
	}

	var (
		rowCount    int
		kappa       *float64
		computedAt  string
		yesYesCount int
	)
	if err := database.QueryRow(`
		SELECT COUNT(*)
		  FROM critic_calibration_snapshots
		 WHERE source_key = 'window=25|threshold=0.7|run=r2|decision=9|count=2'`,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("row count = %d, want 1", rowCount)
	}

	var sqlKappa float64
	if err := database.QueryRow(`
		SELECT kappa, computed_at, agreement_yes_yes
		  FROM critic_calibration_snapshots
		 WHERE source_key = 'window=25|threshold=0.7|run=r2|decision=9|count=2'`,
	).Scan(&sqlKappa, &computedAt, &yesYesCount); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	kappa = &sqlKappa
	testutil.AssertFloatNear(t, *kappa, secondKappa, 1e-9)
	testutil.AssertEqual(t, computedAt, "2026-04-18T12:35:56Z")
	testutil.AssertEqual(t, yesYesCount, 4)
}

func TestCalibrationStore_RecentCriticCalibrationTrend_OldestFirst(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	store := db.NewCalibrationStore(database)

	kappaOne := 0.40
	kappaTwo := 0.60
	if err := store.UpsertCriticCalibrationSnapshot(ctx, "k1", domain.CriticCalibrationSnapshot{
		WindowSize:           25,
		WindowCount:          20,
		Provisional:          true,
		CalibrationThreshold: 0.70,
		Kappa:                &kappaOne,
		ComputedAt:           "2026-04-18T12:00:00Z",
	}); err != nil {
		t.Fatalf("seed k1: %v", err)
	}
	if err := store.UpsertCriticCalibrationSnapshot(ctx, "k2", domain.CriticCalibrationSnapshot{
		WindowSize:           25,
		WindowCount:          21,
		Provisional:          true,
		CalibrationThreshold: 0.70,
		Reason:               "no paired observations",
		ComputedAt:           "2026-04-18T12:10:00Z",
	}); err != nil {
		t.Fatalf("seed k2: %v", err)
	}
	if err := store.UpsertCriticCalibrationSnapshot(ctx, "k3", domain.CriticCalibrationSnapshot{
		WindowSize:           25,
		WindowCount:          25,
		Provisional:          false,
		CalibrationThreshold: 0.70,
		Kappa:                &kappaTwo,
		ComputedAt:           "2026-04-18T12:20:00Z",
	}); err != nil {
		t.Fatalf("seed k3: %v", err)
	}

	got, err := store.RecentCriticCalibrationTrend(ctx, 25, 2)
	if err != nil {
		t.Fatalf("RecentCriticCalibrationTrend: %v", err)
	}

	testutil.AssertEqual(t, len(got), 2)
	testutil.AssertEqual(t, got[0].ComputedAt, "2026-04-18T12:10:00Z")
	testutil.AssertEqual(t, got[0].WindowCount, 21)
	testutil.AssertEqual(t, got[0].Provisional, true)
	if got[0].Kappa != nil {
		t.Fatal("expected nil kappa for unavailable point")
	}
	testutil.AssertEqual(t, got[0].Reason, "no paired observations")
	testutil.AssertEqual(t, got[1].ComputedAt, "2026-04-18T12:20:00Z")
	if got[1].Kappa == nil {
		t.Fatal("expected numeric kappa for latest point")
	}
	testutil.AssertFloatNear(t, *got[1].Kappa, kappaTwo, 1e-9)
}

func TestCalibrationStore_RecentCriticCalibrationTrend_Validation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := db.NewCalibrationStore(testutil.NewTestDB(t))

	_, err := store.RecentCriticCalibrationTrend(context.Background(), 0, 5)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("window validation error = %v, want ErrValidation", err)
	}

	_, err = store.RecentCriticCalibrationTrend(context.Background(), 25, 0)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("limit validation error = %v, want ErrValidation", err)
	}
}
