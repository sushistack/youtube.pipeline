package dashscope_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/dashscope"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func newTextLimiter(t *testing.T) *llmclient.CallLimiter {
	t.Helper()
	limiter, err := llmclient.NewCallLimiter(llmclient.LimitConfig{
		RequestsPerMinute: 60_000,
		MaxConcurrent:     8,
		AcquireTimeout:    5 * time.Second,
	}, clock.RealClock{})
	if err != nil {
		t.Fatalf("NewCallLimiter: %v", err)
	}
	return limiter
}

func TestTextClient_ConstructorRejectsInvalidInputs(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	if _, err := dashscope.NewTextClient(nil, dashscope.TextClientConfig{
		APIKey:  "key",
		Limiter: newTextLimiter(t),
	}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("nil http client: expected ErrValidation, got %v", err)
	}

	if _, err := dashscope.NewTextClient(&http.Client{}, dashscope.TextClientConfig{
		APIKey:  "",
		Limiter: newTextLimiter(t),
	}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("empty api key: expected ErrValidation, got %v", err)
	}

	if _, err := dashscope.NewTextClient(&http.Client{}, dashscope.TextClientConfig{
		APIKey: "key",
	}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("nil limiter: expected ErrValidation, got %v", err)
	}
}

func TestTextClient_Generate_SuccessNormalizesResponse(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}

		var body struct {
			Model          string `json:"model"`
			MaxTokens      int    `json:"max_tokens"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "qwen-max" {
			t.Fatalf("model = %q", body.Model)
		}
		if body.MaxTokens != 800 {
			t.Fatalf("max_tokens = %d", body.MaxTokens)
		}
		if body.ResponseFormat.Type != "json_object" {
			t.Fatalf("response_format.type = %q", body.ResponseFormat.Type)
		}
		if len(body.Messages) != 1 || body.Messages[0].Role != "user" || body.Messages[0].Content != "return json" {
			t.Fatalf("messages = %+v", body.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "qwen-max",
			"choices": [{
				"finish_reason": "stop",
				"message": {"role":"assistant","content":"{\"verdict\":\"pass\"}"}
			}],
			"usage": {"prompt_tokens": 123, "completion_tokens": 45}
		}`))
	}))
	defer srv.Close()

	client, err := dashscope.NewTextClient(srv.Client(), dashscope.TextClientConfig{
		APIKey:   "test-key",
		Endpoint: srv.URL,
		Limiter:  newTextLimiter(t),
	})
	if err != nil {
		t.Fatalf("NewTextClient: %v", err)
	}

	resp, err := client.Generate(context.Background(), domain.TextRequest{
		Prompt:      "return json",
		Model:       "qwen-max",
		MaxTokens:   800,
		Temperature: 0.2,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if resp.Provider != "dashscope" {
		t.Fatalf("provider = %q", resp.Provider)
	}
	if resp.Model != "qwen-max" {
		t.Fatalf("model = %q", resp.Model)
	}
	if resp.Content != "{\"verdict\":\"pass\"}" {
		t.Fatalf("content = %q", resp.Content)
	}
	if resp.TokensIn != 123 || resp.TokensOut != 45 {
		t.Fatalf("tokens = %d/%d", resp.TokensIn, resp.TokensOut)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("finish_reason = %q", resp.FinishReason)
	}
	if resp.DurationMs < 0 {
		t.Fatalf("duration_ms = %d", resp.DurationMs)
	}
}

func TestTextClient_Generate_ForwardsZeroTemperatureExplicitly(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Determinism contract: temperature=0 must reach the provider verbatim.
	// Mirrors the DeepSeek text client guard — silently dropping the field
	// would let DashScope fall back to its default temperature and break
	// eval reproducibility.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Temperature *float64 `json:"temperature"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Temperature == nil {
			t.Fatalf("temperature missing — must be forwarded explicitly even when zero")
		}
		if *body.Temperature != 0 {
			t.Fatalf("temperature = %v, want 0", *body.Temperature)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{}"}}]
		}`))
	}))
	defer srv.Close()

	client, err := dashscope.NewTextClient(srv.Client(), dashscope.TextClientConfig{
		APIKey:   "test-key",
		Endpoint: srv.URL,
		Limiter:  newTextLimiter(t),
	})
	if err != nil {
		t.Fatalf("NewTextClient: %v", err)
	}

	if _, err := client.Generate(context.Background(), domain.TextRequest{
		Prompt:      "noop",
		Model:       "qwen-max",
		Temperature: 0,
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
}

func TestTextClient_Generate_MapsProviderErrors(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	tests := []struct {
		name   string
		status int
		want   error
	}{
		{name: "rate-limit", status: http.StatusTooManyRequests, want: domain.ErrRateLimited},
		{name: "timeout", status: http.StatusGatewayTimeout, want: domain.ErrUpstreamTimeout},
		{name: "server", status: http.StatusInternalServerError, want: domain.ErrStageFailed},
		{name: "bad-request", status: http.StatusBadRequest, want: domain.ErrValidation},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
			}))
			defer srv.Close()

			client, err := dashscope.NewTextClient(srv.Client(), dashscope.TextClientConfig{
				APIKey:   "test-key",
				Endpoint: srv.URL,
				Limiter:  newTextLimiter(t),
			})
			if err != nil {
				t.Fatalf("NewTextClient: %v", err)
			}

			_, err = client.Generate(context.Background(), domain.TextRequest{
				Prompt: "return json",
				Model:  "qwen-max",
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestTextClient_Generate_RejectsMalformedResponse(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	client, err := dashscope.NewTextClient(srv.Client(), dashscope.TextClientConfig{
		APIKey:   "test-key",
		Endpoint: srv.URL,
		Limiter:  newTextLimiter(t),
	})
	if err != nil {
		t.Fatalf("NewTextClient: %v", err)
	}

	_, err = client.Generate(context.Background(), domain.TextRequest{
		Prompt: "noop",
		Model:  "qwen-max",
	})
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed for empty choices, got %v", err)
	}
}

func TestTextClient_Generate_RejectsNonJSONContentType(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// A misconfigured proxy returning text/html with status 200 would
	// otherwise surface as a confusing JSON parse error; the client must
	// reject the content-type up front so the failure mode is clear.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body>nope</body></html>`))
	}))
	defer srv.Close()

	client, err := dashscope.NewTextClient(srv.Client(), dashscope.TextClientConfig{
		APIKey:   "test-key",
		Endpoint: srv.URL,
		Limiter:  newTextLimiter(t),
	})
	if err != nil {
		t.Fatalf("NewTextClient: %v", err)
	}

	_, err = client.Generate(context.Background(), domain.TextRequest{
		Prompt: "noop",
		Model:  "qwen-max",
	})
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed for text/html response, got %v", err)
	}
}
