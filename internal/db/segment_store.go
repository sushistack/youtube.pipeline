package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// SegmentStore provides persistence operations for the segments table.
// Satisfies service.SegmentStore and pipeline.SegmentStore structurally.
type SegmentStore struct {
	db *sql.DB
}

// NewSegmentStore creates a SegmentStore backed by the provided *sql.DB.
func NewSegmentStore(db *sql.DB) *SegmentStore {
	return &SegmentStore{db: db}
}

// ListByRunID returns all segments for a run ordered by scene_index ascending.
// Returns (nil, nil) when the run has no segments.
func (s *SegmentStore) ListByRunID(ctx context.Context, runID string) ([]*domain.Episode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, run_id, scene_index, narration, shot_count, shots,
		        tts_path, tts_duration_ms, clip_path,
		        critic_score, critic_sub, status, review_status, safeguard_flags, created_at
		   FROM segments WHERE run_id = ? ORDER BY scene_index ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("list segments for %s: %w", runID, err)
	}
	defer rows.Close()

	var out []*domain.Episode
	for rows.Next() {
		ep, err := scanEpisode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan segment for %s: %w", runID, err)
		}
		out = append(out, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate segments for %s: %w", runID, err)
	}
	return out, nil
}

// ClearClipPathsByRunID sets clip_path = NULL for every segment row of the
// given run. Used by the engine after cleaning `clips/` on an assemble
// resume so that the DB stops pointing at files that were just deleted.
// Scope is strictly limited to the target run.
// Returns the number of rows updated; 0 on an empty run is not an error.
func (s *SegmentStore) ClearClipPathsByRunID(ctx context.Context, runID string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE segments SET clip_path = NULL WHERE run_id = ?`, runID)
	if err != nil {
		return 0, fmt.Errorf("clear clip_paths for %s: %w", runID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("clear clip_paths for %s rows affected: %w", runID, err)
	}
	return n, nil
}

// UpdateClipPath writes the clip_path column for a specific scene.
// Returns ErrNotFound when no segment row matches (runID, sceneIndex).
func (s *SegmentStore) UpdateClipPath(ctx context.Context, runID string, sceneIndex int, clipPath string) error {
	if runID == "" {
		return fmt.Errorf("update clip path: %w: run_id is empty", domain.ErrValidation)
	}
	if sceneIndex < 0 {
		return fmt.Errorf("update clip path %s[%d]: %w: scene_index is negative", runID, sceneIndex, domain.ErrValidation)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE segments SET clip_path = ? WHERE run_id = ? AND scene_index = ?`,
		clipPath, runID, sceneIndex,
	)
	if err != nil {
		return fmt.Errorf("update clip path %s[%d]: %w", runID, sceneIndex, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update clip path %s[%d] rows affected: %w", runID, sceneIndex, err)
	}
	if n == 0 {
		return fmt.Errorf("update clip path %s[%d]: %w", runID, sceneIndex, domain.ErrNotFound)
	}
	return nil
}

// DeleteByRunID removes every segment row whose run_id equals runID.
// Scope is strictly limited to the target run — other runs' segments are untouched.
// Returns the number of rows removed; 0 on an empty run is not an error.
func (s *SegmentStore) DeleteByRunID(ctx context.Context, runID string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM segments WHERE run_id = ?`, runID)
	if err != nil {
		return 0, fmt.Errorf("delete segments for %s: %w", runID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete segments for %s rows affected: %w", runID, err)
	}
	return n, nil
}

// ClearImageArtifactsByRunID clears only the image_path fields embedded inside
// the shots JSON for the run's segments, preserving TTS data and all other
// shot metadata. Returns the number of segment rows updated.
//
// The read + per-scene updates run inside a single transaction so partial
// failure cannot leave some shots cleared while others retain their stale
// image paths.
func (s *SegmentStore) ClearImageArtifactsByRunID(ctx context.Context, runID string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("clear image artifacts for %s: begin tx: %w", runID, err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT scene_index, shots FROM segments WHERE run_id = ? ORDER BY scene_index ASC`, runID)
	if err != nil {
		return 0, fmt.Errorf("clear image artifacts for %s: %w", runID, err)
	}
	defer rows.Close()

	type update struct {
		sceneIndex int
		shotsJSON  string
	}
	updates := make([]update, 0)
	for rows.Next() {
		var (
			sceneIndex int
			shotsRaw   sql.NullString
		)
		if err := rows.Scan(&sceneIndex, &shotsRaw); err != nil {
			return 0, fmt.Errorf("clear image artifacts for %s scan: %w", runID, err)
		}
		if !shotsRaw.Valid || shotsRaw.String == "" {
			continue
		}
		var shots []domain.Shot
		if err := json.Unmarshal([]byte(shotsRaw.String), &shots); err != nil {
			return 0, fmt.Errorf("clear image artifacts for %s decode shots: %w", runID, err)
		}
		changed := false
		for i := range shots {
			if shots[i].ImagePath != "" {
				shots[i].ImagePath = ""
				changed = true
			}
		}
		if !changed {
			continue
		}
		encoded, err := json.Marshal(shots)
		if err != nil {
			return 0, fmt.Errorf("clear image artifacts for %s encode shots: %w", runID, err)
		}
		updates = append(updates, update{sceneIndex: sceneIndex, shotsJSON: string(encoded)})
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("clear image artifacts for %s iterate: %w", runID, err)
	}

	var total int64
	for _, upd := range updates {
		res, err := tx.ExecContext(ctx,
			`UPDATE segments SET shots = ? WHERE run_id = ? AND scene_index = ?`,
			upd.shotsJSON, runID, upd.sceneIndex,
		)
		if err != nil {
			return 0, fmt.Errorf("clear image artifacts for %s update scene %d: %w", runID, upd.sceneIndex, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("clear image artifacts for %s rows affected: %w", runID, err)
		}
		total += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("clear image artifacts for %s: commit: %w", runID, err)
	}
	return total, nil
}

// ClearTTSArtifactsByRunID nulls only the TTS columns for the run's segments,
// preserving image shot metadata and any successful image artifacts.
func (s *SegmentStore) ClearTTSArtifactsByRunID(ctx context.Context, runID string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE segments
		    SET tts_path = NULL,
		        tts_duration_ms = NULL
		  WHERE run_id = ?
		    AND (tts_path IS NOT NULL OR tts_duration_ms IS NOT NULL)`,
		runID,
	)
	if err != nil {
		return 0, fmt.Errorf("clear tts artifacts for %s: %w", runID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("clear tts artifacts for %s rows affected: %w", runID, err)
	}
	return n, nil
}

// UpsertImageShots inserts or replaces the segments row for (runID, sceneIndex)
// with a refreshed `shots` JSON payload and `shot_count`. Used by Phase B's
// image track to persist per-shot `image_path`, `duration_s`, `transition`,
// and `visual_descriptor` after clean-slate regeneration. Preserves TTS and
// clip fields when the row already exists; creates a fresh row otherwise.
//
// Phase B resume semantics are clean-slate: callers are expected to have
// already deleted stale segments (or cleared image fields) before invoking
// this method. The upsert shape guards against partial-run orphans by
// letting a second call produce identical state.
func (s *SegmentStore) UpsertImageShots(
	ctx context.Context,
	runID string,
	sceneIndex int,
	shots []domain.Shot,
) error {
	if runID == "" {
		return fmt.Errorf("upsert image shots: %w: run_id is empty", domain.ErrValidation)
	}
	if sceneIndex < 0 {
		return fmt.Errorf("upsert image shots %s[%d]: %w: scene_index is negative", runID, sceneIndex, domain.ErrValidation)
	}
	if shots == nil {
		shots = []domain.Shot{}
	}
	raw, err := json.Marshal(shots)
	if err != nil {
		return fmt.Errorf("upsert image shots %s[%d]: encode shots: %w", runID, sceneIndex, err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO segments (run_id, scene_index, shot_count, shots, status)
		 VALUES (?, ?, ?, ?, 'pending')
		 ON CONFLICT(run_id, scene_index) DO UPDATE SET
		     shot_count = excluded.shot_count,
		     shots      = excluded.shots`,
		runID, sceneIndex, len(shots), string(raw),
	)
	if err != nil {
		return fmt.Errorf("upsert image shots %s[%d]: %w", runID, sceneIndex, err)
	}
	return nil
}

// UpsertTTSArtifact inserts or updates the TTS columns for a segment row.
// On insert (when no row exists for run_id+scene_index), a minimal row is
// created with status='pending' and the TTS fields populated.
// On conflict, only tts_path and tts_duration_ms are updated; image shots,
// narration, clip_path, critic, review_status, and safeguard_flags are
// preserved unchanged. This asymmetry is intentional — mixing TTS + image
// updates in a single method would invite regressions across the two tracks.
func (s *SegmentStore) UpsertTTSArtifact(
	ctx context.Context,
	runID string,
	sceneIndex int,
	ttsPath string,
	ttsDurationMs int64,
) error {
	if runID == "" {
		return fmt.Errorf("upsert tts artifact: %w: run_id is empty", domain.ErrValidation)
	}
	if sceneIndex < 0 {
		return fmt.Errorf("upsert tts artifact %s[%d]: %w: scene_index is negative", runID, sceneIndex, domain.ErrValidation)
	}
	if ttsPath == "" {
		return fmt.Errorf("upsert tts artifact %s[%d]: %w: tts_path is empty", runID, sceneIndex, domain.ErrValidation)
	}
	if ttsDurationMs < 0 {
		return fmt.Errorf("upsert tts artifact %s[%d]: %w: tts_duration_ms is negative", runID, sceneIndex, domain.ErrValidation)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO segments (run_id, scene_index, tts_path, tts_duration_ms, status)
		 VALUES (?, ?, ?, ?, 'pending')
		 ON CONFLICT(run_id, scene_index) DO UPDATE SET
		     tts_path        = excluded.tts_path,
		     tts_duration_ms = excluded.tts_duration_ms`,
		runID, sceneIndex, ttsPath, ttsDurationMs,
	)
	if err != nil {
		return fmt.Errorf("upsert tts artifact %s[%d]: %w", runID, sceneIndex, err)
	}
	return nil
}

// SeedFromNarration bulk-inserts a segments row per narration scene so that
// scenario_review (and the per-scene EditNarration UPDATE path) have a
// concrete row to read/update. Phase B's UpsertImageShots and
// UpsertTTSArtifact already use ON CONFLICT(run_id, scene_index) DO UPDATE,
// so seeding here does not collide with their later writes.
//
// scene_index is 0-based (matches segments schema and FE expectations).
// Existing rows are left untouched (ON CONFLICT DO NOTHING) so retries are
// idempotent: re-running Phase A after a critic retry will not wipe edited
// narration text from a previously-seeded row.
//
// Empty scenes is a no-op (returns 0 rows inserted).
func (s *SegmentStore) SeedFromNarration(ctx context.Context, runID string, scenes []domain.NarrationScene) (int64, error) {
	if runID == "" {
		return 0, fmt.Errorf("seed segments: %w: run_id is empty", domain.ErrValidation)
	}
	if len(scenes) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("seed segments for %s: begin tx: %w", runID, err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO segments (run_id, scene_index, narration, shot_count, status)
		 VALUES (?, ?, ?, 1, 'pending')
		 ON CONFLICT(run_id, scene_index) DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("seed segments for %s: prepare: %w", runID, err)
	}
	defer stmt.Close()

	var inserted int64
	for i, scene := range scenes {
		// Use the iteration index as scene_index (0-based); the narration
		// schema's scene_num is 1-based and order-preserving, so the i-th
		// scene maps to scene_index=i regardless of any gaps in scene_num.
		res, err := stmt.ExecContext(ctx, runID, i, scene.Narration)
		if err != nil {
			return inserted, fmt.Errorf("seed segments %s[%d]: %w", runID, i, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return inserted, fmt.Errorf("seed segments %s[%d] rows affected: %w", runID, i, err)
		}
		inserted += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("seed segments for %s: commit: %w", runID, err)
	}
	return inserted, nil
}

// UpdateNarration replaces the narration text for a specific scene. Returns
// ErrNotFound when no row exists for (runID, sceneIndex).
func (s *SegmentStore) UpdateNarration(ctx context.Context, runID string, sceneIndex int, narration string) error {
	if runID == "" {
		return fmt.Errorf("update narration: %w: run_id is empty", domain.ErrValidation)
	}
	if sceneIndex < 0 {
		return fmt.Errorf("update narration %s[%d]: %w: scene_index is negative", runID, sceneIndex, domain.ErrValidation)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE segments SET narration = ? WHERE run_id = ? AND scene_index = ?`,
		narration, runID, sceneIndex,
	)
	if err != nil {
		return fmt.Errorf("update narration %s[%d]: %w", runID, sceneIndex, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update narration %s[%d] rows affected: %w", runID, sceneIndex, err)
	}
	if n == 0 {
		return fmt.Errorf("update narration %s[%d]: %w", runID, sceneIndex, domain.ErrNotFound)
	}
	return nil
}

func (s *SegmentStore) GetByRunIDAndSceneIndex(ctx context.Context, runID string, sceneIndex int) (*domain.Episode, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, run_id, scene_index, narration, shot_count, shots,
		        tts_path, tts_duration_ms, clip_path,
		        critic_score, critic_sub, status, review_status, safeguard_flags, created_at
		   FROM segments
		  WHERE run_id = ? AND scene_index = ?`,
		runID, sceneIndex,
	)
	ep, err := scanEpisode(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("get segment %s[%d]: %w", runID, sceneIndex, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get segment %s[%d]: %w", runID, sceneIndex, err)
	}
	return ep, nil
}

func (s *SegmentStore) UpdateReviewGate(
	ctx context.Context,
	runID string,
	sceneIndex int,
	reviewStatus domain.ReviewStatus,
	safeguardFlags []string,
) error {
	if !reviewStatus.IsValid() {
		return fmt.Errorf("update review gate %s[%d]: invalid review status %q: %w", runID, sceneIndex, reviewStatus, domain.ErrValidation)
	}
	flagsJSON, err := json.Marshal(normalizeSafeguardFlags(safeguardFlags))
	if err != nil {
		return fmt.Errorf("update review gate %s[%d]: marshal safeguard flags: %w", runID, sceneIndex, err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE segments
		    SET review_status = ?, safeguard_flags = ?
		  WHERE run_id = ? AND scene_index = ?`,
		string(reviewStatus), string(flagsJSON), runID, sceneIndex,
	)
	if err != nil {
		return fmt.Errorf("update review gate %s[%d]: %w", runID, sceneIndex, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update review gate %s[%d] rows affected: %w", runID, sceneIndex, err)
	}
	if n == 0 {
		return fmt.Errorf("update review gate %s[%d]: %w", runID, sceneIndex, domain.ErrNotFound)
	}
	return nil
}

func scanEpisode(sc scanner) (*domain.Episode, error) {
	var ep domain.Episode
	var (
		narration     sql.NullString
		shotsJSON     sql.NullString
		ttsPath       sql.NullString
		ttsDurationMs sql.NullInt64
		clipPath      sql.NullString
		criticScore   sql.NullFloat64
		criticSub     sql.NullString
		reviewStatus  sql.NullString
		safeguardJSON sql.NullString
	)
	if err := sc.Scan(
		&ep.ID, &ep.RunID, &ep.SceneIndex, &narration, &ep.ShotCount, &shotsJSON,
		&ttsPath, &ttsDurationMs, &clipPath,
		&criticScore, &criticSub, &ep.Status, &reviewStatus, &safeguardJSON, &ep.CreatedAt,
	); err != nil {
		return nil, err
	}
	if narration.Valid {
		ep.Narration = &narration.String
	}
	if shotsJSON.Valid && shotsJSON.String != "" {
		if err := json.Unmarshal([]byte(shotsJSON.String), &ep.Shots); err != nil {
			return nil, fmt.Errorf("decode shots JSON: %w", err)
		}
	}
	if ttsPath.Valid {
		ep.TTSPath = &ttsPath.String
	}
	if ttsDurationMs.Valid {
		v := int(ttsDurationMs.Int64)
		ep.TTSDurationMs = &v
	}
	if clipPath.Valid {
		ep.ClipPath = &clipPath.String
	}
	if criticScore.Valid {
		ep.CriticScore = &criticScore.Float64
	}
	if criticSub.Valid {
		ep.CriticSub = &criticSub.String
	}
	if reviewStatus.Valid && reviewStatus.String != "" {
		ep.ReviewStatus = domain.ReviewStatus(reviewStatus.String)
	} else {
		ep.ReviewStatus = domain.ReviewStatusPending
	}
	if safeguardJSON.Valid && safeguardJSON.String != "" {
		if err := json.Unmarshal([]byte(safeguardJSON.String), &ep.SafeguardFlags); err != nil {
			return nil, fmt.Errorf("decode safeguard_flags JSON: %w", err)
		}
	}
	return &ep, nil
}

func normalizeSafeguardFlags(flags []string) []string {
	if len(flags) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(flags))
	out := make([]string, 0, len(flags))
	for _, flag := range flags {
		if flag == "" {
			continue
		}
		if _, ok := seen[flag]; ok {
			continue
		}
		seen[flag] = struct{}{}
		out = append(out, flag)
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}
