package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

// DecisionReader is the read-only persistence surface HITLService needs.
// *db.DecisionStore satisfies this interface structurally.
type DecisionReader interface {
	ListByRunID(ctx context.Context, runID string) ([]*domain.Decision, error)
	DecisionCountsByRunID(ctx context.Context, runID string) (db.DecisionCounts, error)
	GetSession(ctx context.Context, runID string) (*domain.HITLSession, error)
}

// HITLService builds the enriched status payload for paused runs (FR49 +
// FR50). Keeps api.RunHandler slim and focused on HTTP concerns.
type HITLService struct {
	runs      RunStore
	decisions DecisionReader
	logger    *slog.Logger
}

// NewHITLService constructs a HITLService. logger is required (constructor
// injection per the NFR-L1 logging contract). runs is the same RunStore
// interface used by RunService — the hitl service needs Get only.
func NewHITLService(runs RunStore, decisions DecisionReader, logger *slog.Logger) *HITLService {
	if logger == nil {
		logger = slog.Default()
	}
	return &HITLService{runs: runs, decisions: decisions, logger: logger}
}

// StatusPayload is the value returned by BuildStatus; api.RunHandler writes
// it directly to the wire via writeJSON. Every optional field uses a pointer
// or omitempty slice so the JSON output is minimal for non-HITL runs.
type StatusPayload struct {
	Run              *domain.Run             `json:"run"`
	PausedPosition   *domain.HITLSession     `json:"paused_position,omitempty"`
	DecisionsSummary *domain.DecisionSummary `json:"decisions_summary,omitempty"`
	Summary          string                  `json:"summary,omitempty"`
	ChangesSince     []pipeline.Change       `json:"changes_since_last_interaction,omitempty"`
}

// BuildStatus assembles the full StatusPayload for a run. Read-only: no
// writes to hitl_sessions (that happens via UpsertSessionFromState on the
// decision-capture path). Steps:
//
//  1. Get the run (ErrNotFound propagates to caller).
//  2. Fetch decision counts → DecisionsSummary.
//  3. If the run is in a HITL wait state (IsHITLStage + status=waiting):
//     a. GetSession — if present, set PausedPosition, parse its
//        snapshot_json, compute the diff vs the live state and attach
//        timestamps from the decisions list.
//     b. If no session row exists, log Warn and fall back to a live-state
//        summary (PausedPosition stays nil, ChangesSince stays nil).
//     c. Build Summary via pipeline.SummaryString.
//  4. For non-HITL runs, only Run + DecisionsSummary are populated.
func (s *HITLService) BuildStatus(ctx context.Context, runID string) (*StatusPayload, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("hitl build status: %w", err)
	}

	counts, err := s.decisions.DecisionCountsByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("hitl build status: counts: %w", err)
	}

	pending := counts.TotalScenes - counts.Approved - counts.Rejected
	if pending < 0 {
		pending = 0
	}
	summary := &domain.DecisionSummary{
		ApprovedCount: counts.Approved,
		RejectedCount: counts.Rejected,
		PendingCount:  pending,
	}

	payload := &StatusPayload{
		Run:              run,
		DecisionsSummary: summary,
	}

	inHITL := pipeline.IsHITLStage(run.Stage) && run.Status == domain.StatusWaiting
	if !inHITL {
		return payload, nil
	}

	// Build the current live snapshot for comparison.
	liveDecisions, err := s.decisions.ListByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("hitl build status: list decisions: %w", err)
	}
	liveSnapshot := pipeline.BuildSessionSnapshot(liveDecisions, counts.TotalScenes)

	session, err := s.decisions.GetSession(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("hitl build status: get session: %w", err)
	}

	sceneIndex := pipeline.NextSceneIndex(liveSnapshot.SceneStatuses, counts.TotalScenes)

	// Use the summary value (even if pointer is nil, Summary() needs a concrete value).
	summaryValue := domain.DecisionSummary{}
	if summary != nil {
		summaryValue = *summary
	}

	if session == nil {
		// Defensive: run is in HITL state but no session row. Log Warn and
		// fall back to the live-state summary without a diff. This is the
		// transient-race edge case documented in AC-DOMAIN-ERRORS-HITL.
		s.logger.Warn("hitl session row missing for waiting run",
			"run_id", runID, "stage", string(run.Stage), "status", string(run.Status))
		payload.Summary = pipeline.SummaryString(runID, run.Stage, run.Status, sceneIndex, counts.TotalScenes, summaryValue)
		return payload, nil
	}

	payload.PausedPosition = session

	// Parse the stored snapshot to compute the FR50 diff.
	var oldSnapshot domain.DecisionSnapshot
	if session.SnapshotJSON != "" {
		if err := json.Unmarshal([]byte(session.SnapshotJSON), &oldSnapshot); err != nil {
			// Corrupt snapshot JSON — log Warn and return payload without
			// the diff (defensive; never fail the endpoint over this).
			s.logger.Warn("hitl snapshot unmarshal failed",
				"run_id", runID, "error", err.Error())
			payload.Summary = pipeline.SummaryString(runID, run.Stage, run.Status, sceneIndex, counts.TotalScenes, summaryValue)
			return payload, nil
		}
	}
	// Normalize nil SceneStatuses so empty snapshot_json ('{}') still produces
	// scene_added entries for all live scenes rather than silently disabling diff.
	if oldSnapshot.SceneStatuses == nil {
		oldSnapshot.SceneStatuses = make(map[string]string)
	}
	changes := pipeline.SnapshotDiff(oldSnapshot, liveSnapshot)
	if len(changes) > 0 {
		// Only pass decisions created after T1 to AttachTimestamps so
		// timestamps reflect when the change occurred, not a pre-pause decision.
		payload.ChangesSince = pipeline.AttachTimestamps(changes, filterDecisionsAfterT1(liveDecisions, session.LastInteractionTimestamp))
	}

	// Summary uses the live scene index (NextSceneIndex) so it reflects the
	// current operator position, not the stale T1 position.
	payload.Summary = pipeline.SummaryString(runID, run.Stage, run.Status, sceneIndex, counts.TotalScenes, summaryValue)
	return payload, nil
}

// filterDecisionsAfterT1 returns decisions whose CreatedAt is strictly after t1.
// String comparison is valid because CreatedAt is RFC3339 / SQLite datetime format.
func filterDecisionsAfterT1(decisions []*domain.Decision, t1 string) []*domain.Decision {
	if t1 == "" {
		return decisions
	}
	out := make([]*domain.Decision, 0, len(decisions))
	for _, d := range decisions {
		if d != nil && d.CreatedAt > t1 {
			out = append(out, d)
		}
	}
	return out
}
