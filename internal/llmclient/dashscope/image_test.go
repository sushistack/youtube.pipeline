package dashscope_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/dashscope"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// imageStubServer wires a fake DashScope text2image surface:
//
//   - POST /api/v1/services/aigc/text2image/image-synthesis → submitHandler
//   - GET  /api/v1/tasks/{id}                              → taskHandler
//   - GET  /image                                          → bytes from imageBytes
//
// Each handler is a closure so individual tests can vary submit/poll/error
// behavior without rewriting the dispatch shell.
type imageStubServer struct {
	*httptest.Server
	imageURL string
}

type stubHandlers struct {
	submit func(w http.ResponseWriter, r *http.Request)
	task   func(w http.ResponseWriter, r *http.Request)
	image  func(w http.ResponseWriter, r *http.Request)
}

func newImageStubServer(t *testing.T, h stubHandlers) *imageStubServer {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/api/v1/services/aigc/text2image/image-synthesis", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if h.submit != nil {
			h.submit(w, r)
		}
	})
	mux.HandleFunc("/api/v1/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if h.task != nil {
			h.task(w, r)
		}
	})
	mux.HandleFunc("/image", func(w http.ResponseWriter, r *http.Request) {
		if h.image != nil {
			h.image(w, r)
		}
	})
	return &imageStubServer{Server: srv, imageURL: srv.URL + "/image"}
}

// driveFakeClock advances clk in small steps so any Sleep callers wake up.
// We don't know exactly how long the client will sleep next, so loop until
// done is signalled or the bound is hit.
func driveFakeClock(t *testing.T, clk *clock.FakeClock, done <-chan struct{}) {
	t.Helper()
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			if clk.PendingSleepers() > 0 {
				clk.Advance(time.Second)
				continue
			}
			time.Sleep(time.Millisecond)
		}
	}()
}

// callImage runs fn in a goroutine while a fake clock driver advances time,
// so polling backoffs unblock. Returns the result of fn.
func callImage[T any](t *testing.T, clk *clock.FakeClock, fn func() (T, error)) (T, error) {
	t.Helper()
	type result struct {
		v   T
		err error
	}
	ch := make(chan result, 1)
	done := make(chan struct{})
	defer close(done)
	driveFakeClock(t, clk, done)
	go func() {
		v, err := fn()
		ch <- result{v: v, err: err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(5 * time.Second):
		t.Fatal("image client call did not return within 5s")
	}
	var zero T
	return zero, nil
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

// writeSucceededTask serializes a task-status response with a SUCCEEDED
// status pointing at the given image URL.
func writeSucceededTask(w http.ResponseWriter, taskID, imageURL string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"output": map[string]any{
			"task_id":     taskID,
			"task_status": "SUCCEEDED",
			"results":     []map[string]any{{"url": imageURL}},
		},
		"request_id": "req-x",
	})
}

func writeStatusTask(w http.ResponseWriter, taskID, status string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"output": map[string]any{
			"task_id":     taskID,
			"task_status": status,
		},
	})
}

func writeSubmitOK(w http.ResponseWriter, taskID string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"output": map[string]any{
			"task_id":     taskID,
			"task_status": "PENDING",
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
			if got := r.Header.Get("X-DashScope-Async"); got != "enable" {
				t.Errorf("async header = %q", got)
			}
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			writeSubmitOK(w, "task-1")
		},
		task: func(w http.ResponseWriter, r *http.Request) {
			writeSucceededTask(w, "task-1", strings.TrimSuffix(r.Host, "")+"/image") //nolint:staticcheck
		},
		image: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(wantPNG)
		},
	})
	// Patch the task handler to use the actual server URL for image.
	taskHandler := func(w http.ResponseWriter, _ *http.Request) {
		writeSucceededTask(w, "task-1", srv.imageURL)
	}
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/api/v1/tasks/task-1", taskHandler)

	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)

	tmp := t.TempDir()
	out := filepath.Join(tmp, "shot_01.png")

	resp, err := callImage(t, clk, func() (domain.ImageResponse, error) {
		return client.Generate(context.Background(), domain.ImageRequest{
			Prompt:     "a serene lake",
			Model:      "qwen-image",
			Width:      1024,
			Height:     1024,
			OutputPath: out,
		})
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
	if resp.Model != "qwen-image" {
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
	if model, _ := capturedBody["model"].(string); model != "qwen-image" {
		t.Errorf("submitted model = %v", capturedBody["model"])
	}
	params, _ := capturedBody["parameters"].(map[string]any)
	if size, _ := params["size"].(string); size != "1024*1024" {
		t.Errorf("submitted size = %v", params["size"])
	}
	input, _ := capturedBody["input"].(map[string]any)
	if _, hasRefs := input["ref_imgs"]; hasRefs {
		t.Error("Generate should omit ref_imgs")
	}
}

// ----------------------------------------------------------------------------
// Edit passes reference URL
// ----------------------------------------------------------------------------

func TestImageClient_Edit_PassesReferenceURL(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var capturedBody map[string]any
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			writeSubmitOK(w, "task-edit")
		},
		image: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("png"))
		},
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/api/v1/tasks/task-edit", func(w http.ResponseWriter, _ *http.Request) {
		writeSucceededTask(w, "task-edit", srv.imageURL)
	})

	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)

	tmp := t.TempDir()
	out := filepath.Join(tmp, "shot_edit.png")

	_, err := callImage(t, clk, func() (domain.ImageResponse, error) {
		return client.Edit(context.Background(), domain.ImageEditRequest{
			Prompt:            "the same character on a cliff",
			Model:             "qwen-image-edit",
			ReferenceImageURL: "https://example.com/character.png",
			Width:             1024,
			Height:            1024,
			OutputPath:        out,
		})
	})
	if err != nil {
		t.Fatalf("Edit err: %v", err)
	}

	input, _ := capturedBody["input"].(map[string]any)
	refImgs, _ := input["ref_imgs"].([]any)
	if len(refImgs) != 1 || refImgs[0] != "https://example.com/character.png" {
		t.Errorf("ref_imgs = %v, want one entry equal to character.png URL", refImgs)
	}
	if model, _ := capturedBody["model"].(string); model != "qwen-image-edit" {
		t.Errorf("submitted model = %v", capturedBody["model"])
	}
}

// ----------------------------------------------------------------------------
// Polling: PENDING twice, then SUCCEEDED
// ----------------------------------------------------------------------------

func TestImageClient_Generate_PollUntilSucceeded(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var pollCount int32
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, _ *http.Request) {
			writeSubmitOK(w, "task-poll")
		},
		image: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("png"))
		},
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/api/v1/tasks/task-poll", func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&pollCount, 1)
		if n < 3 {
			writeStatusTask(w, "task-poll", "RUNNING")
			return
		}
		writeSucceededTask(w, "task-poll", srv.imageURL)
	})

	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)
	out := filepath.Join(t.TempDir(), "shot.png")

	_, err := callImage(t, clk, func() (domain.ImageResponse, error) {
		return client.Generate(context.Background(), domain.ImageRequest{
			Prompt: "x", Model: "qwen-image", OutputPath: out,
		})
	})
	if err != nil {
		t.Fatalf("Generate err: %v", err)
	}
	if got := atomic.LoadInt32(&pollCount); got < 3 {
		t.Errorf("poll count = %d, want >= 3 (RUNNING twice then SUCCEEDED)", got)
	}
}

// ----------------------------------------------------------------------------
// Task FAILED → ErrValidation
// ----------------------------------------------------------------------------

func TestImageClient_TaskFailedSurfacesError(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, _ *http.Request) {
			writeSubmitOK(w, "task-fail")
		},
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/api/v1/tasks/task-fail", func(w http.ResponseWriter, _ *http.Request) {
		writeStatusTask(w, "task-fail", "FAILED")
	})

	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)
	out := filepath.Join(t.TempDir(), "shot.png")

	_, err := callImage(t, clk, func() (domain.ImageResponse, error) {
		return client.Generate(context.Background(), domain.ImageRequest{
			Prompt: "x", Model: "qwen-image", OutputPath: out,
		})
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("output file should not exist after FAILED task")
	}
}

// ----------------------------------------------------------------------------
// HTTP 5xx on submit → ErrStageFailed (retryable)
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
		Prompt: "x", Model: "qwen-image", OutputPath: filepath.Join(t.TempDir(), "out.png"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Errorf("expected ErrStageFailed, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// HTTP 429 on submit → ErrRateLimited (retryable)
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
		Prompt: "x", Model: "qwen-image", OutputPath: filepath.Join(t.TempDir(), "out.png"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// HTTP 4xx on submit → ErrValidation (terminal)
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
		Prompt: "x", Model: "qwen-image", OutputPath: filepath.Join(t.TempDir(), "out.png"),
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
		submit: func(w http.ResponseWriter, _ *http.Request) {
			writeSubmitOK(w, "task-big")
		},
		image: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(huge)
		},
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/api/v1/tasks/task-big", func(w http.ResponseWriter, _ *http.Request) {
		writeSucceededTask(w, "task-big", srv.imageURL)
	})

	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)
	out := filepath.Join(t.TempDir(), "out.png")

	_, err := callImage(t, clk, func() (domain.ImageResponse, error) {
		return client.Generate(context.Background(), domain.ImageRequest{
			Prompt: "x", Model: "qwen-image", OutputPath: out,
		})
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
// Polling exceeds cap → ErrUpstreamTimeout
// ----------------------------------------------------------------------------

func TestImageClient_PollTimeoutSurfacesUpstreamTimeout(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, _ *http.Request) {
			writeSubmitOK(w, "task-stuck")
		},
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/api/v1/tasks/task-stuck", func(w http.ResponseWriter, _ *http.Request) {
		writeStatusTask(w, "task-stuck", "RUNNING") // never completes
	})

	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)

	_, err := callImage(t, clk, func() (domain.ImageResponse, error) {
		return client.Generate(context.Background(), domain.ImageRequest{
			Prompt: "x", Model: "qwen-image", OutputPath: filepath.Join(t.TempDir(), "out.png"),
		})
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, domain.ErrUpstreamTimeout) {
		t.Errorf("expected ErrUpstreamTimeout, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// Cost is recorded
// ----------------------------------------------------------------------------

func TestImageClient_CostsAccumulate(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := newImageStubServer(t, stubHandlers{
		submit: func(w http.ResponseWriter, _ *http.Request) {
			writeSubmitOK(w, "task-cost")
		},
		image: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("png"))
		},
	})
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/api/v1/tasks/task-cost", func(w http.ResponseWriter, _ *http.Request) {
		writeSucceededTask(w, "task-cost", srv.imageURL)
	})

	clk := clock.NewFakeClock(time.Unix(0, 0))
	client := newClient(t, srv, clk)

	resp, err := callImage(t, clk, func() (domain.ImageResponse, error) {
		return client.Generate(context.Background(), domain.ImageRequest{
			Prompt: "x", Model: "qwen-image", OutputPath: filepath.Join(t.TempDir(), "out.png"),
		})
	})
	if err != nil {
		t.Fatalf("Generate err: %v", err)
	}
	if resp.CostUSD != 0.02 {
		t.Errorf("CostUSD = %v, want 0.02", resp.CostUSD)
	}
}
