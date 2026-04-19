package api

import (
	"net/http"
	"strconv"

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

// sceneResponse is the API representation of a single scene/segment.
type sceneResponse struct {
	SceneIndex int     `json:"scene_index"`
	Narration  string  `json:"narration"`
}

// sceneListResponse is the API list envelope for scene rows.
type sceneListResponse struct {
	Items []*sceneResponse `json:"items"`
	Total int              `json:"total"`
}

// editNarrationRequest is the request body for POST /api/runs/{id}/scenes/{idx}/edit.
type editNarrationRequest struct {
	Narration string `json:"narration"`
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
	items := make([]*sceneResponse, len(scenes))
	for i, ep := range scenes {
		items[i] = toSceneResponse(ep)
	}
	writeJSON(w, http.StatusOK, sceneListResponse{Items: items, Total: len(items)})
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
	writeJSON(w, http.StatusOK, &sceneResponse{SceneIndex: sceneIndex, Narration: req.Narration})
}

func toSceneResponse(ep *domain.Episode) *sceneResponse {
	narration := ""
	if ep.Narration != nil {
		narration = *ep.Narration
	}
	return &sceneResponse{
		SceneIndex: ep.SceneIndex,
		Narration:  narration,
	}
}
