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
	hits := terms.MatchNarration(scriptFromBeats("",
		beatSpec{narration: "이건 wiki 문체입니다."},
		beatSpec{narration: "여기 금칙어 표현이 있습니다."},
	))
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	testutil.AssertEqual(t, hits[0].SceneNum, 1)
	testutil.AssertEqual(t, hits[1].SceneNum, 2)
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

func TestMinorSensitivePatterns_LoadAndMatch(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := minorsPolicyRoot(t, "# comment\n미성년자.{0,12}폭행\n아동.{0,12}성착취\n")
	patterns, err := LoadMinorSensitivePatterns(root)
	if err != nil {
		t.Fatalf("LoadMinorSensitivePatterns: %v", err)
	}
	if len(patterns.Raw) != 2 {
		t.Fatalf("got %d patterns, want 2", len(patterns.Raw))
	}
	hits := patterns.MatchNarration(scriptFromBeats("",
		beatSpec{narration: "미성년자 폭행 장면이 이어집니다."},
		beatSpec{narration: "아동 성착취 묘사는 금지입니다."},
	))
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	testutil.AssertEqual(t, hits[0].SceneNum, 1)
	testutil.AssertEqual(t, hits[1].SceneNum, 2)
}

func TestMinorSensitivePatterns_InvalidRegexRejected(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := minorsPolicyRoot(t, "[\n")
	_, err := LoadMinorSensitivePatterns(root)
	if err == nil || !strings.Contains(err.Error(), domain.ErrValidation.Error()) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestMinorSensitivePatterns_VersionStable(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	root := minorsPolicyRoot(t, "미성년자.{0,12}폭행\n")
	first, err := LoadMinorSensitivePatterns(root)
	if err != nil {
		t.Fatalf("LoadMinorSensitivePatterns #1: %v", err)
	}
	second, err := LoadMinorSensitivePatterns(root)
	if err != nil {
		t.Fatalf("LoadMinorSensitivePatterns #2: %v", err)
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
		hits := terms.MatchNarration(scriptFromBeats("The wiki Incident",
			beatSpec{narration: "깨끗한 본문."},
		))
		if len(hits) != 1 {
			t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 0)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
	})

	t.Run("location_only", func(t *testing.T) {
		hits := terms.MatchNarration(scriptFromBeats("Clean Title",
			beatSpec{narration: "본문 깨끗합니다.", location: "wiki archive"},
		))
		if len(hits) != 1 {
			t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 1)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
	})

	t.Run("atmosphere_only", func(t *testing.T) {
		hits := terms.MatchNarration(scriptFromBeats("Clean Title",
			beatSpec{narration: "본문 깨끗합니다.", atmosphere: "wiki-like dread"},
		))
		if len(hits) != 1 {
			t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 1)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
	})

	t.Run("mood_only", func(t *testing.T) {
		hits := terms.MatchNarration(scriptFromBeats("Clean Title",
			beatSpec{narration: "본문 깨끗합니다.", mood: "wiki curiosity"},
		))
		if len(hits) != 1 {
			t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 1)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
	})

	t.Run("fact_tag_content_only", func(t *testing.T) {
		hits := terms.MatchNarration(scriptFromBeats("Clean Title",
			beatSpec{
				narration: "본문 깨끗합니다.",
				factTags:  []domain.FactTag{{Key: "source", Content: "sourced from a wiki dump"}},
			},
		))
		if len(hits) != 1 {
			t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 1)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
	})

	t.Run("narration_only_regression", func(t *testing.T) {
		hits := terms.MatchNarration(scriptFromBeats("Clean Title",
			beatSpec{narration: "이건 wiki 문체입니다."},
			beatSpec{narration: "여기 금칙어 표현이 있습니다."},
		))
		if len(hits) != 2 {
			t.Fatalf("got %d hits, want 2: %+v", len(hits), hits)
		}
		testutil.AssertEqual(t, hits[0].SceneNum, 1)
		testutil.AssertEqual(t, hits[0].Pattern, "wiki")
		testutil.AssertEqual(t, hits[1].SceneNum, 2)
		testutil.AssertEqual(t, hits[1].Pattern, "금칙어")
	})

	t.Run("multi_field_multi_scene_sort_order", func(t *testing.T) {
		hits := terms.MatchNarration(scriptFromBeats("A 금칙어 appears in title",
			beatSpec{
				narration:  "scene 1 narration wiki here",
				location:   "scene 1 wiki archive",
				atmosphere: "clean",
				mood:       "clean",
			},
			beatSpec{
				narration:  "scene 2 clean",
				location:   "clean",
				atmosphere: "wiki-flavored tension",
				mood:       "clean",
				factTags:   []domain.FactTag{{Key: "note", Content: "금칙어 inside fact tag"}},
			},
		))

		want := []ForbiddenTermHit{
			{SceneNum: 0, Pattern: "금칙어"},
			{SceneNum: 1, Pattern: "wiki"},
			{SceneNum: 1, Pattern: "wiki"},
			{SceneNum: 2, Pattern: "wiki"},
			{SceneNum: 2, Pattern: "금칙어"},
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

// beatSpec is the test-scoped shape for synthesizing v2 beats from v1-style
// scene fields. scriptFromBeats stitches the scenes' narration into a single
// monologue under ActIncident and emits one BeatAnchor per spec.
type beatSpec struct {
	narration  string
	location   string
	mood       string
	atmosphere string
	factTags   []domain.FactTag
}

func scriptFromBeats(title string, beats ...beatSpec) *domain.NarrationScript {
	if len(beats) == 0 {
		return &domain.NarrationScript{Title: title}
	}
	parts := make([]string, len(beats))
	for i, b := range beats {
		parts[i] = b.narration
	}
	monologue := strings.Join(parts, " ")
	anchors := make([]domain.BeatAnchor, 0, len(beats))
	offset := 0
	for i, b := range beats {
		runes := []rune(b.narration)
		end := offset + len(runes)
		anchors = append(anchors, domain.BeatAnchor{
			StartOffset:       offset,
			EndOffset:         end,
			Mood:              firstNonEmptyTest(b.mood, "calm"),
			Location:          firstNonEmptyTest(b.location, "site-19"),
			CharactersPresent: []string{"observer"},
			EntityVisible:     false,
			ColorPalette:      "neutral gray",
			Atmosphere:        firstNonEmptyTest(b.atmosphere, "subdued"),
			FactTags:          b.factTags,
		})
		offset = end
		if i < len(beats)-1 {
			offset++ // joining space
		}
	}
	return &domain.NarrationScript{
		Title: title,
		Acts: []domain.ActScript{
			{
				ActID:     domain.ActIncident,
				Monologue: monologue,
				Beats:     anchors,
			},
		},
	}
}

func firstNonEmptyTest(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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

func minorsPolicyRoot(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "docs", "policy")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "minor_sensitive_contexts.ko.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return root
}
