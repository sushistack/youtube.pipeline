package pipeline_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// fakeCorpusReader implements agents.CorpusReader for testing.
type fakeCorpusReader struct {
	doc  agents.CorpusDocument
	err  error
}

func (f *fakeCorpusReader) Read(_ context.Context, scpID string) (agents.CorpusDocument, error) {
	if f.err != nil {
		return agents.CorpusDocument{}, f.err
	}
	doc := f.doc
	doc.SCPID = scpID
	return doc, nil
}

// fakeClock returns a fixed time for testing.
type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }
func (f *fakeClock) Sleep(_ context.Context, _ time.Duration) error { return nil }

func fixedTime() time.Time {
	t, _ := time.Parse(time.RFC3339, "2026-04-22T09:00:00Z")
	return t
}

func writeScenarioJSON(t *testing.T, dir string, state *agents.PipelineState) {
	t.Helper()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal scenario: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenario.json"), data, 0o644); err != nil {
		t.Fatalf("write scenario.json: %v", err)
	}
}

func validPipelineState(scpID string) *agents.PipelineState {
	return &agents.PipelineState{
		RunID: "test-run-1",
		SCPID: scpID,
		Research: &domain.ResearcherOutput{
			Title: "SCP-" + scpID + " - Test Title",
		},
		Narration: &domain.NarrationScript{
			Metadata: domain.NarrationMetadata{
				WriterModel:    "deepseek-chat",
				WriterProvider: "deepseek",
			},
		},
		VisualBreakdown: &domain.VisualBreakdownOutput{
			Metadata: domain.VisualBreakdownMetadata{
				VisualBreakdownModel:    "gemini-2.0-flash",
				VisualBreakdownProvider: "gemini",
			},
		},
	}
}

func validCorpusDoc() agents.CorpusDocument {
	return agents.CorpusDocument{
		Meta: agents.SCPMeta{
			SCPID:      "TEST",
			AuthorName: "Test Author",
			SourceURL:  "https://scp-wiki.wikidot.com/scp-test",
		},
	}
}

func validMetadataBuilderConfig(t *testing.T, outputDir string) pipeline.MetadataBuilderConfig {
	t.Helper()
	return pipeline.MetadataBuilderConfig{
		OutputDir:      outputDir,
		WriterModel:    "deepseek-chat",
		WriterProvider: "deepseek",
		CriticModel:    "gemini-2.0-flash",
		CriticProvider: "gemini",
		ImageModel:     "qwen-max-vl",
		ImageProvider:  "dashscope",
		TTSModel:       "qwen3-tts-flash-2025-09-18",
		TTSProvider:    "dashscope",
		TTSVoice:       "longhua",
		Corpus:         &fakeCorpusReader{doc: validCorpusDoc()},
		Clock:          &fakeClock{now: fixedTime()},
	}
}

func TestNewMetadataBuilder_Valid(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()
	cfg := validMetadataBuilderConfig(t, dir)
	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}
	if builder == nil {
		t.Fatal("builder is nil")
	}
}

func TestNewMetadataBuilder_ValidationErrors(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()
	validCfg := validMetadataBuilderConfig(t, dir)

	tests := []struct {
		name string
		mod  func(*pipeline.MetadataBuilderConfig)
	}{
		{"empty output dir", func(c *pipeline.MetadataBuilderConfig) { c.OutputDir = "" }},
		{"empty critic model", func(c *pipeline.MetadataBuilderConfig) { c.CriticModel = "" }},
		{"empty critic provider", func(c *pipeline.MetadataBuilderConfig) { c.CriticProvider = "" }},
		{"nil corpus", func(c *pipeline.MetadataBuilderConfig) { c.Corpus = nil }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validCfg
			tc.mod(&cfg)
			_, err := pipeline.NewMetadataBuilder(cfg)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestMetadataBuilder_Build_HappyPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	state := validPipelineState("049")
	writeScenarioJSON(t, filepath.Join(dir, "test-run-1"), state)

	cfg := validMetadataBuilderConfig(t, dir)
	cfg.Corpus = &fakeCorpusReader{
		doc: agents.CorpusDocument{
			Meta: agents.SCPMeta{
				SCPID:      "049",
				AuthorName: "Djoric",
				SourceURL:  "https://scp-wiki.wikidot.com/scp-049",
			},
		},
	}

	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	bundle, manifest, err := builder.Build(context.Background(), "test-run-1")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Verify MetadataBundle.
	if bundle.Version != 1 {
		t.Errorf("Version = %d, want 1", bundle.Version)
	}
	if bundle.RunID != "test-run-1" {
		t.Errorf("RunID = %q, want %q", bundle.RunID, "test-run-1")
	}
	if bundle.SCPID != "049" {
		t.Errorf("SCPID = %q, want %q", bundle.SCPID, "049")
	}
	if bundle.Title != "SCP-049 - Test Title" {
		t.Errorf("Title = %q (should come from Research.Title, not Narration.Title)", bundle.Title)
	}
	if !bundle.AIGenerated.Narration || !bundle.AIGenerated.Imagery || !bundle.AIGenerated.TTS {
		t.Error("AIGenerated flags not all true")
	}

	// Verify all 5 ModelsUsed keys.
	expectedKeys := []string{"writer", "critic", "image", "tts", "visual_breakdown"}
	for _, k := range expectedKeys {
		rec, ok := bundle.ModelsUsed[k]
		if !ok {
			t.Errorf("ModelsUsed missing key %q", k)
			continue
		}
		if rec.Provider == "" {
			t.Errorf("ModelsUsed[%q].Provider is empty", k)
		}
		if rec.Model == "" {
			t.Errorf("ModelsUsed[%q].Model is empty", k)
		}
	}

	// Verify SourceManifest.
	if manifest.Version != 1 {
		t.Errorf("manifest Version = %d, want 1", manifest.Version)
	}
	if manifest.SourceURL != "https://scp-wiki.wikidot.com/scp-049" {
		t.Errorf("SourceURL = %q, want %q", manifest.SourceURL, "https://scp-wiki.wikidot.com/scp-049")
	}
	if manifest.AuthorName != "Djoric" {
		t.Errorf("AuthorName = %q, want %q", manifest.AuthorName, "Djoric")
	}
	if manifest.License != domain.LicenseCCBYSA30 {
		t.Errorf("License = %q, want %q", manifest.License, domain.LicenseCCBYSA30)
	}
	if len(manifest.LicenseChain) != 1 {
		t.Fatalf("LicenseChain length = %d, want 1", len(manifest.LicenseChain))
	}
	if manifest.LicenseChain[0].Component != "SCP article text" {
		t.Errorf("LicenseChain[0].Component = %q, want %q", manifest.LicenseChain[0].Component, "SCP article text")
	}
}

func TestMetadataBuilder_Build_MissingScenarioJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	cfg := validMetadataBuilderConfig(t, dir)
	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	_, _, err = builder.Build(context.Background(), "nonexistent-run")
	if err == nil {
		t.Fatal("expected error for missing scenario.json, got nil")
	}
}

func TestMetadataBuilder_Build_MissingWriterProvider(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	state := validPipelineState("049")
	state.Narration.Metadata.WriterProvider = ""
	writeScenarioJSON(t, filepath.Join(dir, "test-run-1"), state)

	cfg := validMetadataBuilderConfig(t, dir)
	cfg.WriterProvider = "" // Also empty at config level

	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	_, _, err = builder.Build(context.Background(), "test-run-1")
	if err == nil {
		t.Fatal("expected error for missing writer provider, got nil")
	}
}

func TestMetadataBuilder_Build_MissingVisualBreakdownProvider(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	state := validPipelineState("049")
	state.VisualBreakdown.Metadata.VisualBreakdownProvider = ""
	writeScenarioJSON(t, filepath.Join(dir, "test-run-1"), state)

	cfg := validMetadataBuilderConfig(t, dir)
	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	_, _, err = builder.Build(context.Background(), "test-run-1")
	if err == nil {
		t.Fatal("expected error for missing visual breakdown provider, got nil")
	}
}

func TestMetadataBuilder_Build_MissingAuthorName(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	state := validPipelineState("049")
	writeScenarioJSON(t, filepath.Join(dir, "test-run-1"), state)

	cfg := validMetadataBuilderConfig(t, dir)
	cfg.Corpus = &fakeCorpusReader{
		doc: agents.CorpusDocument{
			Meta: agents.SCPMeta{
				SCPID:     "049",
				SourceURL: "https://scp-wiki.wikidot.com/scp-049",
				// AuthorName intentionally empty
			},
		},
	}

	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	_, _, err = builder.Build(context.Background(), "test-run-1")
	if err == nil {
		t.Fatal("expected error for missing author_name, got nil")
	}
}

func TestMetadataBuilder_Build_MissingSourceURL(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	state := validPipelineState("049")
	writeScenarioJSON(t, filepath.Join(dir, "test-run-1"), state)

	cfg := validMetadataBuilderConfig(t, dir)
	cfg.Corpus = &fakeCorpusReader{
		doc: agents.CorpusDocument{
			Meta: agents.SCPMeta{
				SCPID:      "049",
				AuthorName: "Djoric",
				// SourceURL intentionally empty
			},
		},
	}

	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	_, _, err = builder.Build(context.Background(), "test-run-1")
	if err == nil {
		t.Fatal("expected error for missing source_url, got nil")
	}
}

func TestMetadataBuilder_Write_Atomic(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	cfg := validMetadataBuilderConfig(t, dir)
	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	bundle := domain.MetadataBundle{
		Version:     1,
		GeneratedAt: "2026-04-22T09:00:00Z",
		RunID:       "test-run-1",
		SCPID:       "049",
		Title:       "Test",
		AIGenerated: domain.AIGeneratedFlags{Narration: true, Imagery: true, TTS: true},
		ModelsUsed: map[string]domain.ModelRecord{
			"writer":           {Provider: "a", Model: "b"},
			"critic":           {Provider: "c", Model: "d"},
			"image":            {Provider: "e", Model: "f"},
			"tts":              {Provider: "g", Model: "h"},
			"visual_breakdown": {Provider: "i", Model: "j"},
		},
	}

	manifest := domain.SourceManifest{
		Version:     1,
		GeneratedAt: "2026-04-22T09:00:00Z",
		RunID:       "test-run-1",
		SCPID:       "049",
		SourceURL:   "https://scp-wiki.wikidot.com/scp-049",
		AuthorName:  "Djoric",
		License:     domain.LicenseCCBYSA30,
		LicenseURL:  domain.LicenseURLCCBYSA30,
		LicenseChain: []domain.LicenseEntry{
			{Component: "SCP article text", SourceURL: "https://scp-wiki.wikidot.com/scp-049", AuthorName: "Djoric", License: domain.LicenseCCBYSA30},
		},
	}

	if err := builder.Write(context.Background(), "test-run-1", bundle, manifest); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify metadata.json exists and is valid.
	metaPath := filepath.Join(dir, "test-run-1", "metadata.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Fatal("metadata.json not written")
	}
	metaRaw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var gotMeta domain.MetadataBundle
	if err := json.Unmarshal(metaRaw, &gotMeta); err != nil {
		t.Fatalf("unmarshal metadata.json: %v", err)
	}
	if gotMeta.Version != 1 {
		t.Errorf("metadata.json Version = %d, want 1", gotMeta.Version)
	}
	if len(gotMeta.ModelsUsed) != 5 {
		t.Errorf("metadata.json ModelsUsed has %d keys, want 5", len(gotMeta.ModelsUsed))
	}

	// Verify manifest.json exists and is valid.
	manifestPath := filepath.Join(dir, "test-run-1", "manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Fatal("manifest.json not written")
	}
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest.json: %v", err)
	}
	var gotManifest domain.SourceManifest
	if err := json.Unmarshal(manifestRaw, &gotManifest); err != nil {
		t.Fatalf("unmarshal manifest.json: %v", err)
	}
	if gotManifest.Version != 1 {
		t.Errorf("manifest.json Version = %d, want 1", gotManifest.Version)
	}
	if gotManifest.AuthorName != "Djoric" {
		t.Errorf("manifest.json AuthorName = %q, want %q", gotManifest.AuthorName, "Djoric")
	}
}

func TestMetadataBuilder_Write_Idempotent(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	cfg := validMetadataBuilderConfig(t, dir)
	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	bundle := domain.MetadataBundle{
		Version:     1,
		GeneratedAt: "2026-04-22T09:00:00Z",
		RunID:       "test-run-1",
		SCPID:       "049",
		Title:       "Test",
		AIGenerated: domain.AIGeneratedFlags{Narration: true, Imagery: true, TTS: true},
		ModelsUsed: map[string]domain.ModelRecord{
			"writer":           {Provider: "a", Model: "b"},
			"critic":           {Provider: "c", Model: "d"},
			"image":            {Provider: "e", Model: "f"},
			"tts":              {Provider: "g", Model: "h", Voice: "v"},
			"visual_breakdown": {Provider: "i", Model: "j"},
		},
	}

	manifest := domain.SourceManifest{
		Version:     1,
		GeneratedAt: "2026-04-22T09:00:00Z",
		RunID:       "test-run-1",
		SCPID:       "049",
		SourceURL:   "https://scp-wiki.wikidot.com/scp-049",
		AuthorName:  "Djoric",
		License:     domain.LicenseCCBYSA30,
		LicenseURL:  domain.LicenseURLCCBYSA30,
		LicenseChain: []domain.LicenseEntry{
			{Component: "SCP article text", SourceURL: "https://scp-wiki.wikidot.com/scp-049", AuthorName: "Djoric", License: domain.LicenseCCBYSA30},
		},
	}

	// Write twice — second call must succeed (idempotent overwrite).
	if err := builder.Write(context.Background(), "test-run-1", bundle, manifest); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := builder.Write(context.Background(), "test-run-1", bundle, manifest); err != nil {
		t.Fatalf("second Write (idempotent): %v", err)
	}

	// Verify both files still exist and are valid.
	metaPath := filepath.Join(dir, "test-run-1", "metadata.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Fatal("metadata.json missing after second write")
	}
	manifestPath := filepath.Join(dir, "test-run-1", "manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Fatal("manifest.json missing after second write")
	}
}

func TestMetadataBuilder_Write_FileMode(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	cfg := validMetadataBuilderConfig(t, dir)
	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	bundle := domain.MetadataBundle{
		Version: 1, GeneratedAt: "2026-04-22T09:00:00Z",
		RunID: "test-run-1", SCPID: "049", Title: "Test",
		AIGenerated: domain.AIGeneratedFlags{Narration: true, Imagery: true, TTS: true},
		ModelsUsed: map[string]domain.ModelRecord{
			"writer": {Provider: "a", Model: "b"}, "critic": {Provider: "c", Model: "d"},
			"image": {Provider: "e", Model: "f"}, "tts": {Provider: "g", Model: "h"},
			"visual_breakdown": {Provider: "i", Model: "j"},
		},
	}
	manifest := domain.SourceManifest{
		Version: 1, GeneratedAt: "2026-04-22T09:00:00Z",
		RunID: "test-run-1", SCPID: "049",
		SourceURL: "https://scp-wiki.wikidot.com/scp-049", AuthorName: "Djoric",
		License: domain.LicenseCCBYSA30, LicenseURL: domain.LicenseURLCCBYSA30,
	}

	if err := builder.Write(context.Background(), "test-run-1", bundle, manifest); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify file mode is 0644.
	for _, name := range []string{"metadata.json", "manifest.json"} {
		info, err := os.Stat(filepath.Join(dir, "test-run-1", name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		mode := info.Mode().Perm()
		if mode != 0o644 {
			t.Errorf("%s mode = %o, want 0644", name, mode)
		}
	}
}

func TestMetadataBuilder_Write_EmptyRunID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	cfg := validMetadataBuilderConfig(t, dir)
	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	err = builder.Write(context.Background(), "", domain.MetadataBundle{}, domain.SourceManifest{})
	if err == nil {
		t.Fatal("expected error for empty run id, got nil")
	}
}

func TestPhaseCMetadataEntry_HappyPath(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	cfg := validMetadataBuilderConfig(t, dir)
	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	// Write a valid scenario.json.
	state := validPipelineState("049")
	writeScenarioJSON(t, filepath.Join(dir, "test-run-1"), state)

	if err := pipeline.PhaseCMetadataEntry(context.Background(), builder, "test-run-1"); err != nil {
		t.Fatalf("PhaseCMetadataEntry: %v", err)
	}

	// Verify files were written.
	for _, name := range []string{"metadata.json", "manifest.json"} {
		path := filepath.Join(dir, "test-run-1", name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after PhaseCMetadataEntry", name)
		}
	}

	// Verify valid JSON.
	metaRaw, err := os.ReadFile(filepath.Join(dir, "test-run-1", "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var bundle domain.MetadataBundle
	if err := json.Unmarshal(metaRaw, &bundle); err != nil {
		t.Fatalf("unmarshal metadata.json: %v", err)
	}
	if bundle.RunID != "test-run-1" {
		t.Errorf("metadata RunID = %q, want %q", bundle.RunID, "test-run-1")
	}

	manifestRaw, err := os.ReadFile(filepath.Join(dir, "test-run-1", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest.json: %v", err)
	}
	var manifest domain.SourceManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatalf("unmarshal manifest.json: %v", err)
	}
	if manifest.RunID != "test-run-1" {
		t.Errorf("manifest RunID = %q, want %q", manifest.RunID, "test-run-1")
	}
}

func TestMetadataBuilder_Build_EmptySCPID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	state := validPipelineState("049")
	state.SCPID = ""
	writeScenarioJSON(t, filepath.Join(dir, "test-run-1"), state)

	cfg := validMetadataBuilderConfig(t, dir)
	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	_, _, err = builder.Build(context.Background(), "test-run-1")
	if err == nil {
		t.Fatal("expected error for empty scp_id in scenario.json, got nil")
	}
}

func TestMetadataBuilder_Build_EmptyTTSVoice(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	state := validPipelineState("049")
	writeScenarioJSON(t, filepath.Join(dir, "test-run-1"), state)

	cfg := validMetadataBuilderConfig(t, dir)
	cfg.TTSVoice = "" // voice required when TTSProvider + TTSModel are set

	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	_, _, err = builder.Build(context.Background(), "test-run-1")
	if err == nil {
		t.Fatal("expected error for empty tts voice, got nil")
	}
}

func TestPhaseCMetadataEntry_NilBuilder(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	err := pipeline.PhaseCMetadataEntry(context.Background(), nil, "test-run-1")
	if err == nil {
		t.Fatal("expected error for nil builder, got nil")
	}
}

func TestPhaseCMetadataEntry_MissingScenarioJSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()

	cfg := validMetadataBuilderConfig(t, dir)
	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}

	err = pipeline.PhaseCMetadataEntry(context.Background(), builder, "nonexistent-run")
	if err == nil {
		t.Fatal("expected error for missing scenario.json, got nil")
	}
}

func TestPhaseCMetadataEntry_ErrorPropagation(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// MetadataBuilderFunc that always returns an error.
	broken := pipeline.MetadataBuilderFunc(func(_ context.Context, runID string) (domain.MetadataBundle, domain.SourceManifest, error) {
		return domain.MetadataBundle{}, domain.SourceManifest{}, domain.ErrStageFailed
	})

	err := pipeline.PhaseCMetadataEntry(context.Background(), broken, "test-run-1")
	if err == nil {
		t.Fatal("expected error from broken builder, got nil")
	}
}
