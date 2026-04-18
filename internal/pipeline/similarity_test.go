package pipeline_test

import (
	"math"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestTokenize_LowercasesAndSplits(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	cases := []struct {
		name string
		in   string
		want map[string]int
	}{
		{"simple", "hello world", map[string]int{"hello": 1, "world": 1}},
		{"mixed case", "Hello World hello", map[string]int{"hello": 2, "world": 1}},
		{"punctuation", "hello, world!", map[string]int{"hello": 1, "world": 1}},
		{"dashes", "the-quick-brown-fox", map[string]int{"the": 1, "quick": 1, "brown": 1, "fox": 1}},
		{"repeats", "a a a b", map[string]int{"a": 3, "b": 1}},
		{"multi whitespace", "  a\tb\nc  ", map[string]int{"a": 1, "b": 1, "c": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pipeline.Tokenize(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len(got)=%d len(want)=%d got=%v", len(got), len(tc.want), got)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("got[%q]=%d want %d", k, got[k], v)
				}
			}
		})
	}
}

func TestTokenize_EmptyInputReturnsEmptyMap(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	got := pipeline.Tokenize("")
	if got == nil {
		t.Fatal("expected non-nil empty map")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}

func TestCosineSimilarity_GoldenPairs(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	cases := []struct {
		name string
		a, b string
		want float64
	}{
		{"both empty", "", "", 0.0},
		{"one empty", "hello", "", 0.0},
		{"identical", "a b c", "a b c", 1.0},
		{"order independent", "a b", "b a", 1.0},
		{"partial overlap 4of5", "the quick brown fox", "the quick brown fox jumped", 4.0 / (2.0 * math.Sqrt(5.0))},
		{"no overlap", "hello", "goodbye", 0.0},
		{"case and punctuation", "The Quick Brown Fox", "the-quick-brown-fox", 1.0},
		{"single overlap 1of3", "aa bb cc", "cc dd ee", 1.0 / 3.0},
		{"repetition scaling", "hello world", "hello world hello world", 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pipeline.CosineSimilarity(tc.a, tc.b)
			if !almostEqual(got, tc.want, 1e-9) {
				t.Errorf("got=%.12f want=%.12f", got, tc.want)
			}
		})
	}
}

func TestCosineSimilarity_Deterministic100Runs(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	a := "the quick brown fox jumps over the lazy dog"
	b := "a quick brown fox jumps over the lazy dog"
	first := pipeline.CosineSimilarity(a, b)
	firstBits := math.Float64bits(first)
	for i := 0; i < 100; i++ {
		got := pipeline.CosineSimilarity(a, b)
		if math.Float64bits(got) != firstBits {
			t.Fatalf("iteration %d: non-deterministic result: got bits=%x want bits=%x (got=%.20f want=%.20f)",
				i, math.Float64bits(got), firstBits, got, first)
		}
	}
}

func almostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}
