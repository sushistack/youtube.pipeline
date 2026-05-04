package pipeline_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

func TestComputeFingerprint_Deterministic(t *testing.T) {
	in := pipeline.FingerprintInputs{
		SourceVersion:     "v1.2-roles",
		PromptTemplateSHA: "abc123",
		FewshotSHA:        "def456",
		Model:             "qwen-plus",
		Provider:          "dashscope",
		SchemaVersion:     "v1",
	}
	a := pipeline.ComputeFingerprint(in)
	b := pipeline.ComputeFingerprint(in)
	if a != b {
		t.Errorf("non-deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(a))
	}
}

func TestComputeFingerprint_FieldSensitivity(t *testing.T) {
	base := pipeline.FingerprintInputs{
		SourceVersion:     "v1.2-roles",
		PromptTemplateSHA: "abc123",
		FewshotSHA:        "def456",
		Model:             "qwen-plus",
		Provider:          "dashscope",
		SchemaVersion:     "v1",
	}
	baseHash := pipeline.ComputeFingerprint(base)

	cases := []struct {
		name  string
		tweak func(f *pipeline.FingerprintInputs)
	}{
		{"source_version", func(f *pipeline.FingerprintInputs) { f.SourceVersion = "v2.0" }},
		{"prompt_template_sha", func(f *pipeline.FingerprintInputs) { f.PromptTemplateSHA = "CHANGED" }},
		{"fewshot_sha", func(f *pipeline.FingerprintInputs) { f.FewshotSHA = "CHANGED" }},
		{"model", func(f *pipeline.FingerprintInputs) { f.Model = "qwen-max" }},
		{"provider", func(f *pipeline.FingerprintInputs) { f.Provider = "gemini" }},
		{"schema_version", func(f *pipeline.FingerprintInputs) { f.SchemaVersion = "v2" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tweaked := base
			tc.tweak(&tweaked)
			got := pipeline.ComputeFingerprint(tweaked)
			if got == baseHash {
				t.Errorf("field %s change did not change fingerprint", tc.name)
			}
		})
	}
}

func TestCacheEnvelope_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "_cache", "research_cache.json")

	inputs := pipeline.FingerprintInputs{
		SourceVersion: "v1.2-roles",
		Model:         "qwen-plus",
		Provider:      "dashscope",
		SchemaVersion: "v1",
	}
	payload := map[string]string{"scp_id": "049", "foo": "bar"}
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	if err := pipeline.WriteEnvelope(path, inputs, payload, now); err != nil {
		t.Fatalf("WriteEnvelope: %v", err)
	}

	env, reason, err := pipeline.LoadEnvelope(path, inputs)
	if err != nil {
		t.Fatalf("LoadEnvelope error: %v", err)
	}
	if reason != "" {
		t.Errorf("staleness_reason = %q, want empty (cache hit)", reason)
	}
	if env == nil {
		t.Fatal("envelope is nil on cache hit")
	}
	if env.EnvelopeVersion != pipeline.CacheEnvelopeVersion {
		t.Errorf("envelope_version = %d, want %d", env.EnvelopeVersion, pipeline.CacheEnvelopeVersion)
	}
	// Payload round-trip
	var got map[string]string
	if err := json.Unmarshal(env.Payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got["scp_id"] != "049" {
		t.Errorf("payload scp_id = %q, want %q", got["scp_id"], "049")
	}
}

func TestLoadEnvelope_MissingFile(t *testing.T) {
	env, reason, err := pipeline.LoadEnvelope("/does/not/exist.json", pipeline.FingerprintInputs{})
	if !os.IsNotExist(err) {
		t.Errorf("expected ErrNotExist, got err=%v reason=%s env=%v", err, reason, env)
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty for missing file", reason)
	}
}

func TestLoadEnvelope_LegacyFlatPayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flat.json")
	// Legacy flat payload: no envelope_version field → EnvelopeVersion zero value
	if err := os.WriteFile(path, []byte(`{"scp_id":"049","source_version":"v1-old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	env, reason, err := pipeline.LoadEnvelope(path, pipeline.FingerprintInputs{})
	if err != nil {
		t.Fatalf("LoadEnvelope error: %v", err)
	}
	if reason != pipeline.StaleEnvelopeCorrupt {
		t.Errorf("reason = %q, want %q", reason, pipeline.StaleEnvelopeCorrupt)
	}
	_ = env // may be nil or partial
}

func TestLoadEnvelope_StalenessClassification(t *testing.T) {
	dir := t.TempDir()

	writeWithInputs := func(name string, inputs pipeline.FingerprintInputs) string {
		path := filepath.Join(dir, name)
		if err := pipeline.WriteEnvelope(path, inputs, "payload", time.Now()); err != nil {
			t.Fatalf("WriteEnvelope %s: %v", name, err)
		}
		return path
	}

	base := pipeline.FingerprintInputs{
		SourceVersion: "v1.2-roles",
		Model:         "qwen-plus",
		Provider:      "dashscope",
		SchemaVersion: "v1",
	}

	cases := []struct {
		name           string
		stored         pipeline.FingerprintInputs
		expected       pipeline.FingerprintInputs
		wantStaleness  pipeline.StalenessReason
	}{
		{
			name:          "source_version_mismatch",
			stored:        base,
			expected:      func() pipeline.FingerprintInputs { f := base; f.SourceVersion = "v2.0"; return f }(),
			wantStaleness: pipeline.StaleSourceVersionMismatch,
		},
		{
			name:          "model_changed",
			stored:        base,
			expected:      func() pipeline.FingerprintInputs { f := base; f.Model = "qwen-max"; return f }(),
			wantStaleness: pipeline.StaleModelChanged,
		},
		{
			name:          "provider_changed",
			stored:        base,
			expected:      func() pipeline.FingerprintInputs { f := base; f.Provider = "gemini"; return f }(),
			wantStaleness: pipeline.StaleProviderChanged,
		},
		{
			name:          "prompt_template_changed",
			stored:        base,
			expected:      func() pipeline.FingerprintInputs { f := base; f.PromptTemplateSHA = "NEWHASH"; return f }(),
			wantStaleness: pipeline.StalePromptTemplateChanged,
		},
		{
			name:          "schema_changed",
			stored:        base,
			expected:      func() pipeline.FingerprintInputs { f := base; f.SchemaVersion = "v2"; return f }(),
			wantStaleness: pipeline.StaleSchemaChanged,
		},
		{
			name:          "fewshot_changed",
			stored:        base,
			expected:      func() pipeline.FingerprintInputs { f := base; f.FewshotSHA = "NEWHASH"; return f }(),
			wantStaleness: pipeline.StaleFewshotChanged,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeWithInputs(tc.name+".json", tc.stored)
			_, reason, err := pipeline.LoadEnvelope(path, tc.expected)
			if err != nil {
				t.Fatalf("LoadEnvelope: %v", err)
			}
			if reason != tc.wantStaleness {
				t.Errorf("staleness_reason = %q, want %q", reason, tc.wantStaleness)
			}
		})
	}
}

func TestCacheDir(t *testing.T) {
	got := pipeline.CacheDir("/output/run-1")
	want := "/output/run-1/_cache"
	if got != want {
		t.Errorf("CacheDir = %q, want %q", got, want)
	}
}

func TestTracesDir(t *testing.T) {
	got := pipeline.TracesDir("/output/run-1")
	want := "/output/run-1/traces"
	if got != want {
		t.Errorf("TracesDir = %q, want %q", got, want)
	}
}
