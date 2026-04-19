package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

// SceneReader is the read surface SceneService needs from the segment store.
type SceneReader interface {
	ListByRunID(ctx context.Context, runID string) ([]*domain.Episode, error)
}

// NarrationUpdater is the write surface SceneService needs for narration edits.
type NarrationUpdater interface {
	UpdateNarration(ctx context.Context, runID string, sceneIndex int, narration string) error
}

type SceneDecisionRecorder interface {
	ListByRunID(ctx context.Context, runID string) ([]*domain.Decision, error)
	ListTimeline(ctx context.Context, opts db.TimelineListOptions) ([]*db.TimelineDecision, *db.TimelineCursor, error)
	DecisionCountsByRunID(ctx context.Context, runID string) (db.DecisionCounts, error)
	GetSession(ctx context.Context, runID string) (*domain.HITLSession, error)
	UpsertSession(ctx context.Context, session *domain.HITLSession) error
	DeleteSession(ctx context.Context, runID string) error
	RecordSceneDecision(ctx context.Context, runID string, sceneIndex int, decisionType string, contextSnapshot *string, note *string) error
	ApproveAllRemaining(ctx context.Context, runID string, aggregateCommandID string, focusSceneIndex int) (*db.BatchApproveResult, error)
	LatestUndoableDecision(ctx context.Context, runID string) (*db.UndoableDecision, error)
	ApplyUndo(ctx context.Context, runID string, originalDecisionID int64) (*db.UndoApplication, error)
	PriorRejectionForScene(ctx context.Context, runID string, sceneIndex int) (*db.PriorRejection, error)
	CountRegenAttempts(ctx context.Context, runID string, sceneIndex int) (int, error)
}

// SceneRegenerator dispatches a regeneration job for a single scene. The
// dispatcher owns the mechanics of refreshing narration / shots / tts; this
// layer only enforces the retry cap and triggers the dispatch.
type SceneRegenerator interface {
	DispatchRegeneration(ctx context.Context, runID string, sceneIndex int) error
}

// MaxSceneRegenAttempts is the hard cap from AC-4. After this many
// operator-triggered regeneration retries for a single scene within a run,
// the next /regen request is rejected with ErrConflict and the client must
// fall back to manual edit / skip & flag.
const MaxSceneRegenAttempts = 2

type sceneDecisionSessionAdapter struct {
	store SceneDecisionRecorder
}

func (a sceneDecisionSessionAdapter) ListByRunID(ctx context.Context, runID string) ([]*domain.Decision, error) {
	return a.store.ListByRunID(ctx, runID)
}

func (a sceneDecisionSessionAdapter) DecisionCountsByRunID(ctx context.Context, runID string) (pipeline.DecisionCounts, error) {
	counts, err := a.store.DecisionCountsByRunID(ctx, runID)
	if err != nil {
		return pipeline.DecisionCounts{}, err
	}
	return pipeline.DecisionCounts{
		Approved:    counts.Approved,
		Rejected:    counts.Rejected,
		TotalScenes: counts.TotalScenes,
	}, nil
}

func (a sceneDecisionSessionAdapter) GetSession(ctx context.Context, runID string) (*domain.HITLSession, error) {
	return a.store.GetSession(ctx, runID)
}

func (a sceneDecisionSessionAdapter) UpsertSession(ctx context.Context, session *domain.HITLSession) error {
	return a.store.UpsertSession(ctx, session)
}

func (a sceneDecisionSessionAdapter) DeleteSession(ctx context.Context, runID string) error {
	return a.store.DeleteSession(ctx, runID)
}

// SceneService implements scenario scene list and narration edit business logic.
type SceneService struct {
	runs     RunStore
	segments interface {
		SceneReader
		NarrationUpdater
	}
	decisions   SceneDecisionRecorder
	regenerator SceneRegenerator
	clock       clock.Clock
}

// SetSceneRegenerator wires a regeneration dispatcher for batch-review scene
// retries. Nil disables the /regen path (DispatchSceneRegeneration returns an
// error). Separated from NewSceneService so existing call sites (tests, CLI)
// do not need the dispatcher to compile.
func (s *SceneService) SetSceneRegenerator(r SceneRegenerator) {
	s.regenerator = r
}

// RegenSegmentStore is the minimal segment surface the V1 stub regenerator
// needs to restore a rejected scene to an actionable state.
type RegenSegmentStore interface {
	GetByRunIDAndSceneIndex(ctx context.Context, runID string, sceneIndex int) (*domain.Episode, error)
	UpdateReviewGate(ctx context.Context, runID string, sceneIndex int, reviewStatus domain.ReviewStatus, safeguardFlags []string) error
}

// NoOpSceneRegenerator is the V1 stub that lands a rejected scene back in
// `waiting_for_review` so the next operator reject cycle can proceed. Real
// regeneration (Phase B clean-slate refresh of shots/images/tts) remains the
// responsibility of Story 5.4; once Phase B is wired into /serve, this stub
// is replaced with the async dispatcher. The stub preserves existing
// safeguard_flags so content flags survive the retry.
type NoOpSceneRegenerator struct {
	segments RegenSegmentStore
}

// NewNoOpSceneRegenerator constructs the V1 stub regenerator.
func NewNoOpSceneRegenerator(segments RegenSegmentStore) *NoOpSceneRegenerator {
	return &NoOpSceneRegenerator{segments: segments}
}

// DispatchRegeneration flips the scene's review_status back to
// waiting_for_review, preserving its existing safeguard_flags. Clients should
// treat /regen success as "the scene is queued for a fresh review pass" and
// refresh /review-items to pick up the restored state.
func (r *NoOpSceneRegenerator) DispatchRegeneration(ctx context.Context, runID string, sceneIndex int) error {
	segment, err := r.segments.GetByRunIDAndSceneIndex(ctx, runID, sceneIndex)
	if err != nil {
		return fmt.Errorf("stub regenerator %s[%d]: load segment: %w", runID, sceneIndex, err)
	}
	// Guard against /regen being invoked on a segment that isn't currently
	// rejected — e.g., a client bug or direct API call that would otherwise
	// downgrade an approved scene back to waiting_for_review with no audit
	// trail. The retry cap alone can't catch this because attempts=0 passes
	// the gate trivially.
	if segment.ReviewStatus != domain.ReviewStatusRejected {
		return fmt.Errorf("stub regenerator %s[%d]: segment status %q is not rejected: %w",
			runID, sceneIndex, segment.ReviewStatus, domain.ErrConflict)
	}
	flags := segment.SafeguardFlags
	if flags == nil {
		flags = []string{}
	}
	if err := r.segments.UpdateReviewGate(ctx, runID, sceneIndex, domain.ReviewStatusWaitingForReview, flags); err != nil {
		return fmt.Errorf("stub regenerator %s[%d]: reset review gate: %w", runID, sceneIndex, err)
	}
	return nil
}

// UndoResult is the response payload from UndoLastDecision.
type UndoResult struct {
	UndoneSceneIndex int    `json:"undone_scene_index"`
	UndoneKind       string `json:"undone_kind"`
	FocusTarget      string `json:"focus_target"`
}

type SceneDecisionInput struct {
	RunID           string
	SceneIndex      int
	DecisionType    string
	ContextSnapshot *string
	Note            *string
}

// PriorRejectionWarning is the FR53 advisory surfaced when a reject lands on
// a scene (same scp_id + scene_index) that was already rejected in a
// different run. Purely informational — never blocks the decision write.
type PriorRejectionWarning struct {
	RunID      string `json:"run_id"`
	SCPID      string `json:"scp_id"`
	SceneIndex int    `json:"scene_index"`
	Reason     string `json:"reason"`
	CreatedAt  string `json:"created_at"`
}

type SceneDecisionResult struct {
	SceneIndex     int                    `json:"scene_index"`
	DecisionType   string                 `json:"decision_type"`
	NextSceneIndex int                    `json:"next_scene_index"`
	RegenAttempts  int                    `json:"regen_attempts"`
	RetryExhausted bool                   `json:"retry_exhausted"`
	PriorRejection *PriorRejectionWarning `json:"prior_rejection,omitempty"`
}

// RegenResult is returned by DispatchSceneRegeneration after a successful
// dispatch. RegenAttempts reflects the post-dispatch attempt count so the
// client can derive the retry-exhausted state without an extra round-trip.
type RegenResult struct {
	SceneIndex     int  `json:"scene_index"`
	RegenAttempts  int  `json:"regen_attempts"`
	RetryExhausted bool `json:"retry_exhausted"`
}

type BatchApproveAllRemainingInput struct {
	RunID           string
	FocusSceneIndex int
}

type BatchApproveAllRemainingResult struct {
	AggregateCommandID string `json:"aggregate_command_id"`
	ApprovedCount      int    `json:"approved_count"`
	ApprovedSceneIDs   []int  `json:"approved_scene_indices"`
	FocusSceneIndex    int    `json:"focus_scene_index"`
}

type TimelineCursorResponse struct {
	BeforeCreatedAt string `json:"before_created_at"`
	BeforeID        int64  `json:"before_id"`
}

type TimelineListInput struct {
	DecisionType    *string
	Limit           int
	BeforeCreatedAt *string
	BeforeID        *int64
}

type TimelineDecisionResponse struct {
	ID                 int64   `json:"id"`
	RunID              string  `json:"run_id"`
	SCPID              string  `json:"scp_id"`
	SceneID            *string `json:"scene_id"`
	DecisionType       string  `json:"decision_type"`
	Note               *string `json:"note"`
	ReasonFromSnapshot *string `json:"reason_from_snapshot"`
	SupersededBy       *int64  `json:"superseded_by"`
	CreatedAt          string  `json:"created_at"`
}

type TimelineListResult struct {
	Items      []*TimelineDecisionResponse `json:"items"`
	NextCursor *TimelineCursorResponse     `json:"next_cursor"`
}

type ReviewItemCriticBreakdown struct {
	AggregateScore     *float64 `json:"aggregate_score,omitempty"`
	HookStrength       *float64 `json:"hook_strength,omitempty"`
	FactAccuracy       *float64 `json:"fact_accuracy,omitempty"`
	EmotionalVariation *float64 `json:"emotional_variation,omitempty"`
	Immersion          *float64 `json:"immersion,omitempty"`
}

type ReviewItemPreviousVersion struct {
	Narration string        `json:"narration"`
	Shots     []domain.Shot `json:"shots"`
}

type ReviewItem struct {
	SceneIndex             int                        `json:"scene_index"`
	Narration              string                     `json:"narration"`
	Shots                  []domain.Shot              `json:"shots"`
	TTSPath                *string                    `json:"tts_path,omitempty"`
	TTSDurationMs          *int                       `json:"tts_duration_ms,omitempty"`
	ClipPath               *string                    `json:"clip_path,omitempty"`
	CriticScore            *float64                   `json:"critic_score,omitempty"`
	CriticBreakdown        *ReviewItemCriticBreakdown `json:"critic_breakdown,omitempty"`
	ReviewStatus           domain.ReviewStatus        `json:"review_status"`
	ContentFlags           []string                   `json:"content_flags,omitempty"`
	HighLeverage           bool                       `json:"high_leverage"`
	HighLeverageReasonCode string                     `json:"high_leverage_reason_code,omitempty"`
	HighLeverageReason     string                     `json:"high_leverage_reason,omitempty"`
	PreviousVersion        *ReviewItemPreviousVersion `json:"previous_version,omitempty"`
	// RegenAttempts is the count of non-superseded reject decisions on this
	// (run_id, scene_index). The client uses this to gate reject/regen vs
	// the retry-exhausted state (Story 8.4 AC-4).
	RegenAttempts  int                    `json:"regen_attempts"`
	RetryExhausted bool                   `json:"retry_exhausted"`
	PriorRejection *PriorRejectionWarning `json:"prior_rejection,omitempty"`
}

// NewSceneService constructs a SceneService.
func NewSceneService(runs RunStore, segments interface {
	SceneReader
	NarrationUpdater
}, decisions SceneDecisionRecorder, clk clock.Clock) *SceneService {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &SceneService{runs: runs, segments: segments, decisions: decisions, clock: clk}
}

// ListScenes returns all segments for a run in scene_index order.
// Returns CONFLICT if the run is not currently paused at scenario_review.
func (s *SceneService) ListScenes(ctx context.Context, runID string) ([]*domain.Episode, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("scene list: %w", err)
	}
	if run.Stage != domain.StageScenarioReview || run.Status != domain.StatusWaiting {
		return nil, fmt.Errorf("scene list: run is not paused at scenario_review: %w", domain.ErrConflict)
	}
	scenes, err := s.segments.ListByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("scene list: %w", err)
	}
	return scenes, nil
}

// ListReviewItems returns the batch-review surface payload.
// Returns CONFLICT if the run is not currently paused at batch_review.
func (s *SceneService) ListReviewItems(ctx context.Context, runID string) ([]*ReviewItem, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("review items: %w", err)
	}
	if run.Stage != domain.StageBatchReview || run.Status != domain.StatusWaiting {
		return nil, fmt.Errorf("review items: run is not paused at batch_review: %w", domain.ErrConflict)
	}
	if run.ScenarioPath == nil || *run.ScenarioPath == "" {
		return nil, fmt.Errorf("review items: run has no scenario path: %w", domain.ErrNotFound)
	}

	scenarioScenes, err := loadNarrationScenes(*run.ScenarioPath)
	if err != nil {
		return nil, fmt.Errorf("review items: %w", err)
	}

	segments, err := s.segments.ListByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("review items: %w", err)
	}

	classifications := computeHighLeverage(sceneHighLeverageInput{
		runSCPID: run.SCPID,
		scenes:   scenarioScenes,
	})

	items := make([]*ReviewItem, 0, len(segments))
	for _, segment := range segments {
		classification := classifications[segment.SceneIndex]
		attempts := 0
		var priorWarning *PriorRejectionWarning
		if s.decisions != nil {
			count, err := s.decisions.CountRegenAttempts(ctx, runID, segment.SceneIndex)
			if err != nil {
				return nil, fmt.Errorf("review items: count regen attempts for scene %d: %w", segment.SceneIndex, err)
			}
			attempts = count
			prior, err := s.decisions.PriorRejectionForScene(ctx, runID, segment.SceneIndex)
			if err != nil {
				return nil, fmt.Errorf("review items: prior rejection for scene %d: %w", segment.SceneIndex, err)
			}
			if prior != nil {
				priorWarning = &PriorRejectionWarning{
					RunID:      prior.RunID,
					SCPID:      prior.SCPID,
					SceneIndex: prior.SceneIndex,
					Reason:     prior.Reason,
					CreatedAt:  prior.CreatedAt,
				}
			}
		}
		items = append(items, &ReviewItem{
			SceneIndex:             segment.SceneIndex,
			Narration:              derefString(segment.Narration),
			Shots:                  normalizeShots(segment.Shots),
			TTSPath:                segment.TTSPath,
			TTSDurationMs:          segment.TTSDurationMs,
			ClipPath:               normalizeOptionalPath(segment.ClipPath),
			CriticScore:            normalizeOptionalScore(segment.CriticScore),
			CriticBreakdown:        parseCriticBreakdown(segment.CriticScore, segment.CriticSub),
			ReviewStatus:           normalizedReviewStatus(segment.ReviewStatus),
			ContentFlags:           append([]string(nil), segment.SafeguardFlags...),
			HighLeverage:           classification.HighLeverage,
			HighLeverageReasonCode: classification.ReasonCode,
			HighLeverageReason:     classification.Reason,
			PreviousVersion:        parsePreviousVersion(segment.SafeguardFlags),
			RegenAttempts:          attempts,
			RetryExhausted:         attempts >= MaxSceneRegenAttempts,
			PriorRejection:         priorWarning,
		})
	}

	slices.SortStableFunc(items, compareReviewItems)
	return items, nil
}

func (s *SceneService) ListDecisionsTimeline(
	ctx context.Context,
	input TimelineListInput,
) (*TimelineListResult, error) {
	if s.decisions == nil {
		return nil, fmt.Errorf("list decisions timeline: decisions store not configured")
	}

	items, cursor, err := s.decisions.ListTimeline(ctx, db.TimelineListOptions{
		DecisionType:    input.DecisionType,
		Limit:           input.Limit,
		BeforeCreatedAt: input.BeforeCreatedAt,
		BeforeID:        input.BeforeID,
	})
	if err != nil {
		return nil, fmt.Errorf("list decisions timeline: %w", err)
	}

	responseItems := make([]*TimelineDecisionResponse, 0, len(items))
	for _, item := range items {
		responseItems = append(responseItems, &TimelineDecisionResponse{
			ID:                 item.ID,
			RunID:              item.RunID,
			SCPID:              item.SCPID,
			SceneID:            item.SceneID,
			DecisionType:       item.DecisionType,
			Note:               item.Note,
			ReasonFromSnapshot: extractTimelineReason(item.ContextSnapshot),
			SupersededBy:       item.SupersededBy,
			CreatedAt:          item.CreatedAt,
		})
	}

	var nextCursor *TimelineCursorResponse
	if cursor != nil {
		nextCursor = &TimelineCursorResponse{
			BeforeCreatedAt: cursor.BeforeCreatedAt,
			BeforeID:        cursor.BeforeID,
		}
	}

	return &TimelineListResult{
		Items:      responseItems,
		NextCursor: nextCursor,
	}, nil
}

// EditNarration updates the narration text for a specific scene.
// Returns CONFLICT if the run is not currently paused at scenario_review.
// Returns NOT_FOUND if the scene index does not exist.
// Returns VALIDATION_ERROR if narration is empty.
func (s *SceneService) EditNarration(ctx context.Context, runID string, sceneIndex int, narration string) error {
	if narration == "" {
		return fmt.Errorf("edit narration: %w: narration is required", domain.ErrValidation)
	}
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return fmt.Errorf("edit narration: %w", err)
	}
	if run.Stage != domain.StageScenarioReview || run.Status != domain.StatusWaiting {
		return fmt.Errorf("edit narration: run is not paused at scenario_review: %w", domain.ErrConflict)
	}
	if err := s.segments.UpdateNarration(ctx, runID, sceneIndex, narration); err != nil {
		return fmt.Errorf("edit narration: %w", err)
	}
	return nil
}

func (s *SceneService) RecordSceneDecision(ctx context.Context, input SceneDecisionInput) (*SceneDecisionResult, error) {
	if s.decisions == nil {
		return nil, fmt.Errorf("record scene decision: decisions store not configured")
	}
	if input.SceneIndex < 0 {
		return nil, fmt.Errorf("record scene decision: %w: scene_index must be non-negative", domain.ErrValidation)
	}
	switch input.DecisionType {
	case domain.DecisionTypeApprove, domain.DecisionTypeReject, domain.DecisionTypeSkipAndRemember:
	default:
		return nil, fmt.Errorf("record scene decision: %w: invalid decision type", domain.ErrValidation)
	}
	// The client must still mark skip intent by sending some
	// context_snapshot payload — it is treated as a hint only, because
	// the service re-assembles the canonical snapshot below from
	// server-held segment state (T8).
	if input.DecisionType == domain.DecisionTypeSkipAndRemember && input.ContextSnapshot == nil {
		return nil, fmt.Errorf("record scene decision: %w: context_snapshot is required for skip decisions", domain.ErrValidation)
	}
	// Story 8.4 AC-2: reject requires a non-empty reason so the decision
	// ledger carries actionable context into any FR53 cross-run warning.
	if input.DecisionType == domain.DecisionTypeReject {
		if input.Note == nil || strings.TrimSpace(*input.Note) == "" {
			return nil, fmt.Errorf("record scene decision: %w: note is required for reject", domain.ErrValidation)
		}
	}

	run, err := s.runs.Get(ctx, input.RunID)
	if err != nil {
		return nil, fmt.Errorf("record scene decision: %w", err)
	}
	if run.Stage != domain.StageBatchReview || run.Status != domain.StatusWaiting {
		return nil, fmt.Errorf("record scene decision: run is not paused at batch_review: %w", domain.ErrConflict)
	}

	// Assemble canonical context_snapshot at the write boundary so every
	// caller emits the same shape and undo metadata is always present.
	// For skip: build from server state (Story 8.2). For approve/reject:
	// add a minimal snapshot with command_kind so the undo log is traceable.
	contextSnapshot := input.ContextSnapshot
	switch input.DecisionType {
	case domain.DecisionTypeSkipAndRemember:
		serverSnapshot, err := s.buildSkipSnapshot(ctx, input.RunID, input.SceneIndex)
		if err != nil {
			return nil, fmt.Errorf("record scene decision: build skip snapshot: %w", err)
		}
		contextSnapshot = &serverSnapshot
	case domain.DecisionTypeApprove, domain.DecisionTypeReject:
		if contextSnapshot == nil {
			snap := buildDecisionSnapshot(input.DecisionType, input.SceneIndex)
			contextSnapshot = &snap
		}
	}

	if err := s.decisions.RecordSceneDecision(
		ctx,
		input.RunID,
		input.SceneIndex,
		input.DecisionType,
		contextSnapshot,
		input.Note,
	); err != nil {
		return nil, fmt.Errorf("record scene decision: %w", err)
	}
	session, err := pipeline.UpsertSessionFromState(
		ctx,
		sceneDecisionSessionAdapter{store: s.decisions},
		s.clock,
		input.RunID,
		run.Stage,
		run.Status,
	)
	if err != nil {
		return nil, fmt.Errorf("record scene decision: upsert session: %w", err)
	}

	nextSceneIndex := input.SceneIndex
	if session != nil {
		nextSceneIndex = session.SceneIndex
	}

	result := &SceneDecisionResult{
		SceneIndex:     input.SceneIndex,
		DecisionType:   input.DecisionType,
		NextSceneIndex: nextSceneIndex,
	}

	// Only reject carries retry/regen semantics. Approve/skip leave the
	// fields at their zero value.
	if input.DecisionType == domain.DecisionTypeReject {
		attempts, err := s.decisions.CountRegenAttempts(ctx, input.RunID, input.SceneIndex)
		if err != nil {
			return nil, fmt.Errorf("record scene decision: count regen attempts: %w", err)
		}
		result.RegenAttempts = attempts
		result.RetryExhausted = attempts > MaxSceneRegenAttempts
		prior, err := s.decisions.PriorRejectionForScene(ctx, input.RunID, input.SceneIndex)
		if err != nil {
			return nil, fmt.Errorf("record scene decision: prior rejection lookup: %w", err)
		}
		if prior != nil {
			result.PriorRejection = &PriorRejectionWarning{
				RunID:      prior.RunID,
				SCPID:      prior.SCPID,
				SceneIndex: prior.SceneIndex,
				Reason:     prior.Reason,
				CreatedAt:  prior.CreatedAt,
			}
		}
	}

	return result, nil
}

// DispatchSceneRegeneration is invoked by the client right after a reject to
// trigger the bounded retry loop (AC-3, AC-4). Sequence:
//  1. validate stage/status is batch_review+waiting
//  2. count prior non-superseded rejects (the regen counter)
//  3. enforce MaxSceneRegenAttempts — return ErrConflict on overflow
//  4. call the injected regenerator (Phase B stub in V1)
//  5. refresh the HITL session so status polling stays coherent
func (s *SceneService) DispatchSceneRegeneration(ctx context.Context, runID string, sceneIndex int) (*RegenResult, error) {
	if s.decisions == nil {
		return nil, fmt.Errorf("dispatch regeneration: decisions store not configured")
	}
	if s.regenerator == nil {
		return nil, fmt.Errorf("dispatch regeneration: regenerator not configured")
	}
	if sceneIndex < 0 {
		return nil, fmt.Errorf("dispatch regeneration: %w: scene_index must be non-negative", domain.ErrValidation)
	}
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("dispatch regeneration: %w", err)
	}
	if run.Stage != domain.StageBatchReview || run.Status != domain.StatusWaiting {
		return nil, fmt.Errorf("dispatch regeneration: run is not paused at batch_review: %w", domain.ErrConflict)
	}
	attempts, err := s.decisions.CountRegenAttempts(ctx, runID, sceneIndex)
	if err != nil {
		return nil, fmt.Errorf("dispatch regeneration: count attempts: %w", err)
	}
	// AC-4: a reject decision has already been recorded before /regen is
	// invoked, so `attempts` includes the current attempt. Allow up to
	// MaxSceneRegenAttempts dispatches total (attempts<=2); block the third.
	if attempts > MaxSceneRegenAttempts {
		return nil, fmt.Errorf("dispatch regeneration: retry cap reached: %w", domain.ErrConflict)
	}
	if err := s.regenerator.DispatchRegeneration(ctx, runID, sceneIndex); err != nil {
		return nil, fmt.Errorf("dispatch regeneration: %w", err)
	}
	if _, err := pipeline.UpsertSessionFromState(
		ctx,
		sceneDecisionSessionAdapter{store: s.decisions},
		s.clock,
		runID,
		run.Stage,
		run.Status,
	); err != nil {
		// The regeneration dispatch itself succeeded — AC-5 says polling
		// should reconcile via the next /review-items refresh. Log and
		// return success so the operator can continue reviewing.
		slog.Warn("dispatch regeneration: session refresh failed after successful dispatch",
			"run_id", runID, "scene_index", sceneIndex, "error", err)
	}
	return &RegenResult{
		SceneIndex:     sceneIndex,
		RegenAttempts:  attempts,
		RetryExhausted: attempts >= MaxSceneRegenAttempts,
	}, nil
}

func randomAggregateSuffix() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand on Linux reads from getrandom(2); a failure here means
		// the kernel entropy source is unavailable, which is not recoverable
		// inside this request. Fall back to a deterministic-but-ordered marker
		// so the caller still gets a non-empty suffix.
		return "rnd"
	}
	return hex.EncodeToString(buf[:])
}

func (s *SceneService) ApproveAllRemaining(ctx context.Context, input BatchApproveAllRemainingInput) (*BatchApproveAllRemainingResult, error) {
	if s.decisions == nil {
		return nil, fmt.Errorf("approve all remaining: decisions store not configured")
	}

	run, err := s.runs.Get(ctx, input.RunID)
	if err != nil {
		return nil, fmt.Errorf("approve all remaining: %w", err)
	}
	if run.Stage != domain.StageBatchReview || run.Status != domain.StatusWaiting {
		return nil, fmt.Errorf("approve all remaining: run is not paused at batch_review: %w", domain.ErrConflict)
	}

	// Collision-resistant id: time-ordered prefix for debuggability, plus 8
	// random bytes so two calls in the same nanosecond (fake clocks, rapid
	// successive approvals) never share an aggregate id.
	aggregateCommandID := fmt.Sprintf(
		"%s-batch-approve-%d-%s",
		input.RunID,
		s.clock.Now().UnixNano(),
		randomAggregateSuffix(),
	)
	batchResult, err := s.decisions.ApproveAllRemaining(ctx, input.RunID, aggregateCommandID, input.FocusSceneIndex)
	if err != nil {
		return nil, fmt.Errorf("approve all remaining: %w", err)
	}

	if _, err := pipeline.UpsertSessionFromState(
		ctx,
		sceneDecisionSessionAdapter{store: s.decisions},
		s.clock,
		input.RunID,
		run.Stage,
		run.Status,
	); err != nil {
		return nil, fmt.Errorf("approve all remaining: upsert session: %w", err)
	}

	return &BatchApproveAllRemainingResult{
		AggregateCommandID: batchResult.AggregateCommandID,
		ApprovedCount:      batchResult.ApprovedCount,
		ApprovedSceneIDs:   append([]int(nil), batchResult.ApprovedSceneIDs...),
		FocusSceneIndex:    batchResult.FocusSceneIndex,
	}, nil
}

// UndoLastDecision reverses the most recent undoable operator decision for a
// run. Returns ErrConflict when the run has entered Phase C (assemble or later)
// or when there is no undoable command. The caller must re-query review items
// after a successful undo to refresh the UI.
func (s *SceneService) UndoLastDecision(ctx context.Context, runID string) (*UndoResult, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("undo last decision: %w", err)
	}
	if !domain.IsPrePhaseC(run.Stage, run.Status) {
		return nil, fmt.Errorf("undo last decision: run stage %q is not undoable: %w", run.Stage, domain.ErrConflict)
	}

	target, err := s.decisions.LatestUndoableDecision(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("undo last decision: %w", err)
	}
	if target == nil {
		return nil, fmt.Errorf("undo last decision: no undoable command: %w", domain.ErrConflict)
	}

	applied, err := s.decisions.ApplyUndo(ctx, runID, target.ID)
	if err != nil {
		return nil, fmt.Errorf("undo last decision: %w", err)
	}

	if _, sessionErr := pipeline.UpsertSessionFromState(
		ctx,
		sceneDecisionSessionAdapter{store: s.decisions},
		s.clock,
		runID,
		run.Stage,
		run.Status,
	); sessionErr != nil {
		// The DB undo already committed; returning an error here would
		// cause the client to think undo failed while the DB reflects the
		// reversed state. Log the failure and let the next write rebuild
		// the session from the now-correct DB rows.
		slog.Warn("undo last decision: session refresh failed after committed undo",
			"run_id", runID, "error", sessionErr)
	}

	kind := applied.CommandKind
	if kind == "" {
		kind = decisionTypeToCommandKind(applied.DecisionType)
	}
	focusTarget := "scene-card"
	if applied.DecisionType == domain.DecisionTypeDescriptorEdit {
		focusTarget = "descriptor"
	}
	return &UndoResult{
		UndoneSceneIndex: applied.SceneIndex,
		UndoneKind:       kind,
		FocusTarget:      focusTarget,
	}, nil
}

// buildDecisionSnapshot constructs the minimal context_snapshot for approve/
// reject decisions so command_kind is always present in the audit trail.
func buildDecisionSnapshot(decisionType string, sceneIndex int) string {
	kind := decisionTypeToCommandKind(decisionType)
	raw, _ := json.Marshal(map[string]any{
		"command_kind": kind,
		"scene_index":  sceneIndex,
	})
	return string(raw)
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
	}
	return decisionType
}

type scenarioEnvelope struct {
	Narration *domain.NarrationScript `json:"narration"`
}

type reviewItemClassification struct {
	HighLeverage bool
	ReasonCode   string
	Reason       string
}

type sceneHighLeverageInput struct {
	runSCPID string
	scenes   []domain.NarrationScene
}

func loadNarrationScenes(path string) ([]domain.NarrationScene, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("scenario.json missing at %s: %w", path, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("read scenario.json: %w", err)
	}

	var envelope scenarioEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode scenario.json: %w", err)
	}
	if envelope.Narration == nil {
		return nil, fmt.Errorf("scenario.json missing narration payload: %w", domain.ErrNotFound)
	}
	return envelope.Narration.Scenes, nil
}

func extractTimelineReason(contextSnapshot *string) *string {
	if contextSnapshot == nil || strings.TrimSpace(*contextSnapshot) == "" {
		return nil
	}
	var payload struct {
		Reason *string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(*contextSnapshot), &payload); err != nil {
		return nil
	}
	if payload.Reason == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*payload.Reason)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func computeHighLeverage(input sceneHighLeverageInput) map[int]reviewItemClassification {
	out := make(map[int]reviewItemClassification, len(input.scenes))
	seenActs := make(map[string]bool)
	entityLabel := normalizeEntityLabel(input.runSCPID)
	entityTitle := formatEntityTitle(input.runSCPID)
	firstAppearanceAssigned := false

	for _, scene := range input.scenes {
		sceneIndex := scene.SceneNum - 1
		if sceneIndex < 0 {
			continue
		}
		reasons := make([]reviewItemClassification, 0, 3)

		if !firstAppearanceAssigned && sceneContainsPrimaryEntity(scene, entityLabel) {
			firstAppearanceAssigned = true
			reasons = append(reasons, reviewItemClassification{
				HighLeverage: true,
				ReasonCode:   "first_appearance",
				Reason:       fmt.Sprintf("First appearance of %s", entityTitle),
			})
		}

		if !seenActs[scene.ActID] {
			seenActs[scene.ActID] = true
			if isHookAct(scene.ActID) || scene.SceneNum == 1 {
				reasons = append(reasons, reviewItemClassification{
					HighLeverage: true,
					ReasonCode:   "hook_scene",
					Reason:       "Opening hook scene",
				})
			} else {
				reasons = append(reasons, reviewItemClassification{
					HighLeverage: true,
					ReasonCode:   "act_boundary",
					Reason:       fmt.Sprintf("Act boundary: %s", scene.ActID),
				})
			}
		}

		if len(reasons) == 0 {
			out[sceneIndex] = reviewItemClassification{}
			continue
		}
		texts := make([]string, 0, len(reasons))
		for _, r := range reasons {
			texts = append(texts, r.Reason)
		}
		out[sceneIndex] = reviewItemClassification{
			HighLeverage: true,
			ReasonCode:   reasons[0].ReasonCode,
			Reason:       strings.Join(texts, "; "),
		}
	}

	return out
}

func sceneContainsPrimaryEntity(scene domain.NarrationScene, entityLabel string) bool {
	if scene.EntityVisible {
		return true
	}
	for _, character := range scene.CharactersPresent {
		if normalizeEntityLabel(character) == entityLabel {
			return true
		}
	}
	return false
}

func normalizeEntityLabel(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	trimmed = strings.ReplaceAll(trimmed, "scp-", "")
	trimmed = strings.ReplaceAll(trimmed, "scp", "")
	return trimmed
}

func formatEntityTitle(scpID string) string {
	trimmed := strings.TrimSpace(scpID)
	if trimmed == "" {
		return "primary entity"
	}
	if strings.HasPrefix(strings.ToUpper(trimmed), "SCP-") {
		return trimmed
	}
	return "SCP-" + trimmed
}

func isHookAct(actID string) bool {
	lowered := strings.ToLower(strings.TrimSpace(actID))
	return strings.Contains(lowered, "hook") || strings.Contains(lowered, "opening")
}

// buildSkipSnapshot assembles the queryable `skip_and_remember` payload
// from server-held segment state. Keys are sorted alphabetically so the
// serialized JSON is byte-stable across callers, and every field named
// in AC-3 is always present (possibly null) so `json_extract` queries
// never miss a column.
func (s *SceneService) buildSkipSnapshot(ctx context.Context, runID string, sceneIndex int) (string, error) {
	segments, err := s.segments.ListByRunID(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("list segments: %w", err)
	}
	var segment *domain.Episode
	for _, ep := range segments {
		if ep == nil || ep.SceneIndex != sceneIndex {
			continue
		}
		segment = ep
		break
	}
	if segment == nil {
		return "", fmt.Errorf("scene %d: %w", sceneIndex, domain.ErrNotFound)
	}
	contentFlags := append([]string(nil), segment.SafeguardFlags...)
	if contentFlags == nil {
		contentFlags = []string{}
	}
	payload := map[string]any{
		"action_source":        "batch_review",
		"content_flags":        contentFlags,
		"critic_score":         normalizeOptionalScore(segment.CriticScore),
		"critic_sub":           parseCriticBreakdown(segment.CriticScore, segment.CriticSub),
		"review_status_before": string(segment.ReviewStatus),
		"scene_index":          sceneIndex,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal snapshot: %w", err)
	}
	return string(raw), nil
}

func parseCriticBreakdown(aggregate *float64, raw *string) *ReviewItemCriticBreakdown {
	if aggregate == nil && raw == nil {
		return nil
	}

	breakdown := &ReviewItemCriticBreakdown{
		AggregateScore: normalizeOptionalScore(aggregate),
	}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return breakdown
	}

	var payload struct {
		HookStrength       *float64 `json:"hook_strength"`
		FactAccuracy       *float64 `json:"fact_accuracy"`
		EmotionalVariation *float64 `json:"emotional_variation"`
		Immersion          *float64 `json:"immersion"`
	}
	if err := json.Unmarshal([]byte(*raw), &payload); err != nil {
		// Downstream analytics (skip_and_remember snapshots) cannot
		// distinguish "no sub-scores" from "corrupted sub-scores"
		// without a log; emit a warning so ops can spot bad rows.
		slog.Warn("parseCriticBreakdown: invalid critic_sub JSON", "error", err)
		return breakdown
	}

	breakdown.HookStrength = normalizeOptionalScore(payload.HookStrength)
	breakdown.FactAccuracy = normalizeOptionalScore(payload.FactAccuracy)
	breakdown.EmotionalVariation = normalizeOptionalScore(payload.EmotionalVariation)
	breakdown.Immersion = normalizeOptionalScore(payload.Immersion)
	return breakdown
}

func normalizeShots(shots []domain.Shot) []domain.Shot {
	if len(shots) == 0 {
		return []domain.Shot{}
	}
	return append([]domain.Shot(nil), shots...)
}

func normalizeOptionalPath(value *string) *string {
	if value == nil {
		return nil
	}
	if strings.TrimSpace(*value) == "" {
		return nil
	}
	return value
}

// normalizeOptionalScore assumes writers use a 0-1 scale (critic outputs)
// and rescales to 0-100 for UI consumption. Any value in [0,1] is rescaled;
// values outside that range are treated as already on the 0-100 scale and
// merely clamped. See deferred-work: 8.1 review flagged this heuristic as
// ambiguous at the exact 1.0 boundary; resolve when upstream writers agree
// on a single scale. Do NOT change this silently — it will skew every
// stored critic score for legacy rows.
func normalizeOptionalScore(value *float64) *float64 {
	if value == nil {
		return nil
	}
	scaled := *value
	if scaled >= 0 && scaled <= 1 {
		scaled *= 100
	}
	if scaled < 0 {
		scaled = 0
	}
	if scaled > 100 {
		scaled = 100
	}
	return &scaled
}

func normalizedReviewStatus(status domain.ReviewStatus) domain.ReviewStatus {
	if status == "" {
		return domain.ReviewStatusWaitingForReview
	}
	return status
}

func parsePreviousVersion(flags []string) *ReviewItemPreviousVersion {
	for _, flag := range flags {
		raw := strings.TrimSpace(flag)
		if !strings.HasPrefix(raw, "previous_version:") {
			continue
		}
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "previous_version:"))
		if raw == "" {
			continue
		}

		var payload ReviewItemPreviousVersion
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			continue
		}
		payload.Shots = normalizeShots(payload.Shots)
		return &payload
	}
	return nil
}

func compareReviewItems(left, right *ReviewItem) int {
	leftBucket := reviewBucket(left)
	rightBucket := reviewBucket(right)
	if leftBucket != rightBucket {
		return leftBucket - rightBucket
	}
	return left.SceneIndex - right.SceneIndex
}

func reviewBucket(item *ReviewItem) int {
	switch item.ReviewStatus {
	case domain.ReviewStatusWaitingForReview, domain.ReviewStatusPending:
		if item.HighLeverage {
			return 0
		}
		return 1
	case domain.ReviewStatusRejected, domain.ReviewStatusApproved:
		return 2
	case domain.ReviewStatusAutoApproved:
		return 3
	default:
		return 2
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
