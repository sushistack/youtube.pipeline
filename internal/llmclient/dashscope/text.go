package dashscope

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient"
)

const (
	// DefaultChatEndpointIntl is the OpenAI-compatible chat-completions URL for
	// the international (Singapore) region. This is the only supported region —
	// China (Beijing) endpoints are not reachable from this deployment, so the
	// region toggle was removed.
	DefaultChatEndpointIntl = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1/chat/completions"

	// defaultChatEndpoint is the fallback when TextClientConfig.Endpoint is
	// empty.
	defaultChatEndpoint = DefaultChatEndpointIntl

	// textProvider is the canonical provider label written to TextResponse and
	// used by the writer/critic distinct-providers contract. Matches the
	// "dashscope" string already in the settings provider enum.
	textProvider = "dashscope"

	// maxResponseBytes caps the success-path body read so a runaway/slow-loris
	// response cannot OOM the server. Chat-completion payloads with
	// max_tokens=1600 are well under 50 KiB; 1 MiB is a comfortable ceiling
	// that still surfaces "provider streamed garbage" as a hard failure.
	maxResponseBytes = 1 << 20

	// maxErrorBodySnippet bounds the bytes echoed back into the wrapped error
	// message on non-2xx responses, so a misbehaving provider cannot produce a
	// multi-megabyte log line.
	maxErrorBodySnippet = 256
)

// TextClientConfig carries the construction-time parameters for TextClient.
type TextClientConfig struct {
	APIKey   string
	Endpoint string
	Limiter  *llmclient.CallLimiter
	// Logger is the optional structured logger. When non-nil, the client emits
	// HTTP request/response boundary events with model + duration + status,
	// so a Phase A hang can be localized to the HTTP call vs. limiter wait
	// vs. response decode. Nil → no-op (existing tests stay valid).
	Logger *slog.Logger
}

// TextClient implements domain.TextGenerator for DashScope's OpenAI-compatible
// chat-completions endpoint (Qwen models). It does not own retry, backoff, or
// the writer/critic distinct-providers check — those are composed by the
// caller (PhaseARunner + agent constructors).
type TextClient struct {
	httpClient *http.Client
	cfg        TextClientConfig
}

// NewTextClient constructs a TextClient. httpClient must be non-nil; APIKey
// and Limiter are required. Endpoint defaults to the public DashScope
// compatible-mode URL when omitted.
func NewTextClient(httpClient *http.Client, cfg TextClientConfig) (*TextClient, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("dashscope text: %w: http client is nil", domain.ErrValidation)
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("dashscope text: %w: api key is empty", domain.ErrValidation)
	}
	if cfg.Limiter == nil {
		return nil, fmt.Errorf("dashscope text: %w: limiter is nil", domain.ErrValidation)
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultChatEndpoint
	}
	return &TextClient{httpClient: httpClient, cfg: cfg}, nil
}

type chatCompletionRequest struct {
	Model          string                  `json:"model"`
	Messages       []chatCompletionMessage `json:"messages"`
	MaxTokens      int                     `json:"max_tokens,omitempty"`
	Temperature    *float64                `json:"temperature,omitempty"`
	ResponseFormat *chatCompletionFormat   `json:"response_format,omitempty"`
}

type chatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionFormat struct {
	Type string `json:"type"`
}

type chatCompletionResponse struct {
	Model   string                 `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
	Usage   *chatCompletionUsage   `json:"usage,omitempty"`
}

type chatCompletionChoice struct {
	FinishReason string                `json:"finish_reason"`
	Message      chatCompletionMessage `json:"message"`
}

type chatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (c *TextClient) Generate(ctx context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return domain.TextResponse{}, fmt.Errorf("dashscope text generate: %w: prompt is empty", domain.ErrValidation)
	}
	if req.Model == "" {
		return domain.TextResponse{}, fmt.Errorf("dashscope text generate: %w: model is empty", domain.ErrValidation)
	}

	var out domain.TextResponse
	start := time.Now()
	err := c.cfg.Limiter.Do(ctx, func(callCtx context.Context) error {
		resp, err := c.doGenerate(callCtx, req)
		if err != nil {
			return err
		}
		out = resp
		return nil
	})
	if err != nil {
		return domain.TextResponse{}, err
	}
	out.DurationMs = time.Since(start).Milliseconds()
	return out, nil
}

func (c *TextClient) doGenerate(ctx context.Context, req domain.TextRequest) (domain.TextResponse, error) {
	body := chatCompletionRequest{
		Model: req.Model,
		Messages: []chatCompletionMessage{
			{Role: "user", Content: req.Prompt},
		},
		ResponseFormat: &chatCompletionFormat{Type: "json_object"},
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}
	// Always forward Temperature explicitly: zero is a meaningful caller
	// intent (deterministic mode), not "unset". DashScope/Qwen would
	// otherwise default to a non-zero temperature and contaminate eval
	// determinism — same reasoning as the DeepSeek text client.
	temp := req.Temperature
	body.Temperature = &temp

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return domain.TextResponse{}, fmt.Errorf("dashscope text: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return domain.TextResponse{}, fmt.Errorf("dashscope text: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	if c.cfg.Logger != nil {
		c.cfg.Logger.Info("dashscope text request",
			"model", req.Model,
			"prompt_chars", utf8.RuneCountInString(req.Prompt),
			"max_tokens", req.MaxTokens,
		)
	}
	httpStart := time.Now()
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if c.cfg.Logger != nil {
			c.cfg.Logger.Error("dashscope text http failed",
				"model", req.Model,
				"duration_ms", time.Since(httpStart).Milliseconds(),
				"error", err.Error(),
			)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return domain.TextResponse{}, fmt.Errorf("dashscope text: %w: %v", domain.ErrUpstreamTimeout, err)
		}
		return domain.TextResponse{}, fmt.Errorf("dashscope text: %w: %v", domain.ErrStageFailed, err)
	}
	defer httpResp.Body.Close()
	if c.cfg.Logger != nil {
		c.cfg.Logger.Info("dashscope text response",
			"model", req.Model,
			"duration_ms", time.Since(httpStart).Milliseconds(),
			"status", httpResp.StatusCode,
		)
	}

	if err := checkTextStatus(httpResp); err != nil {
		return domain.TextResponse{}, err
	}

	// Reject obviously-non-JSON content types up front: a misconfigured proxy
	// returning 200 with text/html would otherwise surface as a confusing
	// `invalid character '<'` decode error.
	if ct := httpResp.Header.Get("Content-Type"); ct != "" {
		mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
		if mediaType != "" && mediaType != "application/json" {
			return domain.TextResponse{}, fmt.Errorf(
				"dashscope text: %w: unexpected content-type %q", domain.ErrStageFailed, ct,
			)
		}
	}

	var payload chatCompletionResponse
	if err := json.NewDecoder(io.LimitReader(httpResp.Body, maxResponseBytes)).Decode(&payload); err != nil {
		return domain.TextResponse{}, fmt.Errorf("dashscope text: parse response: %w: %v", domain.ErrValidation, err)
	}
	if len(payload.Choices) == 0 {
		return domain.TextResponse{}, fmt.Errorf("dashscope text: %w: provider returned no choices", domain.ErrStageFailed)
	}
	content := payload.Choices[0].Message.Content
	if content == "" {
		return domain.TextResponse{}, fmt.Errorf("dashscope text: %w: provider returned empty content", domain.ErrStageFailed)
	}

	model := payload.Model
	if model == "" {
		model = req.Model
	}

	var tokensIn, tokensOut int
	if payload.Usage != nil {
		tokensIn = payload.Usage.PromptTokens
		tokensOut = payload.Usage.CompletionTokens
	}

	return domain.TextResponse{
		NormalizedResponse: domain.NormalizedResponse{
			Content:      content,
			Model:        model,
			Provider:     textProvider,
			TokensIn:     tokensIn,
			TokensOut:    tokensOut,
			FinishReason: payload.Choices[0].FinishReason,
		},
	}, nil
}

func checkTextStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	snippet := textErrorSnippet(resp.Body)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("dashscope text: %w: status %d: %s", domain.ErrRateLimited, resp.StatusCode, snippet)
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusGatewayTimeout:
		return fmt.Errorf("dashscope text: %w: status %d: %s", domain.ErrUpstreamTimeout, resp.StatusCode, snippet)
	case resp.StatusCode >= 500:
		return fmt.Errorf("dashscope text: %w: status %d: %s", domain.ErrStageFailed, resp.StatusCode, snippet)
	default:
		return fmt.Errorf("dashscope text: %w: status %d: %s", domain.ErrValidation, resp.StatusCode, snippet)
	}
}

// textErrorSnippet bounds the body bytes echoed back into a wrapped error and
// strips invalid UTF-8 / control characters so a binary or partial-rune body
// (Korean UTF-8 truncated mid-rune, gzipped payloads) cannot corrupt logs.
// Named with a `text` prefix to avoid collision with the TTS client's
// errorSnippet helper in this same package.
func textErrorSnippet(body io.Reader) string {
	if body == nil {
		return ""
	}
	raw, _ := io.ReadAll(io.LimitReader(body, maxErrorBodySnippet))
	if !utf8.Valid(raw) {
		raw = []byte(strings.ToValidUTF8(string(raw), ""))
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			return r
		}
		return -1
	}, string(raw))
}
