package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestValidateDistinctProviders_DifferentOK(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	if err := ValidateDistinctProviders("openai", "anthropic"); err != nil {
		t.Fatalf("ValidateDistinctProviders: %v", err)
	}
}

func TestValidateDistinctProviders_SameRejected(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	err := ValidateDistinctProviders("openai", "openai")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// (a) Must wrap domain.ErrValidation so callers (e.g., domain.Classify,
	// HTTP mappers) can classify this as a 400 validation failure.
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected errors.Is(err, domain.ErrValidation), got %v", err)
	}
	// (b) Must preserve the exact spec-mandated substring so that
	// internal/config/doctor.go's output matching (and the doctor CLI
	// contract at AC-WRITER-CRITIC) continues to resolve.
	if !strings.Contains(err.Error(), "Writer and Critic must use different LLM providers") {
		t.Errorf("error %q missing spec substring", err.Error())
	}
}

func TestValidateDistinctProviders_EmptyRejected(t *testing.T) {
	testutil.BlockExternalHTTP(t)
	err := ValidateDistinctProviders("", "anthropic")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestPhaseARunner_Run_ProviderGuardTriggersBeforeWriterCall(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	var writerCalls int
	b := defaultRunnerBuilder(t)
	b.writerProvider = "openai"
	b.criticProvider = "openai"
	b.writer = func(ctx context.Context, state *agents.PipelineState) error {
		writerCalls++
		return nil
	}

	err := b.build(t).Run(context.Background(), newState())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected errors.Is(err, domain.ErrValidation), got %v", err)
	}
	if !strings.Contains(err.Error(), "Writer and Critic must use different LLM providers") {
		t.Errorf("error %q missing spec substring", err.Error())
	}
	testutil.AssertEqual(t, writerCalls, 0)
}
