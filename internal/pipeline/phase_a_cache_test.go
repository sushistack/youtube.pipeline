package pipeline

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestPhaseA_CacheHit_FingerprintMatch verifies the happy path: a CacheEnvelope
// whose fingerprint matches cacheInputs is accepted as a hit and the agent is
// not invoked.
func TestPhaseA_CacheHit_FingerprintMatch(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)
	b.cacheInputs = defaultCacheInputs()

	runDir := defaultRunDir(b)
	cached := &domain.ResearcherOutput{SCPID: "scp-1", Title: "envelope-hit", SourceVersion: domain.SourceVersionV1}
	writeTestCacheEnvelope(t, runDir, agents.StageResearcher, cached, b.cacheInputs[agents.StageResearcher])

	var calls int
	b.researcher = func(_ context.Context, _ *agents.PipelineState) error { calls++; return nil }

	r := b.build(t)
	state := newState()
	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run: %v", err)
	}
	testutil.AssertEqual(t, calls, 0)
	if state.Research == nil || state.Research.Title != "envelope-hit" {
		t.Errorf("state.Research not loaded from envelope: %+v", state.Research)
	}
}

// TestPhaseA_CacheStale_PromptTemplateChanged verifies that changing
// PromptTemplateSHA in cacheInputs makes an otherwise-valid envelope stale
// (fingerprint mismatch → staleness_reason prompt_template_changed → cache miss).
func TestPhaseA_CacheStale_PromptTemplateChanged(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)

	// Envelope written with old prompt SHA; runner expects new SHA.
	oldInputs := FingerprintInputs{
		SourceVersion:     domain.SourceVersionV1,
		PromptTemplateSHA: "old-sha",
		SchemaVersion:     "v1",
	}
	newInputs := FingerprintInputs{
		SourceVersion:     domain.SourceVersionV1,
		PromptTemplateSHA: "new-sha",
		SchemaVersion:     "v1",
	}
	b.cacheInputs = map[agents.PipelineStage]FingerprintInputs{
		agents.StageResearcher: newInputs,
		agents.StageStructurer: newInputs,
	}

	runDir := defaultRunDir(b)
	writeTestCacheEnvelope(t, runDir, agents.StageResearcher,
		&domain.ResearcherOutput{SCPID: "scp-1", Title: "stale"}, oldInputs)

	var calls int
	b.researcher = func(_ context.Context, state *agents.PipelineState) error {
		calls++
		state.Research = &domain.ResearcherOutput{SCPID: "scp-1", Title: "fresh", SourceVersion: domain.SourceVersionV1}
		return nil
	}

	r := b.build(t)
	state := newState()
	if err := r.Run(context.Background(), state); err != nil {
		t.Fatalf("Run: %v", err)
	}
	testutil.AssertEqual(t, calls, 1)
	if state.Research == nil || state.Research.Title != "fresh" {
		t.Errorf("expected fresh research after prompt-template cache miss: %+v", state.Research)
	}

	// After re-run, a new envelope must be present with the new fingerprint.
	newEnv, reason, err := LoadEnvelope(
		CacheStageFile(runDir, CacheStageFilenames[agents.StageResearcher]), newInputs)
	if err != nil || reason != "" {
		t.Errorf("post-run envelope not fresh: err=%v reason=%s", err, reason)
	}
	if newEnv == nil || ComputeFingerprint(newInputs) != newEnv.Fingerprint {
		t.Error("post-run envelope fingerprint does not match new inputs")
	}
}

// TestPhaseA_CacheStale_ModelChanged verifies that changing Model in
// cacheInputs invalidates the envelope.
func TestPhaseA_CacheStale_ModelChanged(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)

	storedInputs := FingerprintInputs{
		SourceVersion: domain.SourceVersionV1,
		Model:         "qwen-plus",
		Provider:      "dashscope",
		SchemaVersion: "v1",
	}
	currentInputs := FingerprintInputs{
		SourceVersion: domain.SourceVersionV1,
		Model:         "qwen-max",
		Provider:      "dashscope",
		SchemaVersion: "v1",
	}
	b.cacheInputs = map[agents.PipelineStage]FingerprintInputs{
		agents.StageResearcher: currentInputs,
		agents.StageStructurer: currentInputs,
	}

	runDir := defaultRunDir(b)
	writeTestCacheEnvelope(t, runDir, agents.StageResearcher,
		&domain.ResearcherOutput{SCPID: "scp-1"}, storedInputs)

	var calls int
	b.researcher = func(_ context.Context, state *agents.PipelineState) error {
		calls++
		state.Research = &domain.ResearcherOutput{SCPID: "scp-1", SourceVersion: domain.SourceVersionV1}
		return nil
	}

	r := b.build(t)
	if err := r.Run(context.Background(), newState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	testutil.AssertEqual(t, calls, 1) // miss → agent ran
}

// TestPhaseA_CacheStale_LegacyEnvelopelessFile verifies that a flat JSON
// payload file (no envelope_version field — the pre-envelope cache format)
// is treated as a corrupt envelope → cache miss → agent re-runs.
func TestPhaseA_CacheStale_LegacyEnvelopelessFile(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	b := defaultRunnerBuilder(t)
	b.cacheInputs = defaultCacheInputs()

	runDir := defaultRunDir(b)
	// Simulate old flat-JSON cache format (no envelope_version).
	legacyPath := CacheStageFile(runDir, CacheStageFilenames[agents.StageResearcher])
	if err := os.MkdirAll(CacheDir(runDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{"scp_id":"scp-1","source_version":"v1.2-roles"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls int
	b.researcher = func(_ context.Context, state *agents.PipelineState) error {
		calls++
		state.Research = &domain.ResearcherOutput{SCPID: "scp-1", SourceVersion: domain.SourceVersionV1}
		return nil
	}

	r := b.build(t)
	if err := r.Run(context.Background(), newState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	testutil.AssertEqual(t, calls, 1) // legacy file → miss → agent ran
}

// defaultRunDir returns the canonical run dir for state RunID "run-1" under the
// builder's outputDir. The dir is NOT pre-created — Run() does that.
func defaultRunDir(b *runnerBuilder) string {
	return b.outputDir + "/run-1"
}

// Ensure time is used (for WriteEnvelope's now parameter inside writeTestCacheEnvelope).
var _ = time.Now
