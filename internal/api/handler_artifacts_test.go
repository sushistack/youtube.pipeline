package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// newArtifactTestHandler wires a handler with a run seeded at the given
// stage/status and creates the requested artifact files in the output dir.
// Returns the handler and the actual run ID synthesized by RunStore.Create.
func newArtifactTestHandler(t *testing.T, stage, status string, filenames ...string) (*api.ArtifactsHandler, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	outDir := t.TempDir()

	ctx := context.Background()
	run, err := svc.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("seed create run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET stage = ?, status = ? WHERE id = ?`,
		stage, status, run.ID,
	); err != nil {
		t.Fatalf("seed stage/status: %v", err)
	}

	runDir := filepath.Join(outDir, run.ID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("create run dir: %v", err)
	}
	for _, f := range filenames {
		if err := os.WriteFile(filepath.Join(runDir, f), []byte("test"), 0644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	return api.NewArtifactsHandler(svc, outDir), run.ID
}

func TestArtifactsHandler_Video_Success(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, runID := newArtifactTestHandler(t, "metadata_ack", "waiting", "output.mp4")

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/video", nil)
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Video(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	testutil.AssertEqual(t, rec.Header().Get("Content-Type"), "video/mp4")
	testutil.AssertEqual(t, rec.Header().Get("Cache-Control"), "no-store")
}

func TestArtifactsHandler_Metadata_Success(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, runID := newArtifactTestHandler(t, "metadata_ack", "waiting", "metadata.json")

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/metadata", nil)
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Metadata(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	testutil.AssertEqual(t, rec.Header().Get("Content-Type"), "application/json")
	testutil.AssertEqual(t, rec.Header().Get("Cache-Control"), "no-store")
}

func TestArtifactsHandler_Manifest_Success(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, runID := newArtifactTestHandler(t, "metadata_ack", "waiting", "manifest.json")

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/manifest", nil)
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Manifest(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	testutil.AssertEqual(t, rec.Header().Get("Content-Type"), "application/json")
}

func TestArtifactsHandler_Artifacts_ServeFromCompleteStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, runID := newArtifactTestHandler(t, "complete", "completed", "output.mp4", "metadata.json", "manifest.json")

	// Video
	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/video", nil)
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Video(rec, req)
	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	testutil.AssertEqual(t, rec.Header().Get("Content-Type"), "video/mp4")

	// Metadata
	req2 := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/metadata", nil)
	req2.SetPathValue("id", runID)
	rec2 := httptest.NewRecorder()
	h.Metadata(rec2, req2)
	testutil.AssertEqual(t, rec2.Code, http.StatusOK)
	testutil.AssertEqual(t, rec2.Header().Get("Content-Type"), "application/json")

	// Manifest
	req3 := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/manifest", nil)
	req3.SetPathValue("id", runID)
	rec3 := httptest.NewRecorder()
	h.Manifest(rec3, req3)
	testutil.AssertEqual(t, rec3.Code, http.StatusOK)
	testutil.AssertEqual(t, rec3.Header().Get("Content-Type"), "application/json")
}

func TestArtifactsHandler_NotFound_NonExistentRun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	outDir := t.TempDir()
	h := api.NewArtifactsHandler(svc, outDir)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/scp-999-run-1/video", nil)
	req.SetPathValue("id", "scp-999-run-1")
	rec := httptest.NewRecorder()
	h.Video(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
}

func TestArtifactsHandler_NotFound_WrongStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, runID := newArtifactTestHandler(t, "pending", "pending", "output.mp4")

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/video", nil)
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Video(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
}

func TestArtifactsHandler_NotFound_MissingFile(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, runID := newArtifactTestHandler(t, "metadata_ack", "waiting") // no files created

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/video", nil)
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Video(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
}
