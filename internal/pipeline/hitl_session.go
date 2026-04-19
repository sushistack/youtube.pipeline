package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// HITLSessionStore is the minimal persistence surface HITL session building
// needs. *db.DecisionStore satisfies this interface structurally. Declared
// here (consumer side) to keep pipeline/ free of direct db/ imports.
type HITLSessionStore interface {
	ListByRunID(ctx context.Context, runID string) ([]*domain.Decision, error)
	DecisionCountsByRunID(ctx context.Context, runID string) (DecisionCounts, error)
	GetSession(ctx context.Context, runID string) (*domain.HITLSession, error)
	UpsertSession(ctx context.Context, session *domain.HITLSession) error
	DeleteSession(ctx context.Context, runID string) error
}

// snapshotStatusSkipped is the internal-only pseudo-status used by
// BuildSessionSnapshot to advance NextSceneIndex past skipped scenes.
// It is never written to segments.review_status.
const snapshotStatusSkipped = "skipped"

// DecisionCounts mirrors db.DecisionCounts at the pipeline boundary (avoids
// a direct import of db/ from pipeline/). Populated by the DecisionStore.
type DecisionCounts struct {
	Approved    int
	Rejected    int
	TotalScenes int
}

// BuildSessionSnapshot constructs the DecisionSnapshot payload from the
// current decisions + total scene count for a run. Pure: no DB access.
// The caller passes in decisions (already non-superseded) and the total
// segment count; this function classifies each scene's review status and
// computes the aggregate counts.
//
// scene_id keys in the output map are the string form of segments.scene_index
// (0..totalScenes-1). Scenes without any decision are "pending"; scenes with
// an approve decision are "approved"; scenes with a reject are "rejected".
// If multiple non-superseded decisions exist for the same scene (unusual),
// the LAST one wins (by decisions slice order — caller should pre-sort by
// created_at ASC, which DecisionStore.ListByRunID already guarantees).
//
// Empty decisions + totalScenes=0 returns a zero-valued snapshot whose
// SceneStatuses is a non-nil empty map (so downstream callers can iterate
// without a nil-check).
func BuildSessionSnapshot(decisions []*domain.Decision, totalScenes int) domain.DecisionSnapshot {
	sceneStatuses := make(map[string]string, totalScenes)
	// Batch review work starts in the actionable queue state.
	for i := 0; i < totalScenes; i++ {
		sceneStatuses[strconv.Itoa(i)] = string(domain.ReviewStatusWaitingForReview)
	}
	// Overlay decisions in order; later decisions on the same scene win.
	// Only accept scene_ids within the known 0..totalScenes-1 universe to
	// prevent ghost decisions from inflating aggregate counts.
	for _, d := range decisions {
		if d == nil || d.SceneID == nil {
			continue
		}
		if d.SupersededBy != nil {
			continue
		}
		if _, known := sceneStatuses[*d.SceneID]; !known {
			continue
		}
		switch d.DecisionType {
		case "approve", domain.DecisionTypeOverride, domain.DecisionTypeSystemAutoApproved:
			sceneStatuses[*d.SceneID] = "approved"
		case "reject":
			sceneStatuses[*d.SceneID] = "rejected"
		case domain.DecisionTypeSkipAndRemember:
			// V1 skip leaves segments.review_status unchanged but must
			// advance NextSceneIndex past this scene; a non-pending
			// pseudo-status does that without introducing a new segment
			// review_status value.
			sceneStatuses[*d.SceneID] = snapshotStatusSkipped
		}
	}
	var approved, rejected, pending int
	for _, status := range sceneStatuses {
		switch status {
		case "approved":
			approved++
		case "rejected":
			rejected++
		default:
			pending++
		}
	}
	return domain.DecisionSnapshot{
		TotalScenes:   totalScenes,
		ApprovedCount: approved,
		RejectedCount: rejected,
		PendingCount:  pending,
		SceneStatuses: sceneStatuses,
	}
}

// NextSceneIndex returns the 0-indexed scene number the operator should
// review next. Strategy: lowest scene index whose status is "pending" (or
// is missing from the map entirely). If all scenes are decided, returns
// totalScenes — one past the end, which callers interpret as "review
// complete" (the display layer typically renders this as "all scenes
// reviewed"). Pure function.
func NextSceneIndex(sceneStatuses map[string]string, totalScenes int) int {
	for i := 0; i < totalScenes; i++ {
		status, ok := sceneStatuses[strconv.Itoa(i)]
		if !ok || status == string(domain.ReviewStatusPending) || status == string(domain.ReviewStatusWaitingForReview) {
			return i
		}
	}
	return totalScenes
}

// SummaryString renders the state-aware summary string for FR49. Example:
//
//	"Run scp-049-run-1: reviewing scene 5 of 10, 4 approved, 0 rejected"
//
// When status is not a HITL wait state, returns a safe fallback
// "Run {id}: {status}". sceneIndex is converted to 1-indexed for display
// (operator-facing "scene 5 of 10" convention).
func SummaryString(runID string, stage domain.Stage, status domain.Status, sceneIndex, totalScenes int, summary domain.DecisionSummary) string {
	if status != domain.StatusWaiting || !IsHITLStage(stage) {
		return fmt.Sprintf("Run %s: %s", runID, status)
	}
	if totalScenes == 0 {
		return fmt.Sprintf("Run %s: reviewing (no scenes)", runID)
	}
	return fmt.Sprintf(
		"Run %s: reviewing scene %d of %d, %d approved, %d rejected",
		runID, sceneIndex+1, totalScenes, summary.ApprovedCount, summary.RejectedCount,
	)
}

// UpsertSessionFromState rebuilds the HITL session row from the current
// decisions + segments for a run and persists it. Intended to be called by
// the decision-capture flow (Epic 8) right after a decision is recorded, so
// the session snapshot stays in sync.
//
// Semantics:
//   - If the run is NOT in a HITL wait state (stage ∈ HITL stages AND
//     status=waiting), DeleteSession is called instead (leaving HITL ⇒ no
//     active session row) and (nil, nil) is returned.
//   - Otherwise: build snapshot from live decisions + counts, compute
//     scene_index via NextSceneIndex, upsert the row with
//     last_interaction_timestamp set to clk.Now().UTC() formatted per RFC3339.
//
// Returns the persisted HITLSession for caller logging/inspection.
func UpsertSessionFromState(
	ctx context.Context,
	store HITLSessionStore,
	clk clock.Clock,
	runID string,
	stage domain.Stage,
	status domain.Status,
) (*domain.HITLSession, error) {
	if status != domain.StatusWaiting || !IsHITLStage(stage) {
		if err := store.DeleteSession(ctx, runID); err != nil {
			return nil, fmt.Errorf("upsert session: delete on non-HITL state: %w", err)
		}
		return nil, nil
	}
	decisions, err := store.ListByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("upsert session: list decisions: %w", err)
	}
	counts, err := store.DecisionCountsByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("upsert session: decision counts: %w", err)
	}
	snapshot := BuildSessionSnapshot(decisions, counts.TotalScenes)
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("upsert session: marshal snapshot: %w", err)
	}
	sceneIndex := NextSceneIndex(snapshot.SceneStatuses, counts.TotalScenes)
	session := &domain.HITLSession{
		RunID:                    runID,
		Stage:                    stage,
		SceneIndex:               sceneIndex,
		LastInteractionTimestamp: clk.Now().UTC().Format(time.RFC3339),
		SnapshotJSON:             string(raw),
	}
	if err := store.UpsertSession(ctx, session); err != nil {
		return nil, fmt.Errorf("upsert session: persist: %w", err)
	}
	return session, nil
}
