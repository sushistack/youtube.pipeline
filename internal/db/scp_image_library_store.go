package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// ScpImageLibraryStore persists canonical cartoon images per SCP_ID.
// FilePath is stored relative to PipelineConfig.ScpImageDir; resolution is
// the caller's responsibility so the store remains path-agnostic.
type ScpImageLibraryStore struct {
	db *sql.DB
}

// NewScpImageLibraryStore creates a store backed by the provided DB.
func NewScpImageLibraryStore(db *sql.DB) *ScpImageLibraryStore {
	return &ScpImageLibraryStore{db: db}
}

// Get returns the canonical record for scpID. Returns ErrNotFound when no
// row is present.
func (s *ScpImageLibraryStore) Get(ctx context.Context, scpID string) (*domain.ScpImageRecord, error) {
	if scpID == "" {
		return nil, fmt.Errorf("get scp image: %w: scp_id is empty", domain.ErrValidation)
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT scp_id, file_path, source_ref_url, source_query_key,
		        source_candidate_id, frozen_descriptor, prompt_used, seed,
		        version, created_at, updated_at
		   FROM scp_image_library
		  WHERE scp_id = ?`,
		scpID,
	)
	var rec domain.ScpImageRecord
	if err := row.Scan(
		&rec.ScpID, &rec.FilePath, &rec.SourceRefURL, &rec.SourceQueryKey,
		&rec.SourceCandidateID, &rec.FrozenDescriptor, &rec.PromptUsed,
		&rec.Seed, &rec.Version, &rec.CreatedAt, &rec.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("get scp image %s: %w", scpID, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get scp image %s: %w", scpID, err)
	}
	return &rec, nil
}

// Upsert creates a new row or updates an existing one. On update, version is
// increased by 1 atomically; the caller passes Version=0 (or any value — it's
// recomputed from the prior row + 1). The trigger rewrites updated_at.
func (s *ScpImageLibraryStore) Upsert(ctx context.Context, rec *domain.ScpImageRecord) (*domain.ScpImageRecord, error) {
	if rec == nil {
		return nil, fmt.Errorf("upsert scp image: %w: record is nil", domain.ErrValidation)
	}
	if rec.ScpID == "" {
		return nil, fmt.Errorf("upsert scp image: %w: scp_id is empty", domain.ErrValidation)
	}
	if rec.FilePath == "" {
		return nil, fmt.Errorf("upsert scp image: %w: file_path is empty", domain.ErrValidation)
	}
	// SQLite ON CONFLICT clause supports column-level expressions referencing
	// the existing row; we read OLD.version via the `scp_image_library` alias
	// in the SET list to bump on update.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scp_image_library
		    (scp_id, file_path, source_ref_url, source_query_key,
		     source_candidate_id, frozen_descriptor, prompt_used,
		     seed, version)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
		 ON CONFLICT(scp_id) DO UPDATE SET
		     file_path           = excluded.file_path,
		     source_ref_url      = excluded.source_ref_url,
		     source_query_key    = excluded.source_query_key,
		     source_candidate_id = excluded.source_candidate_id,
		     frozen_descriptor   = excluded.frozen_descriptor,
		     prompt_used         = excluded.prompt_used,
		     seed                = excluded.seed,
		     version             = scp_image_library.version + 1,
		     updated_at          = datetime('now')`,
		rec.ScpID, rec.FilePath, rec.SourceRefURL, rec.SourceQueryKey,
		rec.SourceCandidateID, rec.FrozenDescriptor, rec.PromptUsed, rec.Seed,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert scp image %s: %w", rec.ScpID, err)
	}
	return s.Get(ctx, rec.ScpID)
}

// Delete removes the row for scpID. Missing rows are not an error (idempotent).
func (s *ScpImageLibraryStore) Delete(ctx context.Context, scpID string) error {
	if scpID == "" {
		return fmt.Errorf("delete scp image: %w: scp_id is empty", domain.ErrValidation)
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM scp_image_library WHERE scp_id = ?`, scpID,
	); err != nil {
		return fmt.Errorf("delete scp image %s: %w", scpID, err)
	}
	return nil
}
