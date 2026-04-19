package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

type CharacterRunStore interface {
	Get(ctx context.Context, id string) (*domain.Run, error)
	SetCharacterQueryKey(ctx context.Context, id, queryKey string) error
	ApplyCharacterPick(ctx context.Context, id, queryKey, selectedCharacterID string, stage domain.Stage, status domain.Status) error
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

// CharacterService orchestrates DDG-backed search, cache reuse, and operator picks.
type CharacterService struct {
	runs   CharacterRunStore
	cache  CharacterCacheStore
	client CharacterSearchClient
}

func NewCharacterService(runs CharacterRunStore, cache CharacterCacheStore, client CharacterSearchClient) *CharacterService {
	return &CharacterService{runs: runs, cache: cache, client: client}
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

func (s *CharacterService) Pick(ctx context.Context, runID, candidateID string) (*domain.Run, error) {
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
	if err := s.runs.ApplyCharacterPick(ctx, runID, queryKey, candidateID, nextStage, pipeline.StatusForStage(nextStage)); err != nil {
		return nil, fmt.Errorf("character pick: persist selection: %w", err)
	}
	updated, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("character pick: reload run: %w", err)
	}
	return updated, nil
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
