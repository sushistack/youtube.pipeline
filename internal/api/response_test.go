package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type testEnvelope struct {
	Version int `json:"version"`
	Data    any `json:"data,omitempty"`
	Error   *struct {
		Code        string `json:"code"`
		Message     string `json:"message"`
		Recoverable bool   `json:"recoverable"`
	} `json:"error,omitempty"`
}

func TestWriteJSON_SuccessEnvelope(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"id": "scp-049-run-1"})

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	env := testutil.ReadJSON[testEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Version, 1)
	if env.Data == nil {
		t.Error("data field is nil")
	}
}

func TestWriteDomainError_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	rec := httptest.NewRecorder()
	writeDomainError(rec, domain.ErrNotFound)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	env := testutil.ReadJSON[testEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Version, 1)
	if env.Error == nil {
		t.Fatal("error field is nil")
	}
	testutil.AssertEqual(t, env.Error.Code, "NOT_FOUND")
	testutil.AssertEqual(t, env.Error.Recoverable, false)
}

func TestWriteDomainError_Conflict(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	rec := httptest.NewRecorder()
	writeDomainError(rec, domain.ErrConflict)

	if rec.Code != 409 {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	env := testutil.ReadJSON[testEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "CONFLICT")
}

func TestWriteDomainError_Validation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	rec := httptest.NewRecorder()
	writeDomainError(rec, domain.ErrValidation)

	testutil.AssertEqual(t, rec.Code, 400)
}
