package dashscope

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// Compile-time guard: ImageClient must satisfy domain.ImageGenerator.
var _ domain.ImageGenerator = (*ImageClient)(nil)

const (
	// DefaultImageEndpointIntl is the Singapore (international) sync
	// multimodal-generation base URL. Use this when the API key was issued at
	// modelstudio.console.alibabacloud.com.
	DefaultImageEndpointIntl = "https://dashscope-intl.aliyuncs.com"

	// DefaultImageEndpointCN is the China (Beijing) base URL. Use this when
	// the API key was issued at bailian.console.alibabacloud.com. API keys
	// for the two regions are not interchangeable.
	DefaultImageEndpointCN = "https://dashscope.aliyuncs.com"

	// imagePath is the synchronous multimodal-generation surface used by
	// qwen-image-2.0 / qwen-image-edit. Older async text2image endpoints have
	// been retired for these models — sync returns the result URL in a single
	// request, no polling required.
	imagePath = "/api/v1/services/aigc/multimodal-generation/generation"

	imageProvider = "dashscope"

	// costPerImage is the per-image cost estimate (USD) for qwen-image
	// series. Approximation; real billing is determined by DashScope.
	costPerImage = 0.02

	// imageDownloadLimit caps the bytes pulled from the result URL so a
	// runaway response cannot OOM the process. 50 MiB easily covers a 4K PNG
	// (a 1024×1024 PNG is typically 1–4 MiB).
	imageDownloadLimit = 50 << 20

	// jsonResponseLimit caps decoded JSON payloads.
	jsonResponseLimit = 1 << 20
)

// ImageClientConfig carries the construction-time parameters for ImageClient.
type ImageClientConfig struct {
	// Endpoint overrides the default DashScope base URL. When empty the
	// international (Singapore) endpoint is used. Region-aware callers should
	// pass DefaultImageEndpointCN explicitly when targeting Beijing.
	Endpoint string

	// APIKey is the DashScope access key. Required. Must match the region
	// of Endpoint — keys issued for Singapore are rejected by the Beijing
	// endpoint with HTTP 401 InvalidApiKey, and vice versa.
	APIKey string

	// Clock is the clock used for elapsed-time accounting. nil means real time.
	Clock clock.Clock
}

// ImageClient implements domain.ImageGenerator for DashScope qwen-image-2.0
// and qwen-image-edit via the synchronous multimodal-generation API. It does
// not own retry, backoff, or rate-limiting — those are composed by the image
// track via CallLimiter.Do + llmclient.WithRetry.
type ImageClient struct {
	httpClient *http.Client
	cfg        ImageClientConfig
	clk        clock.Clock
}

// NewImageClient constructs an ImageClient. httpClient must be non-nil
// (never use http.DefaultClient in production paths; callers own lifecycle).
func NewImageClient(httpClient *http.Client, cfg ImageClientConfig) (*ImageClient, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("dashscope image: %w: http client is nil", domain.ErrValidation)
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("dashscope image: %w: api key is empty", domain.ErrValidation)
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultImageEndpointIntl
	}
	cfg.Endpoint = strings.TrimRight(cfg.Endpoint, "/")
	clk := cfg.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &ImageClient{httpClient: httpClient, cfg: cfg, clk: clk}, nil
}

// imageRequestBody is the multimodal-generation request envelope.
type imageRequestBody struct {
	Model      string                 `json:"model"`
	Input      imageRequestInput      `json:"input"`
	Parameters imageRequestParameters `json:"parameters"`
}

type imageRequestInput struct {
	Messages []imageMessage `json:"messages"`
}

type imageMessage struct {
	Role    string                `json:"role"`
	Content []imageMessageContent `json:"content"`
}

// imageMessageContent carries either a text instruction or an image
// reference. The DashScope schema dispatches on which key is populated; both
// are omitempty so each entry is unambiguous.
type imageMessageContent struct {
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"`
}

type imageRequestParameters struct {
	Size      string `json:"size,omitempty"`
	N         int    `json:"n,omitempty"`
	Watermark *bool  `json:"watermark,omitempty"`
}

// imageResponseBody mirrors the multimodal-generation response shape.
type imageResponseBody struct {
	Output    imageResponseOutput `json:"output"`
	Usage     *imageResponseUsage `json:"usage,omitempty"`
	RequestID string              `json:"request_id,omitempty"`
	Code      string              `json:"code,omitempty"`
	Message   string              `json:"message,omitempty"`
}

type imageResponseOutput struct {
	Choices []imageResponseChoice `json:"choices"`
}

type imageResponseChoice struct {
	FinishReason string                `json:"finish_reason,omitempty"`
	Message      imageResponseMessage  `json:"message"`
}

type imageResponseMessage struct {
	Role    string                       `json:"role,omitempty"`
	Content []imageResponseMessageContent `json:"content"`
}

type imageResponseMessageContent struct {
	Image string `json:"image,omitempty"`
	Text  string `json:"text,omitempty"`
}

type imageResponseUsage struct {
	ImageCount int `json:"image_count,omitempty"`
	Width      int `json:"width,omitempty"`
	Height     int `json:"height,omitempty"`
}

// Generate submits a text-to-image job and writes the resulting PNG to
// req.OutputPath. Synchronous: a single request returns the result URL,
// which is then downloaded and persisted atomically.
func (c *ImageClient) Generate(ctx context.Context, req domain.ImageRequest) (domain.ImageResponse, error) {
	if req.Prompt == "" {
		return domain.ImageResponse{}, fmt.Errorf("dashscope image generate: %w: prompt is empty", domain.ErrValidation)
	}
	if req.Model == "" {
		return domain.ImageResponse{}, fmt.Errorf("dashscope image generate: %w: model is empty", domain.ErrValidation)
	}
	if req.OutputPath == "" {
		return domain.ImageResponse{}, fmt.Errorf("dashscope image generate: %w: output path is empty", domain.ErrValidation)
	}
	body := buildRequestBody(req.Model, req.Prompt, "", req.Width, req.Height)
	return c.run(ctx, body, req.Model, req.OutputPath)
}

// Edit submits a reference-conditioned image-edit job. The reference is
// embedded as either an HTTP(S) URL or a base64 data URL via the messages
// content array — the image-track wires a fetcher that pre-converts DDG
// references to data URLs (DashScope cannot reach DDG directly).
func (c *ImageClient) Edit(ctx context.Context, req domain.ImageEditRequest) (domain.ImageResponse, error) {
	if req.Prompt == "" {
		return domain.ImageResponse{}, fmt.Errorf("dashscope image edit: %w: prompt is empty", domain.ErrValidation)
	}
	if req.Model == "" {
		return domain.ImageResponse{}, fmt.Errorf("dashscope image edit: %w: model is empty", domain.ErrValidation)
	}
	if req.ReferenceImageURL == "" {
		return domain.ImageResponse{}, fmt.Errorf("dashscope image edit: %w: reference image url is empty", domain.ErrValidation)
	}
	if req.OutputPath == "" {
		return domain.ImageResponse{}, fmt.Errorf("dashscope image edit: %w: output path is empty", domain.ErrValidation)
	}
	body := buildRequestBody(req.Model, req.Prompt, req.ReferenceImageURL, req.Width, req.Height)
	return c.run(ctx, body, req.Model, req.OutputPath)
}

func buildRequestBody(model, prompt, refImage string, width, height int) imageRequestBody {
	content := make([]imageMessageContent, 0, 2)
	if refImage != "" {
		content = append(content, imageMessageContent{Image: refImage})
	}
	content = append(content, imageMessageContent{Text: prompt})
	watermarkOff := false
	return imageRequestBody{
		Model: model,
		Input: imageRequestInput{
			Messages: []imageMessage{
				{Role: "user", Content: content},
			},
		},
		Parameters: imageRequestParameters{
			Size:      formatSize(width, height),
			N:         1,
			Watermark: &watermarkOff,
		},
	}
}

// run executes the synchronous request → download → atomic-write sequence.
// HTTP status taxonomy:
//
//	429               → domain.ErrRateLimited  (retryable)
//	5xx / dial errors → domain.ErrStageFailed  (retryable)
//	4xx (except 429)  → domain.ErrValidation   (non-retryable)
func (c *ImageClient) run(ctx context.Context, body imageRequestBody, model, outputPath string) (domain.ImageResponse, error) {
	start := c.clk.Now()

	resultURL, err := c.submit(ctx, body)
	if err != nil {
		return domain.ImageResponse{}, err
	}

	imageBytes, err := c.fetchImage(ctx, resultURL)
	if err != nil {
		return domain.ImageResponse{}, err
	}
	if len(imageBytes) == 0 {
		return domain.ImageResponse{}, fmt.Errorf("dashscope image: %w: provider returned empty image body", domain.ErrStageFailed)
	}

	if err := writeFileAtomic(outputPath, imageBytes); err != nil {
		return domain.ImageResponse{}, fmt.Errorf("dashscope image: write image: %w", err)
	}

	durationMs := c.clk.Now().Sub(start).Milliseconds()
	if durationMs < 0 {
		durationMs = 0
	}

	return domain.ImageResponse{
		ImagePath:  outputPath,
		Model:      model,
		Provider:   imageProvider,
		CostUSD:    costPerImage,
		DurationMs: durationMs,
	}, nil
}

// submit POSTs the synchronous job and returns the first content image URL.
// Unlike the legacy async surface, no polling is required — the response
// body either carries the result URL or an error code.
func (c *ImageClient) submit(ctx context.Context, body imageRequestBody) (string, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("dashscope image: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint+imagePath, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("dashscope image: create submit request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("dashscope image: http submit: %w: %v", domain.ErrStageFailed, err)
	}
	defer resp.Body.Close()

	if err := classifyStatus("submit", resp); err != nil {
		return "", err
	}

	var payload imageResponseBody
	if err := json.NewDecoder(io.LimitReader(resp.Body, jsonResponseLimit)).Decode(&payload); err != nil {
		return "", fmt.Errorf("dashscope image: decode submit response: %w: %v", domain.ErrStageFailed, err)
	}
	if payload.Code != "" {
		return "", fmt.Errorf("dashscope image: %w: code=%q message=%q", domain.ErrValidation, payload.Code, payload.Message)
	}
	if len(payload.Output.Choices) == 0 {
		return "", fmt.Errorf("dashscope image: %w: response missing output.choices", domain.ErrStageFailed)
	}
	for _, c := range payload.Output.Choices[0].Message.Content {
		if c.Image != "" {
			return c.Image, nil
		}
	}
	return "", fmt.Errorf("dashscope image: %w: response output.choices[0].message.content has no image", domain.ErrStageFailed)
}

// fetchImage downloads the result URL DashScope returned, capped by
// imageDownloadLimit. Errors map to ErrStageFailed (retryable) since the URL
// is short-lived: a transient failure here is recoverable by re-issuing the
// whole job, which mints a fresh URL.
func (c *ImageClient) fetchImage(ctx context.Context, url string) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("dashscope image: create download request: %w", err)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dashscope image: download image: %w: %v", domain.ErrStageFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("dashscope image: download image: %w: status %d: %s", domain.ErrStageFailed, resp.StatusCode, body)
	}
	// Limit + 1 so we can distinguish "exactly at cap" (allowed) from
	// "exceeded cap" (rejected) via length comparison after read.
	imageBytes, err := io.ReadAll(io.LimitReader(resp.Body, imageDownloadLimit+1))
	if err != nil {
		return nil, fmt.Errorf("dashscope image: read image body: %w: %v", domain.ErrStageFailed, err)
	}
	if len(imageBytes) > imageDownloadLimit {
		return nil, fmt.Errorf("dashscope image: %w: image exceeds %d byte cap", domain.ErrValidation, imageDownloadLimit)
	}
	return imageBytes, nil
}

// classifyStatus inspects an HTTP response and returns nil for 2xx or a
// canonical domain error otherwise. The caller is responsible for closing
// resp.Body — we only peek at a small prefix for diagnostic context.
func classifyStatus(phase string, resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("dashscope image %s: %w: status %d: %s", phase, domain.ErrRateLimited, resp.StatusCode, body)
	case resp.StatusCode >= 500:
		return fmt.Errorf("dashscope image %s: %w: status %d: %s", phase, domain.ErrStageFailed, resp.StatusCode, body)
	default:
		return fmt.Errorf("dashscope image %s: %w: status %d: %s", phase, domain.ErrValidation, resp.StatusCode, body)
	}
}

// writeFileAtomic writes data to path via temp+rename so a partial-failure
// run never leaves a half-written PNG that subsequent reads would mis-trust.
// Story 5.4 idempotency depends on this — image_track verifies file presence
// after the call, and the caller's WithRetry re-runs the whole job on
// failure. A non-atomic write would surface as "file exists but is corrupt"
// which the retry path cannot detect.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".dashscope-img-*.tmp")
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

func formatSize(width, height int) string {
	if width <= 0 {
		width = 1024
	}
	if height <= 0 {
		height = 1024
	}
	return fmt.Sprintf("%d*%d", width, height)
}
