package pipeline_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// mockTextGenerator implements domain.TextGenerator for E2E testing.
type mockTextGenerator struct{}

func (m *mockTextGenerator) Generate(_ context.Context, _ domain.TextRequest) (domain.TextResponse, error) {
	return domain.TextResponse{
		NormalizedResponse: domain.NormalizedResponse{
			Content:  `{"scenes": [{"narration": "test narration", "shots": [{"visual_descriptor": "test shot"}]}]}`,
			Model:    "mock-model",
			Provider: "mock",
		},
	}, nil
}

// mockImageGenerator implements domain.ImageGenerator for E2E testing.
type mockImageGenerator struct{}

func (m *mockImageGenerator) Generate(_ context.Context, _ domain.ImageRequest) (domain.ImageResponse, error) {
	return domain.ImageResponse{ImagePath: "/tmp/mock.png"}, nil
}

func (m *mockImageGenerator) Edit(_ context.Context, _ domain.ImageEditRequest) (domain.ImageResponse, error) {
	return domain.ImageResponse{ImagePath: "/tmp/mock-edit.png"}, nil
}

// mockTTSSynthesizer implements domain.TTSSynthesizer for E2E testing.
type mockTTSSynthesizer struct{}

func (m *mockTTSSynthesizer) Synthesize(_ context.Context, _ domain.TTSRequest) (domain.TTSResponse, error) {
	return domain.TTSResponse{AudioPath: "/tmp/mock.wav"}, nil
}

// Compile-time interface satisfaction checks
var (
	_ domain.TextGenerator  = (*mockTextGenerator)(nil)
	_ domain.ImageGenerator = (*mockImageGenerator)(nil)
	_ domain.TTSSynthesizer = (*mockTTSSynthesizer)(nil)
)

func TestE2E_FullPipeline(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Expected output artifacts after full pipeline execution
	outputDir := t.TempDir()
	expectedArtifacts := []string{
		"scenario.json",
		"images",
		"tts",
		"output.mp4",
		"metadata.json",
		"manifest.json",
	}

	// TODO: Wire up pipeline runner when Phase A/B/C are implemented (Epic 2, 5, 9)
	// The pipeline runner will accept injected mock providers:
	//   textGen := &mockTextGenerator{}
	//   imageGen := &mockImageGenerator{}
	//   ttsSynth := &mockTTSSynthesizer{}
	//   runner := pipeline.NewRunner(textGen, imageGen, ttsSynth, ...)
	//   err := runner.Execute(ctx, "scp-049", outputDir)

	t.Skip("Phase A/B/C pipeline runner not yet implemented — see Epic 2 (state machine), Epic 3 (Phase A agents), Epic 5 (Phase B media), Epic 9 (Phase C assembly)")

	// These assertions will be enabled when the pipeline runner exists:
	for _, artifact := range expectedArtifacts {
		path := filepath.Join(outputDir, artifact)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected artifact missing: %s", artifact)
		}
	}
}
