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
