package pipeline_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	ffmpeg "github.com/u2takey/ffmpeg-go"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// TestXfadeFilter validates that ffmpeg‑go can construct a simple xfade filter
// between two color sources and produce a valid output file.
func TestXfadeFilter(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Skip if ffmpeg not available.
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}

	// Create temporary output directory.
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "xfade_test.mp4")

	// Create two color sources (black and white), each 2 seconds.
	black := ffmpeg.Input("color=c=black:s=1920x1080:d=2", ffmpeg.KwArgs{"f": "lavfi"})
	white := ffmpeg.Input("color=c=white:s=1920x1080:d=2", ffmpeg.KwArgs{"f": "lavfi"})

	// Apply xfade filter: transition=fade, duration=0.5 seconds.
	// The xfade filter expects exactly two video streams.
	// The generic Filter function accepts a slice of streams.
	xfade := ffmpeg.Filter([]*ffmpeg.Stream{black, white}, "xfade",
		ffmpeg.Args{}, // no positional args
		ffmpeg.KwArgs{
			"transition": "fade",
			"duration":   "0.5",
			"offset":     "1.5", // start transition 1.5s into first stream (so it ends at 2s)
		})

	// Output the result.
	err := ffmpeg.Output([]*ffmpeg.Stream{xfade}, outPath).Run()
	if err != nil {
		t.Fatalf("ffmpeg error: %v", err)
	}

	// Verify output file exists and is non‑zero.
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("output file missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is zero bytes")
	}
	t.Logf("xfade test succeeded, output size %d bytes", info.Size())
}