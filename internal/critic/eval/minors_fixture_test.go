package eval

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestGoldenEvalManifest_ContainsMinorsKnownFail(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)
	m, err := loadManifest(root)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	found := false
	for _, pair := range m.Pairs {
		raw := testutil.LoadFixture(t, filepath.Join("golden", pair.NegativePath))
		var fixture Fixture
		if err := json.Unmarshal(raw, &fixture); err != nil {
			t.Fatalf("unmarshal fixture: %v", err)
		}
		if fixture.Category == "minors" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected at least one minors negative fixture")
	}
}

func TestGoldenEvalFixture_MinorsCategoryValidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	root := testutil.ProjectRoot(t)
	// D5 reshape relocated active fixtures under eval/v2/. The minors
	// category fixture stays at index 000003 — only the prefix changed.
	raw := testutil.LoadFixture(t, filepath.Join("golden", "eval", "v2", "000003", "negative.json"))
	fixture, err := ValidateFixture(root, raw)
	if err != nil {
		t.Fatalf("ValidateFixture: %v", err)
	}
	testutil.AssertEqual(t, fixture.Category, "minors")
}
