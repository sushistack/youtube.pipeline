# Deferred Work

## Deferred from: code review of 1-1-go-react-spa-project-scaffolding-build-chain (2026-04-16)

- `-race` flag requires CGO but Makefile uses `CGO_ENABLED=0` — architecture doc internally inconsistent. Resolve when adding CI pipeline (Story 1.7): either split `test-race` target or enable CGO for test target only.
- AC-GITIGNORE spec text says `web/dist/` but actual build output goes to `internal/web/dist/` — update story AC text to match chosen Vite outDir architecture.

## Deferred from: code review of 1-2-sqlite-database-migration-infrastructure (2026-04-16)

- WAL sidecar files (-wal, -shm) inherit default umask permissions instead of 0600. Spec says "DB file" (singular); fixing requires process-level umask or post-hoc chmod of lazily-created files. Localhost-only tool mitigates risk.
- `runs.updated_at` column has DEFAULT but no AFTER UPDATE trigger. Column will be stale after any UPDATE. Add trigger in a future migration when UPDATE operations are implemented (Epic 2).

## Deferred from: code review of 1-3-domain-types-error-system-architecture-interfaces (2026-04-17)

- DomainError sentinel pointers are mutable `var` — any code could mutate fields. Internal package + single-user tool mitigates risk. Consider unexported fields with getters if domain/ becomes shared.
- `Stage("")` is a valid value with no validation — add `IsValid()` when state machine is implemented (Epic 2).
- TextRequest/ImageRequest zero-values have no validation — add `Validate()` methods when provider implementations are built (Epic 5).

## Deferred from: code review of 1-4-test-infrastructure-external-api-isolation (2026-04-17)

- `BlockExternalHTTP` mutates global `http.DefaultTransport` without synchronization — not goroutine-safe. Layer 1 (constructor injection) covers parallel test scenarios. Add mutex if `t.Parallel()` + `BlockExternalHTTP` combination becomes needed.
- `Migrate` sets `PRAGMA user_version` outside transaction — crash between COMMIT and PRAGMA leaves inconsistent state. Pre-existing issue from Story 1.2, acceptable for single-user localhost tool.

## Deferred from: code review of 1-5-cli-foundation-init-doctor-commands (2026-04-17)

- `BindFlags` exported but dead code — `Load()` creates an internal viper instance so external pflags can't be bound. Redesign when CLI flag overrides for individual config values are needed (Story 1.6+ scope).
- `DefaultConfig()` and `DefaultConfigDir()` both compute `~/.youtube-pipeline` independently, both silently ignore `os.UserHomeDir()` error. Single-user desktop tool mitigates; revisit if containerized deployment is ever considered.
- `WriterCriticCheck` returns "must use different providers" when both providers are empty string — technically correct but misleading. Improve when Epic 2 adds full provider validation.
- `godotenv.Load()` permanently mutates process-level environment variables. Acceptable for single-process CLI but may cause env leak in tests calling `Load()` multiple times.

## Deferred from: code review of 1-6-cli-renderer-output-formatting (2026-04-17)

- ANSI color codes emitted unconditionally without TTY detection, NO_COLOR env check, or Windows ANSI VT support verification. Localhost single-operator tool mitigates. Add `golang.org/x/term.IsTerminal` or `NO_COLOR` check when piped/redirected output becomes a use case.
- TOCTOU race in `writeConfigIfNotExists` and `writeEnvIfNotExists` (pre-existing from Story 1.5). Use `os.OpenFile` with `O_CREATE|O_EXCL` for atomic create-if-not-exists.
- `database.Close()` return value silently discarded in init.go (pre-existing from Story 1.5). Add `defer` with error check when init becomes more complex.

## Deferred from: code review of 1-7-ci-pipeline-e2e-smoke-test (2026-04-17)

- `BlockExternalHTTP` mutates global `http.DefaultTransport` without synchronization — not goroutine-safe for parallel tests. Pre-existing from Story 1.4; add mutex when `t.Parallel()` is introduced.

## Deferred from: code review of 2-1-state-machine-core-stage-transitions (2026-04-17)

- `NextStage` does not distinguish "unknown stage" from "invalid transition for known stage" — same error format for both. Input validation is caller responsibility; adding guards would exceed ~100 LoC target.
- Hardcoded magic numbers (15, 45) in `engine_test.go` guard assertions — fragile if stages/events are added. Intentional as documentation; tests WILL fail on enum changes (desired behavior).
- `StatusForStage` returns `StatusRunning` for unknown/invalid Stage values without error. Function is only called with validated stages from DB; signature change not warranted for V1.
- `TestNextStage_InvalidTransitions` checks `err != nil` but does not assert error message content includes stage and event strings. Minor test thoroughness gap.
- `BlockExternalHTTP` only intercepts `http.DefaultTransport` — custom-transport HTTP clients bypass the block. Architecture limitation of Layer 2 defense; Layer 1 (constructor injection) is the primary guard.
- `test-e2e` job uses `continue-on-error: true` which masks real failures. Remove once `pipeline serve` command is implemented (Epic 6) and add `needs: [test-e2e]` to `build` job.

## Deferred from: code review of 2-2-run-create-cancel-inspect (2026-04-17)

- Vite reverse-proxy error responses in `pipeline serve --dev` bypass the api middleware chain — no `X-Request-ID`, no slog JSON line, 502 from stdlib log. Acceptable during dev-only usage; revisit when web UI stabilizes in Epic 6.
- `newRequestID` falls back to the string `"unknown"` when `crypto/rand.Read` returns an error. Concurrent requests all get the same ID, breaking log correlation. Theoretical — `/dev/urandom` has not been observed to fail on Linux. Add monotonic-counter fallback if it ever becomes an issue.

## Deferred from: code review of 2-3-stage-level-resume-artifact-lifecycle (2026-04-17)

- Permission-denied mid-`CleanStageArtifacts` leaves partial on-disk state. `os.RemoveAll` aborts on first error; re-running Resume after permission fix completes the cleanup (idempotent). V1 accept.
- Context cancellation not honored inside `os.RemoveAll` — stdlib constraint. Torn-state risk is addressed by the atomicity patch on `ResumeWithOptions`.
- `scanEpisode` fails completely when a `segments.shots` TEXT column has malformed JSON. A single bad row blocks all Resumes for the run until manual DB intervention. Graceful-skip semantics deferred to V1.5.
- DB lock under concurrent `pipeline serve` + `pipeline resume` on the same SQLite file. `busy_timeout=5000` already applied at `OpenDB`; single-operator tool mitigates risk.
- Reload error after successful engine mutation surfaces to caller as a resume-failure even though state was committed. Retrying then produces `ErrConflict`. Add idempotent acknowledgment semantics in a future revision.

## Deferred from: implementation of 2-4-per-stage-observability-cost-tracking (2026-04-18)

- **Cost accumulator priming on process restart.** When Story 3.1 wires the engine, `CostAccumulator` should be seeded with `runs.cost_usd` so the per-run cap survives a crash/restart. Not implemented here because there is no caller yet — dead code deferred.
- **Jitter seed via clock interface.** `WithRetry`'s jitter uses a package-level `math/rand` seeded from `time.Now().UnixNano()` — cannot be made fully deterministic in tests without a package-level seed swap. Tests assert elapsed-time *bounds* instead. Thread jitter through `clock.Clock` when it gains a `Rand()` surface.
- **Per-stage cost history for a single run.** V1 aggregates cost into one column per run; "cost by stage for one run" requires a separate observability history table. Story 2.7's metrics CLI may surface this gap.
- **Emergency cap override.** No `--ignore-cost-cap` CLI flag. Operators who want to push past `CostCapPerRun` for a critical run must edit `~/.youtube-pipeline/config.yaml`. Deferred to Epic 10.
- **Per-shot cost attribution in Phase B.** Image track records one `StageObservation` per shot → all sums collapse onto `StageImage`. Fine for cost totals, useless for per-shot forensics. V1.5 may add `shot_index` to observability when Story 5.4 ships.
- **`retry_reason` nil-preservation via COALESCE is opinionated.** Passing `nil` means "leave retry_reason alone". A `Recorder.ClearRetryReason(runID)` would be additive; add only if the engine ever needs to clear outside the Resume path (which already clears via `ResetForResume`).
- **Linter consolidation carried forward from Story 2.3.** `SetStatus` + `IncrementRetryCount` were merged into `ResetForResume`; `SetStage` was deleted as dead code; `Engine.Resume` now returns `(*InconsistencyReport, error)`; `ClearClipPathsByRunID` was added for assemble-stage resume. Callers in `service`, `cmd/pipeline/resume.go`, and `internal/api/handler_run.go` were threaded to carry the report through as `warnings`. `docs/contracts/run.resume.response.json` was not touched — verify the contract test still matches the threaded shape in the next code review.
- **Consistency check strictness regression (linter-introduced).** `CheckConsistency` now emits `run_directory_missing` for post-Phase-A runs when the runDir is absent. The pre-existing test `TestCheckConsistency_EmptyScenarioPathTreatedAsMissing` was replaced with `TestCheckConsistency_EmptyScenarioPathTreatedAsNil`. Confirm this flip matches Story 2.3's intent in its code review.
- **Engine.Advance remains a stub.** Story 2.4's `Recorder` + `CostAccumulator` are designed to drop into `Advance` when Epic 3 lands; no wiring today.
