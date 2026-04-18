package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/api"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/service"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// newStatusHandler wires a RunHandler pre-seeded with the given fixture
// and returns (handler, outDir). The caller invokes Status via httptest.
func newStatusHandler(t *testing.T, fixtureName string) *api.RunHandler {
	t.Helper()
	database := testutil.LoadRunStateFixture(t, fixtureName)
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	decisionStore := db.NewDecisionStore(database)
	logger, _ := testutil.CaptureLog(t)
	hitl := service.NewHITLService(store, decisionStore, logger)
	return api.NewRunHandler(svc, hitl, t.TempDir(), logger)
}

func TestRunHandler_Status_NotPaused(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	// Seed a running (non-HITL) run.
	database := testutil.NewTestDB(t)
	if _, err := database.Exec(
		`INSERT INTO runs (id, scp_id, stage, status) VALUES ('r-running', '999', 'write', 'running')`,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	store := db.NewRunStore(database)
	svc := service.NewRunService(store, nil)
	decisionStore := db.NewDecisionStore(database)
	logger, _ := testutil.CaptureLog(t)
	hitl := service.NewHITLService(store, decisionStore, logger)
	h := api.NewRunHandler(svc, hitl, t.TempDir(), logger)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/r-running/status", nil)
	req.SetPathValue("id", "r-running")
	w := httptest.NewRecorder()
	h.Status(w, req)

	testutil.AssertEqual(t, w.Code, http.StatusOK)
	body := w.Body.String()

	// run key must be present.
	if !strings.Contains(body, `"run"`) {
		t.Fatalf("non-HITL status must contain \"run\" key, body: %s", body)
	}
	// HITL-specific keys must be absent.
	for _, banned := range []string{
		`"paused_position"`,
		`"changes_since_last_interaction"`,
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("non-HITL status should not contain %s, body: %s", banned, body)
		}
	}
	// summary key must be absent for non-HITL runs (omitempty empty string).
	if strings.Contains(body, `"summary"`) {
		t.Fatalf("non-HITL status should not have summary key, body: %s", body)
	}
}

func TestRunHandler_Status_Paused(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h := newStatusHandler(t, "paused_at_batch_review")
	req := httptest.NewRequest(http.MethodGet, "/api/runs/scp-049-run-1/status", nil)
	req.SetPathValue("id", "scp-049-run-1")
	w := httptest.NewRecorder()
	h.Status(w, req)

	testutil.AssertEqual(t, w.Code, http.StatusOK)
	body := w.Body.String()
	for _, want := range []string{
		`"paused_position"`,
		`"decisions_summary"`,
		`"summary":"Run scp-049-run-1: reviewing scene 3 of 3, 2 approved, 0 rejected"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("want %s in body, got %s", want, body)
		}
	}
	// Snapshot JSON must NEVER leak into the response.
	if strings.Contains(body, `snapshot_json`) || strings.Contains(body, `SnapshotJSON`) {
		t.Fatalf("snapshot_json leaked into response: %s", body)
	}
}

func TestRunHandler_Status_NotFound(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h, _ := newTestRunHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/runs/missing/status", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.Status(w, req)

	testutil.AssertEqual(t, w.Code, http.StatusNotFound)
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if env.Error == nil || env.Error.Code != "NOT_FOUND" {
		t.Fatalf("expected NOT_FOUND error, got %+v", env.Error)
	}
}

// TestRunHandler_Status_JSONSchemaStable pins the response shape to a golden
// file. Volatile fields (created_at, updated_at) are substituted before
// comparison so the fixture timestamps don't drift with clock changes.
func TestRunHandler_Status_JSONSchemaStable(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	h := newStatusHandler(t, "paused_10scenes_4approved")
	req := httptest.NewRequest(http.MethodGet, "/api/runs/scp-049-run-golden/status", nil)
	req.SetPathValue("id", "scp-049-run-golden")
	w := httptest.NewRecorder()
	h.Status(w, req)

	goldenPath := findGolden(t, "status_paused.json")
	goldenRaw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	assertJSONStructuralMatch(t, goldenRaw, w.Body.Bytes())
}

// findGolden walks up from cwd until it finds testdata/golden/{name}.
func findGolden(t *testing.T, name string) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		candidate := filepath.Join(dir, "testdata", "golden", name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("golden %s not found", name)
		}
		dir = parent
	}
}

// assertJSONStructuralMatch compares two JSON blobs as parsed maps; keys that
// carry volatile timestamp values (created_at, updated_at, last_interaction_timestamp)
// are compared for existence only, not value. This prevents flaky golden
// tests when fixture timestamps drift.
func assertJSONStructuralMatch(t *testing.T, want, got []byte) {
	t.Helper()
	var wantMap, gotMap map[string]any
	if err := json.Unmarshal(want, &wantMap); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if err := json.Unmarshal(got, &gotMap); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	stripVolatileTimestamps(wantMap)
	stripVolatileTimestamps(gotMap)
	if !reflect.DeepEqual(wantMap, gotMap) {
		wantJSON, _ := json.MarshalIndent(wantMap, "", "  ")
		gotJSON, _ := json.MarshalIndent(gotMap, "", "  ")
		t.Fatalf("JSON mismatch\nwant:\n%s\n\ngot:\n%s", wantJSON, gotJSON)
	}
}

func stripVolatileTimestamps(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for _, key := range []string{"created_at", "updated_at", "last_interaction_timestamp"} {
		if _, present := m[key]; present {
			m[key] = "<timestamp>"
		}
	}
	for _, child := range m {
		switch c := child.(type) {
		case map[string]any:
			stripVolatileTimestamps(c)
		case []any:
			for _, item := range c {
				stripVolatileTimestamps(item)
			}
		}
	}
}
