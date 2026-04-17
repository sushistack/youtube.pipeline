# Story 2.1: State Machine Core & Stage Transitions

Status: done

## Story

As a developer,
I want the pipeline state machine implemented as a pure function,
so that all stage transitions are testable, deterministic, and form the backbone of the pipeline lifecycle.

## Acceptance Criteria

1. **AC-NEXT-STAGE:** `internal/pipeline/engine.go` exports `NextStage(current domain.Stage, event domain.Event) (domain.Stage, error)` — a pure function with no DB access, no side effects, no imports beyond `domain` and stdlib.

2. **AC-VALID-TRANSITIONS:** Every valid (stage, event) pair returns the correct next stage with nil error. The 15 valid transitions:

   | Current Stage | Event | Next Stage |
   |---|---|---|
   | pending | Start | research |
   | research | Complete | structure |
   | structure | Complete | write |
   | write | Complete | visual_break |
   | visual_break | Complete | review |
   | review | Complete | critic |
   | critic | Complete | scenario_review |
   | critic | Retry | write |
   | scenario_review | Approve | character_pick |
   | character_pick | Approve | image |
   | image | Complete | tts |
   | tts | Complete | batch_review |
   | batch_review | Approve | assemble |
   | assemble | Complete | metadata_ack |
   | metadata_ack | Approve | complete |

3. **AC-INVALID-TRANSITIONS:** Every invalid (stage, event) pair returns a non-nil error with a descriptive message including the current stage and attempted event. Total invalid combinations: 15 stages x 4 events - 15 valid = 45 invalid.

4. **AC-HITL-STATUS:** `StatusForStage(stage domain.Stage) domain.Status` returns `StatusWaiting` for HITL stages (scenario_review, character_pick, batch_review, metadata_ack), `StatusRunning` for automated stages (research through assemble excluding HITL), `StatusPending` for pending, and `StatusCompleted` for complete.

5. **AC-EVENT-TYPE:** `domain.Event` type with constants: `EventStart`, `EventComplete`, `EventApprove`, `EventRetry`. Defined alongside `Stage` in `domain/types.go`.

6. **AC-STATUS-TYPE:** `domain.Status` type with constants: `StatusPending`, `StatusRunning`, `StatusWaiting`, `StatusCompleted`, `StatusFailed`, `StatusCancelled`. Defined in `domain/types.go`.

7. **AC-IS-VALID:** `Stage.IsValid() bool` method returns true only for the 15 defined stage constants. Resolves deferred work from Story 1.3.

8. **AC-RUNNER-IFACE:** `internal/pipeline/runner.go` defines `Runner` interface with `Advance(ctx context.Context, runID string) error` and `Resume(ctx context.Context, runID string) error`. This breaks the engine ↔ run_service circular dependency.

9. **AC-TESTS:** Exhaustive table-driven tests in `internal/pipeline/engine_test.go`: all 15 valid transitions verified, all 45 invalid transitions return error, HITL StatusForStage assertions for all 15 stages, Stage.IsValid() positive and negative cases.

## Tasks / Subtasks

- [x] **T1: Add Event, Status types and Stage.IsValid() to domain** (AC: #5, #6, #7)
  - [x] Add `Event` type (string) with 4 constants: `EventStart`, `EventComplete`, `EventApprove`, `EventRetry`
  - [x] Add `Status` type (string) with 6 constants: `StatusPending`, `StatusRunning`, `StatusWaiting`, `StatusCompleted`, `StatusFailed`, `StatusCancelled`
  - [x] Add `Stage.IsValid() bool` method (check against `allStages` array)
  - [x] Add `AllEvents() []Event` and `AllStatuses() []Status` convenience functions (match `AllStages()` pattern)
  - [x] Add `Event.IsValid() bool` and `Status.IsValid() bool` for consistency
  - [x] Add unit tests for all new types/methods in `domain/types_test.go`

- [x] **T2: Implement `NextStage()` pure function** (AC: #1, #2, #3)
  - [x] Create `internal/pipeline/engine.go`
  - [x] Implement `NextStage(current domain.Stage, event domain.Event) (domain.Stage, error)`
  - [x] Use Go `switch` statement — NOT a map (architecture: "Go switch + enum ~100 LoC")
  - [x] Return `fmt.Errorf("invalid transition: stage=%s event=%s", current, event)` for invalid pairs
  - [x] No imports beyond `domain`, `fmt`, `errors`

- [x] **T3: Implement `StatusForStage()` pure function** (AC: #4)
  - [x] Add `StatusForStage(stage domain.Stage) domain.Status` to `engine.go`
  - [x] HITL set: `{scenario_review, character_pick, batch_review, metadata_ack}` → `StatusWaiting`
  - [x] `pending` → `StatusPending`, `complete` → `StatusCompleted`
  - [x] All others → `StatusRunning`

- [x] **T4: Define `Runner` interface** (AC: #8)
  - [x] Create `internal/pipeline/runner.go`
  - [x] Define `Runner` interface: `Advance(ctx context.Context, runID string) error`, `Resume(ctx context.Context, runID string) error`
  - [x] Add doc comment explaining the circular dependency prevention purpose

- [x] **T5: Exhaustive engine tests** (AC: #9)
  - [x] Create `internal/pipeline/engine_test.go`
  - [x] Table-driven valid transitions test: 15 cases, each verifying (input stage, event) → expected output stage + nil error
  - [x] Table-driven invalid transitions test: all 45 invalid (stage, event) pairs → non-nil error
  - [x] StatusForStage test: all 15 stages mapped to correct status
  - [x] Stage.IsValid() test: all 15 valid stages + invalid values (`""`, `"garbage"`, `"PENDING"`)
  - [x] Use `testutil.AssertEqual[T]` for assertions (NOT testify, NOT gomock)
  - [x] Call `testutil.BlockExternalHTTP(t)` — paranoid habit, costs nothing

- [x] **T6: Verify green build** (AC: all)
  - [x] `go test ./internal/domain/... ./internal/pipeline/...` passes
  - [x] `make lint-layers` passes (pipeline imports only domain — already allowed)
  - [x] `make test` passes — zero regressions on existing 100+ tests

## Dev Notes

### Architecture Alignment

The architecture document specifies two locations:

1. **`domain/stages.go`** — "Stage/Status/Event enums, NextStage()" (architecture line 1517)
2. **`pipeline/engine.go`** — "State machine: Advance(), Resume()" (architecture line 1543)

**Resolution:** Stage enum already lives in `domain/types.go` (shipped in Story 1.3). Add Event, Status, and IsValid() alongside Stage in `types.go` — they're closely related types and types.go is ~110 lines (well under 300-line cap). `NextStage()` lives in `pipeline/engine.go` as the epics explicitly specify. The architecture's mention of NextStage() in domain/stages.go appears to be a design-time shorthand; the epics and sprint prompts are authoritative.

### Transition Matrix Design

**Critic has TWO valid transitions** — this is the only branching point in the state machine:
- `critic + EventComplete` → `scenario_review` (critic passed, proceed to HITL review)
- `critic + EventRetry` → `write` (critic rejected, retry from write stage)

All other stages have exactly ONE valid transition.

**HITL stages accept only `EventApprove`:**
- `scenario_review` → `character_pick`
- `character_pick` → `image`
- `batch_review` → `assemble`
- `metadata_ack` → `complete`

**`pending` accepts only `EventStart`** (not EventComplete — starting a pipeline is a distinct action).

**`complete` accepts NO events** — it's a terminal state. Any (complete, *) → error.

### Event Semantics

| Event | Meaning | Used by |
|---|---|---|
| `EventStart` | Pipeline begins execution | Only `pending` stage |
| `EventComplete` | Automated stage finished | All automated stages |
| `EventApprove` | Operator approved HITL checkpoint | All 4 HITL stages |
| `EventRetry` | Critic rejected, retry | Only `critic` stage |

The API layer translates operator actions (pick character, ack metadata) into `EventApprove` — semantic distinction happens at the API level, not the state machine level.

### Error Design

Use `fmt.Errorf` for invalid transition errors — NOT `domain.ErrValidation`. The state machine error is informational (describes what went wrong), and the caller (engine/service) wraps it with the appropriate domain error for HTTP mapping. Example:

```go
return "", fmt.Errorf("invalid transition: stage=%s event=%s", current, event)
```

The caller in Story 2.2 will wrap:
```go
next, err := NextStage(run.Stage, event)
if err != nil {
    return fmt.Errorf("advance run %s: %w", runID, err)
}
```

### HITL Stage Set

Maintain as a simple function, not a map:

```go
func IsHITLStage(s domain.Stage) bool {
    switch s {
    case domain.StageScenarioReview, domain.StageCharacterPick,
         domain.StageBatchReview, domain.StageMetadataAck:
        return true
    }
    return false
}
```

Export this — Story 2.2 (run_service) needs it for status determination.

### Runner Interface

The Runner interface exists solely to break the `engine ↔ run_service` circular dependency:

```
service/run_service.go → pipeline.Runner (interface)
pipeline/engine.go     → db/run_store   (concrete)
```

Story 2.1 DEFINES the interface. Story 2.2 IMPLEMENTS it in engine.go and CONSUMES it in run_service.go. The interface is intentionally minimal:

```go
// Runner abstracts the pipeline execution engine.
// Defined here (not in service/) to allow service/ to depend on pipeline.Runner
// without pipeline/ depending on service/.
type Runner interface {
    Advance(ctx context.Context, runID string) error
    Resume(ctx context.Context, runID string) error
}
```

### Import Rules for This Story

| Package | Allowed to import | This story adds |
|---|---|---|
| `pipeline/` | `domain/`, `db/`, `llmclient/`, `clock/` | Only `domain/` used |
| `domain/` | Nothing from `internal/` | No change |

`pipeline/engine.go` imports only `domain` and stdlib. `pipeline/runner.go` imports only `context` and stdlib. Both pass `make lint-layers`.

### Existing Code to Reference

- **Stage constants:** `internal/domain/types.go:4-22` — 15 stages already defined with `allStages` array and `AllStages()` function. Follow this exact pattern for Event and Status.
- **Run.Status field:** `internal/domain/types.go:43` — currently `string` type. Do NOT change the struct field type to `Status` in this story (that's a Story 2.2 concern when Run CRUD is implemented). Just define the `Status` type.
- **Existing tests pattern:** `internal/domain/types_test.go` — snake_case JSON tag assertions, AllStages count check. Follow this pattern for new type tests.
- **Test utilities:** `internal/testutil/assert.go` — `AssertEqual[T](t, got, want)` for assertions. `internal/testutil/nohttp.go` — `BlockExternalHTTP(t)`.
- **Pipeline package:** `internal/pipeline/doc.go` exists (package declaration). `internal/pipeline/e2e_test.go` exists with mock providers — do NOT modify.

### Deferred Work Resolved

- **From Story 1.3:** "`Stage("")` is a valid value with no validation — add `IsValid()` when state machine is implemented (Epic 2)." → Resolved by T1 (Stage.IsValid()).

### Deferred Work Awareness

- **From Story 1.2:** "`runs.updated_at` column has no AFTER UPDATE trigger." → Story 2.2 scope, not this story.
- **From Story 1.5:** "`WriterCriticCheck` returns misleading error for empty providers." → Story 2.2+ scope when full provider validation is added.

### Project Structure After This Story

```
internal/
  domain/
    types.go        # MODIFIED — add Event, Status types + IsValid()
    types_test.go   # MODIFIED — add Event, Status, IsValid tests
  pipeline/
    doc.go          # EXISTING — no change
    engine.go       # NEW — NextStage(), StatusForStage(), IsHITLStage()
    engine_test.go  # NEW — exhaustive table-driven tests
    runner.go       # NEW — Runner interface definition
    e2e_test.go     # EXISTING — no change
```

### Critical Constraints

- **No testify, no gomock.** Go stdlib `testing` + `testutil.AssertEqual[T]` only.
- **No DB dependency.** engine.go is pure logic — zero database imports.
- **CGO_ENABLED=0.** All tests run without CGO.
- **domain/ 300-line cap.** types.go is ~110 lines; adding ~50 lines for Event/Status/IsValid stays well under 300.
- **Module path:** `github.com/sushistack/youtube.pipeline`
- **snake_case for string constants** — stage values are already snake_case, follow same for Event and Status.
- **Switch statement for NextStage** — architecture specifies "Go switch + enum ~100 LoC". Do NOT use a map-based transition table.
- **`testutil.BlockExternalHTTP(t)`** in every test file — Layer 2 defense habit.

### References

- Epic 2 scope and FRs: [epics.md:378-399](_bmad-output/planning-artifacts/epics.md)
- Story 2.1 AC (BDD): [epics.md:905-934](_bmad-output/planning-artifacts/epics.md)
- State machine design: [architecture.md:739-783](_bmad-output/planning-artifacts/architecture.md)
- Pipeline execution model: [architecture.md:679-738](_bmad-output/planning-artifacts/architecture.md)
- Circular dependency prevention: [architecture.md:1710-1727](_bmad-output/planning-artifacts/architecture.md)
- Project structure: [architecture.md:1501-1580](_bmad-output/planning-artifacts/architecture.md)
- Import direction rules: [architecture.md:1059-1077](_bmad-output/planning-artifacts/architecture.md)
- Layer-import linter: [scripts/lintlayers/main.go](../../scripts/lintlayers/main.go)
- Existing Stage constants: [internal/domain/types.go:4-36](../../internal/domain/types.go)
- Existing error types: [internal/domain/errors.go](../../internal/domain/errors.go)
- Test utilities: [internal/testutil/assert.go](../../internal/testutil/assert.go)
- Deferred work registry: [deferred-work.md](_bmad-output/implementation-artifacts/deferred-work.md)
- Previous story (1.7): [1-7-ci-pipeline-e2e-smoke-test.md](_bmad-output/implementation-artifacts/1-7-ci-pipeline-e2e-smoke-test.md)

### Review Findings

- [x] [Review][Defer] NextStage does not distinguish "unknown stage" from "invalid transition for known stage" — same error format for both. Defer: input validation is caller responsibility; adding guards would exceed ~100 LoC target and change function contract.
- [x] [Review][Defer] Hardcoded magic numbers (15, 45) in engine_test.go guard assertions — fragile if stages/events are added. Defer: intentional documentation of expected counts; tests WILL fail on enum changes, which is the desired behavior.
- [x] [Review][Defer] StatusForStage returns StatusRunning for unknown/invalid Stage values — no error return. Defer: function is only called with validated stages from DB; adding error return would change signature beyond spec.
- [x] [Review][Defer] TestNextStage_InvalidTransitions checks err != nil but does not assert error message content includes stage and event. Defer: implementation is correct; test thoroughness gap is minor.

## Dev Agent Record

### Agent Model Used

claude-opus-4-6

### Debug Log References

None

### Completion Notes List

- `domain/types.go`: Added `Event` type (4 constants: start, complete, approve, retry), `Status` type (6 constants: pending, running, waiting, completed, failed, cancelled), `Stage.IsValid()`, `Event.IsValid()`, `Status.IsValid()` methods, `AllEvents()`, `AllStatuses()` convenience functions. All follow existing `AllStages()` pattern.
- `pipeline/engine.go`: `NextStage()` pure function — switch-based 15-stage transition matrix with 15 valid transitions (critic has 2: complete→scenario_review, retry→write). `IsHITLStage()` helper for 4 HITL wait points. `StatusForStage()` maps all 15 stages to correct operational status. Zero DB imports, zero side effects.
- `pipeline/runner.go`: `Runner` interface with `Advance()` and `Resume()` methods. Breaks engine↔run_service circular dependency.
- `pipeline/engine_test.go`: 4 table-driven tests — 15 valid transitions, 45 invalid transitions (exhaustive), 15 StatusForStage mappings, 15 IsHITLStage checks. Uses `testutil.AssertEqual[T]` and `testutil.BlockExternalHTTP(t)`.
- `domain/types_test.go`: 7 new tests — Stage.IsValid(), AllEvents count/values, Event.IsValid(), AllStatuses count/values, Status.IsValid().
- Resolves deferred work from Story 1.3: `Stage("").IsValid()` now returns false.
- All 100+ existing tests pass. Layer-import lint clean. Zero regressions.

### Change Log

- 2026-04-17: Story 2.1 implemented — state machine core (NextStage, StatusForStage, IsHITLStage), Event/Status domain types, Runner interface, exhaustive tests

### File List

- internal/domain/types.go (modified)
- internal/domain/types_test.go (modified)
- internal/pipeline/engine.go (new)
- internal/pipeline/engine_test.go (new)
- internal/pipeline/runner.go (new)
