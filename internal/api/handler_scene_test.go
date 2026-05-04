package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func newTestSceneHandler(t testing.TB) (*api.SceneHandler, *sql.DB, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)
	svc := service.NewSceneService(runStore, segStore, db.NewDecisionStore(database), clock.RealClock{})
	svc.SetSceneRegenerator(service.NewNoOpSceneRegenerator(segStore))
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

func seedBatchReviewRun(t testing.TB, database *sql.DB, outDir string, scenarioPath string) string {
	t.Helper()
	runStore := db.NewRunStore(database)
	ctx := context.Background()
	run, err := runStore.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET stage = 'batch_review', status = 'waiting', scenario_path = ? WHERE id = ?`, scenarioPath, run.ID); err != nil {
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

func writeScenarioFixture(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scenario.json")
	// v2 narration: Acts/BeatAnchor structure (post-D1). Each Act carries a
	// monologue string + per-beat rune-offset slices. The fixture covers two
	// acts (act_hook + act_2) so HighLeverage detection can light up both
	// "first appearance" (entity_visible=true on the first beat) and
	// "act boundary" downstream classifications.
	raw := []byte(`{
  "narration": {
    "scp_id": "049",
    "title": "Scenario",
    "acts": [
      {
        "act_id": "act_hook",
        "monologue": "Hook scene narration text.",
        "mood": "tense",
        "key_points": [],
        "beats": [
          {
            "start_offset": 0, "end_offset": 26,
            "mood": "tense", "location": "Hall",
            "characters_present": ["연구원"], "entity_visible": true,
            "color_palette": "gray", "atmosphere": "tense",
            "fact_tags": []
          }
        ]
      },
      {
        "act_id": "act_2",
        "monologue": "Boundary scene text.",
        "mood": "steady",
        "key_points": [],
        "beats": [
          {
            "start_offset": 0, "end_offset": 20,
            "mood": "steady", "location": "Cell",
            "characters_present": ["연구원"], "entity_visible": false,
            "color_palette": "blue", "atmosphere": "cool",
            "fact_tags": []
          }
        ]
      }
    ],
    "metadata": {"language": "ko", "scene_count": 2},
    "source_version": "v1.2-roles"
  }
}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return path
}

func seedDecisionHistoryRow(
	t testing.TB,
	database *sql.DB,
	id int64,
	runID string,
	sceneID *string,
	decisionType string,
	note *string,
	contextSnapshot *string,
	supersededBy *int64,
	createdAt string,
) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO decisions (
			id, run_id, scene_id, decision_type, context_snapshot, note, superseded_by, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, runID, sceneID, decisionType, contextSnapshot, note, supersededBy, createdAt,
	); err != nil {
		t.Fatalf("seed decision history row: %v", err)
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

func TestSceneHandler_List_ReturnsRichPayloadAtNonReviewStage(t *testing.T) {
	// SCL-5: /scenes carries the same envelope as /review-items so the
	// read-only master/detail panes can render shots, critic_score, and
	// critic_breakdown at any stage with populated segments.
	h, database, outDir := newTestSceneHandler(t)
	runStore := db.NewRunStore(database)
	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`UPDATE runs SET stage = 'image', status = 'running' WHERE id = ?`, run.ID); err != nil {
		t.Fatalf("seed stage: %v", err)
	}
	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO segments (
			run_id, scene_index, narration, shot_count, shots, tts_path,
			critic_score, critic_sub, status, review_status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'completed', ?)`,
		run.ID,
		0,
		"이미지 단계 장면",
		1,
		`[{"image_path":"/images/scene-0.png","duration_s":4.0,"transition":"cut","visual_descriptor":"opening"}]`,
		"/audio/scene-0.wav",
		0.82,
		`{"hook_strength":0.91,"fact_accuracy":0.88,"emotional_variation":0.6,"immersion":0.45}`,
		string(domain.ReviewStatusWaitingForReview),
	); err != nil {
		t.Fatalf("seed segment: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID+"/scenes", nil)
	req.SetPathValue("id", run.ID)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	var env struct {
		Data *struct {
			Items []struct {
				SceneIndex      int      `json:"scene_index"`
				Narration       string   `json:"narration"`
				Shots           []any    `json:"shots"`
				TTSPath         *string  `json:"tts_path,omitempty"`
				CriticScore     *float64 `json:"critic_score,omitempty"`
				CriticBreakdown *struct {
					AggregateScore     *float64 `json:"aggregate_score,omitempty"`
					HookStrength       *float64 `json:"hook_strength,omitempty"`
					FactAccuracy       *float64 `json:"fact_accuracy,omitempty"`
					EmotionalVariation *float64 `json:"emotional_variation,omitempty"`
					Immersion          *float64 `json:"immersion,omitempty"`
				} `json:"critic_breakdown,omitempty"`
				ReviewStatus string `json:"review_status"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Data.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(env.Data.Items))
	}
	item := env.Data.Items[0]
	testutil.AssertEqual(t, item.SceneIndex, 0)
	testutil.AssertEqual(t, item.Narration, "이미지 단계 장면")
	if len(item.Shots) != 1 {
		t.Fatalf("want 1 shot, got %d", len(item.Shots))
	}
	expectedAudioURL := "/api/runs/" + run.ID + "/scenes/0/audio"
	if item.TTSPath == nil || *item.TTSPath != expectedAudioURL {
		t.Fatalf("tts_path mismatch: got %+v, want %s", item.TTSPath, expectedAudioURL)
	}
	if item.CriticScore == nil || *item.CriticScore != 82 {
		t.Fatalf("critic_score mismatch (want 82 after 0..1→0..100 normalization): %+v", item.CriticScore)
	}
	if item.CriticBreakdown == nil || item.CriticBreakdown.HookStrength == nil ||
		*item.CriticBreakdown.HookStrength != 91 {
		t.Fatalf("critic_breakdown.hook_strength mismatch (want 91 after normalization): %+v", item.CriticBreakdown)
	}
	testutil.AssertEqual(t, item.ReviewStatus, string(domain.ReviewStatusWaitingForReview))
}

func TestSceneHandler_List_AllowsAnyStageReturnsEmptyWithoutSegments(t *testing.T) {
	// SCL-5: List is unconditional once the run exists. A run that has not yet
	// produced segments returns 200 with an empty list so the master pane can
	// render its placeholder, instead of 409 which the UI would surface as an
	// error.
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

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	var env struct {
		Data *struct {
			Items []any `json:"items"`
			Total int   `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Data.Total, 0)
	testutil.AssertEqual(t, len(env.Data.Items), 0)
}

func TestSceneHandler_List_ReturnsNotFoundForUnknownRun(t *testing.T) {
	h, _, _ := newTestSceneHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/no-such-run/scenes", nil)
	req.SetPathValue("id", "no-such-run")
	rec := httptest.NewRecorder()
	h.List(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
}

func TestSceneHandler_ListReviewItems_ReturnsPayload(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	scenarioPath := writeScenarioFixture(t)
	runID := seedBatchReviewRun(t, database, outDir, scenarioPath)
	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO segments (
			run_id, scene_index, narration, shot_count, shots, clip_path,
			critic_score, critic_sub, status, review_status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'completed', ?)`,
		runID,
		0,
		"첫 번째 검토 장면",
		1,
		`[{"image_path":"/images/scene-0.png","duration_s":4.0,"transition":"cut","visual_descriptor":"opening"}]`,
		"/clips/scene-0.mp4",
		0.82,
		`{"hook_strength":0.91,"fact_accuracy":0.88,"emotional_variation":0.64,"immersion":0.45}`,
		"waiting_for_review",
	); err != nil {
		t.Fatalf("seed segment: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/review-items", nil)
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.ListReviewItems(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)

	var env struct {
		Version int `json:"version"`
		Data    *struct {
			Items []struct {
				SceneIndex         int    `json:"scene_index"`
				HighLeverage       bool   `json:"high_leverage"`
				HighLeverageReason string `json:"high_leverage_reason"`
				CriticBreakdown    *struct {
					HookStrength float64 `json:"hook_strength"`
				} `json:"critic_breakdown"`
			} `json:"items"`
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Version, 1)
	testutil.AssertEqual(t, env.Data.Total, 1)
	testutil.AssertEqual(t, env.Data.Items[0].SceneIndex, 0)
	testutil.AssertEqual(t, env.Data.Items[0].HighLeverage, true)
	testutil.AssertEqual(t, env.Data.Items[0].HighLeverageReason, "First appearance of SCP-049; Opening hook scene")
	if env.Data.Items[0].CriticBreakdown == nil {
		t.Fatalf("expected critic breakdown")
	}
	testutil.AssertEqual(t, int(env.Data.Items[0].CriticBreakdown.HookStrength), 91)
}

func TestSceneHandler_ListReviewItems_ReturnsConflictWhenRunNotAtBatchReview(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedScenarioReviewRun(t, database, outDir)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/review-items", nil)
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.ListReviewItems(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
}

func TestSceneHandler_ListDecisions_ReturnsEnvelopeAndCursor(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID1 := seedBatchReviewRun(t, database, outDir, writeScenarioFixture(t))
	runID2 := seedBatchReviewRun(t, database, outDir, writeScenarioFixture(t))
	if _, err := database.ExecContext(context.Background(), `UPDATE runs SET scp_id = '173' WHERE id = ?`, runID2); err != nil {
		t.Fatalf("update scp id: %v", err)
	}

	scene0 := "0"
	scene1 := "1"
	note := "manual override"
	snapshotReason := `{"reason":"restored from snapshot"}`
	reversalID := int64(3)
	seedDecisionHistoryRow(t, database, 1, runID1, &scene0, domain.DecisionTypeApprove, &note, nil, nil, "2026-04-18T10:00:00Z")
	seedDecisionHistoryRow(t, database, 3, runID2, nil, domain.DecisionTypeUndo, nil, nil, nil, "2026-04-18T10:02:00Z")
	seedDecisionHistoryRow(t, database, 2, runID2, &scene1, domain.DecisionTypeReject, nil, &snapshotReason, &reversalID, "2026-04-18T10:01:00Z")

	req := httptest.NewRequest(http.MethodGet, "/api/decisions?limit=2", nil)
	rec := httptest.NewRecorder()
	h.ListDecisions(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)

	var env struct {
		Version int `json:"version"`
		Data    *struct {
			Items []struct {
				ID                 int64   `json:"id"`
				SCPID              string  `json:"scp_id"`
				DecisionType       string  `json:"decision_type"`
				ReasonFromSnapshot *string `json:"reason_from_snapshot"`
				SupersededBy       *int64  `json:"superseded_by"`
			} `json:"items"`
			NextCursor *struct {
				BeforeCreatedAt string `json:"before_created_at"`
				BeforeID        int64  `json:"before_id"`
			} `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Version, 1)
	testutil.AssertEqual(t, len(env.Data.Items), 2)
	testutil.AssertEqual(t, env.Data.Items[0].ID, int64(3))
	testutil.AssertEqual(t, env.Data.Items[1].SCPID, "173")
	if env.Data.Items[1].ReasonFromSnapshot == nil {
		t.Fatalf("expected snapshot reason")
	}
	testutil.AssertEqual(t, *env.Data.Items[1].ReasonFromSnapshot, "restored from snapshot")
	if env.Data.Items[1].SupersededBy == nil || *env.Data.Items[1].SupersededBy != reversalID {
		t.Fatalf("expected superseded link, got %+v", env.Data.Items[1])
	}
	if env.Data.NextCursor == nil {
		t.Fatalf("expected next cursor")
	}
	testutil.AssertEqual(t, env.Data.NextCursor.BeforeID, int64(2))
}

func TestSceneHandler_ListDecisions_RejectsInvalidDecisionType(t *testing.T) {
	h, _, _ := newTestSceneHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/decisions?decision_type=nope", nil)
	rec := httptest.NewRecorder()
	h.ListDecisions(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
}

func TestSceneHandler_ListDecisions_RejectsMalformedBeforeCreatedAt(t *testing.T) {
	h, _, _ := newTestSceneHandler(t)

	for _, badVal := range []string{"notadate", "2026-04-19", "zzz", "0"} {
		req := httptest.NewRequest(http.MethodGet, "/api/decisions?before_created_at="+badVal+"&before_id=1", nil)
		rec := httptest.NewRecorder()
		h.ListDecisions(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("before_created_at=%q: expected 400, got %d", badVal, rec.Code)
		}
	}

	// Valid RFC3339 cursor should be accepted (even with no matching rows).
	req := httptest.NewRequest(http.MethodGet, "/api/decisions?before_created_at=2026-01-01T00:00:00Z&before_id=1", nil)
	rec := httptest.NewRecorder()
	h.ListDecisions(rec, req)
	testutil.AssertEqual(t, rec.Code, http.StatusOK)
}

func TestSceneHandler_ListDecisions_PaginationQueryParamsRoundTrip(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedBatchReviewRun(t, database, outDir, writeScenarioFixture(t))

	scene0 := "0"
	scene1 := "1"
	scene2 := "2"
	seedDecisionHistoryRow(t, database, 1, runID, &scene0, domain.DecisionTypeApprove, nil, nil, nil, "2026-04-18T10:00:00Z")
	seedDecisionHistoryRow(t, database, 2, runID, &scene1, domain.DecisionTypeReject, nil, nil, nil, "2026-04-18T10:01:00Z")
	seedDecisionHistoryRow(t, database, 3, runID, &scene2, domain.DecisionTypeReject, nil, nil, nil, "2026-04-18T10:02:00Z")

	firstReq := httptest.NewRequest(http.MethodGet, "/api/decisions?decision_type=reject&limit=1", nil)
	firstRec := httptest.NewRecorder()
	h.ListDecisions(firstRec, firstReq)
	testutil.AssertEqual(t, firstRec.Code, http.StatusOK)

	var firstEnv struct {
		Data struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
			NextCursor *struct {
				BeforeCreatedAt string `json:"before_created_at"`
				BeforeID        int64  `json:"before_id"`
			} `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(firstRec.Body.Bytes(), &firstEnv); err != nil {
		t.Fatalf("unmarshal first page: %v", err)
	}
	if firstEnv.Data.NextCursor == nil {
		t.Fatalf("expected next cursor on first page")
	}
	testutil.AssertEqual(t, firstEnv.Data.Items[0].ID, int64(3))

	secondReq := httptest.NewRequest(
		http.MethodGet,
		"/api/decisions?decision_type=reject&limit=1&before_created_at="+firstEnv.Data.NextCursor.BeforeCreatedAt+"&before_id="+fmt.Sprint(firstEnv.Data.NextCursor.BeforeID),
		nil,
	)
	secondRec := httptest.NewRecorder()
	h.ListDecisions(secondRec, secondReq)
	testutil.AssertEqual(t, secondRec.Code, http.StatusOK)

	var secondEnv struct {
		Data struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(secondRec.Body.Bytes(), &secondEnv); err != nil {
		t.Fatalf("unmarshal second page: %v", err)
	}
	testutil.AssertEqual(t, len(secondEnv.Data.Items), 1)
	testutil.AssertEqual(t, secondEnv.Data.Items[0].ID, int64(2))
}

func TestSceneHandler_RecordDecision_WritesApproveAndReturnsNextScene(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedBatchReviewRun(t, database, outDir, writeScenarioFixture(t))
	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO segments (run_id, scene_index, narration, status, review_status)
		VALUES
		  (?, 0, 'scene zero', 'completed', 'waiting_for_review'),
		  (?, 1, 'scene one', 'completed', 'waiting_for_review')`,
		runID, runID,
	); err != nil {
		t.Fatalf("seed segments: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"scene_index":   0,
		"decision_type": "approve",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/decisions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.RecordDecision(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)

	var env struct {
		Version int `json:"version"`
		Data    *struct {
			SceneIndex     int    `json:"scene_index"`
			DecisionType   string `json:"decision_type"`
			NextSceneIndex int    `json:"next_scene_index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Data.SceneIndex, 0)
	testutil.AssertEqual(t, env.Data.DecisionType, "approve")
	testutil.AssertEqual(t, env.Data.NextSceneIndex, 1)
}

func TestSceneHandler_ApproveAllRemaining_ApprovesWaitingScenes(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedBatchReviewRun(t, database, outDir, writeScenarioFixture(t))
	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO segments (run_id, scene_index, narration, status, review_status)
		VALUES
		  (?, 0, 'scene zero', 'completed', 'waiting_for_review'),
		  (?, 1, 'scene one', 'completed', 'pending'),
		  (?, 2, 'scene two', 'completed', 'approved')`,
		runID, runID, runID,
	); err != nil {
		t.Fatalf("seed segments: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"focus_scene_index": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/approve-all-remaining", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.ApproveAllRemaining(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)

	var env struct {
		Version int `json:"version"`
		Data    *struct {
			AggregateCommandID string `json:"aggregate_command_id"`
			ApprovedCount      int    `json:"approved_count"`
			ApprovedSceneIDs   []int  `json:"approved_scene_indices"`
			FocusSceneIndex    int    `json:"focus_scene_index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Version, 1)
	testutil.AssertEqual(t, env.Data.ApprovedCount, 2)
	testutil.AssertEqual(t, env.Data.FocusSceneIndex, 1)
	if env.Data.AggregateCommandID == "" {
		t.Fatal("expected aggregate command id")
	}
	testutil.AssertEqual(t, len(env.Data.ApprovedSceneIDs), 2)
}

func TestSceneHandler_ApproveAllRemaining_ReturnsConflictOutsideBatchReview(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedScenarioReviewRun(t, database, outDir)

	body, _ := json.Marshal(map[string]any{
		"focus_scene_index": 0,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/approve-all-remaining", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.ApproveAllRemaining(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
}

func TestSceneHandler_RecordDecision_RequiresSnapshotForSkip(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedBatchReviewRun(t, database, outDir, writeScenarioFixture(t))
	seedSegment(t, database, runID, 0, "scene zero")
	if _, err := database.ExecContext(context.Background(),
		`UPDATE segments SET review_status = 'waiting_for_review', status = 'completed' WHERE run_id = ? AND scene_index = 0`,
		runID,
	); err != nil {
		t.Fatalf("seed review status: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"scene_index":   0,
		"decision_type": "skip_and_remember",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/decisions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.RecordDecision(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
}

func TestSceneHandler_RecordDecision_ReturnsConflictWhenRunNotAtBatchReview(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedScenarioReviewRun(t, database, outDir)
	seedSegment(t, database, runID, 0, "scene zero")

	body, _ := json.Marshal(map[string]any{
		"scene_index":   0,
		"decision_type": "reject",
		"note":          "out-of-stage guard check",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/decisions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.RecordDecision(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
}

func TestSceneHandler_RecordDecision_RejectRequiresNote(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedBatchReviewRun(t, database, outDir, writeScenarioFixture(t))
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO segments (run_id, scene_index, narration, status, review_status)
		 VALUES (?, 0, 'scene zero', 'completed', 'waiting_for_review')`,
		runID,
	); err != nil {
		t.Fatalf("seed segment: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"scene_index":   0,
		"decision_type": "reject",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/decisions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.RecordDecision(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
}

func TestSceneHandler_RecordDecision_RejectRecordsNoteAndReturnsWarning(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	ctx := context.Background()
	runID := seedBatchReviewRun(t, database, outDir, writeScenarioFixture(t))
	if _, err := database.ExecContext(ctx,
		`INSERT INTO segments (run_id, scene_index, narration, status, review_status)
		 VALUES (?, 0, 'scene zero', 'completed', 'waiting_for_review')`,
		runID,
	); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	// Seed a prior cross-run reject on scp 049, scene index 0 so FR53 fires.
	if _, err := database.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id) VALUES ('prior-run', '049')`); err != nil {
		t.Fatalf("seed prior run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO decisions (run_id, scene_id, decision_type, note, created_at)
		 VALUES ('prior-run', '0', 'reject', 'tone off', '2026-03-15T11:00:00Z')`,
	); err != nil {
		t.Fatalf("seed prior decision: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"scene_index":   0,
		"decision_type": "reject",
		"note":          "pacing still off",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/decisions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.RecordDecision(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)

	var env struct {
		Data struct {
			SceneIndex     int  `json:"scene_index"`
			RegenAttempts  int  `json:"regen_attempts"`
			RetryExhausted bool `json:"retry_exhausted"`
			PriorRejection *struct {
				RunID  string `json:"run_id"`
				Reason string `json:"reason"`
			} `json:"prior_rejection"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Data.SceneIndex, 0)
	testutil.AssertEqual(t, env.Data.RegenAttempts, 1)
	testutil.AssertEqual(t, env.Data.RetryExhausted, false)
	if env.Data.PriorRejection == nil {
		t.Fatal("expected prior_rejection warning in response")
	}
	testutil.AssertEqual(t, env.Data.PriorRejection.RunID, "prior-run")
	testutil.AssertEqual(t, env.Data.PriorRejection.Reason, "tone off")

	// Verify the reject note landed on the decision row.
	var note string
	if err := database.QueryRow(
		`SELECT note FROM decisions WHERE run_id = ? AND scene_id = '0' AND decision_type = 'reject'`,
		runID,
	).Scan(&note); err != nil {
		t.Fatalf("query decision note: %v", err)
	}
	testutil.AssertEqual(t, note, "pacing still off")
}

// ── POST /api/runs/{id}/scenes/{idx}/regen ───────────────────────────────────

func TestSceneHandler_Regenerate_DispatchesAndRestoresReviewGate(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	ctx := context.Background()
	runID := seedBatchReviewRun(t, database, outDir, writeScenarioFixture(t))
	if _, err := database.ExecContext(ctx,
		`INSERT INTO segments (run_id, scene_index, narration, status, review_status)
		 VALUES (?, 0, 'scene zero', 'completed', 'rejected')`,
		runID,
	); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO decisions (run_id, scene_id, decision_type, note) VALUES (?, '0', 'reject', 'reason')`,
		runID,
	); err != nil {
		t.Fatalf("seed reject decision: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/scenes/0/regen", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	req.SetPathValue("idx", "0")
	rec := httptest.NewRecorder()
	h.Regenerate(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)

	var env struct {
		Data struct {
			SceneIndex     int  `json:"scene_index"`
			RegenAttempts  int  `json:"regen_attempts"`
			RetryExhausted bool `json:"retry_exhausted"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Data.SceneIndex, 0)
	testutil.AssertEqual(t, env.Data.RegenAttempts, 1)
	testutil.AssertEqual(t, env.Data.RetryExhausted, false)

	var status string
	if err := database.QueryRow(
		`SELECT review_status FROM segments WHERE run_id = ? AND scene_index = 0`, runID,
	).Scan(&status); err != nil {
		t.Fatalf("query review_status: %v", err)
	}
	testutil.AssertEqual(t, status, string(domain.ReviewStatusWaitingForReview))
}

func TestSceneHandler_Regenerate_Returns409AfterRetryCap(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	ctx := context.Background()
	runID := seedBatchReviewRun(t, database, outDir, writeScenarioFixture(t))
	if _, err := database.ExecContext(ctx,
		`INSERT INTO segments (run_id, scene_index, narration, status, review_status)
		 VALUES (?, 0, 'scene zero', 'completed', 'rejected')`,
		runID,
	); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	// Seed 3 non-superseded reject decisions so attempts count > MaxSceneRegenAttempts.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO decisions (run_id, scene_id, decision_type, note, created_at) VALUES
		  (?, '0', 'reject', 'r1', '2026-04-01T10:00:00Z'),
		  (?, '0', 'reject', 'r2', '2026-04-01T10:05:00Z'),
		  (?, '0', 'reject', 'r3', '2026-04-01T10:10:00Z')`,
		runID, runID, runID,
	); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/scenes/0/regen", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	req.SetPathValue("idx", "0")
	rec := httptest.NewRecorder()
	h.Regenerate(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
}

func TestSceneHandler_Regenerate_Returns400ForInvalidIndex(t *testing.T) {
	h, database, outDir := newTestSceneHandler(t)
	runID := seedBatchReviewRun(t, database, outDir, writeScenarioFixture(t))
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/scenes/abc/regen", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	req.SetPathValue("idx", "abc")
	rec := httptest.NewRecorder()
	h.Regenerate(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
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

// ── POST /api/runs/{id}/undo ──────────────────────────────────────────────────

func TestSceneHandler_Undo_ReturnsOKOnSuccess(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, database, outDir := newTestSceneHandler(t)
	runID := seedBatchReviewRun(t, database, outDir, "")

	// Seed a waiting_for_review segment and an approve decision.
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO segments (run_id, scene_index, review_status) VALUES (?, 0, 'approved')`,
		runID,
	); err != nil {
		t.Fatalf("seed segment: %v", err)
	}
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO decisions (run_id, scene_id, decision_type) VALUES (?, '0', 'approve')`,
		runID,
	); err != nil {
		t.Fatalf("seed decision: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/undo", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Undo(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	var env struct {
		Version int `json:"version"`
		Data    struct {
			UndoneSceneIndex int    `json:"undone_scene_index"`
			UndoneKind       string `json:"undone_kind"`
			FocusTarget      string `json:"focus_target"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	testutil.AssertEqual(t, env.Data.UndoneSceneIndex, 0)
	testutil.AssertEqual(t, env.Data.UndoneKind, "approve")
	testutil.AssertEqual(t, env.Data.FocusTarget, "scene-card")
}

func TestSceneHandler_Undo_Returns409WhenPhaseC(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, database, outDir := newTestSceneHandler(t)

	// Create a run in assemble stage (Phase C).
	runStore := db.NewRunStore(database)
	ctx := context.Background()
	run, err := runStore.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET stage = 'assemble', status = 'running' WHERE id = ?`, run.ID); err != nil {
		t.Fatalf("seed stage: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/undo", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", run.ID)
	rec := httptest.NewRecorder()
	h.Undo(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
}

func TestSceneHandler_Undo_Returns409WhenNoUndoableCommand(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, database, outDir := newTestSceneHandler(t)
	runID := seedBatchReviewRun(t, database, outDir, "")

	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/undo", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Undo(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
}
