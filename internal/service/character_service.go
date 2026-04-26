package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

type CharacterRunStore interface {
	Get(ctx context.Context, id string) (*domain.Run, error)
	SetCharacterQueryKey(ctx context.Context, id, queryKey string) error
	ApplyCharacterPick(ctx context.Context, id, queryKey, selectedCharacterID string, frozenDescriptor *string, stage domain.Stage, status domain.Status) error
	LatestFrozenDescriptorBySCPID(ctx context.Context, scpID, excludeRunID string) (*string, error)
}

type CharacterCacheStore interface {
	Get(ctx context.Context, queryKey string) (*domain.CharacterGroup, error)
	GetOrCreate(ctx context.Context, queryText, queryKey string, create func(context.Context) (*domain.CharacterGroup, error)) (*domain.CharacterGroup, error)
}

type CharacterSearchClient interface {
	SearchImages(ctx context.Context, query string, limit int) ([]CharacterSearchResult, error)
}

type CharacterSearchResult struct {
	PageURL     string
	ImageURL    string
	PreviewURL  string
	Title       string
	SourceLabel string
}

// DescriptorDecisionRecorder persists descriptor_edit decisions for undo tracking.
// *db.DecisionStore satisfies this interface structurally.
type DescriptorDecisionRecorder interface {
	RecordDescriptorEdit(ctx context.Context, runID, before, after string) (int64, error)
}

// CharacterService orchestrates DDG-backed search, cache reuse, and operator picks.
type CharacterService struct {
	runs      CharacterRunStore
	cache     CharacterCacheStore
	client    CharacterSearchClient
	decisions DescriptorDecisionRecorder
	// outputDir resolves runs.scenario_path. resume.go stores it as the
	// relative literal "scenario.json" (presence flag), so consumers must
	// join it onto {outputDir}/{runID}/. When empty, GetDescriptorPrefill
	// reads the path verbatim — only safe in tests that seed an absolute
	// path.
	outputDir string
}

func NewCharacterService(runs CharacterRunStore, cache CharacterCacheStore, client CharacterSearchClient) *CharacterService {
	return &CharacterService{runs: runs, cache: cache, client: client}
}

// SetDescriptorRecorder injects an optional decision recorder for descriptor_edit
// undo tracking. If not set, descriptor edits are not recorded.
func (s *CharacterService) SetDescriptorRecorder(r DescriptorDecisionRecorder) {
	s.decisions = r
}

// SetOutputDir wires the pipeline output root used to resolve runs.scenario_path
// from its stored "scenario.json" literal to the actual on-disk file.
func (s *CharacterService) SetOutputDir(outputDir string) {
	s.outputDir = outputDir
}

func (s *CharacterService) Search(ctx context.Context, runID, query string) (*domain.CharacterGroup, error) {
	if _, err := s.runs.Get(ctx, runID); err != nil {
		return nil, fmt.Errorf("character search: load run: %w", err)
	}

	queryText, queryKey, err := normalizeCharacterQuery(query)
	if err != nil {
		return nil, fmt.Errorf("character search: %w", err)
	}

	group, err := s.cache.GetOrCreate(ctx, queryText, queryKey, func(ctx context.Context) (*domain.CharacterGroup, error) {
		if s.client == nil {
			return nil, fmt.Errorf("character search: %w: client not configured", domain.ErrValidation)
		}
		results, err := s.client.SearchImages(ctx, queryText, 10)
		if err != nil {
			return nil, fmt.Errorf("character search: external lookup: %w", err)
		}
		return buildCharacterGroup(queryText, queryKey, results), nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.runs.SetCharacterQueryKey(ctx, runID, queryKey); err != nil {
		return nil, fmt.Errorf("character search: persist query key: %w", err)
	}
	return group, nil
}

// Pick finalizes the operator's character selection and, when frozenDescriptor
// is non-empty, persists the edited Vision Descriptor atomically with the
// stage advance. A blank frozenDescriptor leaves the prior runs.frozen_descriptor
// column unchanged (per AC-3: pick + descriptor are one write, but the
// descriptor half is optional to preserve backward compatibility with callers
// that have not yet adopted the extended request shape).
func (s *CharacterService) Pick(ctx context.Context, runID, candidateID, frozenDescriptor string) (*domain.Run, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("character pick: load run: %w", err)
	}
	if run.Stage != domain.StageCharacterPick {
		return nil, fmt.Errorf("character pick: %w: run stage is %s", domain.ErrConflict, run.Stage)
	}
	queryKey := ""
	if run.CharacterQueryKey != nil {
		queryKey = *run.CharacterQueryKey
	}
	if queryKey == "" {
		queryKey = queryKeyFromCandidateID(candidateID)
	}
	if queryKey == "" {
		return nil, fmt.Errorf("character pick: missing active character query: %w", domain.ErrValidation)
	}
	group, err := s.cache.Get(ctx, queryKey)
	if err != nil {
		return nil, fmt.Errorf("character pick: load cached group: %w", err)
	}
	if !containsCandidate(group, candidateID) {
		return nil, fmt.Errorf("character pick: unknown candidate %q: %w", candidateID, domain.ErrValidation)
	}
	nextStage, err := pipeline.NextStage(run.Stage, domain.EventApprove)
	if err != nil {
		return nil, fmt.Errorf("character pick: next stage: %w", err)
	}
	// Capture previous descriptor for undo traceability before overwriting.
	prevDescriptor := ""
	if run.FrozenDescriptor != nil {
		prevDescriptor = *run.FrozenDescriptor
	}

	var descriptorPtr *string
	if trimmed := strings.TrimSpace(frozenDescriptor); trimmed != "" {
		descriptorPtr = &trimmed
	}
	if err := s.runs.ApplyCharacterPick(ctx, runID, queryKey, candidateID, descriptorPtr, nextStage, pipeline.StatusForStage(nextStage)); err != nil {
		return nil, fmt.Errorf("character pick: persist selection: %w", err)
	}

	// Record descriptor_edit decision when the descriptor actually changed.
	// This must succeed for undo history to remain consistent with the pick.
	newDescriptor := ""
	if descriptorPtr != nil {
		newDescriptor = *descriptorPtr
	}
	if s.decisions != nil && newDescriptor != prevDescriptor {
		if _, err := s.decisions.RecordDescriptorEdit(ctx, runID, prevDescriptor, newDescriptor); err != nil {
			return nil, fmt.Errorf("character pick: record descriptor edit: %w", err)
		}
	}

	updated, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("character pick: reload run: %w", err)
	}
	return updated, nil
}

// GetCandidatesByRun returns the cached character group for a run without
// performing an external search. Callers should invoke this path to restore
// the operator-facing grid on page reload — the run must have been through at
// least one successful Search so that character_query_key is set.
func (s *CharacterService) GetCandidatesByRun(ctx context.Context, runID string) (*domain.CharacterGroup, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("character candidates by run: load run: %w", err)
	}
	if run.CharacterQueryKey == nil || *run.CharacterQueryKey == "" {
		return nil, fmt.Errorf("character candidates by run: no active query key: %w", domain.ErrNotFound)
	}
	group, err := s.cache.Get(ctx, *run.CharacterQueryKey)
	if err != nil {
		return nil, fmt.Errorf("character candidates by run: load cache: %w", err)
	}
	return group, nil
}

// DescriptorPrefill carries the two possible sources of a Vision Descriptor
// pre-fill. The operator-facing component chooses prior when non-nil (UX-DR62)
// and falls back to auto otherwise.
type DescriptorPrefill struct {
	Auto  string  `json:"auto"`
	Prior *string `json:"prior"`
}

// GetDescriptorPrefill returns both candidate pre-fill values for the Vision
// Descriptor editor. "auto" is parsed from the run's scenario artifact (the
// original Phase A output); "prior" is the most-recently-saved descriptor for
// any other run sharing the same SCP ID. The artifact is the source of truth
// for auto because the column on runs is only populated after a pick.
func (s *CharacterService) GetDescriptorPrefill(ctx context.Context, runID string) (*DescriptorPrefill, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("descriptor prefill: load run: %w", err)
	}
	// scenario_path is populated by Phase A; its absence means the run has
	// not yet completed Phase A. Treat as NotFound so the handler maps to
	// 404 (matching AC-4 test intent: "404 vs valid descriptor response
	// shapes"). Returning ErrValidation here would surface as 400 and
	// mislead the client into thinking its input was malformed.
	if run.ScenarioPath == nil || *run.ScenarioPath == "" {
		return nil, fmt.Errorf("descriptor prefill: run has no scenario path: %w", domain.ErrNotFound)
	}
	resolved := *run.ScenarioPath
	if s.outputDir != "" && !filepath.IsAbs(resolved) {
		resolved = filepath.Join(s.outputDir, runID, resolved)
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("descriptor prefill: scenario.json missing at %s: %w", resolved, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("descriptor prefill: read scenario: %w", err)
	}
	var envelope struct {
		VisualBreakdown *struct {
			FrozenDescriptor string `json:"frozen_descriptor"`
		} `json:"visual_breakdown"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("descriptor prefill: decode scenario.json: %w", err)
	}
	// Missing visual_breakdown means Phase A produced a malformed artifact.
	// Silently returning auto="" would let the operator confirm an empty
	// descriptor and propagate the bad state downstream. Surface as NotFound
	// so the client can retry or fall back to prior-run lookup.
	if envelope.VisualBreakdown == nil {
		return nil, fmt.Errorf("descriptor prefill: scenario.json has no visual_breakdown: %w", domain.ErrNotFound)
	}
	auto := envelope.VisualBreakdown.FrozenDescriptor
	prior, err := s.runs.LatestFrozenDescriptorBySCPID(ctx, run.SCPID, runID)
	if err != nil {
		return nil, fmt.Errorf("descriptor prefill: lookup prior: %w", err)
	}
	return &DescriptorPrefill{Auto: auto, Prior: prior}, nil
}

func (s *CharacterService) GetSelectedCandidate(ctx context.Context, runID string) (*domain.CharacterCandidate, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("selected candidate: load run: %w", err)
	}
	if run.SelectedCharacterID == nil || *run.SelectedCharacterID == "" {
		return nil, fmt.Errorf("selected candidate: missing selected character id: %w", domain.ErrValidation)
	}

	queryKey := ""
	if run.CharacterQueryKey != nil {
		queryKey = *run.CharacterQueryKey
	}
	if queryKey == "" {
		queryKey = queryKeyFromCandidateID(*run.SelectedCharacterID)
	}
	if queryKey == "" {
		return nil, fmt.Errorf("selected candidate: missing query key: %w", domain.ErrValidation)
	}

	group, err := s.cache.Get(ctx, queryKey)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("selected candidate: cache row missing for %s: %w", queryKey, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("selected candidate: load cache: %w", err)
	}
	for _, candidate := range group.Candidates {
		if candidate.ID == *run.SelectedCharacterID {
			copy := candidate
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("selected candidate: candidate %q missing: %w", *run.SelectedCharacterID, domain.ErrNotFound)
}

var whitespacePattern = regexp.MustCompile(`\s+`)

func normalizeCharacterQuery(query string) (string, string, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "", "", fmt.Errorf("character query is required: %w", domain.ErrValidation)
	}
	canonical := whitespacePattern.ReplaceAllString(trimmed, " ")
	return canonical, strings.ToLower(canonical), nil
}

func buildCharacterGroup(queryText, queryKey string, results []CharacterSearchResult) *domain.CharacterGroup {
	limit := len(results)
	if limit > 10 {
		limit = 10
	}
	candidates := make([]domain.CharacterCandidate, 0, limit)
	for i := 0; i < limit; i++ {
		result := results[i]
		candidate := domain.CharacterCandidate{
			ID:       fmt.Sprintf("%s#%d", queryKey, i+1),
			PageURL:  result.PageURL,
			ImageURL: result.ImageURL,
		}
		if result.PreviewURL != "" {
			candidate.PreviewURL = &result.PreviewURL
		}
		if result.Title != "" {
			candidate.Title = &result.Title
		}
		if result.SourceLabel != "" {
			candidate.SourceLabel = &result.SourceLabel
		}
		candidates = append(candidates, candidate)
	}
	return &domain.CharacterGroup{
		Query:      queryText,
		QueryKey:   queryKey,
		Candidates: candidates,
	}
}

func containsCandidate(group *domain.CharacterGroup, candidateID string) bool {
	if group == nil {
		return false
	}
	for _, candidate := range group.Candidates {
		if candidate.ID == candidateID {
			return true
		}
	}
	return false
}

func queryKeyFromCandidateID(candidateID string) string {
	idx := strings.LastIndex(candidateID, "#")
	if idx <= 0 || idx == len(candidateID)-1 {
		return ""
	}
	if _, err := strconv.Atoi(candidateID[idx+1:]); err != nil {
		return ""
	}
	return candidateID[:idx]
}
