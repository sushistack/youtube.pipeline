package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

var testNow = time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)

// setupTestRoot creates a temp project root mirroring the necessary structure.
func setupTestRoot(t *testing.T) string {
	t.Helper()
	root := testutil.ProjectRoot(t)

	// Use a temp dir that mirrors testdata/golden/eval/ from the real root.
	tmp := t.TempDir()
	evalDir := filepath.Join(tmp, "testdata", "golden", "eval")
	if err := os.MkdirAll(evalDir, 0o755); err != nil {
		t.Fatalf("mkdir eval: %v", err)
	}
	contractsDir := filepath.Join(tmp, "testdata", "contracts")
	if err := os.MkdirAll(contractsDir, 0o755); err != nil {
		t.Fatalf("mkdir contracts: %v", err)
	}
	promptDir := filepath.Join(tmp, "docs", "prompts", "scenario")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt: %v", err)
	}

	// Copy schema files from real project root.
	for _, name := range []string{
		"golden_eval_fixture.schema.json",
		"writer_output.schema.json",
		"golden_eval_manifest.schema.json",
	} {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "contracts", name))
		if err != nil {
			t.Fatalf("read schema %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(contractsDir, name), data, 0o644); err != nil {
			t.Fatalf("write schema %s: %v", name, err)
		}
	}

	// Copy critic prompt.
	promptData, err := os.ReadFile(filepath.Join(root, "docs", "prompts", "scenario", "critic_agent.md"))
	if err != nil {
		t.Fatalf("read critic prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "critic_agent.md"), promptData, 0o644); err != nil {
		t.Fatalf("write critic prompt: %v", err)
	}

	// Seed manifest.
	m := Manifest{
		Version:         1,
		NextIndex:       1,
		LastRefreshedAt: testNow,
		Pairs:           []PairEntry{},
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

func writeTempFixture(t *testing.T, root string, kind string) string {
	t.Helper()
	realRoot := testutil.ProjectRoot(t)
	var samplePath string
	if kind == "positive" {
		samplePath = "contracts/golden_eval_fixture.sample.positive.json"
	} else {
		samplePath = "contracts/golden_eval_fixture.sample.negative.json"
	}
	data, err := os.ReadFile(filepath.Join(realRoot, "testdata", samplePath))
	if err != nil {
		t.Fatalf("read sample fixture: %v", err)
	}
	tmp := filepath.Join(t.TempDir(), kind+".json")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatalf("write temp fixture: %v", err)
	}
	return tmp
}

func TestAddPair_Happy_AssignsMonotonicIndex(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupTestRoot(t)
	posPath := writeTempFixture(t, root, "positive")
	negPath := writeTempFixture(t, root, "negative")

	meta, err := AddPair(root, posPath, negPath, testNow)
	if err != nil {
		t.Fatalf("AddPair: %v", err)
	}
	testutil.AssertEqual(t, 1, meta.Index)
	testutil.AssertEqual(t, "eval/000001/positive.json", meta.PositivePath)
	testutil.AssertEqual(t, "eval/000001/negative.json", meta.NegativePath)

	// Second add must get index 2.
	posPath2 := writeTempFixture(t, root, "positive")
	negPath2 := writeTempFixture(t, root, "negative")
	meta2, err := AddPair(root, posPath2, negPath2, testNow.Add(time.Hour))
	if err != nil {
		t.Fatalf("AddPair second: %v", err)
	}
	testutil.AssertEqual(t, 2, meta2.Index)

	// Verify files exist on disk.
	_, err = os.Stat(filepath.Join(root, "testdata", "golden", "eval", "000001", "positive.json"))
	if err != nil {
		t.Fatalf("positive file not found: %v", err)
	}
}

func TestAddPair_RejectsMissingPositive(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupTestRoot(t)
	negPath := writeTempFixture(t, root, "negative")
	_, err := AddPair(root, "", negPath, testNow)
	if err == nil {
		t.Fatal("expected error for missing positive path, got nil")
	}
}

func TestAddPair_RejectsMissingNegative(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupTestRoot(t)
	posPath := writeTempFixture(t, root, "positive")
	_, err := AddPair(root, posPath, "", testNow)
	if err == nil {
		t.Fatal("expected error for missing negative path, got nil")
	}
}

func TestValidateBalancedSet_MissingNegativeRejected(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	m := Manifest{
		Pairs: []PairEntry{
			{Index: 1, PositivePath: "eval/000001/positive.json", NegativePath: ""},
		},
	}
	err := ValidateBalancedSet(m)
	if err == nil {
		t.Fatal("expected error for missing negative path in pair, got nil")
	}
}

func TestListPairs_SortedByIndexAscending(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupTestRoot(t)

	for range [3]struct{}{} {
		posPath := writeTempFixture(t, root, "positive")
		negPath := writeTempFixture(t, root, "negative")
		if _, err := AddPair(root, posPath, negPath, testNow); err != nil {
			t.Fatalf("AddPair: %v", err)
		}
	}

	pairs, err := ListPairs(root)
	if err != nil {
		t.Fatalf("ListPairs: %v", err)
	}
	if len(pairs) != 3 {
		t.Fatalf("expected 3 pairs, got %d", len(pairs))
	}
	for i, p := range pairs {
		expected := i + 1
		if p.Index != expected {
			t.Errorf("pair[%d] index = %d, want %d", i, p.Index, expected)
		}
	}
}

func TestAddPair_CleansUpPairDirOnFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupTestRoot(t)

	// Pre-create the pair directory as a file so MkdirAll still succeeds but
	// the subsequent positive.json write will fail (cannot create file where
	// a non-directory exists at the target). The simpler failure path is a
	// read-only pair directory — create it, chmod 0500, trigger write fail.
	pairDir := filepath.Join(root, "testdata", "golden", "eval", "000001")
	if err := os.MkdirAll(pairDir, 0o755); err != nil {
		t.Fatalf("pre-mkdir: %v", err)
	}
	if err := os.Chmod(pairDir, 0o500); err != nil {
		t.Fatalf("chmod ro: %v", err)
	}
	defer os.Chmod(pairDir, 0o755) //nolint:errcheck // restore so t.TempDir cleanup works

	posPath := writeTempFixture(t, root, "positive")
	negPath := writeTempFixture(t, root, "negative")

	_, err := AddPair(root, posPath, negPath, testNow)
	if err == nil {
		t.Fatal("expected AddPair to fail when pair dir is read-only")
	}

	// The cleanup defer should have removed the pair directory entirely —
	// no orphan files should remain. Stat must return NotExist.
	if _, statErr := os.Stat(pairDir); !os.IsNotExist(statErr) {
		t.Errorf("expected pair directory removed on failure, still present (err=%v)", statErr)
	}

	// Manifest must not record a pair either.
	m, err := loadManifest(root)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if len(m.Pairs) != 0 {
		t.Errorf("expected 0 pairs after failed AddPair, got %d", len(m.Pairs))
	}
	if m.NextIndex != 1 {
		t.Errorf("expected NextIndex unchanged at 1, got %d", m.NextIndex)
	}
}

func TestValidateBalancedSetOnDisk_MissingFileRejected(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupTestRoot(t)
	m := Manifest{
		Pairs: []PairEntry{
			{Index: 1, PositivePath: "eval/000001/positive.json", NegativePath: "eval/000001/negative.json"},
		},
	}
	err := ValidateBalancedSetOnDisk(root, m)
	if err == nil {
		t.Fatal("expected error when pair files are missing on disk")
	}
}

// TestAddPair_V2ManifestRoutesUnderEvalV2 covers the v2 routing decision
// in activePairSubdir(). Pre-D5 setupTestRoot seeds a Version=1 manifest
// (legacy flat path), so without this test the v2-only code path was
// effectively uncovered. We bump the seeded manifest to Version=2 and
// assert that AddPair writes under eval/v2/000001/.
func TestAddPair_V2ManifestRoutesUnderEvalV2(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupTestRoot(t)

	// Bump the seeded manifest from Version=1 to Version=2.
	m, err := loadManifest(root)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	m.Version = ManifestVersionV2
	if err := saveManifest(root, m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	posPath := writeTempFixture(t, root, "positive")
	negPath := writeTempFixture(t, root, "negative")
	meta, err := AddPair(root, posPath, negPath, testNow)
	if err != nil {
		t.Fatalf("AddPair: %v", err)
	}
	testutil.AssertEqual(t, "eval/v2/000001/positive.json", meta.PositivePath)
	testutil.AssertEqual(t, "eval/v2/000001/negative.json", meta.NegativePath)

	// File must exist on disk under the v2 prefix.
	posAbs := filepath.Join(root, "testdata", "golden", "eval", "v2", "000001", "positive.json")
	if _, err := os.Stat(posAbs); err != nil {
		t.Fatalf("expected file at %s: %v", posAbs, err)
	}
}

// TestLoadManifest_RejectsUnknownVersion guards against a hand-edited
// manifest with a far-future or negative version silently routing through
// legacy handling.
func TestLoadManifest_RejectsUnknownVersion(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupTestRoot(t)

	// Hand-edit the manifest to an unknown version.
	manifestRaw, err := os.ReadFile(filepath.Join(root, "testdata", "golden", "eval", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(manifestRaw, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	raw["version"] = 99
	corrupted, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "testdata", "golden", "eval", "manifest.json"), corrupted, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := loadManifest(root); err == nil {
		t.Fatal("expected error loading manifest with unknown version")
	}
}

func TestAddPair_ManifestIndentation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := setupTestRoot(t)
	posPath := writeTempFixture(t, root, "positive")
	negPath := writeTempFixture(t, root, "negative")

	if _, err := AddPair(root, posPath, negPath, testNow); err != nil {
		t.Fatalf("AddPair: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "testdata", "golden", "eval", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	// Verify valid JSON and ends with newline.
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest JSON invalid: %v", err)
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Error("manifest does not end with newline")
	}
}
