package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// RunStore implements service.RunStore using SQLite.
type RunStore struct {
	db *sql.DB
}

// NewRunStore creates a RunStore backed by the provided *sql.DB.
func NewRunStore(db *sql.DB) *RunStore {
	return &RunStore{db: db}
}

// maxCreateRetries bounds retries when a concurrent Create collides on the
// synthesized primary key. Three is enough under MaxOpenConns=1 + any
// foreseeable concurrency level for a single-operator desktop tool.
const maxCreateRetries = 3

// Create inserts a new run row and creates the per-run output directory.
// Run ID is derived as scp-{scpID}-run-{n} where n = COUNT(*)+1 for this scpID.
// The INSERT and ID calculation are wrapped in a transaction; on rare
// concurrent collisions (two transactions computing the same n) the PK
// conflict is retried with a fresh count. The output directory is created
// BEFORE the transaction commits so a failed mkdir rolls back the DB row,
// avoiding orphans.
func (s *RunStore) Create(ctx context.Context, scpID, outputDir string) (*domain.Run, error) {
	var lastErr error
	for attempt := 0; attempt < maxCreateRetries; attempt++ {
		run, err := s.createOnce(ctx, scpID, outputDir)
		if err == nil {
			return run, nil
		}
		if !isPKCollision(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("create run after %d retries: %w", maxCreateRetries, lastErr)
}

func (s *RunStore) createOnce(ctx context.Context, scpID, outputDir string) (*domain.Run, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var seq int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*)+1 FROM runs WHERE scp_id = ?", scpID,
	).Scan(&seq); err != nil {
		return nil, fmt.Errorf("compute run sequence: %w", err)
	}

	id := fmt.Sprintf("scp-%s-run-%d", scpID, seq)

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id, stage, status) VALUES (?, ?, ?, ?)`,
		id, scpID, string(domain.StagePending), string(domain.StatusPending),
	); err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}

	// Create the output directory BEFORE committing so filesystem failure
	// triggers a rollback (tx.Rollback via defer). Avoids the orphan-row case
	// where the DB has a run with no corresponding output directory.
	runDir := filepath.Join(outputDir, id)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return nil, fmt.Errorf("create output dir %s: %w", runDir, err)
	}

	if err := tx.Commit(); err != nil {
		// The directory exists but the DB row was rolled back — remove the
		// directory to keep filesystem and DB consistent. Best-effort.
		_ = os.Remove(runDir)
		return nil, fmt.Errorf("commit run: %w", err)
	}

	return s.Get(ctx, id)
}

// isPKCollision reports whether err is a SQLite primary-key constraint
// violation. Uses string matching because the driver doesn't export typed
// error codes.
func isPKCollision(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed: runs.id") ||
		strings.Contains(s, "constraint failed") && strings.Contains(s, "runs.id")
}

// Get returns the run with the given ID, or domain.ErrNotFound if absent.
func (s *RunStore) Get(ctx context.Context, id string) (*domain.Run, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, scp_id, stage, status, retry_count, retry_reason,
		        critic_score, cost_usd, token_in, token_out, duration_ms,
		        human_override, scenario_path, created_at, updated_at
		   FROM runs WHERE id = ?`, id)

	run, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("get run %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get run %s: %w", id, err)
	}
	return run, nil
}

// List returns all runs ordered by created_at ascending.
func (s *RunStore) List(ctx context.Context) ([]*domain.Run, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, scp_id, stage, status, retry_count, retry_reason,
		        critic_score, cost_usd, token_in, token_out, duration_ms,
		        human_override, scenario_path, created_at, updated_at
		   FROM runs ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var runs []*domain.Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run row: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs: %w", err)
	}
	return runs, nil
}

// SetStatus updates the status column and retry_reason of a run.
// A nil retryReason clears the retry_reason column. Returns ErrNotFound
// when the run does not exist. The Migration 002 AFTER UPDATE trigger
// advances updated_at as a side effect.
func (s *RunStore) SetStatus(ctx context.Context, id string, status domain.Status, retryReason *string) error {
	var reason sql.NullString
	if retryReason != nil {
		reason = sql.NullString{String: *retryReason, Valid: true}
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET status = ?, retry_reason = ? WHERE id = ?`,
		string(status), reason, id,
	)
	if err != nil {
		return fmt.Errorf("set status for %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set status for %s rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("set status for %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// IncrementRetryCount atomically increments retry_count by 1.
// Returns ErrNotFound when the run does not exist.
func (s *RunStore) IncrementRetryCount(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET retry_count = retry_count + 1 WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("increment retry_count for %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("increment retry_count for %s rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("increment retry_count for %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// ResetForResume atomically sets status, clears retry_reason, and
// increments retry_count in a single UPDATE. Used by the engine's Resume
// path to collapse what would otherwise be two separate round-trips into
// one, removing the torn-state window between SetStatus and
// IncrementRetryCount. Returns ErrNotFound when the run does not exist.
func (s *RunStore) ResetForResume(ctx context.Context, id string, status domain.Status) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs
		    SET status = ?,
		        retry_reason = NULL,
		        retry_count = retry_count + 1
		  WHERE id = ?`,
		string(status), id,
	)
	if err != nil {
		return fmt.Errorf("reset run %s for resume: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reset run %s for resume rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("reset run %s for resume: %w", id, domain.ErrNotFound)
	}
	return nil
}

// RecordStageObservation folds a StageObservation into the target run row.
// Accumulating columns (cost_usd, token_in, token_out, duration_ms, retry_count)
// are added via SQL arithmetic; retry_reason and critic_score use COALESCE so
// a nil pointer preserves the prior value and a non-nil pointer overwrites it;
// human_override is a sticky bit (bitwise-OR) — once set to 1 by any stage it
// never reverts. Returns ErrNotFound when the run does not exist.
//
// No explicit transaction: a single UPDATE is atomic under MaxOpenConns=1,
// and the Migration 002 AFTER UPDATE trigger fires once to advance updated_at.
func (s *RunStore) RecordStageObservation(ctx context.Context, runID string, obs domain.StageObservation) error {
	if err := obs.Validate(); err != nil {
		return fmt.Errorf("record stage observation for %s: %w", runID, err)
	}
	var retryReason sql.NullString
	if obs.RetryReason != nil {
		retryReason = sql.NullString{String: *obs.RetryReason, Valid: true}
	}
	var criticScore sql.NullFloat64
	if obs.CriticScore != nil {
		criticScore = sql.NullFloat64{Float64: *obs.CriticScore, Valid: true}
	}
	var humanOverride int
	if obs.HumanOverride {
		humanOverride = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET
		    cost_usd       = cost_usd + ?,
		    token_in       = token_in + ?,
		    token_out      = token_out + ?,
		    duration_ms    = duration_ms + ?,
		    retry_count    = retry_count + ?,
		    retry_reason   = COALESCE(?, retry_reason),
		    critic_score   = COALESCE(?, critic_score),
		    human_override = (human_override | ?)
		 WHERE id = ?`,
		obs.CostUSD, obs.TokenIn, obs.TokenOut, obs.DurationMs, obs.RetryCount,
		retryReason, criticScore, humanOverride, runID,
	)
	if err != nil {
		return fmt.Errorf("record stage observation for %s: %w", runID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("record stage observation for %s rows affected: %w", runID, err)
	}
	if n == 0 {
		return fmt.Errorf("record stage observation for %s: %w", runID, domain.ErrNotFound)
	}
	return nil
}

// Cancel sets a run's status to cancelled.
// Returns domain.ErrNotFound if the run does not exist.
// Returns domain.ErrConflict if the run exists but is not in a cancellable state.
// The UPDATE + existence check are ordered so a deleted row does not masquerade
// as a conflict: if RowsAffected is 0 we re-Get to disambiguate missing vs. terminal.
func (s *RunStore) Cancel(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET status = ? WHERE id = ? AND status IN (?, ?)`,
		string(domain.StatusCancelled), id,
		string(domain.StatusRunning), string(domain.StatusWaiting),
	)
	if err != nil {
		return fmt.Errorf("cancel run %s: %w", id, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("cancel run %s rows affected: %w", id, err)
	}
	if n > 0 {
		return nil
	}

	// Zero rows affected — either the row does not exist (ErrNotFound) or it
	// exists but is already terminal (ErrConflict). Disambiguate with a Get.
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return fmt.Errorf("cancel run %s: %w", id, domain.ErrConflict)
}

// scanner abstracts *sql.Row and *sql.Rows for scanRun.
type scanner interface {
	Scan(dest ...any) error
}

func scanRun(s scanner) (*domain.Run, error) {
	var r domain.Run
	var retryReason sql.NullString
	var criticScore sql.NullFloat64
	var scenarioPath sql.NullString
	var humanOverride int

	err := s.Scan(
		&r.ID, &r.SCPID, &r.Stage, &r.Status,
		&r.RetryCount, &retryReason,
		&criticScore, &r.CostUSD, &r.TokenIn, &r.TokenOut, &r.DurationMs,
		&humanOverride, &scenarioPath,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	r.HumanOverride = humanOverride != 0
	if retryReason.Valid {
		r.RetryReason = &retryReason.String
	}
	if criticScore.Valid {
		r.CriticScore = &criticScore.Float64
	}
	if scenarioPath.Valid {
		r.ScenarioPath = &scenarioPath.String
	}
	return &r, nil
}
