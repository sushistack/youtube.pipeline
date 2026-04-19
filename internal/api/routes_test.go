package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestRegisterRoutes_SeparatesAPIAndSPAFallback(t *testing.T) {
	testutil.BlockExternalHTTP(t)

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

	mux := http.NewServeMux()
	RegisterRoutes(mux, &Dependencies{
		Run:       NewRunHandler(runService, nil, t.TempDir(), logger),
		Character: NewCharacterHandler(nil),
		Logger:    logger,
		WebFS: fstest.MapFS{
			"dist/index.html": &fstest.MapFile{Data: []byte("<!doctype html><html><body>shell</body></html>")},
		},
	})

	apiReq := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	apiReq.Host = "127.0.0.1:8080"
	apiRec := httptest.NewRecorder()
	mux.ServeHTTP(apiRec, apiReq)

	if apiRec.Code != http.StatusOK {
		t.Fatalf("api status = %d, want %d", apiRec.Code, http.StatusOK)
	}
	if got := apiRec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("api content-type = %q, want application/json", got)
	}
	if strings.Contains(apiRec.Body.String(), "<!doctype html>") {
		t.Fatalf("api response unexpectedly served SPA HTML: %q", apiRec.Body.String())
	}

	spaReq := httptest.NewRequest(http.MethodGet, "/production", nil)
	spaRec := httptest.NewRecorder()
	mux.ServeHTTP(spaRec, spaReq)

	if spaRec.Code != http.StatusOK {
		t.Fatalf("spa status = %d, want %d", spaRec.Code, http.StatusOK)
	}
	if !strings.Contains(spaRec.Body.String(), "shell") {
		t.Fatalf("spa body = %q, want SPA fallback", spaRec.Body.String())
	}
}
