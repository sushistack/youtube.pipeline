package service_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type fakeCharacterSearchClient struct {
	calls   int
	results []service.CharacterSearchResult
	err     error
}

func (f *fakeCharacterSearchClient) SearchImages(ctx context.Context, query string, limit int) ([]service.CharacterSearchResult, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

func TestCharacterService_Search_ReturnsCharacterGroupWithTenCandidates(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	cacheStore := db.NewCharacterCacheStore(database)
	outDir := t.TempDir()
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	results := make([]service.CharacterSearchResult, 12)
	for i := range results {
		idx := strconv.Itoa(i + 1)
		results[i] = service.CharacterSearchResult{
			PageURL:     "https://example.com/page/" + idx,
			ImageURL:    "https://example.com/image/" + idx + ".jpg",
			PreviewURL:  "https://example.com/thumb/" + idx + ".jpg",
			Title:       "candidate " + idx,
			SourceLabel: "Example",
		}
	}
	client := &fakeCharacterSearchClient{results: results}
	svc := service.NewCharacterService(runStore, cacheStore, client)

	group, err := svc.Search(context.Background(), run.ID, "  SCP-049  ")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	testutil.AssertEqual(t, client.calls, 1)
	testutil.AssertEqual(t, group.QueryKey, "scp-049")
	testutil.AssertEqual(t, len(group.Candidates), 10)
	testutil.AssertEqual(t, group.Candidates[0].ID, "scp-049#1")
	testutil.AssertEqual(t, group.Candidates[9].ID, "scp-049#10")
}

func TestCharacterService_Pick_PersistsSelectedCharacterIDAndAdvancesRun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	cacheStore := db.NewCharacterCacheStore(database)
	outDir := t.TempDir()
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`UPDATE runs SET stage = 'character_pick', status = 'waiting' WHERE id = ?`,
		run.ID,
	); err != nil {
		t.Fatalf("seed stage: %v", err)
	}
	if err := cacheStore.Put(context.Background(), &domain.CharacterGroup{
		Query:    "SCP-049",
		QueryKey: "scp-049",
		Candidates: []domain.CharacterCandidate{
			{ID: "scp-049#1", PageURL: "https://example.com/page", ImageURL: "https://example.com/image.jpg"},
		},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := runStore.SetCharacterQueryKey(context.Background(), run.ID, "scp-049"); err != nil {
		t.Fatalf("SetCharacterQueryKey: %v", err)
	}

	svc := service.NewCharacterService(runStore, cacheStore, &fakeCharacterSearchClient{})
	updated, err := svc.Pick(context.Background(), run.ID, "scp-049#1")
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	testutil.AssertEqual(t, updated.Stage, domain.StageImage)
	testutil.AssertEqual(t, updated.Status, domain.StatusRunning)
	if updated.SelectedCharacterID == nil || *updated.SelectedCharacterID != "scp-049#1" {
		t.Fatalf("SelectedCharacterID = %v, want scp-049#1", updated.SelectedCharacterID)
	}
}

func TestCharacterService_Pick_RejectsUnknownCandidate(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	cacheStore := db.NewCharacterCacheStore(database)
	outDir := t.TempDir()
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`UPDATE runs SET stage = 'character_pick', status = 'waiting', character_query_key = 'scp-049' WHERE id = ?`,
		run.ID,
	); err != nil {
		t.Fatalf("seed stage: %v", err)
	}
	if err := cacheStore.Put(context.Background(), &domain.CharacterGroup{
		Query:    "SCP-049",
		QueryKey: "scp-049",
		Candidates: []domain.CharacterCandidate{
			{ID: "scp-049#1", PageURL: "https://example.com/page", ImageURL: "https://example.com/image.jpg"},
		},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	svc := service.NewCharacterService(runStore, cacheStore, &fakeCharacterSearchClient{})
	_, err = svc.Pick(context.Background(), run.ID, "scp-049#9")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestCharacterService_GetSelectedCandidate_ResolvesFromRunStateAndCache(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	cacheStore := db.NewCharacterCacheStore(database)
	outDir := t.TempDir()
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`UPDATE runs
		    SET character_query_key = 'scp-049',
		        selected_character_id = 'scp-049#2'
		  WHERE id = ?`,
		run.ID,
	); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if err := cacheStore.Put(context.Background(), &domain.CharacterGroup{
		Query:    "SCP-049",
		QueryKey: "scp-049",
		Candidates: []domain.CharacterCandidate{
			{ID: "scp-049#1", PageURL: "https://example.com/1", ImageURL: "https://example.com/1.jpg"},
			{ID: "scp-049#2", PageURL: "https://example.com/2", ImageURL: "https://example.com/2.jpg"},
		},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	svc := service.NewCharacterService(runStore, cacheStore, &fakeCharacterSearchClient{})
	candidate, err := svc.GetSelectedCandidate(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetSelectedCandidate: %v", err)
	}
	testutil.AssertEqual(t, candidate.ID, "scp-049#2")
	testutil.AssertEqual(t, candidate.PageURL, "https://example.com/2")
}

func TestCharacterService_GetSelectedCandidate_MissingCacheRowFailsLoudly(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	cacheStore := db.NewCharacterCacheStore(database)
	outDir := t.TempDir()
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`UPDATE runs
		    SET character_query_key = 'scp-049',
		        selected_character_id = 'scp-049#2'
		  WHERE id = ?`,
		run.ID,
	); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	svc := service.NewCharacterService(runStore, cacheStore, &fakeCharacterSearchClient{})
	_, err = svc.GetSelectedCandidate(context.Background(), run.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
