package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// RunArtifactsStore is the minimal run access surface needed by ArtifactsHandler.
type RunArtifactsStore interface {
	Get(ctx context.Context, id string) (*domain.Run, error)
}

// ArtifactsHandler serves run output files (video, metadata, manifest) to the browser.
// Security invariant: run ID is validated against the database before building any file path.
type ArtifactsHandler struct {
	svc       RunArtifactsStore
	outputDir string
}

// NewArtifactsHandler creates an ArtifactsHandler.
// svc is typically *service.RunService (it satisfies RunArtifactsStore structurally).
func NewArtifactsHandler(svc RunArtifactsStore, outputDir string) *ArtifactsHandler {
	return &ArtifactsHandler{svc: svc, outputDir: outputDir}
}

// Video serves the assembled output video file.
func (h *ArtifactsHandler) Video(w http.ResponseWriter, r *http.Request) {
	h.serveRunFile(w, r, "output.mp4", "video/mp4")
}

// Metadata serves the metadata.json bundle.
func (h *ArtifactsHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	h.serveRunFile(w, r, "metadata.json", "application/json")
}

// Manifest serves the manifest.json license-audit file.
func (h *ArtifactsHandler) Manifest(w http.ResponseWriter, r *http.Request) {
	h.serveRunFile(w, r, "manifest.json", "application/json")
}

// serveRunFile is the shared file-serving logic for all artifact endpoints.
// It validates the run exists and is at metadata_ack or complete stage before
// opening the file, preventing arbitrary filesystem reads via path traversal.
func (h *ArtifactsHandler) serveRunFile(w http.ResponseWriter, r *http.Request, filename, contentType string) {
	runID := r.PathValue("id")
	run, err := h.svc.Get(r.Context(), runID)
	if err != nil || run == nil || (run.Stage != domain.StageMetadataAck && run.Stage != domain.StageComplete) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(h.outputDir, runID, filename)
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	// metadata.json/manifest.json are regenerated on resume; disable caching so
	// the browser never serves a stale copy that disagrees with the DB state.
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filename, stat.ModTime(), f)
}
