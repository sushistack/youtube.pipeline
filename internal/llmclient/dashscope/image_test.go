package dashscope_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/dashscope"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// imageStubServer wires a fake DashScope multimodal-generation surface:
//
//   - POST /api/v1/services/aigc/multimodal-generation/generation → submitHandler
//   - GET  /image                                                 → bytes from imageBytes
type imageStubServer struct {
	*httptest.Server
	imageURL string
}

type stubHandlers struct {
	submit func(w http.ResponseWriter, r *http.Request)
	image  func(w http.ResponseWriter, r *http.Request)
}

func newImageStubServer(t *testing.T, h stubHandlers) *imageStubServer {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/api/v1/services/aigc/multimodal-generation/generation", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if h.submit != nil {
			h.submit(w, r)
		}
	})
	mux.HandleFunc("/image", func(w http.ResponseWriter, r *http.Request) {
		if h.image != nil {
			h.image(w, r)
		}
	})
	return &imageStubServer{Server: srv, imageURL: srv.URL + "/image"}
}

func newClient(t *testing.T, srv *imageStubServer, clk clock.Clock) *dashscope.ImageClient {
	t.Helper()
	c, err := dashscope.NewImageClient(&http.Client{Timeout: 5 * time.Second}, dashscope.ImageClientConfig{
		Endpoint: srv.URL,
		APIKey:   "test-key",
		Clock:    clk,
	})
	if err != nil {
		t.Fatalf("new image client: %v", err)
	}
	return c
}

// writeChoiceURL serializes a multimodal-generation response carrying a
// single choice whose first content part is an image URL.
func writeChoiceURL(w http.ResponseWriter, imageURL string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"output": map[string]any{
			"choices": []map[string]any{
				{
					"finish_reason": "stop",
					"message": map[string]any{
						"role": "assistant",
						"content": []map[string]any{
							{"image": imageURL},
						},
					},
				},
			},
		},
		"request_id": "req-x",
	})
}

// ----------------------------------------------------------------------------
// Constructor guards
// ----------------------------------------------------------------------------

func TestImageClient_RejectsNilHTTPClient(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, err := dashscope.NewImageClient(nil, dashscope.ImageClientConfig{APIKey: "k"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestImageClient_RejectsEmptyAPIKey(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, err := dashscope.NewImageClient(&http.Client{}, dashscope.ImageClientConfig{APIKey: ""})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// Generate happy path
// ----------------------------------------------------------------------------

func TestImageClient_Generate_HappyPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	wantPNG := []byte("\x89PNG\r\n\x1a\nfake-png-bytes")
	var capturedBody map[string]any
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Errorf("auth header = %q", got)
			}
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			// Use the actual server URL so the client can fetch the image.
			writeChoiceURL(w, "http://"+r.Host+"/image")
		},
		image: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(wantPNG)
		},
	})

	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)

	tmp := t.TempDir()
	out := filepath.Join(tmp, "shot_01.png")

	resp, err := client.Generate(context.Background(), domain.ImageRequest{
		Prompt:     "a serene lake",
		Model:      "qwen-image-2.0",
		Width:      1024,
		Height:     1024,
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Generate err: %v", err)
	}

	if resp.ImagePath != out {
		t.Errorf("ImagePath = %q, want %q", resp.ImagePath, out)
	}
	if resp.Provider != "dashscope" {
		t.Errorf("Provider = %q", resp.Provider)
	}
	if resp.Model != "qwen-image-2.0" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.CostUSD <= 0 {
		t.Errorf("CostUSD = %v, want >0", resp.CostUSD)
	}
	if resp.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want >=0", resp.DurationMs)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != string(wantPNG) {
		t.Errorf("output bytes mismatch")
	}

	// Body shape sanity — model + size + n + prompt should be wired through.
	if model, _ := capturedBody["model"].(string); model != "qwen-image-2.0" {
		t.Errorf("submitted model = %v", capturedBody["model"])
	}
	params, _ := capturedBody["parameters"].(map[string]any)
	if size, _ := params["size"].(string); size != "1024*1024" {
		t.Errorf("submitted size = %v", params["size"])
	}
	input, _ := capturedBody["input"].(map[string]any)
	messages, _ := input["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	msg, _ := messages[0].(map[string]any)
	content, _ := msg["content"].([]any)
	// Generate omits the image part — only one text part should appear.
	if len(content) != 1 {
		t.Fatalf("expected 1 content part for text-only generate, got %d", len(content))
	}
	first, _ := content[0].(map[string]any)
	if _, hasImage := first["image"]; hasImage {
		t.Error("Generate should omit the image content part")
	}
	if text, _ := first["text"].(string); text != "a serene lake" {
		t.Errorf("text content = %v, want prompt", first["text"])
	}
}

// ----------------------------------------------------------------------------
// Edit passes reference URL through messages.content
// ----------------------------------------------------------------------------

func TestImageClient_Edit_PassesReferenceURL(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var capturedBody map[string]any
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			writeChoiceURL(w, "http://"+r.Host+"/image")
		},
		image: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("png"))
		},
	})

	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)

	tmp := t.TempDir()
	out := filepath.Join(tmp, "shot_edit.png")

	_, err := client.Edit(context.Background(), domain.ImageEditRequest{
		Prompt:            "the same character on a cliff",
		Model:             "qwen-image-edit",
		ReferenceImageURL: "data:image/jpeg;base64,AAA",
		Width:             1024,
		Height:            1024,
		OutputPath:        out,
	})
	if err != nil {
		t.Fatalf("Edit err: %v", err)
	}

	input, _ := capturedBody["input"].(map[string]any)
	messages, _ := input["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	msg, _ := messages[0].(map[string]any)
	content, _ := msg["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("Edit should send image + text content, got %d parts", len(content))
	}
	imagePart, _ := content[0].(map[string]any)
	if got, _ := imagePart["image"].(string); got != "data:image/jpeg;base64,AAA" {
		t.Errorf("image part = %v, want data URL", imagePart["image"])
	}
	textPart, _ := content[1].(map[string]any)
	if got, _ := textPart["text"].(string); got != "the same character on a cliff" {
		t.Errorf("text part = %v", textPart["text"])
	}
	if model, _ := capturedBody["model"].(string); model != "qwen-image-edit" {
		t.Errorf("submitted model = %v", capturedBody["model"])
	}
}

// ----------------------------------------------------------------------------
// HTTP 5xx → ErrStageFailed (retryable)
// ----------------------------------------------------------------------------

func TestImageClient_HTTP5xxIsRetryable(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
	})
	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)

	_, err := client.Generate(context.Background(), domain.ImageRequest{
		Prompt: "x", Model: "qwen-image-2.0", OutputPath: filepath.Join(t.TempDir(), "out.png"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Errorf("expected ErrStageFailed, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// HTTP 429 → ErrRateLimited (retryable)
// ----------------------------------------------------------------------------

func TestImageClient_HTTP429IsRateLimited(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "slow down", http.StatusTooManyRequests)
		},
	})
	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)

	_, err := client.Generate(context.Background(), domain.ImageRequest{
		Prompt: "x", Model: "qwen-image-2.0", OutputPath: filepath.Join(t.TempDir(), "out.png"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// HTTP 4xx → ErrValidation (terminal)
// ----------------------------------------------------------------------------

func TestImageClient_HTTP4xxIsTerminal(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusBadRequest)
		},
	})
	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)

	_, err := client.Generate(context.Background(), domain.ImageRequest{
		Prompt: "x", Model: "qwen-image-2.0", OutputPath: filepath.Join(t.TempDir(), "out.png"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// Response with error code → ErrValidation
// ----------------------------------------------------------------------------

func TestImageClient_PayloadErrorSurfacesValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":       "InvalidParameter",
				"message":    "url error",
				"request_id": "req-fail",
			})
		},
	})
	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)

	_, err := client.Generate(context.Background(), domain.ImageRequest{
		Prompt: "x", Model: "qwen-image-2.0", OutputPath: filepath.Join(t.TempDir(), "out.png"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// Download cap enforced
// ----------------------------------------------------------------------------

func TestImageClient_DownloadCapEnforced(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	huge := make([]byte, (50<<20)+10) // 50 MiB + 10 bytes — over cap
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, r *http.Request) {
			writeChoiceURL(w, "http://"+r.Host+"/image")
		},
		image: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(huge)
		},
	})

	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)
	out := filepath.Join(t.TempDir(), "out.png")

	_, err := client.Generate(context.Background(), domain.ImageRequest{
		Prompt: "x", Model: "qwen-image-2.0", OutputPath: out,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("output file should not exist after over-cap download")
	}
}

// ----------------------------------------------------------------------------
// Cost is recorded
// ----------------------------------------------------------------------------

func TestImageClient_CostsAccumulate(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, r *http.Request) {
			writeChoiceURL(w, "http://"+r.Host+"/image")
		},
		image: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("png"))
		},
	})

	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)

	resp, err := client.Generate(context.Background(), domain.ImageRequest{
		Prompt: "x", Model: "qwen-image-2.0", OutputPath: filepath.Join(t.TempDir(), "out.png"),
	})
	if err != nil {
		t.Fatalf("Generate err: %v", err)
	}
	if resp.CostUSD != 0.02 {
		t.Errorf("CostUSD = %v, want 0.02", resp.CostUSD)
	}
}
