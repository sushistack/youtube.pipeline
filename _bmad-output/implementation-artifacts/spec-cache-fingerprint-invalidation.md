---
title: 'Cache fingerprint invalidation + run dir layout (_cache/, traces/)'
type: 'refactor'
created: '2026-05-04'
status: 'done'
baseline_commit: '3870fe3'
context:
  - "{project-root}/_bmad-output/planning-artifacts/next-session-visual-breakdown-alignment.md"
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Phase A run directory mixes resume caches, debug dumps, and final artifacts at one flat level (`audit.log`, `*_cache.json`, `scenario.json`, legacy `writer_debug.json`). Cache invalidation only checks `SCPID` + `SourceVersion`, so prompt/fewshot/model edits cause silent stale hits — fewshot iteration looks broken when it is actually short-circuited by stale cache.

**Approach:** Move resume caches into `_cache/` and (opt-in) per-attempt LLM traces into `traces/`. Wrap each cache file in a `CacheEnvelope` whose `fingerprint` is sha256 over six inputs (SourceVersion, prompt template SHA, fewshot SHA, model, provider, schema version). On load, recompute fingerprint and reject mismatches with a typed staleness reason; surface that reason via `GET /api/runs/{id}/cache` so the operator can drop with intent. Legacy envelopeless files become silent misses — no migration code.

## Boundaries & Constraints

**Always:**
- Atomic write pattern (tmp → fsync → rename) per `finalize_phase_a.go:55–86` for every cache write.
- Trace writer concurrency-safe per `audit.go:36–57` (mutex + append-only or per-file write).
- `debug_traces` flag lives in `config.yaml` only (no env var, no settings.json runtime override) — operations toggle.
- `NoopTraceWriter` when `debug_traces=false` → zero disk I/O, no `traces/` directory created.
- Resume must work end-to-end: failed-run resume reads `_cache/`, treats stale as miss, regenerates without operator input.
- Fingerprint inputs JSON is canonicalized (sorted keys) before sha256 — same inputs → identical hash across machines.

**Ask First:**
- (None — scope is bounded; new questions = HALT and re-elicit.)

**Never:**
- Phase B/C caches (image / TTS lifecycles untouched).
- `audit.log` location/format change (NDJSON, run-scoped, mutex-locked stays as-is).
- Frontend UI changes — cache panel "stale reason badge" is a separate task.
- Backwards-compat shim for legacy flat-path caches — they fall through to silent miss and regenerate.
- Merging with `tuning_service.go` critic prompt-version system — keep API compatible, do not unify.
- Skipping hooks, `--no-verify`, or any commit that mixes scope outside the 10 facets below.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Cache hit, fingerprint match | `_cache/research_cache.json` envelope, current effective config matches recorded fingerprint | `tryLoadCache` returns true, agent skipped, state populated from `payload` | N/A |
| Stale: prompt template changed | Envelope present, recorded `PromptTemplateSHA` ≠ current sha256 of `prompts/agents/{stage}.tmpl` | Cache miss, `staleness_reason="prompt_template_changed"` logged, agent re-runs, new envelope written | N/A |
| Stale: model or provider changed | Envelope present, recorded `Model`/`Provider` ≠ effective config | Cache miss, `staleness_reason="model_changed"` or `"provider_changed"` | N/A |
| Stale: SourceVersion or SchemaVersion changed | Envelope or payload version field doesn't match | `staleness_reason="source_version_mismatch"` or `"schema_changed"` | N/A |
| Legacy envelopeless file | File exists but lacks `envelope_version` field | Silent miss (treated as cache miss), agent re-runs, new envelope replaces it | N/A |
| Corrupt envelope | JSON unparseable or fingerprint not 64-char hex | `staleness_reason="envelope_corrupt"`, miss, regenerate | Logged as warning |
| `debug_traces=false` (default) | Phase A runs end-to-end | No `traces/` directory created, zero disk I/O for traces | N/A |
| `debug_traces=true`, retried agent | Writer fails attempt 1 then succeeds attempt 2 | `traces/writer.001.json` (with error) + `traces/writer.002.json` (with parsed output) | Trace write failure logged but does not fail the agent |
| `GET /api/runs/{id}/cache` post-edit | Operator changed prompt; envelope still on disk | Response entry has `staleness_reason="prompt_template_changed"`, `fingerprint` populated | Returns gracefully if envelope unparseable: omit fields, omit reason |

</frozen-after-approval>

## Code Map

- `internal/pipeline/cache.go` — **NEW**. `CacheEnvelope`, `FingerprintInputs`, `ComputeFingerprint`, `LoadEnvelope`, `WriteEnvelope`, `StalenessReason` enum + `EnvelopeVersion=1`.
- `internal/pipeline/traces.go` — **NEW**. `TraceWriter` interface, `TraceEntry` struct, `FileTraceWriter`, `NoopTraceWriter`. Per-attempt file naming `{stage}.{NNN}.json`.
- `internal/pipeline/phase_a.go:385–481` — replace `tryLoadCache`/`writeCache` to use envelope + fingerprint, change paths to `runDir/_cache/*`. Inject `TraceWriter` into `runAgent` (line 336–362) and around sub-stage LLM calls.
- `internal/pipeline/runner.go` (or wherever PhaseARunner is constructed) — wire `TraceWriter` from config flag.
- `internal/api/handler_run.go:21–35` — update `cacheFiles` map to `_cache/...` paths. Update `cacheEntryResponse` (line 389–398) to include `fingerprint` and `staleness_reason`. Cache handler decodes envelope + recomputes fingerprint.
- `internal/api/handler_run.go:519–530` — `drop_caches` continues to work via the updated path map.
- `internal/pipeline/rewind.go:195–225` — add `_cache/` and `traces/` dir removal ops; drop legacy flat-path entries.
- `internal/pipeline/artifact.go` — add `_cache/` + `traces/` to archive/cleanup if relevant; otherwise leave (per-stage cleanup not affected).
- `internal/domain/config.go` — add nested `Observability { DebugTraces bool }` struct embedded in `PipelineConfig`. `DefaultConfig()` sets `DebugTraces=false`.
- `internal/config/loader.go` — `v.SetDefault("observability.debug_traces", cfg.Observability.DebugTraces)`.
- `internal/config/settings_files.go` — add `Observability` to `orderedPipelineConfig` + assignment in `writeConfigFile()`.
- `config.yaml` — add `observability:\n  debug_traces: false`.
- `internal/pipeline/cache_test.go` — **NEW**. `ComputeFingerprint` determinism + 6-field sensitivity table; envelope round-trip.
- `internal/pipeline/phase_a_cache_test.go` — **NEW** (or extend `phase_a_test.go`). Cache hit, prompt-changed, model-changed, legacy-envelopeless miss.
- `internal/pipeline/traces_test.go` — **NEW**. Per-attempt files; noop = zero disk.
- `internal/api/handler_run_test.go` — extend with `TestCache_StalenessReason_Surfaced`.
- `internal/pipeline/smoke03_resume_test.go` — update path expectations to `_cache/` only (no behavior change).
- `internal/config/loader_test.go` — `TestLoad_DebugTracesDefaultsFalse` + `TestLoad_DebugTracesFromYAML`.

## Tasks & Acceptance

**Execution:**
- [ ] `internal/pipeline/cache.go` -- new file with `CacheEnvelope`, `FingerprintInputs`, `ComputeFingerprint(in) string`, `LoadEnvelope(path) (env, payload, reason, err)`, `WriteEnvelope(path, fp, sourceVersion, payload) error` (atomic). `StalenessReason` typed string with the 7 values from the matrix.
- [ ] `internal/pipeline/traces.go` -- new file with `TraceWriter` iface, `TraceEntry` (per spec field list), `FileTraceWriter` (mutex per stage+attempt counter, atomic per-file write to `{runDir}/traces/{stage}.{NNN}.json`), `NoopTraceWriter`.
- [ ] `internal/domain/config.go` -- add `type Observability struct { DebugTraces bool yaml:"debug_traces" mapstructure:"debug_traces" }`, embed as `Observability Observability yaml:"observability"`. Update `DefaultConfig()`.
- [ ] `internal/config/loader.go` -- `v.SetDefault("observability.debug_traces", cfg.Observability.DebugTraces)` next to existing setdefaults.
- [ ] `internal/config/settings_files.go` -- mirror new field in `orderedPipelineConfig` and `writeConfigFile()`.
- [ ] `config.yaml` -- add `observability:` block with `debug_traces: false`.
- [ ] `internal/pipeline/phase_a.go` -- replace `tryLoadCache` to call `LoadEnvelope`, build `FingerprintInputs` from prompt asset SHA + effective model/provider/fewshot SHA + schema version, compare, log staleness reason on miss. Replace `writeCache` to call `WriteEnvelope`. Switch paths to `filepath.Join(runDir, "_cache", "research_cache.json")` etc. Ensure `_cache/` is created before write. Wire `TraceWriter` field on `PhaseARunner`; emit `TraceEntry` from `runAgent` (and sub-stage LLM call sites for Polisher / PostWriterCritic / Reviewer / VisualBreakdowner / Critic) per attempt.
- [ ] `internal/pipeline/runner.go` (or PhaseARunner constructor / serve.go wiring) -- inject `FileTraceWriter` when `cfg.Observability.DebugTraces=true`, else `NoopTraceWriter`.
- [ ] `internal/api/handler_run.go` -- update `cacheFiles` paths to `_cache/research_cache.json`, `_cache/structure_cache.json`; `scenario` stays at top level. Extend `cacheEntryResponse` with `Fingerprint` + `StalenessReason` (omitempty). Cache handler decodes envelope, recomputes current fingerprint via shared helper, fills `staleness_reason` on mismatch.
- [ ] `internal/pipeline/rewind.go` -- add `_cache/` and `traces/` directory removal ops alongside `images/`, `tts/`, `clips/`. Drop legacy flat-path cache removals if any.
- [ ] `internal/pipeline/cache_test.go` -- `TestComputeFingerprint_Deterministic`, `TestComputeFingerprint_FieldSensitivity` (table: 6 fields × 2 baseline-vs-changed = 12 cases, each asserts hash differs from baseline), `TestCacheEnvelope_RoundTrip` (write → read → payload bit-stable).
- [ ] `internal/pipeline/phase_a_cache_test.go` -- `TestPhaseA_CacheHit_FingerprintMatch` (agent not called), `TestPhaseA_CacheStale_PromptTemplateChanged`, `TestPhaseA_CacheStale_ModelChanged`, `TestPhaseA_CacheStale_LegacyEnvelopelessFile` (silent miss, regenerated as envelope).
- [ ] `internal/pipeline/traces_test.go` -- `TestFileTraceWriter_PerAttemptFile` (writer attempt 1 + 2 → two files with `001`/`002` suffix, both exist), `TestNoopTraceWriter_NoDiskWrite` (no `traces/` directory after run).
- [ ] `internal/api/handler_run_test.go` -- `TestCache_StalenessReason_Surfaced` (seed envelope with old prompt SHA, hit endpoint, assert `staleness_reason="prompt_template_changed"`).
- [ ] `internal/pipeline/smoke03_resume_test.go` -- update seeded cache file paths to `_cache/` subdir; assert resume completes.
- [ ] `internal/config/loader_test.go` -- `TestLoad_DebugTracesDefaultsFalse` + `TestLoad_DebugTracesFromYAML`.
- [ ] Verification: `grep -rn "writer_debug\|writeWriterDebug" internal/` returns zero matches (already true; spec just locks it as a verify step).

**Acceptance Criteria:**
- Given a fresh run with `debug_traces=false`, when Phase A completes, then `runDir/_cache/research_cache.json` exists as a `CacheEnvelope`, `runDir/traces/` does **not** exist, and `runDir/audit.log` and `runDir/scenario.json` are at the top level unchanged.
- Given an envelope on disk and the operator edits `prompts/agents/script_writer.tmpl`, when the run is resumed, then `_cache/structure_cache.json` is invalidated with `staleness_reason="prompt_template_changed"` and the agent re-runs without operator intervention.
- Given a legacy envelopeless `_cache/research_cache.json` (or no `_cache/` dir at all), when Phase A starts, then it is treated as a cache miss with no error, and a fresh envelope replaces it.
- Given `GET /api/runs/{id}/cache`, when an envelope's fingerprint mismatches current effective config, then the response entry includes the matching `staleness_reason` enum value and a `fingerprint` field.
- Given `debug_traces=true` and Writer retries once, when the run completes, then `traces/writer.001.json` (with `error` populated and `response_parsed=null`) and `traces/writer.002.json` (with parsed output) both exist, neither truncates `prompt_rendered` or `response_raw`.
- Given `go test ./...`, when the suite runs, then it is green; existing resume smoke tests pass after path-only updates.

## Design Notes

**Fingerprint canonical form** — `ComputeFingerprint` marshals `FingerprintInputs` with deterministic field order (use struct field order; Go's `encoding/json` already preserves declaration order). Sha256 hex (lowercase, 64 chars). No timestamps or run-specific data — pure inputs.

**Prompt template SHA** — sha256 of the raw bytes of `prompts/agents/{stage}.tmpl` resolved via the same `LoadPromptAssets` path (`UseTemplatePrompts` flag). Researcher/Structurer use embedded prompts when the template flag is off; in that case fallback to sha256 of the embedded string. Stage→template mapping helper lives in `cache.go`.

**Fewshot SHA** — Researcher/Structurer don't use fewshots → empty string `""`. Writer/VisualBreakdowner do, but they are not currently cached (Phase A only caches Research + Structure outputs). Field present in envelope for future cacheable agents; computed from sorted file SHAs in `docs/exemplars/` if used. For this PR: empty string for both cached stages, but the `FingerprintInputs.FewshotSHA` field is populated and verified by the field-sensitivity test.

**Schema version** — distinct from `SourceVersion` (which gates payload semantics). `SchemaVersion` gates envelope-layer compatibility — bumped only when `CacheEnvelope` itself changes. Start at `"v1"`.

**Trace file naming** — `{stage}.{NNN}.json` where NNN is zero-padded 3-digit attempt counter, scoped per-stage per-run. `FileTraceWriter` keeps an in-memory `map[stage]int` counter under a mutex; on each `Write` call it increments and writes one file (no append, atomic tmp→rename). Sub-stages (e.g. Polisher, PostWriterCritic) each get their own counter. Polisher's monologue / segmenter sub-calls inside Writer get attempt-counted under the stage `"writer"` since the spec calls Writer one logical agent.

**Why `_cache/` not `cache/`** — leading underscore signals "managed by pipeline, do not edit by hand", matches existing `_bmad-output/` convention.

## Verification

**Commands:**
- `go build ./...` -- expected: success.
- `go test ./...` -- expected: green; resume smoke tests pass with new paths.
- `grep -rn "writer_debug\|writeWriterDebug" internal/` -- expected: zero matches.
- `grep -rn '"research_cache.json"\|"structure_cache.json"' --include='*.go' internal/ | grep -v _cache` -- expected: zero matches outside `_cache/` joins (i.e. all references go through the new path constant).

**Manual checks:**
- Run a fresh Phase A, inspect `output/{runID}/` tree: only `audit.log`, `scenario.json`, `_cache/`, `traces/` (if flag on) — no flat `*_cache.json`, no `writer_debug.json`.
- Toggle `debug_traces: true`, re-run, confirm `traces/writer.001.json` contains full prompt + raw response (no 2048-char truncation).
- Hit `GET /api/runs/{id}/cache` after touching a prompt template; confirm `staleness_reason` field appears in JSON.
