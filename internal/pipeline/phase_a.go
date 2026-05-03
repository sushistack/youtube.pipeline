package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sushistack/youtube.pipeline/internal/clock"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/pipeline/agents"
)

// runIDAllowedRE is the allowlist for RunID characters. RunID is used
// verbatim as a directory name under outputDir, so we restrict to an
// identifier-shaped subset — alphanumerics, underscore, dot, hyphen —
// which rejects control characters (NUL, newline, tab), whitespace,
// and any filesystem/shell metacharacters without having to enumerate
// them one by one. Path-separator and ".." checks live alongside this
// regex in validateRunID because their error messages are more
// specific for those common mistakes.
var runIDAllowedRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// PhaseARunner executes the eight Phase A agents sequentially over a
// PipelineState. One instance per run is fine — it holds no per-run
// state between Run() calls.
//
// Construction injects the eight agents in fixed positional order; the
// runner does NOT discover or reorder them. This is deliberate: the
// ordering is a domain invariant (research → structure → write →
// polish → post_writer_critic → visual → review → critic) and the
// compiler enforces completeness via the struct field names.
type PhaseARunner struct {
	researcher        agents.AgentFunc
	structurer        agents.AgentFunc
	writer            agents.AgentFunc
	polisher          agents.AgentFunc
	postWriterCritic  agents.AgentFunc
	visualBreakdowner agents.AgentFunc
	reviewer          agents.AgentFunc
	critic            agents.AgentFunc

	writerProvider string
	criticProvider string
	outputDir      string
	clock          clock.Clock
	logger         *slog.Logger
}

// NewPhaseARunner constructs a runner. All eight AgentFunc arguments are
// required and MUST be non-nil; passing a nil agent returns
// domain.ErrValidation. This is the fail-fast guard against
// "forgot to wire an agent" — the concrete Stories 3.2–3.5 each
// introduce their own agent and plug it in here; a missing wire is a
// compile/test error, not a runtime NPE.
//
// logger == nil falls back to slog.Default() (mirrors pipeline.NewEngine
// precedent). clk == nil returns domain.ErrValidation (no default —
// determinism is mandatory; tests inject FakeClock). outputDir == ""
// returns domain.ErrValidation. writerProvider == "" or
// criticProvider == "" returns domain.ErrValidation at construction
// time — Run() would otherwise catch the empty pair later via
// ValidateDistinctProviders, but failing fast here prevents a runner
// from ever being held by callers in an unrunnable state.
func NewPhaseARunner(
	researcher, structurer, writer, polisher, postWriterCritic, visualBreakdowner, reviewer, critic agents.AgentFunc,
	writerProvider, criticProvider string,
	outputDir string,
	clk clock.Clock,
	logger *slog.Logger,
) (*PhaseARunner, error) {
	switch {
	case researcher == nil:
		return nil, fmt.Errorf("new phase a runner: %w: researcher agent is nil", domain.ErrValidation)
	case structurer == nil:
		return nil, fmt.Errorf("new phase a runner: %w: structurer agent is nil", domain.ErrValidation)
	case writer == nil:
		return nil, fmt.Errorf("new phase a runner: %w: writer agent is nil", domain.ErrValidation)
	case polisher == nil:
		return nil, fmt.Errorf("new phase a runner: %w: polisher agent is nil", domain.ErrValidation)
	case postWriterCritic == nil:
		return nil, fmt.Errorf("new phase a runner: %w: post_writer_critic agent is nil", domain.ErrValidation)
	case visualBreakdowner == nil:
		return nil, fmt.Errorf("new phase a runner: %w: visual_breakdowner agent is nil", domain.ErrValidation)
	case reviewer == nil:
		return nil, fmt.Errorf("new phase a runner: %w: reviewer agent is nil", domain.ErrValidation)
	case critic == nil:
		return nil, fmt.Errorf("new phase a runner: %w: critic agent is nil", domain.ErrValidation)
	case clk == nil:
		return nil, fmt.Errorf("new phase a runner: %w: clock is nil", domain.ErrValidation)
	case outputDir == "":
		return nil, fmt.Errorf("new phase a runner: %w: outputDir is empty", domain.ErrValidation)
	case writerProvider == "":
		return nil, fmt.Errorf("new phase a runner: %w: writerProvider is empty", domain.ErrValidation)
	case criticProvider == "":
		return nil, fmt.Errorf("new phase a runner: %w: criticProvider is empty", domain.ErrValidation)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &PhaseARunner{
		researcher:        researcher,
		structurer:        structurer,
		writer:            writer,
		polisher:          polisher,
		postWriterCritic:  postWriterCritic,
		visualBreakdowner: visualBreakdowner,
		reviewer:          reviewer,
		critic:            critic,
		writerProvider:    writerProvider,
		criticProvider:    criticProvider,
		outputDir:         outputDir,
		clock:             clk,
		logger:            logger,
	}, nil
}

// Run executes the seven Phase A agents sequentially. Ordering is fixed:
// Researcher → Structurer → Writer → PostWriterCritic → VisualBreakdowner → Reviewer → Critic.
// On the first agent error, the chain aborts and the error is returned
// wrapped with the offending PipelineStage (AC-FAIL-FAST-WRAPPING).
//
// state MUST be non-nil. state.RunID and state.SCPID MUST be populated;
// missing either returns domain.ErrValidation. state.RunID MUST be a
// simple identifier with no path separators and no ".." component — it
// is used verbatim as a directory name under outputDir.
//
// State timestamps:
//   - state.StartedAt is stamped on entry (after input validation and
//     the up-front MkdirAll).
//   - state.FinishedAt is stamped ONLY on successful Run. If ANY step
//     after StartedAt fails (agent error, ctx cancel, writeScenario
//     error), FinishedAt is guaranteed to be "" — so callers may use
//     (state.FinishedAt != "") as a reliable "chain completed AND
//     scenario.json is on disk" predicate. The stamp happens before
//     writeScenario so the on-disk file also carries it; on write
//     failure the in-memory stamp is rolled back to "".
//
// On success, Run writes scenario.json to outputDir/runID/ atomically
// (AC-SCENARIO-JSON). On failure, no file is written (AC-FAIL-NO-ARTIFACT).
//
// Run is NOT goroutine-safe: one call per state at a time. Phase A
// runs on a single goroutine by design (parallelism is a Phase B concern).
//
// Order of operations (load-bearing):
//  1. Input validation: state non-nil, RunID/SCPID non-empty, RunID
//     contains no path separators or "..".
//  2. Context pre-check — abort before any agent runs if already canceled.
//  3. Stamp state.StartedAt = clock.Now().Format(time.RFC3339Nano).
//  4. Create the per-run directory up-front (AC-MKDIR-FAILURE) — fail fast
//     before any agent runs so a bad outputDir does not burn LLM cost.
//  5. For each agent in fixed order:
//     a. ctx.Err() — abort between agents (AC-CTX-CANCEL).
//     b. logger.Info("agent start", ...).
//     c. Call the agent; record elapsed wall-clock.
//     d. On error, log and return wrapped error (AC-FAIL-FAST-WRAPPING).
//  6. Post-chain ctx.Err() — guard against a final-stage agent that
//     silently ignored cancellation during its own execution.
//  7. Stamp state.FinishedAt so it appears inside scenario.json.
//  8. Write scenario.json atomically (AC-SCENARIO-JSON). On failure,
//     roll FinishedAt back to "" so callers see a consistent state.
//  9. Log "phase a complete".
func (r *PhaseARunner) Run(ctx context.Context, state *agents.PipelineState) error {
	if state == nil {
		return fmt.Errorf("phase a: %w: state is nil", domain.ErrValidation)
	}
	if state.RunID == "" {
		return fmt.Errorf("phase a: %w: state.RunID is empty", domain.ErrValidation)
	}
	if state.SCPID == "" {
		return fmt.Errorf("phase a: %w: state.SCPID is empty", domain.ErrValidation)
	}
	if err := validateRunID(state.RunID); err != nil {
		return fmt.Errorf("phase a: %w", err)
	}

	// Pre-chain cancellation check — aborts on the first stage (Researcher)
	// without even invoking it.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("phase a: stage=%s: %w", agents.StageResearcher.String(), err)
	}
	if err := ValidateDistinctProviders(r.writerProvider, r.criticProvider); err != nil {
		return fmt.Errorf("phase a: %w", err)
	}

	state.StartedAt = r.clock.Now().Format(time.RFC3339Nano)

	// Fail-fast: ensure the per-run output directory is writable BEFORE
	// any agent runs. A bad outputDir (e.g., points at a regular file,
	// read-only filesystem) must not trigger 6 LLM calls whose output is
	// then thrown away.
	runDir := filepath.Join(r.outputDir, state.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("phase a: create run dir %s: %w", runDir, err)
	}

	chain := []struct {
		ps    agents.PipelineStage
		agent agents.AgentFunc
	}{
		{agents.StageResearcher, r.researcher},
		{agents.StageStructurer, r.structurer},
		{agents.StageWriter, r.writer},
		{agents.StagePolisher, r.polisher},
		{agents.StagePostWriterCritic, r.postWriterCritic},
		{agents.StageVisualBreakdowner, r.visualBreakdowner},
		{agents.StageReviewer, r.reviewer},
		{agents.StageCritic, r.critic},
	}

	for _, entry := range chain {
		if err := ctx.Err(); err != nil {
			r.logger.Error("agent aborted by context",
				"run_id", state.RunID,
				"pipeline_stage", entry.ps.String(),
				"error", err.Error(),
			)
			return fmt.Errorf("phase a: stage=%s: %w", entry.ps.String(), err)
		}

		// Notify the engine that this sub-stage is starting so it can persist
		// run.stage to the DB and surface progress via the SSE status stream.
		// StageResearcher is skipped (engine already wrote research/running
		// before calling Run). StagePolisher and StagePostWriterCritic are
		// skipped because they are write-phase sub-steps; surfacing them would
		// cause the UI to jump away from "write" and back unexpectedly.
		if state.OnSubStageStart != nil &&
			entry.ps != agents.StageResearcher &&
			entry.ps != agents.StagePolisher &&
			entry.ps != agents.StagePostWriterCritic {
			if err := state.OnSubStageStart(ctx, entry.ps); err != nil {
				r.logger.Warn("substage progress update failed",
					"run_id", state.RunID, "pipeline_stage", entry.ps.String(), "error", err.Error())
				// Non-fatal: the run continues; the UI just misses this tick.
			}
		}

		// Deterministic agents: load from cache if available, skip invocation.
		if entry.ps == agents.StageResearcher || entry.ps == agents.StageStructurer {
			if r.tryLoadCache(entry.ps, runDir, state) {
				continue
			}
		}

		if err := r.runAgent(ctx, entry.ps, entry.agent, state); err != nil {
			return err
		}

		// Persist deterministic agent output for future retries.
		if entry.ps == agents.StageResearcher || entry.ps == agents.StageStructurer {
			r.writeCache(entry.ps, runDir, state)
		}

		// Short-circuit: if post_writer_critic rejects the narration, skip
		// visual_breakdowner/reviewer/post_reviewer_critic entirely. The
		// narration will be rewritten on the next attempt — running downstream
		// stages on a rejected script wastes 2+ minutes and produces artifacts
		// that are immediately discarded.
		if entry.ps == agents.StagePostWriterCritic &&
			state.Critic != nil &&
			state.Critic.PostWriter != nil &&
			state.Critic.PostWriter.Verdict == domain.CriticVerdictRetry {
			break
		}
	}

	// Post-chain cancellation check. If a cooperating agent (especially
	// the last one, critic) ignored ctx.Done() during its work, the loop
	// above would not have caught it. Abort before producing an artifact.
	if err := ctx.Err(); err != nil {
		r.logger.Error("agent aborted by context",
			"run_id", state.RunID,
			"pipeline_stage", agents.StageCritic.String(),
			"error", err.Error(),
		)
		return fmt.Errorf("phase a: stage=%s: %w", agents.StageCritic.String(), err)
	}

	if shouldFinalizePhaseA(state) {
		state.FinishedAt = r.clock.Now().Format(time.RFC3339Nano)
		wrote, err := finalizePhaseA(runDir, state)
		if err != nil {
			state.FinishedAt = ""
			return fmt.Errorf("phase a: %w", err)
		}
		if !wrote {
			state.FinishedAt = ""
		}
	}

	r.logger.Info("phase a complete",
		"run_id", state.RunID,
		"scp_id", state.SCPID,
		"started_at", state.StartedAt,
		"finished_at", state.FinishedAt,
	)
	return nil
}

// validateRunID rejects RunIDs that would escape outputDir when used as
// a directory name or would embed control/whitespace characters. A
// RunID is expected to be a simple identifier (e.g., "scp-049-run-1")
// — never a path. The specific path-separator / ".." / "." checks
// produce actionable error messages for the common-mistake cases; the
// runIDAllowedRE allowlist then catches everything else (NUL bytes,
// newlines, tabs, spaces, shell metacharacters) without us having to
// enumerate the disallowed set.
func validateRunID(runID string) error {
	if strings.ContainsAny(runID, `/\`) {
		return fmt.Errorf("%w: state.RunID %q contains a path separator", domain.ErrValidation, runID)
	}
	if strings.Contains(runID, "..") {
		return fmt.Errorf("%w: state.RunID %q contains %q", domain.ErrValidation, runID, "..")
	}
	// Reject "." and other weird-but-separator-free values that Clean
	// would collapse to an unexpected directory.
	if runID == "." {
		return fmt.Errorf("%w: state.RunID %q is not a valid directory name", domain.ErrValidation, runID)
	}
	if !runIDAllowedRE.MatchString(runID) {
		return fmt.Errorf("%w: state.RunID %q contains disallowed characters (allowed: A-Z a-z 0-9 _ . -)", domain.ErrValidation, runID)
	}
	return nil
}

// runAgent invokes a single agent, logging before/after and wrapping
// errors with the offending PipelineStage. Wrap preserves the original
// error chain so errors.Is / domain.Classify continue to work.
func (r *PhaseARunner) runAgent(ctx context.Context, ps agents.PipelineStage, agent agents.AgentFunc, state *agents.PipelineState) error {
	r.logger.Info("agent start",
		"run_id", state.RunID,
		"pipeline_stage", ps.String(),
	)

	start := r.clock.Now()
	err := agent(ctx, state)
	elapsed := r.clock.Now().Sub(start)

	if err != nil {
		r.logger.Error("agent failed",
			"run_id", state.RunID,
			"pipeline_stage", ps.String(),
			"duration_ms", elapsed.Milliseconds(),
			"error", err.Error(),
		)
		return fmt.Errorf("phase a: stage=%s: %w", ps.String(), err)
	}

	r.logger.Info("agent complete",
		"run_id", state.RunID,
		"pipeline_stage", ps.String(),
		"duration_ms", elapsed.Milliseconds(),
	)
	return nil
}

func shouldFinalizePhaseA(state *agents.PipelineState) bool {
	if state == nil ||
		state.Research == nil ||
		state.Structure == nil ||
		state.Narration == nil ||
		state.VisualBreakdown == nil ||
		state.Review == nil ||
		state.Critic == nil ||
		state.Critic.PostWriter == nil ||
		state.Critic.PostReviewer == nil {
		return false
	}
	return state.Critic.PostReviewer.Verdict == domain.CriticVerdictPass ||
		state.Critic.PostReviewer.Verdict == domain.CriticVerdictAcceptWithNotes
}

// tryLoadCache attempts to read a cached agent output from disk for deterministic
// agents (researcher, structurer). On success it populates the relevant state
// field and returns true so the caller can skip invoking the agent. Any read
// or unmarshal failure is treated as a cache miss (returns false) — the agent
// will run normally and overwrite the stale or corrupt file.
func (r *PhaseARunner) tryLoadCache(ps agents.PipelineStage, runDir string, state *agents.PipelineState) bool {
	var cacheFile string
	switch ps {
	case agents.StageResearcher:
		cacheFile = filepath.Join(runDir, "research_cache.json")
	case agents.StageStructurer:
		cacheFile = filepath.Join(runDir, "structure_cache.json")
	default:
		return false
	}

	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return false
	}

	switch ps {
	case agents.StageResearcher:
		var out *domain.ResearcherOutput
		if err := json.Unmarshal(data, &out); err != nil || out == nil {
			return false
		}
		if out.SCPID != state.SCPID {
			return false
		}
		if out.SourceVersion != domain.SourceVersionV1 {
			r.logger.Info("agent cache stale: source_version mismatch",
				"pipeline_stage", ps.String(), "run_id", state.RunID,
				"cached_version", out.SourceVersion, "current_version", domain.SourceVersionV1)
			return false
		}
		state.Research = out
	case agents.StageStructurer:
		var out *domain.StructurerOutput
		if err := json.Unmarshal(data, &out); err != nil || out == nil {
			return false
		}
		if out.SCPID != state.SCPID {
			return false
		}
		if out.SourceVersion != domain.SourceVersionV1 {
			r.logger.Info("agent cache stale: source_version mismatch",
				"pipeline_stage", ps.String(), "run_id", state.RunID,
				"cached_version", out.SourceVersion, "current_version", domain.SourceVersionV1)
			return false
		}
		state.Structure = out
	}

	r.logger.Info("agent cache hit", "pipeline_stage", ps.String(), "run_id", state.RunID)
	return true
}

// writeCache persists a deterministic agent's output to disk atomically
// (tmp file → rename). Errors are logged but not returned — a failed cache
// write is non-fatal; the pipeline result is not affected.
func (r *PhaseARunner) writeCache(ps agents.PipelineStage, runDir string, state *agents.PipelineState) {
	var (
		cacheFile string
		payload   any
	)
	switch ps {
	case agents.StageResearcher:
		if state.Research == nil {
			return
		}
		cacheFile = filepath.Join(runDir, "research_cache.json")
		payload = state.Research
	case agents.StageStructurer:
		if state.Structure == nil {
			return
		}
		cacheFile = filepath.Join(runDir, "structure_cache.json")
		payload = state.Structure
	default:
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		r.logger.Warn("agent cache marshal failed", "pipeline_stage", ps.String(), "run_id", state.RunID, "error", err.Error())
		return
	}

	tmp := cacheFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		r.logger.Warn("agent cache write failed", "pipeline_stage", ps.String(), "run_id", state.RunID, "error", err.Error())
		return
	}
	if err := os.Rename(tmp, cacheFile); err != nil {
		r.logger.Warn("agent cache rename failed", "pipeline_stage", ps.String(), "run_id", state.RunID, "error", err.Error())
		os.Remove(tmp)
		return
	}

	r.logger.Info("agent cache written", "pipeline_stage", ps.String(), "run_id", state.RunID)
}

// ScenarioPath returns the canonical path to scenario.json for the
// given outputDir and runID. Does not touch the filesystem.
func ScenarioPath(outputDir, runID string) string {
	return filepath.Join(outputDir, runID, "scenario.json")
}
