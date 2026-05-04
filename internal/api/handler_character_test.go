package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// characterHandlerFixture bundles the handler + the shared DB handle for tests
// that need to mutate schema state directly (e.g. advancing stage to
// character_pick or writing scenario_path before calling the handler).
type characterHandlerFixture struct {
	handler  *api.CharacterHandler
	runStore *db.RunStore
	cache    *db.CharacterCacheStore
	database *sql.DB
	outDir   string
}

func newCharacterHandlerFixture(t testing.TB) characterHandlerFixture {
	t.Helper()
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	cacheStore := db.NewCharacterCacheStore(database)
	svc := service.NewCharacterService(runStore, cacheStore, &fakeAPICharacterSearchClient{
		results: []service.CharacterSearchResult{
			{
				PageURL:     "https://example.com/page",
				ImageURL:    "https://example.com/image.jpg",
				PreviewURL:  "https://example.com/thumb.jpg",
				Title:       "Example Candidate",
				SourceLabel: "Example",
			},
		},
	})
	return characterHandlerFixture{
		handler:  api.NewCharacterHandler(svc),
		runStore: runStore,
		cache:    cacheStore,
		database: database,
		outDir:   t.TempDir(),
	}
}

type fakeAPICharacterSearchClient struct {
	results []service.CharacterSearchResult
}

func (f *fakeAPICharacterSearchClient) SearchImages(ctx context.Context, query string, limit int) ([]service.CharacterSearchResult, error) {
	return f.results, nil
}

func newTestCharacterHandler(t testing.TB) (*api.CharacterHandler, *db.RunStore, *db.CharacterCacheStore, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	cacheStore := db.NewCharacterCacheStore(database)
	svc := service.NewCharacterService(runStore, cacheStore, &fakeAPICharacterSearchClient{
		results: []service.CharacterSearchResult{
			{
				PageURL:     "https://example.com/page",
				ImageURL:    "https://example.com/image.jpg",
				PreviewURL:  "https://example.com/thumb.jpg",
				Title:       "Example Candidate",
				SourceLabel: "Example",
			},
		},
	})
	return api.NewCharacterHandler(svc), runStore, cacheStore, t.TempDir()
}

func TestCharacterHandler_Search_EncodesCharacterGroupEnvelope(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, runStore, _, outDir := newTestCharacterHandler(t)
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID+"/characters?query=SCP-049", nil)
	req.SetPathValue("id", run.ID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	var env struct {
		Version int `json:"version"`
		Data    *struct {
			Query      string `json:"query"`
			QueryKey   string `json:"query_key"`
			Candidates []struct {
				ID string `json:"id"`
			} `json:"candidates"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Version, 1)
	testutil.AssertEqual(t, env.Data.QueryKey, "scp-049")
	testutil.AssertEqual(t, len(env.Data.Candidates), 1)
	testutil.AssertEqual(t, env.Data.Candidates[0].ID, "scp-049#1")
}

func TestCharacterHandler_Pick_ReturnsConflictOutsideCharacterStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, runStore, cacheStore, outDir := newTestCharacterHandler(t)
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
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

	body, _ := json.Marshal(map[string]string{"candidate_id": "scp-049#1"})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/characters/pick", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", run.ID)
	rec := httptest.NewRecorder()
	h.Pick(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Error.Code, "CONFLICT")
}

func TestCharacterHandler_Search_EmptyQueryFallsBackToCache(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, runStore, cacheStore, outDir := newTestCharacterHandler(t)
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := cacheStore.Put(context.Background(), &domain.CharacterGroup{
		Query:    "SCP-049",
		QueryKey: "scp-049",
		Candidates: []domain.CharacterCandidate{
			{ID: "scp-049#1", PageURL: "https://example.com/p", ImageURL: "https://example.com/i.jpg"},
		},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := runStore.SetCharacterQueryKey(context.Background(), run.ID, "scp-049"); err != nil {
		t.Fatalf("SetCharacterQueryKey: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID+"/characters", nil)
	req.SetPathValue("id", run.ID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	var env struct {
		Data *struct {
			QueryKey   string `json:"query_key"`
			Candidates []struct {
				ID string `json:"id"`
			} `json:"candidates"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Data.QueryKey, "scp-049")
	testutil.AssertEqual(t, len(env.Data.Candidates), 1)
}

func TestCharacterHandler_Search_EmptyQueryReturns404WhenNoQueryKey(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, runStore, _, outDir := newTestCharacterHandler(t)
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID+"/characters", nil)
	req.SetPathValue("id", run.ID)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Error.Code, "NOT_FOUND")
}

func TestCharacterHandler_Pick_PersistsFrozenDescriptor(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := newCharacterHandlerFixture(t)
	ctx := context.Background()
	run, err := f.runStore.Create(ctx, "049", f.outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := f.cache.Put(ctx, &domain.CharacterGroup{
		Query:    "SCP-049",
		QueryKey: "scp-049",
		Candidates: []domain.CharacterCandidate{
			{ID: "scp-049#1", PageURL: "https://example.com/p", ImageURL: "https://example.com/i.jpg"},
		},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := f.database.ExecContext(ctx,
		`UPDATE runs SET stage = 'character_pick', status = 'waiting',
		 character_query_key = 'scp-049' WHERE id = ?`, run.ID); err != nil {
		t.Fatalf("seed stage: %v", err)
	}

	body, _ := json.Marshal(map[string]string{
		"candidate_id":      "scp-049#1",
		"frozen_descriptor": "appearance: plague doctor; environment: dim chamber",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/characters/pick", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", run.ID)
	rec := httptest.NewRecorder()
	f.handler.Pick(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	reloaded, err := f.runStore.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if reloaded.FrozenDescriptor == nil || *reloaded.FrozenDescriptor != "appearance: plague doctor; environment: dim chamber" {
		t.Fatalf("FrozenDescriptor = %v", reloaded.FrozenDescriptor)
	}
}

func TestCharacterHandler_Descriptor_ReturnsAutoWithoutPrior(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := newCharacterHandlerFixture(t)
	ctx := context.Background()
	run, err := f.runStore.Create(ctx, "049", f.outDir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	runDir := filepath.Join(f.outDir, run.ID)
	scenarioPath := filepath.Join(runDir, "scenario.json")
	if err := os.WriteFile(scenarioPath,
		// Phase A serializes the breakdowner output under "visual_script"
		// (renamed in D2 from the legacy "visual_breakdown" key). The
		// CharacterService prefill reads visual_script.frozen_descriptor.
		[]byte(`{"visual_script":{"frozen_descriptor":"auto-from-artifact"}}`),
		0o644,
	); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	if _, err := f.database.ExecContext(ctx,
		`UPDATE runs SET scenario_path = ? WHERE id = ?`, scenarioPath, run.ID,
	); err != nil {
		t.Fatalf("seed scenario_path: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID+"/characters/descriptor", nil)
	req.SetPathValue("id", run.ID)
	rec := httptest.NewRecorder()
	f.handler.Descriptor(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	var env struct {
		Data *struct {
			Auto  string  `json:"auto"`
			Prior *string `json:"prior"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Data.Auto, "auto-from-artifact")
	if env.Data.Prior != nil {
		t.Fatalf("expected nil prior, got %q", *env.Data.Prior)
	}
}
