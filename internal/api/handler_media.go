package api

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// SegmentLookup is the per-scene segment access surface MediaHandler needs.
// *db.SegmentStore satisfies it structurally.
type SegmentLookup interface {
	GetByRunIDAndSceneIndex(ctx context.Context, runID string, sceneIndex int) (*domain.Episode, error)
}

// MediaHandler serves per-scene media files (TTS audio, generated images) by
// resolving the segment row's relative path under {outputDir}/{runID}/. Run ID
// is validated via the runs store before any filesystem work, and the resolved
// absolute path is checked against the run's expected base directory so a
// crafted relative path with `..` segments cannot escape into the rest of the
// filesystem.
type MediaHandler struct {
	runs      RunArtifactsStore
	segments  SegmentLookup
	outputDir string
}

// NewMediaHandler constructs a MediaHandler.
func NewMediaHandler(runs RunArtifactsStore, segments SegmentLookup, outputDir string) *MediaHandler {
	return &MediaHandler{runs: runs, segments: segments, outputDir: outputDir}
}

// Audio handles GET /api/runs/{id}/scenes/{idx}/audio.
// 404 covers all missing-resource cases (run not found, segment not found, no
// TTS file, file missing on disk) so the client surfaces a single audio-
// unavailable state regardless of which step failed.
func (h *MediaHandler) Audio(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	idxStr := r.PathValue("idx")
	sceneIndex, err := strconv.Atoi(idxStr)
	if err != nil || sceneIndex < 0 {
		http.NotFound(w, r)
		return
	}

	if _, err := h.runs.Get(r.Context(), runID); err != nil {
		http.NotFound(w, r)
		return
	}

	seg, err := h.segments.GetByRunIDAndSceneIndex(r.Context(), runID, sceneIndex)
	if err != nil || seg == nil || seg.TTSPath == nil || strings.TrimSpace(*seg.TTSPath) == "" {
		http.NotFound(w, r)
		return
	}

	abs, ok := resolveRunRelativePath(h.outputDir, runID, *seg.TTSPath)
	if !ok {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(abs)
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

	contentType := mime.TypeByExtension(filepath.Ext(abs))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filepath.Base(abs), stat.ModTime(), f)
}

// resolveRunRelativePath joins outputDir/runID/relPath and verifies the
// cleaned absolute path stays under the run's directory. Absolute relPaths and
// paths that would escape the run dir via `..` segments are rejected.
func resolveRunRelativePath(outputDir, runID, relPath string) (string, bool) {
	if outputDir == "" || runID == "" || relPath == "" {
		return "", false
	}
	if filepath.IsAbs(relPath) {
		return "", false
	}
	runRoot, err := filepath.Abs(filepath.Join(outputDir, runID))
	if err != nil {
		return "", false
	}
	abs, err := filepath.Abs(filepath.Join(runRoot, relPath))
	if err != nil {
		return "", false
	}
	if abs != runRoot && !strings.HasPrefix(abs, runRoot+string(filepath.Separator)) {
		return "", false
	}
	// Defense in depth: resolve the run root through symlinks once and confirm
	// the resolved path is still under it. EvalSymlinks fails on non-existent
	// targets, so fall back to the literal containment check above when the
	// file isn't laid down yet (Phase B mid-write, etc.).
	if resolved, err := filepath.EvalSymlinks(abs); err == nil && !errors.Is(err, os.ErrNotExist) {
		if !strings.HasPrefix(resolved, runRoot+string(filepath.Separator)) && resolved != runRoot {
			return "", false
		}
	}
	return abs, true
}
