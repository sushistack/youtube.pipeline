package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

type SettingsStore struct {
	db *sql.DB
}

// SettingsVersionRow carries a persisted non-secret config snapshot plus a
// fingerprint of the secret map at the time of persistence. The raw secret
// values are intentionally NOT stored; callers that need effective .env values
// read the on-disk .env file (which is the single source of truth for secrets).
type SettingsVersionRow struct {
	Version         int64
	Config          domain.PipelineConfig
	EnvFingerprint  string
}

type SettingsStateRow struct {
	EffectiveVersion *int64
	PendingVersion   *int64
	QueuedAt         *string
	UpdatedAt        string
}

func NewSettingsStore(db *sql.DB) *SettingsStore {
	return &SettingsStore{db: db}
}

func (s *SettingsStore) LoadState(ctx context.Context) (SettingsStateRow, error) {
	return loadStateFromQuerier(ctx, s.db)
}

func (s *SettingsStore) LoadVersion(ctx context.Context, version int64) (SettingsVersionRow, error) {
	var configJSON, fingerprint string
	err := s.db.QueryRowContext(ctx, `
SELECT config_json, env_fingerprint
  FROM settings_versions
 WHERE version = ?`,
		version,
	).Scan(&configJSON, &fingerprint)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SettingsVersionRow{}, fmt.Errorf("load settings version %d: %w", version, domain.ErrNotFound)
		}
		return SettingsVersionRow{}, fmt.Errorf("load settings version %d: %w", version, err)
	}
	cfg, err := decodeSettingsConfig(configJSON)
	if err != nil {
		return SettingsVersionRow{}, fmt.Errorf("load settings version %d: %w", version, err)
	}
	return SettingsVersionRow{Version: version, Config: cfg, EnvFingerprint: fingerprint}, nil
}

// SaveSnapshot inserts a new non-secret config version and updates the
// effective-or-pending pointer in settings_state in a single transaction. The
// caller supplies the env map only so we can compute the fingerprint — the
// raw values are not persisted.
func (s *SettingsStore) SaveSnapshot(
	ctx context.Context,
	files domain.SettingsFileSnapshot,
	queued bool,
	queuedAt *string,
) (SettingsStateRow, int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("save settings snapshot: begin tx: %w", err)
	}
	defer tx.Rollback()

	configJSON, err := encodeSettingsConfig(files.Config)
	if err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("save settings snapshot: %w", err)
	}
	fingerprint := envFingerprint(files.Env)

	res, err := tx.ExecContext(ctx, `
INSERT INTO settings_versions (config_json, env_fingerprint)
VALUES (?, ?)`,
		configJSON, fingerprint,
	)
	if err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("save settings snapshot: insert version: %w", err)
	}
	version, err := res.LastInsertId()
	if err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("save settings snapshot: version id: %w", err)
	}

	if queued {
		_, err = tx.ExecContext(ctx, `
UPDATE settings_state
   SET pending_version = ?,
       queued_at = ?,
       updated_at = datetime('now')
 WHERE id = 1`,
			version, queuedAt,
		)
	} else {
		_, err = tx.ExecContext(ctx, `
UPDATE settings_state
   SET effective_version = ?,
       pending_version = NULL,
       queued_at = NULL,
       updated_at = datetime('now')
 WHERE id = 1`,
			version,
		)
	}
	if err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("save settings snapshot: update state: %w", err)
	}

	// Read the post-update state inside the same transaction so the returned
	// row reflects this caller's write, not a subsequent concurrent write.
	state, err := loadStateFromQuerier(ctx, tx)
	if err != nil {
		return SettingsStateRow{}, 0, err
	}

	if err := tx.Commit(); err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("save settings snapshot: commit: %w", err)
	}
	return state, version, nil
}

// PromotePending moves pending_version into effective_version when one is
// queued. Returns (state, promoted, err) where promoted=false means there
// was no pending version (noop).
func (s *SettingsStore) PromotePending(ctx context.Context) (SettingsStateRow, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SettingsStateRow{}, false, fmt.Errorf("promote pending settings: begin tx: %w", err)
	}
	defer tx.Rollback()

	var pending sql.NullInt64
	err = tx.QueryRowContext(ctx, `
SELECT pending_version
  FROM settings_state
 WHERE id = 1`,
	).Scan(&pending)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := tx.Commit(); err != nil {
				return SettingsStateRow{}, false, fmt.Errorf("promote pending settings: commit noop: %w", err)
			}
			return SettingsStateRow{}, false, nil
		}
		return SettingsStateRow{}, false, fmt.Errorf("promote pending settings: load state: %w", err)
	}
	if !pending.Valid {
		state, err := loadStateFromQuerier(ctx, tx)
		if err != nil {
			return SettingsStateRow{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return SettingsStateRow{}, false, fmt.Errorf("promote pending settings: commit noop: %w", err)
		}
		return state, false, nil
	}

	_, err = tx.ExecContext(ctx, `
UPDATE settings_state
   SET effective_version = pending_version,
       pending_version = NULL,
       queued_at = NULL,
       updated_at = datetime('now')
 WHERE id = 1`)
	if err != nil {
		return SettingsStateRow{}, false, fmt.Errorf("promote pending settings: update state: %w", err)
	}
	state, err := loadStateFromQuerier(ctx, tx)
	if err != nil {
		return SettingsStateRow{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return SettingsStateRow{}, false, fmt.Errorf("promote pending settings: commit: %w", err)
	}
	return state, true, nil
}

// EnsureEffectiveVersion seeds settings_state.effective_version from an initial
// disk-backed snapshot if (and only if) no version has ever been recorded.
// This is the bootstrap that makes LoadEffectiveVersion deterministic on a
// freshly migrated database — callers no longer need to fall back to raw disk
// reads on first boot.
func (s *SettingsStore) EnsureEffectiveVersion(
	ctx context.Context,
	files domain.SettingsFileSnapshot,
) (SettingsStateRow, int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("ensure effective version: begin tx: %w", err)
	}
	defer tx.Rollback()

	var effective sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT effective_version FROM settings_state WHERE id = 1`).Scan(&effective)
	if err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("ensure effective version: load state: %w", err)
	}
	if effective.Valid {
		state, err := loadStateFromQuerier(ctx, tx)
		if err != nil {
			return SettingsStateRow{}, 0, err
		}
		if err := tx.Commit(); err != nil {
			return SettingsStateRow{}, 0, fmt.Errorf("ensure effective version: commit: %w", err)
		}
		return state, effective.Int64, nil
	}

	configJSON, err := encodeSettingsConfig(files.Config)
	if err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("ensure effective version: %w", err)
	}
	fingerprint := envFingerprint(files.Env)
	res, err := tx.ExecContext(ctx, `
INSERT INTO settings_versions (config_json, env_fingerprint)
VALUES (?, ?)`, configJSON, fingerprint)
	if err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("ensure effective version: insert: %w", err)
	}
	version, err := res.LastInsertId()
	if err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("ensure effective version: version id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE settings_state
   SET effective_version = ?,
       pending_version = NULL,
       queued_at = NULL,
       updated_at = datetime('now')
 WHERE id = 1`, version); err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("ensure effective version: update: %w", err)
	}
	state, err := loadStateFromQuerier(ctx, tx)
	if err != nil {
		return SettingsStateRow{}, 0, err
	}
	if err := tx.Commit(); err != nil {
		return SettingsStateRow{}, 0, fmt.Errorf("ensure effective version: commit: %w", err)
	}
	return state, version, nil
}

// AssignRunToVersion pins a run to a settings version at run creation. This is
// read by LoadVersionForRun during per-run effective-config resolution so that
// a run already in flight keeps its pinned version even when a newer version
// is promoted concurrently.
func (s *SettingsStore) AssignRunToVersion(ctx context.Context, runID string, version int64) error {
	if runID == "" {
		return fmt.Errorf("assign run to version: empty run_id")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO run_settings_assignments (run_id, settings_version)
VALUES (?, ?)
ON CONFLICT(run_id) DO NOTHING`, runID, version)
	if err != nil {
		return fmt.Errorf("assign run to version: %w", err)
	}
	return nil
}

// LoadVersionForRun returns the settings version pinned to a run. If the run
// has no assignment yet (pre-migration runs, or a run that failed to be
// pinned), the current effective_version is returned as a fallback. Returns
// (0, ErrNotFound) when neither is available.
func (s *SettingsStore) LoadVersionForRun(ctx context.Context, runID string) (int64, error) {
	var version sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT settings_version
  FROM run_settings_assignments
 WHERE run_id = ?`, runID).Scan(&version)
	if err == nil && version.Valid {
		return version.Int64, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("load version for run %s: %w", runID, err)
	}

	state, err := s.LoadState(ctx)
	if err != nil {
		return 0, err
	}
	if state.EffectiveVersion == nil {
		return 0, fmt.Errorf("load version for run %s: %w", runID, domain.ErrNotFound)
	}
	return *state.EffectiveVersion, nil
}

func (s *SettingsStore) ActiveRunsExist(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
  FROM runs
 WHERE status IN (?, ?)`,
		string(domain.StatusRunning),
		string(domain.StatusWaiting),
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("active runs exist: %w", err)
	}
	return count > 0, nil
}

// BudgetSourceRun selects the single run whose cost the budget indicator
// should visualize. Prefers running > waiting (real active spend) over
// failed runs, which are only shown when no active run exists. When multiple
// rows in the preferred bucket exist, the most recently updated wins.
func (s *SettingsStore) BudgetSourceRun(ctx context.Context) (*domain.SettingsBudgetRun, error) {
	// Preferred: truly-active runs ordered by freshness.
	row := s.db.QueryRowContext(ctx, `
SELECT id, status, cost_usd
  FROM runs
 WHERE status IN (?, ?)
 ORDER BY updated_at DESC, id DESC
 LIMIT 1`,
		string(domain.StatusRunning),
		string(domain.StatusWaiting),
	)
	var out domain.SettingsBudgetRun
	err := row.Scan(&out.ID, &out.Status, &out.CostUSD)
	if err == nil {
		return &out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("budget source active run: %w", err)
	}

	// Fallback tier 1: failed runs (resumable spend).
	row = s.db.QueryRowContext(ctx, `
SELECT id, status, cost_usd
  FROM runs
 WHERE status = ?
 ORDER BY updated_at DESC, id DESC
 LIMIT 1`,
		string(domain.StatusFailed),
	)
	err = row.Scan(&out.ID, &out.Status, &out.CostUSD)
	if err == nil {
		return &out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("budget source failed run: %w", err)
	}

	// Fallback tier 2: most recently updated run regardless of status.
	row = s.db.QueryRowContext(ctx, `
SELECT id, status, cost_usd
  FROM runs
 ORDER BY updated_at DESC, id DESC
 LIMIT 1`)
	err = row.Scan(&out.ID, &out.Status, &out.CostUSD)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("budget source latest run: %w", err)
	}
	return &out, nil
}

// querier is any handle that can run a QueryRowContext — both *sql.DB and
// *sql.Tx satisfy it. LoadState uses this to read state either standalone
// or inside a SaveSnapshot/PromotePending transaction.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func loadStateFromQuerier(ctx context.Context, q querier) (SettingsStateRow, error) {
	var (
		effective sql.NullInt64
		pending   sql.NullInt64
		queuedAt  sql.NullString
		updatedAt sql.NullString
	)
	err := q.QueryRowContext(ctx, `
SELECT effective_version, pending_version, queued_at, updated_at
  FROM settings_state
 WHERE id = 1`,
	).Scan(&effective, &pending, &queuedAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// settings_state is seeded by migration 012; ErrNoRows here
			// means the sentinel row was deleted out-of-band. Return a
			// zero-valued state so callers can still render a "no settings
			// yet" surface rather than 500.
			return SettingsStateRow{}, nil
		}
		return SettingsStateRow{}, fmt.Errorf("load settings state: %w", err)
	}
	state := SettingsStateRow{}
	if effective.Valid {
		state.EffectiveVersion = &effective.Int64
	}
	if pending.Valid {
		state.PendingVersion = &pending.Int64
	}
	if queuedAt.Valid {
		state.QueuedAt = &queuedAt.String
	}
	if updatedAt.Valid {
		state.UpdatedAt = updatedAt.String
	}
	return state, nil
}

func encodeSettingsConfig(cfg domain.PipelineConfig) (string, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("encode config snapshot: %w", err)
	}
	return string(data), nil
}

func decodeSettingsConfig(raw string) (domain.PipelineConfig, error) {
	cfg := domain.PipelineConfig{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return domain.PipelineConfig{}, fmt.Errorf("decode config snapshot: %w", err)
	}
	return cfg, nil
}

// envFingerprint returns a deterministic SHA-256 hex digest over the supported
// secret keys' values. Order-stable so equivalent env maps produce identical
// fingerprints. This is the ONLY secret-derived byte we persist — the raw
// values remain only in .env.
func envFingerprint(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0x1f}) // unit separator — prevents key/value boundary collision
		h.Write([]byte(env[k]))
		h.Write([]byte{0x1e}) // record separator
	}
	return hex.EncodeToString(h.Sum(nil))
}
