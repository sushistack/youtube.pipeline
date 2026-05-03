// Package style is the single source of truth for narration style rules,
// Korean SCP terminology, and the CC BY-SA attribution template. The
// configuration lives in configs/style_guide.yaml and is loaded once at
// init by callers that need it.
//
// The package is intentionally additive — no existing config code (Viper,
// godotenv, internal/config/) is touched. Callers opt in by importing this
// package directly.
package style

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultPath is the canonical relative location of the style guide.
// Resolved against the project root by the caller.
const DefaultPath = "configs/style_guide.yaml"

// EnvOverride lets operators point at a different style guide for tests
// or staged rollouts without recompiling.
const EnvOverride = "STYLE_GUIDE_PATH"

// StyleGuide is the typed representation of configs/style_guide.yaml. New
// fields must be additive — existing callers that read only a subset of
// the struct must keep working when the YAML grows.
type StyleGuide struct {
	Narration             Narration         `yaml:"narration"`
	KoreanSCPTerms        map[string]string `yaml:"korean_scp_terms"`
	AttributionTemplateKO string            `yaml:"attribution_template_ko"`
}

// Narration captures the per-script writing rules used by the writer
// agent and the critic rubric.
type Narration struct {
	AvgSentenceLengthTense       int      `yaml:"avg_sentence_length_tense"`
	AvgSentenceLengthCalm        int      `yaml:"avg_sentence_length_calm"`
	RhetoricalQuestionPerMinute  int      `yaml:"rhetorical_question_per_minute"`
	ForbiddenOpenings            []string `yaml:"forbidden_openings"`
	PreferredEndings             []string `yaml:"preferred_endings"`
	ForbiddenEndings             []string `yaml:"forbidden_endings"`
	AbstractEmotionWords         []string `yaml:"abstract_emotion_words"`
	MaxAbstractEmotionWordsPerScript int  `yaml:"max_abstract_emotion_words_per_script"`
}

// ErrNotConfigured is returned when neither STYLE_GUIDE_PATH nor the
// default file exists. Callers can treat this as "feature not enabled"
// rather than a fatal error.
var ErrNotConfigured = errors.New("style: guide not configured")

// Load reads and parses the style guide at the given absolute path.
// Empty path is rejected; missing file returns ErrNotConfigured wrapped
// so callers can use errors.Is.
func Load(path string) (*StyleGuide, error) {
	if path == "" {
		return nil, fmt.Errorf("style: empty path: %w", ErrNotConfigured)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("style: %s: %w", path, ErrNotConfigured)
		}
		return nil, fmt.Errorf("style: read %s: %w", path, err)
	}
	var sg StyleGuide
	if err := yaml.Unmarshal(raw, &sg); err != nil {
		return nil, fmt.Errorf("style: parse %s: %w", path, err)
	}
	if err := sg.Validate(); err != nil {
		return nil, fmt.Errorf("style: validate %s: %w", path, err)
	}
	return &sg, nil
}

// LoadFromProjectRoot resolves the path against the given project root,
// honoring STYLE_GUIDE_PATH if set (absolute path takes precedence).
func LoadFromProjectRoot(projectRoot string) (*StyleGuide, error) {
	if path := os.Getenv(EnvOverride); path != "" {
		return Load(path)
	}
	if projectRoot == "" {
		return nil, fmt.Errorf("style: empty project root: %w", ErrNotConfigured)
	}
	return Load(filepath.Join(projectRoot, DefaultPath))
}

// Validate enforces invariants the rest of the codebase depends on.
// Keeping this in the loader rather than in callers means a malformed
// YAML fails fast at init, not during a critic call.
func (sg *StyleGuide) Validate() error {
	if sg.Narration.AvgSentenceLengthTense <= 0 {
		return fmt.Errorf("narration.avg_sentence_length_tense must be > 0")
	}
	if sg.Narration.AvgSentenceLengthCalm <= 0 {
		return fmt.Errorf("narration.avg_sentence_length_calm must be > 0")
	}
	if sg.Narration.AvgSentenceLengthTense > sg.Narration.AvgSentenceLengthCalm {
		return fmt.Errorf("narration: tense average (%d) must be <= calm average (%d)",
			sg.Narration.AvgSentenceLengthTense, sg.Narration.AvgSentenceLengthCalm)
	}
	if len(sg.KoreanSCPTerms) == 0 {
		return fmt.Errorf("korean_scp_terms must not be empty")
	}
	if sg.AttributionTemplateKO == "" {
		return fmt.Errorf("attribution_template_ko must not be empty")
	}
	return nil
}

// CountAbstractEmotionHits counts substring matches of the configured
// abstract emotion roots ("끔찍", "무서운" …) inside text. Matching is
// substring-based so conjugations ("끔찍한", "끔찍해요") all hit. Used
// by the critic rubric (criterion 7).
func (sg *StyleGuide) CountAbstractEmotionHits(text string) int {
	if text == "" {
		return 0
	}
	hits := 0
	for _, root := range sg.Narration.AbstractEmotionWords {
		if root == "" {
			continue
		}
		hits += strings.Count(text, root)
	}
	return hits
}
