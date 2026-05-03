// Package domain — HITL session types (Story 2.6, FR49 + FR50).
//
// A HITL session is the durable pause state of a human-in-the-loop review:
// it records where the operator left off so the system can replay the exact
// decision point on re-entry. Persistence: one row per run in the
// hitl_sessions table (migration 018); the row is upserted on every decision
// or pause event and deleted when the run exits HITL state (status moves
// away from "waiting" or the run is cancelled).
package domain

// HITLSession is the durable pause state of a HITL review session.
type HITLSession struct {
	RunID                    string `json:"run_id"`
	Stage                    Stage  `json:"stage"`
	SceneIndex               int    `json:"scene_index"`
	LastInteractionTimestamp string `json:"last_interaction_timestamp"`
	// SnapshotJSON stores the JSON-encoded DecisionSnapshot captured at T1.
	// Internal only — NEVER exposed via API (json:"-") because the diff is
	// the user-facing product; the raw blob is an implementation detail.
	SnapshotJSON string `json:"-"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// DecisionSnapshot is the JSON payload persisted in HITLSession.SnapshotJSON.
// It captures per-scene review status at the instant of the last operator
// interaction, so the FR50 diff engine can produce a before/after report
// by comparing this snapshot to the current state.
type DecisionSnapshot struct {
	TotalScenes   int               `json:"total_scenes"`
	ApprovedCount int               `json:"approved_count"`
	RejectedCount int               `json:"rejected_count"`
	PendingCount  int               `json:"pending_count"`
	SceneStatuses map[string]string `json:"scene_statuses"`
}

// DecisionSummary is the lightweight triplet surfaced in the status
// response (FR49). Derived from the current state, NOT from the snapshot.
type DecisionSummary struct {
	ApprovedCount int `json:"approved_count"`
	RejectedCount int `json:"rejected_count"`
	PendingCount  int `json:"pending_count"`
}
