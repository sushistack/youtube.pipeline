package dryrun

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// Provider is the value reported in ImageResponse.Provider / TTSResponse.Provider
// for fake calls. Audit log greps use this to distinguish dry-run runs from
// real ones at a glance.
const Provider = "dryrun"

// placeholderColor is the neutral fill applied to every dry-run image. The
// hex value mirrors a "missing-asset" placeholder feel so the operator does
// not mistake the dry-run output for production-ready imagery.
var placeholderColor = color.RGBA{R: 0x2a, G: 0x2a, B: 0x2a, A: 0xff}

// ImageClient is a fake domain.ImageGenerator that writes a solid-fill PNG
// to req.OutputPath. It does not contact any external service.
type ImageClient struct{}

// NewImageClient constructs a dry-run ImageClient. It takes no configuration
// because there is nothing to configure: the placeholder color, encoder, and
// atomic-write semantics are fixed.
func NewImageClient() *ImageClient {
	return &ImageClient{}
}

// Generate writes a solid-fill PNG of (req.Width × req.Height) at
// req.OutputPath. Validation mirrors dashscope.ImageClient so the same
// caller-side error handling works in both modes.
func (c *ImageClient) Generate(ctx context.Context, req domain.ImageRequest) (domain.ImageResponse, error) {
	if req.Prompt == "" {
		return domain.ImageResponse{}, fmt.Errorf("dryrun image generate: %w: prompt is empty", domain.ErrValidation)
	}
	if req.Model == "" {
		return domain.ImageResponse{}, fmt.Errorf("dryrun image generate: %w: model is empty", domain.ErrValidation)
	}
	if req.OutputPath == "" {
		return domain.ImageResponse{}, fmt.Errorf("dryrun image generate: %w: output path is empty", domain.ErrValidation)
	}
	return c.writePlaceholder(ctx, req.Width, req.Height, req.Model, req.OutputPath)
}

// Edit ignores the reference image — dry-run output is the same flat
// placeholder either way — but otherwise honors the same contract as
// dashscope.ImageClient.Edit.
func (c *ImageClient) Edit(ctx context.Context, req domain.ImageEditRequest) (domain.ImageResponse, error) {
	if req.Prompt == "" {
		return domain.ImageResponse{}, fmt.Errorf("dryrun image edit: %w: prompt is empty", domain.ErrValidation)
	}
	if req.Model == "" {
		return domain.ImageResponse{}, fmt.Errorf("dryrun image edit: %w: model is empty", domain.ErrValidation)
	}
	if req.OutputPath == "" {
		return domain.ImageResponse{}, fmt.Errorf("dryrun image edit: %w: output path is empty", domain.ErrValidation)
	}
	return c.writePlaceholder(ctx, req.Width, req.Height, req.Model, req.OutputPath)
}

func (c *ImageClient) writePlaceholder(_ context.Context, width, height int, model, outputPath string) (domain.ImageResponse, error) {
	if width <= 0 || height <= 0 {
		return domain.ImageResponse{}, fmt.Errorf("dryrun image: %w: invalid dimensions %dx%d", domain.ErrValidation, width, height)
	}

	start := time.Now()

	rect := image.Rect(0, 0, width, height)
	img := image.NewRGBA(rect)
	// image.Uniform fill avoids a per-pixel loop that would dominate the
	// budget at 2688×1536 (~12 M pixels). draw.Draw with a Uniform source
	// dispatches to a fast path inside image/draw and writes the same byte
	// sequence the explicit loop would.
	draw.Draw(img, rect, &image.Uniform{C: placeholderColor}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return domain.ImageResponse{}, fmt.Errorf("dryrun image: encode png: %w", err)
	}

	if err := writeFileAtomic(outputPath, buf.Bytes()); err != nil {
		return domain.ImageResponse{}, fmt.Errorf("dryrun image: write png: %w", err)
	}

	return domain.ImageResponse{
		ImagePath:  outputPath,
		Model:      model,
		Provider:   Provider,
		CostUSD:    0,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

// writeFileAtomic writes data to path via temp+rename so a partial-failure
// run never leaves a half-written placeholder that subsequent reads would
// mis-trust. Mirrors dashscope.writeFileAtomic — kept independent so the
// dryrun package has zero dependency on dashscope.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".dryrun-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
