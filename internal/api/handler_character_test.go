package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

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
