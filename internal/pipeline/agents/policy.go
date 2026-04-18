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

const forbiddenTermsPath = "docs/policy/forbidden_terms.ko.txt"

func LoadForbiddenTerms(projectRoot string) (*ForbiddenTerms, error) {
	if projectRoot == "" {
		return nil, fmt.Errorf("load forbidden terms: %w: projectRoot is empty", domain.ErrValidation)
	}

	raw, err := os.ReadFile(filepath.Join(projectRoot, forbiddenTermsPath))
	if err != nil {
		return nil, fmt.Errorf("load forbidden terms: %w", domain.ErrValidation)
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("load forbidden terms: invalid utf-8: %w", domain.ErrValidation)
	}

	lines := strings.Split(string(raw), "\n")
	patterns := make([]string, 0, len(lines))
	regexps := make([]*regexp.Regexp, 0, len(lines))
	for _, line := range lines {
		pattern := strings.TrimSpace(line)
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		re, err := regexp.Compile("(?i:" + pattern + ")")
		if err != nil {
			return nil, fmt.Errorf("load forbidden terms: compile %q: %w", pattern, domain.ErrValidation)
		}
		patterns = append(patterns, pattern)
		regexps = append(regexps, re)
	}

	sum := sha256.Sum256(raw)
	return &ForbiddenTerms{
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
	for _, scene := range script.Scenes {
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
