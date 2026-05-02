package eval

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
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type fakeRuntimeLoader struct {
	files domain.SettingsFileSnapshot
}

func (f fakeRuntimeLoader) LoadEffectiveRuntimeFiles(context.Context) (domain.SettingsFileSnapshot, error) {
	return f.files, nil
}

func TestRuntimeEvaluator_Evaluate_UsesDeepSeekRuntime(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	sampleReport := string(testutil.LoadFixture(t, "contracts/critic_post_writer.sample.json"))
	encodedContent, err := json.Marshal(sampleReport)
	if err != nil {
		t.Fatalf("marshal sample content: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"deepseek-v4-flash",
			"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":` + string(encodedContent) + `}}],
			"usage":{"prompt_tokens":100,"completion_tokens":50}
		}`))
	}))
	defer srv.Close()

	limiter, err := llmclient.NewCallLimiter(llmclient.LimitConfig{
		RequestsPerMinute: 60_000,
		MaxConcurrent:     4,
		AcquireTimeout:    5 * time.Second,
	}, clock.RealClock{})
	if err != nil {
		t.Fatalf("NewCallLimiter: %v", err)
	}

	evaluator, err := NewRuntimeEvaluator(RuntimeEvaluatorOptions{
		ProjectRoot: testutil.ProjectRoot(t),
		Runtime: fakeRuntimeLoader{files: domain.SettingsFileSnapshot{
			Config: domain.PipelineConfig{
				CriticProvider: "deepseek",
				CriticModel:    "deepseek-v4-flash",
			},
			Env: map[string]string{
				domain.SettingsSecretDeepSeek: "test-key",
			},
		}},
		HTTPClient: srv.Client(),
		Limiter:    limiter,
		Endpoint:   srv.URL,
	})
	if err != nil {
		t.Fatalf("NewRuntimeEvaluator: %v", err)
	}

	fixture := Fixture{
		FixtureID: "scp-049-run-1",
		Input:     testutil.LoadFixture(t, "contracts/writer_output.sample.json"),
	}
	verdict, err := evaluator.Evaluate(context.Background(), fixture)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if verdict.Provider != "deepseek" {
		t.Fatalf("provider = %q", verdict.Provider)
	}
	if verdict.Model != "deepseek-v4-flash" {
		t.Fatalf("model = %q", verdict.Model)
	}
	if verdict.Verdict != domain.CriticVerdictAcceptWithNotes {
		t.Fatalf("verdict = %q", verdict.Verdict)
	}
	if verdict.OverallScore != 81 {
		t.Fatalf("overall_score = %d", verdict.OverallScore)
	}
}

func TestRuntimeEvaluator_RejectsNonDeepSeekProvider(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	limiter, err := llmclient.NewCallLimiter(llmclient.LimitConfig{
		RequestsPerMinute: 60_000,
		MaxConcurrent:     4,
		AcquireTimeout:    5 * time.Second,
	}, clock.RealClock{})
	if err != nil {
		t.Fatalf("NewCallLimiter: %v", err)
	}

	evaluator, err := NewRuntimeEvaluator(RuntimeEvaluatorOptions{
		ProjectRoot: testutil.ProjectRoot(t),
		Runtime: fakeRuntimeLoader{files: domain.SettingsFileSnapshot{
			Config: domain.PipelineConfig{
				CriticProvider: "gemini",
				CriticModel:    "gemini-3.1-flash-lite-preview",
			},
		}},
		HTTPClient: &http.Client{},
		Limiter:    limiter,
	})
	if err != nil {
		t.Fatalf("NewRuntimeEvaluator: %v", err)
	}

	_, err = evaluator.Evaluate(context.Background(), Fixture{
		FixtureID: "scp-049-run-1",
		Input:     testutil.LoadFixture(t, "contracts/writer_output.sample.json"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

