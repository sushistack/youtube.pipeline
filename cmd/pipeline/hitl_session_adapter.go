package main

import (
	"context"

	"github.com/sushistack/youtube.pipeline/internal/db"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline"
)

// hitlSessionStoreAdapter adapts *db.DecisionStore to pipeline.HITLSessionStore.
// pipeline/ deliberately mirrors db.DecisionCounts at its boundary (so the
// pipeline package does not import db/), which makes *db.DecisionStore not a
// structural match — this adapter performs the field-by-field translation.
type hitlSessionStoreAdapter struct {
	store *db.DecisionStore
}

func newHITLSessionStoreAdapter(store *db.DecisionStore) *hitlSessionStoreAdapter {
	return &hitlSessionStoreAdapter{store: store}
}

func (a *hitlSessionStoreAdapter) ListByRunID(ctx context.Context, runID string) ([]*domain.Decision, error) {
	return a.store.ListByRunID(ctx, runID)
}

func (a *hitlSessionStoreAdapter) DecisionCountsByRunID(ctx context.Context, runID string) (pipeline.DecisionCounts, error) {
	counts, err := a.store.DecisionCountsByRunID(ctx, runID)
	if err != nil {
		return pipeline.DecisionCounts{}, err
	}
	return pipeline.DecisionCounts{
		Approved:    counts.Approved,
		Rejected:    counts.Rejected,
		TotalScenes: counts.TotalScenes,
	}, nil
}

func (a *hitlSessionStoreAdapter) GetSession(ctx context.Context, runID string) (*domain.HITLSession, error) {
	return a.store.GetSession(ctx, runID)
}

func (a *hitlSessionStoreAdapter) UpsertSession(ctx context.Context, session *domain.HITLSession) error {
	return a.store.UpsertSession(ctx, session)
}

func (a *hitlSessionStoreAdapter) DeleteSession(ctx context.Context, runID string) error {
	return a.store.DeleteSession(ctx, runID)
}
