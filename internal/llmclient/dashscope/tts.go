package dashscope

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

const (
	// defaultTTSEndpoint is the DashScope qwen3-tts HTTP endpoint.
	// The region-specific URL may be overridden via TTSClientConfig.Endpoint.
	defaultTTSEndpoint = "https://dashscope.aliyuncs.com/api/v1/services/aigc/text-to-speech/generation"

	// ttsProvider is the canonical provider label written to TTSResponse.
	ttsProvider = "dashscope"

	// costPerChar is the per-character cost estimate (USD) for qwen3-tts-flash.
	// This is an approximation; real billing is determined by DashScope.
	costPerChar = 0.000005
)

// TTSClientConfig carries the construction-time parameters for TTSClient.
type TTSClientConfig struct {
	// Endpoint overrides the default DashScope TTS URL (for regional routing or mocking in tests).
	Endpoint string

	// APIKey is the DashScope access key. Required.
	APIKey string
}

// TTSClient implements domain.TTSSynthesizer for DashScope qwen3-tts-flash.
// It does not own retry, backoff, or rate-limiting — those are composed by
// the TTS track via CallLimiter.Do + llmclient.WithRetry.
type TTSClient struct {
	httpClient *http.Client
	cfg        TTSClientConfig
}

// NewTTSClient constructs a TTSClient. httpClient must be non-nil (never
// use http.DefaultClient in production paths; callers own lifecycle).
func NewTTSClient(httpClient *http.Client, cfg TTSClientConfig) (*TTSClient, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("dashscope tts: %w: http client is nil", domain.ErrValidation)
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("dashscope tts: %w: api key is empty", domain.ErrValidation)
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultTTSEndpoint
	}
	return &TTSClient{httpClient: httpClient, cfg: cfg}, nil
}

// ttsRequestBody is the JSON payload sent to DashScope.
type ttsRequestBody struct {
	Model      string         `json:"model"`
	Input      ttsInput       `json:"input"`
	Parameters ttsParameters  `json:"parameters"`
}

type ttsInput struct {
	Text  string `json:"text"`
	Voice string `json:"voice"`
}

type ttsParameters struct {
	Format string `json:"format"`
}

// Synthesize issues a POST to DashScope, writes the returned audio bytes to
// req.OutputPath, and returns a populated TTSResponse. The client does NOT
// perform transliteration; req.Text must already be the final form to synthesize.
//
// HTTP status taxonomy (mirrors retry.go):
//
//	429               → domain.ErrRateLimited  (retryable)
//	5xx / timeout     → domain.ErrStageFailed  (retryable)
//	4xx (except 429)  → domain.ErrValidation   (non-retryable)
func (c *TTSClient) Synthesize(ctx context.Context, req domain.TTSRequest) (domain.TTSResponse, error) {
	if req.Text == "" {
		return domain.TTSResponse{}, fmt.Errorf("dashscope tts synthesize: %w: text is empty", domain.ErrValidation)
	}
	if req.OutputPath == "" {
		return domain.TTSResponse{}, fmt.Errorf("dashscope tts synthesize: %w: output path is empty", domain.ErrValidation)
	}

	format := req.Format
	if format == "" {
		format = "wav"
	}

	body := ttsRequestBody{
		Model: req.Model,
		Input: ttsInput{
			Text:  req.Text,
			Voice: req.Voice,
		},
		Parameters: ttsParameters{
			Format: format,
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return domain.TTSResponse{}, fmt.Errorf("dashscope tts: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return domain.TTSResponse{}, fmt.Errorf("dashscope tts: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.TTSResponse{}, fmt.Errorf("dashscope tts: http: %w: %v", domain.ErrStageFailed, err)
	}
	defer resp.Body.Close()

	if err := c.checkStatus(resp); err != nil {
		return domain.TTSResponse{}, err
	}

	audioBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.TTSResponse{}, fmt.Errorf("dashscope tts: read body: %w: %v", domain.ErrStageFailed, err)
	}
	if len(audioBytes) == 0 {
		return domain.TTSResponse{}, fmt.Errorf("dashscope tts: %w: provider returned empty audio body", domain.ErrStageFailed)
	}

	if err := os.WriteFile(req.OutputPath, audioBytes, 0o644); err != nil {
		return domain.TTSResponse{}, fmt.Errorf("dashscope tts: write audio: %w", err)
	}

	// Estimate duration from file size for wav only (44.1kHz 16-bit mono PCM
	// ≈ 176400 bytes/second). For compressed formats like mp3 the byte rate
	// varies by bitrate, so we return 0 and let a downstream probe fill the
	// authoritative value.
	var durationMs int64
	if format == "wav" {
		const wavBytesPerSecond = 176_400
		durationMs = int64(len(audioBytes)) * 1000 / wavBytesPerSecond
	}

	costUSD := float64(len([]rune(req.Text))) * costPerChar

	return domain.TTSResponse{
		AudioPath:  req.OutputPath,
		DurationMs: durationMs,
		Model:      req.Model,
		Provider:   ttsProvider,
		CostUSD:    costUSD,
	}, nil
}

// checkStatus maps HTTP status codes to canonical domain errors.
func (c *TTSClient) checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("dashscope tts: %w: status %d: %s", domain.ErrRateLimited, resp.StatusCode, body)
	case resp.StatusCode >= 500:
		return fmt.Errorf("dashscope tts: %w: status %d: %s", domain.ErrStageFailed, resp.StatusCode, body)
	default:
		return fmt.Errorf("dashscope tts: %w: status %d: %s", domain.ErrValidation, resp.StatusCode, body)
	}
}
