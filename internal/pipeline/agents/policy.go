package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

type ForbiddenTerms struct {
	Version string
	Raw     []string
	regexps []*regexp.Regexp
}

type MinorSensitivePatterns struct {
	Version string
	Raw     []string
	regexps []*regexp.Regexp
}

// ForbiddenTermHit records a single forbidden-term match against a
// NarrationScript text field.
//
// ActID == "" is the title-level sentinel — the hit came from
// NarrationScript.Title, not from any per-act monologue. ActID matches
// NarrationScript.Acts[i].ActID for per-act hits. RuneOffset is the rune
// index of the match's leading rune within the parent act's Monologue (or 0
// for ActID == ""); per-act metadata field hits (Mood, KeyPoints, Beats[*]
// metadata) report the parent beat's StartOffset (or 0 if matched from an
// act-level field outside any beat) so callers can still translate back to a
// flat scene_index via NarrationScript.BeatIndexAt.
type ForbiddenTermHit struct {
	ActID      string
	RuneOffset int
	Pattern    string
}

type MinorRegexHit = domain.MinorRegexHit

const forbiddenTermsPath = "docs/policy/forbidden_terms.ko.txt"
const minorSensitiveContextsPath = "docs/policy/minor_sensitive_contexts.ko.txt"

func LoadForbiddenTerms(projectRoot string) (*ForbiddenTerms, error) {
	if projectRoot == "" {
		return nil, fmt.Errorf("load forbidden terms: %w: projectRoot is empty", domain.ErrValidation)
	}

	raw, patterns, regexps, err := loadPolicyRegexFile(filepath.Join(projectRoot, forbiddenTermsPath), "load forbidden terms")
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return &ForbiddenTerms{
		Version: hex.EncodeToString(sum[:]),
		Raw:     patterns,
		regexps: regexps,
	}, nil
}

func LoadMinorSensitivePatterns(projectRoot string) (*MinorSensitivePatterns, error) {
	if projectRoot == "" {
		return nil, fmt.Errorf("load minor sensitive patterns: %w: projectRoot is empty", domain.ErrValidation)
	}

	raw, patterns, regexps, err := loadPolicyRegexFile(filepath.Join(projectRoot, minorSensitiveContextsPath), "load minor sensitive patterns")
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return &MinorSensitivePatterns{
		Version: hex.EncodeToString(sum[:]),
		Raw:     patterns,
		regexps: regexps,
	}, nil
}

// MatchNarration scans every policy-relevant free-text field on the script
// (Title, and each act's Monologue, Mood, KeyPoints, plus per-beat
// Location/Atmosphere/Mood/FactTags[i].Content) for any forbidden-term regex.
// A single pattern that matches multiple fields within the same act/beat is
// reported once per field.
//
// Per the D plan ("v2 iterates acts[].Monologue"), the primary unit is the
// act monologue; per-beat metadata scanning is retained from the v1 multi-
// field policy because the regexes target arbitrary leakage surfaces, not
// just narration text. RuneOffset for monologue-text matches is the
// FindStringIndex byte offset converted to runes; for metadata-field
// matches, RuneOffset is the parent beat's StartOffset (or 0 when matched
// outside any beat, e.g. ActScript.Mood). ActID is empty for Title hits.
//
// Results are returned sorted deterministically by (ActID asc — empty first,
// RuneOffset asc, Pattern asc).
func (f *ForbiddenTerms) MatchNarration(script *domain.NarrationScript) []ForbiddenTermHit {
	if f == nil || script == nil {
		return nil
	}
	hits := make([]ForbiddenTermHit, 0)
	for idx, re := range f.regexps {
		if re.MatchString(script.Title) {
			hits = append(hits, ForbiddenTermHit{
				ActID:      "",
				RuneOffset: 0,
				Pattern:    f.Raw[idx],
			})
		}
	}
	for _, act := range script.Acts {
		// Monologue — primary scan unit. Find all (non-overlapping) match
		// positions so distinct rune offsets are reported.
		for idx, re := range f.regexps {
			locs := re.FindAllStringIndex(act.Monologue, -1)
			for _, loc := range locs {
				runeOffset := utf8.RuneCountInString(act.Monologue[:loc[0]])
				hits = append(hits, ForbiddenTermHit{
					ActID:      act.ActID,
					RuneOffset: runeOffset,
					Pattern:    f.Raw[idx],
				})
			}
		}
		// Act-level metadata — Mood + KeyPoints. Reported with RuneOffset=0
		// (start of act) so review_gate can still translate via BeatIndexAt
		// to the act's first beat.
		actLevelFields := []string{act.Mood}
		actLevelFields = append(actLevelFields, act.KeyPoints...)
		for idx, re := range f.regexps {
			for _, field := range actLevelFields {
				if re.MatchString(field) {
					hits = append(hits, ForbiddenTermHit{
						ActID:      act.ActID,
						RuneOffset: 0,
						Pattern:    f.Raw[idx],
					})
				}
			}
		}
		// Per-beat metadata — Location, Atmosphere, Mood, FactTags[i].Content.
		for _, beat := range act.Beats {
			fields := []string{beat.Location, beat.Atmosphere, beat.Mood}
			for _, tag := range beat.FactTags {
				fields = append(fields, tag.Content)
			}
			for idx, re := range f.regexps {
				for _, field := range fields {
					if re.MatchString(field) {
						hits = append(hits, ForbiddenTermHit{
							ActID:      act.ActID,
							RuneOffset: beat.StartOffset,
							Pattern:    f.Raw[idx],
						})
					}
				}
			}
		}
	}
	sortForbiddenHits(hits)
	return hits
}

func (p *MinorSensitivePatterns) MatchNarration(script *domain.NarrationScript) []MinorRegexHit {
	if p == nil || script == nil {
		return nil
	}
	hits := make([]MinorRegexHit, 0)
	for _, act := range script.Acts {
		for idx, re := range p.regexps {
			locs := re.FindAllStringIndex(act.Monologue, -1)
			for _, loc := range locs {
				runeOffset := utf8.RuneCountInString(act.Monologue[:loc[0]])
				hits = append(hits, MinorRegexHit{
					ActID:      act.ActID,
					RuneOffset: runeOffset,
					Pattern:    p.Raw[idx],
				})
			}
		}
		for _, beat := range act.Beats {
			fields := []string{beat.Location, beat.Atmosphere, beat.Mood}
			for _, tag := range beat.FactTags {
				fields = append(fields, tag.Content)
			}
			for idx, re := range p.regexps {
				for _, field := range fields {
					if re.MatchString(field) {
						hits = append(hits, MinorRegexHit{
							ActID:      act.ActID,
							RuneOffset: beat.StartOffset,
							Pattern:    p.Raw[idx],
						})
					}
				}
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].ActID != hits[j].ActID {
			return hits[i].ActID < hits[j].ActID
		}
		if hits[i].RuneOffset != hits[j].RuneOffset {
			return hits[i].RuneOffset < hits[j].RuneOffset
		}
		return hits[i].Pattern < hits[j].Pattern
	})
	return hits
}

func sortForbiddenHits(hits []ForbiddenTermHit) {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].ActID != hits[j].ActID {
			return hits[i].ActID < hits[j].ActID
		}
		if hits[i].RuneOffset != hits[j].RuneOffset {
			return hits[i].RuneOffset < hits[j].RuneOffset
		}
		return hits[i].Pattern < hits[j].Pattern
	})
}

func loadPolicyRegexFile(path string, label string) ([]byte, []string, []*regexp.Regexp, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%s: %w", label, domain.ErrValidation)
	}
	if !utf8.Valid(raw) {
		return nil, nil, nil, fmt.Errorf("%s: invalid utf-8: %w", label, domain.ErrValidation)
	}

	lines := strings.Split(string(raw), "\n")
	patterns := make([]string, 0, len(lines))
	regexps := make([]*regexp.Regexp, 0, len(lines))
	for _, line := range lines {
		pattern := strings.TrimSpace(line)
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		expr := pattern
		if !containsHangul(pattern) {
			expr = "(?i:" + pattern + ")"
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: compile %q: %w", label, pattern, domain.ErrValidation)
		}
		patterns = append(patterns, pattern)
		regexps = append(regexps, re)
	}
	return raw, patterns, regexps, nil
}
