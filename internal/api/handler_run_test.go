package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

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
	svc.SetAdvancer(engine)
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
	// Resume returns 202: PrepareResume runs synchronously, ExecuteResume is
	// dispatched on a goroutine to escape the 30s WriteTimeout for Phase B.
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandlerWithEngine(t, "049", "tts", "failed")

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/resume", nil)
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusAccepted)
}

func TestRunHandler_Advance_NoEngine_Validation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// newTestRunHandler wires a service WITHOUT an advancer; Advance should
	// classify as ErrValidation (400). The pending Start-run UI relies on
	// this path so users get a typed error instead of a 500/panic.
	h, _ := newTestRunHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/advance", nil)
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.Advance(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "VALIDATION_ERROR")
}

func TestRunHandler_Advance_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandlerWithEngine(t, "049", "pending", "pending")

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-999-run-1/advance", nil)
	req.SetPathValue("id", "scp-999-run-1")
	rec := httptest.NewRecorder()
	h.Advance(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusNotFound)
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

	testutil.AssertEqual(t, rec.Code, http.StatusAccepted)
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

// --- Cache panel + drop_caches advance tests (spec: cache-panel-pending) ---

// cacheEnvelope mirrors the GET /api/runs/{id}/cache wire shape. We use a
// dedicated struct (rather than runEnvelope) because the data shape is
// completely different — caches array vs. run fields.
type cacheEnvelope struct {
	Version int `json:"version"`
	Data    *struct {
		Caches []struct {
			Stage           string `json:"stage"`
			Filename        string `json:"filename"`
			SizeBytes       int64  `json:"size_bytes"`
			ModifiedAt      string `json:"modified_at"`
			SourceVersion   string `json:"source_version"`
			Fingerprint     string `json:"fingerprint"`
			StalenessReason string `json:"staleness_reason"`
		} `json:"caches"`
	} `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

// writeCacheFile drops a JSON file into {outDir}/{runID}/{filename}. filename
// may include a subdirectory component (e.g. "_cache/research_cache.json");
// parent directories are created as needed.
func writeCacheFile(t *testing.T, outDir, runID, filename, contents string) {
	t.Helper()
	fullPath := filepath.Join(outDir, runID, filename)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
}

// writeCacheEnvelope writes a proper CacheEnvelope for the given envelope stage
// ("research" or "structure") under {outDir}/{runID}/_cache/. The envelope's
// fingerprint matches inputs so the handler reports it as a valid (non-stale)
// entry. Returns the full path for stat assertions.
func writeCacheEnvelope(t *testing.T, outDir, runID, stage, sourceVersion string) string {
	t.Helper()
	relPaths := map[string]string{
		"research":  "_cache/research_cache.json",
		"structure": "_cache/structure_cache.json",
	}
	rel, ok := relPaths[stage]
	if !ok {
		t.Fatalf("writeCacheEnvelope: unknown stage %q", stage)
	}
	fullPath := filepath.Join(outDir, runID, rel)
	inputs := pipeline.FingerprintInputs{
		SourceVersion: sourceVersion,
		SchemaVersion: "v1",
	}
	payload := map[string]string{"source_version": sourceVersion}
	if err := pipeline.WriteEnvelope(fullPath, inputs, payload, time.Now()); err != nil {
		t.Fatalf("writeCacheEnvelope %s: %v", stage, err)
	}
	return fullPath
}

func TestRunHandler_Cache_TableDriven(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	type cacheCase struct {
		name             string
		seed             func(t *testing.T, outDir, runID string)
		runID            string
		expectedStatus   int
		expectedErrCode  string
		expectedStages   []string // sorted
		expectedSourceVs map[string]string
	}

	cases := []cacheCase{
		{
			name: "both_caches_present",
			seed: func(t *testing.T, outDir, runID string) {
				writeCacheEnvelope(t, outDir, runID, "research", "v1-deterministic")
				writeCacheEnvelope(t, outDir, runID, "structure", "v1-deterministic")
			},
			runID:            "scp-049-run-1",
			expectedStatus:   http.StatusOK,
			expectedStages:   []string{"research", "structure"},
			expectedSourceVs: map[string]string{"research": "v1-deterministic", "structure": "v1-deterministic"},
		},
		{
			name: "one_cache_present",
			seed: func(t *testing.T, outDir, runID string) {
				writeCacheEnvelope(t, outDir, runID, "research", "v1-deterministic")
			},
			runID:            "scp-049-run-1",
			expectedStatus:   http.StatusOK,
			expectedStages:   []string{"research"},
			expectedSourceVs: map[string]string{"research": "v1-deterministic"},
		},
		{
			name: "scenario_cache_present",
			seed: func(t *testing.T, outDir, runID string) {
				writeCacheFile(t, outDir, runID, "scenario.json",
					`{"scp_id":"049","source_version":"v1-deterministic"}`)
			},
			runID:            "scp-049-run-1",
			expectedStatus:   http.StatusOK,
			expectedStages:   []string{"scenario"},
			expectedSourceVs: map[string]string{"scenario": "v1-deterministic"},
		},
		{
			name:             "no_caches_run_dir_missing",
			seed:             func(t *testing.T, outDir, runID string) {},
			runID:            "scp-049-run-1",
			expectedStatus:   http.StatusOK,
			expectedStages:   []string{},
			expectedSourceVs: map[string]string{},
		},
		{
			name:             "run_not_found",
			seed:             func(t *testing.T, outDir, runID string) {},
			runID:            "scp-999-run-1",
			expectedStatus:   http.StatusNotFound,
			expectedErrCode:  "NOT_FOUND",
			expectedStages:   nil,
			expectedSourceVs: nil,
		},
		{
			name: "legacy_flat_json_corrupt",
			seed: func(t *testing.T, outDir, runID string) {
				// Legacy flat payload (no envelope_version) in new _cache/ path.
				// Entry must still surface; source_version extracted from flat JSON.
				writeCacheFile(t, outDir, runID, "_cache/research_cache.json",
					`{"scp_id":"049","source_version":"v1-old"}`)
			},
			runID:            "scp-049-run-1",
			expectedStatus:   http.StatusOK,
			expectedStages:   []string{"research"},
			expectedSourceVs: map[string]string{"research": "v1-old"},
		},
		{
			name: "malformed_json_in_cache",
			seed: func(t *testing.T, outDir, runID string) {
				// Unparseable JSON — entry must still surface, source_version=""
				writeCacheFile(t, outDir, runID, "_cache/research_cache.json", `{not-json{`)
			},
			runID:            "scp-049-run-1",
			expectedStatus:   http.StatusOK,
			expectedStages:   []string{"research"},
			expectedSourceVs: map[string]string{"research": ""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, outDir := newTestRunHandler(t)
			// Seed a run for cases that aren't testing the not-found path.
			// Use Create so the run exists in DB; Cache validates existence
			// via h.svc.Get before scanning disk.
			if tc.expectedStatus != http.StatusNotFound {
				createBody, _ := json.Marshal(map[string]string{"scp_id": "049"})
				createReq := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(createBody))
				createReq.Header.Set("Content-Type", "application/json")
				h.Create(httptest.NewRecorder(), createReq)
			}

			tc.seed(t, outDir, tc.runID)

			req := httptest.NewRequest(http.MethodGet, "/api/runs/"+tc.runID+"/cache", nil)
			req.SetPathValue("id", tc.runID)
			rec := httptest.NewRecorder()
			h.Cache(rec, req)

			testutil.AssertEqual(t, rec.Code, tc.expectedStatus)
			env := testutil.ReadJSON[cacheEnvelope](t, rec.Body)
			if tc.expectedErrCode != "" {
				if env.Error == nil {
					t.Fatalf("expected error envelope with code %q, got data=%+v", tc.expectedErrCode, env.Data)
				}
				testutil.AssertEqual(t, env.Error.Code, tc.expectedErrCode)
				return
			}
			if env.Data == nil {
				t.Fatalf("data is nil; raw body: %s", rec.Body.String())
			}

			gotStages := make([]string, 0, len(env.Data.Caches))
			gotSourceV := make(map[string]string, len(env.Data.Caches))
			for _, c := range env.Data.Caches {
				gotStages = append(gotStages, c.Stage)
				gotSourceV[c.Stage] = c.SourceVersion
			}
			sort.Strings(gotStages)
			sort.Strings(tc.expectedStages)
			if len(gotStages) != len(tc.expectedStages) {
				t.Fatalf("stages = %v, want %v", gotStages, tc.expectedStages)
			}
			for i, s := range gotStages {
				if s != tc.expectedStages[i] {
					t.Errorf("stages[%d] = %q, want %q", i, s, tc.expectedStages[i])
				}
			}
			for stage, want := range tc.expectedSourceVs {
				if got := gotSourceV[stage]; got != want {
					t.Errorf("source_version[%s] = %q, want %q", stage, got, want)
				}
			}
		})
	}
}

// TestRunHandler_Cache_StalenessReason_Surfaced verifies that a legacy flat-JSON
// file in the _cache/ path (no envelope_version field) surfaces staleness_reason
// "envelope_corrupt" in the GET /cache response, letting the operator know the
// cache is unusable without the runner having to be invoked.
func TestRunHandler_Cache_StalenessReason_Surfaced(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, outDir := newTestRunHandler(t)

	runID := "scp-049-run-1"
	createBody, _ := json.Marshal(map[string]string{"scp_id": "049"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	h.Create(httptest.NewRecorder(), createReq)

	// Write legacy flat JSON (no envelope_version) to the _cache/ path.
	writeCacheFile(t, outDir, runID, "_cache/research_cache.json",
		`{"scp_id":"049","source_version":"v1-old"}`)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/cache", nil)
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Cache(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusOK)
	env := testutil.ReadJSON[cacheEnvelope](t, rec.Body)
	if env.Data == nil || len(env.Data.Caches) != 1 {
		t.Fatalf("expected 1 cache entry, got data=%+v", env.Data)
	}
	got := env.Data.Caches[0]
	testutil.AssertEqual(t, got.Stage, "research")
	testutil.AssertEqual(t, got.StalenessReason, "envelope_corrupt")
	// source_version still surfaced from legacy probe
	testutil.AssertEqual(t, got.SourceVersion, "v1-old")
}

// TestRunHandler_Advance_DropCaches covers the drop_caches matrix:
// (a) drop existing file deletes it before the goroutine launches, (b) drop
// missing file is a no-op (no error), (c) unknown stage rejected with 400,
// (d) empty body is backward-compatible with no body. The advancer is
// stubbed via PrepareAdvance returning ErrValidation in (c) so we can assert
// 400 ahead of any FS work; for (a)/(b)/(d) we wire the engine so
// PrepareAdvance succeeds.
func TestRunHandler_Advance_DropCaches_Existing_DeletedBeforeGoroutine(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, outDir := newTestRunHandlerWithEngine(t, "049", "pending", "pending")

	runID := "scp-049-run-1"
	cachePath := writeCacheEnvelope(t, outDir, runID, "research", "v1-deterministic")

	// Sanity precondition.
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("seed cache not present: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"drop_caches": []string{"research"}})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/advance", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Advance(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusAccepted)

	// Synchronous deletion contract: by the time Advance has returned, the
	// file must already be gone — independent of when the goroutine runs.
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("_cache/research_cache.json should be gone after Advance returns; stat err = %v", err)
	}
}

func TestRunHandler_Advance_DropCaches_Missing_NoError(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, outDir := newTestRunHandlerWithEngine(t, "049", "pending", "pending")
	runID := "scp-049-run-1"

	// No cache file seeded — but the request asks to drop it. Idempotent.
	cachePath := filepath.Join(outDir, runID, "_cache", "research_cache.json")
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("precondition: cache file should not exist; stat err = %v", err)
	}

	body, _ := json.Marshal(map[string]any{"drop_caches": []string{"research"}})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/advance", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Advance(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusAccepted)
}

func TestRunHandler_Advance_DropCaches_UnknownStage_Rejected(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, outDir := newTestRunHandlerWithEngine(t, "049", "pending", "pending")
	runID := "scp-049-run-1"

	// Seed both real caches; the request includes one valid + one typo. The
	// validator rejects the entire request — neither file should be deleted.
	researchPath := writeCacheEnvelope(t, outDir, runID, "research", "v1-deterministic")
	structurePath := writeCacheEnvelope(t, outDir, runID, "structure", "v1-deterministic")

	body, _ := json.Marshal(map[string]any{"drop_caches": []string{"research", "typo"}})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/advance", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Advance(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "VALIDATION_ERROR")

	// Neither file should have been deleted on the failed-validation path.
	if _, err := os.Stat(researchPath); err != nil {
		t.Errorf("research cache was deleted despite validation failure: %v", err)
	}
	if _, err := os.Stat(structurePath); err != nil {
		t.Errorf("structure cache was deleted despite validation failure: %v", err)
	}
}

func TestRunHandler_Advance_EmptyBody_BackwardCompatible(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandlerWithEngine(t, "049", "pending", "pending")

	req := httptest.NewRequest(http.MethodPost, "/api/runs/scp-049-run-1/advance", nil)
	req.SetPathValue("id", "scp-049-run-1")
	rec := httptest.NewRecorder()
	h.Advance(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusAccepted)
}

// --- Resume drop_caches tests (spec: rewind-preserve-cache) ---
//
// Mirror the Advance drop_caches matrix on Resume so a failed Phase A run can
// selectively wipe `_cache/` entries before re-execution. The contract is
// identical to Advance: synchronous deletion BEFORE the goroutine, idempotent
// on missing files, whole-request rejection on unknown stages.

func TestRunHandler_Resume_DropCaches_Existing_DeletedBeforeGoroutine(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, outDir := newTestRunHandlerWithEngine(t, "049", "write", "failed")

	runID := "scp-049-run-1"
	researchPath := writeCacheEnvelope(t, outDir, runID, "research", "v1-deterministic")
	structurePath := writeCacheEnvelope(t, outDir, runID, "structure", "v1-deterministic")
	if _, err := os.Stat(researchPath); err != nil {
		t.Fatalf("seed research cache not present: %v", err)
	}
	if _, err := os.Stat(structurePath); err != nil {
		t.Fatalf("seed structure cache not present: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"drop_caches": []string{"research"}})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/resume", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusAccepted)

	if _, err := os.Stat(researchPath); !os.IsNotExist(err) {
		t.Fatalf("_cache/research_cache.json should be gone after Resume returns; stat err = %v", err)
	}
	// AC #4: Resume drops the listed cache only — sibling caches must survive
	// so the operator's per-row keep/drop intent is honored exactly.
	if _, err := os.Stat(structurePath); err != nil {
		t.Errorf("_cache/structure_cache.json must survive when not in drop_caches; stat err = %v", err)
	}
}

func TestRunHandler_Resume_DropCaches_Missing_NoError(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, outDir := newTestRunHandlerWithEngine(t, "049", "write", "failed")
	runID := "scp-049-run-1"

	cachePath := filepath.Join(outDir, runID, "_cache", "research_cache.json")
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("precondition: cache file should not exist; stat err = %v", err)
	}

	body, _ := json.Marshal(map[string]any{"drop_caches": []string{"research"}})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/resume", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusAccepted)
}

// TestRunHandler_Resume_DropCaches_ScenarioRejected pins the contract that
// drop_caches=["scenario"] is rejected on Resume — scenario.json is a Phase A
// output that downstream stages depend on, not a deterministic-agent envelope.
// Operators wanting a clean slate must use POST /rewind to scenario instead.
func TestRunHandler_Resume_DropCaches_ScenarioRejected(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, outDir := newTestRunHandlerWithEngine(t, "049", "image", "failed")
	runID := "scp-049-run-1"

	scenarioPath := filepath.Join(outDir, runID, "scenario.json")
	if err := os.MkdirAll(filepath.Dir(scenarioPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(scenarioPath, []byte(`{"scp_id":"049"}`), 0o644); err != nil {
		t.Fatalf("seed scenario.json: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"drop_caches": []string{"scenario"}})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/resume", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "VALIDATION_ERROR")

	if _, err := os.Stat(scenarioPath); err != nil {
		t.Errorf("scenario.json must survive a rejected drop_caches[\"scenario\"] resume: %v", err)
	}
}

func TestRunHandler_Resume_DropCaches_UnknownStage_Rejected(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, outDir := newTestRunHandlerWithEngine(t, "049", "write", "failed")
	runID := "scp-049-run-1"

	researchPath := writeCacheEnvelope(t, outDir, runID, "research", "v1-deterministic")
	structurePath := writeCacheEnvelope(t, outDir, runID, "structure", "v1-deterministic")

	body, _ := json.Marshal(map[string]any{"drop_caches": []string{"research", "typo"}})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+runID+"/resume", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	rec := httptest.NewRecorder()
	h.Resume(rec, req)

	testutil.AssertEqual(t, rec.Code, http.StatusBadRequest)
	env := testutil.ReadJSON[runEnvelope](t, rec.Body)
	testutil.AssertEqual(t, env.Error.Code, "VALIDATION_ERROR")

	if _, err := os.Stat(researchPath); err != nil {
		t.Errorf("research cache was deleted despite validation failure: %v", err)
	}
	if _, err := os.Stat(structurePath); err != nil {
		t.Errorf("structure cache was deleted despite validation failure: %v", err)
	}
}
