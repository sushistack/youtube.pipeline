package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func newTestRunHandler(t testing.TB) (*api.RunHandler, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	logger, _ := testutil.CaptureLog(t)
	outDir := t.TempDir()
	decisionStore := db.NewDecisionStore(database)
	hitl := service.NewHITLService(store, decisionStore, logger)
	return api.NewRunHandler(svc, hitl, outDir, logger), outDir
}

// newTestRunHandlerWithEngine wires a handler + resumer engine + a seeded
// run in the given (stage, status) for Resume-path coverage. The run ID is
// scp-{scpID}-run-1 (deterministic).
func newTestRunHandlerWithEngine(t testing.TB, scpID, stage, status string) (*api.RunHandler, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)
	logger, _ := testutil.CaptureLog(t)
	outDir := t.TempDir()

	engine := pipeline.NewEngine(store, segStore, nil, clock.RealClock{}, outDir, logger)
	svc := service.NewRunService(store, engine)
	decisionStore := db.NewDecisionStore(database)
	hitl := service.NewHITLService(store, decisionStore, logger)

	// Seed run and advance its state machine column directly.
	ctx := context.Background()
	if _, err := svc.Create(ctx, scpID, outDir); err != nil {
		t.Fatalf("seed create run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET stage = ?, status = ? WHERE scp_id = ?`,
		stage, status, scpID); err != nil {
		t.Fatalf("seed stage/status: %v", err)
	}
	return api.NewRunHandler(svc, hitl, outDir, logger), outDir
}

type runEnvelope struct {
	Version int `json:"version"`
	Data    *struct {
		ID        string `json:"id"`
		SCPID     string `json:"scp_id"`
		Stage     string `json:"stage"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
	} `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

type listEnvelope struct {
	Version int `json:"version"`
	Data    *struct {
		Items []struct{ ID string `json:"id"` } `json:"items"`
		Total int                               `json:"total"`
	} `json:"data"`
}

func TestRunHandler_Create_Success(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, outDir := newTestRunHandler(t)

	body, _ := json.Marshal(map[string]string{"scp_id": "049"})
	req := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	_ = outDir

	testutil.AssertEqual(t, rec.Code, http.StatusCreated)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Version, 1)
	if env.Data == nil {
		t.Fatal("data is nil")
	}
	testutil.AssertEqual(t, env.Data.ID, "scp-049-run-1")
	testutil.AssertEqual(t, env.Data.SCPID, "049")
	testutil.AssertEqual(t, env.Data.Stage, "pending")
	testutil.AssertEqual(t, env.Data.Status, "pending")
}

func TestRunHandler_Create_MissingSCPID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandler(t)

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
}

func TestRunHandler_List_Empty(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	env := testutil.ReadJSON[listEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Data.Total, 0)
}

func TestRunHandler_Get_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/scp-999-run-1", nil)
	req.SetPathValue("id", "scp-999-run-1")
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "NOT_FOUND")
}

func TestRunHandler_Cancel_Conflict(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandler(t)

	// Create a run (status=pending, not cancellable).
	createBody, _ := json.Marshal(map[string]string{"scp_id": "049"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	h.Create(httptest.NewRecorder(), createReq)

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/cancel", nil)
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.Cancel(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
}

func TestRunHandler_Resume_UnknownField_Rejected(t *testing.T) {
	// Typo'd field `force` (should be `confirm_inconsistent`) must produce
	// 400 so the client sees their flag was not honored. Silent default
	// would lead to confusing "validation failed" responses.
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandlerWithEngine(t, "049", "tts", "failed")

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/resume",
		bytes.NewReader([]byte(`{"force":true}`)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "VALIDATION_ERROR")
}

func TestRunHandler_Resume_EmptyBody_TreatedAsDefault(t *testing.T) {
	// Empty body is valid — defaults to confirm_inconsistent=false.
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandlerWithEngine(t, "049", "tts", "failed")

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/resume", nil)
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
}

func TestRunHandler_Resume_NoEngine_Validation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// newTestRunHandler wires a service WITHOUT a resumer; Resume should
	// classify as ErrValidation (400) rather than panicking.
	h, _ := newTestRunHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/resume", nil)
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "VALIDATION_ERROR")
}

func TestRunHandler_Resume_Success(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandlerWithEngine(t, "049", "tts", "failed")

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/resume",
		bytes.NewReader([]byte(`{"confirm_inconsistent": false}`)))
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	if env.Data == nil {
		t.Fatal("data is nil")
	}
	testutil.AssertEqual(t, env.Data.ID, "scp-049-run-1")
	// Phase B resume → status=running after reset.
	testutil.AssertEqual(t, env.Data.Status, "running")
}

func TestRunHandler_Resume_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandlerWithEngine(t, "049", "tts", "failed")

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-999-run-1/resume", nil)
	req.SetPathValue("id", "scp-999-run-1")
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
}

func TestRunHandler_Resume_Conflict_OnCompletedRun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandlerWithEngine(t, "049", "complete", "completed")

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/resume", nil)
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
}

// --- Contract validation tests ---

func TestContract_RunDetailResponse(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	data := testutil.LoadFixture(t, "contracts/run.detail.response.json")

	type contractEnvelope struct {
		Version int `json:"version"`
		Data    struct {
			ID        string  `json:"id"`
			SCPID     string  `json:"scp_id"`
			Stage     string  `json:"stage"`
			Status    string  `json:"status"`
			CostUSD   float64 `json:"cost_usd"`
			CreatedAt string  `json:"created_at"`
			UpdatedAt string  `json:"updated_at"`
		} `json:"data"`
	}

	var env contractEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal run.detail.response.json: %v", err)
	}
	if env.Version != 1 {
		t.Errorf("version = %d, want 1", env.Version)
	}
	if env.Data.ID != "scp-049-run-1" {
		t.Errorf("id = %q, want scp-049-run-1", env.Data.ID)
	}
	if env.Data.Stage != "pending" {
		t.Errorf("stage = %q, want pending", env.Data.Stage)
	}
	if env.Data.Status != "pending" {
		t.Errorf("status = %q, want pending", env.Data.Status)
	}
}

func TestContract_RunResumeResponse(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	data := testutil.LoadFixture(t, "contracts/run.resume.response.json")

	type contractEnvelope struct {
		Version int `json:"version"`
		Data    struct {
			ID         string  `json:"id"`
			SCPID      string  `json:"scp_id"`
			Stage      string  `json:"stage"`
			Status     string  `json:"status"`
			RetryCount int     `json:"retry_count"`
			CostUSD    float64 `json:"cost_usd"`
			CreatedAt  string  `json:"created_at"`
			UpdatedAt  string  `json:"updated_at"`
		} `json:"data"`
		Warnings []string `json:"warnings"`
	}
	var env contractEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal run.resume.response.json: %v", err)
	}
	if env.Version != 1 {
		t.Errorf("version = %d, want 1", env.Version)
	}
	if env.Data.ID != "scp-049-run-1" {
		t.Errorf("id = %q, want scp-049-run-1", env.Data.ID)
	}
	if env.Data.Status != "running" {
		t.Errorf("status = %q, want running (post-resume reset)", env.Data.Status)
	}
	if env.Warnings == nil {
		t.Errorf("warnings field should be present (empty array ok)")
	}
}

func TestContract_RunListResponse(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	data := testutil.LoadFixture(t, "contracts/run.list.response.json")

	type contractEnvelope struct {
		Version int `json:"version"`
		Data    struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			Total int `json:"total"`
		} `json:"data"`
	}

	var env contractEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal run.list.response.json: %v", err)
	}
	if env.Version != 1 {
		t.Errorf("version = %d, want 1", env.Version)
	}
	if env.Data.Total != 1 {
		t.Errorf("total = %d, want 1", env.Data.Total)
	}
	if len(env.Data.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(env.Data.Items))
	}
	if env.Data.Items[0].ID != "scp-049-run-1" {
		t.Errorf("items[0].id = %q, want scp-049-run-1", env.Data.Items[0].ID)
	}
}

func TestRunHandler_AcknowledgeMetadata_Success(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	logger, _ := testutil.CaptureLog(t)
	outDir := t.TempDir()
	decisionStore := db.NewDecisionStore(database)
	hitl := service.NewHITLService(store, decisionStore, logger)
	h := api.NewRunHandler(svc, hitl, outDir, logger)

	// Create run and advance to metadata_ack/waiting.
	ctx := context.Background()
	if _, err := svc.Create(ctx, "049", outDir); err != nil {
		t.Fatalf("seed create run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET stage = 'metadata_ack', status = 'waiting' WHERE scp_id = '049'`,
	); err != nil {
		t.Fatalf("seed stage/status: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/metadata/ack", nil)
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.AcknowledgeMetadata(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	if env.Data == nil {
		t.Fatal("data is nil")
	}
	testutil.AssertEqual(t, env.Data.Stage, "complete")
	testutil.AssertEqual(t, env.Data.Status, "completed")
}

func TestRunHandler_AcknowledgeMetadata_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	logger, _ := testutil.CaptureLog(t)
	outDir := t.TempDir()
	decisionStore := db.NewDecisionStore(database)
	hitl := service.NewHITLService(store, decisionStore, logger)
	h := api.NewRunHandler(svc, hitl, outDir, logger)

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-999-run-1/metadata/ack", nil)
	req.SetPathValue("id", "scp-999-run-1")
	rec := httptest.NewRecorder()
	h.AcknowledgeMetadata(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "NOT_FOUND")
}

func TestRunHandler_AcknowledgeMetadata_WrongStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	logger, _ := testutil.CaptureLog(t)
	outDir := t.TempDir()
	decisionStore := db.NewDecisionStore(database)
	hitl := service.NewHITLService(store, decisionStore, logger)
	h := api.NewRunHandler(svc, hitl, outDir, logger)

	// Create a run at pending/pending (default).
	ctx := context.Background()
	if _, err := svc.Create(ctx, "049", outDir); err != nil {
		t.Fatalf("seed create run: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/metadata/ack", nil)
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.AcknowledgeMetadata(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "CONFLICT")
}

// TestRunHandler_SMOKE_07_ComplianceGate exercises the FR23 / NFR-L1
// compliance gate end-to-end at the handler boundary as three coupled
// sub-tests sharing one DB+handler instance: gate-closed → 409,
// gate-open → 200 + complete/completed, and replay-on-completed → 409
// (the gate is one-shot).
//
// Step 3 §4 SMOKE-07's literal `POST /api/runs/{id}/upload → 409` step
// is mapped onto the actual route surface: no `/upload` endpoint exists
// in routes.go, and the compliance gate is enforced at
// `metadata/ack` itself (RunStore.MarkComplete is the atomic check).
// Calling ack at the wrong stage returns the same 409 the upload
// endpoint would have, so the regression guard is preserved.
//
// Runtime budget: ≤ 3 s.
func TestRunHandler_SMOKE_07_ComplianceGate(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	logger, _ := testutil.CaptureLog(t)
	outDir := t.TempDir()
	decisionStore := db.NewDecisionStore(database)
	hitl := service.NewHITLService(store, decisionStore, logger)
	h := api.NewRunHandler(svc, hitl, outDir, logger)

	ctx := context.Background()
	created, err := svc.Create(ctx, "049", outDir)
	if err != nil {
		t.Fatalf("seed create run: %v", err)
	}
	runID := created.ID

	// requireAffectedOne fails the test if the given UPDATE did not affect
	// exactly one row, defending against silent-no-op regressions if
	// run-id derivation drifts from the hardcoded fixture id.
	requireAffectedOne := func(t *testing.T, query string, args ...any) {
		t.Helper()
		res, err := database.ExecContext(ctx, query, args...)
		if err != nil {
			t.Fatalf("UPDATE %q: %v", query, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			t.Fatalf("RowsAffected: %v", err)
		}
		if n != 1 {
			t.Fatalf("UPDATE %q affected %d rows, want 1", query, n)
		}
	}

	postAck := func(t *testing.T, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		var req *http.Request
		if body == nil {
			req = httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/metadata/ack", nil)
		} else {
			req = httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/metadata/ack", bytes.NewBuffer(body))
		}
		req.SetPathValue("id", runID)
		rec := httptest.NewRecorder()
		h.AcknowledgeMetadata(rec, req)
		return rec
	}

	t.Run("gate_closed_returns_409", func(t *testing.T) {
		// Park the run at a Phase B sub-stage so the gate is closed.
		// Step 3 §4 wording said "phase_b"; the closest real stage is
		// `image`, a Phase B sub-stage per domain/types.go.
		requireAffectedOne(t,
			`UPDATE runs SET stage = 'image', status = 'running' WHERE id = ?`, runID)

		rec := postAck(t, nil)
		testutil.AssertEqual(t, rec.Code, http.StatusConflict)
		env := testutil.ReadJSON[runEnvelope](t, rec.Body)
		testutil.AssertEqual(t, env.Error.Code, "CONFLICT")

		// Failed gate must NOT have advanced the row.
		var stage, status string
		if err := database.QueryRowContext(ctx,
			`SELECT stage, status FROM runs WHERE id = ?`, runID).
			Scan(&stage, &status); err != nil {
			t.Fatalf("read state: %v", err)
		}
		if stage != "image" || status != "running" {
			t.Errorf("DB state mutated by failed gate: stage=%q status=%q, want image/running",
				stage, status)
		}
	})

	t.Run("gate_open_succeeds_and_ignores_body", func(t *testing.T) {
		requireAffectedOne(t,
			`UPDATE runs SET stage = 'metadata_ack', status = 'waiting' WHERE id = ?`, runID)

		// Send a deliberately malformed JSON body. AcknowledgeMetadata's
		// contract is that it consumes no request body. If the handler
		// ever started parsing the body, json.Unmarshal would reject this
		// payload with 400 — a 200 here positively pins the body-ignored
		// invariant as the strongest regression guard available given
		// this endpoint has no MaxBytesReader (see Design Notes table).
		rec := postAck(t, []byte(`{not-json{`))
		testutil.AssertEqual(t, rec.Code, http.StatusOK)
		env := testutil.ReadJSON[runEnvelope](t, rec.Body)
		if env.Data == nil {
			t.Fatal("ack response data is nil")
		}
		testutil.AssertEqual(t, env.Data.Stage, "complete")
		testutil.AssertEqual(t, env.Data.Status, "completed")

		// Atomic transition is visible immediately.
		var stage, status string
		if err := database.QueryRowContext(ctx,
			`SELECT stage, status FROM runs WHERE id = ?`, runID).
			Scan(&stage, &status); err != nil {
			t.Fatalf("read state: %v", err)
		}
		if stage != "complete" || status != "completed" {
			t.Errorf("post-ack DB state: stage=%q status=%q, want complete/completed",
				stage, status)
		}
	})

	t.Run("replay_on_completed_run_returns_409", func(t *testing.T) {
		rec := postAck(t, nil)
		testutil.AssertEqual(t, rec.Code, http.StatusConflict)
	})
}

func TestRunHandler_ApproveScenarioReview_Success(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	logger, _ := testutil.CaptureLog(t)
	outDir := t.TempDir()
	decisionStore := db.NewDecisionStore(database)
	hitl := service.NewHITLService(store, decisionStore, logger)
	h := api.NewRunHandler(svc, hitl, outDir, logger)

	ctx := context.Background()
	if _, err := svc.Create(ctx, "049", outDir); err != nil {
		t.Fatalf("seed create run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE runs SET stage = 'scenario_review', status = 'waiting', scenario_path = 'scenario.json' WHERE scp_id = '049'`,
	); err != nil {
		t.Fatalf("seed stage/status: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/scenario/approve", nil)
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.ApproveScenarioReview(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	if env.Data == nil {
		t.Fatal("data is nil")
	}
	testutil.AssertEqual(t, env.Data.Stage, "character_pick")
	testutil.AssertEqual(t, env.Data.Status, "waiting")
}

func TestRunHandler_ApproveScenarioReview_WrongStage(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	logger, _ := testutil.CaptureLog(t)
	outDir := t.TempDir()
	decisionStore := db.NewDecisionStore(database)
	hitl := service.NewHITLService(store, decisionStore, logger)
	h := api.NewRunHandler(svc, hitl, outDir, logger)

	ctx := context.Background()
	if _, err := svc.Create(ctx, "049", outDir); err != nil {
		t.Fatalf("seed create run: %v", err)
	}
	// Run stays at pending/pending — wrong stage for scenario approve.

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/scenario/approve", nil)
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.ApproveScenarioReview(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusConflict)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "CONFLICT")
}

func TestRunHandler_ApproveScenarioReview_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	logger, _ := testutil.CaptureLog(t)
	outDir := t.TempDir()
	decisionStore := db.NewDecisionStore(database)
	hitl := service.NewHITLService(store, decisionStore, logger)
	h := api.NewRunHandler(svc, hitl, outDir, logger)

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-999-run-1/scenario/approve", nil)
	req.SetPathValue("id", "scp-999-run-1")
	rec := httptest.NewRecorder()
	h.ApproveScenarioReview(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "NOT_FOUND")
}
