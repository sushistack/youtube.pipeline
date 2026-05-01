package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// PreRewindCancel parks the run in status='cancelled' so subsequent cleanup
// runs against a known-quiet target. Workers that observe the cancel via
// their cancellation context exit cleanly; any racing UPDATE that completes
// after this point is overwritten by ApplyRewindReset's final stage write.
//
// Idempotent on already-cancelled runs (the WHERE keeps it a true no-op
// when nothing changes). Returns ErrNotFound when the run does not exist.
// Unlike Cancel, this method accepts ANY current status — rewind is
// authorized for completed/failed/cancelled rows as well.
func (s *RunStore) PreRewindCancel(ctx context.Context, runID string) error {
	if runID == "" {
		return fmt.Errorf("pre-rewind cancel: %w: run_id is empty", domain.ErrValidation)
	}
	if _, err := s.Get(ctx, runID); err != nil {
		return fmt.Errorf("pre-rewind cancel %s: %w", runID, err)
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE runs SET status = ? WHERE id = ? AND status != ?`,
		string(domain.StatusCancelled), runID, string(domain.StatusCancelled),
	); err != nil {
		return fmt.Errorf("pre-rewind cancel %s: %w", runID, err)
	}
	return nil
}

// ApplyRewindReset performs the full database cleanup + final stage reset
// for a rewind. Caller MUST have already (1) called PreRewindCancel and
// (2) drained any in-flight workers via the engine's CancelRegistry.
//
// Order within this method (each step assumes status='cancelled'):
//  1. DELETE FROM hitl_sessions — invariant safe: status=cancelled means
//     no row should exist anyway, so the DELETE only removes a stale row.
//  2. DELETE FROM decisions  — bucket-aware deletion using
//     params.DecisionTypesToDelete. No deletion when the slice is empty.
//  3. Per-flag segment cleanup. DeleteSegments wins over Clear* if both
//     happen to be set.
//  4. UPDATE runs — single statement: stage, status, retry_count++,
//     retry_reason=NULL, plus per-flag column nulling. retry_count
//     increments so the run history shows that a fresh attempt began.
//
// Returns ErrNotFound when the run does not exist. Returns ErrValidation
// when params.FinalStage / params.FinalStatus is empty.
func (s *RunStore) ApplyRewindReset(ctx context.Context, runID string, params domain.RewindResetParams) error {
	if runID == "" {
		return fmt.Errorf("apply rewind reset: %w: run_id is empty", domain.ErrValidation)
	}
	if params.FinalStage == "" || params.FinalStatus == "" {
		return fmt.Errorf("apply rewind reset %s: %w: final stage/status missing", runID, domain.ErrValidation)
	}

	// Step 1 — clear HITL session row unconditionally.
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM hitl_sessions WHERE run_id = ?`, runID,
	); err != nil {
		return fmt.Errorf("apply rewind reset %s: delete hitl_sessions: %w", runID, err)
	}

	// Step 2 — selectively delete decision rows by bucket.
	if len(params.DecisionTypesToDelete) > 0 {
		placeholders := make([]string, len(params.DecisionTypesToDelete))
		args := make([]any, 0, len(params.DecisionTypesToDelete)+1)
		args = append(args, runID)
		for i, t := range params.DecisionTypesToDelete {
			placeholders[i] = "?"
			args = append(args, t)
		}
		query := fmt.Sprintf(
			`DELETE FROM decisions WHERE run_id = ? AND decision_type IN (%s)`,
			strings.Join(placeholders, ","),
		)
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("apply rewind reset %s: delete decisions: %w", runID, err)
		}
	}

	// Step 3 — segment-row effects. DeleteSegments wins (it's the strict
	// superset cleanup); Clear* fields are skipped in that branch.
	if params.DeleteSegments {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM segments WHERE run_id = ?`, runID,
		); err != nil {
			return fmt.Errorf("apply rewind reset %s: delete segments: %w", runID, err)
		}
	} else {
		if params.ClearImageArtifacts {
			if err := s.clearImageArtifactsForRun(ctx, runID); err != nil {
				return fmt.Errorf("apply rewind reset %s: clear image artifacts: %w", runID, err)
			}
		}
		if params.ClearTTSArtifacts {
			if _, err := s.db.ExecContext(ctx,
				`UPDATE segments SET tts_path = NULL, tts_duration_ms = NULL WHERE run_id = ?`,
				runID,
			); err != nil {
				return fmt.Errorf("apply rewind reset %s: clear tts artifacts: %w", runID, err)
			}
		}
		if params.ClearClipPaths {
			if _, err := s.db.ExecContext(ctx,
				`UPDATE segments SET clip_path = NULL WHERE run_id = ?`, runID,
			); err != nil {
				return fmt.Errorf("apply rewind reset %s: clear clip_paths: %w", runID, err)
			}
		}
	}

	// Step 4 — single UPDATE on runs row. Build the SET dynamically so
	// preserved columns stay untouched (their old values remain valid).
	setClauses := []string{
		"stage = ?",
		"status = ?",
		"retry_reason = NULL",
		"retry_count = retry_count + 1",
	}
	args := []any{string(params.FinalStage), string(params.FinalStatus)}
	if params.ClearScenarioPath {
		setClauses = append(setClauses, "scenario_path = NULL")
	}
	if params.ClearCharacterPick {
		setClauses = append(setClauses,
			"selected_character_id = NULL",
			"frozen_descriptor = NULL",
			"character_query_key = NULL",
		)
	}
	if params.ClearOutputPath {
		setClauses = append(setClauses, "output_path = NULL")
	}
	if params.ClearCriticScore {
		setClauses = append(setClauses, "critic_score = NULL")
	}
	args = append(args, runID)

	query := fmt.Sprintf(
		`UPDATE runs SET %s WHERE id = ?`,
		strings.Join(setClauses, ", "),
	)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("apply rewind reset %s: update runs: %w", runID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("apply rewind reset %s: rows affected: %w", runID, err)
	}
	if n == 0 {
		return fmt.Errorf("apply rewind reset %s: %w", runID, domain.ErrNotFound)
	}
	return nil
}

// clearImageArtifactsForRun rewrites every segments.shots JSON for the run
// so that each shot's image_path is "". Mirrors
// SegmentStore.ClearImageArtifactsByRunID without the cross-package round-trip
// — keeping the rewind path entirely on the RunStore connection inside one
// serialized window.
func (s *RunStore) clearImageArtifactsForRun(ctx context.Context, runID string) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT scene_index, shots FROM segments WHERE run_id = ? ORDER BY scene_index ASC`,
		runID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type pending struct {
		idx       int
		newJSON   string
		hadChange bool
	}
	var updates []pending
	for rows.Next() {
		var (
			idx int
			raw sql.NullString
		)
		if err := rows.Scan(&idx, &raw); err != nil {
			return err
		}
		if !raw.Valid || raw.String == "" {
			continue
		}
		rewritten, changed, err := rewriteImagePathsToEmpty(raw.String)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		updates = append(updates, pending{idx: idx, newJSON: rewritten, hadChange: true})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, u := range updates {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE segments SET shots = ? WHERE run_id = ? AND scene_index = ?`,
			u.newJSON, runID, u.idx,
		); err != nil {
			return err
		}
	}
	return nil
}

// rewriteImagePathsToEmpty parses a shots JSON array and sets every shot's
// image_path to "". Returns (newJSON, didChange, err). A no-op input
// (already empty paths or zero shots) returns didChange=false and the
// original string unchanged.
func rewriteImagePathsToEmpty(raw string) (string, bool, error) {
	var shots []domain.Shot
	if err := json.Unmarshal([]byte(raw), &shots); err != nil {
		return "", false, fmt.Errorf("decode shots: %w", err)
	}
	changed := false
	for i := range shots {
		if shots[i].ImagePath != "" {
			shots[i].ImagePath = ""
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(shots)
	if err != nil {
		return "", false, fmt.Errorf("encode shots: %w", err)
	}
	return string(out), true, nil
}
