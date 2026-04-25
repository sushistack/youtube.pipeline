package pipeline_test

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// ── fake updaters ────────────────────────────────────────────────────────────

type fakeSegmentUpdater struct {
	mu      sync.Mutex
	updated []struct {
		runID      string
		sceneIndex int
		clipPath   string
	}
	err error
}

func (f *fakeSegmentUpdater) UpdateClipPath(ctx context.Context, runID string, sceneIndex int, clipPath string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updated = append(f.updated, struct {
		runID      string
		sceneIndex int
		clipPath   string
	}{runID, sceneIndex, clipPath})
	return nil
}

type fakeRunUpdater struct {
	mu      sync.Mutex
	updated []struct {
		runID      string
		outputPath string
	}
	err error
}

func (f *fakeRunUpdater) UpdateOutputPath(ctx context.Context, runID string, outputPath string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updated = append(f.updated, struct {
		runID      string
		outputPath string
	}{runID, outputPath})
	return nil
}

// ── test helpers ─────────────────────────────────────────────────────────────

func skipIfNoFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not in PATH")
	}
}

// makeTestImage writes a single 1920×1080 PNG using a lavfi color source.
func makeTestImage(t *testing.T, path string, color string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=%s:s=1920x1080:d=1", color),
		"-frames:v", "1", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("makeTestImage %s: %v\n%s", path, err, out)
	}
}

// makeTestAudio writes a WAV sine-tone of the given duration.
func makeTestAudio(t *testing.T, path string, durationSec float64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi",
		"-i", fmt.Sprintf("sine=frequency=440:duration=%.3f", durationSec),
		path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("makeTestAudio %s: %v\n%s", path, err, out)
	}
}

// probeFileDuration returns the duration of a media file via ffprobe.
func probeFileDuration(t *testing.T, path string) float64 {
	t.Helper()
	out, err := exec.Command("ffprobe",
		"-v", "quiet", "-print_format", "json", "-show_format", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	// Simple float extraction — no external dependencies.
	var dur float64
	if _, err := fmt.Sscanf(extractJSON(string(out), "duration"), "%f", &dur); err != nil {
		t.Fatalf("parse duration from %s: %v", path, err)
	}
	return dur
}

// extractJSON pulls the value of a JSON string key from a flat JSON blob.
func extractJSON(blob, key string) string {
	needle := `"` + key + `": "`
	idx := 0
	for i := 0; i < len(blob)-len(needle); i++ {
		if blob[i:i+len(needle)] == needle {
			idx = i + len(needle)
			end := idx
			for end < len(blob) && blob[end] != '"' {
				end++
			}
			return blob[idx:end]
		}
	}
	return ""
}

func newTestRunner(t *testing.T) (*pipeline.PhaseCRunner, *fakeSegmentUpdater, *fakeRunUpdater) {
	t.Helper()
	seg := &fakeSegmentUpdater{}
	run := &fakeRunUpdater{}
	runner := pipeline.NewPhaseCRunner(seg, run, nil, nil, nil)
	return runner, seg, run
}

// ── integration tests ────────────────────────────────────────────────────────

func TestPhaseCRunner_1ShotKenBurns(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	skipIfNoFFmpeg(t)

	runDir := t.TempDir()
	imgPath := filepath.Join(runDir, "images", "scene_00", "shot_00.png")
	ttsPath := filepath.Join(runDir, "tts", "scene_00.wav")
	makeTestImage(t, imgPath, "blue")
	makeTestAudio(t, ttsPath, 3.0)

	ttsDurationMs := 3000
	ep := &domain.Episode{
		SceneIndex: 0,
		ShotCount:  1,
		Shots: []domain.Shot{
			{ImagePath: imgPath, DurationSeconds: 3.0, Transition: domain.TransitionKenBurns},
		},
		TTSPath:       strPtr(ttsPath),
		TTSDurationMs: &ttsDurationMs,
	}

	runner, seg, run := newTestRunner(t)
	req := pipeline.PhaseCRequest{
		RunID:    "test-run-1shot",
		RunDir:   runDir,
		Segments: []*domain.Episode{ep},
	}
	res, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// AC8a: clip count matches ShotCount (1 shot → 1 clip).
	if len(res.ClipPaths) != 1 {
		t.Errorf("clip count = %d, want 1", len(res.ClipPaths))
	}
	// Clip file must exist.
	if _, statErr := os.Stat(res.ClipPaths[0]); statErr != nil {
		t.Errorf("clip file missing: %v", statErr)
	}
	// AC8d: clip duration ≈ TTS duration.
	clipDur := probeFileDuration(t, res.ClipPaths[0])
	if math.Abs(clipDur-3.0) > 0.5 {
		t.Errorf("clip duration = %.3fs, want ≈3.0s", clipDur)
	}
	// output.mp4 must exist.
	if _, statErr := os.Stat(res.OutputPath); statErr != nil {
		t.Errorf("output.mp4 missing: %v", statErr)
	}
	// DB persistence: segment updater called once.
	if len(seg.updated) != 1 {
		t.Errorf("segment updater calls = %d, want 1", len(seg.updated))
	}
	// DB persistence: run updater called once.
	if len(run.updated) != 1 {
		t.Errorf("run updater calls = %d, want 1", len(run.updated))
	}
}

func TestPhaseCRunner_3ShotCrossDissolve(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	skipIfNoFFmpeg(t)

	runDir := t.TempDir()
	shots := make([]domain.Shot, 3)
	colors := []string{"red", "green", "blue"}
	for i, c := range colors {
		p := filepath.Join(runDir, "images", fmt.Sprintf("scene_00/shot_%02d.png", i))
		makeTestImage(t, p, c)
		shots[i] = domain.Shot{ImagePath: p, DurationSeconds: 2.0, Transition: domain.TransitionCrossDissolve}
	}
	ttsPath := filepath.Join(runDir, "tts", "scene_00.wav")
	makeTestAudio(t, ttsPath, 5.0)
	ttsDur := 5000

	ep := &domain.Episode{
		SceneIndex:    0,
		ShotCount:     3,
		Shots:         shots,
		TTSPath:       strPtr(ttsPath),
		TTSDurationMs: &ttsDur,
	}

	runner, _, _ := newTestRunner(t)
	req := pipeline.PhaseCRequest{
		RunID:    "test-run-xdissolve",
		RunDir:   runDir,
		Segments: []*domain.Episode{ep},
	}
	res, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// AC8b: clip produced for cross_dissolve scene.
	if len(res.ClipPaths) != 1 {
		t.Fatalf("clip count = %d, want 1", len(res.ClipPaths))
	}
	if _, statErr := os.Stat(res.ClipPaths[0]); statErr != nil {
		t.Errorf("clip file missing: %v", statErr)
	}
	// Duration should be approximately TTS (5s). Loose tolerance for xfade.
	clipDur := probeFileDuration(t, res.ClipPaths[0])
	if clipDur < 4.0 || clipDur > 7.0 {
		t.Errorf("clip duration = %.3fs, expected 4–7s range", clipDur)
	}
}

func TestPhaseCRunner_HardCutTransition(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	skipIfNoFFmpeg(t)

	runDir := t.TempDir()
	var shots []domain.Shot
	for i, c := range []string{"red", "blue"} {
		p := filepath.Join(runDir, "images", fmt.Sprintf("scene_00/shot_%02d.png", i))
		makeTestImage(t, p, c)
		shots = append(shots, domain.Shot{ImagePath: p, DurationSeconds: 2.0, Transition: domain.TransitionHardCut})
	}
	ttsPath := filepath.Join(runDir, "tts", "scene_00.wav")
	makeTestAudio(t, ttsPath, 4.0)
	ttsDur := 4000

	ep := &domain.Episode{
		SceneIndex:    0,
		ShotCount:     2,
		Shots:         shots,
		TTSPath:       strPtr(ttsPath),
		TTSDurationMs: &ttsDur,
	}

	runner, _, _ := newTestRunner(t)
	res, err := runner.Run(context.Background(), pipeline.PhaseCRequest{
		RunID: "test-run-hardcut", RunDir: runDir, Segments: []*domain.Episode{ep},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// AC8b: hard_cut produces valid MP4.
	if _, statErr := os.Stat(res.ClipPaths[0]); statErr != nil {
		t.Errorf("clip file missing: %v", statErr)
	}
	// Hard cut: duration ≈ sum of shots = 4s.
	clipDur := probeFileDuration(t, res.ClipPaths[0])
	if math.Abs(clipDur-4.0) > 0.5 {
		t.Errorf("clip duration = %.3fs, want ≈4.0s", clipDur)
	}
}

func TestPhaseCRunner_FinalConcatDuration(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	skipIfNoFFmpeg(t)

	runDir := t.TempDir()
	var episodes []*domain.Episode
	for i := 0; i < 2; i++ {
		p := filepath.Join(runDir, "images", fmt.Sprintf("scene_%02d/shot_00.png", i))
		makeTestImage(t, p, []string{"red", "blue"}[i])
		ttsPath := filepath.Join(runDir, "tts", fmt.Sprintf("scene_%02d.wav", i))
		makeTestAudio(t, ttsPath, 3.0)
		ttsDur := 3000
		episodes = append(episodes, &domain.Episode{
			SceneIndex: i,
			ShotCount:  1,
			Shots: []domain.Shot{
				{ImagePath: p, DurationSeconds: 3.0, Transition: domain.TransitionKenBurns},
			},
			TTSPath:       strPtr(ttsPath),
			TTSDurationMs: &ttsDur,
		})
	}

	runner, _, _ := newTestRunner(t)
	res, err := runner.Run(context.Background(), pipeline.PhaseCRequest{
		RunID: "test-run-concat", RunDir: runDir, Segments: episodes,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// AC8c: total output duration ≈ sum of scene clip durations (no gap).
	var totalClipDur float64
	for _, cp := range res.ClipPaths {
		totalClipDur += probeFileDuration(t, cp)
	}
	outputDur := probeFileDuration(t, res.OutputPath)
	if math.Abs(outputDur-totalClipDur) > 0.5 {
		t.Errorf("output duration = %.3fs, sum clips = %.3fs, diff > 0.5s", outputDur, totalClipDur)
	}
}

func TestPhaseCRunner_ResumeReEntry(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	skipIfNoFFmpeg(t)

	runDir := t.TempDir()
	imgPath := filepath.Join(runDir, "images", "scene_00", "shot_00.png")
	ttsPath := filepath.Join(runDir, "tts", "scene_00.wav")
	makeTestImage(t, imgPath, "green")
	makeTestAudio(t, ttsPath, 2.0)
	ttsDur := 2000

	ep := &domain.Episode{
		SceneIndex:    0,
		ShotCount:     1,
		Shots:         []domain.Shot{{ImagePath: imgPath, DurationSeconds: 2.0, Transition: domain.TransitionKenBurns}},
		TTSPath:       strPtr(ttsPath),
		TTSDurationMs: &ttsDur,
	}

	runner, _, _ := newTestRunner(t)
	req := pipeline.PhaseCRequest{
		RunID: "test-run-resume", RunDir: runDir, Segments: []*domain.Episode{ep},
	}

	// First run.
	res1, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// AC8e: second run (resume re-entry) must succeed without error.
	// The -y flag ensures existing clips are overwritten rather than rejected.
	res2, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("second Run (re-entry): %v", err)
	}

	// Both runs must produce the same output path.
	if res1.OutputPath != res2.OutputPath {
		t.Errorf("output path changed between runs: %q → %q", res1.OutputPath, res2.OutputPath)
	}
	if _, statErr := os.Stat(res2.OutputPath); statErr != nil {
		t.Errorf("output.mp4 missing after re-entry: %v", statErr)
	}
}

// TestPhaseCRunner_3ShotCrossDissolve_ShortFirstShot exercises the R-09 short-
// shot guard (Story 11-5 AC-3): when the composed pre-transition stream is
// shorter than the 0.5 s dissolve window, Phase C must degrade to a hard cut
// instead of emitting an offset-clamped-to-zero xfade. Verifies the produced
// clip remains a valid MP4 under real FFmpeg.
func TestPhaseCRunner_3ShotCrossDissolve_ShortFirstShot(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	skipIfNoFFmpeg(t)

	runDir := t.TempDir()
	// First shot: 0.3 s (< 0.5 s dissolve window). Subsequent shots are normal.
	durations := []float64{0.3, 1.5, 1.5}
	colors := []string{"red", "green", "blue"}
	shots := make([]domain.Shot, len(durations))
	for i := range durations {
		p := filepath.Join(runDir, "images", fmt.Sprintf("scene_00/shot_%02d.png", i))
		makeTestImage(t, p, colors[i])
		shots[i] = domain.Shot{ImagePath: p, DurationSeconds: durations[i], Transition: domain.TransitionCrossDissolve}
	}
	ttsPath := filepath.Join(runDir, "tts", "scene_00.wav")
	makeTestAudio(t, ttsPath, 3.0)
	ttsDur := 3000

	ep := &domain.Episode{
		SceneIndex:    0,
		ShotCount:     len(shots),
		Shots:         shots,
		TTSPath:       strPtr(ttsPath),
		TTSDurationMs: &ttsDur,
	}

	runner, _, _ := newTestRunner(t)
	res, err := runner.Run(context.Background(), pipeline.PhaseCRequest{
		RunID: "test-run-short-xfade", RunDir: runDir, Segments: []*domain.Episode{ep},
	})
	if err != nil {
		t.Fatalf("Run with short pre-transition stream: %v", err)
	}
	if len(res.ClipPaths) != 1 {
		t.Fatalf("clip count = %d, want 1", len(res.ClipPaths))
	}
	if _, statErr := os.Stat(res.ClipPaths[0]); statErr != nil {
		t.Errorf("clip file missing: %v", statErr)
	}
	clipDur := probeFileDuration(t, res.ClipPaths[0])
	if clipDur <= 0 {
		t.Errorf("clip duration = %.3fs, want > 0 (degenerate xfade would yield empty/invalid output)", clipDur)
	}
}

// TestPhaseCRunner_Run_ValidatesRequest verifies that empty requests are rejected.
func TestPhaseCRunner_Run_ValidatesRequest(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	runner := pipeline.NewPhaseCRunner(nil, nil, nil, nil, nil)

	cases := []struct {
		name string
		req  pipeline.PhaseCRequest
	}{
		{"empty", pipeline.PhaseCRequest{}},
		{"no_run_dir", pipeline.PhaseCRequest{RunID: "x"}},
		{"no_segments", pipeline.PhaseCRequest{RunID: "x", RunDir: "/tmp"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runner.Run(context.Background(), tc.req)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}
