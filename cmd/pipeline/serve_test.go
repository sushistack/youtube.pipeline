package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestConfigureServeMux_ProductionServesEmbeddedSPA(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	deps := newServeTestDependencies(t)
	deps.WebFS = fstest.MapFS{
		"dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html><html><body>prod shell</body></html>")},
	}

	mux := http.NewServeMux()
	if err := configureServeMux(mux, deps, false, mustParseURL(viteDevServerURL), nil); err != nil {
		t.Fatalf("configure serve mux: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/production", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "prod shell") {
		t.Fatalf("body = %q, want production SPA shell", rec.Body.String())
	}
}

func TestConfigureServeMux_DevModeKeepsAPIAndProxiesFrontend(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var proxied int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxied, 1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("vite:" + r.URL.Path))
	}))
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream url: %v", err)
	}

	deps := newServeTestDependencies(t)
	deps.WebFS = fstest.MapFS{
		"dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html><html><body>prod shell</body></html>")},
	}

	mux := http.NewServeMux()
	var output strings.Builder
	if err := configureServeMux(mux, deps, true, target, &output); err != nil {
		t.Fatalf("configure serve mux: %v", err)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	apiReq.Host = "127.0.0.1:8080"
	apiRec := httptest.NewRecorder()
	mux.ServeHTTP(apiRec, apiReq)

	if apiRec.Code != http.StatusOK {
		t.Fatalf("api status = %d, want %d", apiRec.Code, http.StatusOK)
	}
	if got := atomic.LoadInt32(&proxied); got != 0 {
		t.Fatalf("api request should not hit frontend proxy, got %d proxied requests", got)
	}

	spaReq := httptest.NewRequest(http.MethodGet, "/production", nil)
	spaRec := httptest.NewRecorder()
	mux.ServeHTTP(spaRec, spaReq)

	if spaRec.Code != http.StatusOK {
		t.Fatalf("spa status = %d, want %d", spaRec.Code, http.StatusOK)
	}
	if body := spaRec.Body.String(); body != "vite:/production" {
		t.Fatalf("spa body = %q, want proxied frontend response", body)
	}
	if got := atomic.LoadInt32(&proxied); got != 1 {
		t.Fatalf("expected 1 proxied frontend request, got %d", got)
	}
	if !strings.Contains(output.String(), "Go serves /api/*") {
		t.Fatalf("output = %q, want dev routing message", output.String())
	}
}

func newServeTestDependencies(t *testing.T) *api.Dependencies {
	t.Helper()

	testDB := testutil.NewTestDB(t)
	logger, _ := testutil.CaptureLog(t)
	runStore := db.NewRunStore(testDB)
	runService := service.NewRunService(runStore, nil)

	run, err := runService.Create(context.Background(), "049", t.TempDir())
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run == nil {
		t.Fatal("create run returned nil")
	}

	return &api.Dependencies{
		Run:       api.NewRunHandler(runService, nil, t.TempDir(), logger),
		Character: api.NewCharacterHandler(nil),
		Logger:    logger,
	}
}
