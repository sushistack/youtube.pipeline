package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
    WHERE run_id = ? AND decision_type = 'approve'
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
