package pipeline_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// --- harness ---------------------------------------------------------------

type rewindHarness struct {
	t        *testing.T
	database *sql.DB
	runStore *db.RunStore
	segStore *db.SegmentStore
	decStore *db.DecisionStore
	engine   *pipeline.Engine
	outDir   string
	runID    string
}

func newRewindHarness(t *testing.T) *rewindHarness {
	t.Helper()
	testutil.BlockExternalHTTP(t)
	database := testutil.NewTestDB(t)
	runStore := db.NewRunStore(database)
	segStore := db.NewSegmentStore(database)
	decStore := db.NewDecisionStore(database)
	outDir := t.TempDir()

	logger := slog.New(slog.NewTextHandler(&silentSink{}, nil))
	eng := pipeline.NewEngine(runStore, segStore, decStore, clock.RealClock{}, outDir, logger)
	eng.SetHITLSessionStore(&hitlSessionStoreShim{decStore: decStore})
	eng.SetRewindStore(runStore)
	eng.SetCancelRegistry(pipeline.NewCancelRegistry())

	run, err := runStore.Create(context.Background(), "049", outDir)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	return &rewindHarness{
		t:        t,
		database: database,
		runStore: runStore,
		segStore: segStore,
		decStore: decStore,
		engine:   eng,
		outDir:   outDir,
		runID:    run.ID,
	}
}

// silentSink discards engine logger output during tests.
type silentSink struct{}

func (s *silentSink) Write(p []byte) (int, error) { return len(p), nil }

// hitlSessionStoreShim composes db.DecisionStore (which already implements
// most of the surface) into pipeline.HITLSessionStore for the rewind tests.
type hitlSessionStoreShim struct {
	decStore *db.DecisionStore
}

func (s *hitlSessionStoreShim) ListByRunID(ctx context.Context, runID string) ([]*domain.Decision, error) {
	return s.decStore.ListByRunID(ctx, runID)
}
func (s *hitlSessionStoreShim) DecisionCountsByRunID(ctx context.Context, runID string) (pipeline.DecisionCounts, error) {
	c, err := s.decStore.DecisionCountsByRunID(ctx, runID)
	if err != nil {
		return pipeline.DecisionCounts{}, err
	}
	return pipeline.DecisionCounts{
		Approved:    c.Approved,
		Rejected:    c.Rejected,
		TotalScenes: c.TotalScenes,
	}, nil
}
func (s *hitlSessionStoreShim) GetSession(ctx context.Context, runID string) (*domain.HITLSession, error) {
	return s.decStore.GetSession(ctx, runID)
}
func (s *hitlSessionStoreShim) UpsertSession(ctx context.Context, sess *domain.HITLSession) error {
	return s.decStore.UpsertSession(ctx, sess)
}
func (s *hitlSessionStoreShim) DeleteSession(ctx context.Context, runID string) error {
	return s.decStore.DeleteSession(ctx, runID)
}

// seedRunAtMetadataAckWithArtifacts populates segments + decisions + on-disk
// artifacts so a rewind has something concrete to clean up. Returns nothing —
// the harness state is updated in place.
func (h *rewindHarness) seedRunAtMetadataAckWithArtifacts() {
	h.t.Helper()
	ctx := context.Background()

	// Move the run forward to metadata_ack/waiting with character pick + scenario path set.
	scenarioPath := "scenario.json"
	criticScore := 0.85
	frozen := "elderly scholar in dusty library"
	if _, err := h.database.ExecContext(ctx,
		`UPDATE runs SET stage = ?, status = ?, scenario_path = ?, critic_score = ?,
		 selected_character_id = ?, frozen_descriptor = ?, character_query_key = ?,
		 output_path = ?
		 WHERE id = ?`,
		string(domain.StageMetadataAck), string(domain.StatusWaiting),
		scenarioPath, criticScore,
		"cand-1", frozen, "scholar",
		"output.mp4",
		h.runID,
	); err != nil {
		h.t.Fatalf("seed metadata_ack: %v", err)
	}

	// Two segments with image+tts+clip.
	for i := 0; i < 2; i++ {
		shots := []domain.Shot{{ImagePath: "images/" + strconv.Itoa(i) + ".png", DurationSeconds: 1.5, Transition: "cut", VisualDescriptor: "x"}}
		shotsJSON, _ := json.Marshal(shots)
		if _, err := h.database.ExecContext(ctx,
			`INSERT INTO segments (run_id, scene_index, narration, shot_count, shots, tts_path, tts_duration_ms, clip_path, status)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending')`,
			h.runID, i, "narration "+strconv.Itoa(i), 1, string(shotsJSON),
			"tts/"+strconv.Itoa(i)+".mp3", 1500, "clips/"+strconv.Itoa(i)+".mp4",
		); err != nil {
			h.t.Fatalf("seed segment %d: %v", i, err)
		}
	}

	// Decisions covering every bucket: descriptor_edit (character_pick),
	// approve (batch_review), reject (batch_review).
	insertDecision := func(typ string, sceneID *string) {
		var sid sql.NullString
		if sceneID != nil {
			sid = sql.NullString{String: *sceneID, Valid: true}
		}
		if _, err := h.database.ExecContext(ctx,
			`INSERT INTO decisions (run_id, scene_id, decision_type) VALUES (?, ?, ?)`,
			h.runID, sid, typ,
		); err != nil {
			h.t.Fatalf("seed decision %s: %v", typ, err)
		}
	}
	insertDecision(domain.DecisionTypeDescriptorEdit, nil)
	scene0 := "0"
	scene1 := "1"
	insertDecision(domain.DecisionTypeApprove, &scene0)
	insertDecision(domain.DecisionTypeReject, &scene1)

	// hitl_sessions row for metadata_ack.
	if _, err := h.database.ExecContext(ctx,
		`INSERT INTO hitl_sessions (run_id, stage, scene_index, last_interaction_timestamp, snapshot_json)
		 VALUES (?, ?, ?, ?, ?)`,
		h.runID, string(domain.StageMetadataAck), 0, "2026-04-30T00:00:00Z", "{}",
	); err != nil {
		h.t.Fatalf("seed hitl_sessions: %v", err)
	}

	// On-disk artifacts.
	runDir := filepath.Join(h.outDir, h.runID)
	if err := os.MkdirAll(filepath.Join(runDir, "images"), 0755); err != nil {
		h.t.Fatalf("mkdir images: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(runDir, "tts"), 0755); err != nil {
		h.t.Fatalf("mkdir tts: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(runDir, "clips"), 0755); err != nil {
		h.t.Fatalf("mkdir clips: %v", err)
	}
	for _, p := range []string{
		"scenario.json",
		"images/0.png",
		"tts/0.mp3",
		"clips/0.mp4",
		"output.mp4",
		"metadata.json",
		"manifest.json",
	} {
		if err := os.WriteFile(filepath.Join(runDir, p), []byte("x"), 0644); err != nil {
			h.t.Fatalf("write %s: %v", p, err)
		}
	}
}

func (h *rewindHarness) countSegments() int {
	var n int
	if err := h.database.QueryRow(`SELECT COUNT(*) FROM segments WHERE run_id = ?`, h.runID).Scan(&n); err != nil {
		h.t.Fatalf("count segments: %v", err)
	}
	return n
}

func (h *rewindHarness) countDecisionsByType(typ string) int {
	var n int
	if err := h.database.QueryRow(
		`SELECT COUNT(*) FROM decisions WHERE run_id = ? AND decision_type = ?`,
		h.runID, typ,
	).Scan(&n); err != nil {
		h.t.Fatalf("count decisions: %v", err)
	}
	return n
}

func (h *rewindHarness) countHitlSessions() int {
	var n int
	if err := h.database.QueryRow(`SELECT COUNT(*) FROM hitl_sessions WHERE run_id = ?`, h.runID).Scan(&n); err != nil {
		h.t.Fatalf("count hitl_sessions: %v", err)
	}
	return n
}

func (h *rewindHarness) fileExists(rel string) bool {
	_, err := os.Stat(filepath.Join(h.outDir, h.runID, rel))
	return err == nil
}

// --- per-node behavior tests ---------------------------------------------

func TestEngineRewind_ToScenario_FullReset(t *testing.T) {
	h := newRewindHarness(t)
	h.seedRunAtMetadataAckWithArtifacts()

	run, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeScenario)
	if err != nil {
		t.Fatalf("Rewind(scenario): %v", err)
	}
	if run.Stage != domain.StagePending || run.Status != domain.StatusPending {
		t.Errorf("post-state = %s/%s, want pending/pending", run.Stage, run.Status)
	}
	if run.ScenarioPath != nil || run.SelectedCharacterID != nil ||
		run.FrozenDescriptor != nil || run.CharacterQueryKey != nil ||
		run.CriticScore != nil {
		t.Errorf("scenario rewind must clear every per-run pointer; got %+v", run)
	}
	if got := h.countSegments(); got != 0 {
		t.Errorf("segments after scenario rewind = %d, want 0", got)
	}
	if got := h.countDecisionsByType(domain.DecisionTypeApprove); got != 0 {
		t.Errorf("approve decisions after scenario rewind = %d, want 0", got)
	}
	if got := h.countDecisionsByType(domain.DecisionTypeDescriptorEdit); got != 0 {
		t.Errorf("descriptor_edit decisions after scenario rewind = %d, want 0", got)
	}
	if got := h.countHitlSessions(); got != 0 {
		t.Errorf("hitl_sessions after scenario rewind = %d, want 0", got)
	}
	for _, p := range []string{"scenario.json", "images", "tts", "clips", "output.mp4", "metadata.json", "manifest.json"} {
		if h.fileExists(p) {
			t.Errorf("scenario rewind should remove %s", p)
		}
	}
}

func TestEngineRewind_ToCharacter_PreservesPhaseA(t *testing.T) {
	h := newRewindHarness(t)
	h.seedRunAtMetadataAckWithArtifacts()

	run, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeCharacter)
	if err != nil {
		t.Fatalf("Rewind(character): %v", err)
	}
	if run.Stage != domain.StageCharacterPick || run.Status != domain.StatusWaiting {
		t.Errorf("post-state = %s/%s, want character_pick/waiting", run.Stage, run.Status)
	}
	if run.ScenarioPath == nil {
		t.Errorf("character rewind must preserve scenario_path")
	}
	if run.CriticScore == nil {
		t.Errorf("character rewind must preserve critic_score")
	}
	if run.SelectedCharacterID != nil || run.FrozenDescriptor != nil || run.CharacterQueryKey != nil {
		t.Errorf("character rewind must clear character pick: %+v", run)
	}
	if got := h.countSegments(); got != 2 {
		t.Errorf("segments after character rewind = %d, want 2 (preserved)", got)
	}
	if got := h.countDecisionsByType(domain.DecisionTypeApprove); got != 0 {
		t.Errorf("approve decisions after character rewind = %d, want 0", got)
	}
	if got := h.countDecisionsByType(domain.DecisionTypeDescriptorEdit); got != 0 {
		t.Errorf("descriptor_edit decisions after character rewind = %d, want 0", got)
	}
	// hitl_sessions row must exist for character_pick after rewind.
	if got := h.countHitlSessions(); got != 1 {
		t.Errorf("hitl_sessions after character rewind = %d, want 1 (HITL invariant)", got)
	}
	if !h.fileExists("scenario.json") {
		t.Errorf("scenario.json must be preserved on character rewind")
	}
	for _, p := range []string{"images", "tts", "clips", "output.mp4", "metadata.json", "manifest.json"} {
		if h.fileExists(p) {
			t.Errorf("character rewind should remove %s", p)
		}
	}
}

func TestEngineRewind_ToAssets_KeepsCharacter(t *testing.T) {
	h := newRewindHarness(t)
	h.seedRunAtMetadataAckWithArtifacts()

	run, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeAssets)
	if err != nil {
		t.Fatalf("Rewind(assets): %v", err)
	}
	if run.Stage != domain.StageImage || run.Status != domain.StatusWaiting {
		t.Errorf("post-state = %s/%s, want image/waiting", run.Stage, run.Status)
	}
	if run.SelectedCharacterID == nil || run.FrozenDescriptor == nil {
		t.Errorf("assets rewind must preserve character pick: %+v", run)
	}
	if got := h.countSegments(); got != 2 {
		t.Errorf("segments after assets rewind = %d, want 2 (preserved with cleared artifacts)", got)
	}
	// Approve decisions go (bucket = batch_review >= image).
	if got := h.countDecisionsByType(domain.DecisionTypeApprove); got != 0 {
		t.Errorf("approve decisions after assets rewind = %d, want 0", got)
	}
	// descriptor_edit (bucket = character_pick) is preserved (bucket < image).
	if got := h.countDecisionsByType(domain.DecisionTypeDescriptorEdit); got != 1 {
		t.Errorf("descriptor_edit after assets rewind = %d, want 1 (preserved)", got)
	}
	if got := h.countHitlSessions(); got != 0 {
		t.Errorf("hitl_sessions after assets rewind = %d, want 0 (image is non-HITL)", got)
	}
	if !h.fileExists("scenario.json") {
		t.Errorf("scenario.json must be preserved on assets rewind")
	}
}

func TestEngineRewind_ToAssemble_KeepsImageAndTTS(t *testing.T) {
	h := newRewindHarness(t)
	h.seedRunAtMetadataAckWithArtifacts()

	run, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeAssemble)
	if err != nil {
		t.Fatalf("Rewind(assemble): %v", err)
	}
	if run.Stage != domain.StageAssemble || run.Status != domain.StatusWaiting {
		t.Errorf("post-state = %s/%s, want assemble/waiting", run.Stage, run.Status)
	}
	if got := h.countSegments(); got != 2 {
		t.Errorf("segments after assemble rewind = %d, want 2 (preserved)", got)
	}
	// All decisions are preserved — both buckets (character_pick=8, batch_review=11)
	// are strictly before assemble=12.
	if got := h.countDecisionsByType(domain.DecisionTypeApprove); got != 1 {
		t.Errorf("approve decisions after assemble rewind = %d, want 1 (preserved)", got)
	}
	if got := h.countDecisionsByType(domain.DecisionTypeDescriptorEdit); got != 1 {
		t.Errorf("descriptor_edit after assemble rewind = %d, want 1 (preserved)", got)
	}
	if !h.fileExists("scenario.json") || !h.fileExists("images") || !h.fileExists("tts") {
		t.Errorf("assemble rewind must preserve scenario.json + images/ + tts/")
	}
	for _, p := range []string{"clips", "output.mp4", "metadata.json", "manifest.json"} {
		if h.fileExists(p) {
			t.Errorf("assemble rewind should remove %s", p)
		}
	}
}

// --- guard / idempotency / invariant tests --------------------------------

func TestEngineRewind_Idempotent(t *testing.T) {
	h := newRewindHarness(t)
	h.seedRunAtMetadataAckWithArtifacts()

	if _, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeAssets); err != nil {
		t.Fatalf("first Rewind: %v", err)
	}
	// Second call: target = assets, but current stage is now image, so target
	// is not strictly before current → the API must reject as ErrConflict.
	// This is the correct idempotent behavior — rewind is a one-way trip
	// per request, and the invariant is "result of the operation matches
	// the post-state regardless of how many times you call". We assert
	// the FE-equivalent: caller sees 409 and the run state is unchanged.
	_, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeAssets)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second Rewind(assets): want ErrConflict, got %v", err)
	}
	run, err := h.runStore.Get(context.Background(), h.runID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if run.Stage != domain.StageImage || run.Status != domain.StatusWaiting {
		t.Errorf("idempotent retry must not change state; got %s/%s", run.Stage, run.Status)
	}

	// But rewinding to a STRICTLY EARLIER node from image is valid — still
	// idempotent in the "no FS error" sense even though artifacts are
	// already gone. This exercises the FS no-op path.
	if _, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeScenario); err != nil {
		t.Fatalf("Rewind(scenario) after assets rewind: %v", err)
	}
	if _, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeScenario); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("re-Rewind(scenario) at pending: want ErrConflict (target == current), got %v", err)
	}
}

func TestEngineRewind_RejectsTargetAtOrAfterCurrent(t *testing.T) {
	h := newRewindHarness(t)
	// Move run to image stage.
	if _, err := h.database.Exec(
		`UPDATE runs SET stage = ?, status = ? WHERE id = ?`,
		string(domain.StageImage), string(domain.StatusWaiting), h.runID,
	); err != nil {
		t.Fatalf("seed image: %v", err)
	}

	// target=assets (entry=image) at current=image → not strictly before.
	_, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeAssets)
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("rewind to current-node: want ErrConflict, got %v", err)
	}
	// target=assemble (entry=assemble) at current=image → after, not before.
	_, err = h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeAssemble)
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("rewind to later-node: want ErrConflict, got %v", err)
	}
	// target=character (entry=character_pick) at current=image → before, allowed.
	if _, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeCharacter); err != nil {
		t.Errorf("rewind to earlier-node should succeed: %v", err)
	}
}

func TestEngineRewind_RejectsInvalidNode(t *testing.T) {
	h := newRewindHarness(t)
	if _, err := h.database.Exec(
		`UPDATE runs SET stage = ?, status = ? WHERE id = ?`,
		string(domain.StageMetadataAck), string(domain.StatusWaiting), h.runID,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeKey("complete"))
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("invalid node: want ErrValidation, got %v", err)
	}
}

// TestEngineRewind_HITLInvariant checks the four representative paths that
// must satisfy the invariant "hitl_sessions exists iff status=waiting AND
// stage∈HITL".
func TestEngineRewind_HITLInvariant(t *testing.T) {
	cases := []struct {
		name       string
		target     pipeline.StageNodeKey
		wantRowCnt int
	}{
		{"to-scenario-removes-row", pipeline.StageNodeScenario, 0},
		{"to-character-creates-row", pipeline.StageNodeCharacter, 1},
		{"to-assets-removes-row", pipeline.StageNodeAssets, 0},
		{"to-assemble-removes-row", pipeline.StageNodeAssemble, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newRewindHarness(t)
			h.seedRunAtMetadataAckWithArtifacts()
			if _, err := h.engine.Rewind(context.Background(), h.runID, tc.target); err != nil {
				t.Fatalf("Rewind(%s): %v", tc.target, err)
			}
			if got := h.countHitlSessions(); got != tc.wantRowCnt {
				t.Errorf("hitl_sessions count = %d, want %d", got, tc.wantRowCnt)
			}
		})
	}
}

// TestEngineRewind_CancelsInflightWorker exercises the cancel-and-wait
// race protocol. A "worker" goroutine registers with the engine's cancel
// registry and blocks on its context. The rewind must (1) signal cancel,
// (2) the worker observes cancel and exits, (3) the rewind completes
// without races. We assert the worker saw ctx.Err() before rewind returned.
func TestEngineRewind_CancelsInflightWorker(t *testing.T) {
	h := newRewindHarness(t)
	h.seedRunAtMetadataAckWithArtifacts()

	// Reach into the engine's registry by re-using the same registry
	// object through the test seam: SetCancelRegistry was invoked in the
	// harness, so we replicate the same registry here for the worker.
	// The harness builds its own; we replace it so we can register a
	// worker under the same registry instance the engine uses.
	reg := pipeline.NewCancelRegistry()
	h.engine.SetCancelRegistry(reg)

	var workerCancelled int32
	workerStarted := make(chan struct{})
	workerExited := make(chan struct{})
	go func() {
		ctx, _, release := reg.Begin(context.Background(), h.runID)
		defer release()
		close(workerStarted)
		// Simulate a long-running stage that only exits on cancellation.
		select {
		case <-ctx.Done():
			atomic.StoreInt32(&workerCancelled, 1)
		case <-time.After(5 * time.Second):
			// Safety net: if cancel never arrives, exit anyway.
		}
		close(workerExited)
	}()

	<-workerStarted

	doneRewind := make(chan error, 1)
	go func() {
		_, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeScenario)
		doneRewind <- err
	}()

	// Worker must exit BEFORE rewind returns (rewind blocks on CancelAndWait).
	select {
	case <-workerExited:
	case <-time.After(2 * time.Second):
		t.Fatalf("worker never exited within 2s — cancel did not propagate")
	}

	select {
	case err := <-doneRewind:
		if err != nil {
			t.Fatalf("rewind: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("rewind did not complete after worker exited")
	}

	if atomic.LoadInt32(&workerCancelled) == 0 {
		t.Errorf("worker did not observe ctx.Done() — cancel signal was not propagated")
	}
}

// TestEngineRewind_DoubleClickSerialized verifies the per-run mutex: two
// concurrent rewind requests for the same run produce one success + one
// either success-or-conflict (depending on which finishes first), never a
// torn DB state.
func TestEngineRewind_DoubleClickSerialized(t *testing.T) {
	h := newRewindHarness(t)
	h.seedRunAtMetadataAckWithArtifacts()

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			_, err := h.engine.Rewind(context.Background(), h.runID, pipeline.StageNodeAssets)
			results[i] = err
		}()
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrConflict):
			// Expected: second-runner sees current=image after first's commit.
		default:
			t.Errorf("unexpected error from concurrent rewind: %v", err)
		}
	}
	if successes < 1 {
		t.Errorf("at least one concurrent rewind must succeed, got %d", successes)
	}

	run, err := h.runStore.Get(context.Background(), h.runID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if run.Stage != domain.StageImage || run.Status != domain.StatusWaiting {
		t.Errorf("end state after concurrent rewind = %s/%s, want image/waiting", run.Stage, run.Status)
	}
}

// TestEngineRewind_NotFound ensures rewind on a non-existent run returns ErrNotFound.
func TestEngineRewind_NotFound(t *testing.T) {
	h := newRewindHarness(t)
	_, err := h.engine.Rewind(context.Background(), "scp-049-run-99", pipeline.StageNodeScenario)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown run: want ErrNotFound, got %v", err)
	}
}
