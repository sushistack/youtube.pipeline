package eval

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestLoadV1ArchiveReport_RealRoot_RoundTripsByteIdentically(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)

	report, err := LoadV1ArchiveReport(root)
	if err != nil {
		t.Fatalf("LoadV1ArchiveReport: %v", err)
	}
	// The frozen v1 last_report.json snapshot from the pre-D5 run was
	// captured with recall=1.0 (3/3 negatives detected, zero false rejects).
	// Reproducibility check: the archive must round-trip those exact values.
	if report.Recall != 1.0 {
		t.Errorf("v1 archive recall = %v, want 1.0", report.Recall)
	}
	testutil.AssertEqual(t, 3, report.TotalNegative)
	testutil.AssertEqual(t, 3, report.DetectedNegative)
	testutil.AssertEqual(t, 0, report.FalseRejects)
	if len(report.Pairs) != 3 {
		t.Fatalf("expected 3 pair results, got %d", len(report.Pairs))
	}
	for _, p := range report.Pairs {
		testutil.AssertEqual(t, "retry", p.NegVerdict)
		testutil.AssertEqual(t, "pass", p.PosVerdict)
	}
}

func TestLoadV1ArchiveReport_StableAcrossCalls(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)

	first, err := LoadV1ArchiveReport(root)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	second, err := LoadV1ArchiveReport(root)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}

	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Errorf("v1 archive report should be stable across calls\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
}

func TestLoadV1ArchiveReport_RejectsMissing(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	_, err := LoadV1ArchiveReport(tmp)
	if err == nil {
		t.Fatal("expected error when v1 archive is missing")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestLoadV1ArchiveReport_RejectsMalformed(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "testdata", "golden", "eval", "v1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last_report.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	_, err := LoadV1ArchiveReport(tmp)
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestLoadV1ArchiveReport_RejectsEmptySnapshot(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "testdata", "golden", "eval", "v1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// `{}` parses cleanly into a zero-valued Report; without the empty-snapshot
	// guard, callers would log it as "v1 had recall=0 with zero pairs", and
	// the v1→v2 delta becomes a fabricated +1.00 win.
	if err := os.WriteFile(filepath.Join(dir, "last_report.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	_, err := LoadV1ArchiveReport(tmp)
	if err == nil {
		t.Fatal("expected error on empty snapshot")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestLoadV1ArchiveManifest_RealRoot(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)

	m, err := LoadV1ArchiveManifest(root)
	if err != nil {
		t.Fatalf("LoadV1ArchiveManifest: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("v1 archive manifest version = %d, want 1", m.Version)
	}
	if len(m.Pairs) != 3 {
		t.Errorf("v1 archive manifest pair count = %d, want 3", len(m.Pairs))
	}
}
