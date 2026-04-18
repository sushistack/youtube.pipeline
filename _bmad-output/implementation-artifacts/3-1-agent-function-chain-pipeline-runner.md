# Story 3.1: Agent Function Chain & Pipeline Runner

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer,
I want a Phase A chain runner that orchestrates the six agents as pure functions over a shared `PipelineState` and persists `scenario.json` on completion,
so that Stories 3.2–3.5 can plug their concrete agents into a typed scaffold without re-inventing orchestration, purity enforcement, or artifact persistence — and so the engine has a deterministic, testable entry point for Phase A execution.

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, and use inline fakes + `testutil.AssertEqual[T]` (no testify, no gomock). Module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

1. **AC-AGENTFUNC-TYPE:** `internal/pipeline/agents/agent.go` (NEW) declares the canonical agent signature exactly as architecture.md:685-688 specifies:

    ```go
    // AgentFunc is the Phase A agent contract. Each agent is a pure function
    // that reads input fields of state, writes its output fields of state,
    // and returns an error on failure. Purity rule — enforced by layer-lint
    // (AC-PURITY-LINT) and asserted in tests (AC-TESTS-PURITY):
    //   - NO database access (no internal/db import)
    //   - NO HTTP calls (LLM calls are injected via domain.TextGenerator)
    //   - NO filesystem side effects (state is in-memory; the runner owns
    //     scenario.json persistence)
    //   - NO goroutines that outlive the call
    //   - NO shared mutable state beyond the *PipelineState argument
    //
    // If an agent needs external capabilities (text generation, local corpus
    // reads), construct it via a factory that closes over the dependency
    // and returns an AgentFunc — do NOT add fields to PipelineState that
    // carry service-layer or db-layer types.
    type AgentFunc func(ctx context.Context, state *PipelineState) error
    ```

    The doc comment is the operator-facing contract for Stories 3.2–3.5 — it must survive code review unchanged. Add `var _ AgentFunc = noopAgent` at the bottom of `agent_test.go` as a compile-time check that the signature does not drift.

2. **AC-PIPELINESTATE-STRUCT:** `internal/pipeline/agents/agent.go` declares `PipelineState` as the in-memory inter-agent carrier:

    ```go
    // PipelineState is the in-memory data carrier passed between Phase A
    // agents. Each agent reads upstream fields and writes its own output
    // field. Fields are EXPLICITLY TYPED per agent — no map[string]any,
    // no generic "payload" bag — so schema drift is a compile error.
    //
    // Persistence: PipelineState lives in memory during Phase A execution
    // only. The runner serializes it to {outputDir}/{runID}/scenario.json
    // after the Critic agent returns successfully (AC-SCENARIO-JSON).
    // Never embed, never carry domain.TextGenerator, *sql.DB, *http.Client,
    // or any other service/infrastructure handle — those flow through agent
    // factories (AC-AGENTFUNC-TYPE).
    //
    // Story 3.1 defines the slots but not the concrete schemas for each
    // output — Stories 3.2–3.5 promote these fields to domain types
    // (ResearchSummary, ScenarioStructure, NarrationSet, ShotBreakdown,
    // ReviewReport, CriticVerdict). For 3.1 the fields are typed but
    // opaque `json.RawMessage` placeholders so the scaffold compiles and
    // round-trips JSON without freezing the schema prematurely.
    type PipelineState struct {
        // Input — populated by the runner from the Run row before the chain starts.
        RunID     string `json:"run_id"`
        SCPID     string `json:"scp_id"`

        // Agent outputs — populated left-to-right by the chain. A nil value
        // means "upstream agent has not run yet"; a non-nil value is the
        // agent's serialized output. Stories 3.2–3.5 replace json.RawMessage
        // with strongly-typed structs.
        Research       json.RawMessage `json:"research,omitempty"`         // Researcher (3.2)
        Structure      json.RawMessage `json:"structure,omitempty"`        // Structurer (3.2)
        Narration      json.RawMessage `json:"narration,omitempty"`        // Writer (3.3)
        VisualBreakdown json.RawMessage `json:"visual_breakdown,omitempty"` // VisualBreakdowner (3.4)
        Review         json.RawMessage `json:"review,omitempty"`           // Reviewer (3.4)
        Critic         json.RawMessage `json:"critic,omitempty"`           // Critic (3.3 post-Writer + 3.5 post-Reviewer)

        // Provenance — runner-populated bookkeeping for NFR-M2 (version-
        // controlled artifacts must record their own generator).
        StartedAt  string `json:"started_at"`  // RFC3339 from clock.Clock
        FinishedAt string `json:"finished_at"` // RFC3339; empty until chain completes
    }
    ```

    JSON tags are snake_case matching the project-wide rule. `omitempty` on optional slots lets partial states round-trip without polluting scenario.json with nulls. Add `TestPipelineState_JSONShape` asserting: (a) a zero-valued state marshals to exactly `{"run_id":"","scp_id":"","started_at":"","finished_at":""}`, (b) a fully-populated state round-trips (`Marshal → Unmarshal → Marshal` byte-identical after canonical-map ordering), (c) no field names use camelCase or snake_case variants that would break the contract.

3. **AC-NEW-STAGES-FOR-AGENTS:** `PipelineStage` (new) enumerates the six Phase A agent positions; it is an ordered index, distinct from `domain.Stage` (which is the run-level state machine position). Declared in `internal/pipeline/agents/agent.go`:

    ```go
    // PipelineStage is the ordinal position of a Phase A agent within the
    // chain. It is NOT persisted — it exists only so the runner and tests
    // can reference a specific agent slot by a typed constant instead of
    // an integer.
    type PipelineStage int

    const (
        StageResearcher PipelineStage = iota
        StageStructurer
        StageWriter
        StageVisualBreakdowner
        StageReviewer
        StageCritic
        phaseAStageCount // sentinel: number of agents in the Phase A chain
    )
    ```

    `phaseAStageCount` is unexported — its only caller is the runner's invariant check (AC-CHAIN-ORDER-INVARIANT). Add `String() string` on `PipelineStage` returning the canonical name (`"researcher"`, `"structurer"`, `"writer"`, `"visual_breakdowner"`, `"reviewer"`, `"critic"`) — used in logs and error messages. Add `TestPipelineStage_String` and `TestPipelineStage_Count` (`phaseAStageCount == 6`).

    **Why separate from `domain.Stage`:** `domain.Stage` has 15 values (15 run-level states); `PipelineStage` has 6 (the Phase A agent slots). Conflating them couples the runner's internal chain mechanics to the run-level state machine and complicates testing. The runner maps `PipelineStage → domain.Stage` via a small table (see AC-STAGE-MAPPING) when it emits stage observations, but the chain's own invariants speak in `PipelineStage`.

4. **AC-STAGE-MAPPING:** The runner maps each `PipelineStage` to its corresponding `domain.Stage` for observability/logging purposes only:

    | PipelineStage | domain.Stage |
    |---|---|
    | `StageResearcher` | `domain.StageResearch` |
    | `StageStructurer` | `domain.StageStructure` |
    | `StageWriter` | `domain.StageWrite` |
    | `StageVisualBreakdowner` | `domain.StageVisualBreak` |
    | `StageReviewer` | `domain.StageReview` |
    | `StageCritic` | `domain.StageCritic` |

    Exposed as `func (ps PipelineStage) DomainStage() domain.Stage`; unknown value panics (programmer error, unreachable). Add `TestPipelineStage_DomainStage` covering all 6 valid values + the panic path for out-of-range values.

5. **AC-PHASEARUNNER-STRUCT:** `internal/pipeline/phase_a.go` (NEW) declares the chain orchestrator:

    ```go
    // PhaseARunner executes the six Phase A agents sequentially over a
    // PipelineState. One instance per run is fine — it holds no per-run
    // state between Run() calls.
    //
    // Construction injects the six agents in fixed positional order; the
    // runner does NOT discover or reorder them. This is deliberate: the
    // ordering is a domain invariant (research → structure → write → visual
    // → review → critic) and the compiler enforces completeness via the
    // struct field names.
    type PhaseARunner struct {
        researcher        agents.AgentFunc
        structurer        agents.AgentFunc
        writer            agents.AgentFunc
        visualBreakdowner agents.AgentFunc
        reviewer          agents.AgentFunc
        critic            agents.AgentFunc

        outputDir string        // base output dir; per-run dir is outputDir/runID
        clock     clock.Clock   // for StartedAt/FinishedAt stamps
        logger    *slog.Logger  // stage-by-stage structured logging (NFR-O2)
    }

    // NewPhaseARunner constructs a runner. All six AgentFunc arguments are
    // required and MUST be non-nil; passing a nil agent returns
    // domain.ErrValidation. This is the fail-fast guard against
    // "forgot to wire an agent" — the concrete Stories 3.2–3.5 each
    // introduce their own agent and plug it in here; a missing wire is a
    // compile/test error, not a runtime NPE.
    func NewPhaseARunner(
        researcher, structurer, writer, visualBreakdowner, reviewer, critic agents.AgentFunc,
        outputDir string,
        clk clock.Clock,
        logger *slog.Logger,
    ) (*PhaseARunner, error)
    ```

    `logger == nil` → fall back to `slog.Default()` (mirrors `pipeline.NewRecorder` precedent). `clk == nil` → return `domain.ErrValidation` (no default — determinism is mandatory; tests inject FakeClock). `outputDir == ""` → return `domain.ErrValidation`.

6. **AC-CHAIN-RUN:** `PhaseARunner` exposes a single public method:

    ```go
    // Run executes the six Phase A agents sequentially. Ordering is fixed:
    // Researcher → Structurer → Writer → VisualBreakdowner → Reviewer → Critic.
    // On the first agent error, the chain aborts and the error is returned
    // wrapped with the offending PipelineStage (AC-FAIL-FAST-WRAPPING).
    //
    // state MUST be non-nil. state.RunID and state.SCPID MUST be populated;
    // missing either returns domain.ErrValidation. Run overwrites
    // state.StartedAt on entry and state.FinishedAt on successful exit
    // (both via clock.Clock.Now()). On failure, FinishedAt is left empty —
    // the caller can distinguish a successful chain from a failed one by
    // (FinishedAt != "") even if they only have state, not the returned error.
    //
    // On success, Run writes scenario.json to outputDir/runID/ atomically
    // (AC-SCENARIO-JSON). On failure, no file is written (AC-FAIL-NO-ARTIFACT).
    //
    // Run is NOT goroutine-safe: one call per state at a time. Phase A
    // runs on a single goroutine by design (parallelism is a Phase B
    // concern per architecture.md:695-738).
    func (r *PhaseARunner) Run(ctx context.Context, state *agents.PipelineState) error
    ```

    Order of operations inside Run is load-bearing — document it as a numbered list in the method's doc comment:
    1. Input validation: state non-nil, RunID/SCPID non-empty.
    2. Context pre-check: `ctx.Err()` — abort before any agent runs if already canceled.
    3. Stamp `state.StartedAt = clock.Now().Format(time.RFC3339Nano)`.
    4. For each agent in fixed order:
       a. `ctx.Err()` — abort between agents (AC-CTX-CANCEL).
       b. `logger.Info("agent start", "pipeline_stage", ps.String(), "run_id", runID)`.
       c. Call the agent. Record wall-clock elapsed for the stage observation (wired by Story 3.5; for 3.1 we just log it).
       d. On error, log `logger.Error("agent failed", ...)` and return wrapped error (AC-FAIL-FAST-WRAPPING).
    5. Stamp `state.FinishedAt = clock.Now().Format(time.RFC3339Nano)`.
    6. Write `scenario.json` atomically (AC-SCENARIO-JSON). Persistence failure returns error; FinishedAt is already set in state (that's fine — caller can log the partial state for debugging).
    7. Log `logger.Info("phase a complete", "run_id", runID, "duration_ms", ...)`.

7. **AC-CHAIN-ORDER-INVARIANT:** The runner MUST enforce the 6-agent sequence. A test `TestPhaseARunner_ExecutionOrder` wires six spy agents (each appends its PipelineStage to a shared slice) and asserts the slice is exactly `[StageResearcher, StageStructurer, StageWriter, StageVisualBreakdowner, StageReviewer, StageCritic]`. A second test `TestPhaseARunner_StageCountIs6` asserts `phaseAStageCount == 6` — a guard against a future refactor silently adding or removing an agent.

8. **AC-FAIL-FAST-WRAPPING:** When any agent returns an error, the chain stops and returns:

    ```go
    fmt.Errorf("phase a: stage=%s: %w", ps.String(), err)
    ```

    so `errors.Is(returnedErr, originalAgentErr)` still holds (preserves domain error classification via `errors.As`). No subsequent agents run — this is strict fail-fast. `TestPhaseARunner_StopsOnFirstError` wires an agent at position 3 (`StageWriter`) that returns a sentinel error; asserts positions 4–6 never execute (spy pattern), and asserts the returned error matches both the `"stage=write"` substring and `errors.Is(err, sentinel)`. Additionally, asserts the error is classified through `domain.Classify` correctly when the underlying error is a `*domain.DomainError` (e.g. `domain.ErrValidation` from a future agent).

9. **AC-FAIL-NO-ARTIFACT:** When any agent fails, the runner MUST NOT write `scenario.json`. `TestPhaseARunner_NoArtifactOnFailure` asserts that after a chain failure, `{outputDir}/{runID}/scenario.json` does NOT exist on disk (use `os.Stat` + `errors.Is(err, fs.ErrNotExist)`). This is a hard requirement so that a half-complete chain never leaves a misleading artifact that downstream consumers (Phase B, HITL) would pick up as authoritative. The per-run directory MAY exist (because it was created for the write attempt) or may not exist — the test does NOT require directory absence, only file absence.

10. **AC-CTX-CANCEL:** Context cancellation between agents aborts the chain promptly. `TestPhaseARunner_ContextCancelBetweenAgents` wires a cancel-on-stage-3 pattern: after the Structurer returns, the test cancels the context; the Writer MUST NOT run; the returned error MUST satisfy `errors.Is(err, context.Canceled)` AND contain the `"stage=write"` substring (the stage being *aborted*, not *completed*). A second test `TestPhaseARunner_ContextAlreadyCanceled` passes a pre-canceled ctx; no agent runs; the error is `context.Canceled`-classified with `"stage=researcher"` (the stage never entered). This is the NFR-O2/NFR-R4 obligation: cancellation is observable and attributable to a specific stage.

11. **AC-PURITY-LINT:** Extend `scripts/lintlayers/main.go` to enforce a stricter rule for the new `internal/pipeline/agents` subpackage than its parent `internal/pipeline`. The current `resolveTopLevelPackage` and `resolveTopLevelFromImport` helpers collapse nested paths to the first two segments (so `internal/pipeline/agents/agent.go` resolves to `internal/pipeline`). Two coordinated changes are required:

    1. **Introduce a nested-package allow list** — a small prefix map consulted BEFORE the generic two-segment collapse. When a path/import starts with a key in the map, the key itself is the resolved package.

        ```go
        // nestedTrackedPackages lists internal/ subpackages that have their
        // own layer-import rules distinct from their parent. Check these
        // with longest-match semantics BEFORE the generic two-segment collapse.
        var nestedTrackedPackages = []string{
            "internal/pipeline/agents",
        }
        ```

        `resolveTopLevelPackage` and `resolveTopLevelFromImport` both check this list first; if any entry is a prefix of the input path (followed by `/` or end-of-path), that entry is returned.

    2. **Add the rule** in `allowedImports`:

        ```go
        "internal/pipeline/agents": {"internal/domain", "internal/clock"},
        ```

        Agents may import `domain` (for interfaces like `TextGenerator` and types like `NormalizedResponse`) and `clock` (for deterministic time), but NOT `db`, NOT `llmclient`, NOT `os`/`net/http` (stdlib is ungated but filesystem/HTTP usage is covered by code review and the purity tests). Rationale comment in the script: "Agents are pure functions (architecture.md:1731-1734); LLM calls flow in via domain.TextGenerator closures, not direct llmclient imports."

    `scripts/lintlayers/main_test.go` gains three tests:
    - `TestResolveTopLevelPackage_NestedAgents` — asserts `internal/pipeline/agents/agent.go` → `internal/pipeline/agents`, while `internal/pipeline/engine.go` → `internal/pipeline` (regression guard).
    - `TestResolveTopLevelFromImport_NestedAgents` — asserts `internal/pipeline/agents` → `internal/pipeline/agents`.
    - `TestAllowedImports_Agents` — asserts `allowedImports["internal/pipeline/agents"]` is exactly `["internal/domain", "internal/clock"]` and that `internal/pipeline/agents` imports of `internal/db` or `internal/llmclient` are violations.

    As a negative-case fixture, construct a throwaway Go file (via `t.TempDir()` + `os.WriteFile`) in a simulated `internal/pipeline/agents/` path that imports `internal/llmclient`; run `checkImports` against the temp root; assert exactly one violation. This proves the rule bites in practice, not only in the static allow-list table.

12. **AC-PURITY-RUNTIME-CHECK:** The runner itself does NOT attempt runtime introspection of agent purity — that is neither feasible nor desirable in Go. Instead, `TestAgentFunc_SignatureStable` is a compile-time assertion: `var _ agents.AgentFunc = func(ctx context.Context, state *agents.PipelineState) error { return nil }` in `internal/pipeline/agents/agent_test.go`. If the signature ever changes, all 6 concrete agents in Stories 3.2–3.5 fail to compile — the right failure mode.

13. **AC-SCENARIO-JSON:** On successful chain completion, the runner writes the marshaled PipelineState to `{outputDir}/{runID}/scenario.json`:

    - **Atomic write:** `os.CreateTemp(runDir, "scenario-*.json")` → `json.MarshalIndent(state, "", "  ")` → write → `f.Sync()` → `f.Close()` → `os.Rename(tmpPath, finalPath)`. The rename is atomic on POSIX; a crash mid-write never produces a partial scenario.json.
    - **Directory creation:** `os.MkdirAll(runDir, 0o755)` before the temp file. The caller (the engine) guarantees `outputDir` exists (per Story 1.5 init); the per-run `runID` subdirectory may not yet exist on a fresh run.
    - **File permissions:** `0o644` — world-readable, owner-writable. scenario.json is an operator-inspectable artifact, not a secret.
    - **Indentation:** 2-space indented JSON (`MarshalIndent(state, "", "  ")`) so operators can `cat scenario.json` and read it directly in V1. Byte size is tolerable (<50 KB for a typical 10-scene scenario).

    `TestPhaseARunner_WritesScenarioJSON` asserts: file exists at the expected path; file is valid JSON (`json.Unmarshal` succeeds); unmarshaled state has all 6 agent output fields populated (spy agents inject non-empty `json.RawMessage` values); `started_at < finished_at` in RFC3339 ordering. A second test `TestPhaseARunner_AtomicWrite` asserts no temp file remains in the run dir after a successful write (the rename cleared it). A third test `TestPhaseARunner_IdempotentOverwrite` invokes `Run` twice in a row (same state, same dir) and asserts the second call overwrites rather than appending or failing (this supports Story 2.3's resume-from-Phase-A semantics).

14. **AC-MKDIR-FAILURE:** If `os.MkdirAll(runDir, 0o755)` fails (e.g. disk full, permission denied), `Run` returns the wrapped error and does NOT run any agents. Rationale: we already stamped `state.StartedAt`, but the chain hasn't produced anything yet — re-running is safe. Test `TestPhaseARunner_MkdirFailure_ReturnsError` injects an `outputDir` that is actually a regular file (not a directory) so MkdirAll returns an error; asserts `Run` returns the error wrapped with `"create run dir"` substring, and no agents ran (spy pattern).

15. **AC-NOOP-AGENT-HELPER:** `internal/pipeline/agents/noop.go` (NEW) exports a `NoopAgent` helper for Stories 3.2–3.5 and tests:

    ```go
    // NoopAgent returns an AgentFunc that succeeds without touching state.
    // Useful as a placeholder while wiring a partial chain during incremental
    // development (Stories 3.2–3.5) and as a spy stand-in in tests.
    func NoopAgent() AgentFunc {
        return func(ctx context.Context, state *PipelineState) error { return nil }
    }
    ```

    `TestNoopAgent` asserts it returns nil on any state (including nil — but that's a PipelineState-level choice, not a requirement; prefer documenting that NoopAgent does not guard its input because the runner already does).

16. **AC-ENGINE-ADVANCE-UNCHANGED:** `pipeline.Engine.Advance(ctx, runID)` remains the stub introduced in Story 2.3 (`return fmt.Errorf("advance not implemented: epic 3 scope")`). Story 3.1 does NOT wire the `PhaseARunner` into the engine — that wiring lands in Story 3.5 (`Phase A Completion & Post-Reviewer Critic`) once all six agents exist and the full chain can be exercised end-to-end. Rationale: integrating an incomplete chain into the engine would either (a) make `pipeline create → advance` silently no-op on stages past Researcher, surfacing a misleading "works" signal, or (b) require feature-flagging that cost is deferred twice. Instead, Story 3.1 is exercisable only via direct `NewPhaseARunner(...).Run(...)` in tests — the engine integration waits until the chain is real. Add `TestEngine_AdvanceStillStub` to `engine_test.go` asserting the stub message is unchanged (guards against accidental silent wiring).

17. **AC-RUNNER-INTERFACE-PRESERVED:** `internal/pipeline/runner.go` still declares `Runner interface { Advance, Resume }` — Story 3.1 does NOT modify it. The `PhaseARunner` struct is a separate concrete type (different concern: Phase A chain orchestration, not run-level state machine), intentionally named distinctly to prevent confusion. Lint: `grep -n "type Runner interface" internal/pipeline/runner.go` must still match; `grep -n "type PhaseARunner struct" internal/pipeline/phase_a.go` must match. A tiny regression test `TestRunnerInterface_Signature` asserts `Runner` still has exactly `Advance(context.Context, string) error` and `Resume(context.Context, string) error`.

18. **AC-SCENARIO-JSON-PATH-ON-RUN:** After `PhaseARunner.Run` succeeds, Story 3.5 will update `runs.scenario_path`. Story 3.1 does NOT write to the DB — it only writes the file. However, the canonical filesystem path (computed the same way both here and by Story 3.5) MUST be exposed as a pure helper so the engine and tests agree on where to look:

    ```go
    // ScenarioPath returns the canonical path to scenario.json for the
    // given outputDir and runID. Does not touch the filesystem.
    func ScenarioPath(outputDir, runID string) string {
        return filepath.Join(outputDir, runID, "scenario.json")
    }
    ```

    Exported from `internal/pipeline/phase_a.go`. `TestScenarioPath` table-drives across several (outputDir, runID) pairs including empty strings (documents the behavior — empty inputs produce "scenario.json" at the dir root, which is a caller-contract violation but not the helper's problem). Story 2.3's consistency checker (`CheckConsistency`) will adopt this helper in a follow-up — noted in `deferred-work.md` so the duplicate literal in `consistency.go` is tracked.

19. **AC-TESTS-INTEGRATION:** `internal/pipeline/phase_a_integration_test.go` (NEW) wires a real `*slog.Logger` (capturing via `testutil.CaptureLog`), real `clock.FakeClock`, six spy agents (each writes a distinct JSON payload into its PipelineState field), and a temp-dir `outputDir`. Asserts end-to-end:

    - Chain completes without error.
    - All 6 agents ran exactly once in order (spy slice matches the expected ordering).
    - `scenario.json` exists at `ScenarioPath(outputDir, runID)`.
    - Unmarshaled scenario.json has all 6 output fields non-nil and matches each spy's payload byte-for-byte.
    - `state.StartedAt` and `state.FinishedAt` are parseable RFC3339Nano and `FinishedAt >= StartedAt` (FakeClock advances on each `Now()` call the test drives).
    - Captured logs contain one "agent start" line per stage, six total, in order.
    - `testutil.BlockExternalHTTP(t)` is called — agents MUST NOT trigger any HTTP (even though the spies are pure Go, this is the paranoid habit from Story 2.1+).

20. **AC-TESTS-UNIT:** `internal/pipeline/phase_a_test.go` (NEW) covers:

    - `TestNewPhaseARunner_NilAgent_ReturnsValidation` — each of the six agent parameters being nil returns `domain.ErrValidation` (six parameterized cases).
    - `TestNewPhaseARunner_NilClock_ReturnsValidation` — clock==nil returns `domain.ErrValidation`.
    - `TestNewPhaseARunner_EmptyOutputDir_ReturnsValidation`.
    - `TestNewPhaseARunner_NilLogger_DefaultsToSlogDefault` — logger==nil → `slog.Default()` used; assert no panic and that `r.logger != nil`.
    - `TestPhaseARunner_Run_NilState_ReturnsValidation`.
    - `TestPhaseARunner_Run_EmptyRunID_ReturnsValidation`.
    - `TestPhaseARunner_Run_EmptySCPID_ReturnsValidation`.
    - `TestPhaseARunner_ExecutionOrder` (AC-CHAIN-ORDER-INVARIANT).
    - `TestPhaseARunner_StopsOnFirstError` (AC-FAIL-FAST-WRAPPING).
    - `TestPhaseARunner_NoArtifactOnFailure` (AC-FAIL-NO-ARTIFACT).
    - `TestPhaseARunner_ContextCancelBetweenAgents` + `TestPhaseARunner_ContextAlreadyCanceled` (AC-CTX-CANCEL).
    - `TestPhaseARunner_WritesScenarioJSON` + `TestPhaseARunner_AtomicWrite` + `TestPhaseARunner_IdempotentOverwrite` (AC-SCENARIO-JSON).
    - `TestPhaseARunner_MkdirFailure_ReturnsError` (AC-MKDIR-FAILURE).

21. **AC-DOC-PACKAGE-COMMENT:** `internal/pipeline/agents/doc.go` (NEW) carries the package-level doc comment:

    ```go
    // Package agents declares the AgentFunc contract and PipelineState
    // carrier for the Phase A 6-agent chain (Researcher → Structurer →
    // Writer → VisualBreakdowner → Reviewer → Critic).
    //
    // Agents are pure functions (no DB, no HTTP, no filesystem). External
    // capabilities (LLM text generation, local corpus reads) are injected
    // via domain.TextGenerator closures provided through agent factory
    // functions. The PhaseARunner in the parent pipeline/ package owns
    // orchestration and scenario.json persistence.
    //
    // Stories 3.2–3.5 each introduce one or two concrete agents; Story 3.1
    // ships this scaffold.
    package agents
    ```

    `internal/pipeline/doc.go` (update in place) gains one sentence: "`phase_a.go` implements the sequential Phase A chain; agents live in the `agents` subpackage per the purity rule."

22. **AC-LAYER-LINT-PASS:** `go run scripts/lintlayers/main.go` succeeds. Two new top-level packages are introduced: `internal/pipeline/agents`. The lint script already walks top-level `internal/` packages; Story 3.1 adds one entry to the `allowedImports` map (AC-PURITY-LINT). No other rules change. `scripts/lintlayers/main_test.go` gains a test `TestAllowedImports_Agents` asserting exactly `["internal/domain", "internal/clock"]` allowed and everything else is rejected.

23. **AC-NO-REGRESSIONS:** `go test ./... && go build ./... && go run scripts/lintlayers/main.go && make test-go` all pass. All Stories 1.x / 2.x tests unchanged. `scripts/frcoverage/main.go` (if it parses epics for FR coverage) still passes — Story 3.1 does not claim any FR (the FRs for Phase A, FR9–FR13 + FR48, are claimed by 3.2–3.5; see "FR Coverage" in Dev Notes).

---

## Tasks / Subtasks

- [ ] **T1: `internal/pipeline/agents/agent.go` — AgentFunc + PipelineState + PipelineStage** (AC: #1, #2, #3, #4, #21)
  - [ ] Create new subpackage directory.
  - [ ] Declare `AgentFunc` type with the exact doc comment from AC-AGENTFUNC-TYPE.
  - [ ] Declare `PipelineState` struct with the 10 fields + snake_case JSON tags.
  - [ ] Declare `PipelineStage` type + 6 ordered constants + `phaseAStageCount` sentinel.
  - [ ] Implement `(PipelineStage) String() string` for the 6 canonical names.
  - [ ] Implement `(PipelineStage) DomainStage() domain.Stage` for the 6-way mapping + panic on out-of-range.
  - [ ] Create `internal/pipeline/agents/doc.go` with the package doc comment.

- [ ] **T2: `internal/pipeline/agents/noop.go` — NoopAgent helper** (AC: #15)
  - [ ] Export `func NoopAgent() AgentFunc`.
  - [ ] Add godoc explaining intended use (test spies, incremental wiring during 3.2–3.5).

- [ ] **T3: `internal/pipeline/agents/agent_test.go` + `noop_test.go`** (AC: #1, #2, #3, #4, #12, #15, #20)
  - [ ] `TestAgentFunc_SignatureStable` — compile-time interface satisfaction.
  - [ ] `TestPipelineState_JSONShape` — zero state marshals to expected bytes; full-state round-trip; snake_case tags.
  - [ ] `TestPipelineStage_String` — table-driven across all 6 values.
  - [ ] `TestPipelineStage_Count` — asserts `phaseAStageCount == 6`.
  - [ ] `TestPipelineStage_DomainStage` — table-driven across all 6 values + panic on out-of-range.
  - [ ] `TestNoopAgent` — returns nil on any state.
  - [ ] All tests call `testutil.BlockExternalHTTP(t)`.

- [ ] **T4: `internal/pipeline/phase_a.go` — PhaseARunner struct + constructor** (AC: #5, #6, #17)
  - [ ] Import `context`, `encoding/json`, `errors`, `fmt`, `log/slog`, `os`, `path/filepath`, `time`, `domain`, `clock`, `agents`.
  - [ ] Declare `PhaseARunner` struct with six AgentFunc fields named per `PipelineStage`.
  - [ ] Implement `NewPhaseARunner(...)` with nil-agent guards (six parameterized checks) + nil-clock + empty-outputDir guards returning `domain.ErrValidation`.
  - [ ] Implement `Run(ctx, state)` with the 7-step procedure from AC-CHAIN-RUN, calling a private `r.runAgent(ctx, ps, agent, state)` helper that handles logging + wrapping.
  - [ ] Implement `ScenarioPath(outputDir, runID string) string` helper (AC-SCENARIO-JSON-PATH-ON-RUN).
  - [ ] Implement a private `writeScenario(runDir string, state *agents.PipelineState) error` helper with the atomic-write procedure from AC-SCENARIO-JSON.

- [ ] **T5: `internal/pipeline/phase_a_test.go` — unit tests** (AC: #5, #6, #7, #8, #9, #10, #13, #14, #20)
  - [ ] All tests: `testutil.BlockExternalHTTP(t)` at top; `t.TempDir()` for outputDir; `clock.NewFakeClock(fixedTime)` for determinism; spy agents implemented inline via closure that records its PipelineStage into a shared slice.
  - [ ] Constructor validation cases (6+3=9 cases).
  - [ ] Execution order test (AC-CHAIN-ORDER-INVARIANT).
  - [ ] Fail-fast test with sentinel error at position 3 (AC-FAIL-FAST-WRAPPING).
  - [ ] No-artifact-on-failure test (AC-FAIL-NO-ARTIFACT).
  - [ ] Context-canceled-between-agents + context-already-canceled (AC-CTX-CANCEL).
  - [ ] scenario.json write tests: exists + valid JSON + atomicity (no temp file remains) + idempotent overwrite.
  - [ ] `TestPhaseARunner_MkdirFailure_ReturnsError` — outputDir is a regular file.

- [ ] **T6: `internal/pipeline/phase_a_integration_test.go` — end-to-end** (AC: #19)
  - [ ] `testutil.BlockExternalHTTP(t)` + `testutil.CaptureLog(t)` for logger assertions.
  - [ ] Six spy agents each inject a distinct `json.RawMessage` payload.
  - [ ] Assert chain completes, scenario.json exists and unmarshals to a state with all 6 fields populated.
  - [ ] Assert `started_at <= finished_at` (RFC3339Nano, parsed via `time.Parse`).
  - [ ] Assert captured logs contain exactly 6 "agent start" entries in stage order.

- [ ] **T7: Layer-lint integration** (AC: #11, #22)
  - [ ] Edit `scripts/lintlayers/main.go`:
    - Add `var nestedTrackedPackages = []string{"internal/pipeline/agents"}`.
    - Update `resolveTopLevelPackage` and `resolveTopLevelFromImport` to check `nestedTrackedPackages` (longest-prefix match, require `/` or EOS boundary) BEFORE the generic two-segment collapse.
    - Add `"internal/pipeline/agents": {"internal/domain", "internal/clock"}` to `allowedImports`.
  - [ ] Edit `scripts/lintlayers/main_test.go` — add three tests: `TestResolveTopLevelPackage_NestedAgents`, `TestResolveTopLevelFromImport_NestedAgents`, `TestAllowedImports_Agents` (including a temp-dir negative-case fixture proving the rule bites).
  - [ ] Update any total-package-count assertions if present.
  - [ ] Run `go run scripts/lintlayers/main.go` locally and confirm `layer-import lint: OK`.

- [ ] **T8: Engine advance stub guard** (AC: #16)
  - [ ] In `internal/pipeline/engine_test.go`, add `TestEngine_AdvanceStillStub` that constructs an Engine with inline fake stores and asserts `Advance` returns an error with `"advance not implemented: epic 3 scope"` as its message.

- [ ] **T9: Runner interface preservation guard** (AC: #17)
  - [ ] In `internal/pipeline/runner_test.go` (NEW if missing), add `TestRunnerInterface_Signature` using reflection or `var _ Runner = ...` to assert both methods on a minimal inline type.
  - [ ] If `runner_test.go` exists, add the test there.

- [ ] **T10: Documentation update** (AC: #21)
  - [ ] Update `internal/pipeline/doc.go` with the one-sentence pointer to `phase_a.go` and the `agents` subpackage.
  - [ ] Append a row to `_bmad-output/implementation-artifacts/deferred-work.md`: "ConsistencyChecker should reuse `pipeline.ScenarioPath` — currently duplicates the `filepath.Join(runDir, 'scenario.json')` literal. Follow-up after 3.5 integration."

- [ ] **T11: Sprint status update & final validation**
  - [ ] Run `go test ./... -race -count=1 -timeout=120s` — zero failures.
  - [ ] Run `go build ./...` — clean.
  - [ ] Run `go run scripts/lintlayers/main.go` — `layer-import lint: OK`.
  - [ ] Flip `3-1-agent-function-chain-pipeline-runner: backlog` → `in-progress` via `/bmad-dev-story`; then `→ review` after implementation; then `→ done` via `/bmad-code-review`.

---

## Dev Notes

### The Core Design Decision: Typed 6-Field Struct vs `[]AgentFunc`

The natural Go idiom is `[]AgentFunc` iterated in order. We explicitly reject that. The `PhaseARunner` struct has six named fields — one per agent — because:

1. **Compiler enforces completeness.** A future refactor that forgets to wire an agent fails to compile (`NewPhaseARunner` has 6 required AgentFunc args). With `[]AgentFunc`, a 5-element slice is a runtime bug discoverable only by testing.
2. **Order is a domain invariant, not configuration.** The sequence Researcher → Structurer → Writer → VisualBreakdowner → Reviewer → Critic is not something the caller should be able to reorder via a slice. Named fields make the ordering a property of the type system, not the data.
3. **IDE/reviewer ergonomics.** `r.writer` is clearer than `chain[2]`. Code review catches "you swapped writer and structurer" immediately.

Trade-off acknowledged: adding a 7th agent in V1.5 requires touching the struct, the constructor signature, and the `Run` method. That is the RIGHT trade-off — adding an agent is a significant architectural event and the compiler should force every call site and test to acknowledge it.

### The Second Decision: `PipelineStage` enum vs Reusing `domain.Stage`

`domain.Stage` has 15 values covering the full run lifecycle (pending, research, structure, write, visual_break, review, critic, scenario_review, character_pick, image, tts, batch_review, assemble, metadata_ack, complete). The Phase A chain cares about exactly 6 of those. Reasons for a separate `PipelineStage`:

- **Decoupling.** The chain's internal bookkeeping (execution order, stage count invariants) should not be coupled to the 15-state run-level machine. If V1.5 adds a `pre_research` warmup stage to the chain, `domain.Stage` is NOT where that goes — it's a sub-structure of the existing `research` stage.
- **Test invariants.** `phaseAStageCount == 6` is a simple boolean assertion; `len(filter(domain.AllStages, isPhaseA)) == 6` is a dynamic filter over 15 values with a predicate the test also has to maintain.
- **Mapping is the narrow interface.** `DomainStage()` is the only bridge; changes on either side require crossing it intentionally, not by omission.

### PipelineState: Why `json.RawMessage` Placeholders

Story 3.1 must ship a compilable, testable scaffold WITHOUT pre-freezing the schemas for Research / Structure / Narration / VisualBreakdown / Review / Critic outputs. Those domain types evolve across Stories 3.2–3.5 (and frankly the Critic rubric shape will change as the calibration data comes in during Epic 4).

Using `json.RawMessage`:
- Allows spy agents in tests to inject any valid JSON without importing a placeholder type.
- Serializes cleanly to scenario.json: the field is the raw bytes, already JSON.
- Stories 3.2–3.5 replace each field with a typed struct (`ResearchSummary`, `ScenarioStructure`, `NarrationSet`, `ShotBreakdown`, `ReviewReport`, `CriticVerdict`). Every such story touches this struct — by design — and the schema contract becomes load-bearing in `testdata/contracts/phase_a_state.json`.

The alternative (defining all six types now with "TBD" fields) is worse: it introduces types that nobody consumes in 3.1, locks in schema decisions before the agents exist, and forces Stories 3.2–3.5 to either accept bad types or rewrite them.

### Fail-Fast Semantics vs Retry Loops

Story 3.1 is STRICT fail-fast: first agent error aborts the chain. This is NOT the place for retry logic — that belongs in:
- **Individual agents** (via `llmclient.WithRetry` for 429/503 — Story 2.4's retry classifier is already in place).
- **Critic-driven retries** (Story 3.3: post-Writer Critic verdict="retry" re-invokes the Writer; anti-progress detector from Story 2.5 guards the retry count).

The runner does one pass. If the Writer returns `ErrRateLimited` after exhausting retries, the runner reports the failure up to the engine, which marks the run as `status=failed, stage=write`. Operator resumes via Story 2.3's resume flow (which re-invokes the chain; partial state is in-memory so Phase A fully re-executes — consistent with architecture.md:786-788 "in-memory `PipelineState`").

### Why No Engine.Advance Wiring in 3.1

Listed as AC-ENGINE-ADVANCE-UNCHANGED but worth calling out why:

The engine's `Advance` method needs to drive the full Phase A chain, then transition the run's stage to `scenario_review` (the first HITL wait point), then return. If we wire `Advance` to call `PhaseARunner.Run` now with 5 NoopAgents, then `pipeline create <scp-id> && pipeline advance <run-id>` silently "succeeds" — the chain completes in microseconds, scenario.json contains stubs, and the run advances to `scenario_review`. An operator or automated test could interpret that as "Phase A works" when it doesn't.

Story 3.5 wires `Advance` after all six concrete agents exist. The guard test (T8) ensures Story 3.1's scaffold doesn't accidentally flip the stub off.

### Atomic Write: Why MarshalIndent and Why Rename

- **Atomic rename** is a POSIX guarantee for files on the same filesystem. If the write process crashes after `Sync` but before `Rename`, the temp file is orphaned but `scenario.json` either doesn't exist (never written before) or is still the previous-run copy (if this is a resume). Orphans are collected by a future cleanup pass or by the operator.
- **MarshalIndent with 2-space indentation** is tuned for human readability. The file is an operator-facing artifact — `cat scenario.json | jq '.critic'` must work. V1.5 may introduce a compact variant for network transport; V1 ships only the indented one. Size overhead of indentation is <20% and irrelevant for files <1 MB.
- **f.Sync** before close flushes OS buffers — important on Linux where the default is lazy write-back. Without Sync, a power-loss crash could leave `scenario.json` present-but-empty. With Sync, the file is either absent or fully written.

### Logger Discipline (NFR-O2)

Every slog entry uses the structured-key convention already established in 2.4's Recorder:
- `run_id` (string, always)
- `pipeline_stage` (string from `PipelineStage.String()`) — NOT `stage` (which refers to `domain.Stage` in other code paths)
- `duration_ms` for the agent call (integer)
- Agent failures: add `error` (string via err.Error())

Do NOT log agent inputs or outputs verbatim — they may contain the SCP narration which is user-facing content. Log structure (was it empty, what was its size), not content. Parallel precedent: `Recorder.Record` logs `cost_usd` and `token_in/out` but never the prompt or response text.

### Test Discipline — Spies Not Mocks

Testing an orchestrator is where "spy agents" shine: inline closures that record their invocation and payload, then return a canned result. No mocking library, no testify, no stubs across test files. Pattern:

```go
var order []agents.PipelineStage
spy := func(ps agents.PipelineStage, out json.RawMessage) agents.AgentFunc {
    return func(ctx context.Context, state *agents.PipelineState) error {
        order = append(order, ps)
        switch ps {
        case agents.StageResearcher: state.Research = out
        // ...
        }
        return nil
    }
}
```

The runner's behavior is entirely deterministic given spy inputs — no sleep, no randomness, no external state. `FakeClock` advances time on each call so StartedAt/FinishedAt are distinct.

### Rollout Risk & Mitigation

- **Risk:** Story 3.2 adds the Researcher and inadvertently takes over scenario.json persistence (agent writes file). **Mitigation:** AC-PURITY-LINT structurally forbids `os` imports in agents/; a Researcher that writes files fails layer-lint.
- **Risk:** Story 3.5 wires the engine and duplicates the "write scenario.json then advance stage" logic elsewhere. **Mitigation:** AC-SCENARIO-JSON-PATH-ON-RUN exports `ScenarioPath` as the single source of truth for the file location.
- **Risk:** Future agent orders accidentally drift (e.g. Reviewer before VisualBreakdowner). **Mitigation:** `TestPhaseARunner_ExecutionOrder` is a red-hot assertion; re-ordering the struct fields (where the constructor enforces order positionally) forces the test's expected slice to update.

### Previous Story Learnings Applied

From 2.1:
- Pure-function preference (the AgentFunc contract is a pure function; NextStage is our precedent).
- Stage/Event enums stable; `Stage.IsValid()` is the model for `PipelineStage.DomainStage()`.

From 2.2:
- snake_case JSON tags everywhere (PipelineState fields).
- `domain.Classify` is the error classifier — do not re-invent; let agents return classified errors and propagate them.

From 2.3:
- Local interface declarations in `pipeline/` are preferred over importing `service/` (we apply this pattern to `PhaseARunner` dependencies: logger, clock, no store).
- `t.TempDir()` for filesystem tests; `testutil.BlockExternalHTTP(t)` always.
- Atomic-write pattern (temp file + rename) — we adapt 2.3's fixture-write pattern here.

From 2.4:
- Recorder's approach to "nil logger → slog.Default()" is reused.
- `FakeClock` for deterministic timestamps — mandatory.
- Concurrency posture: Phase A is single-goroutine; mutex discipline is unnecessary. Contrast with 2.4's CostAccumulator mutex (Phase B shared across tracks).

From 2.5:
- Anti-progress detector ownership is deferred to Critic's retry loop in Story 3.3; NOT in the chain runner. We do NOT construct an `AntiProgressDetector` in Phase A runner state.

From 2.6:
- HITL sessions live on the state machine side (engine), not in PipelineState. When the run reaches `scenario_review`, engine creates the hitl_sessions row; scenario.json contains the Phase A output consumed by that HITL stage.

From 2.7:
- Observability is a stage-level concern wired via `Recorder`. Story 3.1 does NOT instrument per-agent cost/tokens — that comes when agents are real (Stories 3.2–3.5). Logging (as structured slog) is still done in 3.1 so the scaffold is observable.

### Deferred Work Awareness (Do Not Resolve Here)

- **Typed output structs for PipelineState:** `json.RawMessage` is V1's placeholder; Stories 3.2–3.5 replace each. Do not preemptively define them.
- **Inter-agent schema validation:** FR11 mandates "system validates each agent's output against schema." That validator lives in Story 3.2+ (where agents generate real content). Story 3.1 does NOT call a validator between agents.
- **Forbidden-term regex:** FR48 concerns the Writer agent; validator lives in Story 3.3.
- **Writer ≠ Critic runtime enforcement:** FR12 — enforced in Story 3.3 at the moment the Writer agent factory is constructed, not in 3.1.
- **`ConsistencyChecker` duplication of scenario.json path literal:** `internal/pipeline/consistency.go` currently contains `filepath.Join(runDir, "scenario.json")` inline. Story 3.1 exports `ScenarioPath` but does NOT refactor consistency.go to use it (scope creep). Logged as deferred work (T10).
- **`BlockExternalHTTP` global mutation:** known limitation from 1.4; do not refactor here.

### Deferred Work This Story May Generate

- **Engine.Advance wiring:** Story 3.5 picks up.
- **PipelineState typed fields:** Stories 3.2–3.5 each promote one slot from `json.RawMessage` to a domain type.
- **Per-agent retry observability:** When agents are real, each agent call should record via `pipeline.Recorder` (from 2.4). Story 3.1 does not yet wire a Recorder into PhaseARunner — the integration is cleaner once agents have real cost/token data.
- **scenario.json schema contract:** `testdata/contracts/phase_a_state.json` — introduced by Story 3.5 once the state is fully typed.
- **`PipelineStage.DomainStage` inverse (domain.Stage → PipelineStage):** needed by Story 3.5 for Recorder wiring. Not needed in 3.1.

### FR Coverage Claimed

Story 3.1 claims NO FRs. FR9/10/11/13/24/25/48 belong to Stories 3.2–3.5 which contain the logic that satisfies those requirements. Story 3.1 is pure infrastructure — a scaffold that makes those subsequent stories possible.

`testdata/fr-coverage.json` is NOT edited by this story. The coverage entry for FR9–FR13 currently points at Epic 3 in aggregate; it remains unchanged.

### Project Structure After This Story

```
internal/
  pipeline/
    agents/                             # NEW
      agent.go                          # NEW — AgentFunc, PipelineState, PipelineStage
      agent_test.go                     # NEW
      noop.go                           # NEW — NoopAgent helper
      noop_test.go                      # NEW
      doc.go                            # NEW
    phase_a.go                          # NEW — PhaseARunner, ScenarioPath
    phase_a_test.go                     # NEW
    phase_a_integration_test.go         # NEW
    engine.go                           # unchanged (Advance stub preserved)
    engine_test.go                      # TestEngine_AdvanceStillStub added
    runner.go                           # unchanged (Runner interface preserved)
    runner_test.go                      # TestRunnerInterface_Signature added (new file if absent)
    doc.go                              # one-sentence update
scripts/
  lintlayers/
    main.go                             # new entry in allowedImports
    main_test.go                        # TestAllowedImports_Agents added
_bmad-output/implementation-artifacts/
  deferred-work.md                      # ConsistencyChecker duplicate-literal row added
  sprint-status.yaml                    # 3-1-... transitions backlog → ready-for-dev (this story file) → in-progress (dev-story) → review → done
```

### Commands Reference

```bash
# Test suite
go test ./... -race -count=1 -timeout=120s

# Layer lint
go run scripts/lintlayers/main.go

# Targeted tests during dev
go test ./internal/pipeline/... -run TestPhaseARunner -race -v
go test ./internal/pipeline/agents/... -race -v

# Build
go build ./...
```

### Project Structure Notes

Alignment with architecture.md:
- `internal/pipeline/phase_a.go` — AC-PHASEARUNNER-STRUCT per architecture.md:1545.
- `internal/pipeline/agents/agent.go` — AC-AGENTFUNC-TYPE per architecture.md:1551-1552.
- `AgentFunc func(ctx, *PipelineState) error` — verbatim architecture.md:686.
- Purity rule — architecture.md:1731-1734 + enforced via layer-lint extension.
- `scenario.json` at `{outputDir}/{run-id}/scenario.json` — architecture.md:797.

Variance: The user's instruction names `internal/pipeline/runner.go` for Phase A chain execution. That file already exists as the `Runner` interface declaration (Story 2.1; preserved by AC-RUNNER-INTERFACE-PRESERVED). Architecture.md canonically places Phase A chain execution in `phase_a.go`. This story follows the architecture canonical file layout — the sprint-prompt text is an informal pointer, not the contract.

### References

- [PRD Phase A](docs/prd.md) — FR9–FR13, FR48
- [Architecture — Agent Chain](_bmad-output/planning-artifacts/architecture.md#Pipeline-Execution-Model) §architecture.md:679-783
- [Architecture — Agent Purity Rule](_bmad-output/planning-artifacts/architecture.md#Agent-Purity-Rule) §architecture.md:1729-1734
- [Architecture — Source Tree](_bmad-output/planning-artifacts/architecture.md#Source-Tree) §architecture.md:1540-1563
- [Architecture — Requirements to Structure Mapping](_bmad-output/planning-artifacts/architecture.md#Requirements-to-Structure-Mapping) §architecture.md:1748-1759
- [Epics — Epic 3 Story 3.1](_bmad-output/planning-artifacts/epics.md#Story-3.1) §epics.md:1134-1156
- [Epic 3 Scope Summary](_bmad-output/planning-artifacts/epics.md#Epic-3) §epics.md:402-422
- [Sprint Prompts — Epic 3 Story 3.1](_bmad-output/planning-artifacts/sprint-prompts.md#Epic-3---Story-3.1-개발) §sprint-prompts.md:402-424
- [Prior art — Story 2.3 engine.Advance stub](_bmad-output/implementation-artifacts/2-3-stage-level-resume-artifact-lifecycle.md) — the deliberate stub preserved here.
- [Prior art — Story 2.4 Recorder + logger discipline](_bmad-output/implementation-artifacts/2-4-per-stage-observability-cost-tracking.md) — logging conventions reused.
- Go stdlib: [`encoding/json`](https://pkg.go.dev/encoding/json), [`os.Rename` atomicity](https://pkg.go.dev/os#Rename), [`context`](https://pkg.go.dev/context).

## Dev Agent Record

### Agent Model Used

Claude Opus 4.7 (1M context)

### Debug Log References

### Completion Notes List

- Ultimate context engine analysis completed — comprehensive developer guide created.

### File List
