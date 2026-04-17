package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
)

// maxRequestBodyBytes caps any JSON request body we accept. 64KB is far more
// than any current endpoint needs; resume is a tiny flag, create is a short
// ID. Protects against runaway or malicious payloads.
const maxRequestBodyBytes = 1 << 16

// RunHandler handles all pipeline run lifecycle HTTP endpoints.
// outputDir is sourced from server configuration and never from the client,
// to prevent arbitrary filesystem writes via the API.
type RunHandler struct {
	svc       *service.RunService
	outputDir string
	logger    *slog.Logger
}

// NewRunHandler creates a RunHandler. outputDir must come from server config.
func NewRunHandler(svc *service.RunService, outputDir string, logger *slog.Logger) *RunHandler {
	return &RunHandler{svc: svc, outputDir: outputDir, logger: logger}
}

// createRequest is the request body for POST /api/runs.
// Only scp_id is accepted from the client — output_dir is always server-configured.
type createRequest struct {
	SCPID string `json:"scp_id"`
}

// runResponse is the API representation of a pipeline run.
type runResponse struct {
	ID         string  `json:"id"`
	SCPID      string  `json:"scp_id"`
	Stage      string  `json:"stage"`
	Status     string  `json:"status"`
	RetryCount int     `json:"retry_count"`
	CostUSD    float64 `json:"cost_usd"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

// resumeResponse wraps runResponse with optional warnings populated when
// FS/DB inconsistencies are bypassed via confirm_inconsistent=true.
type resumeResponse struct {
	*runResponse
	Warnings []string `json:"warnings,omitempty"`
}

// runListResponse is the API representation of a run list.
type runListResponse struct {
	Items []*runResponse `json:"items"`
	Total int            `json:"total"`
}

// Create handles POST /api/runs.
func (h *RunHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeJSONBody(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}
	if req.SCPID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "scp_id is required", false)
		return
	}

	run, err := h.svc.Create(r.Context(), req.SCPID, h.outputDir)
	if err != nil {
		h.logger.Error("create run", "error", err)
		writeDomainError(w, err)
		return
	}

	h.logger.Info("run created", "run_id", run.ID, "scp_id", run.SCPID)
	writeJSON(w, http.StatusCreated, toRunResponse(run))
}

// List handles GET /api/runs.
func (h *RunHandler) List(w http.ResponseWriter, r *http.Request) {
	runs, err := h.svc.List(r.Context())
	if err != nil {
		h.logger.Error("list runs", "error", err)
		writeDomainError(w, err)
		return
	}

	items := make([]*runResponse, len(runs))
	for i, run := range runs {
		items[i] = toRunResponse(run)
	}
	writeJSON(w, http.StatusOK, runListResponse{Items: items, Total: len(items)})
}

// Get handles GET /api/runs/{id}.
func (h *RunHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRunResponse(run))
}

// Status handles GET /api/runs/{id}/status.
// For now returns the same detail as Get; full stage-by-stage data is Epic 2.3+.
func (h *RunHandler) Status(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRunResponse(run))
}

// Cancel handles POST /api/runs/{id}/cancel.
func (h *RunHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.svc.Cancel(r.Context(), id); err != nil {
		writeDomainError(w, err)
		return
	}
	run, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	h.logger.Info("run cancelled", "run_id", id)
	writeJSON(w, http.StatusOK, toRunResponse(run))
}

// resumeRequest is the optional request body for POST /api/runs/{id}/resume.
// confirm_inconsistent mirrors the CLI --force flag: when true, the server
// proceeds with the resume even if a filesystem/DB mismatch is detected.
type resumeRequest struct {
	ConfirmInconsistent bool `json:"confirm_inconsistent"`
}

// Resume handles POST /api/runs/{id}/resume.
// Body is optional. Empty body → confirm_inconsistent defaults to false.
// Malformed, too-large, or unknown-field bodies are rejected with 400 so
// clients do not silently fall back to default behavior on typos
// (e.g. {"force":true} instead of {"confirm_inconsistent":true}).
func (h *RunHandler) Resume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body resumeRequest
	if err := decodeJSONBody(r, &body, true); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}

	run, report, err := h.svc.Resume(r.Context(), id, body.ConfirmInconsistent)
	if err != nil {
		h.logger.Error("resume run", "run_id", id, "error", err)
		writeDomainError(w, err)
		return
	}

	h.logger.Info("run resumed", "run_id", id, "stage", run.Stage, "status", run.Status)
	writeJSON(w, http.StatusOK, &resumeResponse{
		runResponse: toRunResponse(run),
		Warnings:    mismatchStrings(report),
	})
}

// decodeJSONBody decodes r.Body into out with three guards:
//  1. max request size (http.MaxBytesReader)
//  2. disallow unknown fields (typo detection — critical for tiny optional
//     bodies like resume's {"confirm_inconsistent":bool} so clients don't
//     silently fall back to default on misspelled field names)
//  3. optional empty-body tolerance via allowEmpty (true for Resume: a
//     POST with no body is valid and means "default options"; false for
//     Create: scp_id is required).
func decodeJSONBody(r *http.Request, out any, allowEmpty bool) error {
	if r.Body == nil {
		if allowEmpty {
			return nil
		}
		return errors.New("request body required")
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) && allowEmpty {
			return nil
		}
		return err
	}
	// Reject trailing garbage after the JSON object.
	if dec.More() {
		return errors.New("request body contains trailing data after JSON object")
	}
	return nil
}

// mismatchStrings renders each Mismatch as a short single-line description
// for the API response. Nil/empty report → nil slice (omitempty elides the field).
func mismatchStrings(report *domain.InconsistencyReport) []string {
	if report == nil || len(report.Mismatches) == 0 {
		return nil
	}
	out := make([]string, 0, len(report.Mismatches))
	for _, m := range report.Mismatches {
		line := m.Kind + "@" + m.Path
		if m.Detail != "" {
			line += ": " + m.Detail
		}
		out = append(out, line)
	}
	return out
}

func toRunResponse(r *domain.Run) *runResponse {
	return &runResponse{
		ID:         r.ID,
		SCPID:      r.SCPID,
		Stage:      string(r.Stage),
		Status:     string(r.Status),
		RetryCount: r.RetryCount,
		CostUSD:    r.CostUSD,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}
