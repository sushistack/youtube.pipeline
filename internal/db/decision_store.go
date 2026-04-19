package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// DecisionStore provides read access to the decisions table and CRUD for
// hitl_sessions. Satisfies pipeline.HITLSessionStore and service.DecisionReader
// structurally.
type DecisionStore struct {
	db *sql.DB
}

// NewDecisionStore constructs a DecisionStore backed by the provided *sql.DB.
func NewDecisionStore(db *sql.DB) *DecisionStore {
	return &DecisionStore{db: db}
}

// DecisionCounts is the lightweight triplet returned by DecisionCountsByRunID.
// Pending is computed by the caller (TotalScenes - Approved - Rejected).
type DecisionCounts struct {
	Approved    int
	Rejected    int
	TotalScenes int
}

type TimelineListOptions struct {
	DecisionType    *string
	Limit           int
	BeforeCreatedAt *string
	BeforeID        *int64
}

type TimelineCursor struct {
	BeforeCreatedAt string
	BeforeID        int64
}

type TimelineDecision struct {
	ID              int64
	RunID           string
	SCPID           string
	SceneID         *string
	DecisionType    string
	ContextSnapshot *string
	Note            *string
	SupersededBy    *int64
	CreatedAt       string
}

type AutoApprovalInput struct {
	SceneIndex   int
	CriticScore  float64
	Threshold    float64
	ReviewStatus domain.ReviewStatus
}

type SceneReviewUpdate struct {
	SceneIndex     int
	ReviewStatus   domain.ReviewStatus
	SafeguardFlags []string
	AutoApproved   bool
}

const BatchApproveChunkSize = 50

type BatchApproveResult struct {
	AggregateCommandID string
	ApprovedCount      int
	ApprovedSceneIDs   []int
	FocusSceneIndex    int
}

// KappaPair is one run-level observation for Cohen's kappa.
type KappaPair struct {
	CriticPass      bool
	OperatorApprove bool
}

// DefectEscape captures the aggregate defect-escape tallies for a run window.
type DefectEscape struct {
	AutoPassedScenes int
	EscapedScenes    int
}

// ListByRunID returns all non-superseded decisions for a run ordered by
// created_at ascending. superseded_by IS NOT NULL rows are excluded (those
// are undone decisions per the V1 undo model). Returns (nil, nil) for a
// run with no decisions.
func (s *DecisionStore) ListByRunID(ctx context.Context, runID string) ([]*domain.Decision, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, run_id, scene_id, decision_type, context_snapshot, outcome_link,
		        tags, feedback_source, external_ref, feedback_at, superseded_by,
		        note, created_at
		   FROM decisions
		  WHERE run_id = ? AND superseded_by IS NULL
		  ORDER BY created_at ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("list decisions for %s: %w", runID, err)
	}
	defer rows.Close()

	var out []*domain.Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("scan decision for %s: %w", runID, err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate decisions for %s: %w", runID, err)
	}
	return out, nil
}

// ListTimeline returns a cross-run decisions history page ordered newest-first.
// Superseded rows are intentionally included because the timeline is an audit
// surface, not a live-state projection.
func (s *DecisionStore) ListTimeline(
	ctx context.Context,
	opts TimelineListOptions,
) ([]*TimelineDecision, *TimelineCursor, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	args := make([]any, 0, 5)
	query := strings.Builder{}
	query.WriteString(`
SELECT d.id, d.run_id, r.scp_id, d.scene_id, d.decision_type,
       d.context_snapshot, d.note, d.superseded_by, d.created_at
  FROM decisions d
  JOIN runs r ON r.id = d.run_id
 WHERE 1 = 1`)

	if opts.DecisionType != nil && strings.TrimSpace(*opts.DecisionType) != "" {
		query.WriteString(` AND d.decision_type = ?`)
		args = append(args, *opts.DecisionType)
	}
	if opts.BeforeCreatedAt != nil && opts.BeforeID != nil {
		query.WriteString(` AND (d.created_at < ? OR (d.created_at = ? AND d.id < ?))`)
		args = append(args, *opts.BeforeCreatedAt, *opts.BeforeCreatedAt, *opts.BeforeID)
	}

	query.WriteString(`
 ORDER BY d.created_at DESC, d.id DESC
 LIMIT ?`)
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list decisions timeline: %w", err)
	}
	defer rows.Close()

	out := make([]*TimelineDecision, 0, limit)
	var nextCursor *TimelineCursor
	for rows.Next() {
		decision, err := scanTimelineDecision(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("scan decisions timeline row: %w", err)
		}
		if len(out) == limit {
			last := out[len(out)-1]
			nextCursor = &TimelineCursor{
				BeforeCreatedAt: last.CreatedAt,
				BeforeID:        last.ID,
			}
			break
		}
		out = append(out, decision)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate decisions timeline: %w", err)
	}
	return out, nextCursor, nil
}

// GetSession returns the current HITL pause state for a run, or
// (nil, nil) if no row exists. Does NOT return ErrNotFound — the caller
// distinguishes "paused" vs "not paused" via the nil pointer.
func (s *DecisionStore) GetSession(ctx context.Context, runID string) (*domain.HITLSession, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT run_id, stage, scene_index, last_interaction_timestamp,
		        snapshot_json, created_at, updated_at
		   FROM hitl_sessions WHERE run_id = ?`, runID)

	var sess domain.HITLSession
	err := row.Scan(
		&sess.RunID, &sess.Stage, &sess.SceneIndex, &sess.LastInteractionTimestamp,
		&sess.SnapshotJSON, &sess.CreatedAt, &sess.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get hitl_session for %s: %w", runID, err)
	}
	return &sess, nil
}

// UpsertSession writes the pause state for a run via an atomic
// INSERT ... ON CONFLICT (run_id) DO UPDATE. snapshot_json is already
// JSON-encoded by the caller (domain layer owns the encoding format).
func (s *DecisionStore) UpsertSession(ctx context.Context, session *domain.HITLSession) error {
	if session == nil {
		return fmt.Errorf("upsert hitl_session: %w: nil session", domain.ErrValidation)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO hitl_sessions
		    (run_id, stage, scene_index, last_interaction_timestamp, snapshot_json)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(run_id) DO UPDATE SET
		    stage = excluded.stage,
		    scene_index = excluded.scene_index,
		    last_interaction_timestamp = excluded.last_interaction_timestamp,
		    snapshot_json = excluded.snapshot_json`,
		session.RunID, string(session.Stage), session.SceneIndex,
		session.LastInteractionTimestamp, session.SnapshotJSON,
	)
	if err != nil {
		return fmt.Errorf("upsert hitl_session for %s: %w", session.RunID, err)
	}
	return nil
}

// DeleteSession removes the hitl_sessions row for a run, or no-ops if no
// row exists. Called when a run exits HITL state (status moves away from
// waiting, or on cancel/complete).
func (s *DecisionStore) DeleteSession(ctx context.Context, runID string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM hitl_sessions WHERE run_id = ?`, runID); err != nil {
		return fmt.Errorf("delete hitl_session for %s: %w", runID, err)
	}
	return nil
}

// DecisionCountsByRunID returns approved/rejected/total_scenes counts for a
// run in a single round-trip. Pending = TotalScenes - Approved - Rejected
// (computed by the caller). Uses COUNT(DISTINCT scene_id) to dedupe
// multiple decisions on the same scene (e.g., reject → approve counts as
// one approval, not two events). NULL scene_id rows are excluded (those
// are run-level decisions like metadata_ack, not scene-level).
func (s *DecisionStore) DecisionCountsByRunID(ctx context.Context, runID string) (DecisionCounts, error) {
	const q = `
SELECT
  (SELECT COUNT(DISTINCT scene_id) FROM decisions
    WHERE run_id = ? AND decision_type IN ('approve', 'override', 'system_auto_approved')
      AND superseded_by IS NULL AND scene_id IS NOT NULL) AS approved,
  (SELECT COUNT(DISTINCT scene_id) FROM decisions
    WHERE run_id = ? AND decision_type = 'reject'
      AND superseded_by IS NULL AND scene_id IS NOT NULL) AS rejected,
  (SELECT COUNT(*) FROM segments WHERE run_id = ?) AS total_scenes;
`
	var counts DecisionCounts
	if err := s.db.QueryRowContext(ctx, q, runID, runID, runID).Scan(
		&counts.Approved, &counts.Rejected, &counts.TotalScenes,
	); err != nil {
		return DecisionCounts{}, fmt.Errorf("decision counts for %s: %w", runID, err)
	}
	return counts, nil
}

func (s *DecisionStore) PrepareBatchReview(
	ctx context.Context,
	runID string,
	sceneResults []SceneReviewUpdate,
	autoApprovals []AutoApprovalInput,
) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("prepare batch review %s: begin tx: %w", runID, err)
	}
	defer tx.Rollback()

	for _, result := range sceneResults {
		sceneIndex := result.SceneIndex
		if !result.ReviewStatus.IsValid() {
			return false, fmt.Errorf("prepare batch review %s[%d]: invalid review status %q: %w", runID, sceneIndex, result.ReviewStatus, domain.ErrValidation)
		}
		flagsJSON, err := json.Marshal(normalizeSafeguardFlags(result.SafeguardFlags))
		if err != nil {
			return false, fmt.Errorf("prepare batch review %s[%d]: marshal safeguard flags: %w", runID, sceneIndex, err)
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE segments
			    SET review_status = ?, safeguard_flags = ?
			  WHERE run_id = ? AND scene_index = ?`,
			string(result.ReviewStatus), string(flagsJSON), runID, sceneIndex,
		)
		if err != nil {
			return false, fmt.Errorf("prepare batch review %s[%d]: update segment: %w", runID, sceneIndex, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("prepare batch review %s[%d]: rows affected: %w", runID, sceneIndex, err)
		}
		if n == 0 {
			return false, fmt.Errorf("prepare batch review %s[%d]: %w", runID, sceneIndex, domain.ErrNotFound)
		}
	}

	for _, approval := range autoApprovals {
		var exists int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*)
			   FROM decisions
			  WHERE run_id = ?
			    AND scene_id = ?
			    AND decision_type = ?
			    AND superseded_by IS NULL`,
			runID, strconv.Itoa(approval.SceneIndex), domain.DecisionTypeSystemAutoApproved,
		).Scan(&exists); err != nil {
			return false, fmt.Errorf("prepare batch review %s[%d]: check existing system decision: %w", runID, approval.SceneIndex, err)
		}
		if exists > 0 {
			continue
		}
		snapshot, err := json.Marshal(map[string]any{
			"threshold":    approval.Threshold,
			"critic_score": approval.CriticScore,
		})
		if err != nil {
			return false, fmt.Errorf("prepare batch review %s[%d]: marshal context snapshot: %w", runID, approval.SceneIndex, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO decisions (run_id, scene_id, decision_type, context_snapshot, note)
			 VALUES (?, ?, ?, ?, NULL)`,
			runID, strconv.Itoa(approval.SceneIndex), domain.DecisionTypeSystemAutoApproved, string(snapshot),
		); err != nil {
			return false, fmt.Errorf("prepare batch review %s[%d]: insert system decision: %w", runID, approval.SceneIndex, err)
		}
	}

	var waitingCount int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*)
		   FROM segments
		  WHERE run_id = ? AND review_status = ?`,
		runID, string(domain.ReviewStatusWaitingForReview),
	).Scan(&waitingCount); err != nil {
		return false, fmt.Errorf("prepare batch review %s: count waiting scenes: %w", runID, err)
	}
	autoContinue := waitingCount == 0
	if autoContinue {
		res, err := tx.ExecContext(ctx,
			`UPDATE runs
			    SET stage = ?, status = ?
			  WHERE id = ?`,
			string(domain.StageAssemble), string(domain.StatusRunning), runID,
		)
		if err != nil {
			return false, fmt.Errorf("prepare batch review %s: advance run: %w", runID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("prepare batch review %s: advance rows affected: %w", runID, err)
		}
		if n == 0 {
			return false, fmt.Errorf("prepare batch review %s: %w", runID, domain.ErrNotFound)
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("prepare batch review %s: commit: %w", runID, err)
	}
	return autoContinue, nil
}

func (s *DecisionStore) RecordSceneDecision(
	ctx context.Context,
	runID string,
	sceneIndex int,
	decisionType string,
	contextSnapshot *string,
	note *string,
) error {
	if runID == "" {
		return fmt.Errorf("record scene decision: %w: run_id is required", domain.ErrValidation)
	}
	if sceneIndex < 0 {
		return fmt.Errorf("record scene decision %s[%d]: %w: scene_index must be non-negative", runID, sceneIndex, domain.ErrValidation)
	}

	var nextStatus *domain.ReviewStatus
	switch decisionType {
	case domain.DecisionTypeApprove:
		status := domain.ReviewStatusApproved
		nextStatus = &status
	case domain.DecisionTypeReject:
		status := domain.ReviewStatusRejected
		nextStatus = &status
	case domain.DecisionTypeSkipAndRemember:
		// V1 skip persists only the decision row; the scene review status
		// stays unchanged and the client advances locally to the next scene.
	default:
		return fmt.Errorf("record scene decision %s[%d]: %w: invalid decision type %q", runID, sceneIndex, domain.ErrValidation, decisionType)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record scene decision %s[%d]: begin tx: %w", runID, sceneIndex, err)
	}
	defer tx.Rollback()

	var reviewStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT review_status
		   FROM segments
		  WHERE run_id = ? AND scene_index = ?`,
		runID, sceneIndex,
	).Scan(&reviewStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("record scene decision %s[%d]: %w", runID, sceneIndex, domain.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("record scene decision %s[%d]: load segment: %w", runID, sceneIndex, err)
	}

	currentStatus := domain.ReviewStatus(reviewStatus)
	if currentStatus == domain.ReviewStatusApproved ||
		currentStatus == domain.ReviewStatusRejected ||
		currentStatus == domain.ReviewStatusAutoApproved {
		return fmt.Errorf("record scene decision %s[%d]: %w", runID, sceneIndex, domain.ErrConflict)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO decisions (run_id, scene_id, decision_type, context_snapshot, note)
		 VALUES (?, ?, ?, ?, ?)`,
		runID,
		strconv.Itoa(sceneIndex),
		decisionType,
		contextSnapshot,
		note,
	); err != nil {
		return fmt.Errorf("record scene decision %s[%d]: insert decision: %w", runID, sceneIndex, err)
	}

	if nextStatus != nil {
		res, err := tx.ExecContext(ctx,
			`UPDATE segments
			    SET review_status = ?
			  WHERE run_id = ? AND scene_index = ?`,
			string(*nextStatus), runID, sceneIndex,
		)
		if err != nil {
			return fmt.Errorf("record scene decision %s[%d]: update segment: %w", runID, sceneIndex, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("record scene decision %s[%d]: rows affected: %w", runID, sceneIndex, err)
		}
		if n == 0 {
			return fmt.Errorf("record scene decision %s[%d]: %w", runID, sceneIndex, domain.ErrNotFound)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record scene decision %s[%d]: commit: %w", runID, sceneIndex, err)
	}
	return nil
}

func (s *DecisionStore) ApproveAllRemaining(
	ctx context.Context,
	runID string,
	aggregateCommandID string,
	focusSceneIndex int,
) (*BatchApproveResult, error) {
	if runID == "" {
		return nil, fmt.Errorf("approve all remaining: %w: run_id is required", domain.ErrValidation)
	}
	if aggregateCommandID == "" {
		return nil, fmt.Errorf("approve all remaining %s: %w: aggregate command id is required", runID, domain.ErrValidation)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("approve all remaining %s: begin tx: %w", runID, err)
	}
	defer tx.Rollback()

	// AC-2 rule: "skipped-only" scenes (latest non-superseded decision is
	// skip_and_remember) must not be re-approved. V1 skip persists the decision
	// row but leaves segments.review_status unchanged, so the review_status
	// filter alone would pick them up — exclude them explicitly.
	rows, err := tx.QueryContext(ctx,
		`SELECT s.scene_index
		   FROM segments s
		  WHERE s.run_id = ?
		    AND s.review_status IN (?, ?)
		    AND NOT EXISTS (
		      SELECT 1 FROM decisions d
		       WHERE d.run_id = s.run_id
		         AND d.scene_id = CAST(s.scene_index AS TEXT)
		         AND d.decision_type = ?
		         AND d.superseded_by IS NULL
		    )
		  ORDER BY s.scene_index ASC`,
		runID,
		string(domain.ReviewStatusWaitingForReview),
		string(domain.ReviewStatusPending),
		domain.DecisionTypeSkipAndRemember,
	)
	if err != nil {
		return nil, fmt.Errorf("approve all remaining %s: list target scenes: %w", runID, err)
	}
	defer rows.Close()

	sceneIndices := make([]int, 0)
	for rows.Next() {
		var sceneIndex int
		if err := rows.Scan(&sceneIndex); err != nil {
			return nil, fmt.Errorf("approve all remaining %s: scan target scene: %w", runID, err)
		}
		sceneIndices = append(sceneIndices, sceneIndex)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("approve all remaining %s: iterate target scenes: %w", runID, err)
	}

	if len(sceneIndices) == 0 {
		// No decision rows are inserted, so the aggregate_command_id never
		// lands in the DB and would point at nothing from an undo perspective.
		// Zero it out here so the client can distinguish "batch committed but
		// empty" from "real undoable batch" and skip the phantom undo push.
		return &BatchApproveResult{
			AggregateCommandID: "",
			ApprovedCount:      0,
			ApprovedSceneIDs:   []int{},
			FocusSceneIndex:    focusSceneIndex,
		}, nil
	}

	// AC-4 focus-restoration: undo replays this index. If the caller passes an
	// index that isn't actually part of the approved set (client default of 0
	// when nothing is selected, or a selected scene that was already auto-
	// approved), snap to the first approved scene so undo lands on a scene
	// the batch actually modified.
	if focusSceneIndex < 0 || !slices.Contains(sceneIndices, focusSceneIndex) {
		focusSceneIndex = sceneIndices[0]
	}

	sceneIDsAny := make([]any, 0, len(sceneIndices))
	for _, sceneIndex := range sceneIndices {
		sceneIDsAny = append(sceneIDsAny, sceneIndex)
	}
	snapshotBytes, err := json.Marshal(map[string]any{
		"aggregate_command_id":   aggregateCommandID,
		"approved_scene_indices": sceneIndices,
		"chunk_size":             BatchApproveChunkSize,
		"command_kind":           domain.CommandKindApproveAllRemaining,
		"focus_scene_index":      focusSceneIndex,
		"focus_target":           "scene-card",
	})
	if err != nil {
		return nil, fmt.Errorf("approve all remaining %s: marshal snapshot: %w", runID, err)
	}
	snapshot := string(snapshotBytes)

	for start := 0; start < len(sceneIndices); start += BatchApproveChunkSize {
		end := start + BatchApproveChunkSize
		if end > len(sceneIndices) {
			end = len(sceneIndices)
		}
		chunk := sceneIndices[start:end]

		for _, sceneIndex := range chunk {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO decisions (run_id, scene_id, decision_type, context_snapshot, note)
				 VALUES (?, ?, ?, ?, NULL)`,
				runID,
				strconv.Itoa(sceneIndex),
				domain.DecisionTypeApprove,
				snapshot,
			); err != nil {
				return nil, fmt.Errorf("approve all remaining %s[%d]: insert decision: %w", runID, sceneIndex, err)
			}
		}

		placeholders := make([]string, len(chunk))
		args := make([]any, 0, len(chunk)+1)
		args = append(args, runID)
		for i, sceneIndex := range chunk {
			placeholders[i] = "?"
			args = append(args, sceneIndex)
		}
		query := fmt.Sprintf(
			`UPDATE segments
			    SET review_status = ?
			  WHERE run_id = ?
			    AND scene_index IN (%s)`,
			strings.Join(placeholders, ","),
		)
		updateArgs := make([]any, 0, len(args)+1)
		updateArgs = append(updateArgs, string(domain.ReviewStatusApproved))
		updateArgs = append(updateArgs, args...)
		if _, err := tx.ExecContext(ctx, query, updateArgs...); err != nil {
			return nil, fmt.Errorf("approve all remaining %s: update chunk [%d:%d]: %w", runID, start, end, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("approve all remaining %s: commit: %w", runID, err)
	}

	return &BatchApproveResult{
		AggregateCommandID: aggregateCommandID,
		ApprovedCount:      len(sceneIndices),
		ApprovedSceneIDs:   sceneIndices,
		FocusSceneIndex:    focusSceneIndex,
	}, nil
}

func (s *DecisionStore) OverrideMinorSafeguard(ctx context.Context, runID string, sceneIndex int, note string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("override minor safeguard %s[%d]: begin tx: %w", runID, sceneIndex, err)
	}
	defer tx.Rollback()

	var (
		reviewStatus  string
		safeguardJSON string
	)
	err = tx.QueryRowContext(ctx,
		`SELECT review_status, safeguard_flags
		   FROM segments
		  WHERE run_id = ? AND scene_index = ?`,
		runID, sceneIndex,
	).Scan(&reviewStatus, &safeguardJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("override minor safeguard %s[%d]: %w", runID, sceneIndex, domain.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("override minor safeguard %s[%d]: load segment: %w", runID, sceneIndex, err)
	}

	var flags []string
	if safeguardJSON != "" {
		if err := json.Unmarshal([]byte(safeguardJSON), &flags); err != nil {
			return fmt.Errorf("override minor safeguard %s[%d]: decode safeguard flags: %w", runID, sceneIndex, err)
		}
	}
	if reviewStatus != string(domain.ReviewStatusWaitingForReview) || !containsString(flags, domain.SafeguardFlagMinors) {
		return fmt.Errorf("override minor safeguard %s[%d]: %w", runID, sceneIndex, domain.ErrConflict)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO decisions (run_id, scene_id, decision_type, note)
		 VALUES (?, ?, ?, ?)`,
		runID, strconv.Itoa(sceneIndex), domain.DecisionTypeOverride, note,
	); err != nil {
		return fmt.Errorf("override minor safeguard %s[%d]: insert override decision: %w", runID, sceneIndex, err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE segments
		    SET review_status = ?
		  WHERE run_id = ? AND scene_index = ?`,
		string(domain.ReviewStatusApproved), runID, sceneIndex,
	); err != nil {
		return fmt.Errorf("override minor safeguard %s[%d]: update segment: %w", runID, sceneIndex, err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE runs
		    SET human_override = (human_override | 1)
		  WHERE id = ?`,
		runID,
	); err != nil {
		return fmt.Errorf("override minor safeguard %s[%d]: update run: %w", runID, sceneIndex, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("override minor safeguard %s[%d]: commit: %w", runID, sceneIndex, err)
	}
	return nil
}

// KappaPairsForRuns returns one run-level pair per run with a non-null
// critic_score and at least one non-superseded approve/reject scene decision.
// The operator side is the dominant run decision; ties conservatively break to
// reject.
func (s *DecisionStore) KappaPairsForRuns(
	ctx context.Context,
	runIDs []string,
	calibrationThreshold float64,
) ([]KappaPair, error) {
	if len(runIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(runIDs))
	args := make([]any, 0, len(runIDs))
	for i, id := range runIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
SELECT d.run_id, d.decision_type, r.critic_score, COUNT(DISTINCT d.scene_id) AS decision_count
  FROM decisions d
  JOIN runs r ON r.id = d.run_id
 WHERE d.run_id IN (%s)
   AND d.scene_id IS NOT NULL
   AND d.decision_type IN ('approve', 'reject')
   AND d.superseded_by IS NULL
   AND r.critic_score IS NOT NULL
 GROUP BY d.run_id, d.decision_type, r.critic_score
 ORDER BY d.run_id ASC, d.decision_type ASC`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("kappa pairs for runs: %w", err)
	}
	defer rows.Close()

	type runAgg struct {
		criticScore float64
		approve     int
		reject      int
	}
	byRun := make(map[string]runAgg, len(runIDs))
	for rows.Next() {
		var (
			runID        string
			decisionType string
			criticScore  float64
			count        int
		)
		if err := rows.Scan(&runID, &decisionType, &criticScore, &count); err != nil {
			return nil, fmt.Errorf("kappa pairs for runs scan: %w", err)
		}
		agg := byRun[runID]
		agg.criticScore = criticScore
		if decisionType == "approve" {
			agg.approve = count
		}
		if decisionType == "reject" {
			agg.reject = count
		}
		byRun[runID] = agg
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("kappa pairs for runs iterate: %w", err)
	}
	if len(byRun) == 0 {
		return nil, nil
	}

	pairs := make([]KappaPair, 0, len(runIDs))
	for _, runID := range runIDs {
		agg, ok := byRun[runID]
		if !ok {
			continue
		}
		pairs = append(pairs, KappaPair{
			CriticPass:      agg.criticScore >= calibrationThreshold,
			OperatorApprove: agg.approve > agg.reject,
		})
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	return pairs, nil
}

// LatestDecisionIDForRuns returns the max non-superseded approve/reject
// decision id in the evaluated run window, or 0 when none exist.
func (s *DecisionStore) LatestDecisionIDForRuns(ctx context.Context, runIDs []string) (int, error) {
	if len(runIDs) == 0 {
		return 0, nil
	}

	placeholders := make([]string, len(runIDs))
	args := make([]any, 0, len(runIDs))
	for i, id := range runIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
SELECT COALESCE(MAX(id), 0)
  FROM decisions
 WHERE run_id IN (%s)
   AND decision_type IN ('approve', 'reject')
   AND superseded_by IS NULL`, strings.Join(placeholders, ","))

	var latest int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&latest); err != nil {
		return 0, fmt.Errorf("latest decision id for runs: %w", err)
	}
	return latest, nil
}

// DefectEscapeInRuns returns aggregate auto-passed and escaped scene counts for
// the provided run window.
func (s *DecisionStore) DefectEscapeInRuns(
	ctx context.Context,
	runIDs []string,
	calibrationThreshold float64,
) (DefectEscape, error) {
	if len(runIDs) == 0 {
		return DefectEscape{}, nil
	}

	placeholders := make([]string, len(runIDs))
	args := make([]any, 0, len(runIDs)+1)
	for i, id := range runIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, calibrationThreshold)

	query := fmt.Sprintf(`
SELECT COUNT(*) AS auto_passed_scenes,
       SUM(
           CASE
               WHEN EXISTS (
                   SELECT 1
                     FROM decisions d
                    WHERE d.run_id = s.run_id
                      AND d.scene_id = CAST(s.scene_index AS TEXT)
                      AND d.decision_type = 'reject'
                      AND d.superseded_by IS NULL
               ) THEN 1
               ELSE 0
           END
       ) AS escaped_scenes
  FROM segments s
 WHERE s.run_id IN (%s)
   AND s.critic_score >= ?`, strings.Join(placeholders, ","))

	var (
		out     DefectEscape
		escaped sql.NullInt64
	)
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&out.AutoPassedScenes, &escaped); err != nil {
		return DefectEscape{}, fmt.Errorf("defect escape in runs: %w", err)
	}
	out.EscapedScenes = int(escaped.Int64)
	return out, nil
}

// PriorRejection is the FR53 warning payload surfaced when a reject decision
// lands on a scene that was previously rejected in a different run with the
// same scp_id and scene_index.
type PriorRejection struct {
	RunID      string
	SCPID      string
	SceneIndex int
	Reason     string
	CreatedAt  string
}

// PriorRejectionForScene returns the most recent non-superseded reject
// decision (with a non-empty note) on the same scp_id + scene_index from a
// DIFFERENT run. Returns (nil, nil) if there is no cross-run prior failure.
// The lookup is deterministic (no NLP / similarity) and honors Story 8.3
// undo semantics by ignoring superseded rows.
func (s *DecisionStore) PriorRejectionForScene(
	ctx context.Context,
	runID string,
	sceneIndex int,
) (*PriorRejection, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT d.run_id, r.scp_id, d.note, d.created_at
  FROM decisions d
  JOIN runs r ON r.id = d.run_id
 WHERE r.scp_id = (SELECT scp_id FROM runs WHERE id = ?)
   AND d.scene_id = ?
   AND d.decision_type = 'reject'
   AND d.superseded_by IS NULL
   AND d.note IS NOT NULL
   AND d.note != ''
   AND d.run_id != ?
 ORDER BY d.created_at DESC, d.id DESC
 LIMIT 1`,
		runID, strconv.Itoa(sceneIndex), runID,
	)
	var prior PriorRejection
	var note sql.NullString
	err := row.Scan(&prior.RunID, &prior.SCPID, &note, &prior.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("prior rejection for %s[%d]: %w", runID, sceneIndex, err)
	}
	if note.Valid {
		prior.Reason = note.String
	}
	prior.SceneIndex = sceneIndex
	return &prior, nil
}

// CountRegenAttempts returns the total number of reject decisions for a
// specific run + scene, including superseded (undone) rows. Since every
// operator reject in batch review triggers a regeneration dispatch whose
// side-effects are NOT rolled back by /undo (the stub flipped segment
// status, and a future Phase B dispatcher will have already modified
// media artifacts), the retry cap must reflect actual dispatches rather
// than "currently-standing rejects." Counting superseded rows prevents
// a reject → undo → reject loop from resetting the AC-4 cap indefinitely.
func (s *DecisionStore) CountRegenAttempts(
	ctx context.Context,
	runID string,
	sceneIndex int,
) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
  FROM decisions
 WHERE run_id = ?
   AND scene_id = ?
   AND decision_type = 'reject'`,
		runID, strconv.Itoa(sceneIndex),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count regen attempts for %s[%d]: %w", runID, sceneIndex, err)
	}
	return count, nil
}

// UndoableDecision is the minimal projection of a decisions row needed by the
// undo service to reverse a prior operator action.
type UndoableDecision struct {
	ID           int64
	RunID        string
	SceneID      *string
	DecisionType string
	Note         *string
}

// UndoApplication is the result returned by ApplyUndo: the original decision
// id that was superseded and the new reversal decision id.
type UndoApplication struct {
	OriginalDecisionID int64
	ReversalDecisionID int64
	SceneIndex         int
	DecisionType       string
	CommandKind        string
}

// RecordDescriptorEdit inserts a descriptor_edit decision row capturing the
// before/after values of runs.frozen_descriptor for undo traceability.
// Returns the new decision row id.
func (s *DecisionStore) RecordDescriptorEdit(ctx context.Context, runID, before, after string) (int64, error) {
	snapshot, err := json.Marshal(map[string]any{
		"command_kind": domain.CommandKindDescriptorEdit,
		"before":       before,
		"after":        after,
	})
	if err != nil {
		return 0, fmt.Errorf("record descriptor edit %s: marshal snapshot: %w", runID, err)
	}
	snap := string(snapshot)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO decisions (run_id, decision_type, context_snapshot) VALUES (?, ?, ?)`,
		runID, domain.DecisionTypeDescriptorEdit, snap,
	)
	if err != nil {
		return 0, fmt.Errorf("record descriptor edit %s: insert: %w", runID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("record descriptor edit %s: last insert id: %w", runID, err)
	}
	return id, nil
}

// LatestUndoableDecision returns the most recently created non-superseded
// decision that is eligible for V1 undo (approve, reject, skip_and_remember,
// descriptor_edit). Returns (nil, nil) when no undoable decision exists.
func (s *DecisionStore) LatestUndoableDecision(ctx context.Context, runID string) (*UndoableDecision, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, run_id, scene_id, decision_type, note
		   FROM decisions
		  WHERE run_id = ?
		    AND decision_type IN ('approve', 'reject', 'skip_and_remember', 'descriptor_edit')
		    AND superseded_by IS NULL
		  ORDER BY created_at DESC, id DESC
		  LIMIT 1`,
		runID,
	)
	var d UndoableDecision
	var (
		sceneID sql.NullString
		note    sql.NullString
	)
	err := row.Scan(&d.ID, &d.RunID, &sceneID, &d.DecisionType, &note)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest undoable decision for %s: %w", runID, err)
	}
	if sceneID.Valid {
		d.SceneID = &sceneID.String
	}
	if note.Valid {
		d.Note = &note.String
	}
	return &d, nil
}

// ApplyUndo atomically reverses an operator scene decision by:
//  1. Inserting a reversal decision row (decision_type = "undo")
//  2. Setting the original decision's superseded_by = reversal ID
//  3. Restoring the segment's review_status to waiting_for_review when the
//     original was approve or reject (skip leaves segment status unchanged).
//
// The caller must validate the Phase C gate before calling this function.
func (s *DecisionStore) ApplyUndo(ctx context.Context, runID string, originalDecisionID int64) (*UndoApplication, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("apply undo %s[%d]: begin tx: %w", runID, originalDecisionID, err)
	}
	defer tx.Rollback()

	var (
		sceneIDStr      sql.NullString
		decisionType    string
		contextSnapshot sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT scene_id, decision_type, context_snapshot FROM decisions WHERE id = ? AND run_id = ? AND superseded_by IS NULL`,
		originalDecisionID, runID,
	).Scan(&sceneIDStr, &decisionType, &contextSnapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("apply undo %s[%d]: %w", runID, originalDecisionID, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("apply undo %s[%d]: load original: %w", runID, originalDecisionID, err)
	}

	note := fmt.Sprintf("undo of decision %d", originalDecisionID)
	result, err := tx.ExecContext(ctx,
		`INSERT INTO decisions (run_id, scene_id, decision_type, note) VALUES (?, ?, ?, ?)`,
		runID, sceneIDStr, domain.DecisionTypeUndo, note,
	)
	if err != nil {
		return nil, fmt.Errorf("apply undo %s[%d]: insert reversal: %w", runID, originalDecisionID, err)
	}
	reversalID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("apply undo %s[%d]: reversal id: %w", runID, originalDecisionID, err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE decisions SET superseded_by = ? WHERE id = ?`,
		reversalID, originalDecisionID,
	); err != nil {
		return nil, fmt.Errorf("apply undo %s[%d]: mark superseded: %w", runID, originalDecisionID, err)
	}

	sceneIndex := -1
	commandKind := decisionTypeToCommandKind(decisionType)
	if sceneIDStr.Valid && sceneIDStr.String != "" {
		sceneIndex, err = strconv.Atoi(sceneIDStr.String)
		if err != nil {
			return nil, fmt.Errorf("apply undo %s[%d]: parse scene_id %q: %w", runID, originalDecisionID, sceneIDStr.String, err)
		}
	}

	switch decisionType {
	case domain.DecisionTypeApprove, domain.DecisionTypeReject:
		if decisionType == domain.DecisionTypeApprove && contextSnapshot.Valid {
			undoneSceneIndices, focusSceneIndex, handled, err := s.applyBatchApproveUndo(ctx, tx, runID, originalDecisionID, reversalID, contextSnapshot.String)
			if err != nil {
				return nil, err
			}
			if handled {
				commandKind = domain.CommandKindApproveAllRemaining
				if focusSceneIndex >= 0 {
					sceneIndex = focusSceneIndex
				} else if len(undoneSceneIndices) > 0 {
					sceneIndex = undoneSceneIndices[0]
				}
				break
			}
		}
		// Restore the segment's review_status to waiting_for_review.
		if sceneIndex < 0 {
			return nil, fmt.Errorf("apply undo %s[%d]: %w: approve/reject undo requires scene_id", runID, originalDecisionID, domain.ErrValidation)
		}
		// Verify the segment is still in the expected terminal state before
		// restoring. In a single-writer SQLite environment this guard is
		// defensive but prevents silent corruption if a concurrent admin
		// action has already changed the status.
		var currentSegStatus string
		err = tx.QueryRowContext(ctx,
			`SELECT review_status FROM segments WHERE run_id = ? AND scene_index = ?`,
			runID, sceneIndex,
		).Scan(&currentSegStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("apply undo %s[%d]: segment not found: %w", runID, originalDecisionID, domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("apply undo %s[%d]: check segment status: %w", runID, originalDecisionID, err)
		}
		switch domain.ReviewStatus(currentSegStatus) {
		case domain.ReviewStatusApproved, domain.ReviewStatusRejected:
			// expected — proceed with restore
		default:
			return nil, fmt.Errorf("apply undo %s[%d]: segment status %q is not undoable: %w", runID, originalDecisionID, currentSegStatus, domain.ErrConflict)
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE segments SET review_status = ? WHERE run_id = ? AND scene_index = ?`,
			string(domain.ReviewStatusWaitingForReview), runID, sceneIndex,
		)
		if err != nil {
			return nil, fmt.Errorf("apply undo %s[%d]: restore segment: %w", runID, originalDecisionID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("apply undo %s[%d]: restore segment rows affected: %w", runID, originalDecisionID, err)
		}
		if n == 0 {
			return nil, fmt.Errorf("apply undo %s[%d]: restore segment: %w", runID, originalDecisionID, domain.ErrNotFound)
		}

	case domain.DecisionTypeDescriptorEdit:
		// Extract the "before" value from context_snapshot and restore frozen_descriptor.
		var snapshotRaw sql.NullString
		err = tx.QueryRowContext(ctx,
			`SELECT context_snapshot FROM decisions WHERE id = ?`,
			originalDecisionID,
		).Scan(&snapshotRaw)
		if err != nil {
			return nil, fmt.Errorf("apply undo %s: load descriptor snapshot: %w", runID, err)
		}
		if !snapshotRaw.Valid {
			return nil, fmt.Errorf("apply undo %s: descriptor_edit has no context_snapshot: %w", runID, domain.ErrValidation)
		}
		var payload struct {
			Before string `json:"before"`
		}
		if err := json.Unmarshal([]byte(snapshotRaw.String), &payload); err != nil {
			return nil, fmt.Errorf("apply undo %s: parse descriptor snapshot: %w", runID, err)
		}
		var frozenPtr *string
		if payload.Before != "" {
			frozenPtr = &payload.Before
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE runs SET frozen_descriptor = ? WHERE id = ?`,
			frozenPtr, runID,
		); err != nil {
			return nil, fmt.Errorf("apply undo %s: restore frozen_descriptor: %w", runID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("apply undo %s[%d]: commit: %w", runID, originalDecisionID, err)
	}

	return &UndoApplication{
		OriginalDecisionID: originalDecisionID,
		ReversalDecisionID: reversalID,
		SceneIndex:         sceneIndex,
		DecisionType:       decisionType,
		CommandKind:        commandKind,
	}, nil
}

func decisionTypeToCommandKind(decisionType string) string {
	switch decisionType {
	case domain.DecisionTypeApprove:
		return domain.CommandKindApprove
	case domain.DecisionTypeReject:
		return domain.CommandKindReject
	case domain.DecisionTypeSkipAndRemember:
		return domain.CommandKindSkip
	case domain.DecisionTypeDescriptorEdit:
		return domain.CommandKindDescriptorEdit
	default:
		return decisionType
	}
}

func (s *DecisionStore) applyBatchApproveUndo(
	ctx context.Context,
	tx *sql.Tx,
	runID string,
	originalDecisionID int64,
	reversalID int64,
	contextSnapshot string,
) ([]int, int, bool, error) {
	var payload struct {
		AggregateCommandID   string `json:"aggregate_command_id"`
		CommandKind          string `json:"command_kind"`
		FocusSceneIndex      int    `json:"focus_scene_index"`
		ApprovedSceneIndices []int  `json:"approved_scene_indices"`
	}
	if err := json.Unmarshal([]byte(contextSnapshot), &payload); err != nil {
		return nil, -1, false, fmt.Errorf("apply undo %s[%d]: parse batch snapshot: %w", runID, originalDecisionID, err)
	}
	if payload.CommandKind != domain.CommandKindApproveAllRemaining || payload.AggregateCommandID == "" {
		return nil, -1, false, nil
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id, scene_id
		   FROM decisions
		  WHERE run_id = ?
		    AND decision_type = ?
		    AND (id = ? OR superseded_by IS NULL)
		    AND json_extract(context_snapshot, '$.command_kind') = ?
		    AND json_extract(context_snapshot, '$.aggregate_command_id') = ?
		  ORDER BY CAST(scene_id AS INTEGER) ASC, id ASC`,
		runID,
		domain.DecisionTypeApprove,
		originalDecisionID,
		domain.CommandKindApproveAllRemaining,
		payload.AggregateCommandID,
	)
	if err != nil {
		return nil, -1, false, fmt.Errorf("apply undo %s[%d]: load batch decisions: %w", runID, originalDecisionID, err)
	}
	defer rows.Close()

	decisionIDs := make([]int64, 0)
	sceneIndices := make([]int, 0)
	for rows.Next() {
		var (
			id      int64
			sceneID string
		)
		if err := rows.Scan(&id, &sceneID); err != nil {
			return nil, -1, false, fmt.Errorf("apply undo %s[%d]: scan batch decision: %w", runID, originalDecisionID, err)
		}
		sceneIndex, err := strconv.Atoi(sceneID)
		if err != nil {
			return nil, -1, false, fmt.Errorf("apply undo %s[%d]: parse batch scene_id %q: %w", runID, originalDecisionID, sceneID, err)
		}
		decisionIDs = append(decisionIDs, id)
		sceneIndices = append(sceneIndices, sceneIndex)
	}
	if err := rows.Err(); err != nil {
		return nil, -1, false, fmt.Errorf("apply undo %s[%d]: iterate batch decisions: %w", runID, originalDecisionID, err)
	}
	if len(decisionIDs) == 0 {
		return nil, -1, false, fmt.Errorf("apply undo %s[%d]: batch command missing decisions: %w", runID, originalDecisionID, domain.ErrNotFound)
	}

	decisionPlaceholders := make([]string, len(decisionIDs))
	decisionArgs := make([]any, 0, len(decisionIDs)+1)
	decisionArgs = append(decisionArgs, reversalID)
	for i, id := range decisionIDs {
		decisionPlaceholders[i] = "?"
		decisionArgs = append(decisionArgs, id)
	}
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE decisions SET superseded_by = ? WHERE id IN (%s)`, strings.Join(decisionPlaceholders, ",")),
		decisionArgs...,
	); err != nil {
		return nil, -1, false, fmt.Errorf("apply undo %s[%d]: mark batch superseded: %w", runID, originalDecisionID, err)
	}

	segmentPlaceholders := make([]string, len(sceneIndices))
	segmentArgs := make([]any, 0, len(sceneIndices)+2)
	segmentArgs = append(segmentArgs, string(domain.ReviewStatusWaitingForReview), runID)
	for i, sceneIndex := range sceneIndices {
		segmentPlaceholders[i] = "?"
		segmentArgs = append(segmentArgs, sceneIndex)
	}
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(
			`UPDATE segments
			    SET review_status = ?
			  WHERE run_id = ?
			    AND scene_index IN (%s)`,
			strings.Join(segmentPlaceholders, ","),
		),
		segmentArgs...,
	); err != nil {
		return nil, -1, false, fmt.Errorf("apply undo %s[%d]: restore batch segments: %w", runID, originalDecisionID, err)
	}

	return sceneIndices, payload.FocusSceneIndex, true, nil
}

func scanDecision(sc scanner) (*domain.Decision, error) {
	var d domain.Decision
	var (
		sceneID        sql.NullString
		contextSnap    sql.NullString
		outcomeLink    sql.NullString
		tags           sql.NullString
		feedbackSource sql.NullString
		externalRef    sql.NullString
		feedbackAt     sql.NullString
		supersededBy   sql.NullInt64
		note           sql.NullString
	)
	if err := sc.Scan(
		&d.ID, &d.RunID, &sceneID, &d.DecisionType, &contextSnap, &outcomeLink,
		&tags, &feedbackSource, &externalRef, &feedbackAt, &supersededBy,
		&note, &d.CreatedAt,
	); err != nil {
		return nil, err
	}
	if sceneID.Valid {
		d.SceneID = &sceneID.String
	}
	if contextSnap.Valid {
		d.ContextSnapshot = &contextSnap.String
	}
	if outcomeLink.Valid {
		d.OutcomeLink = &outcomeLink.String
	}
	if tags.Valid {
		d.Tags = &tags.String
	}
	if feedbackSource.Valid {
		d.FeedbackSource = &feedbackSource.String
	}
	if externalRef.Valid {
		d.ExternalRef = &externalRef.String
	}
	if feedbackAt.Valid {
		d.FeedbackAt = &feedbackAt.String
	}
	if supersededBy.Valid {
		d.SupersededBy = &supersededBy.Int64
	}
	if note.Valid {
		d.Note = &note.String
	}
	return &d, nil
}

func scanTimelineDecision(sc scanner) (*TimelineDecision, error) {
	var d TimelineDecision
	var (
		sceneID      sql.NullString
		contextSnap  sql.NullString
		note         sql.NullString
		supersededBy sql.NullInt64
	)
	if err := sc.Scan(
		&d.ID,
		&d.RunID,
		&d.SCPID,
		&sceneID,
		&d.DecisionType,
		&contextSnap,
		&note,
		&supersededBy,
		&d.CreatedAt,
	); err != nil {
		return nil, err
	}
	if sceneID.Valid {
		d.SceneID = &sceneID.String
	}
	if contextSnap.Valid {
		d.ContextSnapshot = &contextSnap.String
	}
	if note.Valid {
		d.Note = &note.String
	}
	if supersededBy.Valid {
		d.SupersededBy = &supersededBy.Int64
	}
	return &d, nil
}

func containsString(values []string, needle string) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}
