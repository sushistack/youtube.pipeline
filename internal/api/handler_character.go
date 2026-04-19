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
	CandidateID      string `json:"candidate_id"`
	FrozenDescriptor string `json:"frozen_descriptor"`
}

type descriptorPrefillResponse struct {
	Auto  string  `json:"auto"`
	Prior *string `json:"prior"`
}

// Search handles GET /api/runs/{id}/characters.
// When the "query" parameter is empty, Search returns the cached CharacterGroup
// associated with the run's active character_query_key (404 if the run has
// never searched before). This supports restoring the grid on page reload
// without re-issuing an external DDG call.
func (h *CharacterHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	runID := r.PathValue("id")
	if query == "" {
		group, err := h.svc.GetCandidatesByRun(r.Context(), runID)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, group)
		return
	}
	group, err := h.svc.Search(r.Context(), runID, query)
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
	run, err := h.svc.Pick(r.Context(), r.PathValue("id"), req.CandidateID, req.FrozenDescriptor)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRunResponse(run))
}

// Descriptor handles GET /api/runs/{id}/characters/descriptor.
// Returns the auto (artifact-derived) descriptor and the prior-run descriptor
// (when any other completed run for the same SCP ID has one). The frontend
// decides precedence: prior overrides auto when present (UX-DR62).
func (h *CharacterHandler) Descriptor(w http.ResponseWriter, r *http.Request) {
	prefill, err := h.svc.GetDescriptorPrefill(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, descriptorPrefillResponse{
		Auto:  prefill.Auto,
		Prior: prefill.Prior,
	})
}
