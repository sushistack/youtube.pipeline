package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/service"
)

const settingsETagHeader = "ETag"

type SettingsHandler struct {
	svc *service.SettingsService
}

func NewSettingsHandler(svc *service.SettingsService) *SettingsHandler {
	return &SettingsHandler{svc: svc}
}

func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "settings service unavailable", false)
		return
	}
	snapshot, err := h.svc.Snapshot(r.Context())
	if err != nil {
		if errors.Is(err, config.ErrCorruptedConfig) {
			writeErrorWithDetails(
				w,
				http.StatusUnprocessableEntity,
				"SETTINGS_CORRUPTED",
				"config.yaml is unreadable; reset to defaults from the UI or fix it on disk",
				true,
				nil,
			)
			return
		}
		writeDomainError(w, err)
		return
	}
	w.Header().Set(settingsETagHeader, etagFromVersion(snapshot.Application.EffectiveVersion))
	writeJSON(w, http.StatusOK, snapshot)
}

func (h *SettingsHandler) Put(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "settings service unavailable", false)
		return
	}

	ifMatch, ok := parseIfMatch(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid If-Match header", false)
		return
	}

	var req service.SettingsUpdateInput
	if err := decodeJSONBody(r, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "request body is not a valid settings payload", false)
		return
	}

	snapshot, err := h.svc.Save(r.Context(), req, ifMatch)
	if err != nil {
		var validationErr *service.SettingsValidationError
		if errors.As(err, &validationErr) {
			writeErrorWithDetails(
				w,
				http.StatusBadRequest,
				"VALIDATION_ERROR",
				clientMessage(http.StatusBadRequest, "VALIDATION_ERROR"),
				false,
				validationErr.FieldErrors,
			)
			return
		}
		if errors.Is(err, service.ErrSettingsConflict) {
			writeError(
				w,
				http.StatusConflict,
				"SETTINGS_STALE",
				"settings changed since you loaded this page; refresh and retry",
				true,
			)
			return
		}
		if errors.Is(err, config.ErrCorruptedConfig) {
			writeErrorWithDetails(
				w,
				http.StatusUnprocessableEntity,
				"SETTINGS_CORRUPTED",
				"config.yaml is unreadable; reset to defaults from the UI or fix it on disk",
				true,
				nil,
			)
			return
		}
		writeDomainError(w, err)
		return
	}
	w.Header().Set(settingsETagHeader, etagFromVersion(snapshot.Application.EffectiveVersion))
	writeJSON(w, http.StatusOK, snapshot)
}

// ResetToDefaults rewrites config.yaml with domain.DefaultConfig(). Invoked
// by the UI "reset" action when config.yaml is corrupted beyond what GET can
// parse. The .env file is left untouched because secrets should never be
// reset via this path.
func (h *SettingsHandler) ResetToDefaults(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "settings service unavailable", false)
		return
	}
	snapshot, err := h.svc.ResetToDefaults(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	w.Header().Set(settingsETagHeader, etagFromVersion(snapshot.Application.EffectiveVersion))
	writeJSON(w, http.StatusOK, snapshot)
}

// parseIfMatch returns (expectedVersion, true) if the header is either
// absent (nil) or a valid version integer. An unparseable header fails
// validation so clients can't accidentally bypass concurrency checks.
func parseIfMatch(r *http.Request) (*int64, bool) {
	raw := r.Header.Get("If-Match")
	if raw == "" {
		return nil, true
	}
	// Strip quotes if the client sent the canonical ETag form like "42".
	stripped := raw
	if len(stripped) >= 2 && stripped[0] == '"' && stripped[len(stripped)-1] == '"' {
		stripped = stripped[1 : len(stripped)-1]
	}
	if stripped == "" || stripped == "0" {
		// Explicit "0" means "I expect no effective version yet."
		return nil, true
	}
	v, err := strconv.ParseInt(stripped, 10, 64)
	if err != nil || v < 0 {
		return nil, false
	}
	return &v, true
}

func etagFromVersion(version *int64) string {
	if version == nil {
		return `"0"`
	}
	return fmt.Sprintf("%q", strconv.FormatInt(*version, 10))
}

