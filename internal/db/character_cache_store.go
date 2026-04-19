package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// CharacterCacheStore persists normalized character search results in SQLite.
type CharacterCacheStore struct {
	db *sql.DB
}

// NewCharacterCacheStore creates a cache store backed by the provided DB.
func NewCharacterCacheStore(db *sql.DB) *CharacterCacheStore {
	return &CharacterCacheStore{db: db}
}

// Get returns a cached CharacterGroup for queryKey.
func (s *CharacterCacheStore) Get(ctx context.Context, queryKey string) (*domain.CharacterGroup, error) {
	var (
		queryText string
		rawJSON   string
	)
	if err := s.db.QueryRowContext(ctx,
		`SELECT query_text, result_json
		   FROM character_search_cache
		  WHERE query_key = ?`,
		queryKey,
	).Scan(&queryText, &rawJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("get character cache %s: %w", queryKey, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get character cache %s: %w", queryKey, err)
	}

	var group domain.CharacterGroup
	if err := json.Unmarshal([]byte(rawJSON), &group); err != nil {
		return nil, fmt.Errorf("get character cache %s: decode: %w", queryKey, err)
	}
	if group.Query == "" {
		group.Query = queryText
	}
	if group.QueryKey == "" {
		group.QueryKey = queryKey
	}
	return &group, nil
}

// Put upserts a CharacterGroup into the cache.
func (s *CharacterCacheStore) Put(ctx context.Context, group *domain.CharacterGroup) error {
	if group == nil {
		return fmt.Errorf("put character cache: %w", domain.ErrValidation)
	}
	if group.QueryKey == "" {
		return fmt.Errorf("put character cache: missing query key: %w", domain.ErrValidation)
	}
	rawJSON, err := json.Marshal(group)
	if err != nil {
		return fmt.Errorf("put character cache %s: encode: %w", group.QueryKey, err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO character_search_cache (query_key, query_text, result_json)
		 VALUES (?, ?, ?)
		 ON CONFLICT(query_key) DO UPDATE SET
		     query_text = excluded.query_text,
		     result_json = excluded.result_json,
		     updated_at = datetime('now')`,
		group.QueryKey,
		group.Query,
		string(rawJSON),
	)
	if err != nil {
		return fmt.Errorf("put character cache %s: %w", group.QueryKey, err)
	}
	return nil
}

// GetOrCreate returns a cached CharacterGroup or stores a freshly created one.
func (s *CharacterCacheStore) GetOrCreate(
	ctx context.Context,
	queryText string,
	queryKey string,
	create func(context.Context) (*domain.CharacterGroup, error),
) (*domain.CharacterGroup, error) {
	group, err := s.Get(ctx, queryKey)
	if err == nil {
		return group, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	group, err = create(ctx)
	if err != nil {
		return nil, err
	}
	if group.Query == "" {
		group.Query = queryText
	}
	if group.QueryKey == "" {
		group.QueryKey = queryKey
	}
	if err := s.Put(ctx, group); err != nil {
		return nil, err
	}
	return s.Get(ctx, queryKey)
}
