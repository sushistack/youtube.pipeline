// Package service contains business logic that orchestrates domain, db, and pipeline.
package service

import (
	"context"
	"fmt"
	"regexp"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

// scpIDPattern matches allowed characters in an SCP identifier: alphanumeric,
// underscore, and hyphen. Deliberately rejects `/`, `..`, and control bytes
// to prevent path-escape via the run output directory.
var scpIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// RunStore is the persistence interface for pipeline runs.
// Defined here (consumer) and implemented by internal/db.RunStore.
type RunStore interface {
	Create(ctx context.Context, scpID, outputDir string) (*domain.Run, error)
	Get(ctx context.Context, id string) (*domain.Run, error)
	List(ctx context.Context) ([]*domain.Run, error)
	Cancel(ctx context.Context, id string) error
	MarkComplete(ctx context.Context, id string) error // NEW: sets stage=complete, status=completed
}

// Resumer is the minimal engine surface that RunService delegates Resume to.
// *pipeline.Engine satisfies this interface. Declaring it here keeps the
// dependency direction one-way: service → pipeline interface. The returned
// report carries FS/DB inconsistency descriptions surfaced to the caller as
// warnings (CLI `--force` output, API response `warnings` field).
type Resumer interface {
	ResumeWithOptions(ctx context.Context, runID string, opts pipeline.ResumeOptions) (*domain.InconsistencyReport, error)
}

// RunService implements pipeline run lifecycle management.
type RunService struct {
	store   RunStore
	resumer Resumer
}

// NewRunService creates a RunService backed by the provided RunStore.
// resumer MAY be nil for call paths that never invoke Resume (tests, tools);
// runtime Resume calls with a nil resumer return ErrValidation rather than panicking.
func NewRunService(store RunStore, resumer Resumer) *RunService {
	return &RunService{store: store, resumer: resumer}
}

// Create creates a new pipeline run for the given SCP ID and returns it.
// scpID is validated against scpIDPattern to block path-escape and injection.
func (s *RunService) Create(ctx context.Context, scpID, outputDir string) (*domain.Run, error) {
	if !scpIDPattern.MatchString(scpID) {
		return nil, fmt.Errorf("create run: invalid scp_id %q: %w", scpID, domain.ErrValidation)
	}
	run, err := s.store.Create(ctx, scpID, outputDir)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	return run, nil
}

// Get returns the run with the given ID.
func (s *RunService) Get(ctx context.Context, id string) (*domain.Run, error) {
	run, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	return run, nil
}

// List returns all pipeline runs ordered by creation time.
func (s *RunService) List(ctx context.Context) ([]*domain.Run, error) {
	runs, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	return runs, nil
}

// Cancel cancels the run with the given ID.
// Returns ErrNotFound if the run does not exist.
// Returns ErrConflict if the run is not in a cancellable state.
func (s *RunService) Cancel(ctx context.Context, id string) error {
	if err := s.store.Cancel(ctx, id); err != nil {
		return fmt.Errorf("cancel run: %w", err)
	}
	return nil
}

// Resume re-enters the failed (or waiting) stage of a run and returns the
// updated run snapshot plus any FS/DB inconsistency warnings that were
// bypassed via force. When force is true, inconsistencies are tolerated
// rather than aborting; the report lists what was bypassed so the caller can
// surface them (CLI/API warnings).
//
// Error classes propagated from the engine:
//   - ErrNotFound:   run does not exist.
//   - ErrConflict:   run state is not resumable (pending/running/completed/cancelled).
//   - ErrValidation: FS/DB inconsistency without force, or resumer not configured.
func (s *RunService) Resume(ctx context.Context, id string, force bool) (*domain.Run, *domain.InconsistencyReport, error) {
	if s.resumer == nil {
		return nil, nil, fmt.Errorf("resume run: %w: engine not configured", domain.ErrValidation)
	}
	report, err := s.resumer.ResumeWithOptions(ctx, id, pipeline.ResumeOptions{Force: force})
	if err != nil {
		return nil, report, fmt.Errorf("resume run: %w", err)
	}
	run, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, report, fmt.Errorf("resume run: reload: %w", err)
	}
	return run, report, nil
}

// AcknowledgeMetadata transitions a run from metadata_ack+waiting to complete+completed.
// Returns ErrConflict if the run is not in the correct stage/status.
// This is the NFR-L1 enforcement point: ready-for-upload is ONLY reachable via this path.
func (s *RunService) AcknowledgeMetadata(ctx context.Context, runID string) (*domain.Run, error) {
	run, err := s.store.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Stage != domain.StageMetadataAck || run.Status != domain.StatusWaiting {
		return nil, fmt.Errorf("acknowledge metadata: run is not awaiting metadata acknowledgment: %w", domain.ErrConflict)
	}
	if err := s.store.MarkComplete(ctx, runID); err != nil {
		return nil, fmt.Errorf("acknowledge metadata: persist: %w", err)
	}
	return s.store.Get(ctx, runID)
}
