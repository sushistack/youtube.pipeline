package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestWithRequestID_AddsHeader(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	handler := api.WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-ID")
	if id == "" {
		t.Error("X-Request-ID header not set")
	}
	// UUID v4 format: 8-4-4-4-12 hex chars
	if len(id) != 36 {
		t.Errorf("X-Request-ID length = %d, want 36", len(id))
	}
}

func TestWithRecover_CatchesPanic(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	handler := api.WithRecover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestWithCORS_SetsHeaders(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	handler := api.WithCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	origin := rec.Header().Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", origin)
	}
}

func TestWithCORS_OPTIONS(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	handler := api.WithCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called for OPTIONS")
	}))

	req := httptest.NewRequest("OPTIONS", "/api/runs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want 204", rec.Code)
	}
}

func TestChain_AppliesMiddlewareInOrder(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var order []string
	mkMiddleware := func(name string) api.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+"-before")
				next.ServeHTTP(w, r)
				order = append(order, name+"-after")
			})
		}
	}

	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	})

	handler := api.Chain(base, mkMiddleware("A"), mkMiddleware("B"))
	req := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	got := strings.Join(order, ",")
	want := "A-before,B-before,handler,B-after,A-after"
	if got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}
