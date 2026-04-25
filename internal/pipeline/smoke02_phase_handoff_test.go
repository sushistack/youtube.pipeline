package pipeline_test

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestSMOKE_02_PhaseHandoff exercises the Phase B → Phase C handoff with
// real ffmpeg, using the bundled SCP-049 canonical seed. It guards against
// regressions in: scene-index preservation, frozen-descriptor verbatim
// propagation, ffmpeg pipeline integrity (h264/aac, duration tolerance),
// the assemble dispatch contract, and the per-scene clip-path persistence
// invariant.
//
// Runtime budget: ≤ 15 s. Real ffmpeg is required; the test skips
// gracefully if the binary is absent.
func TestSMOKE_02_PhaseHandoff(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	skipIfNoFFmpeg(t)

	scenarioPath, segments := loadSCP049Seed(t)

	// Sanity-check the canonical seed before investing in ffmpeg work.
	raw, err := os.ReadFile(scenarioPath)
	if err != nil {
		t.Fatalf("read scenario.json: %v", err)
	}
	var probe struct {
		RunID string `json:"run_id"`
		SCPID string `json:"scp_id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("decode scenario.json: %v", err)
	}
	if probe.RunID == "" || probe.SCPID == "" {
		t.Fatalf("seed scenario.json missing required ids: %+v", probe)
	}

	// Pre-handoff snapshot. We guard the loop with an explicit length check
	// so a Phase B regression that drops a shot fails cleanly instead of
	// panicking with index-out-of-range.
	preIndices := make([]int, len(segments))
	preDescriptors := make([]string, len(segments))
	preShotCount := make([]int, len(segments))
	for i, ep := range segments {
		if len(ep.Shots) == 0 {
			t.Fatalf("seed segment %d: Shots empty (canonical seed corrupted?)", i)
		}
		preIndices[i] = ep.SceneIndex
		preDescriptors[i] = ep.Shots[0].VisualDescriptor
		preShotCount[i] = ep.ShotCount
	}

	// ── Phase B with stub tracks ──────────────────────────────────────────
	var imageCalls, ttsCalls atomic.Int32
	var assembleCalled atomic.Bool
	imageTrack := func(_ context.Context, _ pipeline.PhaseBRequest) (pipeline.ImageTrackResult, error) {
		imageCalls.Add(1)
		return pipeline.ImageTrackResult{
			Observation: domain.StageObservation{Stage: domain.StageImage, DurationMs: 1},
			Artifacts:   []string{},
		}, nil
	}
	ttsTrack := func(_ context.Context, _ pipeline.PhaseBRequest) (pipeline.TTSTrackResult, error) {
		ttsCalls.Add(1)
		return pipeline.TTSTrackResult{
			Observation: domain.StageObservation{Stage: domain.StageTTS, DurationMs: 1},
			Artifacts:   []string{},
		}, nil
	}
	assemble := func(_ context.Context, _ pipeline.PhaseBResult) error {
		assembleCalled.Store(true)
		return nil
	}

	bRunner := pipeline.NewPhaseBRunner(imageTrack, ttsTrack, nil, nil, nil, assemble, nil)
	bReq := pipeline.PhaseBRequest{
		RunID:        "scp-049-seed",
		Stage:        domain.StageImage,
		ScenarioPath: scenarioPath,
		Segments:     segments,
	}
	bRes, err := bRunner.Run(context.Background(), bReq)
	if err != nil {
		t.Fatalf("phase_b.Run: %v", err)
	}
	if !bRes.AssemblyDone {
		t.Error("phase B did not record AssemblyDone=true")
	}
	if !assembleCalled.Load() {
		t.Error("assemble closure was not called")
	}
	if got := imageCalls.Load(); got != 1 {
		t.Errorf("image track invocations = %d, want 1", got)
	}
	if got := ttsCalls.Load(); got != 1 {
		t.Errorf("tts track invocations = %d, want 1", got)
	}

	// Handoff invariant: phase B must not silently drop, duplicate, or
	// reorder segments; descriptor and shot count must be byte-identical.
	if len(segments) != len(preIndices) {
		t.Fatalf("segment count after Phase B: got %d, want %d", len(segments), len(preIndices))
	}
	for i, ep := range segments {
		if len(ep.Shots) == 0 {
			t.Fatalf("post-handoff segment %d: Shots emptied (drop regression)", i)
		}
		if ep.SceneIndex != preIndices[i] {
			t.Errorf("segments[%d].SceneIndex = %d, want %d", i, ep.SceneIndex, preIndices[i])
		}
		if ep.ShotCount != preShotCount[i] {
			t.Errorf("segments[%d].ShotCount = %d, want %d", i, ep.ShotCount, preShotCount[i])
		}
		if got := ep.Shots[0].VisualDescriptor; got != preDescriptors[i] {
			t.Errorf("segments[%d] frozen_descriptor mutated: got %q, want %q", i, got, preDescriptors[i])
		}
	}

	// ── Phase C with real ffmpeg ──────────────────────────────────────────
	runDir := t.TempDir()
	segUpdater := &fakeSegmentUpdater{}
	runUpdater := &fakeRunUpdater{}
	cRunner := pipeline.NewPhaseCRunner(segUpdater, runUpdater, nil, nil, nil)
	cReq := pipeline.PhaseCRequest{
		RunID:    "scp-049-seed",
		RunDir:   runDir,
		Segments: segments,
	}
	cRes, err := cRunner.Run(context.Background(), cReq)
	if err != nil {
		t.Fatalf("phase_c.Run: %v", err)
	}

	if got, want := len(cRes.ClipPaths), len(segments); got != want {
		t.Errorf("clip count = %d, want %d", got, want)
	}
	for i, p := range cRes.ClipPaths {
		if _, statErr := os.Stat(p); statErr != nil {
			t.Errorf("clip %d missing on disk: %v", i, statErr)
		}
	}
	if cRes.OutputPath == "" {
		t.Fatal("output path is empty")
	}
	if cRes.OutputPath != filepath.Join(runDir, "output.mp4") {
		t.Errorf("output path = %q, want runDir/output.mp4", cRes.OutputPath)
	}

	// Per-scene clip-path persistence: SegmentClipUpdater must be called
	// exactly once per scene with matching scene_index, and the run-level
	// updater must be called exactly once with the final output path.
	if got, want := len(segUpdater.updated), len(segments); got != want {
		t.Errorf("SegmentClipUpdater calls = %d, want %d", got, want)
	}
	seenScene := make(map[int]bool, len(segments))
	for _, u := range segUpdater.updated {
		if u.runID != "scp-049-seed" {
			t.Errorf("SegmentClipUpdater runID = %q, want scp-049-seed", u.runID)
		}
		seenScene[u.sceneIndex] = true
	}
	for _, ep := range segments {
		if !seenScene[ep.SceneIndex] {
			t.Errorf("SegmentClipUpdater missed scene %d", ep.SceneIndex)
		}
	}
	if got := len(runUpdater.updated); got != 1 {
		t.Errorf("RunOutputUpdater calls = %d, want 1", got)
	}

	// ── ffprobe assertions on output.mp4 ──────────────────────────────────
	expectedDur := 0.0
	for _, ep := range segments {
		expectedDur += float64(*ep.TTSDurationMs) / 1000.0
	}
	gotDur := probeFileDuration(t, cRes.OutputPath)
	// Tolerance pinned to spec I/O matrix (±0.1s). ffmpeg `-c copy` concat is
	// tight to this tolerance for short clips on Linux x86-64 ffmpeg ≥6.
	if math.Abs(gotDur-expectedDur) > 0.1 {
		t.Errorf("output duration = %.3fs, want %.3fs ±0.1", gotDur, expectedDur)
	}
	codecs := probeCodecs(t, cRes.OutputPath)
	if codecs.video != "h264" {
		t.Errorf("video codec = %q, want h264", codecs.video)
	}
	if codecs.audio != "aac" {
		t.Errorf("audio codec = %q, want aac", codecs.audio)
	}
}

// probeCodecs returns the first video and audio codec_name reported by
// ffprobe for the given media file. Empty strings indicate a missing
// stream of that kind.
type ffprobeCodecs struct {
	video string
	audio string
}

func probeCodecs(t testing.TB, path string) ffprobeCodecs {
	t.Helper()
	out, err := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		path,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe streams %s: %v", path, err)
	}
	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("parse ffprobe streams output: %v", err)
	}
	var c ffprobeCodecs
	for _, s := range parsed.Streams {
		if s.CodecType == "video" && c.video == "" {
			c.video = s.CodecName
		}
		if s.CodecType == "audio" && c.audio == "" {
			c.audio = s.CodecName
		}
	}
	return c
}
