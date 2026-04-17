package pipeline

import (
	"fmt"
	"sync"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// CostAccumulator tracks per-stage and per-run cost spend and fires as a
// circuit breaker when either cap is breached. It is goroutine-safe:
// Phase B's parallel image+TTS tracks share a single instance.
//
// Design (story 2.4 AC-COST-ACCUMULATOR):
//   - Every Add records the cost (NFR-C3: no truncation, no sampling).
//   - Add returns ErrCostCapExceeded when the post-add total breaches a cap.
//   - Once tripped, the accumulator stays tripped: all subsequent Adds
//     record AND return the wrapped error. The caller is responsible for
//     halting further external API calls.
//   - A perStageCap of 0 means "no per-stage cap" (defensive default for
//     stages that have no mapped cap); same for perRunCap.
type CostAccumulator struct {
	mu           sync.Mutex
	stageTotals  map[domain.Stage]float64
	runTotal     float64
	perStageCaps map[domain.Stage]float64
	perRunCap    float64
	tripped      bool
	tripStage    domain.Stage
	tripReason   string
}

// NewCostAccumulator constructs an accumulator with the supplied caps. Pass
// a nil map to disable per-stage enforcement entirely. perRunCap ≤ 0 disables
// the per-run cap.
func NewCostAccumulator(perStageCaps map[domain.Stage]float64, perRunCap float64) *CostAccumulator {
	caps := make(map[domain.Stage]float64, len(perStageCaps))
	for k, v := range perStageCaps {
		caps[k] = v
	}
	return &CostAccumulator{
		stageTotals:  make(map[domain.Stage]float64),
		perStageCaps: caps,
		perRunCap:    perRunCap,
	}
}

// Add records additional cost spent for the given stage. Returns
// ErrCostCapExceeded (wrapped) when the post-add stage total exceeds
// perStageCaps[stage] OR the post-add run total exceeds perRunCap. The cost
// is always recorded even on cap violation (NFR-C3). Negative cost returns
// ErrValidation without recording.
func (a *CostAccumulator) Add(stage domain.Stage, costUSD float64) error {
	if costUSD < 0 {
		return fmt.Errorf("cost accumulator: negative cost %.4f: %w", costUSD, domain.ErrValidation)
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	a.stageTotals[stage] += costUSD
	a.runTotal += costUSD

	if a.tripped {
		return a.wrappedCapErrLocked()
	}

	if cap, ok := a.perStageCaps[stage]; ok && cap > 0 && a.stageTotals[stage] > cap {
		a.tripped = true
		a.tripStage = stage
		a.tripReason = "stage_cap"
		return a.wrappedCapErrLocked()
	}

	if a.perRunCap > 0 && a.runTotal > a.perRunCap {
		a.tripped = true
		a.tripStage = stage
		a.tripReason = "run_cap"
		return a.wrappedCapErrLocked()
	}

	return nil
}

// StageTotal returns the accumulated cost for the given stage.
func (a *CostAccumulator) StageTotal(stage domain.Stage) float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stageTotals[stage]
}

// RunTotal returns the cumulative cost across all stages.
func (a *CostAccumulator) RunTotal() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runTotal
}

// Tripped reports whether a cap has been breached at any point.
func (a *CostAccumulator) Tripped() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tripped
}

// TripReason returns the stage and reason ("stage_cap" or "run_cap") of the
// first cap breach, or ("", "") if never tripped.
func (a *CostAccumulator) TripReason() (domain.Stage, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tripStage, a.tripReason
}

func (a *CostAccumulator) wrappedCapErrLocked() error {
	var actual, cap float64
	switch a.tripReason {
	case "stage_cap":
		actual = a.stageTotals[a.tripStage]
		cap = a.perStageCaps[a.tripStage]
	case "run_cap":
		actual = a.runTotal
		cap = a.perRunCap
	}
	return fmt.Errorf(
		"%w: stage=%s reason=%s actual=$%.4f cap=$%.4f",
		domain.ErrCostCapExceeded, a.tripStage, a.tripReason, actual, cap,
	)
}
