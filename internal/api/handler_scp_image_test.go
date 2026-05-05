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

// fakeScpImageGen is the bare-minimum domain.ImageGenerator for the handler
// tests. Edit() writes deterministic bytes so the static endpoint can verify
// the served file is the one the service produced.
type fakeScpImageGen struct{}

func (fakeScpImageGen) Generate(ctx context.Context, _ domain.ImageRequest) (domain.ImageResponse, error) {
	return domain.ImageResponse{}, nil
}

func (fakeScpImageGen) Edit(_ context.Context, req domain.ImageEditRequest) (domain.ImageResponse, error) {
	if req.OutputPath == "" {
		return domain.ImageResponse{}, nil
	}
	if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0o755); err != nil {
		return domain.ImageResponse{}, err
	}
	if err := os.WriteFile(req.OutputPath, []byte("png-bytes"), 0o644); err != nil {
		return domain.ImageResponse{}, err
	}
	return domain.ImageResponse{ImagePath: req.OutputPath, Provider: "test", Seed: req.Seed}, nil
}

type scpImageHandlerFixture struct {
	handler  *api.ScpImageHandler
	runStore *db.RunStore
	database *sql.DB
	imageDir string
}

func newScpImageHandlerFixture(t *testing.T) scpImageHandlerFixture {
	t.Helper()
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	cacheStore := db.NewCharacterCacheStore(database)
	libStore := db.NewScpImageLibraryStore(database)
	imageDir := t.TempDir()
	svc, err := service.NewScpImageService(service.ScpImageServiceConfig{
		Runs:           runStore,
		Cache:          cacheStore,
		Library:        libStore,
		Images:         fakeScpImageGen{},
		EditModel:      "qwen-image-edit",
		StylePrompt:    "Kid-friendly cartoon",
		ScpImageDir:    imageDir,
		CanonicalWidth: 1280,
		CanonicalHt:    720,
	})
	if err != nil {
		t.Fatalf("NewScpImageService: %v", err)
	}
	return scpImageHandlerFixture{
		handler:  api.NewScpImageHandler(svc, imageDir),
		runStore: runStore,
		database: database,
		imageDir: imageDir,
	}
}

func seedCanonicalReadyRun(t *testing.T, f scpImageHandlerFixture) *domain.Run {
	t.Helper()
	ctx := context.Background()
	run, err := f.runStore.Create(ctx, "SCP-049", t.TempDir())
	if err != nil {
		t.Fatalf("Create run: %v", err)
	}
	if _, err := f.database.ExecContext(ctx,
		`UPDATE runs SET stage='character_pick', status='waiting',
		     character_query_key='scp-049', selected_character_id='scp-049#1',
		     frozen_descriptor='Appearance: tall plague doctor.'
		   WHERE id = ?`, run.ID); err != nil {
		t.Fatalf("seed run state: %v", err)
	}
	cacheStore := db.NewCharacterCacheStore(f.database)
	if err := cacheStore.Put(ctx, &domain.CharacterGroup{
		Query: "SCP-049", QueryKey: "scp-049",
		Candidates: []domain.CharacterCandidate{
			{ID: "scp-049#1", PageURL: "https://e.com/p", ImageURL: "https://e.com/049.jpg"},
		},
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	updated, err := f.runStore.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	return updated
}

func TestScpImageHandler_Get_NotFoundWhenNoCanonical(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := newScpImageHandlerFixture(t)
	run := seedCanonicalReadyRun(t, f)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID+"/characters/canonical", nil)
	req.SetPathValue("id", run.ID)
	rec := httptest.NewRecorder()
	f.handler.Get(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
}

func TestScpImageHandler_Generate_CreatesRecordAndStaticServes(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := newScpImageHandlerFixture(t)
	run := seedCanonicalReadyRun(t, f)

	body, _ := json.Marshal(map[string]bool{"regenerate": false})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/characters/canonical", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", run.ID)
	rec := httptest.NewRecorder()
	f.handler.Generate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Generate status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data *struct {
			ScpID    string `json:"scp_id"`
			ImageURL string `json:"image_url"`
			Version  int    `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Data.ScpID, "SCP-049")
	testutil.AssertEqual(t, env.Data.ImageURL, "/api/scp_images/SCP-049")
	testutil.AssertEqual(t, env.Data.Version, 1)

	// Subsequent GET /characters/canonical now returns the record.
	getReq := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID+"/characters/canonical", nil)
	getReq.SetPathValue("id", run.ID)
	getRec := httptest.NewRecorder()
	f.handler.Get(getRec, getReq)
	testutil.AssertEqual(t, getRec.Code, http.StatusOK)

	// Static serve returns the bytes the fake provider wrote.
	staticReq := httptest.NewRequest(http.MethodGet, "/api/scp_images/SCP-049", nil)
	staticReq.SetPathValue("scp_id", "SCP-049")
	staticRec := httptest.NewRecorder()
	f.handler.Static(staticRec, staticReq)
	testutil.AssertEqual(t, staticRec.Code, http.StatusOK)
	if staticRec.Body.String() != "png-bytes" {
		t.Fatalf("static body = %q, want png-bytes", staticRec.Body.String())
	}
	if got := staticRec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
}

func TestScpImageHandler_Generate_RegenerateBumpsVersion(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := newScpImageHandlerFixture(t)
	run := seedCanonicalReadyRun(t, f)

	post := func(regen bool) int {
		body, _ := json.Marshal(map[string]bool{"regenerate": regen})
		req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/characters/canonical", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", run.ID)
		rec := httptest.NewRecorder()
		f.handler.Generate(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Generate(regen=%v) status=%d body=%s", regen, rec.Code, rec.Body.String())
		}
		var env struct {
			Data *struct {
				Version int `json:"version"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		return env.Data.Version
	}

	v1 := post(false)
	v1again := post(false)
	testutil.AssertEqual(t, v1again, v1) // idempotent
	v2 := post(true)
	if v2 != v1+1 {
		t.Fatalf("regenerate version = %d, want %d", v2, v1+1)
	}
}

func TestScpImageHandler_Static_RejectsTraversal(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := newScpImageHandlerFixture(t)

	for _, scpID := range []string{"../etc/passwd", "..", "scp/049", "scp\\049", ""} {
		req := httptest.NewRequest(http.MethodGet, "/api/scp_images/"+scpID, nil)
		req.SetPathValue("scp_id", scpID)
		rec := httptest.NewRecorder()
		f.handler.Static(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("Static(%q) status=%d, want 400", scpID, rec.Code)
		}
	}
}

func TestScpImageHandler_Static_NotFoundWhenNoFile(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	f := newScpImageHandlerFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/api/scp_images/SCP-NEVER", nil)
	req.SetPathValue("scp_id", "SCP-NEVER")
	rec := httptest.NewRecorder()
	f.handler.Static(rec, req)
	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
}
