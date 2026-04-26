package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/config"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestSettingsHandler_GetAndPutCycle(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	envPath := filepath.Join(filepath.Dir(configPath), ".env")
	manager := config.NewSettingsFileManager(configPath, envPath)
	initial := domain.SettingsFileSnapshot{
		Config: domain.DefaultConfig(),
		Env: map[string]string{
			domain.SettingsSecretDashScope: "dashscope-existing",
			domain.SettingsSecretGemini:    "gemini-existing",
		},
	}
	if err := manager.Write(initial); err != nil {
		t.Fatalf("seed settings files: %v", err)
	}
	store := db.NewSettingsStore(testDB)
	if _, _, err := store.EnsureEffectiveVersion(context.Background(), initial); err != nil {
		t.Fatalf("seed settings state: %v", err)
	}
	handler := NewSettingsHandler(service.NewSettingsService(store, manager, clock.RealClock{}))

	getReq := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	getRes := httptest.NewRecorder()
	handler.Get(getRes, getReq)

	if getRes.Code != http.StatusOK {
		t.Fatalf("GET /api/settings status = %d, want 200", getRes.Code)
	}
	etag := getRes.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("GET did not set ETag header")
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{
	  "config": {
	    "writer_model": "deepseek-chat-v2",
	    "critic_model": "gemini-3.1-flash-lite-preview",
	    "image_model": "qwen-max-vl",
	    "tts_model": "qwen3-tts",
	    "tts_voice": "longhua",
	    "tts_audio_format": "wav",
	    "writer_provider": "deepseek",
	    "critic_provider": "gemini",
	    "image_provider": "dashscope",
	    "tts_provider": "dashscope",
	    "dashscope_region": "cn-beijing",
	    "cost_cap_research": 0.5,
	    "cost_cap_write": 0.5,
	    "cost_cap_image": 2,
	    "cost_cap_tts": 1,
	    "cost_cap_assemble": 0.1,
	    "cost_cap_per_run": 5
	  },
	  "env": {
	    "DEEPSEEK_API_KEY": "deepseek-new"
	  }
	}`))
	putRes := httptest.NewRecorder()
	handler.Put(putRes, putReq)

	if putRes.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings status = %d, want 200, body=%s", putRes.Code, putRes.Body.String())
	}

	files, err := manager.Load()
	if err != nil {
		t.Fatalf("reload settings files: %v", err)
	}
	if files.Config.WriterModel != "deepseek-chat-v2" {
		t.Fatalf("writer_model = %q, want deepseek-chat-v2", files.Config.WriterModel)
	}
	if files.Env[domain.SettingsSecretDeepSeek] != "deepseek-new" {
		t.Fatalf("deepseek env value = %q, want deepseek-new", files.Env[domain.SettingsSecretDeepSeek])
	}
	if files.Env[domain.SettingsSecretGemini] != "gemini-existing" {
		t.Fatalf("gemini env value = %q, want preserved existing secret", files.Env[domain.SettingsSecretGemini])
	}
}

func TestSettingsHandler_PutReturnsFieldDetailsForValidation(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	envPath := filepath.Join(filepath.Dir(configPath), ".env")
	manager := config.NewSettingsFileManager(configPath, envPath)
	initial := domain.SettingsFileSnapshot{Config: domain.DefaultConfig(), Env: map[string]string{}}
	if err := manager.Write(initial); err != nil {
		t.Fatalf("seed settings files: %v", err)
	}
	store := db.NewSettingsStore(testDB)
	if _, _, err := store.EnsureEffectiveVersion(context.Background(), initial); err != nil {
		t.Fatalf("seed settings state: %v", err)
	}
	handler := NewSettingsHandler(service.NewSettingsService(store, manager, clock.RealClock{}))

	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{
	  "config": {
	    "writer_model": "writer",
	    "critic_model": "critic",
	    "image_model": "image",
	    "tts_model": "tts",
	    "tts_voice": "voice",
	    "tts_audio_format": "wav",
	    "writer_provider": "same",
	    "critic_provider": "same",
	    "image_provider": "dashscope",
	    "tts_provider": "dashscope",
	    "dashscope_region": "cn-beijing",
	    "cost_cap_research": 0.5,
	    "cost_cap_write": 0.5,
	    "cost_cap_image": 2,
	    "cost_cap_tts": 1,
	    "cost_cap_assemble": 0.1,
	    "cost_cap_per_run": 5
	  },
	  "env": {}
	}`))
	res := httptest.NewRecorder()
	handler.Put(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.Code)
	}

	var payload struct {
		Error struct {
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Details["config.writer_provider"] == "" {
		t.Fatalf("expected config.writer_provider detail, got %+v", payload.Error.Details)
	}
}

func TestSettingsHandler_PutReturns409WhenIfMatchStale(t *testing.T) {
	testDB := testutil.NewTestDB(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	envPath := filepath.Join(filepath.Dir(configPath), ".env")
	manager := config.NewSettingsFileManager(configPath, envPath)
	initial := domain.SettingsFileSnapshot{Config: domain.DefaultConfig(), Env: map[string]string{}}
	if err := manager.Write(initial); err != nil {
		t.Fatalf("seed: %v", err)
	}
	store := db.NewSettingsStore(testDB)
	if _, _, err := store.EnsureEffectiveVersion(context.Background(), initial); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	handler := NewSettingsHandler(service.NewSettingsService(store, manager, clock.RealClock{}))

	body := `{
	  "config": {
	    "writer_model": "deepseek-chat-v2",
	    "critic_model": "gemini-3.1-flash-lite-preview",
	    "image_model": "qwen-max-vl",
	    "tts_model": "qwen3-tts",
	    "tts_voice": "longhua",
	    "tts_audio_format": "wav",
	    "writer_provider": "deepseek",
	    "critic_provider": "gemini",
	    "image_provider": "dashscope",
	    "tts_provider": "dashscope",
	    "dashscope_region": "cn-beijing",
	    "cost_cap_research": 0.5,
	    "cost_cap_write": 0.5,
	    "cost_cap_image": 2,
	    "cost_cap_tts": 1,
	    "cost_cap_assemble": 0.1,
	    "cost_cap_per_run": 5
	  },
	  "env": {}
	}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
	req.Header.Set("If-Match", `"99"`)
	res := httptest.NewRecorder()
	handler.Put(res, req)

	if res.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", res.Code, res.Body.String())
	}
}
