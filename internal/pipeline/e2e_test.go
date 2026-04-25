package pipeline_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// ── fake stores ──────────────────────────────────────────────────────────────

type e2eRunStore struct {
	mu  sync.Mutex
	run *domain.Run
}

func (s *e2eRunStore) Get(_ context.Context, id string) (*domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil || s.run.ID != id {
		return nil, domain.ErrNotFound
	}
	cp := *s.run
	return &cp, nil
}

func (s *e2eRunStore) ResetForResume(_ context.Context, id string, status domain.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run != nil && s.run.ID == id {
		s.run.Status = status
	}
	return nil
}

func (s *e2eRunStore) ApplyPhaseAResult(_ context.Context, id string, res domain.PhaseAAdvanceResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil || s.run.ID != id {
		return domain.ErrNotFound
	}
	s.run.Stage = res.Stage
	s.run.Status = res.Status
	s.run.RetryReason = res.RetryReason
	s.run.CriticScore = res.CriticScore
	s.run.ScenarioPath = res.ScenarioPath
	return nil
}

func (s *e2eRunStore) setStageStatus(stage domain.Stage, status domain.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.run.Stage = stage
	s.run.Status = status
}

type e2eSegmentStore struct {
	mu       sync.Mutex
	segments []*domain.Episode
}

func (s *e2eSegmentStore) ListByRunID(_ context.Context, _ string) ([]*domain.Episode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.segments, nil
}

func (s *e2eSegmentStore) DeleteByRunID(_ context.Context, _ string) (int64, error) { return 0, nil }
func (s *e2eSegmentStore) ClearClipPathsByRunID(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (s *e2eSegmentStore) ClearImageArtifactsByRunID(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (s *e2eSegmentStore) ClearTTSArtifactsByRunID(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (s *e2eSegmentStore) setSegments(segs []*domain.Episode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.segments = segs
}

// ── fake executors ────────────────────────────────────────────────────────────

// e2ePhaseAExecutor writes a minimal scenario.json and populates the
// PipelineState fields that Engine.Advance validates after Phase A.
type e2ePhaseAExecutor struct {
	outDir string
}

func (e *e2ePhaseAExecutor) Run(_ context.Context, state *agents.PipelineState) error {
	runDir := filepath.Join(e.outDir, state.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	scenario := map[string]any{
		"run_id": state.RunID,
		"scp_id": state.SCPID,
		"scenes": []any{},
	}
	raw, err := json.Marshal(scenario)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runDir, "scenario.json"), raw, 0o644); err != nil {
		return err
	}
	state.Quality = &agents.PhaseAQualitySummary{
		PostWriterScore:   85,
		PostReviewerScore: 90,
		CumulativeScore:   87,
		FinalVerdict:      domain.CriticVerdictPass,
	}
	state.Contracts = &agents.PhaseAContractManifest{}
	state.Critic = &domain.CriticOutput{
		PostReviewer: &domain.CriticCheckpointReport{
			Checkpoint:   domain.CriticCheckpointPostReviewer,
			Verdict:      domain.CriticVerdictPass,
			OverallScore: 90,
		},
	}
	return nil
}

// e2ePhaseBExecutor is a no-op stub. Segments are pre-loaded into the
// segment store before Advance is called at Phase B, matching the real
// flow where scene/character services populate segments during HITL review.
type e2ePhaseBExecutor struct{}

func (e *e2ePhaseBExecutor) Run(_ context.Context, _ pipeline.PhaseBRequest) (pipeline.PhaseBResult, error) {
	return pipeline.PhaseBResult{}, nil
}

// e2eMetadataBuilder writes minimal metadata.json and manifest.json files.
type e2eMetadataBuilder struct {
	outDir string
}

func (b *e2eMetadataBuilder) Build(_ context.Context, runID string) (domain.MetadataBundle, domain.SourceManifest, error) {
	return domain.MetadataBundle{RunID: runID}, domain.SourceManifest{RunID: runID}, nil
}

func (b *e2eMetadataBuilder) Write(_ context.Context, runID string, bundle domain.MetadataBundle, manifest domain.SourceManifest) error {
	runDir := filepath.Join(b.outDir, runID)
	metaRaw, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runDir, "metadata.json"), metaRaw, 0o644); err != nil {
		return err
	}
	maniRaw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, "manifest.json"), maniRaw, 0o644)
}

// ── compile-time interface checks ─────────────────────────────────────────────

var (
	_ pipeline.PhaseAExecutor  = (*e2ePhaseAExecutor)(nil)
	_ pipeline.PhaseBExecutor  = (*e2ePhaseBExecutor)(nil)
	_ pipeline.MetadataBuilder = (*e2eMetadataBuilder)(nil)
)

// ── TestE2E_FullPipeline ──────────────────────────────────────────────────────

// TestE2E_FullPipeline validates that Engine.Advance dispatches Phase A, Phase B,
// and Phase C end-to-end using injected mock/stub providers. Real ffmpeg is
// required for Phase C assembly; the test is skipped if ffmpeg is absent.
//
// The test drives the pipeline through all automated stages and verifies:
//   - Stage transitions after each Advance call
//   - Required artifacts written to the run directory
func TestE2E_FullPipeline(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	skipIfNoFFmpeg(t)

	const runID = "run-e2e"
	outDir := t.TempDir()

	runStore := &e2eRunStore{
		run: &domain.Run{
			ID:     runID,
			SCPID:  "SCP-049",
			Stage:  domain.StagePending,
			Status: domain.StatusPending,
		},
	}
	segStore := &e2eSegmentStore{}

	engine := pipeline.NewEngine(runStore, segStore, nil, clock.RealClock{}, outDir, slog.Default())
	engine.SetPhaseAExecutor(&e2ePhaseAExecutor{outDir: outDir})
	engine.SetPhaseBExecutor(&e2ePhaseBExecutor{})
	engine.SetPhaseCRunner(pipeline.NewPhaseCRunner(
		&fakeSegmentUpdater{},
		&fakeRunUpdater{},
		nil,
		clock.RealClock{},
		slog.Default(),
	))
	engine.SetPhaseCMetadataBuilder(&e2eMetadataBuilder{outDir: outDir})

	ctx := context.Background()

	// ── Phase A ───────────────────────────────────────────────────────────────
	if err := engine.Advance(ctx, runID); err != nil {
		t.Fatalf("Phase A Advance: %v", err)
	}
	if runStore.run.Stage != domain.StageScenarioReview {
		t.Fatalf("after Phase A: expected stage=%s, got %s", domain.StageScenarioReview, runStore.run.Stage)
	}
	if runStore.run.Status != domain.StatusWaiting {
		t.Fatalf("after Phase A: expected status=%s, got %s", domain.StatusWaiting, runStore.run.Status)
	}

	// ── Simulate HITL: scenario_review → character_pick → image/running ───────
	// In production these transitions happen via CharacterService.Pick; here
	// we directly advance the state to the Phase B entry point.
	_, seedSegs := loadSCP049Seed(t)
	// Assign the run-e2e runID to segments so PhaseCRunner can match them.
	for _, seg := range seedSegs {
		seg.RunID = runID
	}
	segStore.setSegments(seedSegs)
	runStore.setStageStatus(domain.StageImage, domain.StatusRunning)

	// ── Phase B ───────────────────────────────────────────────────────────────
	if err := engine.Advance(ctx, runID); err != nil {
		t.Fatalf("Phase B Advance: %v", err)
	}
	if runStore.run.Stage != domain.StageBatchReview {
		t.Fatalf("after Phase B: expected stage=%s, got %s", domain.StageBatchReview, runStore.run.Stage)
	}
	if runStore.run.Status != domain.StatusWaiting {
		t.Fatalf("after Phase B: expected status=%s, got %s", domain.StatusWaiting, runStore.run.Status)
	}

	// ── Simulate HITL: batch_review approval → assemble/running ──────────────
	runStore.setStageStatus(domain.StageAssemble, domain.StatusRunning)

	// ── Phase C ───────────────────────────────────────────────────────────────
	if err := engine.Advance(ctx, runID); err != nil {
		t.Fatalf("Phase C Advance: %v", err)
	}
	if runStore.run.Stage != domain.StageMetadataAck {
		t.Fatalf("after Phase C: expected stage=%s, got %s", domain.StageMetadataAck, runStore.run.Stage)
	}
	if runStore.run.Status != domain.StatusWaiting {
		t.Fatalf("after Phase C: expected status=%s, got %s", domain.StatusWaiting, runStore.run.Status)
	}

	// ── Artifact verification ─────────────────────────────────────────────────
	runDir := filepath.Join(outDir, runID)
	requiredArtifacts := []string{
		"scenario.json",
		"output.mp4",
		"metadata.json",
		"manifest.json",
	}
	for _, artifact := range requiredArtifacts {
		path := filepath.Join(runDir, artifact)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected artifact missing: %s", artifact)
		}
	}

	// ── SMOKE-01 assertions (test-design §4) ──────────────────────────────────
	// Frozen descriptor is the Phase A→B handoff invariant; it must survive
	// dispatch through Phase C without mutation. The fake stores hold the
	// segments by reference — re-read via the engine seam to mirror real
	// production (DB roundtrip).
	postSegs, err := segStore.ListByRunID(ctx, runID)
	if err != nil {
		t.Fatalf("post-Advance ListByRunID: %v", err)
	}
	if len(postSegs) != 3 {
		t.Fatalf("post-Advance segments: got %d, want 3", len(postSegs))
	}
	for i, ep := range postSegs {
		if len(ep.Shots) != 1 {
			t.Fatalf("scene %d: shot count = %d, want 1", i, len(ep.Shots))
		}
		want := scp049FrozenDescriptor(i)
		if ep.Shots[0].VisualDescriptor != want {
			t.Errorf("scene %d frozen descriptor: got %q, want %q",
				i, ep.Shots[0].VisualDescriptor, want)
		}
	}

	// Probe output.mp4: duration ≈ Σ(scene tts durations), codec h264 + aac.
	// The 0.2 s tolerance matches SMOKE-02; xfade overlap subtracts a small
	// constant from the raw sum on Linux x86-64 ffmpeg ≥6.
	mp4Path := filepath.Join(runDir, "output.mp4")
	expectedDur := 0.0
	for _, ep := range postSegs {
		expectedDur += float64(*ep.TTSDurationMs) / 1000.0
	}
	gotDur := probeFileDuration(t, mp4Path)
	if math.Abs(gotDur-expectedDur) > 0.2 {
		t.Errorf("output.mp4 duration = %.3fs, want %.3fs ±0.2", gotDur, expectedDur)
	}
	codecs := probeCodecs(t, mp4Path)
	if codecs.video != "h264" {
		t.Errorf("output.mp4 video codec = %q, want h264", codecs.video)
	}
	if codecs.audio != "aac" {
		t.Errorf("output.mp4 audio codec = %q, want aac", codecs.audio)
	}

	// metadata.json ↔ manifest.json must agree on run_id (cross-file pair-write
	// invariant — Story 11-5 Phase C hardening codifies the atomicity).
	type runIDEnvelope struct {
		RunID string `json:"run_id"`
	}
	var meta, mani runIDEnvelope
	if raw, err := os.ReadFile(filepath.Join(runDir, "metadata.json")); err != nil {
		t.Fatalf("read metadata.json: %v", err)
	} else if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("parse metadata.json: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(runDir, "manifest.json")); err != nil {
		t.Fatalf("read manifest.json: %v", err)
	} else if err := json.Unmarshal(raw, &mani); err != nil {
		t.Fatalf("parse manifest.json: %v", err)
	}
	if meta.RunID != runID || mani.RunID != runID {
		t.Errorf("metadata/manifest run_id mismatch: meta=%q, mani=%q, want %q",
			meta.RunID, mani.RunID, runID)
	}
}
