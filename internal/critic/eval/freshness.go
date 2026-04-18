package eval

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const criticPromptRelPath = "docs/prompts/scenario/critic_agent.md"

// FreshnessStatus summarises staleness warnings from a Golden set freshness check.
type FreshnessStatus struct {
	Warnings          []string
	DaysSinceRefresh  int
	PromptHashChanged bool
	CurrentPromptHash string
}

// EvaluateFreshness checks the Golden manifest for staleness against the
// given threshold and detects Critic prompt changes. Warnings are advisory;
// this function never returns a hard error for staleness conditions.
func EvaluateFreshness(projectRoot string, now time.Time, thresholdDays int) (FreshnessStatus, error) {
	if thresholdDays < 1 {
		return FreshnessStatus{}, fmt.Errorf("thresholdDays must be >= 1")
	}

	m, err := loadManifest(projectRoot)
	if err != nil {
		return FreshnessStatus{}, fmt.Errorf("load manifest: %w", err)
	}

	currentHash, err := CurrentCriticPromptHash(projectRoot)
	if err != nil {
		return FreshnessStatus{}, fmt.Errorf("hash critic prompt: %w", err)
	}

	days := int(now.Sub(m.LastRefreshedAt).Hours() / 24)

	var warnings []string
	if days >= thresholdDays {
		warnings = append(warnings, fmt.Sprintf(
			"Staleness Warning: Golden set last refreshed %d days ago (threshold: %d days)",
			days, thresholdDays,
		))
	}

	promptHashChanged := m.LastSuccessfulPromptHash != "" && m.LastSuccessfulPromptHash != currentHash
	if promptHashChanged {
		warnings = append(warnings, "Staleness Warning: Critic prompt changed since last Golden validation")
	}

	return FreshnessStatus{
		Warnings:          warnings,
		DaysSinceRefresh:  days,
		PromptHashChanged: promptHashChanged,
		CurrentPromptHash: currentHash,
	}, nil
}

// CurrentCriticPromptHash returns the SHA-256 hex digest of the raw bytes of
// docs/prompts/scenario/critic_agent.md. Missing file is a hard error.
func CurrentCriticPromptHash(projectRoot string) (string, error) {
	path := filepath.Join(projectRoot, criticPromptRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read critic prompt %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}
