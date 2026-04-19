package api

import (
	"net/http"

	"github.com/sushistack/youtube.pipeline/internal/service"
)

type CharacterHandler struct {
	svc *service.CharacterService
}

func NewCharacterHandler(svc *service.CharacterService) *CharacterHandler {
	return &CharacterHandler{svc: svc}
}

type pickCharacterRequest struct {
	CandidateID string `json:"candidate_id"`
}

func (h *CharacterHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "query is required", false)
		return
	}
	group, err := h.svc.Search(r.Context(), r.PathValue("id"), query)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, group)
}

func (h *CharacterHandler) Pick(w http.ResponseWriter, r *http.Request) {
	var req pickCharacterRequest
	if err := decodeJSONBody(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}
	if req.CandidateID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "candidate_id is required", false)
		return
	}
	run, err := h.svc.Pick(r.Context(), r.PathValue("id"), req.CandidateID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRunResponse(run))
}
