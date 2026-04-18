package pipeline

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// V1 bag-of-words cosine similarity for FR8 anti-progress detection.
// V1.5 will replace with embedding-based similarity (deferred — see
// deferred-work.md). Signature stays stable across the migration.

// Tokenize splits s into lowercased tokens on Unicode whitespace and
// punctuation. Tokenization is deterministic (no randomness, no global
// state); iteration over the returned map is NOT — Go randomizes map
// iteration order. Downstream consumers that need stable ordering must
// sort the keys themselves (CosineSimilarity does this internally).
// Empty input returns an empty (non-nil) map so callers can iterate
// without a nil check. Exported so tests and the anti-progress detector
// can assert on the tokenization contract.
func Tokenize(s string) map[string]int {
	counts := make(map[string]int)
	if s == "" {
		return counts
	}
	lower := strings.ToLower(s)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})
	for _, tok := range fields {
		if tok == "" {
			continue
		}
		counts[tok]++
	}
	return counts
}

// CosineSimilarity returns the bag-of-words cosine similarity of a and b
// in [0.0, 1.0]:
//
//   - Tokenize both inputs.
//   - Build sparse term-frequency maps.
//   - Return dot(Ta, Tb) / (||Ta|| * ||Tb||).
//
// Returns 0 when either side has no tokens (avoids NaN and means "no
// evidence of stuckness" — safe default for the anti-progress detector).
// The result is bit-exact deterministic across runs: token keys are
// sorted before summation so Go's randomized map iteration order does
// not introduce floating-point reordering drift.
func CosineSimilarity(a, b string) float64 {
	tokensA := Tokenize(a)
	tokensB := Tokenize(b)
	if len(tokensA) == 0 || len(tokensB) == 0 {
		return 0
	}

	// Iterate the smaller side for the dot product; fall back to the
	// larger side for norm computations where iteration order still
	// must be deterministic.
	smaller, larger := tokensA, tokensB
	if len(larger) < len(smaller) {
		smaller, larger = larger, smaller
	}

	dot := sumSortedProducts(smaller, larger)
	normA := sumSortedSquares(tokensA)
	normB := sumSortedSquares(tokensB)

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	sim := dot / denom
	// Clamp to [0, 1] defensively: floating-point rounding on repetition-
	// scaled inputs can produce ~1e-16 overshoot above 1.0 or drift below 0,
	// which would violate the detector's strict `>` threshold contract.
	if sim > 1.0 {
		return 1.0
	}
	if sim < 0 {
		return 0
	}
	return sim
}

// sumSortedProducts iterates `small` in sorted key order, looks each key
// up in `large`, and sums the product. Sorted iteration pins the
// floating-point summation order so the result is bit-exact across runs.
func sumSortedProducts(small, large map[string]int) float64 {
	keys := make([]string, 0, len(small))
	for k := range small {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sum float64
	for _, k := range keys {
		if v, ok := large[k]; ok {
			sum += float64(small[k]) * float64(v)
		}
	}
	return sum
}

// sumSortedSquares iterates `m` in sorted key order and sums the squared
// counts. Sorted iteration pins the floating-point summation order.
func sumSortedSquares(m map[string]int) float64 {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sum float64
	for _, k := range keys {
		c := float64(m[k])
		sum += c * c
	}
	return sum
}
