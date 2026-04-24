package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/sushistack/youtube.pipeline/internal/service"
)

// TuningService is the narrow interface the handler needs from
// *service.TuningService. Keeping the consumer interface here preserves
// the one-way import direction (api → service interface) and keeps the
// handler testable with a minimal fake.
type TuningService interface {
	GetCriticPrompt(ctx context.Context) (service.CriticPromptEnvelope, error)
	SaveCriticPrompt(ctx context.Context, body string) (service.CriticPromptEnvelope, error)
	GoldenState(ctx context.Context) (service.TuningGoldenState, error)
	RunGolden(ctx context.Context) (service.TuningGoldenReport, error)
	AddGoldenPair(ctx context.Context, positive, negative []byte) (service.TuningGoldenPair, error)
	RunShadow(ctx context.Context) (service.TuningShadowReport, error)
	Calibration(ctx context.Context, window, limit int) (service.TuningCalibration, error)
	FastFeedback(ctx context.Context) (service.FastFeedbackReport, error)
}

// TuningHandler serves /api/tuning/* routes.
type TuningHandler struct {
	svc TuningService
}

// NewTuningHandler wires a TuningHandler to svc.
func NewTuningHandler(svc TuningService) *TuningHandler {
	return &TuningHandler{svc: svc}
}

// maxPromptBodyBytes bounds the PUT body so a malicious or misconfigured
// client cannot submit an unbounded prompt. 256 KiB is comfortably larger
// than any realistic prompt.
const maxPromptBodyBytes = 256 * 1024

// maxFixtureUploadBytes bounds each uploaded fixture. Golden fixtures are
// small narration JSON payloads; 1 MiB each is more than enough.
const maxFixtureUploadBytes = 1 << 20

// maxMultipartBodyBytes caps the entire multipart request body. 4 MiB
// accommodates two 1 MiB fixtures, multipart boundary overhead, and
// headroom for metadata while preventing unbounded disk spill via
// mime/multipart's tempfile fallback for parts that exceed maxMemory.
const maxMultipartBodyBytes = 4 * maxFixtureUploadBytes

// GetPrompt implements GET /api/tuning/critic-prompt.
func (h *TuningHandler) GetPrompt(w http.ResponseWriter, r *http.Request) {
	env, err := h.svc.GetCriticPrompt(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// PutPrompt implements PUT /api/tuning/critic-prompt.
// Request shape: { "body": "<raw markdown>" }.
func (h *TuningHandler) PutPrompt(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxPromptBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body", false)
		return
	}
	env, err := h.svc.SaveCriticPrompt(r.Context(), req.Body)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// GetGolden implements GET /api/tuning/golden.
func (h *TuningHandler) GetGolden(w http.ResponseWriter, r *http.Request) {
	state, err := h.svc.GoldenState(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// RunGolden implements POST /api/tuning/golden/run.
func (h *TuningHandler) RunGolden(w http.ResponseWriter, r *http.Request) {
	report, err := h.svc.RunGolden(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// AddGoldenPair implements POST /api/tuning/golden/pairs.
// Accepts multipart/form-data with two fields: "positive" and "negative".
func (h *TuningHandler) AddGoldenPair(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "expected multipart/form-data", false)
		return
	}
	// Cap the total request body BEFORE parsing so a large multipart body
	// cannot exhaust disk via spill-to-tempfile. ParseMultipartForm's
	// maxMemory only bounds in-memory parts; remaining bytes spill to temp
	// files without a limit unless the body reader enforces one.
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBodyBytes)
	if err := r.ParseMultipartForm(2 * maxFixtureUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid multipart body", false)
		return
	}
	positive, err := readFixturePart(r.MultipartForm, "positive")
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}
	negative, err := readFixturePart(r.MultipartForm, "negative")
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), false)
		return
	}

	meta, err := h.svc.AddGoldenPair(r.Context(), positive, negative)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, meta)
}

// RunShadow implements POST /api/tuning/shadow/run.
func (h *TuningHandler) RunShadow(w http.ResponseWriter, r *http.Request) {
	report, err := h.svc.RunShadow(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// FastFeedback implements POST /api/tuning/fast-feedback.
func (h *TuningHandler) FastFeedback(w http.ResponseWriter, r *http.Request) {
	report, err := h.svc.FastFeedback(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// GetCalibration implements GET /api/tuning/calibration.
func (h *TuningHandler) GetCalibration(w http.ResponseWriter, r *http.Request) {
	window := parseIntQuery(r, "window", 0)
	limit := parseIntQuery(r, "limit", 0)
	payload, err := h.svc.Calibration(r.Context(), window, limit)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// readFixturePart extracts the first file value for the named multipart
// field, capped at maxFixtureUploadBytes. Reads one extra byte to
// distinguish "exactly at the cap" from "overflow"; the +1 pattern is
// paired with an explicit length check so oversized uploads fail loudly
// instead of being silently truncated into malformed JSON.
func readFixturePart(form *multipart.Form, field string) ([]byte, error) {
	files := form.File[field]
	if len(files) == 0 {
		return nil, fmt.Errorf("missing fixture field %q", field)
	}
	f, err := files[0].Open()
	if err != nil {
		return nil, fmt.Errorf("open %s fixture: %w", field, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFixtureUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s fixture: %w", field, err)
	}
	if len(data) > maxFixtureUploadBytes {
		return nil, fmt.Errorf("%s fixture exceeds %d-byte limit", field, maxFixtureUploadBytes)
	}
	return data, nil
}

// parseIntQuery reads an integer query parameter, returning fallback on
// absence or parse failure. Negative values pass through unchanged — the
// service layer clamps them.
func parseIntQuery(r *http.Request, key string, fallback int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}
