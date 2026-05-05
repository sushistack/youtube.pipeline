package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
)

// ScpImageHandler exposes the canonical-image library: per-run lookup,
// generate/regenerate, and the static file endpoint used by the UI to
// preview the canonical PNG.
type ScpImageHandler struct {
	svc         *service.ScpImageService
	scpImageDir string
}

// NewScpImageHandler wires the service and the on-disk root for static serving.
// scpImageDir is the same path the service writes files under and is treated
// as the trust boundary — every served path is rejoined and verified to live
// inside this directory.
func NewScpImageHandler(svc *service.ScpImageService, scpImageDir string) *ScpImageHandler {
	return &ScpImageHandler{svc: svc, scpImageDir: scpImageDir}
}

// scpCanonicalResponse is the on-the-wire shape returned by the per-run
// canonical endpoints. ImageURL is a server-relative path to the static
// serve route — the FE drops it directly into <img src>.
//
// SourceCandidateID is exposed so the UI's "이대로 사용" reuse flow can
// re-issue /characters/pick on a fresh run without forcing the operator to
// redo the DDG search — the candidate ID survives in the shared
// character_search_cache row keyed on SourceQueryKey.
type scpCanonicalResponse struct {
	ScpID             string `json:"scp_id"`
	FilePath          string `json:"file_path"`
	ImageURL          string `json:"image_url"`
	SourceQueryKey    string `json:"source_query_key"`
	SourceCandidateID string `json:"source_candidate_id"`
	FrozenDescriptor  string `json:"frozen_descriptor"`
	Seed              int64  `json:"seed"`
	PromptUsed        string `json:"prompt_used"`
	Version           int    `json:"version"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type generateCanonicalRequest struct {
	Regenerate       bool   `json:"regenerate"`
	CandidateID      string `json:"candidate_id,omitempty"`
	FrozenDescriptor string `json:"frozen_descriptor,omitempty"`
}

// Get handles GET /api/runs/{id}/characters/canonical.
// Returns 404 when the run's SCP_ID has no canonical record yet.
func (h *ScpImageHandler) Get(w http.ResponseWriter, r *http.Request) {
	rec, err := h.svc.GetByRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scpCanonicalResponseFromRecord(rec))
}

// Generate handles POST /api/runs/{id}/characters/canonical.
// Body: {"regenerate": bool}. When regenerate is false and the library has
// a hit, returns the existing record without invoking the image provider.
// On a fresh generate, calls Edit and persists the file + library row.
func (h *ScpImageHandler) Generate(w http.ResponseWriter, r *http.Request) {
	var req generateCanonicalRequest
	// allowEmpty=true: clients may POST with no body to mean regenerate=false.
	if err := decodeJSONBody(r, &req, true); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}
	rec, err := h.svc.Generate(r.Context(), r.PathValue("id"), service.GenerateCanonicalInput{
		Regenerate:       req.Regenerate,
		CandidateID:      req.CandidateID,
		FrozenDescriptor: req.FrozenDescriptor,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scpCanonicalResponseFromRecord(rec))
}

// Static handles GET /api/scp_images/{scp_id}.
// Validates the SCP_ID shape, joins under scpImageDir, and refuses any
// resolved path that escapes the configured root. Returns 400 on invalid
// shape, 404 when no canonical exists, 200 image/png on hit.
func (h *ScpImageHandler) Static(w http.ResponseWriter, r *http.Request) {
	scpID := r.PathValue("scp_id")
	if !service.IsValidSCPID(scpID) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid scp_id", false)
		return
	}
	resolved, err := h.resolveCanonicalPath(scpID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid scp_id", false)
		return
	}
	f, err := os.Open(resolved)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// Cache-Control: short max-age + must-revalidate. The canonical changes
	// only on regenerate; ETag from ModTime is sufficient for invalidation.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, scpID+".png", stat.ModTime(), f)
}

// resolveCanonicalPath joins scpImageDir with the canonical filename and
// verifies the resolved path stays inside scpImageDir. Belt-and-braces against
// any future SCP_ID validator regression.
func (h *ScpImageHandler) resolveCanonicalPath(scpID string) (string, error) {
	root, err := filepath.Abs(h.scpImageDir)
	if err != nil {
		return "", err
	}
	candidate, err := filepath.Abs(filepath.Join(root, scpID, "canonical.png"))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", err
	}
	if rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return "", os.ErrPermission
	}
	return candidate, nil
}

func scpCanonicalResponseFromRecord(rec *domain.ScpImageRecord) scpCanonicalResponse {
	return scpCanonicalResponse{
		ScpID:             rec.ScpID,
		FilePath:          rec.FilePath,
		ImageURL:          "/api/scp_images/" + rec.ScpID,
		SourceQueryKey:    rec.SourceQueryKey,
		SourceCandidateID: rec.SourceCandidateID,
		FrozenDescriptor:  rec.FrozenDescriptor,
		Seed:              rec.Seed,
		PromptUsed:        rec.PromptUsed,
		Version:           rec.Version,
		CreatedAt:         rec.CreatedAt,
		UpdatedAt:         rec.UpdatedAt,
	}
}
