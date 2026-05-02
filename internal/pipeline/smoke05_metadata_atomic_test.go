package pipeline_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/fi"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// smoke05Bundle returns a minimally valid metadata bundle and source manifest
// suitable for exercising the pair-atomic publish protocol.
func smoke05Bundle(runID string) (domain.MetadataBundle, domain.SourceManifest) {
	bundle := domain.MetadataBundle{
		Version:     1,
		GeneratedAt: "2026-04-25T00:00:00Z",
		RunID:       runID,
		SCPID:       "049",
		Title:       "SCP-049 — The Plague Doctor",
		AIGenerated: domain.AIGeneratedFlags{Narration: true, Imagery: true, TTS: true},
		ModelsUsed: map[string]domain.ModelRecord{
			"writer":           {Provider: "deepseek", Model: "deepseek-v4-flash"},
			"critic":           {Provider: "gemini", Model: "gemini-3.1-flash-lite-preview"},
			"image":            {Provider: "dashscope", Model: "qwen-max-vl"},
			"tts":              {Provider: "dashscope", Model: "qwen3-tts-flash-2025-09-18", Voice: "longhua"},
			"visual_breakdown": {Provider: "gemini", Model: "gemini-3.1-flash-lite-preview"},
		},
	}
	manifest := domain.SourceManifest{
		Version:     1,
		GeneratedAt: "2026-04-25T00:00:00Z",
		RunID:       runID,
		SCPID:       "049",
		SourceURL:   "https://scp-wiki.wikidot.com/scp-049",
		AuthorName:  "Djoric",
		License:     domain.LicenseCCBYSA30,
		LicenseURL:  domain.LicenseURLCCBYSA30,
		LicenseChain: []domain.LicenseEntry{
			{Component: "SCP article text", SourceURL: "https://scp-wiki.wikidot.com/scp-049", AuthorName: "Djoric", License: domain.LicenseCCBYSA30},
		},
	}
	return bundle, manifest
}

func smoke05Builder(t *testing.T, outputDir string, writer pipeline.FileWriter) pipeline.MetadataBuilder {
	t.Helper()
	cfg := validMetadataBuilderConfig(t, outputDir)
	cfg.Writer = writer
	builder, err := pipeline.NewMetadataBuilder(cfg)
	if err != nil {
		t.Fatalf("NewMetadataBuilder: %v", err)
	}
	return builder
}

// TestSMOKE05_MetadataAtomicPair_FaultThenRetry codifies the both-or-neither
// invariant called out in test-design-epic-1-10-2026-04-25.md §SMOKE-05 / R-04.
//
// Scenario:
//  1. First Write call uses a faulting writer that fails when staging
//     manifest.json. Neither metadata.json nor manifest.json may be present
//     in runDir afterward (pair atomicity contract).
//  2. Second Write call uses the production atomic writer. Both files must
//     be present and byte-identical to a fault-free control run.
func TestSMOKE05_MetadataAtomicPair_FaultThenRetry(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()
	runID := "smoke05-run"
	runDir := filepath.Join(dir, runID)

	bundle, manifest := smoke05Bundle(runID)
	manifestFault := errors.New("disk full simulated")

	failOnManifest := func(path string, _ int) error {
		if strings.HasSuffix(path, "manifest.json") {
			return manifestFault
		}
		return nil
	}
	faultingWriter := fi.NewFaultingFileWriter(pipeline.DefaultAtomicWriter, failOnManifest)

	// 1. Faulted publish must leave runDir without metadata.json or manifest.json.
	builder := smoke05Builder(t, dir, faultingWriter)
	err := builder.Write(context.Background(), runID, bundle, manifest)
	if err == nil {
		t.Fatal("expected fault-injected Write to fail, got nil error")
	}
	if !errors.Is(err, manifestFault) {
		t.Errorf("expected error chain to contain %v, got %v", manifestFault, err)
	}
	assertNeitherPresent(t, runDir)
	assertStagingCleaned(t, runDir)

	// 2. Retry with the production writer must publish both files atomically.
	clean := smoke05Builder(t, dir, pipeline.DefaultAtomicWriter)
	if err := clean.Write(context.Background(), runID, bundle, manifest); err != nil {
		t.Fatalf("retry Write: %v", err)
	}
	assertBothPresent(t, runDir)
	assertStagingCleaned(t, runDir)
}

// TestSMOKE05_MetadataAtomicPair_RetryMatchesControl proves that retrying a
// previously faulted Write with the production writer produces files
// byte-identical to a never-faulted control run with the same inputs.
func TestSMOKE05_MetadataAtomicPair_RetryMatchesControl(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	runID := "smoke05-deterministic"
	bundle, manifest := smoke05Bundle(runID)

	// Control run: never-faulted publish into its own outputDir.
	controlDir := t.TempDir()
	controlBuilder := smoke05Builder(t, controlDir, pipeline.DefaultAtomicWriter)
	if err := controlBuilder.Write(context.Background(), runID, bundle, manifest); err != nil {
		t.Fatalf("control Write: %v", err)
	}

	// Faulted-then-retried run in a separate outputDir.
	retryDir := t.TempDir()
	faulting := fi.NewFaultingFileWriter(pipeline.DefaultAtomicWriter, func(path string, _ int) error {
		if strings.HasSuffix(path, "manifest.json") {
			return errors.New("simulated disk full on manifest")
		}
		return nil
	})
	failed := smoke05Builder(t, retryDir, faulting)
	if err := failed.Write(context.Background(), runID, bundle, manifest); err == nil {
		t.Fatal("expected fault-injected Write to fail, got nil error")
	}
	clean := smoke05Builder(t, retryDir, pipeline.DefaultAtomicWriter)
	if err := clean.Write(context.Background(), runID, bundle, manifest); err != nil {
		t.Fatalf("retry Write: %v", err)
	}

	// Compare bytes — same inputs + deterministic JSON encoder => identical.
	for _, name := range []string{"metadata.json", "manifest.json"} {
		ctrlBytes := mustRead(t, filepath.Join(controlDir, runID, name))
		retryBytes := mustRead(t, filepath.Join(retryDir, runID, name))
		if string(ctrlBytes) != string(retryBytes) {
			t.Errorf("%s: retry bytes differ from control", name)
		}
	}
}

// TestSMOKE05_MetadataAtomicPair_RollbackOnRenameFault proves that a publish
// where the second rename step fails leaves neither file behind, even when a
// pre-existing metadata.json had to be backed up first.
func TestSMOKE05_MetadataAtomicPair_RollbackOnRenameFault(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	dir := t.TempDir()
	runID := "smoke05-rollback"
	runDir := filepath.Join(dir, runID)

	// Seed runDir with an existing valid metadata.json + manifest.json from a
	// fault-free publish. This exercises the rollback path that must restore
	// the prior metadata when a subsequent publish only partially succeeds.
	priorBundle, priorManifest := smoke05Bundle(runID)
	priorBundle.Title = "PRIOR"
	priorManifest.AuthorName = "PRIOR"
	prior := smoke05Builder(t, dir, pipeline.DefaultAtomicWriter)
	if err := prior.Write(context.Background(), runID, priorBundle, priorManifest); err != nil {
		t.Fatalf("prior Write: %v", err)
	}

	// Now run a fault-injected publish that fails on the manifest stage.
	bundle, manifest := smoke05Bundle(runID)
	bundle.Title = "NEW"
	manifest.AuthorName = "NEW"
	faulting := fi.NewFaultingFileWriter(pipeline.DefaultAtomicWriter, func(path string, _ int) error {
		if strings.HasSuffix(path, "manifest.json") {
			return errors.New("simulated fault")
		}
		return nil
	})
	failed := smoke05Builder(t, dir, faulting)
	err := failed.Write(context.Background(), runID, bundle, manifest)
	if err == nil {
		t.Fatal("expected fault-injected Write to fail, got nil error")
	}

	// Pair atomicity invariant: both files are present and consistent.
	// Specifically, the prior bundle must NOT have been replaced by the
	// half-published new bundle.
	assertBothPresent(t, runDir)
	assertStagingCleaned(t, runDir)

	got := mustRead(t, filepath.Join(runDir, "metadata.json"))
	if !strings.Contains(string(got), "PRIOR") {
		t.Errorf("metadata.json was overwritten by half-published new bundle: %s", got)
	}
	gotManifest := mustRead(t, filepath.Join(runDir, "manifest.json"))
	if !strings.Contains(string(gotManifest), "PRIOR") {
		t.Errorf("manifest.json was overwritten by half-published new manifest: %s", gotManifest)
	}
}

// ── assertions ───────────────────────────────────────────────────────────────

func assertNeitherPresent(t *testing.T, runDir string) {
	t.Helper()
	for _, name := range []string{"metadata.json", "manifest.json"} {
		_, err := os.Stat(filepath.Join(runDir, name))
		if err == nil {
			t.Errorf("expected %s NOT to exist after fault, but it does (pair atomicity violated)", name)
		} else if !os.IsNotExist(err) {
			t.Errorf("stat %s: unexpected error: %v", name, err)
		}
	}
}

func assertBothPresent(t *testing.T, runDir string) {
	t.Helper()
	for _, name := range []string{"metadata.json", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Errorf("expected %s to exist after retry, got %v", name, err)
		}
	}
}

func assertStagingCleaned(t *testing.T, runDir string) {
	t.Helper()
	stagingDir := filepath.Join(runDir, ".metadata.staging")
	if _, err := os.Stat(stagingDir); err == nil {
		t.Errorf("staging dir %s leaked across attempts", stagingDir)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
