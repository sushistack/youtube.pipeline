package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestForbiddenTerms_LoadAndMatch(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := policyRoot(t, "# comment\n\nwiki\n금칙어\n")
	terms, err := LoadForbiddenTerms(root)
	if err != nil {
		t.Fatalf("LoadForbiddenTerms: %v", err)
	}
	if len(terms.Raw) != 2 {
		t.Fatalf("got %d terms, want 2", len(terms.Raw))
	}
	hits := terms.MatchNarration(&domain.NarrationScript{
		Scenes: []domain.NarrationScene{
			{SceneNum: 2, Narration: "이건 wiki 문체입니다."},
			{SceneNum: 4, Narration: "여기 금칙어 표현이 있습니다."},
		},
	})
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	testutil.AssertEqual(t, hits[0].SceneNum, 2)
	testutil.AssertEqual(t, hits[1].SceneNum, 4)
}

func TestForbiddenTerms_InvalidRegexFailure(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := policyRoot(t, "[\n")
	_, err := LoadForbiddenTerms(root)
	if err == nil || !strings.Contains(err.Error(), domain.ErrValidation.Error()) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestForbiddenTerms_VersionStable(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := policyRoot(t, "wiki\n")
	first, err := LoadForbiddenTerms(root)
	if err != nil {
		t.Fatalf("LoadForbiddenTerms #1: %v", err)
	}
	second, err := LoadForbiddenTerms(root)
	if err != nil {
		t.Fatalf("LoadForbiddenTerms #2: %v", err)
	}
	testutil.AssertEqual(t, first.Version, second.Version)
}

func TestMatchNarration_ScansAllTextFields(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := policyRoot(t, "wiki\n금칙어\n")
	terms, err := LoadForbiddenTerms(root)
	if err != nil {
		t.Fatalf("LoadForbiddenTerms: %v", err)
	}

	t.Run("title_only", func(t *testing.T) {
		hits := terms.MatchNarration(&domain.NarrationScript{
			Title: "The wiki Incident",
			Scenes: []domain.NarrationScene{
				{SceneNum: 1, Narration: "깨끗한 본문."},
			},
		})
		if len(hits) != 1 {
			t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 0)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
	})

	t.Run("location_only", func(t *testing.T) {
		hits := terms.MatchNarration(&domain.NarrationScript{
			Title: "Clean Title",
			Scenes: []domain.NarrationScene{
				{SceneNum: 3, Narration: "본문 깨끗합니다.", Location: "wiki archive"},
			},
		})
		if len(hits) != 1 {
			t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 3)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
	})

	t.Run("atmosphere_only", func(t *testing.T) {
		hits := terms.MatchNarration(&domain.NarrationScript{
			Title: "Clean Title",
			Scenes: []domain.NarrationScene{
				{SceneNum: 5, Narration: "본문 깨끗합니다.", Atmosphere: "wiki-like dread"},
			},
		})
		if len(hits) != 1 {
			t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 5)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
	})

	t.Run("mood_only", func(t *testing.T) {
		hits := terms.MatchNarration(&domain.NarrationScript{
			Title: "Clean Title",
			Scenes: []domain.NarrationScene{
				{SceneNum: 7, Narration: "본문 깨끗합니다.", Mood: "wiki curiosity"},
			},
		})
		if len(hits) != 1 {
			t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 7)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
	})

	t.Run("fact_tag_content_only", func(t *testing.T) {
		hits := terms.MatchNarration(&domain.NarrationScript{
			Title: "Clean Title",
			Scenes: []domain.NarrationScene{
				{
					SceneNum:  9,
					Narration: "본문 깨끗합니다.",
					FactTags: []domain.FactTag{
						{Key: "source", Content: "sourced from a wiki dump"},
					},
				},
			},
		})
		if len(hits) != 1 {
			t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 9)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
	})

	t.Run("narration_only_regression", func(t *testing.T) {
		hits := terms.MatchNarration(&domain.NarrationScript{
			Title: "Clean Title",
			Scenes: []domain.NarrationScene{
				{SceneNum: 2, Narration: "이건 wiki 문체입니다."},
				{SceneNum: 4, Narration: "여기 금칙어 표현이 있습니다."},
			},
		})
		if len(hits) != 2 {
			t.Fatalf("got %d hits, want 2: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 2)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
		testutil.AssertEqual(t, hits[1].SceneNum, 4)
		testutil.AssertEqual(t, hits[1].Pattern, "금칙어")
	})

	t.Run("multi_field_multi_scene_sort_order", func(t *testing.T) {
		hits := terms.MatchNarration(&domain.NarrationScript{
			Title: "A 금칙어 appears in title",
			Scenes: []domain.NarrationScene{
				{
					SceneNum:   4,
					Narration:  "scene 4 narration wiki here",
					Location:   "scene 4 wiki archive",
					Atmosphere: "clean",
					Mood:       "clean",
				},
				{
					SceneNum:   2,
					Narration:  "scene 2 clean",
					Location:   "clean",
					Atmosphere: "wiki-flavored tension",
					Mood:       "clean",
					FactTags: []domain.FactTag{
						{Key: "note", Content: "금칙어 inside fact tag"},
					},
				},
			},
		})

		want := []ForbiddenTermHit{
			{SceneNum: 0, Pattern: "금칙어"},
			{SceneNum: 2, Pattern: "wiki"},
			{SceneNum: 2, Pattern: "금칙어"},
			{SceneNum: 4, Pattern: "wiki"},
			{SceneNum: 4, Pattern: "wiki"},
		}
		if len(hits) != len(want) {
			t.Fatalf("got %d hits, want %d: %+v", len(hits), len(want), hits)
		}
		for i := range want {
			testutil.AssertEqual(t, hits[i].SceneNum, want[i].SceneNum)
			testutil.AssertEqual(t, hits[i].Pattern, want[i].Pattern)
		}
	})
}

func policyRoot(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "docs", "policy")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "forbidden_terms.ko.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return root
}
