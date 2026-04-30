package comfyui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func newHTTPClient() *http.Client { return &http.Client{Timeout: 5 * time.Second} }

func TestSubmitPrompt_HappyPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	var receivedPrompt map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prompt" || r.Method != http.MethodPost {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&receivedPrompt)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"prompt_id":"abc-123","number":1}`))
	}))
	t.Cleanup(srv.Close)

	id, err := submitPrompt(context.Background(), newHTTPClient(), srv.URL, "client-1", []byte(`{"x":1}`))
	if err != nil {
		t.Fatalf("submitPrompt: %v", err)
	}
	if id != "abc-123" {
		t.Fatalf("got id %q", id)
	}
	if receivedPrompt["client_id"] != "client-1" {
		t.Fatalf("client_id missing: %+v", receivedPrompt)
	}
}

func TestSubmitPrompt_5xx_StageFailed(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	t.Cleanup(srv.Close)
	_, err := submitPrompt(context.Background(), newHTTPClient(), srv.URL, "c", []byte(`{}`))
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed, got %v", err)
	}
}

func TestSubmitPrompt_4xx_Validation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
	}))
	t.Cleanup(srv.Close)
	_, err := submitPrompt(context.Background(), newHTTPClient(), srv.URL, "c", []byte(`{}`))
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestSubmitPrompt_429_RateLimited(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	t.Cleanup(srv.Close)
	_, err := submitPrompt(context.Background(), newHTTPClient(), srv.URL, "c", []byte(`{}`))
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestFetchHistory_PendingEmptyBody(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	_, present, err := fetchHistory(context.Background(), newHTTPClient(), srv.URL, "p1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if present {
		t.Fatal("expected present=false")
	}
}

func TestFetchHistory_PendingCompletedFalse(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"p1":{"outputs":{},"status":{"status_str":"running","completed":false}}}`))
	}))
	t.Cleanup(srv.Close)
	_, present, err := fetchHistory(context.Background(), newHTTPClient(), srv.URL, "p1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if present {
		t.Fatal("expected present=false")
	}
}

func TestFetchHistory_CompletedSuccess(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"p1":{"outputs":{"9":{"images":[{"filename":"out.png","subfolder":"","type":"output"}]}},"status":{"status_str":"success","completed":true}}}`))
	}))
	t.Cleanup(srv.Close)
	entry, present, err := fetchHistory(context.Background(), newHTTPClient(), srv.URL, "p1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !present {
		t.Fatal("expected present=true")
	}
	if got := entry.Outputs["9"].Images[0].Filename; got != "out.png" {
		t.Fatalf("filename %q", got)
	}
}

func TestDownloadView_HappyPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	payload := []byte("fake-png-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/view" {
			http.Error(w, "bad", 400)
			return
		}
		if r.URL.Query().Get("filename") != "out.png" {
			http.Error(w, "bad filename", 400)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	got, err := downloadView(context.Background(), newHTTPClient(), srv.URL, historyImage{Filename: "out.png", Type: "output"}, 1<<20)
	if err != nil {
		t.Fatalf("downloadView: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q want %q", got, payload)
	}
}

func TestDownloadView_ExceedsCap(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	payload := strings.Repeat("a", 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)
	_, err := downloadView(context.Background(), newHTTPClient(), srv.URL, historyImage{Filename: "x.png", Type: "output"}, 50)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestUploadImage_MultipartContract(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	var (
		gotField    string
		gotFilename string
		gotBody     []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/upload/image" || r.Method != http.MethodPost {
			http.Error(w, "bad", 400)
			return
		}
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
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
			gotField = p.FormName()
			gotFilename = p.FileName()
			gotBody, _ = io.ReadAll(p)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ref-stored.png","subfolder":"","type":"input"}`))
	}))
	t.Cleanup(srv.Close)

	got, err := uploadImage(context.Background(), newHTTPClient(), srv.URL, "ref-orig.png", "image/png", []byte("PNG-BYTES"))
	if err != nil {
		t.Fatalf("uploadImage: %v", err)
	}
	if got != "ref-stored.png" {
		t.Fatalf("returned name %q", got)
	}
	if gotField != "image" {
		t.Fatalf("multipart field name %q, want \"image\"", gotField)
	}
	if gotFilename != "ref-orig.png" {
		t.Fatalf("filename %q, want ref-orig.png", gotFilename)
	}
	if string(gotBody) != "PNG-BYTES" {
		t.Fatalf("body %q", gotBody)
	}
}

func TestUploadImage_5xx_StageFailed(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	_, err := uploadImage(context.Background(), newHTTPClient(), srv.URL, "x.png", "image/png", []byte("a"))
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Fatalf("expected ErrStageFailed, got %v", err)
	}
}
