package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestCharacterSearchCache_GetOrCreate_CacheMissStoresResult(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewCharacterCacheStore(database)

	createCalls := 0
	group, err := store.GetOrCreate(context.Background(), "SCP-049", "scp-049", func(context.Context) (*domain.CharacterGroup, error) {
		createCalls++
		return &domain.CharacterGroup{
			Query:    "SCP-049",
			QueryKey: "scp-049",
			Candidates: []domain.CharacterCandidate{
				{ID: "scp-049#1", PageURL: "https://example.com/page", ImageURL: "https://example.com/image.jpg"},
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	testutil.AssertEqual(t, createCalls, 1)
	testutil.AssertEqual(t, group.QueryKey, "scp-049")

	cached, err := store.Get(context.Background(), "scp-049")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotJSON, err := json.Marshal(group)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	wantJSON, err := json.Marshal(cached)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	testutil.AssertJSONEq(t, string(gotJSON), string(wantJSON))
}

func TestCharacterSearchCache_GetOrCreate_CacheHitAvoidsExternalLookup(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewCharacterCacheStore(database)
	ctx := context.Background()

	if err := store.Put(ctx, &domain.CharacterGroup{
		Query:    "SCP-049",
		QueryKey: "scp-049",
		Candidates: []domain.CharacterCandidate{
			{ID: "scp-049#1", PageURL: "https://example.com/page", ImageURL: "https://example.com/image.jpg"},
		},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	createCalls := 0
	group, err := store.GetOrCreate(ctx, "SCP-049", "scp-049", func(context.Context) (*domain.CharacterGroup, error) {
		createCalls++
		return nil, errors.New("unexpected lookup")
	})
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	testutil.AssertEqual(t, createCalls, 0)
	testutil.AssertEqual(t, len(group.Candidates), 1)
}

func TestCharacterSearchCache_PersistsAcrossStoreReopen(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	tmp := t.TempDir() + "/cache.db"
	database, err := db.OpenDB(tmp)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	store := db.NewCharacterCacheStore(database)
	ctx := context.Background()

	if err := store.Put(ctx, &domain.CharacterGroup{
		Query:    "SCP-173",
		QueryKey: "scp-173",
		Candidates: []domain.CharacterCandidate{
			{ID: "scp-173#1", PageURL: "https://example.com/173", ImageURL: "https://example.com/173.jpg"},
		},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := db.OpenDB(tmp)
	if err != nil {
		t.Fatalf("OpenDB reopen: %v", err)
	}
	defer reopened.Close()

	reloaded, err := db.NewCharacterCacheStore(reopened).Get(ctx, "scp-173")
	if err != nil {
		t.Fatalf("Get reopened: %v", err)
	}
	testutil.AssertEqual(t, reloaded.QueryKey, "scp-173")
	testutil.AssertEqual(t, reloaded.Candidates[0].ID, "scp-173#1")
}
