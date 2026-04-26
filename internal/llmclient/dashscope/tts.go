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
	// DefaultTTSEndpointIntl is the Singapore (international) qwen3-tts URL.
	// Use this when the API key was issued at modelstudio.console.alibabacloud.com.
	DefaultTTSEndpointIntl = "https://dashscope-intl.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"

	// DefaultTTSEndpointCN is the China (Beijing) qwen3-tts URL.
	// Use this when the API key was issued at bailian.console.alibabacloud.com.
	// API keys for the two regions are not interchangeable.
	DefaultTTSEndpointCN = "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"

	ttsProvider = "dashscope"

	// costPerChar is the per-character cost estimate (USD) for qwen3-tts-flash.
	// Approximation; real billing is determined by DashScope.
	costPerChar = 0.000005

	// audioDownloadLimit caps the bytes pulled from the audio URL so a
	// runaway response cannot OOM the process. 50 MiB easily covers a
	// per-scene WAV — multi-minute narration at 44.1 kHz / 16-bit mono is
	// ~10 MiB/min — while still surfacing absurd payloads as a hard error.
	audioDownloadLimit = 50 << 20
)

// TTSClientConfig carries the construction-time parameters for TTSClient.
type TTSClientConfig struct {
	// Endpoint overrides the default DashScope TTS URL. When empty the
	// international (Singapore) endpoint is used. Region-aware callers should
	// pass DefaultTTSEndpointCN explicitly when targeting Beijing.
	Endpoint string

	// APIKey is the DashScope access key. Required. Must match the region
	// of Endpoint — keys issued for Singapore are rejected by the Beijing
	// endpoint with HTTP 401 InvalidApiKey, and vice versa.
	APIKey string

	// LanguageType is forwarded as input.language_type. Empty means omit
	// the field and let the model auto-detect. For Korean narration, set
	// "Korean" so pronunciation and intonation match the text.
	LanguageType string
}

// TTSClient implements domain.TTSSynthesizer for DashScope qwen3-tts via the
// MultiModalConversation HTTP surface (multimodal-generation/generation).
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
		cfg.Endpoint = DefaultTTSEndpointIntl
	}
	return &TTSClient{httpClient: httpClient, cfg: cfg}, nil
}

type ttsRequestBody struct {
	Model string   `json:"model"`
	Input ttsInput `json:"input"`
}

type ttsInput struct {
	Text         string `json:"text"`
	Voice        string `json:"voice"`
	LanguageType string `json:"language_type,omitempty"`
}

type ttsResponseBody struct {
	Output    ttsResponseOutput `json:"output"`
	Usage     *ttsResponseUsage `json:"usage,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	Code      string            `json:"code,omitempty"`
	Message   string            `json:"message,omitempty"`
}

type ttsResponseOutput struct {
	Audio        ttsResponseAudio `json:"audio"`
	FinishReason string           `json:"finish_reason,omitempty"`
}

type ttsResponseAudio struct {
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
}

type ttsResponseUsage struct {
	InputTokensTotal  int `json:"input_tokens_total,omitempty"`
	OutputTokensTotal int `json:"output_tokens_total,omitempty"`
}

// Synthesize POSTs to the MultiModalConversation endpoint, downloads the
// returned audio URL (valid 24h per DashScope), writes the bytes to
// req.OutputPath, and returns a populated TTSResponse. The client does NOT
// perform transliteration; req.Text must already be the final form.
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
			Text:         req.Text,
			Voice:        req.Voice,
			LanguageType: c.cfg.LanguageType,
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

	var payload ttsResponseBody
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return domain.TTSResponse{}, fmt.Errorf("dashscope tts: decode response: %w: %v", domain.ErrStageFailed, err)
	}
	if payload.Output.Audio.URL == "" {
		return domain.TTSResponse{}, fmt.Errorf("dashscope tts: %w: response missing output.audio.url (code=%q message=%q)", domain.ErrStageFailed, payload.Code, payload.Message)
	}

	audioBytes, err := c.fetchAudio(ctx, payload.Output.Audio.URL)
	if err != nil {
		return domain.TTSResponse{}, err
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

// fetchAudio GETs the transient audio URL DashScope returned and reads the
// body. Errors map to ErrStageFailed (retryable) since the URL is short-
// lived: a transient failure here is recoverable by re-issuing the synth
// request, which mints a fresh URL.
func (c *TTSClient) fetchAudio(ctx context.Context, url string) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("dashscope tts: create audio download request: %w", err)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dashscope tts: download audio: %w: %v", domain.ErrStageFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("dashscope tts: download audio: %w: status %d: %s", domain.ErrStageFailed, resp.StatusCode, body)
	}
	audio, err := io.ReadAll(io.LimitReader(resp.Body, audioDownloadLimit))
	if err != nil {
		return nil, fmt.Errorf("dashscope tts: read audio body: %w: %v", domain.ErrStageFailed, err)
	}
	return audio, nil
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
