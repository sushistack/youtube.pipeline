package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

type CalibrationStore struct {
	db *sql.DB
}

func NewCalibrationStore(db *sql.DB) *CalibrationStore {
	return &CalibrationStore{db: db}
}

func (s *CalibrationStore) UpsertCriticCalibrationSnapshot(
	ctx context.Context,
	sourceKey string,
	snap domain.CriticCalibrationSnapshot,
) error {
	if sourceKey == "" {
		return fmt.Errorf("upsert critic calibration snapshot: empty source key: %w", domain.ErrValidation)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO critic_calibration_snapshots (
	source_key,
	window_size,
	window_count,
	provisional,
	calibration_threshold,
	kappa,
	reason,
	agreement_yes_yes,
	disagreement_yes_no,
	disagreement_no_yes,
	agreement_no_no,
	window_start_run_id,
	window_end_run_id,
	latest_decision_id,
	computed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source_key) DO UPDATE SET
	window_size = excluded.window_size,
	window_count = excluded.window_count,
	provisional = excluded.provisional,
	calibration_threshold = excluded.calibration_threshold,
	kappa = excluded.kappa,
	reason = excluded.reason,
	agreement_yes_yes = excluded.agreement_yes_yes,
	disagreement_yes_no = excluded.disagreement_yes_no,
	disagreement_no_yes = excluded.disagreement_no_yes,
	agreement_no_no = excluded.agreement_no_no,
	window_start_run_id = excluded.window_start_run_id,
	window_end_run_id = excluded.window_end_run_id,
	latest_decision_id = excluded.latest_decision_id,
	computed_at = excluded.computed_at`,
		sourceKey,
		snap.WindowSize,
		snap.WindowCount,
		boolToInt(snap.Provisional),
		snap.CalibrationThreshold,
		snap.Kappa,
		nullIfEmpty(snap.Reason),
		snap.AgreementYesYes,
		snap.DisagreementYesNo,
		snap.DisagreementNoYes,
		snap.AgreementNoNo,
		nullIfEmpty(snap.WindowStartRunID),
		nullIfEmpty(snap.WindowEndRunID),
		snap.LatestDecisionID,
		snap.ComputedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert critic calibration snapshot: %w", err)
	}
	return nil
}

func (s *CalibrationStore) RecentCriticCalibrationTrend(
	ctx context.Context,
	windowSize int,
	limit int,
) ([]domain.CriticCalibrationTrendPoint, error) {
	if windowSize <= 0 {
		return nil, fmt.Errorf("recent critic calibration trend: window size %d must be > 0: %w", windowSize, domain.ErrValidation)
	}
	if limit <= 0 {
		return nil, fmt.Errorf("recent critic calibration trend: limit %d must be > 0: %w", limit, domain.ErrValidation)
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT computed_at, window_count, provisional, kappa, reason
  FROM (
	SELECT id, computed_at, window_count, provisional, kappa, reason
	  FROM critic_calibration_snapshots
	 WHERE window_size = ?
	 ORDER BY computed_at DESC, id DESC
	 LIMIT ?
  )
 ORDER BY computed_at ASC, id ASC`, windowSize, limit)
	if err != nil {
		return nil, fmt.Errorf("recent critic calibration trend: %w", err)
	}
	defer rows.Close()

	var out []domain.CriticCalibrationTrendPoint
	for rows.Next() {
		var (
			point       domain.CriticCalibrationTrendPoint
			provisional int
			kappa       sql.NullFloat64
			reason      sql.NullString
		)
		if err := rows.Scan(&point.ComputedAt, &point.WindowCount, &provisional, &kappa, &reason); err != nil {
			return nil, fmt.Errorf("recent critic calibration trend scan: %w", err)
		}
		point.Provisional = provisional != 0
		if kappa.Valid {
			point.Kappa = &kappa.Float64
		}
		if reason.Valid {
			point.Reason = reason.String
		}
		out = append(out, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recent critic calibration trend iterate: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
