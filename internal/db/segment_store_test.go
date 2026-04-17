package db_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/db"
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
