package service_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

type fakeImageGen struct {
	mu        sync.Mutex
	editCalls []domain.ImageEditRequest
	editErr   error
	body      []byte
	echoSeed  bool
}

func (f *fakeImageGen) Generate(ctx context.Context, req domain.ImageRequest) (domain.ImageResponse, error) {
	return domain.ImageResponse{}, errors.New("Generate not used in scp image tests")
}

func (f *fakeImageGen) Edit(ctx context.Context, req domain.ImageEditRequest) (domain.ImageResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.editErr != nil {
		return domain.ImageResponse{}, f.editErr
	}
	f.editCalls = append(f.editCalls, req)
	if req.OutputPath != "" {
		if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0o755); err != nil {
			return domain.ImageResponse{}, err
		}
		body := f.body
		if body == nil {
			body = []byte("png-bytes")
		}
		if err := os.WriteFile(req.OutputPath, body, 0o644); err != nil {
			return domain.ImageResponse{}, err
		}
	}
	resp := domain.ImageResponse{
		ImagePath: req.OutputPath,
		Model:     req.Model,
		Provider:  "test",
		CostUSD:   0.04,
	}
	if f.echoSeed {
		resp.Seed = req.Seed
	}
	return resp, nil
}

func setupCanonicalRun(t *testing.T, scpID string) (*sql.DB, *db.RunStore, *db.CharacterCacheStore, *db.ScpImageLibraryStore, *domain.Run) {
	t.Helper()
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	cacheStore := db.NewCharacterCacheStore(database)
	libStore := db.NewScpImageLibraryStore(database)
	ctx := context.Background()

	run, err := runStore.Create(ctx, scpID, t.TempDir())
	if err != nil {
		t.Fatalf("Create run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET stage='character_pick', status='waiting',
		     character_query_key='scp-049', selected_character_id='scp-049#1',
		     frozen_descriptor='Appearance: tall plague doctor in black robes.'
		   WHERE id = ?`, run.ID); err != nil {
		t.Fatalf("seed run state: %v", err)
	}
	if err := cacheStore.Put(ctx, &domain.CharacterGroup{
		Query: "SCP-049", QueryKey: "scp-049",
		Candidates: []domain.CharacterCandidate{
			{ID: "scp-049#1", PageURL: "https://e.com/p", ImageURL: "https://e.com/049.jpg"},
		},
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	updated, err := runStore.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	return database, runStore, cacheStore, libStore, updated
}

func newCanonicalService(t *testing.T, runStore *db.RunStore, cache *db.CharacterCacheStore, lib *db.ScpImageLibraryStore, gen domain.ImageGenerator, dir string) *service.ScpImageService {
	t.Helper()
	svc, err := service.NewScpImageService(service.ScpImageServiceConfig{
		Runs:           runStore,
		Cache:          cache,
		Library:        lib,
		Images:         gen,
		EditModel:      "qwen-image-edit",
		StylePrompt:    "Kid-friendly cartoon",
		ScpImageDir:    dir,
		CanonicalWidth: 1280,
		CanonicalHt:    720,
	})
	if err != nil {
		t.Fatalf("NewScpImageService: %v", err)
	}
	return svc
}

func TestScpImageService_Generate_NewRecord_WritesFileAndRow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, runStore, cache, lib, run := setupCanonicalRun(t, "SCP-049")
	imgDir := t.TempDir()
	gen := &fakeImageGen{echoSeed: true}
	svc := newCanonicalService(t, runStore, cache, lib, gen, imgDir)

	rec, err := svc.Generate(context.Background(), run.ID, service.GenerateCanonicalInput{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	testutil.AssertEqual(t, rec.ScpID, "SCP-049")
	testutil.AssertEqual(t, rec.FilePath, filepath.Join("SCP-049", "canonical.png"))
	testutil.AssertEqual(t, rec.Version, 1)
	if rec.Seed == 0 {
		t.Fatalf("expected non-zero seed propagated from provider")
	}
	if !strings.Contains(rec.PromptUsed, "Kid-friendly cartoon") || !strings.Contains(rec.PromptUsed, "plague doctor") {
		t.Fatalf("composed prompt missing parts: %q", rec.PromptUsed)
	}
	if _, err := os.Stat(filepath.Join(imgDir, rec.FilePath)); err != nil {
		t.Fatalf("canonical file missing: %v", err)
	}
	if len(gen.editCalls) != 1 {
		t.Fatalf("expected 1 Edit call, got %d", len(gen.editCalls))
	}
	call := gen.editCalls[0]
	testutil.AssertEqual(t, call.Width, 1280)
	testutil.AssertEqual(t, call.Height, 720)
	testutil.AssertEqual(t, call.ReferenceImageURL, "https://e.com/049.jpg")
}

func TestScpImageService_Generate_HitWithoutRegenerate_IsIdempotent(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, runStore, cache, lib, run := setupCanonicalRun(t, "SCP-049")
	imgDir := t.TempDir()
	gen := &fakeImageGen{echoSeed: true}
	svc := newCanonicalService(t, runStore, cache, lib, gen, imgDir)

	first, err := svc.Generate(context.Background(), run.ID, service.GenerateCanonicalInput{})
	if err != nil {
		t.Fatalf("Generate first: %v", err)
	}
	second, err := svc.Generate(context.Background(), run.ID, service.GenerateCanonicalInput{})
	if err != nil {
		t.Fatalf("Generate second: %v", err)
	}
	testutil.AssertEqual(t, second.Version, first.Version)
	if len(gen.editCalls) != 1 {
		t.Fatalf("expected only 1 Edit call (idempotent), got %d", len(gen.editCalls))
	}
}

func TestScpImageService_Generate_Regenerate_BumpsVersion(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, runStore, cache, lib, run := setupCanonicalRun(t, "SCP-049")
	imgDir := t.TempDir()
	gen := &fakeImageGen{echoSeed: true}
	svc := newCanonicalService(t, runStore, cache, lib, gen, imgDir)

	first, err := svc.Generate(context.Background(), run.ID, service.GenerateCanonicalInput{})
	if err != nil {
		t.Fatalf("first Generate: %v", err)
	}
	gen.body = []byte("new-png-bytes")

	regen, err := svc.Generate(context.Background(), run.ID, service.GenerateCanonicalInput{Regenerate: true})
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	testutil.AssertEqual(t, regen.Version, first.Version+1)
	if len(gen.editCalls) != 2 {
		t.Fatalf("expected 2 Edit calls, got %d", len(gen.editCalls))
	}
	contents, err := os.ReadFile(filepath.Join(imgDir, regen.FilePath))
	if err != nil {
		t.Fatalf("read regenerated file: %v", err)
	}
	if string(contents) != "new-png-bytes" {
		t.Fatalf("regenerate did not overwrite file, got %q", contents)
	}
}

func TestScpImageService_Generate_RefFetcher_RewritesURL(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, runStore, cache, lib, run := setupCanonicalRun(t, "SCP-049")
	imgDir := t.TempDir()
	gen := &fakeImageGen{echoSeed: true}
	svc, err := service.NewScpImageService(service.ScpImageServiceConfig{
		Runs:    runStore,
		Cache:   cache,
		Library: lib,
		Images:  gen,
		RefFetcher: func(_ context.Context, url string) (string, error) {
			return "data:image/png;base64,FAKE-" + url, nil
		},
		EditModel:      "qwen-image-edit",
		StylePrompt:    "Kid-friendly cartoon",
		ScpImageDir:    imgDir,
		CanonicalWidth: 1280,
		CanonicalHt:    720,
	})
	if err != nil {
		t.Fatalf("NewScpImageService: %v", err)
	}

	rec, err := svc.Generate(context.Background(), run.ID, service.GenerateCanonicalInput{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(gen.editCalls[0].ReferenceImageURL, "data:image/png;base64,FAKE-") {
		t.Fatalf("expected refFetcher to rewrite URL, got %q", gen.editCalls[0].ReferenceImageURL)
	}
	// The library row records the original DDG URL, not the rewritten data URL.
	testutil.AssertEqual(t, rec.SourceRefURL, "https://e.com/049.jpg")
}

func TestScpImageService_Generate_EditFails_NoLibraryRow(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, runStore, cache, lib, run := setupCanonicalRun(t, "SCP-049")
	imgDir := t.TempDir()
	gen := &fakeImageGen{editErr: errors.New("upstream timeout")}
	svc := newCanonicalService(t, runStore, cache, lib, gen, imgDir)

	_, err := svc.Generate(context.Background(), run.ID, service.GenerateCanonicalInput{})
	if err == nil {
		t.Fatalf("expected error from failing Edit")
	}
	if _, err := lib.Get(context.Background(), "SCP-049"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("library row must not exist after Edit failure, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(imgDir, "SCP-049", "canonical.png")); err == nil {
		t.Fatalf("canonical file must not exist after Edit failure")
	}
}

func TestScpImageService_Generate_RequiresSelectedCandidate(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database, runStore, cache, lib, run := setupCanonicalRun(t, "SCP-049")
	imgDir := t.TempDir()

	// Clear selected_character_id to simulate "pick was not done yet".
	if _, err := database.ExecContext(context.Background(),
		`UPDATE runs SET selected_character_id = NULL WHERE id = ?`, run.ID); err != nil {
		t.Fatalf("clear selected: %v", err)
	}

	gen := &fakeImageGen{}
	svc := newCanonicalService(t, runStore, cache, lib, gen, imgDir)

	_, err := svc.Generate(context.Background(), run.ID, service.GenerateCanonicalInput{})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation when no selected candidate, got %v", err)
	}
}

func TestScpImageService_GetByRun_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	_, runStore, cache, lib, run := setupCanonicalRun(t, "SCP-049")
	imgDir := t.TempDir()
	svc := newCanonicalService(t, runStore, cache, lib, &fakeImageGen{}, imgDir)

	_, err := svc.GetByRun(context.Background(), run.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestScpImageService_IsValidSCPID(t *testing.T) {
	cases := map[string]bool{
		"SCP-049":             true,
		"scp-173":             true,
		"49_alpha.v2":         true,
		"":                    false,
		"../etc/passwd":       false,
		"SCP/049":             false,
		"SCP\\049":            false,
		"SCP 049":             false,
		// Pure-symbol strings would collide at the scp_image_dir root.
		".":                     false,
		"-":                     false,
		"_":                     false,
		"-_-":                   false,
		strings.Repeat("a", 65): false,
	}
	for input, want := range cases {
		got := service.IsValidSCPID(input)
		if got != want {
			t.Errorf("IsValidSCPID(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestScpImageService_Generate_StripsSceneSegmentsFromPrompt(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database, runStore, cache, lib, run := setupCanonicalRun(t, "SCP-049")
	imgDir := t.TempDir()

	// Replace the simple frozen descriptor with a full Phase A-shape one
	// containing every standard label so we can assert the scene segments
	// are filtered out of the prompt sent to image-edit while still
	// being preserved verbatim on the library row.
	full := "Appearance: tall plague doctor in black robes" +
		"; Distinguishing features: ceramic beak mask, doctor's bag" +
		"; Environment: sterile humanoid containment cell" +
		"; Key visual moments: SCP-049 performing surgery, the entity reading a journal"
	if _, err := database.ExecContext(context.Background(),
		`UPDATE runs SET frozen_descriptor = ? WHERE id = ?`, full, run.ID); err != nil {
		t.Fatalf("seed full descriptor: %v", err)
	}

	gen := &fakeImageGen{echoSeed: true}
	svc := newCanonicalService(t, runStore, cache, lib, gen, imgDir)

	rec, err := svc.Generate(context.Background(), run.ID, service.GenerateCanonicalInput{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(gen.editCalls) != 1 {
		t.Fatalf("expected 1 Edit call, got %d", len(gen.editCalls))
	}
	prompt := gen.editCalls[0].Prompt
	if !strings.Contains(prompt, "plague doctor") || !strings.Contains(prompt, "ceramic beak mask") {
		t.Fatalf("character segments missing from prompt: %q", prompt)
	}
	if strings.Contains(prompt, "Environment:") || strings.Contains(prompt, "Key visual moments:") ||
		strings.Contains(prompt, "containment cell") || strings.Contains(prompt, "performing surgery") {
		t.Fatalf("scene segments leaked into prompt: %q", prompt)
	}
	// Library row preserves the full descriptor (Phase B's per-shot prompts
	// still need the unfiltered version).
	if rec.FrozenDescriptor != full {
		t.Fatalf("library row should preserve full descriptor, got %q", rec.FrozenDescriptor)
	}
}
