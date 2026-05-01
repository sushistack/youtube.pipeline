package comfyui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// Compile-time guard: ImageClient must satisfy domain.ImageGenerator.
var _ domain.ImageGenerator = (*ImageClient)(nil)

const (
	// imageProvider tags the value reported in ImageResponse.Provider.
	// Audit log greps use this to distinguish ComfyUI runs from DashScope.
	imageProvider = "comfyui"

	// imageDownloadLimit caps the bytes pulled from the /view endpoint so a
	// runaway response cannot OOM the process. 50 MiB easily covers a 4K PNG.
	imageDownloadLimit = 50 << 20

	// pollInterval is the fixed cadence between /history polls. ComfyUI
	// completes a 4-step FLUX.2 Klein 4B render in 60–70s on RX 9060 XT;
	// 250ms gives the operator a roughly 280-poll budget per success path.
	pollInterval = 250 * time.Millisecond

	// pollMaxDuration is the cumulative wall-clock cap for /history polling.
	// Beyond this we surface ErrUpstreamTimeout so the caller's WithRetry can
	// re-issue the entire job.
	pollMaxDuration = 300 * time.Second
)

// ImageClientConfig carries construction-time parameters.
type ImageClientConfig struct {
	// Endpoint is the ComfyUI base URL (e.g. "http://127.0.0.1:8188").
	// Must be `scheme://host[:port]` — trailing slashes are stripped, but
	// non-empty paths and missing schemes are rejected at construction.
	Endpoint string

	// ClientID is the client_id passed on every /prompt submit. Empty means
	// "generate a UUIDv4 at construction time" — preferred so each ImageClient
	// instance has its own queue identity.
	ClientID string

	// Clock is the clock used for elapsed accounting and polling cadence.
	// nil → RealClock.
	Clock clock.Clock

	// LoRAName is the LoRA filename injected into both the t2i and edit
	// workflows. Empty disables injection — the base model runs unchanged.
	// The file must already exist under ComfyUI's `models/loras/` directory;
	// validation happens server-side via /prompt node_errors.
	LoRAName string

	// LoRAStrengthModel scales the LoRA's diffusion-model contribution.
	// Ignored when LoRAName is empty.
	LoRAStrengthModel float64

	// LoRAStrengthClip scales the LoRA's text-encoder contribution.
	// Decoupled from the model strength so prompt bleed can be attenuated
	// independently. Ignored when LoRAName is empty.
	LoRAStrengthClip float64
}

// ImageClient implements domain.ImageGenerator backed by a local ComfyUI
// 0.12.3 server. It owns no retry, backoff, or rate-limiting — those are
// composed by the image track via CallLimiter.Do + llmclient.WithRetry.
type ImageClient struct {
	httpClient *http.Client
	cfg        ImageClientConfig
	clk        clock.Clock
	clientID   string
}

// NewImageClient constructs an ImageClient. It validates the embedded
// workflow JSON at construction time so a missing `_meta.title` label fails
// the server start rather than the first Phase B call.
func NewImageClient(httpClient *http.Client, cfg ImageClientConfig) (*ImageClient, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("comfyui image: %w: http client is nil", domain.ErrValidation)
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("comfyui image: %w: endpoint is empty", domain.ErrValidation)
	}
	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("comfyui image: %w: endpoint %q must be scheme://host[:port]", domain.ErrValidation, cfg.Endpoint)
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("comfyui image: %w: endpoint %q must not include path/query/fragment", domain.ErrValidation, cfg.Endpoint)
	}

	if err := validateWorkflow(WorkflowT2I, false); err != nil {
		return nil, fmt.Errorf("comfyui image: t2i workflow invalid: %w", err)
	}
	if err := validateWorkflow(WorkflowEdit, true); err != nil {
		return nil, fmt.Errorf("comfyui image: edit workflow invalid: %w", err)
	}

	clk := cfg.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	clientID := cfg.ClientID
	if clientID == "" {
		generated, err := newUUIDv4()
		if err != nil {
			return nil, fmt.Errorf("comfyui image: generate client_id: %w", err)
		}
		clientID = generated
	}
	return &ImageClient{httpClient: httpClient, cfg: cfg, clk: clk, clientID: clientID}, nil
}

// Generate runs the t2i workflow.
func (c *ImageClient) Generate(ctx context.Context, req domain.ImageRequest) (domain.ImageResponse, error) {
	if req.Prompt == "" {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image generate: %w: prompt is empty", domain.ErrValidation)
	}
	if req.Model == "" {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image generate: %w: model is empty", domain.ErrValidation)
	}
	if req.OutputPath == "" {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image generate: %w: output path is empty", domain.ErrValidation)
	}
	if req.Width <= 0 || req.Height <= 0 {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image generate: %w: invalid dimensions %dx%d",
			domain.ErrValidation, req.Width, req.Height)
	}

	seed, err := newSeed()
	if err != nil {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image generate: seed: %w", err)
	}
	workflow, outputID, err := prepareWorkflow(WorkflowT2I, substitution{
		Prompt:            req.Prompt,
		Width:             req.Width,
		Height:            req.Height,
		Seed:              seed,
		LoRAName:          c.cfg.LoRAName,
		LoRAStrengthModel: c.cfg.LoRAStrengthModel,
		LoRAStrengthClip:  c.cfg.LoRAStrengthClip,
	})
	if err != nil {
		return domain.ImageResponse{}, err
	}
	return c.run(ctx, workflow, outputID, req.Model, req.OutputPath)
}

// Edit runs the edit workflow. The reference image arrives as a
// `data:image/<mime>;base64,<payload>` URL (image_track.FetchReferenceImageAsDataURL
// upstream). We decode → upload → inject the assigned filename into LoadImage.
func (c *ImageClient) Edit(ctx context.Context, req domain.ImageEditRequest) (domain.ImageResponse, error) {
	if req.Prompt == "" {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image edit: %w: prompt is empty", domain.ErrValidation)
	}
	if req.Model == "" {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image edit: %w: model is empty", domain.ErrValidation)
	}
	if req.ReferenceImageURL == "" {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image edit: %w: reference image url is empty", domain.ErrValidation)
	}
	if req.OutputPath == "" {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image edit: %w: output path is empty", domain.ErrValidation)
	}
	if req.Width <= 0 || req.Height <= 0 {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image edit: %w: invalid dimensions %dx%d",
			domain.ErrValidation, req.Width, req.Height)
	}

	mime, payload, err := decodeDataURL(req.ReferenceImageURL)
	if err != nil {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image edit: %w", err)
	}
	ext, err := extFromMIME(mime)
	if err != nil {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image edit: %w", err)
	}
	uploadName, err := c.uploadReference(ctx, mime, ext, payload)
	if err != nil {
		return domain.ImageResponse{}, err
	}

	seed, err := newSeed()
	if err != nil {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image edit: seed: %w", err)
	}
	workflow, outputID, err := prepareWorkflow(WorkflowEdit, substitution{
		Prompt:             req.Prompt,
		Width:              req.Width,
		Height:             req.Height,
		Seed:               seed,
		ReferenceImageName: uploadName,
		RequireReference:   true,
		LoRAName:           c.cfg.LoRAName,
		LoRAStrengthModel:  c.cfg.LoRAStrengthModel,
		LoRAStrengthClip:   c.cfg.LoRAStrengthClip,
	})
	if err != nil {
		return domain.ImageResponse{}, err
	}
	return c.run(ctx, workflow, outputID, req.Model, req.OutputPath)
}

func (c *ImageClient) uploadReference(ctx context.Context, mimeType, ext string, payload []byte) (string, error) {
	suffix, err := randomHex(8)
	if err != nil {
		return "", fmt.Errorf("comfyui image edit: random suffix: %w", err)
	}
	filename := "ref-" + suffix + ext
	name, err := uploadImage(ctx, c.httpClient, c.cfg.Endpoint, filename, mimeType, payload)
	if err != nil {
		return "", err
	}
	return name, nil
}

// run submits the workflow, polls history, downloads the result, and writes
// atomically to outputPath. HTTP status taxonomy and polling timeout follow
// the spec's Boundaries & Constraints block.
func (c *ImageClient) run(ctx context.Context, workflow []byte, outputID, model, outputPath string) (domain.ImageResponse, error) {
	start := c.clk.Now()

	promptID, err := submitPrompt(ctx, c.httpClient, c.cfg.Endpoint, c.clientID, workflow)
	if err != nil {
		return domain.ImageResponse{}, err
	}

	entry, err := c.pollUntilDone(ctx, promptID)
	if err != nil {
		return domain.ImageResponse{}, err
	}

	img, err := pickOutputImage(entry, outputID)
	if err != nil {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image: %w: prompt_id=%s", err, promptID)
	}

	imageBytes, err := downloadView(ctx, c.httpClient, c.cfg.Endpoint, img, imageDownloadLimit)
	if err != nil {
		return domain.ImageResponse{}, err
	}
	if len(imageBytes) == 0 {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image: %w: provider returned empty body", domain.ErrStageFailed)
	}
	if err := writeFileAtomic(outputPath, imageBytes); err != nil {
		return domain.ImageResponse{}, fmt.Errorf("comfyui image: write image: %w", err)
	}

	durationMs := c.clk.Now().Sub(start).Milliseconds()
	if durationMs < 0 {
		durationMs = 0
	}
	return domain.ImageResponse{
		ImagePath:  outputPath,
		Model:      model,
		Provider:   imageProvider,
		CostUSD:    0,
		DurationMs: durationMs,
	}, nil
}

// pollUntilDone polls /history at pollInterval cadence up to pollMaxDuration.
// Cadence sleep uses c.clk so tests can fast-forward.
func (c *ImageClient) pollUntilDone(ctx context.Context, promptID string) (historyEntry, error) {
	start := c.clk.Now()
	for {
		if err := ctx.Err(); err != nil {
			return historyEntry{}, err
		}
		entry, present, err := fetchHistory(ctx, c.httpClient, c.cfg.Endpoint, promptID)
		if err != nil {
			return historyEntry{}, err
		}
		if present {
			if entry.Status != nil && entry.Status.StatusStr == "error" {
				return historyEntry{}, fmt.Errorf("comfyui image: %w: prompt_id=%s workflow execution error: %s",
					domain.ErrValidation, promptID, formatFirstStatusMessage(entry.Status))
			}
			return entry, nil
		}
		if c.clk.Now().Sub(start) >= pollMaxDuration {
			return historyEntry{}, fmt.Errorf("comfyui image: %w: polling exceeded %s for prompt_id=%s",
				domain.ErrUpstreamTimeout, pollMaxDuration, promptID)
		}
		if err := c.clk.Sleep(ctx, pollInterval); err != nil {
			return historyEntry{}, err
		}
	}
}

// pickOutputImage looks up the SaveImage node by its ID (captured from the
// workflow's OUTPUT_IMAGE label at prepare time) so a workflow with multiple
// SaveImage nodes still selects deterministically. Falls back to first-found
// only when the ID is absent (defensive — should not happen in practice).
func pickOutputImage(entry historyEntry, outputID string) (historyImage, error) {
	if outputID != "" {
		if out, ok := entry.Outputs[outputID]; ok && len(out.Images) > 0 {
			return out.Images[0], nil
		}
	}
	for _, out := range entry.Outputs {
		if len(out.Images) > 0 {
			return out.Images[0], nil
		}
	}
	return historyImage{}, fmt.Errorf("%w: history outputs has no image", domain.ErrStageFailed)
}

func formatFirstStatusMessage(status *historyStatus) string {
	if status == nil || len(status.Messages) == 0 {
		return "no message"
	}
	first := status.Messages[0]
	parts := make([]string, 0, len(first))
	for _, raw := range first {
		parts = append(parts, string(raw))
	}
	return strings.Join(parts, " ")
}

// decodeDataURL parses `data:image/<mime>;base64,<payload>` and returns
// (mime, decoded). Anything that does not match this exact contract is an
// ErrValidation — the upstream image_track always emits this shape.
func decodeDataURL(url string) (string, []byte, error) {
	if !strings.HasPrefix(url, "data:") {
		return "", nil, fmt.Errorf("%w: reference url is not a data url", domain.ErrValidation)
	}
	rest := strings.TrimPrefix(url, "data:")
	commaIdx := strings.Index(rest, ",")
	if commaIdx < 0 {
		return "", nil, fmt.Errorf("%w: reference url missing payload separator", domain.ErrValidation)
	}
	header := rest[:commaIdx]
	body := rest[commaIdx+1:]
	if !strings.HasSuffix(header, ";base64") {
		return "", nil, fmt.Errorf("%w: reference url not base64-encoded", domain.ErrValidation)
	}
	mime := strings.TrimSuffix(header, ";base64")
	if !strings.HasPrefix(mime, "image/") {
		return "", nil, fmt.Errorf("%w: reference url mime %q is not an image", domain.ErrValidation, mime)
	}
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return "", nil, fmt.Errorf("%w: reference url base64 decode: %v", domain.ErrValidation, err)
	}
	if len(decoded) == 0 {
		return "", nil, fmt.Errorf("%w: reference url payload is empty", domain.ErrValidation)
	}
	return mime, decoded, nil
}

// extFromMIME maps the four image MIME types ComfyUI's LoadImage supports
// to a filename extension. Unknown types are rejected at construction so the
// caller fails fast instead of triggering a 300s polling timeout downstream.
func extFromMIME(mime string) (string, error) {
	switch mime {
	case "image/png":
		return ".png", nil
	case "image/jpeg", "image/jpg":
		return ".jpg", nil
	case "image/webp":
		return ".webp", nil
	case "image/gif":
		return ".gif", nil
	default:
		return "", fmt.Errorf("%w: reference url mime %q is not a supported image type (png/jpeg/webp/gif)", domain.ErrValidation, mime)
	}
}

// writeFileAtomic writes data to path via temp+rename. Independent of
// dashscope/dryrun — the comfyui package owns its own implementation so it
// has zero cross-package coupling.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".comfyui-img-*.tmp")
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

// newUUIDv4 generates an RFC 4122 UUIDv4 from crypto/rand. We do not import
// github.com/google/uuid because crypto/rand suffices and adds no module-graph
// coupling.
func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// newSeed returns a positive-int63 seed for the RandomNoise node. ComfyUI
// stores noise_seed as an int64; using crypto/rand keeps test runs from
// trivially colliding across short test bursts.
func newSeed() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	v := int64(binary.BigEndian.Uint64(b[:]) & 0x7fffffffffffffff)
	return v, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
