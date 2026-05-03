package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestLoadPromptAssets_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	assets, err := LoadPromptAssets(testutil.ProjectRoot(t), false)
	if err != nil {
		t.Fatalf("LoadPromptAssets: %v", err)
	}
	if assets.WriterTemplate == "" || assets.CriticTemplate == "" || assets.VisualBreakdownTemplate == "" || assets.ReviewerTemplate == "" || assets.RoleClassifierTemplate == "" || assets.FormatGuide == "" {
		t.Fatalf("expected all assets loaded, got %#v", assets)
	}
	for _, actID := range domain.ActOrder {
		if assets.ExemplarsByAct[actID] == "" {
			t.Fatalf("ExemplarsByAct[%s] is empty after LoadPromptAssets", actID)
		}
	}
	if containsFold(assets.VisualBreakdownTemplate, "1:1 sentence-to-image mapping") {
		t.Fatal("visual breakdown template still contains stale sentence-to-image mapping rule")
	}
	if containsFold(assets.VisualBreakdownTemplate, "Total shot count == {sentence_count}") {
		t.Fatal("visual breakdown template still contains stale sentence-count authority")
	}
	if strings.Contains(assets.VisualBreakdownTemplate, "{sentence_count}") {
		t.Fatal("visual breakdown template still references {sentence_count} placeholder")
	}
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func TestLoadPromptAssets_MissingFile(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "prompts", "scenario"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := LoadPromptAssets(root, false); err == nil {
		t.Fatal("expected error for missing files")
	}
}
