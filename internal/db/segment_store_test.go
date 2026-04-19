package db_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestSegmentStore_ListByRunID_Empty(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)

	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "scp-049-run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	got, err := store.ListByRunID(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func TestSegmentStore_ListByRunID_Populated(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)

	seedRunWithSegments(t, database, "scp-049-run-1")

	got, err := store.ListByRunID(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	for i, ep := range got {
		if ep.SceneIndex != i {
			t.Errorf("got[%d].SceneIndex = %d, want %d", i, ep.SceneIndex, i)
		}
	}
	if got[0].Narration == nil || *got[0].Narration == "" {
		t.Errorf("got[0].Narration is nil/empty")
	}
	if got[0].TTSPath == nil || *got[0].TTSPath == "" {
		t.Errorf("got[0].TTSPath is nil/empty")
	}
}

func TestSegmentStore_DeleteByRunID_ScopeIsolation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)

	seedRunWithSegments(t, database, "scp-049-run-1")
	seedRunWithSegments(t, database, "scp-096-run-1")

	n, err := store.DeleteByRunID(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("DeleteByRunID: %v", err)
	}
	if n != 3 {
		t.Errorf("rows deleted = %d, want 3", n)
	}

	got049, _ := store.ListByRunID(context.Background(), "scp-049-run-1")
	if len(got049) != 0 {
		t.Errorf("target run still has %d segments; expected 0", len(got049))
	}

	// Scope guarantee: unrelated run must be untouched.
	got096, _ := store.ListByRunID(context.Background(), "scp-096-run-1")
	if len(got096) != 3 {
		t.Errorf("scope leak: run 096 lost segments (%d remaining, want 3)", len(got096))
	}
}

func TestSegmentStore_DeleteByRunID_EmptyRun(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)

	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "scp-049-run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	n, err := store.DeleteByRunID(context.Background(), "scp-049-run-1")
	if err != nil {
		t.Fatalf("DeleteByRunID on empty run: %v", err)
	}
	if n != 0 {
		t.Errorf("rows deleted = %d, want 0", n)
	}
}

func TestSegmentStore_ClearClipPathsByRunID_ScopeIsolation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	// Seed two runs with clip_path values.
	for _, id := range []string{"scp-049-run-1", "scp-096-run-1"} {
		if _, err := database.ExecContext(ctx,
			`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, id, id); err != nil {
			t.Fatalf("seed run %s: %v", id, err)
		}
		if _, err := database.ExecContext(ctx,
			`INSERT INTO segments (run_id, scene_index, shot_count, clip_path, status)
			 VALUES (?, 0, 1, ?, 'completed')`, id, "clips/scene_01.mp4"); err != nil {
			t.Fatalf("seed segment: %v", err)
		}
	}

	n, err := store.ClearClipPathsByRunID(ctx, "scp-049-run-1")
	if err != nil {
		t.Fatalf("ClearClipPathsByRunID: %v", err)
	}
	if n != 1 {
		t.Errorf("rows cleared = %d, want 1", n)
	}

	// Target run's clip_path is NULL.
	target, _ := store.ListByRunID(ctx, "scp-049-run-1")
	if len(target) != 1 || target[0].ClipPath != nil {
		t.Errorf("target clip_path not cleared: %+v", target)
	}
	// Other run's clip_path preserved.
	other, _ := store.ListByRunID(ctx, "scp-096-run-1")
	if len(other) != 1 || other[0].ClipPath == nil || *other[0].ClipPath == "" {
		t.Errorf("scope leak: other run clip_path was cleared: %+v", other)
	}
}

func TestSegmentStore_ClearTTSArtifactsByRunID_ScopeIsolation(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	for _, id := range []string{"scp-049-run-1", "scp-096-run-1"} {
		if _, err := database.ExecContext(ctx,
			`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, id, id); err != nil {
			t.Fatalf("seed run %s: %v", id, err)
		}
		if _, err := database.ExecContext(ctx,
			`INSERT INTO segments (run_id, scene_index, shot_count, tts_path, tts_duration_ms, status)
			 VALUES (?, 0, 1, ?, 1234, 'completed')`, id, "tts/scene_01.wav"); err != nil {
			t.Fatalf("seed segment: %v", err)
		}
	}

	n, err := store.ClearTTSArtifactsByRunID(ctx, "scp-049-run-1")
	if err != nil {
		t.Fatalf("ClearTTSArtifactsByRunID: %v", err)
	}
	testutil.AssertEqual(t, n, int64(1))

	target, _ := store.ListByRunID(ctx, "scp-049-run-1")
	if len(target) != 1 || target[0].TTSPath != nil || target[0].TTSDurationMs != nil {
		t.Fatalf("target tts fields not cleared: %+v", target)
	}
	other, _ := store.ListByRunID(ctx, "scp-096-run-1")
	if len(other) != 1 || other[0].TTSPath == nil || *other[0].TTSPath == "" || other[0].TTSDurationMs == nil {
		t.Fatalf("scope leak: %+v", other)
	}
}

func TestSegmentStore_ClearImageArtifactsByRunID_PreservesTTS(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "scp-049-run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	shots := `[{"image_path":"images/scene_01/shot_01.png","duration_s":5.0,"transition":"ken_burns","visual_descriptor":"d1"}]`
	if _, err := database.ExecContext(ctx,
		`INSERT INTO segments (run_id, scene_index, shot_count, shots, tts_path, tts_duration_ms, status)
		 VALUES (?, 0, 1, ?, ?, 1500, 'completed')`,
		"scp-049-run-1", shots, "tts/scene_01.wav"); err != nil {
		t.Fatalf("seed segment: %v", err)
	}

	n, err := store.ClearImageArtifactsByRunID(ctx, "scp-049-run-1")
	if err != nil {
		t.Fatalf("ClearImageArtifactsByRunID: %v", err)
	}
	testutil.AssertEqual(t, n, int64(1))

	got, err := store.ListByRunID(ctx, "scp-049-run-1")
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(got))
	}
	if got[0].TTSPath == nil || *got[0].TTSPath != "tts/scene_01.wav" {
		t.Fatalf("tts path not preserved: %+v", got[0])
	}
	if got[0].TTSDurationMs == nil || *got[0].TTSDurationMs != 1500 {
		t.Fatalf("tts duration not preserved: %+v", got[0])
	}
	if len(got[0].Shots) != 1 || got[0].Shots[0].ImagePath != "" {
		t.Fatalf("image paths not cleared: %+v", got[0].Shots)
	}

	var raw string
	if err := database.QueryRowContext(ctx,
		`SELECT shots FROM segments WHERE run_id = ? AND scene_index = 0`, "scp-049-run-1").Scan(&raw); err != nil {
		t.Fatalf("select shots: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal updated shots: %v", err)
	}
	if decoded[0]["image_path"] != "" {
		t.Fatalf("raw shots image_path = %#v, want empty string", decoded[0]["image_path"])
	}
}

func TestSegmentStore_SaveImageShots_PersistsImagePathDurationAndTransition(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "scp-049-run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	shots := []domain.Shot{
		{ImagePath: "images/scene_01/shot_01.png", DurationSeconds: 4.2, Transition: domain.TransitionKenBurns, VisualDescriptor: "d1"},
		{ImagePath: "images/scene_01/shot_02.png", DurationSeconds: 3.8, Transition: domain.TransitionCrossDissolve, VisualDescriptor: "d2"},
	}
	if err := store.UpsertImageShots(ctx, "scp-049-run-1", 0, shots); err != nil {
		t.Fatalf("UpsertImageShots insert: %v", err)
	}

	got, err := store.ListByRunID(ctx, "scp-049-run-1")
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(got))
	}
	testutil.AssertEqual(t, got[0].ShotCount, 2)
	if len(got[0].Shots) != 2 {
		t.Fatalf("expected 2 shots persisted, got %d", len(got[0].Shots))
	}
	testutil.AssertEqual(t, got[0].Shots[0].ImagePath, "images/scene_01/shot_01.png")
	testutil.AssertEqual(t, got[0].Shots[0].Transition, domain.TransitionKenBurns)
	testutil.AssertEqual(t, got[0].Shots[0].VisualDescriptor, "d1")
	testutil.AssertEqual(t, got[0].Shots[1].ImagePath, "images/scene_01/shot_02.png")
	testutil.AssertEqual(t, got[0].Shots[1].DurationSeconds, 3.8)

	// Upsert again to verify deterministic replace-in-place behavior.
	replaced := []domain.Shot{
		{ImagePath: "images/scene_01/shot_01.png", DurationSeconds: 5.0, Transition: domain.TransitionHardCut, VisualDescriptor: "d1-v2"},
	}
	if err := store.UpsertImageShots(ctx, "scp-049-run-1", 0, replaced); err != nil {
		t.Fatalf("UpsertImageShots replace: %v", err)
	}
	again, err := store.ListByRunID(ctx, "scp-049-run-1")
	if err != nil {
		t.Fatalf("ListByRunID after replace: %v", err)
	}
	if len(again) != 1 {
		t.Fatalf("expected 1 segment after replace, got %d", len(again))
	}
	testutil.AssertEqual(t, again[0].ShotCount, 1)
	if len(again[0].Shots) != 1 {
		t.Fatalf("expected 1 shot after replace, got %d", len(again[0].Shots))
	}
	testutil.AssertEqual(t, again[0].Shots[0].DurationSeconds, 5.0)
	testutil.AssertEqual(t, again[0].Shots[0].Transition, domain.TransitionHardCut)
	testutil.AssertEqual(t, again[0].Shots[0].VisualDescriptor, "d1-v2")
}

func TestSegmentStore_ClearAndReupsertImageShots_CleanSlateSemantics(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "scp-049-run-clear", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	// Seed a row with both image and TTS data.
	shots := `[{"image_path":"images/scene_01/shot_01.png","duration_s":4.0,"transition":"ken_burns","visual_descriptor":"d1"}]`
	if _, err := database.ExecContext(ctx,
		`INSERT INTO segments (run_id, scene_index, shot_count, shots, tts_path, tts_duration_ms, status)
		 VALUES (?, 0, 1, ?, ?, 1200, 'completed')`,
		"scp-049-run-clear", shots, "tts/scene_01.wav"); err != nil {
		t.Fatalf("seed segment: %v", err)
	}

	// Step 1: clear image artifacts (simulates Phase B clean-slate clear step).
	n, err := store.ClearImageArtifactsByRunID(ctx, "scp-049-run-clear")
	if err != nil {
		t.Fatalf("ClearImageArtifactsByRunID: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 scene updated by clear, got %d", n)
	}

	// Step 2: upsert fresh shots (simulates image track regeneration).
	fresh := []domain.Shot{
		{ImagePath: "images/scene_01/shot_01.png", DurationSeconds: 5.5, Transition: domain.TransitionHardCut, VisualDescriptor: "d1"},
	}
	if err := store.UpsertImageShots(ctx, "scp-049-run-clear", 0, fresh); err != nil {
		t.Fatalf("UpsertImageShots after clear: %v", err)
	}

	// Verify: image path restored, TTS preserved, shot metadata correct.
	got, err := store.ListByRunID(ctx, "scp-049-run-clear")
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(got))
	}
	if got[0].TTSPath == nil || *got[0].TTSPath != "tts/scene_01.wav" {
		t.Fatalf("tts path lost after upsert: %+v", got[0])
	}
	if len(got[0].Shots) != 1 {
		t.Fatalf("expected 1 shot after upsert, got %d", len(got[0].Shots))
	}
	testutil.AssertEqual(t, got[0].Shots[0].ImagePath, "images/scene_01/shot_01.png")
	testutil.AssertEqual(t, got[0].Shots[0].DurationSeconds, 5.5)
	testutil.AssertEqual(t, got[0].Shots[0].Transition, domain.TransitionHardCut)
	testutil.AssertEqual(t, got[0].Shots[0].VisualDescriptor, "d1")
}

func TestSegmentStore_ListByRunID_DecodesShots(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)

	ctx := context.Background()
	if _, err := database.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "scp-049-run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	shots := `[{"image_path":"images/scene_01/shot_01.png","duration_s":5.0,"transition":"ken_burns","visual_descriptor":"d1"}]`
	if _, err := database.ExecContext(ctx,
		`INSERT INTO segments (run_id, scene_index, shot_count, shots, status)
		 VALUES (?, ?, ?, ?, ?)`, "scp-049-run-1", 0, 1, shots, "completed"); err != nil {
		t.Fatalf("seed segment: %v", err)
	}

	got, err := store.ListByRunID(ctx, "scp-049-run-1")
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if len(got) != 1 || len(got[0].Shots) != 1 {
		t.Fatalf("expected 1 segment with 1 shot, got %+v", got)
	}
	shot := got[0].Shots[0]
	if shot.ImagePath != "images/scene_01/shot_01.png" {
		t.Errorf("shot.ImagePath = %q, want images/scene_01/shot_01.png", shot.ImagePath)
	}
	if shot.Transition != "ken_burns" {
		t.Errorf("shot.Transition = %q, want ken_burns", shot.Transition)
	}
}

func TestSegmentStore_ListByRunID_DecodesReviewGateFields(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "scp-049-run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO segments (run_id, scene_index, review_status, safeguard_flags)
		 VALUES (?, ?, ?, ?)`,
		"scp-049-run-1", 0, "waiting_for_review", `["Safeguard Triggered: Minors"]`); err != nil {
		t.Fatalf("seed segment: %v", err)
	}

	got, err := store.ListByRunID(ctx, "scp-049-run-1")
	if err != nil {
		t.Fatalf("ListByRunID: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(got))
	}
	testutil.AssertEqual(t, got[0].ReviewStatus, "waiting_for_review")
	testutil.AssertEqual(t, got[0].SafeguardFlags[0], "Safeguard Triggered: Minors")
}

func seedRunWithSegments(t *testing.T, database *sql.DB, runID string) {
	t.Helper()
	ctx := context.Background()
	scpID := runID
	if _, err := database.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, runID, scpID); err != nil {
		t.Fatalf("seed run %s: %v", runID, err)
	}
	type seg struct {
		idx       int
		narration string
		ttsPath   string
		status    string
	}
	segments := []seg{
		{0, "scene zero", "tts/scene_01.wav", "completed"},
		{1, "scene one", "tts/scene_02.wav", "completed"},
		{2, "scene two", "", "pending"},
	}
	for _, s := range segments {
		var tts any = nil
		if s.ttsPath != "" {
			tts = s.ttsPath
		}
		if _, err := database.ExecContext(ctx,
			`INSERT INTO segments (run_id, scene_index, narration, shot_count, tts_path, status)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			runID, s.idx, s.narration, 1, tts, s.status); err != nil {
			t.Fatalf("seed segment %s[%d]: %v", runID, s.idx, err)
		}
	}
}

// ── UpsertTTSArtifact tests ──────────────────────────────────────────────────

func TestSegmentStore_UpsertTTSArtifact_InsertsWhenSegmentMissing(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx, `INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	err := store.UpsertTTSArtifact(ctx, "run-1", 0, "tts/scene_01.wav", 1234)
	if err != nil {
		t.Fatalf("UpsertTTSArtifact: %v", err)
	}

	ep, err := store.GetByRunIDAndSceneIndex(ctx, "run-1", 0)
	if err != nil {
		t.Fatalf("GetByRunIDAndSceneIndex: %v", err)
	}
	if ep.TTSPath == nil || *ep.TTSPath != "tts/scene_01.wav" {
		t.Errorf("TTSPath = %v, want \"tts/scene_01.wav\"", ep.TTSPath)
	}
	if ep.TTSDurationMs == nil || *ep.TTSDurationMs != 1234 {
		t.Errorf("TTSDurationMs = %v, want 1234", ep.TTSDurationMs)
	}
	testutil.AssertEqual(t, ep.Status, "pending")
}

func TestSegmentStore_UpsertTTSArtifact_UpdatesOnlyTTSColumnsWhenSegmentExists(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx, `INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	// Pre-insert a row with image shots and narration
	shots := `[{"image_path":"images/scene_01/shot_01.png","duration_s":4.2}]`
	if _, err := database.ExecContext(ctx,
		`INSERT INTO segments (run_id, scene_index, narration, shot_count, shots, status)
		 VALUES ('run-1', 0, 'narration text', 1, ?, 'pending')`, shots); err != nil {
		t.Fatalf("seed segment: %v", err)
	}

	err := store.UpsertTTSArtifact(ctx, "run-1", 0, "tts/scene_01.wav", 5000)
	if err != nil {
		t.Fatalf("UpsertTTSArtifact: %v", err)
	}

	ep, err := store.GetByRunIDAndSceneIndex(ctx, "run-1", 0)
	if err != nil {
		t.Fatalf("GetByRunIDAndSceneIndex: %v", err)
	}
	// TTS fields updated
	if ep.TTSPath == nil || *ep.TTSPath != "tts/scene_01.wav" {
		t.Errorf("TTSPath = %v, want \"tts/scene_01.wav\"", ep.TTSPath)
	}
	if ep.TTSDurationMs == nil || *ep.TTSDurationMs != 5000 {
		t.Errorf("TTSDurationMs = %v, want 5000", ep.TTSDurationMs)
	}
	// Image/narration fields preserved
	if ep.Narration == nil || *ep.Narration != "narration text" {
		t.Errorf("Narration = %v, want \"narration text\"", ep.Narration)
	}
	if ep.ShotCount != 1 {
		t.Errorf("ShotCount = %d, want 1", ep.ShotCount)
	}
	if len(ep.Shots) != 1 || ep.Shots[0].ImagePath != "images/scene_01/shot_01.png" {
		t.Errorf("Shots not preserved: %+v", ep.Shots)
	}
}

func TestSegmentStore_UpsertTTSArtifact_RejectsInvalidInput(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	cases := []struct {
		name          string
		runID         string
		sceneIndex    int
		ttsPath       string
		ttsDurationMs int64
	}{
		{"empty runID", "", 0, "tts/scene_01.wav", 100},
		{"negative sceneIndex", "run-1", -1, "tts/scene_01.wav", 100},
		{"empty ttsPath", "run-1", 0, "", 100},
		{"negative duration", "run-1", 0, "tts/scene_01.wav", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := store.UpsertTTSArtifact(ctx, tc.runID, tc.sceneIndex, tc.ttsPath, tc.ttsDurationMs)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, domain.ErrValidation) {
				t.Errorf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestSegmentStore_UpsertTTSArtifact_CoexistsWithUpsertImageShots(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx, `INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "run-1", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	// Insert image shots first
	shots := []domain.Shot{
		{ImagePath: "images/scene_01/shot_01.png", DurationSeconds: 3.0},
	}
	if err := store.UpsertImageShots(ctx, "run-1", 0, shots); err != nil {
		t.Fatalf("UpsertImageShots: %v", err)
	}

	// Then upsert TTS artifact
	if err := store.UpsertTTSArtifact(ctx, "run-1", 0, "tts/scene_01.wav", 2000); err != nil {
		t.Fatalf("UpsertTTSArtifact: %v", err)
	}

	ep, err := store.GetByRunIDAndSceneIndex(ctx, "run-1", 0)
	if err != nil {
		t.Fatalf("GetByRunIDAndSceneIndex: %v", err)
	}
	// Both sets of fields should be intact
	if len(ep.Shots) != 1 || ep.Shots[0].ImagePath != "images/scene_01/shot_01.png" {
		t.Errorf("image shots not preserved: %+v", ep.Shots)
	}
	if ep.TTSPath == nil || *ep.TTSPath != "tts/scene_01.wav" {
		t.Errorf("TTSPath = %v, want \"tts/scene_01.wav\"", ep.TTSPath)
	}
	if ep.TTSDurationMs == nil || *ep.TTSDurationMs != 2000 {
		t.Errorf("TTSDurationMs = %v, want 2000", ep.TTSDurationMs)
	}

	// Now test the reverse order: TTS then image
	if _, err := database.ExecContext(ctx, `INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "run-2", "049"); err != nil {
		t.Fatalf("seed run-2: %v", err)
	}
	if err := store.UpsertTTSArtifact(ctx, "run-2", 0, "tts/scene_01.wav", 3000); err != nil {
		t.Fatalf("UpsertTTSArtifact run-2: %v", err)
	}
	if err := store.UpsertImageShots(ctx, "run-2", 0, shots); err != nil {
		t.Fatalf("UpsertImageShots run-2: %v", err)
	}
	ep2, err := store.GetByRunIDAndSceneIndex(ctx, "run-2", 0)
	if err != nil {
		t.Fatalf("GetByRunIDAndSceneIndex run-2: %v", err)
	}
	if len(ep2.Shots) != 1 || ep2.Shots[0].ImagePath != "images/scene_01/shot_01.png" {
		t.Errorf("run-2 image shots not preserved: %+v", ep2.Shots)
	}
	if ep2.TTSPath == nil || *ep2.TTSPath != "tts/scene_01.wav" {
		t.Errorf("run-2 TTSPath = %v, want \"tts/scene_01.wav\"", ep2.TTSPath)
	}
}

// ── UpdateNarration ───────────────────────────────────────────────────────────

func TestSegmentStore_UpdateNarration_PersistsText(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "run-narr", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO segments (run_id, scene_index, narration, status) VALUES (?, 0, ?, 'pending')`,
		"run-narr", "원래 텍스트"); err != nil {
		t.Fatalf("seed segment: %v", err)
	}

	if err := store.UpdateNarration(ctx, "run-narr", 0, "새 텍스트"); err != nil {
		t.Fatalf("UpdateNarration: %v", err)
	}

	row, err := store.GetByRunIDAndSceneIndex(ctx, "run-narr", 0)
	if err != nil {
		t.Fatalf("GetByRunIDAndSceneIndex: %v", err)
	}
	if row.Narration == nil || *row.Narration != "새 텍스트" {
		t.Errorf("narration = %v, want \"새 텍스트\"", row.Narration)
	}
}

func TestSegmentStore_UpdateNarration_ReturnsNotFoundForMissingScene(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx,
		`INSERT INTO runs (id, scp_id) VALUES (?, ?)`, "run-narr2", "049"); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	err := store.UpdateNarration(ctx, "run-narr2", 99, "text")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSegmentStore_UpdateNarration_ReturnsValidationErrorForEmptyRunID(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	store := db.NewSegmentStore(database)

	err := store.UpdateNarration(context.Background(), "", 0, "text")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}
