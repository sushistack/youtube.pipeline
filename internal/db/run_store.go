package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// RunStore implements service.RunStore using SQLite.
type RunStore struct {
	db *sql.DB
}

// RunMetricsRow is the per-run observability slice required by Day-90 metrics.
type RunMetricsRow struct {
	ID            string
	Status        string
	CriticScore   *float64
	HumanOverride bool
	RetryCount    int
	RetryReason   *string
	CreatedAt     string
}

// NewRunStore creates a RunStore backed by the provided *sql.DB.
func NewRunStore(db *sql.DB) *RunStore {
	return &RunStore{db: db}
}

// maxCreateRetries bounds retries when a concurrent Create collides on the
// synthesized primary key. Three is enough under MaxOpenConns=1 + any
// foreseeable concurrency level for a single-operator desktop tool.
const maxCreateRetries = 3

// PromptVersionTag carries the Critic prompt version metadata stamped on a
// run at creation time. Both fields are required together: Story 10.2 AC-3
// treats version+hash as one immutable unit.
type PromptVersionTag struct {
	Version string
	Hash    string
}

// Create inserts a new run row and creates the per-run output directory.
// Run ID is derived as scp-{scpID}-run-{n} where n = COUNT(*)+1 for this scpID.
// The INSERT and ID calculation are wrapped in a transaction; on rare
// concurrent collisions (two transactions computing the same n) the PK
// conflict is retried with a fresh count. The output directory is created
// BEFORE the transaction commits so a failed mkdir rolls back the DB row,
// avoiding orphans.
func (s *RunStore) Create(ctx context.Context, scpID, outputDir string) (*domain.Run, error) {
	return s.CreateWithPromptVersion(ctx, scpID, outputDir, nil)
}

// CreateWithPromptVersion is the AC-3 variant of Create that also stamps the
// active Critic prompt version/hash on the new row. When tag is nil the
// behavior is identical to Create — the columns remain NULL, matching the
// "existing rows stay NULL" rule for runs created before a prompt was ever
// saved through the Tuning surface.
func (s *RunStore) CreateWithPromptVersion(
	ctx context.Context,
	scpID, outputDir string,
	tag *PromptVersionTag,
) (*domain.Run, error) {
	var lastErr error
	for attempt := 0; attempt < maxCreateRetries; attempt++ {
		run, err := s.createOnce(ctx, scpID, outputDir, tag)
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

func (s *RunStore) createOnce(ctx context.Context, scpID, outputDir string, tag *PromptVersionTag) (*domain.Run, error) {
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

	var promptVersion, promptHash sql.NullString
	if tag != nil && tag.Version != "" {
		promptVersion = sql.NullString{String: tag.Version, Valid: true}
		promptHash = sql.NullString{String: tag.Hash, Valid: true}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id, stage, status, critic_prompt_version, critic_prompt_hash)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, scpID, string(domain.StagePending), string(domain.StatusPending),
		promptVersion, promptHash,
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
// violation on runs.id. Uses string matching because the driver doesn't
// export typed error codes. The match is intentionally exact: earlier
// variants also accepted any message mentioning "constraint failed" and
// "runs.id" together, which spuriously matched unrelated FK violations
// and triggered retry loops on non-recoverable errors.
func isPKCollision(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed: runs.id")
}

// Get returns the run with the given ID, or domain.ErrNotFound if absent.
func (s *RunStore) Get(ctx context.Context, id string) (*domain.Run, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, scp_id, stage, status, retry_count, retry_reason,
		        critic_score, cost_usd, token_in, token_out, duration_ms,
		        human_override, scenario_path, character_query_key,
		        selected_character_id, frozen_descriptor,
		        critic_prompt_version, critic_prompt_hash,
		        created_at, updated_at
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
		        human_override, scenario_path, character_query_key,
		        selected_character_id, frozen_descriptor,
		        critic_prompt_version, critic_prompt_hash,
		        created_at, updated_at
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

func (s *RunStore) ApplyPhaseAResult(ctx context.Context, runID string, res domain.PhaseAAdvanceResult) error {
	var retryReason sql.NullString
	if res.RetryReason != nil {
		retryReason = sql.NullString{String: *res.RetryReason, Valid: true}
	}
	var criticScore sql.NullFloat64
	if res.CriticScore != nil {
		criticScore = sql.NullFloat64{Float64: *res.CriticScore, Valid: true}
	}
	var scenarioPath sql.NullString
	if res.ScenarioPath != nil {
		scenarioPath = sql.NullString{String: *res.ScenarioPath, Valid: true}
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE runs
		    SET stage = ?,
		        status = ?,
		        retry_reason = ?,
		        critic_score = ?,
		        scenario_path = ?
		  WHERE id = ?`,
		string(res.Stage),
		string(res.Status),
		retryReason,
		criticScore,
		scenarioPath,
		runID,
	)
	if err != nil {
		return fmt.Errorf("apply phase a result for %s: %w", runID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("apply phase a result for %s rows affected: %w", runID, err)
	}
	if n == 0 {
		return fmt.Errorf("apply phase a result for %s: %w", runID, domain.ErrNotFound)
	}
	return nil
}

// SetCharacterQueryKey records the active normalized character query for a run.
func (s *RunStore) SetCharacterQueryKey(ctx context.Context, id, queryKey string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET character_query_key = ? WHERE id = ?`,
		queryKey, id,
	)
	if err != nil {
		return fmt.Errorf("set character query key for %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set character query key for %s rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("set character query key for %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// MarkComplete atomically transitions a run from (metadata_ack, waiting) to
// (complete, completed). This is the NFR-L1 gate: ready-for-upload is ONLY
// reachable via this path, and the stage/status predicate in WHERE prevents
// TOCTOU races with concurrent Cancel or repeat Ack attempts.
//
// Returns domain.ErrNotFound when the run does not exist, domain.ErrConflict
// when the run exists but is not at (metadata_ack, waiting). Ordering mirrors
// Cancel: on RowsAffected=0 we re-Get to disambiguate missing vs. terminal.
func (s *RunStore) MarkComplete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET stage = 'complete', status = 'completed', updated_at = ?
		  WHERE id = ? AND stage = ? AND status = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), id,
		string(domain.StageMetadataAck), string(domain.StatusWaiting),
	)
	if err != nil {
		return fmt.Errorf("mark complete %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark complete %s rows affected: %w", id, err)
	}
	if n == 0 {
		if _, err := s.Get(ctx, id); err != nil {
			return err
		}
		return fmt.Errorf("mark complete %s: %w", id, domain.ErrConflict)
	}
	return nil
}

// SetSelectedCharacterID persists the operator's selected character candidate ID.
func (s *RunStore) SetSelectedCharacterID(ctx context.Context, id, selectedCharacterID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET selected_character_id = ? WHERE id = ?`,
		selectedCharacterID, id,
	)
	if err != nil {
		return fmt.Errorf("set selected character id for %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set selected character id for %s rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("set selected character id for %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// ApplyCharacterPick atomically persists the selected character, optionally
// records the operator-edited frozen descriptor, and advances the run in a
// single UPDATE. A nil frozenDescriptor leaves the existing value unchanged
// (COALESCE preserves whatever was already there); a non-nil pointer writes
// the new value verbatim (empty string is allowed to explicitly clear).
func (s *RunStore) ApplyCharacterPick(
	ctx context.Context,
	id string,
	queryKey string,
	selectedCharacterID string,
	frozenDescriptor *string,
	stage domain.Stage,
	status domain.Status,
) error {
	var fd sql.NullString
	if frozenDescriptor != nil {
		fd = sql.NullString{String: *frozenDescriptor, Valid: true}
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs
		    SET character_query_key = ?,
		        selected_character_id = ?,
		        frozen_descriptor = COALESCE(?, frozen_descriptor),
		        stage = ?,
		        status = ?
		  WHERE id = ?`,
		queryKey,
		selectedCharacterID,
		fd,
		string(stage),
		string(status),
		id,
	)
	if err != nil {
		return fmt.Errorf("apply character pick for %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("apply character pick for %s rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("apply character pick for %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// SetFrozenDescriptor writes the operator-edited frozen descriptor to the run
// row. An empty string explicitly clears the column (NULL is not an option via
// this entry point). Returns ErrNotFound when the run does not exist.
func (s *RunStore) SetFrozenDescriptor(ctx context.Context, id, descriptor string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET frozen_descriptor = ? WHERE id = ?`,
		descriptor, id,
	)
	if err != nil {
		return fmt.Errorf("set frozen descriptor for %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set frozen descriptor for %s rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("set frozen descriptor for %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// UpdateOutputPath writes the output_path column for a run.
// Returns ErrNotFound when the run does not exist.
func (s *RunStore) UpdateOutputPath(ctx context.Context, runID string, outputPath string) error {
	if runID == "" {
		return fmt.Errorf("update output path: %w: run_id is empty", domain.ErrValidation)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET output_path = ? WHERE id = ?`,
		outputPath, runID,
	)
	if err != nil {
		return fmt.Errorf("update output path %s: %w", runID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update output path %s rows affected: %w", runID, err)
	}
	if n == 0 {
		return fmt.Errorf("update output path %s: %w", runID, domain.ErrNotFound)
	}
	return nil
}

// LatestFrozenDescriptorBySCPID returns the most-recently-updated non-null
// frozen_descriptor for *completed* runs sharing scpID, excluding excludeRunID
// (typically the current run). Returns (nil, nil) when no prior run has a
// saved value — callers interpret that as "no prior descriptor available".
//
// The status=completed filter implements AC-4's "most recent other *completed*
// run" rule: a cancelled/failed/mid-flight run that happened to persist a
// descriptor must not shadow the last descriptor an operator shipped to
// completion. Without this predicate, a blown-up run whose only artifact is a
// saved descriptor would pollute the prefill of every future pick for this SCP.
func (s *RunStore) LatestFrozenDescriptorBySCPID(ctx context.Context, scpID, excludeRunID string) (*string, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT frozen_descriptor
		   FROM runs
		  WHERE scp_id = ?
		    AND id != ?
		    AND status = ?
		    AND frozen_descriptor IS NOT NULL
		  ORDER BY updated_at DESC, id DESC
		  LIMIT 1`,
		scpID, excludeRunID, string(domain.StatusCompleted),
	)
	var fd sql.NullString
	if err := row.Scan(&fd); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("latest frozen descriptor by scp %s: %w", scpID, err)
	}
	if !fd.Valid {
		return nil, nil
	}
	value := fd.String
	return &value, nil
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

// Cancel sets a run's status to cancelled and removes any HITL session
// row in the same transaction. Returns domain.ErrNotFound if the run does
// not exist. Returns domain.ErrConflict if the run exists but is not in a
// cancellable state. The UPDATE + existence check are ordered so a deleted
// row does not masquerade as a conflict: if RowsAffected is 0 we re-Get to
// disambiguate missing vs. terminal.
//
// The hitl_sessions DELETE is unconditional within the tx: if the run was
// not paused the DELETE is a no-op (0 rows affected). Wrapping both ops in
// one transaction preserves the invariant "hitl_sessions row exists iff
// run.status=waiting AND run.stage ∈ HITL stages" even across crashes.
func (s *RunStore) Cancel(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("cancel run %s: begin tx: %w", id, err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
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
	if n == 0 {
		// Zero rows affected — either the row does not exist (ErrNotFound)
		// or it exists but is already terminal (ErrConflict). Roll the tx
		// back FIRST so the subsequent Get can acquire the lone connection
		// (MaxOpenConns=1 serializes tx + plain query).
		_ = tx.Rollback() // release connection before disambiguation Get (MaxOpenConns=1)
		if _, err := s.Get(ctx, id); err != nil {
			return err
		}
		return fmt.Errorf("cancel run %s: %w", id, domain.ErrConflict)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM hitl_sessions WHERE run_id = ?`, id); err != nil {
		return fmt.Errorf("cancel run %s: delete hitl_session: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("cancel run %s: commit: %w", id, err)
	}
	return nil
}

// AntiProgressStats summarizes anti-progress events over the N most recent
// runs that tripped the detector. Inputs to NFR-R2's V1.5 ≤5% gate.
type AntiProgressStats struct {
	Total            int  // runs with retry_reason='anti_progress' in the window
	OperatorOverride int  // of Total, runs where human_override=1 (FP proxy in V1)
	Provisional      bool // true when Total < window (insufficient data)
}

// AntiProgressFalsePositiveStats counts anti-progress events over the last
// `window` runs (ordered by created_at DESC) that carry
// retry_reason='anti_progress'. The "false-positive" definition in V1 is
// a proxy: runs where the operator intervened post-escalation
// (human_override=1) are treated as FP candidates. V1.5 will promote this
// to a ground-truth signal (e.g., a subsequent successful auto-retry).
//
// The subquery uses idx_runs_retry_reason_created_at (Migration 004)
// to avoid a full table scan: it seeks the selective retry_reason value,
// then walks the index backwards to satisfy ORDER BY created_at DESC.
// Provisional = (Total < window).
//
// Returns ErrValidation if window <= 0.
func (s *RunStore) AntiProgressFalsePositiveStats(
	ctx context.Context,
	window int,
) (AntiProgressStats, error) {
	if window <= 0 {
		return AntiProgressStats{}, fmt.Errorf("anti-progress stats: window %d must be > 0: %w", window, domain.ErrValidation)
	}

	const q = `
SELECT COUNT(*)                                              AS total,
       SUM(CASE WHEN human_override = 1 THEN 1 ELSE 0 END)   AS overridden
FROM (
    SELECT human_override
    FROM runs
    WHERE retry_reason = 'anti_progress'
    ORDER BY created_at DESC, id DESC
    LIMIT ?
);
`

	var total int
	var overridden sql.NullInt64
	if err := s.db.QueryRowContext(ctx, q, window).Scan(&total, &overridden); err != nil {
		return AntiProgressStats{}, fmt.Errorf("anti-progress stats query: %w", err)
	}

	return AntiProgressStats{
		Total:            total,
		OperatorOverride: int(overridden.Int64),
		Provisional:      total < window,
	}, nil
}

// RecentCompletedRunsForMetrics returns up to window most-recent completed
// runs, ordered by created_at DESC then id DESC for deterministic ties.
func (s *RunStore) RecentCompletedRunsForMetrics(ctx context.Context, window int) ([]RunMetricsRow, error) {
	if window <= 0 {
		return nil, fmt.Errorf("recent completed runs for metrics: window %d must be > 0: %w", window, domain.ErrValidation)
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, status, critic_score, human_override, retry_count, retry_reason, created_at
  FROM runs
 WHERE status = ?
 ORDER BY created_at DESC, id DESC
 LIMIT ?`,
		string(domain.StatusCompleted), window,
	)
	if err != nil {
		return nil, fmt.Errorf("recent completed runs for metrics: %w", err)
	}
	defer rows.Close()

	var out []RunMetricsRow
	for rows.Next() {
		var (
			row           RunMetricsRow
			criticScore   sql.NullFloat64
			humanOverride int
			retryReason   sql.NullString
		)
		if err := rows.Scan(
			&row.ID, &row.Status, &criticScore, &humanOverride,
			&row.RetryCount, &retryReason, &row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("recent completed runs for metrics scan: %w", err)
		}
		row.HumanOverride = humanOverride != 0
		if criticScore.Valid {
			row.CriticScore = &criticScore.Float64
		}
		if retryReason.Valid {
			row.RetryReason = &retryReason.String
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recent completed runs for metrics iterate: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
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
	var characterQueryKey sql.NullString
	var selectedCharacterID sql.NullString
	var frozenDescriptor sql.NullString
	var criticPromptVersion sql.NullString
	var criticPromptHash sql.NullString
	var humanOverride int

	err := s.Scan(
		&r.ID, &r.SCPID, &r.Stage, &r.Status,
		&r.RetryCount, &retryReason,
		&criticScore, &r.CostUSD, &r.TokenIn, &r.TokenOut, &r.DurationMs,
		&humanOverride, &scenarioPath, &characterQueryKey, &selectedCharacterID,
		&frozenDescriptor,
		&criticPromptVersion, &criticPromptHash,
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
	if characterQueryKey.Valid {
		r.CharacterQueryKey = &characterQueryKey.String
	}
	if selectedCharacterID.Valid {
		r.SelectedCharacterID = &selectedCharacterID.String
	}
	if frozenDescriptor.Valid {
		r.FrozenDescriptor = &frozenDescriptor.String
	}
	if criticPromptVersion.Valid {
		r.CriticPromptVersion = &criticPromptVersion.String
	}
	if criticPromptHash.Valid {
		r.CriticPromptHash = &criticPromptHash.String
	}
	return &r, nil
}
