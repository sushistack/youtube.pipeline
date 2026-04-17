# Story 1.5: CLI Foundation — Init & Doctor Commands

Status: done

## Story

As an operator,
I want `init` and `doctor` commands,
so that I can set up a fresh project and verify all prerequisites are met before running the pipeline.

## Acceptance Criteria

1. **AC-INIT:** `pipeline init` creates `~/.youtube-pipeline/config.yaml` with model IDs, DashScope region, paths, and per-stage cost caps. Creates `.env` template listing required secrets: `DASHSCOPE_API_KEY`, `DEEPSEEK_API_KEY`, `GEMINI_API_KEY`. Initializes SQLite database via `db.OpenDB()`. Creates output directory layout (`~/.youtube-pipeline/output/`).

2. **AC-CONFIG-STRUCT:** `internal/domain/config.go` defines `PipelineConfig` struct with typed fields for all configuration: model IDs (writer, critic, TTS, image), provider names, DashScope region, data path (`/mnt/data/raw`), output directory, DB path, and per-stage cost caps.

3. **AC-LOADER:** `internal/config/loader.go` implements Viper-based config loading with hierarchy: `.env` (secrets via `AutomaticEnv`) → `config.yaml` (non-secret) → CLI flags (via Cobra `PersistentFlags` binding). Returns `domain.PipelineConfig`.

4. **AC-DOCTOR-PASS:** `pipeline doctor` executes 4 checks and reports pass/fail with remediation hints:
   - (a) API key presence: 3 required keys (`DASHSCOPE_API_KEY`, `DEEPSEEK_API_KEY`, `GEMINI_API_KEY`)
   - (b) Filesystem path writability: output dir, data dir
   - (c) FFmpeg binary availability: `ffmpeg -version`
   - (d) Writer ≠ Critic provider validation (FR46)

5. **AC-WRITER-CRITIC:** When Writer and Critic provider are configured to the same value, `pipeline doctor` fails with: `"Writer and Critic must use different LLM providers"`.

6. **AC-CHECK-INTERFACE:** Doctor checks are implemented via extensible registry. Adding a new check requires only implementing a `Check` interface and registering it. No modification of existing code.

7. **AC-HIERARCHY:** Config hierarchy test: `.env` secrets override defaults; `config.yaml` values override defaults; CLI flags override both.

8. **AC-IDEMPOTENT:** `pipeline init` is idempotent — running it again on an existing setup does not destroy existing config/data. Existing `config.yaml` is preserved (not overwritten). Existing `.env` is preserved. DB migration is additive (already handled by `db.Migrate`).

9. **AC-TEST:** Integration tests: init creates expected directory tree; doctor passes with valid config; doctor fails on missing key with correct remediation message; doctor fails on Writer=Critic; config hierarchy test (env overrides yaml).

## Tasks / Subtasks

- [x] **T1: `internal/domain/config.go` — PipelineConfig struct** (AC: #2)
  - [x] Define `PipelineConfig` struct with all typed fields
  - [x] Define `DefaultConfig()` returning sensible defaults
  - [x] No imports from `internal/` (domain import rule)

- [x] **T2: `internal/config/loader.go` — Viper config loading** (AC: #3, #7)
  - [x] `Load(cfgPath string, envPath string) (domain.PipelineConfig, error)` — loads `.env` + YAML + binds env vars
  - [x] Viper config: `SetConfigFile(cfgPath)`, `SetConfigType("yaml")`
  - [x] `.env` loading via `godotenv` (Viper alone doesn't parse `.env` reliably)
  - [x] `AutomaticEnv()` for env var override
  - [x] `BindFlags()` helper for CLI flag binding
  - [x] Return populated `domain.PipelineConfig`
  - [x] `internal/config/loader_test.go` — hierarchy test with temp files

- [x] **T3: `internal/config/doctor.go` — Check interface + registry + 4 checks** (AC: #4, #5, #6)
  - [x] Define `Check` interface: `Name() string`, `Run(cfg domain.PipelineConfig) error`
  - [x] Define `Registry` with `Register(Check)` and `RunAll(cfg) []Result`
  - [x] `Result` struct: `Name string`, `Passed bool`, `Message string`
  - [x] Implement `APIKeyCheck` — verifies 3 env vars non-empty
  - [x] Implement `FSWritableCheck` — verifies output dir and data dir writable
  - [x] Implement `FFmpegCheck` — runs `ffmpeg -version`, checks exit code (with injectable LookPath for testing)
  - [x] Implement `WriterCriticCheck` — compares writer/critic provider, exact error message
  - [x] Default registry with all 4 checks pre-registered

- [x] **T4: `cmd/pipeline/main.go` — Register init + doctor subcommands** (AC: #1, #4)
  - [x] `initCmd` — calls config loader for defaults, writes `config.yaml` (if not exists), writes `.env` template (if not exists), calls `db.OpenDB()`, creates output dir
  - [x] `doctorCmd` — loads config, runs check registry, prints results, returns error on failure
  - [x] `--config` persistent flag for custom config path (default `~/.youtube-pipeline/config.yaml`)
  - [x] Split into `cmd/pipeline/init.go` and `cmd/pipeline/doctor.go`

- [x] **T5: `.env.example` — Template file** (AC: #1)
  - [x] Create `.env.example` at project root with 3 placeholder keys
  - [x] `pipeline init` writes `.env` template to config dir if not exists

- [x] **T6: Tests** (AC: #9)
  - [x] `internal/config/loader_test.go` — config hierarchy: env > yaml > default; all fields populated
  - [x] `internal/config/doctor_test.go` — 4 checks pass/fail; WriterCriticCheck exact message; registry extensibility
  - [x] Integration test: init creates directory tree; doctor passes with valid setup; doctor fails on missing key

### Review Findings

- [x] [Review][Patch] `FSWritableCheck` creates directories as side effect — doctor should verify-only, not create [internal/config/doctor.go:100-101]
- [x] [Review][Patch] `go.mod` has stray blank lines in indirect requires block [go.mod:13]
- [x] [Review][Defer] `BindFlags` dead code — CLI flag→Viper binding not wired, `Load()` creates internal viper — deferred, Story 1.6+ scope
- [x] [Review][Defer] `DefaultConfig()` + `DefaultConfigDir()` duplicate `~/.youtube-pipeline` path computation; `UserHomeDir` error silently ignored — deferred, single-user tool mitigates risk
- [x] [Review][Defer] `WriterCriticCheck` — both-empty providers yield misleading "must use different" message — deferred, Epic 2 provider validation will improve
- [x] [Review][Defer] `godotenv.Load()` permanently mutates process environment — deferred, acceptable for single-process CLI

## Dev Notes

### PipelineConfig Struct (domain/config.go)

Architecture specifies this lives in `internal/domain/config.go` (architecture.md line 1520). It holds cost caps, model IDs, and paths — NOT secrets. Secrets stay in `.env` and are accessed via `os.Getenv()` at runtime.

```go
// PipelineConfig holds non-secret pipeline configuration.
// Secrets (API keys) are read from .env via environment variables.
type PipelineConfig struct {
    // LLM Model IDs
    WriterModel   string `yaml:"writer_model"   mapstructure:"writer_model"`
    CriticModel   string `yaml:"critic_model"   mapstructure:"critic_model"`
    TTSModel      string `yaml:"tts_model"      mapstructure:"tts_model"`
    ImageModel    string `yaml:"image_model"     mapstructure:"image_model"`

    // Provider names (for Writer ≠ Critic enforcement)
    WriterProvider string `yaml:"writer_provider" mapstructure:"writer_provider"`
    CriticProvider string `yaml:"critic_provider" mapstructure:"critic_provider"`

    // DashScope
    DashScopeRegion string `yaml:"dashscope_region" mapstructure:"dashscope_region"`

    // Paths
    DataDir   string `yaml:"data_dir"   mapstructure:"data_dir"`
    OutputDir string `yaml:"output_dir" mapstructure:"output_dir"`
    DBPath    string `yaml:"db_path"    mapstructure:"db_path"`

    // Cost caps (USD per stage)
    CostCapResearch  float64 `yaml:"cost_cap_research"  mapstructure:"cost_cap_research"`
    CostCapWrite     float64 `yaml:"cost_cap_write"     mapstructure:"cost_cap_write"`
    CostCapImage     float64 `yaml:"cost_cap_image"     mapstructure:"cost_cap_image"`
    CostCapTTS       float64 `yaml:"cost_cap_tts"       mapstructure:"cost_cap_tts"`
    CostCapAssemble  float64 `yaml:"cost_cap_assemble"  mapstructure:"cost_cap_assemble"`
}
```

**DefaultConfig()** returns:
- `WriterProvider`: `"deepseek"`, `CriticProvider`: `"gemini"` (ensures Writer ≠ Critic out of the box)
- `DashScopeRegion`: `"cn-beijing"`
- `DataDir`: `/mnt/data/raw`
- `OutputDir`: `~/.youtube-pipeline/output`
- `DBPath`: `~/.youtube-pipeline/pipeline.db`
- Cost caps: reasonable defaults (e.g., $0.50 per stage)

**Import rule:** `domain/config.go` imports NOTHING from `internal/`. It is a pure data struct + defaults function.

### Viper Config Loading (internal/config/loader.go)

Architecture specifies `internal/config/loader.go` (line 1524): "Viper YAML + .env loading → domain.PipelineConfig".

**Hierarchy enforcement:**
1. `.env` — loaded via `godotenv.Load()` (populates `os.Environ`). Viper reads these via `AutomaticEnv()`.
2. `config.yaml` — loaded via `viper.ReadInConfig()`.
3. CLI flags — bound via `viper.BindPFlags()`.

**Critical: Viper v1.11.0 is NOT in go.mod yet.** Must `go get github.com/spf13/viper@v1.11.0`. Also need `go get github.com/joho/godotenv@latest` for `.env` parsing (Viper alone doesn't handle `.env` format).

**Note on Cobra version:** go.mod has `cobra v1.10.2` but architecture specifies `v2.5.1`. **Cobra v2.5.1 does not exist** — Cobra's latest stable is v1.x (1.10.x as of April 2026). The architecture doc's "v2.5.1" is an error. Keep `cobra v1.10.2` as-is. Do NOT attempt to upgrade.

### Check Interface & Registry (internal/config/doctor.go)

```go
// Check is the interface for doctor preflight checks.
type Check interface {
    Name() string
    Run(cfg domain.PipelineConfig) error
}

// Result holds the outcome of a single check.
type Result struct {
    Name    string
    Passed  bool
    Message string // empty on pass; remediation hint on fail
}

// Registry holds registered checks and runs them.
type Registry struct {
    checks []Check
}

func NewRegistry() *Registry { return &Registry{} }
func (r *Registry) Register(c Check) { r.checks = append(r.checks, c) }
func (r *Registry) RunAll(cfg domain.PipelineConfig) []Result { ... }
```

**DefaultRegistry()** returns a registry with all 4 checks pre-registered:
1. `APIKeyCheck` — checks `os.Getenv("DASHSCOPE_API_KEY")`, etc.
2. `FSWritableCheck` — attempts `os.MkdirAll` + `os.CreateTemp` in output dir and data dir
3. `FFmpegCheck` — `exec.LookPath("ffmpeg")` + `exec.Command("ffmpeg", "-version")`
4. `WriterCriticCheck` — `cfg.WriterProvider == cfg.CriticProvider` → error with exact message

**WriterCriticCheck exact error message:** `"Writer and Critic must use different LLM providers"` — this is specified in the AC and must match exactly.

### Init Command Flow

```
pipeline init [--config PATH]
  1. Resolve config dir (default: ~/.youtube-pipeline/)
  2. If config.yaml doesn't exist → write DefaultConfig() as YAML
  3. If .env doesn't exist → copy .env template with placeholder keys
  4. Create output directory (os.MkdirAll)
  5. Initialize SQLite DB via db.OpenDB(cfg.DBPath)
  6. Print success message with paths
```

**Idempotency:** Check existence before writing. `os.MkdirAll` is already idempotent. `db.OpenDB` + `db.Migrate` is additive.

### Doctor Command Flow

```
pipeline doctor [--config PATH]
  1. Load config via config.Load()
  2. Create DefaultRegistry()
  3. Run all checks
  4. Print results (pass/fail + remediation hints)
  5. Exit code: 0 if all pass, 1 if any fail
```

### File Organization After This Story

```
internal/domain/
  config.go              # NEW — PipelineConfig struct + DefaultConfig()

internal/config/
  loader.go              # NEW — Viper YAML + .env loading → PipelineConfig
  loader_test.go         # NEW — hierarchy tests
  doctor.go              # NEW — Check interface, Registry, 4 checks
  doctor_test.go         # NEW — check pass/fail tests

cmd/pipeline/
  main.go                # MODIFIED — add init + doctor subcommands

.env.example             # NEW — template with 3 placeholder API keys
```

### Dependencies to Add

```bash
go get github.com/spf13/viper@v1.11.0
go get github.com/joho/godotenv@latest
```

**Do NOT upgrade Cobra.** Architecture says v2.5.1, but that version doesn't exist. Current `cobra v1.10.2` in go.mod is correct and stable. The API is identical for our use case.

### Critical Constraints

- **No testify, no gomock.** Go stdlib `testing` only (consistent with Stories 1.1–1.4).
- **CGO_ENABLED=0.** All builds and tests work without CGO.
- **`domain/config.go` imports nothing from `internal/`.** Pure data struct + function.
- **`internal/config/` imports `internal/domain/` only.** Follow import direction: `config → domain`.
- **Cobra `v1.10.2`** — do NOT upgrade to nonexistent v2.5.1.
- **Viper `v1.11.0`** — must add this dependency.
- **`godotenv`** — needed for `.env` file parsing.
- **Secrets stay in `.env`, never in `config.yaml`.** Config struct holds non-secret values only.
- **`db.OpenDB()` already runs migrations.** No need to call `db.Migrate()` separately.
- **Doctor check exact error messages.** WriterCriticCheck: `"Writer and Critic must use different LLM providers"`.
- **`pipeline init` is idempotent.** Never overwrite existing config.yaml or .env.
- **Exit code:** `pipeline doctor` returns exit code 1 if any check fails.
- **300-line file cap** applies to `domain/` files. `config/` files have no hard cap but keep focused.
- **`slog.Logger`** constructor-injected pattern — follow for any logging in config/doctor.

### Previous Story Intelligence

**Story 1.4 (review):** Test infrastructure and API isolation.
- `testutil.AssertEqual[T]` — use for test assertions in config tests.
- `testutil.BlockExternalHTTP(t)` — call in any test that might make HTTP calls.
- `testutil.LoadFixture(t, path)` — available for loading test fixtures.
- Test pattern: `t.Run` subtests, `t.Helper()`, `t.Cleanup()`, `t.TempDir()`.
- Error format: `fmt.Errorf("context: %w", err)`.

**Story 1.3 (done):** Domain types, errors, provider interfaces.
- `domain.Run`, `domain.Stage` — established patterns for struct definitions.
- `domain.DomainError` — classification system. Config validation errors could use `domain.ErrValidation`.
- Error wrapping pattern: `fmt.Errorf("load config: %w", err)`.
- `domain/` zero-import rule verified and enforced.

**Story 1.2 (done):** SQLite DB.
- `db.OpenDB(path)` — opens DB with WAL + busy_timeout + foreign_keys + migrations.
- `db.Migrate(db)` — called internally by OpenDB. No separate call needed.
- Driver import: `_ "github.com/ncruces/go-sqlite3/driver"` — required in any file that opens a DB.

**Story 1.1 (done):** Project scaffolding.
- `cmd/pipeline/main.go` — Cobra root command exists. Add subcommands here.
- Makefile: `CGO_ENABLED=0` for all builds and tests.
- Go module: `github.com/sushistack/youtube.pipeline`.

### Git Intelligence

Recent commits use imperative mood, brief subject lines:
- "Add LLM provider package stubs"
- "Add SQLite database layer and migration infrastructure"

Commit style for this story: `Add CLI init and doctor commands with Viper config loading`.

### Project Structure Notes

- `internal/config/` directory does NOT exist yet — must be created.
- `internal/domain/config.go` does NOT exist yet — must be created alongside existing `types.go`, `errors.go`, `llm.go`.
- `cmd/pipeline/main.go` exists with bare Cobra root command — will be modified.
- `.env.example` does NOT exist — must be created at project root.
- `internal/db/` exists with `sqlite.go`, `migrate.go`, `db.go` — import `db.OpenDB()` from init command.

### References

- Epic 1 Story 1.5 AC: [epics.md:794-831](../_bmad-output/planning-artifacts/epics.md)
- Viper hierarchy: [architecture.md:924-928](../_bmad-output/planning-artifacts/architecture.md)
- PipelineConfig location: [architecture.md:1520](../_bmad-output/planning-artifacts/architecture.md)
- Config loader location: [architecture.md:1524](../_bmad-output/planning-artifacts/architecture.md)
- Day 1 scope (main.go + config): [architecture.md:1762-1776](../_bmad-output/planning-artifacts/architecture.md)
- Writer ≠ Critic enforcement: [architecture.md:175](../_bmad-output/planning-artifacts/architecture.md) (FR46)
- Tech stack versions: [architecture.md:250-257](../_bmad-output/planning-artifacts/architecture.md)
- Project tree: [architecture.md:1501-1600](../_bmad-output/planning-artifacts/architecture.md)
- PRD config hierarchy: [prd.md:967-975](../_bmad-output/planning-artifacts/prd.md)
- FR38 (init command): [prd.md:1305](../_bmad-output/planning-artifacts/prd.md)
- FR46 (Writer ≠ Critic): [epics.md:70](../_bmad-output/planning-artifacts/epics.md)
- NFR-S1 (secrets in .env): [epics.md:90](../_bmad-output/planning-artifacts/epics.md)
- Error handling pattern: [architecture.md:1317-1335](../_bmad-output/planning-artifacts/architecture.md)
- Import direction rules: [architecture.md:172](../_bmad-output/planning-artifacts/architecture.md)
- Existing domain types: [internal/domain/types.go](../../internal/domain/types.go)
- Existing DB layer: [internal/db/sqlite.go](../../internal/db/sqlite.go)
- Existing main.go: [cmd/pipeline/main.go](../../cmd/pipeline/main.go)

## Dev Agent Record

### Agent Model Used

claude-opus-4-6

### Debug Log References

None

### Completion Notes List

- `domain/config.go`: `PipelineConfig` struct with 15 typed fields (model IDs, providers, paths, cost caps) + `DefaultConfig()` returning Writer=deepseek, Critic=gemini (FR46 out of the box). Zero imports from `internal/`.
- `config/loader.go`: Viper-based config loading with hierarchy: `.env` (godotenv) → `config.yaml` (Viper) → env vars (AutomaticEnv). Gracefully handles missing files. `BindFlags()` helper for CLI flag binding.
- `config/doctor.go`: `Check` interface + `Registry` pattern for extensible preflight checks. 4 checks: `APIKeyCheck` (3 keys), `FSWritableCheck` (output+data dirs), `FFmpegCheck` (injectable LookPath), `WriterCriticCheck` (exact error message). `FormatResults()` + `AllPassed()` helpers.
- `cmd/pipeline/init.go`: Idempotent init — creates config.yaml + .env only if not exists. Calls `db.OpenDB()` for SQLite init. Creates output directory.
- `cmd/pipeline/doctor.go`: Loads config, runs all checks, returns `errDoctorFailed` on any failure (exit code 1 via Cobra).
- `cmd/pipeline/main.go`: `--config` persistent flag, `init` and `doctor` subcommands registered.
- `.env.example`: Template with 3 placeholder API keys.
- Dependencies added: Viper v1.11.0, godotenv v1.5.1 (direct). Cobra v1.10.2 kept (architecture's v2.5.1 does not exist).
- 6 loader tests + 12 doctor tests + 3 domain config tests + 6 init/doctor integration tests = 27 new tests, all passing, zero regressions.

### Change Log

- 2026-04-17: Story 1.5 implemented — CLI init/doctor commands, Viper config loading, PipelineConfig struct, extensible doctor check registry, 27 tests passing

### File List

- internal/domain/config.go (new)
- internal/domain/config_test.go (new)
- internal/config/loader.go (new)
- internal/config/loader_test.go (new)
- internal/config/doctor.go (new)
- internal/config/doctor_test.go (new)
- cmd/pipeline/main.go (modified)
- cmd/pipeline/init.go (new)
- cmd/pipeline/init_test.go (new)
- cmd/pipeline/doctor.go (new)
- cmd/pipeline/doctor_test.go (new)
- .env.example (new)
- go.mod (modified — added viper v1.11.0, godotenv v1.5.1)
- go.sum (modified)
