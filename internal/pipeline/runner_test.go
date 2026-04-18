package pipeline

import (
	"context"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

// runnerStub is a minimal type that satisfies Runner. Its sole purpose
// is to make TestRunnerInterface_Signature a compile-time check that
// the Runner interface still has exactly:
//
//	Advance(context.Context, string) error
//	Resume(context.Context, string)  error
//
// If either method signature drifts (extra parameter, different return
// type), this file fails to compile.
type runnerStub struct{}

func (runnerStub) Advance(ctx context.Context, runID string) error { return nil }
func (runnerStub) Resume(ctx context.Context, runID string) error  { return nil }

var _ Runner = runnerStub{}

func TestRunnerInterface_Signature(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// If we reach here the compile-time assertion above succeeded. The
	// runtime call is just a smoke test that the methods are invokable.
	var r Runner = runnerStub{}
	if err := r.Advance(context.Background(), "x"); err != nil {
		t.Errorf("Advance: %v", err)
	}
	if err := r.Resume(context.Background(), "x"); err != nil {
		t.Errorf("Resume: %v", err)
	}
}
