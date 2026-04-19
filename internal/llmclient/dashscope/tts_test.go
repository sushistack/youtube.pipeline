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

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/llmclient/dashscope"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestTTSClient_ConstructorRejectsNilHTTPClient(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	_, err := dashscope.NewTTSClient(nil, dashscope.TTSClientConfig{APIKey: "key"})
	if err == nil {
		t.Fatal("expected error for nil http client, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestTTSClient_ConstructorRejectsEmptyAPIKey(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	_, err := dashscope.NewTTSClient(&http.Client{}, dashscope.TTSClientConfig{APIKey: ""})
	if err == nil {
		t.Fatal("expected error for empty api key, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestTTSClient_Synthesize_SuccessWritesAudioAndReturnsDuration(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	wantAudio := []byte("fake-wav-audio-bytes")
	wantModel := "fake-tts-model"
	wantVoice := "longhua"
	wantText := "안녕하세요"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header, got %q", r.Header.Get("Authorization"))
		}
		var body struct {
			Model string `json:"model"`
			Input struct {
				Text  string `json:"text"`
				Voice string `json:"voice"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body.Model != wantModel {
			t.Errorf("model = %q, want %q", body.Model, wantModel)
		}
		if body.Input.Text != wantText {
			t.Errorf("text = %q, want %q", body.Input.Text, wantText)
		}
		if body.Input.Voice != wantVoice {
			t.Errorf("voice = %q, want %q", body.Input.Voice, wantVoice)
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(wantAudio)
	}))
	defer srv.Close()

	outputPath := filepath.Join(t.TempDir(), "scene_01.wav")
	client, err := dashscope.NewTTSClient(srv.Client(), dashscope.TTSClientConfig{
		Endpoint: srv.URL,
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("NewTTSClient: %v", err)
	}

	resp, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       wantText,
		Model:      wantModel,
		Voice:      wantVoice,
		OutputPath: outputPath,
		Format:     "wav",
	})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	testutil.AssertEqual(t, resp.Model, wantModel)
	testutil.AssertEqual(t, resp.Provider, "dashscope")
	testutil.AssertEqual(t, resp.AudioPath, outputPath)
	if resp.DurationMs < 0 {
		t.Errorf("DurationMs = %d, expected >= 0", resp.DurationMs)
	}

	gotAudio, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read audio file: %v", err)
	}
	if string(gotAudio) != string(wantAudio) {
		t.Errorf("audio bytes mismatch: got %q, want %q", gotAudio, wantAudio)
	}
}

func TestTTSClient_Synthesize_MapsRateLimitTo_ErrRateLimited(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"RateLimitExceeded"}`))
	}))
	defer srv.Close()

	client, _ := dashscope.NewTTSClient(srv.Client(), dashscope.TTSClientConfig{
		Endpoint: srv.URL,
		APIKey:   "test-key",
	})

	_, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       "test",
		Model:      "fake-tts",
		OutputPath: filepath.Join(t.TempDir(), "out.wav"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestTTSClient_Synthesize_MapsServerErrorToRetryable(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"InternalError"}`))
	}))
	defer srv.Close()

	client, _ := dashscope.NewTTSClient(srv.Client(), dashscope.TTSClientConfig{
		Endpoint: srv.URL,
		APIKey:   "test-key",
	})

	_, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       "test",
		Model:      "fake-tts",
		OutputPath: filepath.Join(t.TempDir(), "out.wav"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 5xx maps to ErrStageFailed (retryable)
	if !errors.Is(err, domain.ErrStageFailed) {
		t.Errorf("expected ErrStageFailed for 5xx, got %v", err)
	}
}

func TestTTSClient_Synthesize_MapsClientErrorTo_ErrValidation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"InvalidParameter"}`))
	}))
	defer srv.Close()

	client, _ := dashscope.NewTTSClient(srv.Client(), dashscope.TTSClientConfig{
		Endpoint: srv.URL,
		APIKey:   "test-key",
	})

	_, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       "test",
		Model:      "fake-tts",
		OutputPath: filepath.Join(t.TempDir(), "out.wav"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation for 4xx, got %v", err)
	}
}

func TestTTSClient_Synthesize_EmptyBodySurfacesError(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write no body — client must reject this
	}))
	defer srv.Close()

	client, _ := dashscope.NewTTSClient(srv.Client(), dashscope.TTSClientConfig{
		Endpoint: srv.URL,
		APIKey:   "test-key",
	})

	_, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       "test",
		Model:      "fake-tts",
		OutputPath: filepath.Join(t.TempDir(), "out.wav"),
	})
	if err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
}

func TestTTSClient_Synthesize_RejectsEmptyText(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	client, _ := dashscope.NewTTSClient(&http.Client{}, dashscope.TTSClientConfig{APIKey: "key"})

	_, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       "",
		Model:      "fake-tts",
		OutputPath: filepath.Join(t.TempDir(), "out.wav"),
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation for empty text, got %v", err)
	}
}

func TestTTSClient_Synthesize_RejectsEmptyOutputPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	client, _ := dashscope.NewTTSClient(&http.Client{}, dashscope.TTSClientConfig{APIKey: "key"})

	_, err := client.Synthesize(context.Background(), domain.TTSRequest{
		Text:       "hello",
		Model:      "fake-tts",
		OutputPath: "",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation for empty output path, got %v", err)
	}
}
