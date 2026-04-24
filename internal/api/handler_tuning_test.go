package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
)

// deterministicPassEvaluator grades every fixture "pass" with a constant
// score. Used to verify Fast Feedback aggregation and ordering without
// relying on a real LLM.
type deterministicPassEvaluator struct {
	score int
}

func (d deterministicPassEvaluator) Evaluate(_ context.Context, _ eval.Fixture) (eval.VerdictResult, error) {
	return eval.VerdictResult{Verdict: domain.CriticVerdictPass, OverallScore: d.score}, nil
}

// newTuningTestService builds a TuningService rooted at a throwaway project
// tree populated with a minimal prompt file and empty manifest. Returns the
// service plus its project root so tests can seed additional files.
func newTuningTestService(t *testing.T, evaluator eval.Evaluator) (*service.TuningService, string) {
	t.Helper()
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "docs", "prompts", "scenario"), 0o755); err != nil {
		t.Fatalf("mkdir prompts dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "docs", "prompts", "scenario", "critic_agent.md"),
		[]byte("# Critic Prompt\n\nbaseline content.\n"),
		0o644,
	); err != nil {
		t.Fatalf("seed critic prompt: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "testdata", "golden", "eval"), 0o755); err != nil {
		t.Fatalf("mkdir golden dir: %v", err)
	}
	manifest := []byte(`{
  "version": 1,
  "next_index": 1,
  "last_refreshed_at": "2026-04-24T00:00:00Z",
  "pairs": []
}
`)
	if err := os.WriteFile(filepath.Join(root, "testdata", "golden", "eval", "manifest.json"), manifest, 0o644); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	svc := service.NewTuningService(service.TuningServiceOptions{
		ProjectRoot: root,
		Evaluator:   evaluator,
		Calibration: stubCalibration{},
		Clock:       clock.RealClock{},
	})
	return svc, root
}

type stubCalibration struct {
	points []domain.CriticCalibrationTrendPoint
	err    error
}

func (s stubCalibration) RecentCriticCalibrationTrend(_ context.Context, _, _ int) ([]domain.CriticCalibrationTrendPoint, error) {
	return s.points, s.err
}

func TestTuningHandler_GetPrompt_ReturnsBodyAndMetadata(t *testing.T) {
	svc, _ := newTuningTestService(t, nil)
	handler := NewTuningHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/tuning/critic-prompt", nil)
	res := httptest.NewRecorder()
	handler.GetPrompt(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	var env struct {
		Version int                             `json:"version"`
		Data    service.CriticPromptEnvelope    `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Body == "" || !strings.Contains(env.Data.Body, "baseline content") {
		t.Errorf("body not returned verbatim, got %q", env.Data.Body)
	}
	if env.Data.PromptHash == "" {
		t.Errorf("prompt_hash empty")
	}
	// git sha is either a real short SHA or the "nogit" sentinel; never empty.
	if env.Data.GitShortSHA == "" {
		t.Errorf("git_short_sha empty")
	}
}

func TestTuningHandler_PutPrompt_PersistsAndReturnsVersionTag(t *testing.T) {
	svc, root := newTuningTestService(t, nil)
	handler := NewTuningHandler(svc)

	body := "# updated critic\n\nnew rubric rules."
	req := httptest.NewRequest(http.MethodPut, "/api/tuning/critic-prompt",
		strings.NewReader(`{"body":`+mustJSONString(body)+`}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.PutPrompt(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	var env struct {
		Version int                          `json:"version"`
		Data    service.CriticPromptEnvelope `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.VersionTag == "" {
		t.Errorf("version_tag empty")
	}
	// trailing newline must be appended.
	saved, err := os.ReadFile(filepath.Join(root, "docs", "prompts", "scenario", "critic_agent.md"))
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if !bytes.HasSuffix(saved, []byte("\n")) {
		t.Errorf("saved file missing trailing newline: %q", saved)
	}
	if !strings.Contains(string(saved), "new rubric rules.") {
		t.Errorf("saved file missing new content: %q", saved)
	}
	if svc.ActivePromptVersion() == nil {
		t.Errorf("active prompt version not published after save")
	}
}

func TestTuningHandler_PutPrompt_RejectsEmptyBody(t *testing.T) {
	svc, _ := newTuningTestService(t, nil)
	handler := NewTuningHandler(svc)

	req := httptest.NewRequest(http.MethodPut, "/api/tuning/critic-prompt",
		strings.NewReader(`{"body":"   \n"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.PutPrompt(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", res.Code, res.Body.String())
	}
}

func TestTuningHandler_GetGolden_ReturnsEmptyPairsAndFreshness(t *testing.T) {
	svc, _ := newTuningTestService(t, nil)
	handler := NewTuningHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/tuning/golden", nil)
	res := httptest.NewRecorder()
	handler.GetGolden(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	var env struct {
		Version int                       `json:"version"`
		Data    service.TuningGoldenState `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.PairCount != 0 {
		t.Errorf("pair_count = %d, want 0", env.Data.PairCount)
	}
	if env.Data.Freshness.CurrentPromptHash == "" {
		t.Errorf("freshness.current_prompt_hash empty")
	}
}

func TestTuningHandler_AddGoldenPair_RejectsMissingNegative(t *testing.T) {
	svc, _ := newTuningTestService(t, nil)
	handler := NewTuningHandler(svc)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	pos, _ := mw.CreateFormFile("positive", "positive.json")
	_, _ = io.WriteString(pos, `{"fixture_id":"p","kind":"positive"}`)
	// deliberately omit negative field
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/tuning/golden/pairs", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res := httptest.NewRecorder()
	handler.AddGoldenPair(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", res.Code, res.Body.String())
	}
}

// AC-5 mirror: the pair-add path must reject missing positive, not just
// missing negative. Both slots are required; the 1:1 rule forbids a
// single-fixture add path.
func TestTuningHandler_AddGoldenPair_RejectsMissingPositive(t *testing.T) {
	svc, _ := newTuningTestService(t, nil)
	handler := NewTuningHandler(svc)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	neg, _ := mw.CreateFormFile("negative", "negative.json")
	_, _ = io.WriteString(neg, `{"fixture_id":"n","kind":"negative"}`)
	// deliberately omit positive field
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/tuning/golden/pairs", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res := httptest.NewRecorder()
	handler.AddGoldenPair(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", res.Code, res.Body.String())
	}
}

func TestTuningHandler_AddGoldenPair_RejectsNonMultipart(t *testing.T) {
	svc, _ := newTuningTestService(t, nil)
	handler := NewTuningHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/tuning/golden/pairs",
		strings.NewReader(`{"positive":"p","negative":"n"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.AddGoldenPair(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", res.Code, res.Body.String())
	}
}

func TestTuningHandler_RunShadow_WithoutSourceFailsLoud(t *testing.T) {
	svc, _ := newTuningTestService(t, eval.NotConfiguredEvaluator{})
	handler := NewTuningHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/tuning/shadow/run", nil)
	res := httptest.NewRecorder()
	handler.RunShadow(res, req)

	// Shadow source not configured → ErrValidation → 400.
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", res.Code, res.Body.String())
	}
	var env struct {
		Version int `json:"version"`
		Error   struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("error.code = %q, want VALIDATION_ERROR", env.Error.Code)
	}
}

func TestTuningHandler_GetCalibration_ReturnsOldestToNewest(t *testing.T) {
	svc, _ := newTuningTestService(t, nil)
	handler := NewTuningHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/tuning/calibration?window=20&limit=3", nil)
	res := httptest.NewRecorder()
	handler.GetCalibration(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	var env struct {
		Version int                     `json:"version"`
		Data    service.TuningCalibration `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Window != 20 {
		t.Errorf("window = %d, want 20", env.Data.Window)
	}
	if env.Data.Limit != 3 {
		t.Errorf("limit = %d, want 3", env.Data.Limit)
	}
}

func TestTuningHandler_GetCalibration_MissingStoreFailsLoud(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "prompts", "scenario"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "prompts", "scenario", "critic_agent.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := service.NewTuningService(service.TuningServiceOptions{
		ProjectRoot: root,
		Evaluator:   eval.NotConfiguredEvaluator{},
		Calibration: nil,
	})
	handler := NewTuningHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/tuning/calibration", nil)
	res := httptest.NewRecorder()
	handler.GetCalibration(res, req)

	if res.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.Code)
	}
}

func TestTuningHandler_FastFeedback_EvaluatesExactlyTenSamplesInOrder(t *testing.T) {
	svc, root := newTuningTestService(t, deterministicPassEvaluator{score: 85})

	// Mirror the real fast_feedback corpus into the test project tree so
	// the runner finds 10 samples without depending on the repo layout.
	if err := os.MkdirAll(filepath.Join(root, "testdata", "fixtures", "fast_feedback"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// handler_tuning_test.go runs from internal/api, so climb to repo root.
	repoRoot = filepath.Join(repoRoot, "..", "..")
	corpus, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "fixtures", "fast_feedback", "corpus.json"))
	if err != nil {
		t.Fatalf("read real corpus: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "testdata", "fixtures", "fast_feedback", "corpus.json"), corpus, 0o644); err != nil {
		t.Fatalf("seed corpus: %v", err)
	}

	handler := NewTuningHandler(svc)
	req := httptest.NewRequest(http.MethodPost, "/api/tuning/fast-feedback", nil)
	res := httptest.NewRecorder()
	handler.FastFeedback(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	var env struct {
		Version int                         `json:"version"`
		Data    service.FastFeedbackReport  `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.SampleCount != 10 {
		t.Errorf("sample_count = %d, want 10", env.Data.SampleCount)
	}
	if env.Data.PassCount != 10 {
		t.Errorf("pass_count = %d, want 10 (deterministic pass evaluator)", env.Data.PassCount)
	}
	if len(env.Data.Samples) != 10 {
		t.Fatalf("samples len = %d, want 10", len(env.Data.Samples))
	}
	// Deterministic ordering: samples[i].fixture_id must match corpus order.
	for i, s := range env.Data.Samples {
		want := "ff_sample_0" + string(rune('0'+i))
		if s.FixtureID != want {
			t.Errorf("samples[%d].fixture_id = %q, want %q", i, s.FixtureID, want)
		}
	}
}

func TestTuningHandler_FastFeedback_FailsLoudOnShortCorpus(t *testing.T) {
	svc, root := newTuningTestService(t, deterministicPassEvaluator{score: 80})
	if err := os.MkdirAll(filepath.Join(root, "testdata", "fixtures", "fast_feedback"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Only three samples — must be rejected.
	short := `[
	  {"fixture_id":"x1","kind":"positive","checkpoint":"post_writer","input":{},"expected_verdict":"pass"},
	  {"fixture_id":"x2","kind":"positive","checkpoint":"post_writer","input":{},"expected_verdict":"pass"},
	  {"fixture_id":"x3","kind":"positive","checkpoint":"post_writer","input":{},"expected_verdict":"pass"}
	]`
	if err := os.WriteFile(filepath.Join(root, "testdata", "fixtures", "fast_feedback", "corpus.json"), []byte(short), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := NewTuningHandler(svc)
	req := httptest.NewRequest(http.MethodPost, "/api/tuning/fast-feedback", nil)
	res := httptest.NewRecorder()
	handler.FastFeedback(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", res.Code, res.Body.String())
	}
}

// fakeTuningService implements the api.TuningService interface directly so
// handler-level tests can assert shape without staging the full evaluator
// + scenario-fixture machinery. Only the methods a given test exercises
// need to be populated.
type fakeTuningService struct {
	shadowReport service.TuningShadowReport
	shadowErr    error
	calibration  service.TuningCalibration
	calibErr     error
}

func (f *fakeTuningService) GetCriticPrompt(context.Context) (service.CriticPromptEnvelope, error) {
	return service.CriticPromptEnvelope{}, nil
}
func (f *fakeTuningService) SaveCriticPrompt(context.Context, string) (service.CriticPromptEnvelope, error) {
	return service.CriticPromptEnvelope{}, nil
}
func (f *fakeTuningService) GoldenState(context.Context) (service.TuningGoldenState, error) {
	return service.TuningGoldenState{}, nil
}
func (f *fakeTuningService) RunGolden(context.Context) (service.TuningGoldenReport, error) {
	return service.TuningGoldenReport{}, nil
}
func (f *fakeTuningService) AddGoldenPair(context.Context, []byte, []byte) (service.TuningGoldenPair, error) {
	return service.TuningGoldenPair{}, nil
}
func (f *fakeTuningService) RunShadow(context.Context) (service.TuningShadowReport, error) {
	return f.shadowReport, f.shadowErr
}
func (f *fakeTuningService) Calibration(_ context.Context, window, limit int) (service.TuningCalibration, error) {
	out := f.calibration
	out.Window = window
	out.Limit = limit
	return out, f.calibErr
}
func (f *fakeTuningService) FastFeedback(context.Context) (service.FastFeedbackReport, error) {
	return service.FastFeedbackReport{}, nil
}

// AC-6 Rule: "API test verifies Shadow report is returned even when false
// rejections are present." Regressions are data, not transport errors —
// the handler must emit 200 with the full payload so the UI can render
// the retry/false-rejection rows.
func TestTuningHandler_RunShadow_ReturnsReportWithFalseRejections(t *testing.T) {
	fake := &fakeTuningService{
		shadowReport: service.TuningShadowReport{
			Window:          20,
			Evaluated:       2,
			FalseRejections: 1,
			Empty:           false,
			SummaryLine:     "shadow eval: window=20 evaluated=2 false_rejections=1",
			Results: []service.TuningShadowResultRow{
				{
					RunID:           "scp-001-run-1",
					CreatedAt:       "2026-04-24T00:00:00Z",
					BaselineVerdict: "pass",
					BaselineScore:   0.85,
					NewVerdict:      "retry",
					NewOverallScore: 55,
					OverallDiff:     -0.30,
					FalseRejection:  true,
				},
				{
					RunID:           "scp-001-run-2",
					CreatedAt:       "2026-04-23T00:00:00Z",
					BaselineVerdict: "pass",
					BaselineScore:   0.88,
					NewVerdict:      "pass",
					NewOverallScore: 87,
					OverallDiff:     -0.01,
					FalseRejection:  false,
				},
			},
			VersionTag: "20260424T030000Z-abc1234",
		},
	}
	handler := NewTuningHandler(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/tuning/shadow/run", nil)
	res := httptest.NewRecorder()
	handler.RunShadow(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	var env struct {
		Version int                         `json:"version"`
		Data    service.TuningShadowReport  `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.FalseRejections != 1 {
		t.Errorf("false_rejections = %d, want 1", env.Data.FalseRejections)
	}
	if len(env.Data.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(env.Data.Results))
	}
	if !env.Data.Results[0].FalseRejection {
		t.Errorf("results[0].false_rejection = false, want true")
	}
	if env.Data.VersionTag != "20260424T030000Z-abc1234" {
		t.Errorf("version_tag = %q, want populated tag", env.Data.VersionTag)
	}
}

// AC-8 Rule: "API test verifies oldest → newest ordering and provisional
// fields." The service layer is a pass-through over
// RecentCriticCalibrationTrend, so exercising the handler with a stub
// that hands back a canned oldest→newest slice proves the wire contract
// preserves the ordering guarantee end-to-end.
func TestTuningHandler_GetCalibration_PreservesOldestToNewestOrdering(t *testing.T) {
	kappa := 0.72
	svc, _ := newTuningTestService(t, nil)
	// Swap in a calibration reader that returns a concrete trend.
	trendSvc := service.NewTuningService(service.TuningServiceOptions{
		ProjectRoot: tempProjectRoot(t),
		Calibration: stubCalibration{
			points: []domain.CriticCalibrationTrendPoint{
				{ComputedAt: "2026-04-20T00:00:00Z", WindowCount: 5, Provisional: true, Reason: "insufficient_data"},
				{ComputedAt: "2026-04-22T00:00:00Z", WindowCount: 12, Provisional: true},
				{ComputedAt: "2026-04-24T00:00:00Z", WindowCount: 20, Provisional: false, Kappa: &kappa},
			},
		},
	})
	_ = svc
	handler := NewTuningHandler(trendSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/tuning/calibration?window=20&limit=3", nil)
	res := httptest.NewRecorder()
	handler.GetCalibration(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	var env struct {
		Version int                       `json:"version"`
		Data    service.TuningCalibration `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Points) != 3 {
		t.Fatalf("points len = %d, want 3", len(env.Data.Points))
	}
	// Oldest → newest ordering must be preserved.
	wantOrder := []string{"2026-04-20T00:00:00Z", "2026-04-22T00:00:00Z", "2026-04-24T00:00:00Z"}
	for i, want := range wantOrder {
		if env.Data.Points[i].ComputedAt != want {
			t.Errorf("points[%d].computed_at = %q, want %q", i, env.Data.Points[i].ComputedAt, want)
		}
	}
	// Latest summary must reflect the last point (newest) with non-provisional kappa.
	if env.Data.Latest == nil {
		t.Fatalf("latest missing")
	}
	if env.Data.Latest.Provisional {
		t.Errorf("latest.provisional = true, want false")
	}
	if env.Data.Latest.Kappa == nil || *env.Data.Latest.Kappa != kappa {
		t.Errorf("latest.kappa = %v, want %v", env.Data.Latest.Kappa, kappa)
	}
	// Early provisional points must keep their provisional=true flag.
	if !env.Data.Points[0].Provisional {
		t.Errorf("points[0].provisional = false, want true")
	}
}

// tempProjectRoot builds a minimal project tree that satisfies
// TuningService construction invariants (prompt file exists, so the
// constructor's hydration path finds something to hash). Shared across
// tests that only need a stub calibration reader.
func tempProjectRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "prompts", "scenario"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "prompts", "scenario", "critic_agent.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	return root
}

// mustJSONString escapes v into a JSON string literal so it can be inlined
// into a request body template without hand-escaping quotes/backslashes.
func mustJSONString(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// silence unused import warnings in stub-only branches.
var _ = errors.Is
