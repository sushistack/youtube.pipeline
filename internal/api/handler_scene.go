package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
)

// SceneHandler handles scenario scene review endpoints.
type SceneHandler struct {
	svc *service.SceneService
}

// NewSceneHandler creates a SceneHandler.
func NewSceneHandler(svc *service.SceneService) *SceneHandler {
	return &SceneHandler{svc: svc}
}

// narrationEditResponse is the slim envelope returned by the inline narration
// edit endpoint. The list endpoint returns the richer reviewItemResponse so
// non-batch surfaces can render scene cards and the read-only DetailPanel
// without a second fetch (see SCL-5 in spec-production-master-detail.md).
type narrationEditResponse struct {
	SceneIndex int    `json:"scene_index"`
	Narration  string `json:"narration"`
}

type reviewItemCriticBreakdownResponse struct {
	AggregateScore     *float64 `json:"aggregate_score,omitempty"`
	HookStrength       *float64 `json:"hook_strength,omitempty"`
	FactAccuracy       *float64 `json:"fact_accuracy,omitempty"`
	EmotionalVariation *float64 `json:"emotional_variation,omitempty"`
	Immersion          *float64 `json:"immersion,omitempty"`
}

type reviewItemPreviousVersionResponse struct {
	Narration string        `json:"narration"`
	Shots     []domain.Shot `json:"shots"`
}

type reviewItemResponse struct {
	SceneIndex             int                                `json:"scene_index"`
	Narration              string                             `json:"narration"`
	Shots                  []domain.Shot                      `json:"shots"`
	TTSPath                *string                            `json:"tts_path,omitempty"`
	TTSDurationMs          *int                               `json:"tts_duration_ms,omitempty"`
	ClipPath               *string                            `json:"clip_path,omitempty"`
	CriticScore            *float64                           `json:"critic_score,omitempty"`
	CriticBreakdown        *reviewItemCriticBreakdownResponse `json:"critic_breakdown,omitempty"`
	ReviewStatus           domain.ReviewStatus                `json:"review_status"`
	ContentFlags           []string                           `json:"content_flags,omitempty"`
	HighLeverage           bool                               `json:"high_leverage"`
	HighLeverageReasonCode string                             `json:"high_leverage_reason_code,omitempty"`
	HighLeverageReason     string                             `json:"high_leverage_reason,omitempty"`
	PreviousVersion        *reviewItemPreviousVersionResponse `json:"previous_version,omitempty"`
	RegenAttempts          int                                `json:"regen_attempts"`
	RetryExhausted         bool                               `json:"retry_exhausted"`
	PriorRejection         *priorRejectionWarningResponse     `json:"prior_rejection,omitempty"`
}

// sceneListResponse is the API list envelope for scene rows. The shape is
// shared with the batch_review surface (reviewItemResponse) so scene cards
// and the read-only DetailPanel can be rendered at any post-Phase-A stage.
type sceneListResponse struct {
	Items []*reviewItemResponse `json:"items"`
	Total int                   `json:"total"`
}

type reviewItemListResponse struct {
	Items []*reviewItemResponse `json:"items"`
	Total int                   `json:"total"`
}

type timelineCursorResponse struct {
	BeforeCreatedAt string `json:"before_created_at"`
	BeforeID        int64  `json:"before_id"`
}

type timelineDecisionResponse struct {
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

type timelineListResponse struct {
	Items      []*timelineDecisionResponse `json:"items"`
	NextCursor *timelineCursorResponse     `json:"next_cursor"`
}

// editNarrationRequest is the request body for POST /api/runs/{id}/scenes/{idx}/edit.
type editNarrationRequest struct {
	Narration string `json:"narration"`
}

type sceneDecisionRequest struct {
	SceneIndex      int             `json:"scene_index"`
	DecisionType    string          `json:"decision_type"`
	ContextSnapshot json.RawMessage `json:"context_snapshot,omitempty"`
	Note            *string         `json:"note,omitempty"`
}

type batchApproveAllRemainingRequest struct {
	FocusSceneIndex int `json:"focus_scene_index"`
}

type priorRejectionWarningResponse struct {
	RunID      string `json:"run_id"`
	SCPID      string `json:"scp_id"`
	SceneIndex int    `json:"scene_index"`
	Reason     string `json:"reason"`
	CreatedAt  string `json:"created_at"`
}

type sceneDecisionResponse struct {
	SceneIndex     int                            `json:"scene_index"`
	DecisionType   string                         `json:"decision_type"`
	NextSceneIndex int                            `json:"next_scene_index"`
	RegenAttempts  int                            `json:"regen_attempts"`
	RetryExhausted bool                           `json:"retry_exhausted"`
	PriorRejection *priorRejectionWarningResponse `json:"prior_rejection,omitempty"`
}

type regenSceneResponse struct {
	SceneIndex     int  `json:"scene_index"`
	RegenAttempts  int  `json:"regen_attempts"`
	RetryExhausted bool `json:"retry_exhausted"`
}

type undoResponse struct {
	UndoneSceneIndex int    `json:"undone_scene_index"`
	UndoneKind       string `json:"undone_kind"`
	FocusTarget      string `json:"focus_target"`
}

type batchApproveAllRemainingResponse struct {
	AggregateCommandID string `json:"aggregate_command_id"`
	ApprovedCount      int    `json:"approved_count"`
	ApprovedSceneIDs   []int  `json:"approved_scene_indices"`
	FocusSceneIndex    int    `json:"focus_scene_index"`
}

// List handles GET /api/runs/{id}/scenes.
// Returns 409 if the run is not paused at scenario_review.
func (h *SceneHandler) List(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	scenes, err := h.svc.ListScenes(r.Context(), runID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	items := make([]*reviewItemResponse, len(scenes))
	for i, ep := range scenes {
		items[i] = toReviewItemResponseFromEpisode(ep)
	}
	writeJSON(w, http.StatusOK, sceneListResponse{Items: items, Total: len(items)})
}

// ListReviewItems handles GET /api/runs/{id}/review-items.
// Returns 409 if the run is not paused at batch_review.
func (h *SceneHandler) ListReviewItems(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	items, err := h.svc.ListReviewItems(r.Context(), runID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	responseItems := make([]*reviewItemResponse, len(items))
	for i, item := range items {
		responseItems[i] = toReviewItemResponse(item)
	}
	writeJSON(w, http.StatusOK, reviewItemListResponse{Items: responseItems, Total: len(responseItems)})
}

// ListDecisions handles GET /api/decisions.
func (h *SceneHandler) ListDecisions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	var decisionType *string
	if raw := strings.TrimSpace(query.Get("decision_type")); raw != "" {
		if !isTimelineDecisionType(raw) {
			writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "decision_type is invalid", false)
			return
		}
		decisionType = &raw
	}

	limit := 100
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "limit must be a positive integer", false)
			return
		}
		limit = parsed
	}

	beforeCreatedAtRaw := strings.TrimSpace(query.Get("before_created_at"))
	beforeIDRaw := strings.TrimSpace(query.Get("before_id"))
	if (beforeCreatedAtRaw == "") != (beforeIDRaw == "") {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "before_created_at and before_id must be provided together", false)
		return
	}

	var (
		beforeCreatedAt *string
		beforeID        *int64
	)
	if beforeCreatedAtRaw != "" {
		if _, err := time.Parse(time.RFC3339, beforeCreatedAtRaw); err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "before_created_at must be an RFC3339 timestamp", false)
			return
		}
		parsed, err := strconv.ParseInt(beforeIDRaw, 10, 64)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "before_id must be a positive integer", false)
			return
		}
		beforeCreatedAt = &beforeCreatedAtRaw
		beforeID = &parsed
	}

	result, err := h.svc.ListDecisionsTimeline(r.Context(), service.TimelineListInput{
		DecisionType:    decisionType,
		Limit:           limit,
		BeforeCreatedAt: beforeCreatedAt,
		BeforeID:        beforeID,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]*timelineDecisionResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, &timelineDecisionResponse{
			ID:                 item.ID,
			RunID:              item.RunID,
			SCPID:              item.SCPID,
			SceneID:            item.SceneID,
			DecisionType:       item.DecisionType,
			Note:               item.Note,
			ReasonFromSnapshot: item.ReasonFromSnapshot,
			SupersededBy:       item.SupersededBy,
			CreatedAt:          item.CreatedAt,
		})
	}

	var nextCursor *timelineCursorResponse
	if result.NextCursor != nil {
		nextCursor = &timelineCursorResponse{
			BeforeCreatedAt: result.NextCursor.BeforeCreatedAt,
			BeforeID:        result.NextCursor.BeforeID,
		}
	}

	writeJSON(w, http.StatusOK, timelineListResponse{
		Items:      items,
		NextCursor: nextCursor,
	})
}

// Edit handles POST /api/runs/{id}/scenes/{idx}/edit.
// Returns 409 if the run is not paused at scenario_review.
// Returns 404 if the scene index does not exist.
// Returns 400 if narration is empty.
func (h *SceneHandler) Edit(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	idxStr := r.PathValue("idx")
	sceneIndex, err := strconv.Atoi(idxStr)
	if err != nil || sceneIndex < 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "scene index must be a non-negative integer", false)
		return
	}

	var req editNarrationRequest
	if err := decodeJSONBody(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}
	if req.Narration == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "narration is required", false)
		return
	}

	if err := h.svc.EditNarration(r.Context(), runID, sceneIndex, req.Narration); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, &narrationEditResponse{SceneIndex: sceneIndex, Narration: req.Narration})
}

// RecordDecision handles POST /api/runs/{id}/decisions.
// Returns 409 if the run is not paused at batch_review.
// Returns 404 if the scene index does not exist.
// Returns 400 for invalid request payloads.
func (h *SceneHandler) RecordDecision(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")

	var req sceneDecisionRequest
	if err := decodeJSONBody(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}
	if req.SceneIndex < 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "scene index must be a non-negative integer", false)
		return
	}
	switch req.DecisionType {
	case domain.DecisionTypeApprove, domain.DecisionTypeReject, domain.DecisionTypeSkipAndRemember:
	default:
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "decision_type must be approve, reject, or skip_and_remember", false)
		return
	}

	var contextSnapshot *string
	if len(req.ContextSnapshot) > 0 && string(req.ContextSnapshot) != "null" {
		raw := string(req.ContextSnapshot)
		contextSnapshot = &raw
	}
	if req.DecisionType == domain.DecisionTypeSkipAndRemember && contextSnapshot == nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "context_snapshot is required for skip_and_remember", false)
		return
	}
	// Story 8.4 AC-2: mirror the service-layer reject-note guard at the
	// HTTP boundary so a missing reason surfaces as a clean 400 without
	// having to parse the wrapped service error string.
	if req.DecisionType == domain.DecisionTypeReject {
		if req.Note == nil || strings.TrimSpace(*req.Note) == "" {
			writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "note is required for reject", false)
			return
		}
	}

	result, err := h.svc.RecordSceneDecision(r.Context(), service.SceneDecisionInput{
		RunID:           runID,
		SceneIndex:      req.SceneIndex,
		DecisionType:    req.DecisionType,
		ContextSnapshot: contextSnapshot,
		Note:            req.Note,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	response := &sceneDecisionResponse{
		SceneIndex:     result.SceneIndex,
		DecisionType:   result.DecisionType,
		NextSceneIndex: result.NextSceneIndex,
		RegenAttempts:  result.RegenAttempts,
		RetryExhausted: result.RetryExhausted,
	}
	if result.PriorRejection != nil {
		response.PriorRejection = &priorRejectionWarningResponse{
			RunID:      result.PriorRejection.RunID,
			SCPID:      result.PriorRejection.SCPID,
			SceneIndex: result.PriorRejection.SceneIndex,
			Reason:     result.PriorRejection.Reason,
			CreatedAt:  result.PriorRejection.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, response)
}

// Regenerate handles POST /api/runs/{id}/scenes/{idx}/regen.
// Returns 409 if the run is not paused at batch_review OR the per-scene
// retry cap (Story 8.4 AC-4) has been reached.
// Returns 400 for invalid scene indexes.
func (h *SceneHandler) Regenerate(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	idxStr := r.PathValue("idx")
	sceneIndex, err := strconv.Atoi(idxStr)
	if err != nil || sceneIndex < 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "scene index must be a non-negative integer", false)
		return
	}
	result, err := h.svc.DispatchSceneRegeneration(r.Context(), runID, sceneIndex)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, &regenSceneResponse{
		SceneIndex:     result.SceneIndex,
		RegenAttempts:  result.RegenAttempts,
		RetryExhausted: result.RetryExhausted,
	})
}

// ApproveAllRemaining handles POST /api/runs/{id}/approve-all-remaining.
// Returns 409 if the run is not paused at batch_review.
func (h *SceneHandler) ApproveAllRemaining(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")

	var req batchApproveAllRemainingRequest
	if err := decodeJSONBody(r, &req, true); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}
	if req.FocusSceneIndex < 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "focus_scene_index must be a non-negative integer", false)
		return
	}

	result, err := h.svc.ApproveAllRemaining(r.Context(), service.BatchApproveAllRemainingInput{
		RunID:           runID,
		FocusSceneIndex: req.FocusSceneIndex,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, &batchApproveAllRemainingResponse{
		AggregateCommandID: result.AggregateCommandID,
		ApprovedCount:      result.ApprovedCount,
		ApprovedSceneIDs:   result.ApprovedSceneIDs,
		FocusSceneIndex:    result.FocusSceneIndex,
	})
}

// Undo handles POST /api/runs/{id}/undo.
// Returns 409 if the run is in Phase C or has no undoable commands.
func (h *SceneHandler) Undo(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	result, err := h.svc.UndoLastDecision(r.Context(), runID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, &undoResponse{
		UndoneSceneIndex: result.UndoneSceneIndex,
		UndoneKind:       result.UndoneKind,
		FocusTarget:      result.FocusTarget,
	})
}

// toReviewItemResponseFromEpisode maps a raw segments-table row to the
// reviewItemResponse shape used at every post-Phase-A surface. Fields that
// require batch_review-only assembly (high_leverage classification, previous
// versions of regenerated scenes, prior-rejection warnings) are intentionally
// left at their zero values — the read-only master/detail panes do not depend
// on them.
func toReviewItemResponseFromEpisode(ep *domain.Episode) *reviewItemResponse {
	narration := ""
	if ep.Narration != nil {
		narration = *ep.Narration
	}
	shots := ep.Shots
	if shots == nil {
		shots = []domain.Shot{}
	}
	flags := ep.SafeguardFlags
	if flags == nil {
		flags = []string{}
	}
	reviewStatus := ep.ReviewStatus
	if reviewStatus == "" {
		reviewStatus = domain.ReviewStatusWaitingForReview
	}
	response := &reviewItemResponse{
		SceneIndex:    ep.SceneIndex,
		Narration:     narration,
		Shots:         shots,
		TTSPath:       ep.TTSPath,
		TTSDurationMs: ep.TTSDurationMs,
		ClipPath:      ep.ClipPath,
		CriticScore:   service.NormalizeOptionalScore(ep.CriticScore),
		ReviewStatus:  reviewStatus,
		ContentFlags:  flags,
	}
	if breakdown := service.ParseCriticBreakdown(ep.CriticScore, ep.CriticSub); breakdown != nil {
		response.CriticBreakdown = &reviewItemCriticBreakdownResponse{
			AggregateScore:     breakdown.AggregateScore,
			HookStrength:       breakdown.HookStrength,
			FactAccuracy:       breakdown.FactAccuracy,
			EmotionalVariation: breakdown.EmotionalVariation,
			Immersion:          breakdown.Immersion,
		}
	}
	return response
}

func isTimelineDecisionType(value string) bool {
	switch value {
	case domain.DecisionTypeApprove,
		domain.DecisionTypeReject,
		domain.DecisionTypeSkipAndRemember,
		domain.DecisionTypeDescriptorEdit,
		domain.DecisionTypeUndo,
		domain.DecisionTypeSystemAutoApproved,
		domain.DecisionTypeOverride:
		return true
	default:
		return false
	}
}

func toReviewItemResponse(item *service.ReviewItem) *reviewItemResponse {
	response := &reviewItemResponse{
		SceneIndex:             item.SceneIndex,
		Narration:              item.Narration,
		Shots:                  item.Shots,
		TTSPath:                item.TTSPath,
		TTSDurationMs:          item.TTSDurationMs,
		ClipPath:               item.ClipPath,
		CriticScore:            item.CriticScore,
		ReviewStatus:           item.ReviewStatus,
		ContentFlags:           item.ContentFlags,
		HighLeverage:           item.HighLeverage,
		HighLeverageReasonCode: item.HighLeverageReasonCode,
		HighLeverageReason:     item.HighLeverageReason,
		RegenAttempts:          item.RegenAttempts,
		RetryExhausted:         item.RetryExhausted,
	}
	if item.CriticBreakdown != nil {
		response.CriticBreakdown = &reviewItemCriticBreakdownResponse{
			AggregateScore:     item.CriticBreakdown.AggregateScore,
			HookStrength:       item.CriticBreakdown.HookStrength,
			FactAccuracy:       item.CriticBreakdown.FactAccuracy,
			EmotionalVariation: item.CriticBreakdown.EmotionalVariation,
			Immersion:          item.CriticBreakdown.Immersion,
		}
	}
	if item.PreviousVersion != nil {
		response.PreviousVersion = &reviewItemPreviousVersionResponse{
			Narration: item.PreviousVersion.Narration,
			Shots:     item.PreviousVersion.Shots,
		}
	}
	if item.PriorRejection != nil {
		response.PriorRejection = &priorRejectionWarningResponse{
			RunID:      item.PriorRejection.RunID,
			SCPID:      item.PriorRejection.SCPID,
			SceneIndex: item.PriorRejection.SceneIndex,
			Reason:     item.PriorRejection.Reason,
			CreatedAt:  item.PriorRejection.CreatedAt,
		}
	}
	return response
}
