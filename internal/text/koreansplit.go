// Package text provides text-manipulation helpers shared across pipeline
// stages. The Korean sentence splitter is consumed by the TTS track to chunk
// continuous monologue input under DashScope's per-call byte cap while
// keeping seams on natural sentence boundaries.
package text

import (
	"strings"
	"unicode/utf8"
)

// KRSentenceTerminators is the set of runes that close a Korean sentence for
// TTS-chunking purposes. Korean sentence-final eomi (`다.`, `요.`, `니다.`)
// all end in `.`, so rune-level terminator matching covers them; questions
// and exclamations and ellipses are tracked explicitly. Newline is included
// so paragraph breaks act as natural chunk boundaries.
var KRSentenceTerminators = map[rune]bool{
	'.': true, '?': true, '!': true, '…': true, '\n': true,
}

// SplitKRSentences breaks text into sentence units. The terminator rune AND
// any immediately-trailing whitespace (space / tab) are kept with the
// preceding sentence so concatenating the returned slice reproduces the input
// byte-for-byte — a property the TTS track relies on for sample-accurate
// rune-offset → time-offset mapping after chunked synthesis.
//
// Example:
//
//	SplitKRSentences("안녕하세요. 오늘은 049입니다.")
//	  → ["안녕하세요. ", "오늘은 049입니다."]
//
// An input without any terminator returns the whole input as one element.
// Empty input returns a nil slice.
func SplitKRSentences(text string) []string {
	if text == "" {
		return nil
	}
	runes := []rune(text)
	out := make([]string, 0, 4)
	var cur strings.Builder
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		cur.WriteRune(r)
		if !KRSentenceTerminators[r] {
			continue
		}
		for i+1 < len(runes) && isInlineWhitespace(runes[i+1]) {
			i++
			cur.WriteRune(runes[i])
		}
		out = append(out, cur.String())
		cur.Reset()
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// PackKRChunks packs sentences greedily into chunks bounded by maxBytes UTF-8
// bytes. Sentences are NOT trimmed — concat(out) == concat(sentences) byte
// for byte, so the TTS track can rely on the merged-monologue rune count
// matching the synthesized audio's rune content.
//
// A single sentence longer than maxBytes falls through to UTF-8-safe hard
// byte split (HardSplitUTF8) so callers never produce a chunk that breaks a
// multi-byte rune mid-sequence. Rare in practice (a ~600-byte cap holds
// ~200 Korean glyphs, longer than any realistic single sentence) but the
// fallback is safer than silent truncation.
//
// maxBytes <= 0 disables packing — sentences are returned as-is.
func PackKRChunks(sentences []string, maxBytes int) []string {
	if maxBytes <= 0 {
		return sentences
	}
	out := make([]string, 0, len(sentences))
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, s := range sentences {
		if len(s) > maxBytes {
			flush()
			out = append(out, HardSplitUTF8(s, maxBytes)...)
			continue
		}
		if cur.Len()+len(s) > maxBytes {
			flush()
		}
		cur.WriteString(s)
	}
	flush()
	return out
}

// ChunkKR is the convenience wrapper: SplitKRSentences then PackKRChunks.
// Returns the input as a single chunk when it already fits maxBytes (the
// common single-call synthesis case).
func ChunkKR(text string, maxBytes int) []string {
	if text == "" {
		return nil
	}
	if maxBytes <= 0 || len(text) <= maxBytes {
		return []string{text}
	}
	return PackKRChunks(SplitKRSentences(text), maxBytes)
}

// isInlineWhitespace reports whether r is whitespace that should be absorbed
// onto the trailing edge of a just-closed sentence (so concat(parts) recovers
// the original input). Newline is included because monologue inputs often
// place a single blank line between paragraphs — keeping it on the previous
// sentence yields cleaner TTS chunks. A blank-line run is fully absorbed:
// successive newlines all attach to the preceding sentence rather than
// returning empty `\n` elements.
func isInlineWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n'
}

// HardSplitUTF8 splits s into chunks of at most maxBytes UTF-8 bytes while
// never breaking a multi-byte rune mid-sequence. Used as the fallback path
// when a single sentence exceeds the per-call cap. Whitespace is preserved.
func HardSplitUTF8(s string, maxBytes int) []string {
	if maxBytes <= 0 {
		return []string{s}
	}
	out := make([]string, 0, len(s)/maxBytes+1)
	var cur strings.Builder
	for _, r := range s {
		rb := utf8.RuneLen(r)
		if rb < 0 {
			rb = 1
		}
		if cur.Len()+rb > maxBytes && cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
