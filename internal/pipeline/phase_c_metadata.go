package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
)

// MetadataBuilder is the interface for building and writing metadata bundles.
type MetadataBuilder interface {
	Build(ctx context.Context, runID string) (domain.MetadataBundle, domain.SourceManifest, error)
	Write(ctx context.Context, runID string, bundle domain.MetadataBundle, manifest domain.SourceManifest) error
}

// MetadataBuilderFunc is a function adapter that implements MetadataBuilder.
type MetadataBuilderFunc func(ctx context.Context, runID string) (domain.MetadataBundle, domain.SourceManifest, error)

// Build implements MetadataBuilder.Build.
func (f MetadataBuilderFunc) Build(ctx context.Context, runID string) (domain.MetadataBundle, domain.SourceManifest, error) {
	return f(ctx, runID)
}

// Write implements MetadataBuilder.Write.
func (MetadataBuilderFunc) Write(_ context.Context, _ string, _ domain.MetadataBundle, _ domain.SourceManifest) error {
	return fmt.Errorf("metadata builder func: %w: Write not implemented on function adapter", domain.ErrValidation)
}

// MetadataBuilderConfig bundles the dependencies required to build a metadata
// builder. Provider/model identifiers are config-driven so business logic does
// not hard-code provider names.
type MetadataBuilderConfig struct {
	OutputDir           string
	WriterModel         string
	WriterProvider      string
	CriticModel         string
	CriticProvider      string
	ImageModel          string
	ImageProvider       string
	TTSModel            string
	TTSProvider         string
	TTSVoice            string
	Corpus              agents.CorpusReader
	Clock               clock.Clock
	Logger              *slog.Logger
}

type metadataBuilder struct {
	cfg MetadataBuilderConfig
}

// NewMetadataBuilder constructs a MetadataBuilder from cfg. Returns
// domain.ErrValidation if required fields are missing.
func NewMetadataBuilder(cfg MetadataBuilderConfig) (MetadataBuilder, error) {
	if cfg.OutputDir == "" {
		return nil, fmt.Errorf("metadata builder: %w: output dir is empty", domain.ErrValidation)
	}
	if cfg.CriticModel == "" {
		return nil, fmt.Errorf("metadata builder: %w: critic model is empty", domain.ErrValidation)
	}
	if cfg.CriticProvider == "" {
		return nil, fmt.Errorf("metadata builder: %w: critic provider is empty", domain.ErrValidation)
	}
	if cfg.Corpus == nil {
		return nil, fmt.Errorf("metadata builder: %w: corpus reader is nil", domain.ErrValidation)
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &metadataBuilder{
		cfg: MetadataBuilderConfig{
			OutputDir:      cfg.OutputDir,
			WriterModel:    cfg.WriterModel,
			WriterProvider: cfg.WriterProvider,
			CriticModel:    cfg.CriticModel,
			CriticProvider: cfg.CriticProvider,
			ImageModel:     cfg.ImageModel,
			ImageProvider:  cfg.ImageProvider,
			TTSModel:       cfg.TTSModel,
			TTSProvider:    cfg.TTSProvider,
			TTSVoice:       cfg.TTSVoice,
			Corpus:         cfg.Corpus,
			Clock:          clk,
			Logger:         logger,
		},
	}, nil
}

// Build reads scenario.json and corpus metadata, then assembles and returns
// a MetadataBundle and SourceManifest. Returns domain.ErrValidation for
// missing or invalid data.
func (b *metadataBuilder) Build(ctx context.Context, runID string) (domain.MetadataBundle, domain.SourceManifest, error) {
	if runID == "" {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: run id is empty", domain.ErrValidation)
	}

	runDir := filepath.Join(b.cfg.OutputDir, runID)

	// Read scenario.json.
	scenarioPath := filepath.Join(runDir, "scenario.json")
	raw, err := os.ReadFile(scenarioPath)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: scenario.json missing at %s", domain.ErrValidation, scenarioPath)
		}
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: read scenario.json: %w", err)
	}

	var state agents.PipelineState
	if err := json.Unmarshal(raw, &state); err != nil {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: decode scenario.json: %v", domain.ErrValidation, err)
	}

	// Read corpus metadata.
	corpusDoc, err := b.cfg.Corpus.Read(ctx, state.SCPID)
	if err != nil {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: read corpus: %w", err)
	}

	// Determine title.
	title := state.SCPID
	if state.Narration != nil && state.Narration.Title != "" {
		title = state.Narration.Title
	} else if state.Research != nil && state.Research.Title != "" {
		title = state.Research.Title
	}

	// Validate required fields.
	if b.cfg.WriterProvider == "" && state.Narration != nil && state.Narration.Metadata.WriterProvider != "" {
		// Use config-level WriterProvider if set, otherwise fall through to scenario.json.
	}
	writerProvider := b.cfg.WriterProvider
	writerModel := b.cfg.WriterModel
	if writerProvider == "" && state.Narration != nil {
		writerProvider = state.Narration.Metadata.WriterProvider
	}
	if writerModel == "" && state.Narration != nil {
		writerModel = state.Narration.Metadata.WriterModel
	}
	if writerProvider == "" {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: writer provider is empty", domain.ErrValidation)
	}
	if writerModel == "" {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: writer model is empty", domain.ErrValidation)
	}

	// Visual breakdown model/provider from scenario.json.
	var vbProvider, vbModel string
	if state.VisualBreakdown != nil {
		vbProvider = state.VisualBreakdown.Metadata.VisualBreakdownProvider
		vbModel = state.VisualBreakdown.Metadata.VisualBreakdownModel
	}
	if vbProvider == "" {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: visual breakdown provider is empty", domain.ErrValidation)
	}
	if vbModel == "" {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: visual breakdown model is empty", domain.ErrValidation)
	}

	// Validate corpus attribution fields.
	if corpusDoc.Meta.AuthorName == "" {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: corpus author_name is empty for %q", domain.ErrValidation, state.SCPID)
	}
	if corpusDoc.Meta.SourceURL == "" {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: corpus source_url is empty for %q", domain.ErrValidation, state.SCPID)
	}

	now := b.cfg.Clock.Now()
	generatedAt := now.Format(time.RFC3339)

	// Build ModelsUsed map.
	modelsUsed := map[string]domain.ModelRecord{
		"writer":           {Provider: writerProvider, Model: writerModel},
		"critic":           {Provider: b.cfg.CriticProvider, Model: b.cfg.CriticModel},
		"image":            {Provider: b.cfg.ImageProvider, Model: b.cfg.ImageModel},
		"tts":              {Provider: b.cfg.TTSProvider, Model: b.cfg.TTSModel, Voice: b.cfg.TTSVoice},
		"visual_breakdown": {Provider: vbProvider, Model: vbModel},
	}

	// Validate all 5 entries are non-empty (FR45 non-null guarantee).
	requiredKeys := []string{"writer", "critic", "image", "tts", "visual_breakdown"}
	for _, key := range requiredKeys {
		rec, ok := modelsUsed[key]
		if !ok {
			return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: models_used missing key %q", domain.ErrValidation, key)
		}
		if rec.Provider == "" {
			return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: models_used[%q].provider is empty", domain.ErrValidation, key)
		}
		if rec.Model == "" {
			return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: models_used[%q].model is empty", domain.ErrValidation, key)
		}
	}

	bundle := domain.MetadataBundle{
		Version:     1,
		GeneratedAt: generatedAt,
		RunID:       runID,
		SCPID:       state.SCPID,
		Title:       title,
		AIGenerated: domain.AIGeneratedFlags{
			Narration: true,
			Imagery:   true,
			TTS:       true,
		},
		ModelsUsed: modelsUsed,
	}

	manifest := domain.SourceManifest{
		Version:     1,
		GeneratedAt: generatedAt,
		RunID:       runID,
		SCPID:       state.SCPID,
		SourceURL:   corpusDoc.Meta.SourceURL,
		AuthorName:  corpusDoc.Meta.AuthorName,
		License:     domain.LicenseCCBYSA30,
		LicenseURL:  domain.LicenseURLCCBYSA30,
		LicenseChain: []domain.LicenseEntry{
			{
				Component:  "SCP article text",
				SourceURL:  corpusDoc.Meta.SourceURL,
				AuthorName: corpusDoc.Meta.AuthorName,
				License:    domain.LicenseCCBYSA30,
			},
		},
	}

	b.cfg.Logger.Info("metadata bundle built",
		"run_id", runID,
		"scp_id", state.SCPID,
		"title", title,
	)

	return bundle, manifest, nil
}

// Write atomically writes metadata.json and manifest.json to the run output
// directory. Uses a temp file + os.Rename for atomicity.
func (b *metadataBuilder) Write(_ context.Context, runID string, bundle domain.MetadataBundle, manifest domain.SourceManifest) error {
	if runID == "" {
		return fmt.Errorf("metadata builder write: %w: run id is empty", domain.ErrValidation)
	}

	runDir := filepath.Join(b.cfg.OutputDir, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("metadata builder write: create run dir: %w", err)
	}

	// Write metadata.json atomically.
	if err := writeJSONFile(filepath.Join(runDir, "metadata.json"), bundle); err != nil {
		return fmt.Errorf("metadata builder write: metadata.json: %w", err)
	}

	// Write manifest.json atomically.
	if err := writeJSONFile(filepath.Join(runDir, "manifest.json"), manifest); err != nil {
		return fmt.Errorf("metadata builder write: manifest.json: %w", err)
	}

	b.cfg.Logger.Info("metadata files written",
		"run_id", runID,
		"dir", runDir,
	)

	return nil
}

// writeJSONFile marshals v as indented JSON and writes it atomically to path
// using a temp file + os.Rename.
func writeJSONFile(path string, v any) error {
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}
	return nil
}
