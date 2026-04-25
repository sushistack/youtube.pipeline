package pipeline_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// scp049SeedDir returns the absolute path to the bundled canonical SCP-049
// fixture. It is anchored to this source file so the path is correct
// regardless of the package's working directory at test time.
func scp049SeedDir(t testing.TB) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("scp049SeedDir: runtime.Caller failed")
	}
	// .../internal/pipeline/smoke_seed_test.go -> repo root
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "e2e", "scp-049-seed")
}

// loadSCP049Seed returns (scenarioPath, segments) wired to the bundled
// fixture. The 3 segments map to the 3 PNG/WAV pairs under
// responses/{images,tts}/. Image paths and TTS paths are absolute so they
// can be passed verbatim into Phase B / Phase C without needing chdir.
//
// Reused by SMOKE-02 today and (post-CP-1) by SMOKE-01 / SMOKE-03.
func loadSCP049Seed(t testing.TB) (scenarioPath string, segments []*domain.Episode) {
	t.Helper()
	root := scp049SeedDir(t)
	scenarioPath = filepath.Join(root, "scenario.json")

	const sceneCount = 3
	const ttsDurationMs = 1000
	segments = make([]*domain.Episode, sceneCount)
	for i := 0; i < sceneCount; i++ {
		imgPath := filepath.Join(root, "responses", "images", scp049SceneFilename("scene", i, ".png"))
		ttsPath := filepath.Join(root, "responses", "tts", scp049SceneFilename("scene", i, ".wav"))
		ttsMs := ttsDurationMs
		segments[i] = &domain.Episode{
			RunID:      "scp-049-seed",
			SceneIndex: i,
			ShotCount:  1,
			Shots: []domain.Shot{
				{
					ImagePath:        imgPath,
					DurationSeconds:  1.0,
					Transition:       domain.TransitionKenBurns,
					VisualDescriptor: scp049FrozenDescriptor(i),
				},
			},
			TTSPath:       &ttsPath,
			TTSDurationMs: &ttsMs,
		}
	}
	return scenarioPath, segments
}

// scp049SceneFilename returns "<prefix>_<NN><ext>" with a 2-digit zero-padded
// scene index, matching the bundled fixture layout.
func scp049SceneFilename(prefix string, idx int, ext string) string {
	return fmt.Sprintf("%s_%02d%s", prefix, idx, ext)
}

// scp049FrozenDescriptor returns the canonical seed's verbatim frozen
// visual descriptor for scene idx. SMOKE-02 asserts this string is
// preserved byte-for-byte through Phase B and into Phase C, guarding the
// frozen-descriptor invariant (FR-VD).
func scp049FrozenDescriptor(idx int) string {
	switch idx {
	case 0:
		return "scene_00 fixture solid red 1920x1080"
	case 1:
		return "scene_01 fixture solid green 1920x1080"
	case 2:
		return "scene_02 fixture solid blue 1920x1080"
	}
	return ""
}

// seedRunAtCost inserts a runs row with the given cost_usd. Used by SMOKE-04
// to anchor a run record at a chosen cost so the cap-trip scenario has
// deterministic database state. stage/status default to pending.
func seedRunAtCost(t testing.TB, database *sql.DB, runID string, costUSD float64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id, stage, status, cost_usd, created_at, updated_at)
		 VALUES (?, ?, 'pending', 'pending', ?, ?, ?)`,
		runID, "049", costUSD, now, now,
	)
	if err != nil {
		t.Fatalf("seedRunAtCost: %v", err)
	}
}
