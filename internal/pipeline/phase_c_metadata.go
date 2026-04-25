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

// Write implements MetadataBuilder.Write as a no-op. MetadataBuilderFunc is a
// Build-only test adapter; callers that need real file writes must use NewMetadataBuilder.
func (MetadataBuilderFunc) Write(_ context.Context, _ string, _ domain.MetadataBundle, _ domain.SourceManifest) error {
	return nil
}

// FileWriter writes payload bytes to a destination path. Production code uses
// DefaultAtomicWriter (temp file + os.Rename). Tests inject faulting variants
// via MetadataBuilderConfig.Writer to exercise the pair-atomic publish
// protocol used by Phase C metadata (SMOKE-05, R-04).
type FileWriter func(path string, payload []byte) error

// DefaultAtomicWriter writes payload to path atomically via os.CreateTemp +
// os.Rename. The temp file is created in the parent directory of path so the
// rename is atomic on POSIX. The final file mode is 0o644.
func DefaultAtomicWriter(path string, payload []byte) error {
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

// MetadataBuilderConfig bundles the dependencies required to build a metadata
// builder. Provider/model identifiers are config-driven so business logic does
// not hard-code provider names.
type MetadataBuilderConfig struct {
	OutputDir      string
	WriterModel    string
	WriterProvider string
	CriticModel    string
	CriticProvider string
	ImageModel     string
	ImageProvider  string
	TTSModel       string
	TTSProvider    string
	TTSVoice       string
	Corpus         agents.CorpusReader
	Clock          clock.Clock
	Logger         *slog.Logger
	// Writer is the per-file atomic writer used during pair-atomic publish.
	// Defaults to DefaultAtomicWriter when nil. Tests inject faulting variants
	// to drive SMOKE-05.
	Writer FileWriter
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
	writer := cfg.Writer
	if writer == nil {
		writer = DefaultAtomicWriter
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
			Writer:         writer,
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
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: decode scenario.json: %w", domain.ErrValidation, err)
	}

	if state.SCPID == "" {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: scp_id is empty in scenario.json", domain.ErrValidation)
	}

	// Read corpus metadata.
	corpusDoc, err := b.cfg.Corpus.Read(ctx, state.SCPID)
	if err != nil {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: read corpus: %w", err)
	}

	// Determine title: Research.Title is the primary source (AC-3).
	title := state.SCPID
	if state.Research != nil && state.Research.Title != "" {
		title = state.Research.Title
	}

	// Writer model/provider from scenario.json only (AC-3, LLM source map).
	if state.Narration == nil || state.Narration.Metadata.WriterProvider == "" {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: writer provider is empty in scenario.json", domain.ErrValidation)
	}
	if state.Narration.Metadata.WriterModel == "" {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: writer model is empty in scenario.json", domain.ErrValidation)
	}
	writerProvider := state.Narration.Metadata.WriterProvider
	writerModel := state.Narration.Metadata.WriterModel

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
	// TTS voice is required when TTS provider and model are both set (D5).
	if b.cfg.TTSProvider != "" && b.cfg.TTSModel != "" && b.cfg.TTSVoice == "" {
		return domain.MetadataBundle{}, domain.SourceManifest{}, fmt.Errorf("metadata builder: %w: tts voice is empty", domain.ErrValidation)
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

// metadataStagingDirName is the per-run subdirectory used to stage
// metadata.json and manifest.json before pair-atomic publish.
const metadataStagingDirName = ".metadata.staging"

// Write publishes metadata.json and manifest.json to the run output directory
// as an atomic pair: either both files are present and internally consistent,
// or neither is. Pair-atomic publish protocol (SMOKE-05, R-04):
//
//  1. Marshal both payloads in memory; abort early if marshalling fails.
//  2. Stage both files under runDir/.metadata.staging/ via the configured
//     FileWriter (atomic temp+rename per file).
//  3. If either staging write fails, the staging dir is removed and the run
//     dir is untouched.
//  4. Once both files are staged, rename them into runDir. The first rename
//     is undone if the second rename fails so the both-or-neither invariant
//     holds even on partial-publish faults.
func (b *metadataBuilder) Write(_ context.Context, runID string, bundle domain.MetadataBundle, manifest domain.SourceManifest) error {
	if runID == "" {
		return fmt.Errorf("metadata builder write: %w: run id is empty", domain.ErrValidation)
	}

	runDir := filepath.Join(b.cfg.OutputDir, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("metadata builder write: create run dir: %w", err)
	}

	bundleBytes, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return fmt.Errorf("metadata builder write: marshal metadata.json: %w", err)
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("metadata builder write: marshal manifest.json: %w", err)
	}

	stagingDir := filepath.Join(runDir, metadataStagingDirName)
	// Defensive: a previous crashed attempt may have left stale staging files.
	if err := os.RemoveAll(stagingDir); err != nil {
		return fmt.Errorf("metadata builder write: clean staging dir: %w", err)
	}
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return fmt.Errorf("metadata builder write: create staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	writer := b.cfg.Writer
	if writer == nil {
		writer = DefaultAtomicWriter
	}

	metaStaged := filepath.Join(stagingDir, "metadata.json")
	manifestStaged := filepath.Join(stagingDir, "manifest.json")

	if err := writer(metaStaged, bundleBytes); err != nil {
		return fmt.Errorf("metadata builder write: stage metadata.json: %w", err)
	}
	if err := writer(manifestStaged, manifestBytes); err != nil {
		return fmt.Errorf("metadata builder write: stage manifest.json: %w", err)
	}

	metaFinal := filepath.Join(runDir, "metadata.json")
	manifestFinal := filepath.Join(runDir, "manifest.json")

	// Snapshot prior versions for rollback on partial publish.
	metaExisted := fileExists(metaFinal)
	var metaBackup string
	if metaExisted {
		metaBackup = metaFinal + ".rollback"
		if err := os.Rename(metaFinal, metaBackup); err != nil {
			return fmt.Errorf("metadata builder write: backup metadata.json: %w", err)
		}
	}

	if err := os.Rename(metaStaged, metaFinal); err != nil {
		if metaBackup != "" {
			_ = os.Rename(metaBackup, metaFinal)
		}
		return fmt.Errorf("metadata builder write: publish metadata.json: %w", err)
	}
	if err := os.Rename(manifestStaged, manifestFinal); err != nil {
		// Pair atomicity: undo the metadata publish so neither file is left
		// behind in a half-published state.
		if metaBackup != "" {
			_ = os.Rename(metaBackup, metaFinal)
		} else {
			_ = os.Remove(metaFinal)
		}
		return fmt.Errorf("metadata builder write: publish manifest.json: %w", err)
	}
	if metaBackup != "" {
		_ = os.Remove(metaBackup)
	}

	b.cfg.Logger.Info("metadata files written",
		"run_id", runID,
		"dir", runDir,
	)

	return nil
}

// fileExists returns true when path resolves to an existing entry. Symlink
// hardening is out of scope for Story 11-5 (deferred to 9-2 W4).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
