package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestCurrentCriticPromptHash_StableForSameBytes(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)
	h1, err := CurrentCriticPromptHash(root)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	h2, err := CurrentCriticPromptHash(root)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	testutil.AssertEqual(t, h1, h2)
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex, got %d chars: %s", len(h1), h1)
	}
}

func TestCurrentCriticPromptHash_ChangesWhenPromptBytesChange(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	promptDir := filepath.Join(tmp, "docs", "prompts", "scenario")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	promptPath := filepath.Join(promptDir, "critic_agent.md")

	if err := os.WriteFile(promptPath, []byte("version one"), 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	h1, err := CurrentCriticPromptHash(tmp)
	if err != nil {
		t.Fatalf("hash v1: %v", err)
	}

	if err := os.WriteFile(promptPath, []byte("version two"), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	h2, err := CurrentCriticPromptHash(tmp)
	if err != nil {
		t.Fatalf("hash v2: %v", err)
	}

	if h1 == h2 {
		t.Error("expected hashes to differ after prompt content change")
	}
}

func TestEvaluateFreshness_TimeThresholdWarning(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupTestRootWithManifest(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "")
	now := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC) // 107 days later
	status, err := EvaluateFreshness(root, now, 30)
	if err != nil {
		t.Fatalf("EvaluateFreshness: %v", err)
	}
	found := false
	for _, w := range status.Warnings {
		if strings.HasPrefix(w, "Staleness Warning:") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Staleness Warning: prefix in warnings, got %v", status.Warnings)
	}
	if status.DaysSinceRefresh < 30 {
		t.Errorf("expected DaysSinceRefresh >= 30, got %d", status.DaysSinceRefresh)
	}
}

func TestEvaluateFreshness_PromptHashChangedWarning(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupTestRootWithManifest(t, testNow, "old-hash-that-does-not-match")
	now := testNow.Add(time.Hour) // within threshold — only prompt warning
	status, err := EvaluateFreshness(root, now, 30)
	if err != nil {
		t.Fatalf("EvaluateFreshness: %v", err)
	}
	found := false
	for _, w := range status.Warnings {
		if strings.Contains(w, "Critic prompt changed") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected prompt-change warning, got %v", status.Warnings)
	}
	if !status.PromptHashChanged {
		t.Error("expected PromptHashChanged=true")
	}
}

func TestEvaluateFreshness_NoWarningWhenFresh(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)
	// Use the real root — compute current hash so it won't report prompt change.
	currentHash, err := CurrentCriticPromptHash(root)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	tmp := setupTestRootWithManifest(t, testNow, currentHash)
	now := testNow.Add(time.Hour) // 0 days — within any threshold
	status, err := EvaluateFreshness(tmp, now, 30)
	if err != nil {
		t.Fatalf("EvaluateFreshness: %v", err)
	}
	if len(status.Warnings) > 0 {
		t.Errorf("expected no warnings for fresh set, got %v", status.Warnings)
	}
}

// setupTestRootWithManifest builds a minimal project root with a manifest
// whose last_refreshed_at and last_successful_prompt_hash are set as given.
func setupTestRootWithManifest(t *testing.T, refreshedAt time.Time, promptHash string) string {
	t.Helper()
	realRoot := testutil.ProjectRoot(t)
	tmp := t.TempDir()

	// Copy schemas and critic prompt.
	for _, rel := range []string{
		"testdata/contracts/golden_eval_fixture.schema.json",
		"testdata/contracts/writer_output.schema.json",
		"testdata/contracts/golden_eval_manifest.schema.json",
	} {
		data, err := os.ReadFile(filepath.Join(realRoot, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		dst := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	promptData, err := os.ReadFile(filepath.Join(realRoot, criticPromptRelPath))
	if err != nil {
		t.Fatalf("read critic prompt: %v", err)
	}
	promptDst := filepath.Join(tmp, criticPromptRelPath)
	if err := os.MkdirAll(filepath.Dir(promptDst), 0o755); err != nil {
		t.Fatalf("mkdir prompt: %v", err)
	}
	if err := os.WriteFile(promptDst, promptData, 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	evalDir := filepath.Join(tmp, "testdata", "golden", "eval")
	if err := os.MkdirAll(evalDir, 0o755); err != nil {
		t.Fatalf("mkdir eval: %v", err)
	}
	m := Manifest{
		Version:                  1,
		NextIndex:                1,
		LastRefreshedAt:          refreshedAt.UTC(),
		LastSuccessfulPromptHash: promptHash,
		Pairs:                    []PairEntry{},
	}
	data, err := marshalIndented(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(evalDir, "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return tmp
}
