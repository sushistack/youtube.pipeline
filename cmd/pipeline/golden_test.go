package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/critic/eval"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// setupGoldenRoot prepares a temp project root suitable for golden CLI tests.
func setupGoldenRoot(t *testing.T) string {
	t.Helper()
	realRoot := testutil.ProjectRoot(t)
	tmp := t.TempDir()

	// Mirror required directories.
	for _, rel := range []string{
		"testdata/contracts",
		"testdata/golden/eval",
		"docs/prompts/scenario",
	} {
		if err := os.MkdirAll(filepath.Join(tmp, rel), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}

	// Copy schemas.
	for _, name := range []string{
		"golden_eval_fixture.schema.json",
		"writer_output.schema.json",
		"golden_eval_manifest.schema.json",
	} {
		data, err := os.ReadFile(filepath.Join(realRoot, "testdata", "contracts", name))
		if err != nil {
			t.Fatalf("read schema %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(tmp, "testdata", "contracts", name), data, 0o644); err != nil {
			t.Fatalf("write schema: %v", err)
		}
	}

	// Copy critic prompt.
	promptData, err := os.ReadFile(filepath.Join(realRoot, "docs", "prompts", "scenario", "critic_agent.md"))
	if err != nil {
		t.Fatalf("read critic prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "docs", "prompts", "scenario", "critic_agent.md"), promptData, 0o644); err != nil {
		t.Fatalf("write critic prompt: %v", err)
	}

	// Seed an empty manifest.
	type emptyManifest struct {
		Version         int             `json:"version"`
		NextIndex       int             `json:"next_index"`
		LastRefreshedAt string          `json:"last_refreshed_at"`
		Pairs           []eval.PairEntry `json:"pairs"`
	}
	m := emptyManifest{
		Version:         1,
		NextIndex:       1,
		LastRefreshedAt: time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Pairs:           []eval.PairEntry{},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(tmp, "testdata", "golden", "eval", "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	return tmp
}

func copyFixtureTo(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", dst, err)
	}
}

func TestGoldenAddCmd_Human(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupGoldenRoot(t)
	realRoot := testutil.ProjectRoot(t)

	posPath := filepath.Join(t.TempDir(), "positive.json")
	negPath := filepath.Join(t.TempDir(), "negative.json")
	copyFixtureTo(t, filepath.Join(realRoot, "testdata", "contracts", "golden_eval_fixture.sample.positive.json"), posPath)
	copyFixtureTo(t, filepath.Join(realRoot, "testdata", "contracts", "golden_eval_fixture.sample.negative.json"), negPath)

	prevRoot, prevJSON := goldenProjectRoot, jsonOutput
	goldenProjectRoot, jsonOutput = root, false
	defer func() { goldenProjectRoot, jsonOutput = prevRoot, prevJSON }()

	cmd := newGoldenCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"add", "--positive", posPath, "--negative", negPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("golden add: %v\noutput: %s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "Golden pair added") {
		t.Errorf("expected 'Golden pair added' in output, got: %s", out)
	}
	if !strings.Contains(out, "Index:") {
		t.Errorf("expected 'Index:' in output, got: %s", out)
	}
}

func TestGoldenAddCmd_JSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupGoldenRoot(t)
	realRoot := testutil.ProjectRoot(t)

	posPath := filepath.Join(t.TempDir(), "positive.json")
	negPath := filepath.Join(t.TempDir(), "negative.json")
	copyFixtureTo(t, filepath.Join(realRoot, "testdata", "contracts", "golden_eval_fixture.sample.positive.json"), posPath)
	copyFixtureTo(t, filepath.Join(realRoot, "testdata", "contracts", "golden_eval_fixture.sample.negative.json"), negPath)

	prevRoot, prevJSON := goldenProjectRoot, jsonOutput
	goldenProjectRoot, jsonOutput = root, true
	defer func() { goldenProjectRoot, jsonOutput = prevRoot, prevJSON }()

	cmd := newGoldenCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"add", "--positive", posPath, "--negative", negPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("golden add JSON: %v\noutput: %s", err, buf.String())
	}

	var env struct {
		Version int `json:"version"`
		Data    struct {
			Index        int    `json:"index"`
			CreatedAt    string `json:"created_at"`
			PositivePath string `json:"positive_path"`
			NegativePath string `json:"negative_path"`
			PairCount    int    `json:"pair_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal JSON output: %v\nraw: %s", err, buf.String())
	}
	testutil.AssertEqual(t, 1, env.Version)
	testutil.AssertEqual(t, 1, env.Data.Index)
	testutil.AssertEqual(t, 1, env.Data.PairCount)
	if env.Data.PositivePath == "" {
		t.Error("expected non-empty positive_path in JSON output")
	}
}

func TestGoldenAddCmd_RejectsSchemaViolation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupGoldenRoot(t)

	// Write a fixture that is clearly invalid JSON schema.
	badFixture := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badFixture, []byte(`{"fixture_id":"x","kind":"positive","checkpoint":"post_writer","input":{},"expected_verdict":"pass","category":"test"}`), 0o644); err != nil {
		t.Fatalf("write bad fixture: %v", err)
	}
	validNeg := filepath.Join(t.TempDir(), "negative.json")
	realRoot := testutil.ProjectRoot(t)
	copyFixtureTo(t, filepath.Join(realRoot, "testdata", "contracts", "golden_eval_fixture.sample.negative.json"), validNeg)

	prevRoot, prevJSON := goldenProjectRoot, jsonOutput
	goldenProjectRoot, jsonOutput = root, false
	defer func() { goldenProjectRoot, jsonOutput = prevRoot, prevJSON }()

	cmd := newGoldenCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"add", "--positive", badFixture, "--negative", validNeg})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for schema violation, got nil")
	}
}

func TestGoldenListCmd_Human(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupGoldenRoot(t)
	realRoot := testutil.ProjectRoot(t)

	// Add a pair first.
	posPath := filepath.Join(t.TempDir(), "positive.json")
	negPath := filepath.Join(t.TempDir(), "negative.json")
	copyFixtureTo(t, filepath.Join(realRoot, "testdata", "contracts", "golden_eval_fixture.sample.positive.json"), posPath)
	copyFixtureTo(t, filepath.Join(realRoot, "testdata", "contracts", "golden_eval_fixture.sample.negative.json"), negPath)
	if _, err := eval.AddPair(root, posPath, negPath, time.Now().UTC()); err != nil {
		t.Fatalf("AddPair setup: %v", err)
	}

	prevRoot, prevJSON := goldenProjectRoot, jsonOutput
	goldenProjectRoot, jsonOutput = root, false
	defer func() { goldenProjectRoot, jsonOutput = prevRoot, prevJSON }()

	cmd := newGoldenCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("golden list: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[1]") {
		t.Errorf("expected '[1]' in list output, got: %s", out)
	}
	if !strings.Contains(out, "1 pair(s) total") {
		t.Errorf("expected pair count in output, got: %s", out)
	}
}

func TestGoldenListCmd_JSON(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupGoldenRoot(t)
	realRoot := testutil.ProjectRoot(t)

	posPath := filepath.Join(t.TempDir(), "positive.json")
	negPath := filepath.Join(t.TempDir(), "negative.json")
	copyFixtureTo(t, filepath.Join(realRoot, "testdata", "contracts", "golden_eval_fixture.sample.positive.json"), posPath)
	copyFixtureTo(t, filepath.Join(realRoot, "testdata", "contracts", "golden_eval_fixture.sample.negative.json"), negPath)
	if _, err := eval.AddPair(root, posPath, negPath, time.Now().UTC()); err != nil {
		t.Fatalf("AddPair setup: %v", err)
	}

	prevRoot, prevJSON := goldenProjectRoot, jsonOutput
	goldenProjectRoot, jsonOutput = root, true
	defer func() { goldenProjectRoot, jsonOutput = prevRoot, prevJSON }()

	cmd := newGoldenCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("golden list JSON: %v", err)
	}

	var env struct {
		Version int `json:"version"`
		Data    struct {
			Pairs []struct {
				Index        int    `json:"index"`
				CreatedAt    string `json:"created_at"`
				PositivePath string `json:"positive_path"`
				NegativePath string `json:"negative_path"`
			} `json:"pairs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal JSON: %v\nraw: %s", err, buf.String())
	}
	testutil.AssertEqual(t, 1, env.Version)
	if len(env.Data.Pairs) != 1 {
		t.Fatalf("expected 1 pair in JSON output, got %d", len(env.Data.Pairs))
	}
	testutil.AssertEqual(t, 1, env.Data.Pairs[0].Index)
}
