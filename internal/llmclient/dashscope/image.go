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
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// Compile-time guard: ImageClient must satisfy domain.ImageGenerator.
var _ domain.ImageGenerator = (*ImageClient)(nil)

const (
	// DefaultImageEndpointIntl is the Singapore (international) text2image
	// async submission base URL. Use this when the API key was issued at
	// modelstudio.console.alibabacloud.com.
	DefaultImageEndpointIntl = "https://dashscope-intl.aliyuncs.com"

	// DefaultImageEndpointCN is the China (Beijing) base URL. Use this when
	// the API key was issued at bailian.console.alibabacloud.com. API keys
	// for the two regions are not interchangeable.
	DefaultImageEndpointCN = "https://dashscope.aliyuncs.com"

	imageSubmitPath = "/api/v1/services/aigc/text2image/image-synthesis"
	imageTaskPath   = "/api/v1/tasks/"

	imageProvider = "dashscope"

	// costPerImage is the per-image cost estimate (USD) for qwen-image /
	// qwen-image-edit. Approximation; real billing is determined by DashScope.
	// Source: https://help.aliyun.com/zh/model-studio/text-to-image
	costPerImage = 0.02

	// imageDownloadLimit caps the bytes pulled from the result URL so a
	// runaway response cannot OOM the process. 50 MiB easily covers a 4K PNG
	// (a 1024×1024 PNG is typically 1–4 MiB).
	imageDownloadLimit = 50 << 20

	// jsonResponseLimit caps decoded JSON payloads.
	jsonResponseLimit = 1 << 20

	// pollMaxWall is the cumulative wall-clock cap for polling a single
	// text2image task. qwen-image jobs typically resolve in <30s; jobs that
	// exceed this window are surfaced as ErrUpstreamTimeout so the caller's
	// WithRetry can re-issue submission rather than blocking indefinitely.
	pollMaxWall = 60 * time.Second

	// pollInitialBackoff is the first sleep before the first poll.
	pollInitialBackoff = 2 * time.Second

	// pollMaxBackoff caps each individual sleep step.
	pollMaxBackoff = 30 * time.Second
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

	// Clock is the clock used for poll backoff. nil means real time.
	Clock clock.Clock
}

// ImageClient implements domain.ImageGenerator for DashScope qwen-image and
// qwen-image-edit via the asynchronous text2image jobs API. It does not own
// retry, backoff, or rate-limiting — those are composed by the image track
// via CallLimiter.Do + llmclient.WithRetry. The internal poll loop only
// converts the async job into a synchronous call shape; transport-level
// retries belong outside.
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

// imageRequestBody is the on-the-wire shape DashScope text2image accepts.
// Generate omits Input.RefImgs; Edit populates it with the reference URL.
type imageRequestBody struct {
	Model      string                 `json:"model"`
	Input      imageRequestInput      `json:"input"`
	Parameters imageRequestParameters `json:"parameters"`
}

type imageRequestInput struct {
	Prompt  string   `json:"prompt"`
	RefImgs []string `json:"ref_imgs,omitempty"`
}

type imageRequestParameters struct {
	Size string `json:"size"`
	N    int    `json:"n"`
}

type imageSubmitResponse struct {
	Output    imageSubmitOutput `json:"output"`
	RequestID string            `json:"request_id,omitempty"`
	Code      string            `json:"code,omitempty"`
	Message   string            `json:"message,omitempty"`
}

type imageSubmitOutput struct {
	TaskID     string `json:"task_id"`
	TaskStatus string `json:"task_status"`
}

type imageTaskResponse struct {
	Output    imageTaskOutput `json:"output"`
	RequestID string          `json:"request_id,omitempty"`
	Code      string          `json:"code,omitempty"`
	Message   string          `json:"message,omitempty"`
}

type imageTaskOutput struct {
	TaskID     string            `json:"task_id"`
	TaskStatus string            `json:"task_status"`
	Code       string            `json:"code,omitempty"`
	Message    string            `json:"message,omitempty"`
	Results    []imageTaskResult `json:"results,omitempty"`
}

type imageTaskResult struct {
	URL string `json:"url"`
}

// Generate submits a text-to-image job, polls until it resolves, downloads
// the result, and writes it atomically to req.OutputPath.
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
	body := imageRequestBody{
		Model: req.Model,
		Input: imageRequestInput{Prompt: req.Prompt},
		Parameters: imageRequestParameters{
			Size: formatSize(req.Width, req.Height),
			N:    1,
		},
	}
	return c.runJob(ctx, body, req.Model, req.OutputPath)
}

// Edit submits a reference-conditioned image-edit job (qwen-image-edit),
// polls, downloads, and writes the result atomically.
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
	body := imageRequestBody{
		Model: req.Model,
		Input: imageRequestInput{
			Prompt:  req.Prompt,
			RefImgs: []string{req.ReferenceImageURL},
		},
		Parameters: imageRequestParameters{
			Size: formatSize(req.Width, req.Height),
			N:    1,
		},
	}
	return c.runJob(ctx, body, req.Model, req.OutputPath)
}

// runJob executes the submit → poll → download → atomic-write sequence and
// records elapsed wall time. HTTP status taxonomy mirrors retry.go:
//
//	429               → domain.ErrRateLimited  (retryable)
//	5xx / dial errors → domain.ErrStageFailed  (retryable)
//	4xx (except 429)  → domain.ErrValidation   (non-retryable)
//	poll wall > cap   → domain.ErrUpstreamTimeout (retryable)
//	task FAILED       → domain.ErrValidation   (non-retryable; payload-level)
func (c *ImageClient) runJob(ctx context.Context, body imageRequestBody, model, outputPath string) (domain.ImageResponse, error) {
	start := c.clk.Now()

	taskID, err := c.submit(ctx, body)
	if err != nil {
		return domain.ImageResponse{}, err
	}

	resultURL, err := c.poll(ctx, taskID)
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

// submit POSTs the job and returns the task_id. The async header
// X-DashScope-Async:enable instructs DashScope to queue the job rather than
// stream synchronously — text2image is async-only.
func (c *ImageClient) submit(ctx context.Context, body imageRequestBody) (string, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("dashscope image: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint+imageSubmitPath, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("dashscope image: create submit request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	httpReq.Header.Set("X-DashScope-Async", "enable")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("dashscope image: http submit: %w: %v", domain.ErrStageFailed, err)
	}
	defer resp.Body.Close()

	if err := classifyStatus("submit", resp); err != nil {
		return "", err
	}

	var payload imageSubmitResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, jsonResponseLimit)).Decode(&payload); err != nil {
		return "", fmt.Errorf("dashscope image: decode submit response: %w: %v", domain.ErrStageFailed, err)
	}
	if payload.Output.TaskID == "" {
		return "", fmt.Errorf("dashscope image: %w: submit response missing task_id (code=%q message=%q)", domain.ErrStageFailed, payload.Code, payload.Message)
	}
	return payload.Output.TaskID, nil
}

// poll repeatedly GETs the task endpoint until the task resolves. Backoff
// doubles from pollInitialBackoff to pollMaxBackoff; the cumulative wall
// clock is capped at pollMaxWall, after which ErrUpstreamTimeout is returned.
func (c *ImageClient) poll(ctx context.Context, taskID string) (string, error) {
	deadline := c.clk.Now().Add(pollMaxWall)
	backoff := pollInitialBackoff

	for {
		// Sleep first — the async submit just returned, so the task is
		// almost never SUCCEEDED on the very first read.
		remaining := deadline.Sub(c.clk.Now())
		if remaining <= 0 {
			return "", fmt.Errorf("dashscope image: %w: poll exceeded %s (task %s)", domain.ErrUpstreamTimeout, pollMaxWall, taskID)
		}
		sleep := backoff
		if sleep > remaining {
			sleep = remaining
		}
		if err := c.clk.Sleep(ctx, sleep); err != nil {
			return "", err
		}

		status, resultURL, err := c.fetchTask(ctx, taskID)
		if err != nil {
			return "", err
		}
		switch status {
		case "SUCCEEDED":
			if resultURL == "" {
				return "", fmt.Errorf("dashscope image: %w: task %s succeeded but result url is empty", domain.ErrStageFailed, taskID)
			}
			return resultURL, nil
		case "FAILED", "CANCELED", "UNKNOWN":
			return "", fmt.Errorf("dashscope image: %w: task %s status=%s", domain.ErrValidation, taskID, status)
		case "PENDING", "RUNNING":
			// keep polling
		default:
			// Unexpected status — treat as terminal so we surface DashScope
			// API drift loudly rather than spinning indefinitely.
			return "", fmt.Errorf("dashscope image: %w: task %s unexpected status=%q", domain.ErrValidation, taskID, status)
		}

		backoff *= 2
		if backoff > pollMaxBackoff {
			backoff = pollMaxBackoff
		}
	}
}

func (c *ImageClient) fetchTask(ctx context.Context, taskID string) (string, string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.Endpoint+imageTaskPath+taskID, nil)
	if err != nil {
		return "", "", fmt.Errorf("dashscope image: create task request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", "", fmt.Errorf("dashscope image: http poll: %w: %v", domain.ErrStageFailed, err)
	}
	defer resp.Body.Close()

	if err := classifyStatus("poll", resp); err != nil {
		return "", "", err
	}

	var payload imageTaskResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, jsonResponseLimit)).Decode(&payload); err != nil {
		return "", "", fmt.Errorf("dashscope image: decode poll response: %w: %v", domain.ErrStageFailed, err)
	}

	var url string
	if len(payload.Output.Results) > 0 {
		url = payload.Output.Results[0].URL
	}
	return payload.Output.TaskStatus, url, nil
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
