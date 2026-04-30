package comfyui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// fakeBackend is a configurable httptest.Server that mimics the four ComfyUI
// endpoints. Each handler is replaceable so individual matrix rows can wire
// custom failure shapes.
type fakeBackend struct {
	server          *httptest.Server
	historyResponses []string
	historyIdx       atomic.Int32
	viewBytes        []byte
	viewStatus       int
	submitStatus     int
	uploadStored     string
	lastUploadName   string
	lastUploadBody   []byte
	mu               sync.Mutex
}

func newFakeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	fb := &fakeBackend{
		viewBytes:    []byte("PNG-FAKE"),
		viewStatus:   200,
		submitStatus: 200,
		uploadStored: "ref-server.png",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/prompt", fb.handleSubmit)
	mux.HandleFunc("/history/", fb.handleHistory)
	mux.HandleFunc("/view", fb.handleView)
	mux.HandleFunc("/upload/image", fb.handleUpload)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	fb.server = srv
	return fb
}

func (fb *fakeBackend) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if fb.submitStatus != 200 {
		w.WriteHeader(fb.submitStatus)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"prompt_id":"prompt-x","number":1}`))
}

func (fb *fakeBackend) handleHistory(w http.ResponseWriter, r *http.Request) {
	idx := fb.historyIdx.Load()
	var body string
	if int(idx) < len(fb.historyResponses) {
		body = fb.historyResponses[idx]
	} else if len(fb.historyResponses) > 0 {
		body = fb.historyResponses[len(fb.historyResponses)-1]
	} else {
		body = `{}`
	}
	fb.historyIdx.Add(1)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func (fb *fakeBackend) handleView(w http.ResponseWriter, r *http.Request) {
	if fb.viewStatus != 200 {
		w.WriteHeader(fb.viewStatus)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(fb.viewBytes)
}

func (fb *fakeBackend) handleUpload(w http.ResponseWriter, r *http.Request) {
	_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	mr := multipart.NewReader(r.Body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		fb.mu.Lock()
		fb.lastUploadName = p.FileName()
		fb.lastUploadBody, _ = io.ReadAll(p)
		fb.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"name": fb.uploadStored, "type": "input"})
}

func newTestClient(t *testing.T, fb *fakeBackend, clk clock.Clock) *ImageClient {
	t.Helper()
	c, err := NewImageClient(&http.Client{Timeout: 5 * time.Second}, ImageClientConfig{
		Endpoint: fb.server.URL,
		Clock:    clk,
	})
	if err != nil {
		t.Fatalf("NewImageClient: %v", err)
	}
	return c
}

func successHistoryBody() string {
	return `{"prompt-x":{"outputs":{"9":{"images":[{"filename":"out.png","subfolder":"","type":"output"}]}},"status":{"status_str":"success","completed":true}}}`
}

// ---------------------------------------------------------------------------
// Constructor guards
// ---------------------------------------------------------------------------

func TestNewImageClient_RejectsNilHTTPClient(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, err := NewImageClient(nil, ImageClientConfig{Endpoint: "http://x"})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestNewImageClient_RejectsEmptyEndpoint(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, err := NewImageClient(&http.Client{}, ImageClientConfig{Endpoint: ""})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestNewImageClient_RejectsMalformedEndpoint(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	cases := []string{
		"127.0.0.1:8188",                  // missing scheme
		"http://127.0.0.1:8188/api",       // path present
		"http://127.0.0.1:8188?foo=bar",   // query present
		"://x",                            // unparseable
	}
	for _, ep := range cases {
		_, err := NewImageClient(&http.Client{}, ImageClientConfig{Endpoint: ep})
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("endpoint %q: expected ErrValidation, got %v", ep, err)
		}
	}
}

// ---------------------------------------------------------------------------
// I/O matrix coverage (12 rows)
// ---------------------------------------------------------------------------

// Row 1: Generate happy path.
func TestImageClient_Generate_HappyPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	fb := newFakeBackend(t)
	fb.historyResponses = []string{successHistoryBody()}
	clk := clock.RealClock{}
	c := newTestClient(t, fb, clk)

	out := filepath.Join(t.TempDir(), "shot.png")
	resp, err := c.Generate(context.Background(), domain.ImageRequest{
		Prompt: "a forest", Model: "flux2-klein-4b-fp8", Width: 2688, Height: 1536, OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Provider != "comfyui" {
		t.Fatalf("provider %q", resp.Provider)
	}
	if resp.CostUSD != 0 {
		t.Fatalf("cost %v, want 0", resp.CostUSD)
	}
	if resp.DurationMs < 0 {
		t.Fatalf("DurationMs=%d must be non-negative", resp.DurationMs)
	}
	if data, err := os.ReadFile(out); err != nil || string(data) != "PNG-FAKE" {
		t.Fatalf("output file: data=%q err=%v", data, err)
	}
}

// Row 2: Edit happy path — base64 decode → upload → inject filename.
func TestImageClient_Edit_HappyPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	fb := newFakeBackend(t)
	fb.historyResponses = []string{successHistoryBody()}
	c := newTestClient(t, fb, clock.RealClock{})

	rawPNG := []byte("PNG-REF-BYTES")
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(rawPNG)

	out := filepath.Join(t.TempDir(), "edit.png")
	resp, err := c.Edit(context.Background(), domain.ImageEditRequest{
		Prompt: "a portrait", Model: "flux2-klein-4b-fp8-edit",
		ReferenceImageURL: dataURL,
		Width:             2688, Height: 1536, OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if resp.Provider != "comfyui" || resp.CostUSD != 0 {
		t.Fatalf("response %+v", resp)
	}

	fb.mu.Lock()
	defer fb.mu.Unlock()
	if string(fb.lastUploadBody) != string(rawPNG) {
		t.Fatalf("upload body %q", fb.lastUploadBody)
	}
	if !strings.HasPrefix(fb.lastUploadName, "ref-") || !strings.HasSuffix(fb.lastUploadName, ".png") {
		t.Fatalf("upload filename %q", fb.lastUploadName)
	}
}

// Row 3: Polling PENDING → COMPLETED. First poll empty body, second has outputs.
func TestImageClient_Generate_PollingPendingThenComplete(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	fb := newFakeBackend(t)
	fb.historyResponses = []string{`{}`, successHistoryBody()}
	clk := clock.NewFakeClock(time.Unix(0, 0))
	c := newTestClient(t, fb, clk)

	out := filepath.Join(t.TempDir(), "shot.png")
	done := make(chan error, 1)
	go func() {
		_, err := c.Generate(context.Background(), domain.ImageRequest{
			Prompt: "x", Model: "m", Width: 100, Height: 100, OutputPath: out,
		})
		done <- err
	}()

	// Drive the fake clock past the 250ms poll cadence at least once.
	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("Generate did not complete within real-clock budget")
		}
		runtime.Gosched()
		if clk.PendingSleepers() > 0 {
			clk.Advance(pollInterval + 10*time.Millisecond)
		}
	}
}

// Row 4: Polling timeout → ErrUpstreamTimeout.
func TestImageClient_Generate_PollingTimeout(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	fb := newFakeBackend(t)
	// Always return empty — never completes.
	fb.historyResponses = []string{`{}`}
	clk := clock.NewFakeClock(time.Unix(0, 0))
	c := newTestClient(t, fb, clk)

	out := filepath.Join(t.TempDir(), "shot.png")
	done := make(chan error, 1)
	go func() {
		_, err := c.Generate(context.Background(), domain.ImageRequest{
			Prompt: "x", Model: "m", Width: 100, Height: 100, OutputPath: out,
		})
		done <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-done:
			if !errors.Is(err, domain.ErrUpstreamTimeout) {
				t.Fatalf("expected ErrUpstreamTimeout, got %v", err)
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("Generate did not return within real-clock budget")
		}
		runtime.Gosched()
		if clk.PendingSleepers() > 0 {
			clk.Advance(pollMaxDuration + pollInterval)
		}
	}
}

// Row 5: Workflow execution error in history status.
func TestImageClient_Generate_HistoryErrorStatus(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	fb := newFakeBackend(t)
	fb.historyResponses = []string{
		`{"prompt-x":{"outputs":{},"status":{"status_str":"error","completed":true,"messages":[["execution_error","missing model"]]}}}`,
	}
	c := newTestClient(t, fb, clock.RealClock{})

	out := filepath.Join(t.TempDir(), "shot.png")
	_, err := c.Generate(context.Background(), domain.ImageRequest{
		Prompt: "x", Model: "m", Width: 100, Height: 100, OutputPath: out,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "prompt-x") {
		t.Fatalf("error must include prompt_id, got %v", err)
	}
}

// Row 6: HTTP 5xx on submit → ErrStageFailed.
func TestImageClient_Generate_5xxOnSubmit(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	fb := newFakeBackend(t)
	fb.submitStatus = 503
	c := newTestClient(t, fb, clock.RealClock{})

	out := filepath.Join(t.TempDir(), "shot.png")
	_, err := c.Generate(context.Background(), domain.ImageRequest{
		Prompt: "x", Model: "m", Width: 100, Height: 100, OutputPath: out,
	})
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed, got %v", err)
	}
}

// Row 7: HTTP 4xx (non-429) on submit → ErrValidation.
func TestImageClient_Generate_4xxOnSubmit(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	fb := newFakeBackend(t)
	fb.submitStatus = 400
	c := newTestClient(t, fb, clock.RealClock{})

	out := filepath.Join(t.TempDir(), "shot.png")
	_, err := c.Generate(context.Background(), domain.ImageRequest{
		Prompt: "x", Model: "m", Width: 100, Height: 100, OutputPath: out,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// Row 8: HTTP 429 → ErrRateLimited.
func TestImageClient_Generate_429OnSubmit(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	fb := newFakeBackend(t)
	fb.submitStatus = 429
	c := newTestClient(t, fb, clock.RealClock{})

	out := filepath.Join(t.TempDir(), "shot.png")
	_, err := c.Generate(context.Background(), domain.ImageRequest{
		Prompt: "x", Model: "m", Width: 100, Height: 100, OutputPath: out,
	})
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

// Row 9: Reference base64 decode failure.
func TestImageClient_Edit_BadDataURL(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	fb := newFakeBackend(t)
	c := newTestClient(t, fb, clock.RealClock{})

	out := filepath.Join(t.TempDir(), "edit.png")
	_, err := c.Edit(context.Background(), domain.ImageEditRequest{
		Prompt: "p", Model: "m",
		ReferenceImageURL: "https://example.com/not-a-data-url.png",
		Width:             1, Height: 1, OutputPath: out,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// Row 10: result image > 50 MiB → ErrValidation; tmp file cleanup.
func TestImageClient_Generate_OutputExceedsCap(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	fb := newFakeBackend(t)
	fb.historyResponses = []string{successHistoryBody()}
	// Build a payload one byte past the cap.
	huge := make([]byte, imageDownloadLimit+1)
	for i := range huge {
		huge[i] = 'x'
	}
	fb.viewBytes = huge
	c := newTestClient(t, fb, clock.RealClock{})

	dir := t.TempDir()
	out := filepath.Join(dir, "shot.png")
	_, err := c.Generate(context.Background(), domain.ImageRequest{
		Prompt: "x", Model: "m", Width: 100, Height: 100, OutputPath: out,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output file must not exist after cap rejection, stat err=%v", err)
	}
	// No leftover temp files in the output dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".comfyui-img-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

// Row 11: workflow JSON label missing → ErrValidation at construction time.
func TestImageClient_Construction_RejectsMutatedT2IWorkflow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// validateWorkflow already covers per-byte mutation — this row asserts
	// NewImageClient surfaces the same error class with workflow context.
	// Since we cannot replace WorkflowT2I without mutating package state,
	// invoke validateWorkflow directly and verify the wrap is consistent.
	mutated := mutateClassType(t, WorkflowT2I, "POSITIVE_PROMPT", "BogusType")
	if err := validateWorkflow(mutated, false); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("validateWorkflow on mutated json: %v", err)
	}
}

// Row 12: constructor guards (nil http client + empty endpoint covered by
// dedicated tests above; malformed endpoint by RejectsMalformedEndpoint).
