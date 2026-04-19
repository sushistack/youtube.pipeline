package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestSPAHandler_ServesExistingAsset(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	handler := spaHandler(testSPAAssetsFS())
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "console.log('embedded app');" {
		t.Fatalf("body = %q", body)
	}
}

func TestSPAHandler_FallsBackToIndexForClientRoute(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	handler := spaHandler(testSPAAssetsFS())
	req := httptest.NewRequest(http.MethodGet, "/production", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "<div id=\"root\"></div>") {
		t.Fatalf("body = %q, want index.html fallback", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("content-type = %q, want text/html", got)
	}
}

func TestSPAHandler_MissingAssetReturnsNotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	handler := spaHandler(testSPAAssetsFS())
	req := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func testSPAAssetsFS() fs.FS {
	return fstest.MapFS{
		"dist/index.html":    &fstest.MapFile{Data: []byte("<!doctype html><html><body><div id=\"root\"></div></body></html>")},
		"dist/assets/app.js": &fstest.MapFile{Data: []byte("console.log('embedded app');")},
	}
}
