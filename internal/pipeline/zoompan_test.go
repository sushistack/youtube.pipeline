package pipeline_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	ffmpeg "github.com/u2takey/ffmpeg-go"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestZoomPan(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Skip if ffmpeg not available (doctor check passes, but we can still skip if command fails)
	// We'll just attempt to run ffmpeg with a simple command to verify.
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}

	// Create a temporary output file
	out := filepath.Join(t.TempDir(), "output.mp4")

	// Input color source
	input := ffmpeg.Input("color=c=black:s=1920x1080:d=2", ffmpeg.KwArgs{"f": "lavfi"})

	// Apply zoompan filter with default parameters
	zoom := input.ZoomPan(ffmpeg.KwArgs{
		"zoom": "min(zoom+0.0015,1.5)",
		"d":    50, // 2 seconds at 25 fps
		"s":    "1920x1080",
	})

	// Output
	err := ffmpeg.Output([]*ffmpeg.Stream{zoom}, out).Run()
	if err != nil {
		t.Fatalf("ffmpeg error: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	t.Logf("generated %s", out)
}