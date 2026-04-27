package dryrun_test

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/dryrun"
)

func TestImageClient_ImplementsImageGenerator(t *testing.T) {
	t.Parallel()
	var _ domain.ImageGenerator = dryrun.NewImageClient()
}

func TestImageClient_Generate_WritesValidPNG(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "scene_01", "shot_01.png")

	client := dryrun.NewImageClient()
	resp, err := client.Generate(context.Background(), domain.ImageRequest{
		Prompt:     "a serene foundation classroom",
		Model:      "qwen-image-2.0",
		Width:      640,
		Height:     360,
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if resp.ImagePath != out {
		t.Errorf("ImagePath = %q, want %q", resp.ImagePath, out)
	}
	if resp.Provider != dryrun.Provider {
		t.Errorf("Provider = %q, want %q", resp.Provider, dryrun.Provider)
	}
	if resp.Model != "qwen-image-2.0" {
		t.Errorf("Model = %q, want echo of request model", resp.Model)
	}
	if resp.CostUSD != 0 {
		t.Errorf("CostUSD = %v, want 0", resp.CostUSD)
	}

	// File must exist and decode as a 640×360 PNG with the placeholder color.
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 640 || bounds.Dy() != 360 {
		t.Errorf("png dimensions = %dx%d, want 640x360", bounds.Dx(), bounds.Dy())
	}
	r, g, b, a := img.At(100, 100).RGBA()
	// RGBA() returns 16-bit values; 0x2a maps to 0x2a2a after expansion.
	wantR, wantG, wantB, wantA := uint32(0x2a2a), uint32(0x2a2a), uint32(0x2a2a), uint32(0xffff)
	if r != wantR || g != wantG || b != wantB || a != wantA {
		t.Errorf("pixel(100,100) = (%x,%x,%x,%x), want (%x,%x,%x,%x)", r, g, b, a, wantR, wantG, wantB, wantA)
	}
}

func TestImageClient_Edit_WritesValidPNG(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "out.png")

	client := dryrun.NewImageClient()
	resp, err := client.Edit(context.Background(), domain.ImageEditRequest{
		Prompt:            "edit prompt",
		Model:             "qwen-image-edit",
		ReferenceImageURL: "https://example.invalid/ref.png",
		Width:             320,
		Height:            180,
		OutputPath:        out,
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if resp.Model != "qwen-image-edit" {
		t.Errorf("Model = %q, want echo of edit model", resp.Model)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output file missing: %v", err)
	}
}

func TestImageClient_Generate_ValidationErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "x.png")
	client := dryrun.NewImageClient()

	cases := []struct {
		name string
		req  domain.ImageRequest
	}{
		{"empty prompt", domain.ImageRequest{Model: "m", Width: 1, Height: 1, OutputPath: out}},
		{"empty model", domain.ImageRequest{Prompt: "p", Width: 1, Height: 1, OutputPath: out}},
		{"empty path", domain.ImageRequest{Prompt: "p", Model: "m", Width: 1, Height: 1}},
		{"zero width", domain.ImageRequest{Prompt: "p", Model: "m", Width: 0, Height: 1, OutputPath: out}},
		{"zero height", domain.ImageRequest{Prompt: "p", Model: "m", Width: 1, Height: 0, OutputPath: out}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client.Generate(context.Background(), tc.req)
			if !errors.Is(err, domain.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestImageClient_Generate_AtomicWrite_NoLeakedTempFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "scene", "shot.png")

	client := dryrun.NewImageClient()
	if _, err := client.Generate(context.Background(), domain.ImageRequest{
		Prompt:     "p",
		Model:      "m",
		Width:      32,
		Height:     32,
		OutputPath: out,
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(out))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" || (len(e.Name()) > 7 && e.Name()[:7] == ".dryrun") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}
