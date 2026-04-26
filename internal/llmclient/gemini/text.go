package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient"
)

const (
	// defaultChatEndpoint is the Gemini OpenAI-compatible chat completions URL.
	defaultChatEndpoint = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"

	textProvider = "gemini"

	maxResponseBytes    = 1 << 20
	maxErrorBodySnippet = 256
)

type TextClientConfig struct {
	APIKey   string
	Endpoint string
	Limiter  *llmclient.CallLimiter
}

// TextClient implements domain.TextGenerator for Google Gemini via its
// OpenAI-compatible chat-completions endpoint. The request/response shape
// is identical to OpenAI; the only difference is the endpoint URL.
type TextClient struct {
	httpClient *http.Client
	cfg        TextClientConfig
}

func NewTextClient(httpClient *http.Client, cfg TextClientConfig) (*TextClient, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("gemini text: %w: http client is nil", domain.ErrValidation)
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("gemini text: %w: api key is empty", domain.ErrValidation)
	}
	if cfg.Limiter == nil {
		return nil, fmt.Errorf("gemini text: %w: limiter is nil", domain.ErrValidation)
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
		return domain.TextResponse{}, fmt.Errorf("gemini text generate: %w: prompt is empty", domain.ErrValidation)
	}
	if req.Model == "" {
		return domain.TextResponse{}, fmt.Errorf("gemini text generate: %w: model is empty", domain.ErrValidation)
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
	temp := req.Temperature
	body.Temperature = &temp

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return domain.TextResponse{}, fmt.Errorf("gemini text: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return domain.TextResponse{}, fmt.Errorf("gemini text: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return domain.TextResponse{}, fmt.Errorf("gemini text: %w: %v", domain.ErrUpstreamTimeout, err)
		}
		return domain.TextResponse{}, fmt.Errorf("gemini text: %w: %v", domain.ErrStageFailed, err)
	}
	defer httpResp.Body.Close()

	if err := checkStatus(httpResp); err != nil {
		return domain.TextResponse{}, err
	}

	if ct := httpResp.Header.Get("Content-Type"); ct != "" {
		mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
		if mediaType != "" && mediaType != "application/json" {
			return domain.TextResponse{}, fmt.Errorf(
				"gemini text: %w: unexpected content-type %q", domain.ErrStageFailed, ct,
			)
		}
	}

	var payload chatCompletionResponse
	if err := json.NewDecoder(io.LimitReader(httpResp.Body, maxResponseBytes)).Decode(&payload); err != nil {
		return domain.TextResponse{}, fmt.Errorf("gemini text: parse response: %w: %v", domain.ErrValidation, err)
	}
	if len(payload.Choices) == 0 {
		return domain.TextResponse{}, fmt.Errorf("gemini text: %w: provider returned no choices", domain.ErrStageFailed)
	}
	content := payload.Choices[0].Message.Content
	if content == "" {
		return domain.TextResponse{}, fmt.Errorf("gemini text: %w: provider returned empty content", domain.ErrStageFailed)
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

func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	snippet := errorSnippet(resp.Body)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("gemini text: %w: status %d: %s", domain.ErrRateLimited, resp.StatusCode, snippet)
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusGatewayTimeout:
		return fmt.Errorf("gemini text: %w: status %d: %s", domain.ErrUpstreamTimeout, resp.StatusCode, snippet)
	case resp.StatusCode >= 500:
		return fmt.Errorf("gemini text: %w: status %d: %s", domain.ErrStageFailed, resp.StatusCode, snippet)
	default:
		return fmt.Errorf("gemini text: %w: status %d: %s", domain.ErrValidation, resp.StatusCode, snippet)
	}
}

func errorSnippet(body io.Reader) string {
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
