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
// SceneNum carries the scene number of the hit, with one sentinel:
// SceneNum == 0 means the hit came from the top-level NarrationScript.Title
// (title-level), not from any scene. In practice scene numbering starts at 1,
// so 0 is safe as a "title" marker.
type ForbiddenTermHit struct {
	SceneNum int
	Pattern  string
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
// (Title, and each scene's Narration, Location, Atmosphere, Mood, and
// FactTags[i].Content) for any forbidden-term regex. A single pattern that
// matches multiple fields within the same scene is reported once per field.
//
// Results are returned sorted deterministically by (SceneNum asc, Pattern asc).
// Title-level hits use the sentinel SceneNum = 0 (see ForbiddenTermHit) and
// therefore sort before any scene.
func (f *ForbiddenTerms) MatchNarration(script *domain.NarrationScript) []ForbiddenTermHit {
	if f == nil || script == nil {
		return nil
	}
	hits := make([]ForbiddenTermHit, 0)
	for idx, re := range f.regexps {
		if re.MatchString(script.Title) {
			hits = append(hits, ForbiddenTermHit{
				SceneNum: 0,
				Pattern:  f.Raw[idx],
			})
		}
	}
	for _, scene := range script.LegacyScenes() {
		fields := []string{scene.Narration, scene.Location, scene.Atmosphere, scene.Mood}
		for _, tag := range scene.FactTags {
			fields = append(fields, tag.Content)
		}
		for idx, re := range f.regexps {
			for _, field := range fields {
				if re.MatchString(field) {
					hits = append(hits, ForbiddenTermHit{
						SceneNum: scene.SceneNum,
						Pattern:  f.Raw[idx],
					})
				}
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].SceneNum == hits[j].SceneNum {
			return hits[i].Pattern < hits[j].Pattern
		}
		return hits[i].SceneNum < hits[j].SceneNum
	})
	return hits
}

func (p *MinorSensitivePatterns) MatchNarration(script *domain.NarrationScript) []MinorRegexHit {
	if p == nil || script == nil {
		return nil
	}
	hits := make([]MinorRegexHit, 0)
	for _, scene := range script.LegacyScenes() {
		fields := []string{scene.Narration, scene.Location, scene.Atmosphere, scene.Mood}
		for _, tag := range scene.FactTags {
			fields = append(fields, tag.Content)
		}
		for idx, re := range p.regexps {
			for _, field := range fields {
				if re.MatchString(field) {
					hits = append(hits, MinorRegexHit{
						SceneNum: scene.SceneNum,
						Pattern:  p.Raw[idx],
					})
				}
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].SceneNum == hits[j].SceneNum {
			return hits[i].Pattern < hits[j].Pattern
		}
		return hits[i].SceneNum < hits[j].SceneNum
	})
	return hits
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
