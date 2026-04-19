package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func newTestSceneHandler(t testing.TB) (*api.SceneHandler, *sql.DB, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)
	svc := service.NewSceneService(runStore, segStore)
	return api.NewSceneHandler(svc), database, t.TempDir()
}

// seedScenarioReviewRun creates a run and sets it to scenario_review/waiting.
func seedScenarioReviewRun(t testing.TB, database *sql.DB, outDir string) string {
	t.Helper()
	runStore := db.NewRunStore(database)
	ctx := context.Background()
	run, err := runStore.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET stage = 'scenario_review', status = 'waiting' WHERE id = ?`, run.ID); err != nil {
		t.Fatalf("seed stage/status: %v", err)
	}
	return run.ID
}

// seedSegment inserts a segment row with narration for a given run.
func seedSegment(t testing.TB, database *sql.DB, runID string, sceneIndex int, narration string) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO segments (run_id, scene_index, narration, status) VALUES (?, ?, ?, 'pending')`,
		runID, sceneIndex, narration,
	); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
}

// ── GET /api/runs/{id}/scenes ────────────────────────────────────────────────

func TestSceneHandler_List_ReturnsScenes(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedScenarioReviewRun(t, database, outDir)
	seedSegment(t, database, runID, 0, "첫 번째 장면 나레이션")
	seedSegment(t, database, runID, 1, "두 번째 장면 나레이션")

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/scenes", nil)
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)

	var env struct {
		Version int `json:"version"`
		Data    *struct {
			Items []struct {
				SceneIndex int    `json:"scene_index"`
				Narration  string `json:"narration"`
			} `json:"items"`
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Version, 1)
	testutil.AssertEqual(t, env.Data.Total, 2)
	testutil.AssertEqual(t, env.Data.Items[0].SceneIndex, 0)
	testutil.AssertEqual(t, env.Data.Items[0].Narration, "첫 번째 장면 나레이션")
	testutil.AssertEqual(t, env.Data.Items[1].SceneIndex, 1)
}

func TestSceneHandler_List_ReturnsConflictWhenRunNotAtScenarioReview(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runStore := db.NewRunStore(database)
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID+"/scenes", nil)
	req.SetPathValue("id", run.ID)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
}

func TestSceneHandler_List_ReturnsNotFoundForUnknownRun(t *testing.T) {
	h, _, _ := newTestSceneHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/no-such-run/scenes", nil)
	req.SetPathValue("id", "no-such-run")
	rec := httptest.NewRecorder()
	h.List(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
}

// ── POST /api/runs/{id}/scenes/{idx}/edit ───────────────────────────────────

func TestSceneHandler_Edit_PersistsNarrationUpdate(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedScenarioReviewRun(t, database, outDir)
	seedSegment(t, database, runID, 0, "원래 나레이션")

	body, _ := json.Marshal(map[string]string{"narration": "수정된 나레이션"})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/scenes/0/edit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	req.SetPathValue("idx", "0")
	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)

	var env struct {
		Version int `json:"version"`
		Data    *struct {
			SceneIndex int    `json:"scene_index"`
			Narration  string `json:"narration"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Version, 1)
	testutil.AssertEqual(t, env.Data.Narration, "수정된 나레이션")
}

func TestSceneHandler_Edit_ReturnsConflictWhenRunNotAtScenarioReview(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runStore := db.NewRunStore(database)
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"narration": "some text"})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/scenes/0/edit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", run.ID)
	req.SetPathValue("idx", "0")
	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
}

func TestSceneHandler_Edit_ReturnsNotFoundForMissingScene(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedScenarioReviewRun(t, database, outDir)

	body, _ := json.Marshal(map[string]string{"narration": "text"})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/scenes/99/edit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	req.SetPathValue("idx", "99")
	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
}

func TestSceneHandler_Edit_ReturnsValidationErrorForEmptyNarration(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedScenarioReviewRun(t, database, outDir)
	seedSegment(t, database, runID, 0, "기존 나레이션")

	body, _ := json.Marshal(map[string]string{"narration": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/scenes/0/edit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	req.SetPathValue("idx", "0")
	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
}

func TestSceneHandler_Edit_ReturnsValidationErrorForInvalidIndex(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedScenarioReviewRun(t, database, outDir)

	body, _ := json.Marshal(map[string]string{"narration": "text"})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/scenes/abc/edit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	req.SetPathValue("idx", "abc")
	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
}
