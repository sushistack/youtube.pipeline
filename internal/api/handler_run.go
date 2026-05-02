package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/service"
)

// cacheFiles is the single source of truth for the deterministic-agent cache
// stage → on-disk filename mapping. Used by the pending-state cache panel
// (GET /api/runs/{id}/cache) and by the optional drop_caches body of
// POST /api/runs/{id}/advance. Keys must match the strings used by the UI.
var cacheFiles = map[string]string{
	"research":  "research_cache.json",
	"structure": "structure_cache.json",
	"scenario":  "scenario.json",
}

// cacheStageOrder is the canonical iteration / display order for cache
// stages. Map iteration is random in Go, so any caller that needs stable
// ordering (response rows, error messages) ranges this slice and looks up
// filenames in cacheFiles.
var cacheStageOrder = []string{"research", "structure", "scenario"}

// maxRequestBodyBytes caps any JSON request body we accept. 64KB is far more
// than any current endpoint needs; resume is a tiny flag, create is a short
// ID. Protects against runaway or malicious payloads.
const maxRequestBodyBytes = 1 << 16

// RunHandler handles all pipeline run lifecycle HTTP endpoints.
// outputDir is sourced from server configuration and never from the client,
// to prevent arbitrary filesystem writes via the API.
type RunHandler struct {
	svc       *service.RunService
	hitl      *service.HITLService
	outputDir string
	logger    *slog.Logger
}

// NewRunHandler creates a RunHandler. outputDir must come from server config.
// hitl is required; Status delegates to it unconditionally.
func NewRunHandler(svc *service.RunService, hitl *service.HITLService, outputDir string, logger *slog.Logger) *RunHandler {
	return &RunHandler{svc: svc, hitl: hitl, outputDir: outputDir, logger: logger}
}

// createRequest is the request body for POST /api/runs.
// Only scp_id is accepted from the client — output_dir is always server-configured.
type createRequest struct {
	SCPID string `json:"scp_id"`
}

// runResponse is the API representation of a pipeline run.
type runResponse struct {
	ID                  string   `json:"id"`
	SCPID               string   `json:"scp_id"`
	Stage               string   `json:"stage"`
	Status              string   `json:"status"`
	RetryCount          int      `json:"retry_count"`
	RetryReason         *string  `json:"retry_reason,omitempty"`
	CriticScore         *float64 `json:"critic_score,omitempty"`
	CostUSD             float64  `json:"cost_usd"`
	TokenIn             int      `json:"token_in"`
	TokenOut            int      `json:"token_out"`
	DurationMs          int64    `json:"duration_ms"`
	HumanOverride       bool     `json:"human_override"`
	CharacterQueryKey   *string  `json:"character_query_key,omitempty"`
	SelectedCharacterID *string  `json:"selected_character_id,omitempty"`
	FrozenDescriptor    *string  `json:"frozen_descriptor,omitempty"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
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
//
// Response envelope carries the base run plus (when applicable):
//
//	paused_position:                  where the operator left off (FR49)
//	decisions_summary:                approved/rejected/pending counts
//	summary:                          state-aware summary string
//	changes_since_last_interaction:   FR50 diff array (omitted when empty)
//
// Non-HITL runs get just the run field — all other keys are omitted via
// JSON omitempty. Delegates to HITLService.BuildStatus for the full payload.
func (h *RunHandler) Status(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	payload, err := h.hitl.BuildStatus(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// StatusStream handles GET /api/runs/{id}/status/stream.
// Streams status as Server-Sent Events at 1-second intervals until the run
// reaches a terminal state or the client disconnects. Each "data" event carries
// the same {version, data} envelope as the polling endpoint. A "done" event
// signals end of stream so the client can close cleanly without auto-reconnect.
func (h *RunHandler) StatusStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "SSE_UNSUPPORTED", "streaming not supported", false)
		return
	}

	// Disable the server's global WriteTimeout for this long-lived stream.
	// Phase A can take several minutes; the 30s deadline would kill the
	// connection mid-run. Client reconnects (EventSource auto-retry) serve
	// as the effective liveness check instead.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sendOnce := func() (stop bool) {
		if r.Context().Err() != nil {
			return true
		}
		payload, err := h.hitl.BuildStatus(r.Context(), id)
		if err != nil {
			return true
		}
		data, _ := json.Marshal(apiResponse{Version: 1, Data: payload})
		fmt.Fprintf(w, "data: %s\n\n", data) //nolint:errcheck
		flusher.Flush()
		s := payload.Run.Status
		return s == domain.StatusCompleted || s == domain.StatusFailed || s == domain.StatusCancelled
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if sendOnce() {
			if r.Context().Err() == nil {
				fmt.Fprintf(w, "event: done\ndata: {}\n\n") //nolint:errcheck
				flusher.Flush()
			}
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
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

// AcknowledgeMetadata handles POST /api/runs/{id}/metadata/ack.
// No request body. Transitions metadata_ack → complete (NFR-L1 gate).
func (h *RunHandler) AcknowledgeMetadata(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, err := h.svc.AcknowledgeMetadata(r.Context(), runID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRunResponse(run))
}

// ApproveScenarioReview handles POST /api/runs/{id}/scenario/approve.
// No request body. Transitions scenario_review/waiting → character_pick/waiting.
// Returns 409 ErrConflict when the run is not paused at scenario_review,
// 404 ErrNotFound when the run does not exist.
func (h *RunHandler) ApproveScenarioReview(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, err := h.svc.ApproveScenarioReview(r.Context(), runID)
	if err != nil {
		h.logger.Error("approve scenario review", "run_id", runID, "error", err)
		writeDomainError(w, err)
		return
	}
	h.logger.Info("scenario review approved", "run_id", runID, "stage", run.Stage, "status", run.Status)
	writeJSON(w, http.StatusOK, toRunResponse(run))
}

// FinalizeBatchReview handles POST /api/runs/{id}/batch-review/approve.
// No request body. Transitions batch_review/waiting → assemble/waiting once
// every scene has a decision; the operator then dispatches Phase C via
// /advance (manual gate, mirrors image/waiting). Returns 409 ErrConflict
// when the run is not paused at batch_review OR when scenes are still
// pending; 404 ErrNotFound when the run does not exist.
func (h *RunHandler) FinalizeBatchReview(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, err := h.svc.FinalizeBatchReview(r.Context(), runID)
	if err != nil {
		h.logger.Error("finalize batch review", "run_id", runID, "error", err)
		writeDomainError(w, err)
		return
	}
	h.logger.Info("batch review finalized", "run_id", runID, "stage", run.Stage, "status", run.Status)
	writeJSON(w, http.StatusOK, toRunResponse(run))
}

// rewindRequest is the request body for POST /api/runs/{id}/rewind.
// target_stage_node is one of the four operator-clickable work-phase keys:
// "scenario" / "character" / "assets" / "assemble". Rejected with 400 when
// the value is missing or not in the allow-list. The server enforces the
// stage-ordering check separately (target must be strictly before current
// stage) — that surfaces as 409 ErrConflict.
type rewindRequest struct {
	TargetStageNode string `json:"target_stage_node"`
}

// Rewind handles POST /api/runs/{id}/rewind.
// Body: {"target_stage_node": "scenario"|"character"|"assets"|"assemble"}.
// Synchronous — the orchestration cancels in-flight workers, performs all
// DB and on-disk cleanup, and returns the post-rewind run snapshot. Errors:
//   - 400 VALIDATION_ERROR when the body is malformed or the node is
//     unknown (the four-key allow-list is the contract surface).
//   - 404 NOT_FOUND when the run does not exist.
//   - 409 CONFLICT when the rewind target is not strictly before the
//     run's current stage (e.g. clicking "Cast" while still at Cast).
func (h *RunHandler) Rewind(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body rewindRequest
	if err := decodeJSONBody(r, &body, false); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}
	node := pipeline.StageNodeKey(body.TargetStageNode)
	if !node.IsRewindable() {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			fmt.Sprintf("target_stage_node must be one of scenario|character|assets|assemble, got %q", body.TargetStageNode),
			false)
		return
	}

	run, err := h.svc.Rewind(r.Context(), id, node)
	if err != nil {
		h.logger.Error("rewind run", "run_id", id, "node", body.TargetStageNode, "error", err)
		writeDomainError(w, err)
		return
	}
	h.logger.Info("run rewound",
		"run_id", id, "node", body.TargetStageNode,
		"stage", run.Stage, "status", run.Status)
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
//
// Resume runs Phase B (TTS/image, minutes-long) which exceeds the server's
// 30s WriteTimeout. The handler runs PrepareResume synchronously (validation,
// FS/DB consistency check, artifact cleanup, status reset) so 4xx errors come
// back without committing to async work, then dispatches ExecuteResume on a
// detached context and returns 202 Accepted with the post-prepare snapshot.
// The UI observes completion via /status polling. Mirrors Advance's split.
func (h *RunHandler) Resume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body resumeRequest
	if err := decodeJSONBody(r, &body, true); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}

	run, report, err := h.svc.PrepareResume(r.Context(), id, body.ConfirmInconsistent)
	if err != nil {
		h.logger.Error("resume run", "run_id", id, "error", err)
		writeDomainError(w, err)
		return
	}

	go func() {
		if err := h.svc.ExecuteResume(context.Background(), id); err != nil {
			h.logger.Error("resume execute", "run_id", id, "error", err)
		} else {
			h.logger.Info("run resume executed", "run_id", id)
		}
	}()

	h.logger.Info("run resume prepared", "run_id", id, "stage", run.Stage, "status", run.Status)
	writeJSON(w, http.StatusAccepted, &resumeResponse{
		runResponse: toRunResponse(run),
		Warnings:    mismatchStrings(report),
	})
}

// cacheEntryResponse is one row of GET /api/runs/{id}/cache.
// source_version is "" when the file is unparseable as JSON or has no
// source_version key — the entry still surfaces so the operator can decide.
type cacheEntryResponse struct {
	Stage         string `json:"stage"`
	Filename      string `json:"filename"`
	SizeBytes     int64  `json:"size_bytes"`
	ModifiedAt    string `json:"modified_at"`
	SourceVersion string `json:"source_version"`
}

// cacheListResponse is the envelope payload for GET /api/runs/{id}/cache.
// caches is always a non-nil slice; an empty caches array (run dir missing
// or no cached files) is the well-defined "no caches" state and renders as
// `{"caches":[]}` rather than `{"caches":null}`.
type cacheListResponse struct {
	Caches []cacheEntryResponse `json:"caches"`
}

// advanceRequest is the optional request body for POST /api/runs/{id}/advance.
// drop_caches lists the deterministic-agent caches to delete BEFORE Phase A
// dispatches; each entry must be a key of the cacheFiles map. Empty / missing
// → no deletions, preserving the legacy advance behavior verbatim.
type advanceRequest struct {
	DropCaches []string `json:"drop_caches"`
}

// Cache handles GET /api/runs/{id}/cache. Returns the list of deterministic-
// agent caches present on disk for the run, with size, mtime, and the
// embedded source_version. Caches are only meaningful for pending runs (the
// caller gates the fetch on status), but the handler does not gate by status
// — the listing is harmless at any stage.
//
// Returns 404 NOT_FOUND when the run does not exist in the DB. Run-dir
// missing or empty → 200 with an empty caches array.
func (h *RunHandler) Cache(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Verify the run exists; surfaces a typed 404 instead of just returning
	// an empty list for a non-existent run.
	if _, err := h.svc.Get(r.Context(), id); err != nil {
		writeDomainError(w, err)
		return
	}

	runDir := filepath.Join(h.outputDir, id)
	entries := make([]cacheEntryResponse, 0, len(cacheFiles))
	// Iterate in the canonical order researcher → structurer → scenario so
	// the UI sees a stable row order across calls. Map iteration is random,
	// so range cacheStageOrder and look up filenames in cacheFiles.
	for _, stage := range cacheStageOrder {
		filename, ok := cacheFiles[stage]
		if !ok {
			continue
		}
		full := filepath.Join(runDir, filename)
		info, err := os.Stat(full)
		if err != nil {
			// Missing or unreadable → not surfaced; the panel only lists
			// caches that actually exist.
			continue
		}
		entry := cacheEntryResponse{
			Stage:      stage,
			Filename:   filename,
			SizeBytes:  info.Size(),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339Nano),
		}
		// Partial unmarshal: tolerant of any shape so long as source_version
		// is a string. JSON errors → empty source_version, entry still
		// surfaces (operator decides whether to drop).
		if data, readErr := os.ReadFile(full); readErr == nil {
			var probe struct {
				SourceVersion string `json:"source_version"`
			}
			if err := json.Unmarshal(data, &probe); err == nil {
				entry.SourceVersion = probe.SourceVersion
			}
		}
		entries = append(entries, entry)
	}

	writeJSON(w, http.StatusOK, cacheListResponse{Caches: entries})
}

// Advance handles POST /api/runs/{id}/advance. Used by the UI Start-run button
// to kick off a freshly-created pending run (Phase A entry). Resume rejects
// pending status by design (it is the failed/waiting recovery path), so the
// pending → critic transition needs a separate endpoint that maps to the
// engine's automated dispatch. HITL stages are still rejected at the engine.
//
// Optional body {"drop_caches": ["research", ...]}: each entry must be a key
// of cacheFiles; the listed cache files are deleted SYNCHRONOUSLY between
// PrepareAdvance and the goroutine launch so the engine sees a clean slate
// (no race where it writes a fresh cache before deletion lands). Missing
// files are not errors. Empty / no body → existing behavior preserved.
//
// Phase A involves multiple LLM calls that can take several minutes, so the
// handler dispatches the engine in a goroutine and returns 202 Accepted
// immediately. The UI polls GET /api/runs/{id}/status to observe progress.
func (h *RunHandler) Advance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Optional body: empty body / no Content-Length → DropCaches stays nil.
	var body advanceRequest
	if err := decodeJSONBody(r, &body, true); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}

	// Validate every requested stage BEFORE we touch the run or the
	// filesystem. An unknown stage is a typo, not a partial-success scenario;
	// reject the entire request so no files are deleted on bad input.
	for _, stage := range body.DropCaches {
		if _, ok := cacheFiles[stage]; !ok {
			writeError(w, http.StatusBadRequest, "VALIDATION_ERROR",
				fmt.Sprintf("drop_caches contains unknown stage %q (valid: %s)", stage, strings.Join(cacheStageOrder, ", ")),
				false)
			return
		}
	}

	// Validate synchronously: returns typed errors for missing run or
	// unconfigured advancer before we commit to async execution.
	run, err := h.svc.PrepareAdvance(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	// Synchronous deletion BEFORE the goroutine: guarantees the engine sees
	// the post-delete state and lets tests observe deletions immediately
	// after the handler returns. Missing file → not an error (idempotent).
	for _, stage := range body.DropCaches {
		filename := cacheFiles[stage]
		full := filepath.Join(h.outputDir, id, filename)
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			h.logger.Warn("drop cache failed", "run_id", id, "stage", stage, "error", err.Error())
			continue
		}
		h.logger.Info("cache dropped", "run_id", id, "stage", stage, "filename", filename)
	}

	// Phase A runs multiple LLM calls that can take several minutes.
	// Dispatch to a goroutine so the HTTP response is not held open past
	// WriteTimeout. The engine writes success/failure directly to the DB;
	// the UI observes progress by polling GET /api/runs/{id}/status.
	go func() {
		if err := h.svc.ExecuteAdvance(context.Background(), id); err != nil {
			h.logger.Error("advance run", "run_id", id, "error", err)
		} else {
			h.logger.Info("run advanced", "run_id", id)
		}
	}()

	writeJSON(w, http.StatusAccepted, toRunResponse(run))
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

// toRunResponse is used by Create/Get/Cancel/Resume — endpoints where the
// thinner shape is sufficient. Status uses the full *domain.Run via
// HITLService.BuildStatus so cost/token/duration are carried in the response.
func toRunResponse(r *domain.Run) *runResponse {
	return &runResponse{
		ID:                  r.ID,
		SCPID:               r.SCPID,
		Stage:               string(r.Stage),
		Status:              string(r.Status),
		RetryCount:          r.RetryCount,
		RetryReason:         r.RetryReason,
		CriticScore:         r.CriticScore,
		CostUSD:             r.CostUSD,
		TokenIn:             r.TokenIn,
		TokenOut:            r.TokenOut,
		DurationMs:          r.DurationMs,
		HumanOverride:       r.HumanOverride,
		CharacterQueryKey:   r.CharacterQueryKey,
		SelectedCharacterID: r.SelectedCharacterID,
		FrozenDescriptor:    r.FrozenDescriptor,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
	}
}
