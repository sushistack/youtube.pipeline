package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestScpImageLibrary_GetMissing_ReturnsNotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := db.NewScpImageLibraryStore(testutil.NewTestDB(t))

	_, err := store.Get(context.Background(), "SCP-049")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestScpImageLibrary_UpsertInsert_RoundTrip(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := db.NewScpImageLibraryStore(testutil.NewTestDB(t))
	ctx := context.Background()

	rec, err := store.Upsert(ctx, &domain.ScpImageRecord{
		ScpID:             "SCP-049",
		FilePath:          "SCP-049/canonical.png",
		SourceRefURL:      "https://example.com/049.jpg",
		SourceQueryKey:    "scp-049",
		SourceCandidateID: "scp-049#1", FrozenDescriptor: "appearance: ...",
		PromptUsed:        "cartoon style; A plague doctor",
		Seed:              42,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	testutil.AssertEqual(t, rec.Version, 1)
	testutil.AssertEqual(t, rec.FilePath, "SCP-049/canonical.png")
	testutil.AssertEqual(t, rec.Seed, int64(42))

	got, err := store.Get(ctx, "SCP-049")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	testutil.AssertEqual(t, got.ScpID, "SCP-049")
	testutil.AssertEqual(t, got.PromptUsed, "cartoon style; A plague doctor")
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Fatalf("expected timestamps populated, got %+v", got)
	}
}

func TestScpImageLibrary_UpsertConflict_BumpsVersion(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := db.NewScpImageLibraryStore(testutil.NewTestDB(t))
	ctx := context.Background()

	if _, err := store.Upsert(ctx, &domain.ScpImageRecord{
		ScpID: "SCP-049", FilePath: "SCP-049/canonical.png",
		SourceRefURL: "https://example.com/v1.jpg", SourceQueryKey: "scp-049",
		SourceCandidateID: "scp-049#1", FrozenDescriptor: "appearance: ...",
		PromptUsed:        "v1", Seed: 1,
	}); err != nil {
		t.Fatalf("Upsert v1: %v", err)
	}

	updated, err := store.Upsert(ctx, &domain.ScpImageRecord{
		ScpID: "SCP-049", FilePath: "SCP-049/canonical.png",
		SourceRefURL: "https://example.com/v2.jpg", SourceQueryKey: "scp-049",
		SourceCandidateID: "scp-049#3", FrozenDescriptor: "v2",
		PromptUsed:        "v2", Seed: 99,
	})
	if err != nil {
		t.Fatalf("Upsert v2: %v", err)
	}
	testutil.AssertEqual(t, updated.Version, 2)
	testutil.AssertEqual(t, updated.SourceRefURL, "https://example.com/v2.jpg")
	testutil.AssertEqual(t, updated.Seed, int64(99))
}

func TestScpImageLibrary_Delete_Idempotent(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := db.NewScpImageLibraryStore(testutil.NewTestDB(t))
	ctx := context.Background()

	if _, err := store.Upsert(ctx, &domain.ScpImageRecord{
		ScpID: "SCP-049", FilePath: "SCP-049/canonical.png",
		SourceRefURL: "u", SourceQueryKey: "scp-049",
		SourceCandidateID: "scp-049#1", FrozenDescriptor: "f",
		PromptUsed: "p", Seed: 1,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := store.Delete(ctx, "SCP-049"); err != nil {
		t.Fatalf("Delete first: %v", err)
	}
	if err := store.Delete(ctx, "SCP-049"); err != nil {
		t.Fatalf("Delete second (should be idempotent): %v", err)
	}
	if _, err := store.Get(ctx, "SCP-049"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestScpImageLibrary_Validation_RejectsEmptyKeys(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	store := db.NewScpImageLibraryStore(testutil.NewTestDB(t))
	ctx := context.Background()

	if _, err := store.Get(ctx, ""); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Get(\"\"): expected ErrValidation, got %v", err)
	}
	if _, err := store.Upsert(ctx, &domain.ScpImageRecord{ScpID: "", FilePath: "x"}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Upsert empty scp_id: expected ErrValidation, got %v", err)
	}
	if _, err := store.Upsert(ctx, &domain.ScpImageRecord{ScpID: "X", FilePath: ""}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Upsert empty file_path: expected ErrValidation, got %v", err)
	}
	if err := store.Delete(ctx, ""); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Delete(\"\"): expected ErrValidation, got %v", err)
	}
}
