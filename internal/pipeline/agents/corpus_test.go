package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestFilesystemCorpus_Read_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := t.TempDir()
	writeCorpusTree(t, root, "SCP-173")

	doc, err := NewFilesystemCorpus(root).Read(context.Background(), "SCP-173")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	testutil.AssertEqual(t, doc.Facts.SCPID, "SCP-173")
	testutil.AssertEqual(t, doc.Meta.SCPID, "SCP-173")
	testutil.AssertEqual(t, doc.MainText, "Main text.")
}

func TestFilesystemCorpus_Read_MissingSCP(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	_, err := NewFilesystemCorpus(t.TempDir()).Read(context.Background(), "SCP-404")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if !errors.Is(err, ErrCorpusNotFound) {
		t.Fatalf("expected ErrCorpusNotFound, got %v", err)
	}
}

func TestFilesystemCorpus_Read_MissingMainText(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := t.TempDir()
	writeCorpusJSON(t, filepath.Join(root, "SCP-173", "facts.json"), validFactsJSON("SCP-173"))
	writeCorpusJSON(t, filepath.Join(root, "SCP-173", "meta.json"), validMetaJSON("SCP-173"))

	_, err := NewFilesystemCorpus(root).Read(context.Background(), "SCP-173")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestFilesystemCorpus_Read_MalformedFacts(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "SCP-173", "facts.json"), []byte("{"))
	writeCorpusJSON(t, filepath.Join(root, "SCP-173", "meta.json"), validMetaJSON("SCP-173"))
	writeFile(t, filepath.Join(root, "SCP-173", "main.txt"), []byte("Main text."))

	_, err := NewFilesystemCorpus(root).Read(context.Background(), "SCP-173")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestFilesystemCorpus_Read_InvalidUTF8Main(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := t.TempDir()
	writeCorpusTree(t, root, "SCP-173")
	writeFile(t, filepath.Join(root, "SCP-173", "main.txt"), []byte{0xff, 0xfe})

	_, err := NewFilesystemCorpus(root).Read(context.Background(), "SCP-173")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestFilesystemCorpus_Read_CaseSensitive(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := t.TempDir()
	if !isCaseSensitiveFS(t, root) {
		t.Skip("filesystem is case-insensitive; case-sensitivity guarantee N/A here")
	}
	writeCorpusTree(t, root, "SCP-173")

	_, err := NewFilesystemCorpus(root).Read(context.Background(), "scp-173")
	if !errors.Is(err, ErrCorpusNotFound) {
		t.Fatalf("expected ErrCorpusNotFound, got %v", err)
	}
}

func TestFilesystemCorpus_Read_PathTraversal(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := t.TempDir()
	writeCorpusTree(t, root, "SCP-173")

	cases := []string{"", "../SCP-173", "SCP-173/..", "..", "/etc/passwd", `SCP\173`}
	for _, id := range cases {
		_, err := NewFilesystemCorpus(root).Read(context.Background(), id)
		if !errors.Is(err, ErrCorpusNotFound) {
			t.Fatalf("id=%q: expected ErrCorpusNotFound, got %v", id, err)
		}
	}
}

func isCaseSensitiveFS(t *testing.T, root string) bool {
	t.Helper()
	probe := filepath.Join(root, ".case-probe")
	if err := os.WriteFile(probe, nil, 0o644); err != nil {
		t.Fatalf("probe write: %v", err)
	}
	defer os.Remove(probe)
	_, err := os.Stat(filepath.Join(root, ".CASE-PROBE"))
	return err != nil
}

func writeCorpusTree(t *testing.T, root, scpID string) {
	t.Helper()
	writeCorpusJSON(t, filepath.Join(root, scpID, "facts.json"), validFactsJSON(scpID))
	writeCorpusJSON(t, filepath.Join(root, scpID, "meta.json"), validMetaJSON(scpID))
	writeFile(t, filepath.Join(root, scpID, "main.txt"), []byte("Main text."))
}

func writeCorpusJSON(t *testing.T, path, body string) {
	t.Helper()
	writeFile(t, path, []byte(body))
}

func writeFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func validFactsJSON(scpID string) string {
	return `{
  "scp_id": "` + scpID + `",
  "title": "` + scpID + `",
  "object_class": "Euclid",
  "rating": 1,
  "physical_description": "Stone",
  "anomalous_properties": ["Moves"],
  "containment_procedures": "Watch it",
  "behavior_and_nature": "Hostile",
  "origin_and_discovery": "Unknown",
  "incidents": [],
  "related_documents": [],
  "visual_elements": {
    "appearance": "Stone statue",
    "distinguishing_features": ["Cracks"],
    "environment_setting": "Cell",
    "key_visual_moments": ["It waits"]
  },
  "cross_references": [],
  "tags": ["scp"]
}`
}

func TestFilesystemCorpus_Read_SCPMetaAttribution(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := t.TempDir()
	writeCorpusTree(t, root, "SCP-173")

	doc, err := NewFilesystemCorpus(root).Read(context.Background(), "SCP-173")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if doc.Meta.AuthorName != "Test Author" {
		t.Errorf("AuthorName = %q, want %q", doc.Meta.AuthorName, "Test Author")
	}
	if doc.Meta.SourceURL != "https://scp-wiki.wikidot.com/SCP-173" {
		t.Errorf("SourceURL = %q, want %q", doc.Meta.SourceURL, "https://scp-wiki.wikidot.com/SCP-173")
	}
}

func TestFilesystemCorpus_Read_SCPMetaAttributionOmitted(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := t.TempDir()
	// Write meta.json WITHOUT author_name/source_url.
	writeCorpusJSON(t, filepath.Join(root, "SCP-404", "facts.json"), validFactsJSON("SCP-404"))
	writeCorpusJSON(t, filepath.Join(root, "SCP-404", "meta.json"), `{
  "scp_id": "SCP-404",
  "tags": ["scp"],
  "related_docs": []
}`)
	writeFile(t, filepath.Join(root, "SCP-404", "main.txt"), []byte("Main text."))

	doc, err := NewFilesystemCorpus(root).Read(context.Background(), "SCP-404")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if doc.Meta.AuthorName != "" {
		t.Errorf("AuthorName = %q, want empty string", doc.Meta.AuthorName)
	}
	if doc.Meta.SourceURL != "" {
		t.Errorf("SourceURL = %q, want empty string", doc.Meta.SourceURL)
	}
}

func validMetaJSON(scpID string) string {
	return `{
  "scp_id": "` + scpID + `",
  "tags": ["scp"],
  "related_docs": [],
  "author_name": "Test Author",
  "source_url": "https://scp-wiki.wikidot.com/` + scpID + `"
}`
}
