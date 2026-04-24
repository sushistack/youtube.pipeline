package api

import (
	"encoding/json"
	"net/http"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// apiResponse is the versioned JSON envelope for all API responses.
type apiResponse struct {
	Version int       `json:"version"`
	Data    any       `json:"data,omitempty"`
	Error   *apiError `json:"error,omitempty"`
}

// apiError carries classified error details in the response envelope.
type apiError struct {
	Code        string `json:"code"`
	Details     any    `json:"details,omitempty"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

// writeJSON writes a versioned success envelope with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiResponse{Version: 1, Data: data}) //nolint:errcheck
}

// writeError maps an error to an HTTP status via domain.Classify and writes
// a versioned error envelope. Use the raw variant for non-domain errors.
func writeError(w http.ResponseWriter, status int, code, msg string, recoverable bool) {
	writeErrorWithDetails(w, status, code, msg, recoverable, nil)
}

func writeErrorWithDetails(w http.ResponseWriter, status int, code, msg string, recoverable bool, details any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiResponse{ //nolint:errcheck
		Version: 1,
		Error:   &apiError{Code: code, Details: details, Message: msg, Recoverable: recoverable},
	})
}

// writeDomainError uses domain.Classify to derive HTTP status from the error chain.
// Internal error details (5xx) are NEVER included in the response body — only
// logged server-side. Client-facing messages are derived from the domain code
// to prevent leakage of SQL/FS internals.
func writeDomainError(w http.ResponseWriter, err error) {
	status, code, recoverable := domain.Classify(err)
	writeError(w, status, code, clientMessage(status, code), recoverable)
}

// clientMessage returns a safe, user-facing message for an API error.
// For client errors (4xx) it uses the domain code's canonical message; for
// server errors (5xx) it returns a generic string so internal details never
// reach the HTTP body.
func clientMessage(status int, code string) string {
	if status >= 500 {
		return "internal server error"
	}
	switch code {
	case "NOT_FOUND":
		return "resource not found"
	case "VALIDATION_ERROR":
		return "request validation failed"
	case "CONFLICT":
		return "resource state conflict"
	case "COST_CAP_EXCEEDED":
		return "cost cap exceeded"
	case "RATE_LIMITED":
		return "rate limited"
	default:
		return "request failed"
	}
}
