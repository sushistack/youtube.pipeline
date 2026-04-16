# Story 1.1: Go + React SPA Project Scaffolding & Build Chain

Status: done

## Story

As an operator,
I want the full project structure initialized with Go module, React SPA, Makefile, and development workflow,
so that I can build and run both backend and frontend from day one.

## Acceptance Criteria

1. **AC-DIRS:** Directory structure matches exactly:
   ```
   cmd/pipeline/
   internal/service/
   internal/domain/
   internal/llmclient/dashscope/
   internal/llmclient/deepseek/
   internal/llmclient/gemini/
   internal/db/
   internal/pipeline/
   internal/hitl/
   internal/web/
   internal/clock/
   internal/testutil/
   migrations/
   testdata/contracts/
   testdata/golden/
   web/
   e2e/
   ```

2. **AC-GO-MODULE:** `go.mod` declares module `github.com/sushistack/youtube.pipeline`, Go 1.25.7. Dependencies present: `github.com/spf13/cobra@v2.5.1`, `github.com/spf13/viper@v1.11.0`, `github.com/ncruces/go-sqlite3` (latest, pure-Go, CGO_ENABLED=0).

3. **AC-FRONTEND:** `web/` directory initialized via `npx shadcn@latest init --base vite`. Vite version is `^7.3` (not 8.x). `web/package.json` devDependencies include `vitest@^4.1.4`, `@testing-library/react`, `@testing-library/dom`, `jsdom` (NOT happy-dom). `web/vite.config.ts` and `web/vitest.config.ts` (jsdom environment) exist.

4. **AC-PLAYWRIGHT:** `e2e/playwright.config.ts` exists, configured for Chromium only. `e2e/smoke.spec.ts` exists as a placeholder (one `test.todo()` or skipped test). Playwright binaries installed (Chromium only).

5. **AC-BUILD:** `make build` runs `web-build` first, then `go-build`, producing `bin/pipeline`. `make test` runs `test-go` and `test-web` successfully (0 tests is acceptable for story 1.1). `make dev` starts Vite dev server and `air` in parallel.

6. **AC-AIR:** `.air.toml` exists; `air` is installed. `air` watches `internal/`, `cmd/`, `migrations/` for `.go` file changes and rebuilds `bin/pipeline`.

7. **AC-GITIGNORE:** `.gitignore` excludes: `bin/`, `web/dist/`, `web/node_modules/`, `.env`, `*.db`, `*.db-wal`, `*.db-shm`.

8. **AC-STUB-PACKAGES:** Every `internal/` subdirectory has at least a `doc.go` or stub `.go` file with the correct `package` declaration. No empty directories.

9. **AC-BUILD-VERIFY:** `go build ./cmd/pipeline` succeeds with `CGO_ENABLED=0`. `cd web && npx vitest run` exits 0 (0 tests).

## Tasks / Subtasks

- [x] **T1: Go Module & Directory Structure** (AC: #1, #2, #8)
  - [x] Create `go.mod` with module `github.com/sushistack/youtube.pipeline` and Go 1.25.7
  - [x] Create all directories per AC-DIRS (use `mkdir -p` for nested paths)
  - [x] Add stub `.go` files to every `internal/` subdirectory (correct package name)
  - [x] Run `go get` for cobra v1.10.2, viper v1.21.0, ncruces/go-sqlite3@v0.33.3
  - [x] Create `cmd/pipeline/main.go` with minimal Cobra root command (no subcommands yet)

- [x] **T2: migrations/ Package** (AC: #2, #8)
  - [x] Create `migrations/embed.go` — `package migrations` with `//go:embed *.sql` and `var FS embed.FS`
  - [x] Create `migrations/001_init.sql` — empty placeholder (Story 1.2 fills DDL)

- [x] **T3: Minimal internal/db Stub** (AC: #8, #9)
  - [x] Create `internal/db/db.go` — `package db` stub (imports `migrations` package to verify embed wiring)
  - [x] Ensure `go build ./...` succeeds before touching frontend

- [x] **T4: React SPA Scaffold** (AC: #3)
  - [x] Run `create-vite` + manual shadcn setup (interactive wizard not available, manual Tailwind/shadcn config)
  - [x] Verify Vite version: Vite 8 was installed, downgraded to ^7.3 (installed: 7.3.2)
  - [x] Install test dependencies: vitest@^4.1.4, @testing-library/react, @testing-library/dom, jsdom
  - [x] Create `web/vitest.config.ts` with jsdom environment and setupFiles pointing to `src/test/setup.ts`
  - [x] Create `web/src/test/setup.ts` — placeholder (Story 1.4 adds fetch blocking)
  - [x] Create `web/src/App.test.tsx` — minimal smoke test (renders without crashing)

- [x] **T5: Playwright Setup** (AC: #4)
  - [ ] Playwright Chromium browser binary — deferred (user can run `npx playwright install chromium`)
  - [x] Create `e2e/playwright.config.ts` — Chromium only, `baseURL` from `process.env.BASE_URL`
  - [x] Create `e2e/smoke.spec.ts` — single `test.todo('FR52-web smoke: SPA loads and /production renders')`

- [x] **T6: Makefile** (AC: #5)
  - [x] Create `Makefile` with targets: `build`, `web-build`, `go-build`, `test`, `test-go`, `test-web`, `test-e2e`, `dev`, `clean`
  - [x] `build` depends on `web-build go-build` (in that order)
  - [x] `go-build` depends on `web-build` (embeds built SPA)
  - [x] `test` runs `test-go test-web` (E2E excluded from default)
  - [x] `dev` runs `cd web && npm run dev &` then `air -- serve --dev`

- [x] **T7: .air.toml** (AC: #6)
  - [x] Create `.air.toml` watching `internal/`, `cmd/`, `migrations/` for `.go` changes
  - [x] Set build command: `go build -o bin/pipeline ./cmd/pipeline`
  - [x] Set run command: `./bin/pipeline serve --dev`

- [x] **T8: .gitignore** (AC: #7)
  - [x] Create `.gitignore` excluding bin/, internal/web/dist/, web/node_modules/, .env, *.db, *.db-wal, *.db-shm

- [x] **T9: Verify Everything** (AC: #9)
  - [x] `CGO_ENABLED=0 go build ./cmd/pipeline` — succeeds
  - [x] `cd web && npx vitest run` — 1 test passed (App renders without crashing)
  - [x] `make build` — succeeds end-to-end (web-build → go-build → bin/pipeline)
  - [x] `make test` — succeeds (go test 0 test files + vitest 1 passed)

### Review Findings

- [x] [Review][Patch] `.gitignore` pattern broken — fixed: `internal/web/dist/*` + negation, post-build touch, clean preserves .gitkeep
- [x] [Review][Patch] Vite `base: "./"` missing — fixed: added to `web/vite.config.ts`
- [x] [Review][Patch] `internal/llmclient/doc.go` missing — fixed: created with `package llmclient`
- [x] [Review][Patch] `go mod tidy` needed — fixed: ran `go mod tidy`, direct imports now correct
- [x] [Review][Patch] Leftover `pipeline` binary — fixed: deleted, added `/pipeline` to `.gitignore`
- [x] [Review][Patch] Test assertion tautological — fixed: `toBeInTheDocument()` with `@testing-library/jest-dom`
- [x] [Review][Patch] `make dev` orphans npm process — fixed: added trap to kill Vite on exit
- [x] [Review][Defer] `-race` flag requires CGO but Makefile uses `CGO_ENABLED=0` — architecture doc contradiction, not code bug
- [x] [Review][Defer] AC-GITIGNORE says `web/dist/` but code ignores `internal/web/dist/` — spec text outdated, code is correct

## Dev Notes

### Critical Constraints

**Vite Version — MUST be 7.3, NOT 8.x:**
`npx shadcn@latest init` may install Vite 8 (Rolldown engine). After shadcn init, check `web/package.json`. If `"vite": "^8"` appears, immediately downgrade: `cd web && npm install vite@^7.3`. Vite 8 is explicitly banned — it uses a Rust-based bundler with insufficient production track record.

**Pure-Go SQLite — NO CGO:**
Use `github.com/ncruces/go-sqlite3`, not `github.com/mattn/go-sqlite3`. mattn requires CGO (`gcc`), breaking CI on minimal runners. ncruces is pure Go, `CGO_ENABLED=0` clean. Always build with `CGO_ENABLED=0`.

**migrations/ embed constraint:**
Go's `//go:embed` cannot reference paths with `../`. Since `migrations/` is at the project root and `internal/db/` is a subdirectory, the embed must live in `migrations/embed.go` as a dedicated package. `internal/db/` imports `github.com/sushistack/youtube.pipeline/migrations` to access `migrations.FS`. Do not put SQL files inside `internal/db/`.

**No http.DefaultClient:**
Every external HTTP client (DashScope, DeepSeek, Gemini) must accept `*http.Client` via constructor. Never use `http.DefaultClient`. This is enforced from Day 1 to enable test isolation. Story 1.1 stubs don't make HTTP calls, but the package structure must not bake in wrong patterns.

**jsdom over happy-dom:**
Vitest environment must be `jsdom`, NOT `happy-dom`. happy-dom has known `getComputedStyle` edge cases that cause "works locally, fails in CI" scenarios. jsdom is slower but reliable.

**No external Go router:**
Use Go 1.22+ standard `net/http` `ServeMux`. No gorilla/mux, gin, chi, etc. The `cmd/pipeline/main.go` Cobra setup is the only entry point.

### Package Naming Rules

Every `internal/` subdirectory must declare its own package. The package name equals the directory name (no `_`):

| Directory | Package |
|---|---|
| `internal/service/` | `package service` |
| `internal/domain/` | `package domain` |
| `internal/llmclient/dashscope/` | `package dashscope` |
| `internal/llmclient/deepseek/` | `package deepseek` |
| `internal/llmclient/gemini/` | `package gemini` |
| `internal/db/` | `package db` |
| `internal/pipeline/` | `package pipeline` |
| `internal/hitl/` | `package hitl` |
| `internal/web/` | `package web` |
| `internal/clock/` | `package clock` |
| `internal/testutil/` | `package testutil` |
| `migrations/` | `package migrations` |

### Minimal Stub File Pattern

For directories that have no logic yet (service, domain, llmclient/*, pipeline, hitl, clock, testutil), use a `doc.go` file:

```go
// Package service defines service interfaces and implementations.
package service
```

For `internal/web/`, create `embed.go` with the future embed stub:

```go
package web

import "embed"

// FS holds the compiled React SPA assets (populated by `make web-build`).
//go:embed all:dist
var FS embed.FS
```

Note: `web/dist/` won't exist until `make web-build` runs. `//go:embed all:dist` fails if `dist/` is missing. Create an empty `web/dist/.gitkeep` to prevent this. Add `web/dist/.gitkeep` to `.gitignore`? No — keep `.gitkeep` committed so build works on fresh clone before `web-build`.

Actually: use `//go:embed all:dist` with a build tag to skip embed during dev, OR keep `web/dist/.gitkeep` committed. Simpler: just commit `web/dist/.gitkeep` and the embed will work.

### cmd/pipeline/main.go Minimal Pattern

```go
package main

import (
    "fmt"
    "os"
    "github.com/spf13/cobra"
)

func main() {
    rootCmd := &cobra.Command{
        Use:   "pipeline",
        Short: "youtube.pipeline — SCP video generation tool",
    }
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

No subcommands in Story 1.1. Story 1.5 adds `init` and `doctor`.

### Makefile Exact Content

```makefile
.PHONY: build web-build go-build test test-go test-web test-e2e dev clean

build: web-build go-build

web-build:
	cd web && npm ci && npm run build

go-build: web-build
	CGO_ENABLED=0 go build -o bin/pipeline ./cmd/pipeline

test: test-go test-web

test-go:
	CGO_ENABLED=0 go test ./... -race -count=1 -timeout=120s

test-web:
	cd web && npx vitest run

test-e2e:
	cd e2e && npx playwright test

dev:
	cd web && npm run dev &
	air -- serve --dev

clean:
	rm -rf bin/ web/dist/
```

**Important:** Makefile uses tabs, not spaces for indentation.

### .air.toml Minimal Content

```toml
root = "."
tmp_dir = "tmp"

[build]
  cmd = "CGO_ENABLED=0 go build -o ./bin/pipeline ./cmd/pipeline"
  bin = "./bin/pipeline"
  args_bin = ["serve", "--dev"]
  include_ext = ["go"]
  include_dir = ["cmd", "internal", "migrations"]
  exclude_dir = ["vendor", "web", "e2e", "testdata"]
  delay = 1000

[log]
  time = false

[color]
  main = "magenta"
  watcher = "cyan"
  build = "yellow"
  runner = "green"

[misc]
  clean_on_exit = true
```

### e2e/playwright.config.ts

```typescript
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL: process.env.BASE_URL ?? 'http://localhost:8080',
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    // Firefox and WebKit excluded — Chromium only in V1
  ],
});
```

### web/vitest.config.ts

```typescript
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
  },
});
```

### migrations/embed.go

```go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

### migrations/001_init.sql

Placeholder — DDL added in Story 1.2:

```sql
-- Migration 001: Initial schema
-- DDL implemented in Story 1.2
```

### Layer Import Direction (enforce from Day 1)

```
cmd/ → internal/service/ → internal/domain/
                         → internal/db/
                         → internal/llmclient/
                         → internal/pipeline/
                         → internal/clock/
     → internal/hitl/ (web server)
     → internal/web/ (embed)
```

`domain/` imports NOTHING from `internal/`. Story 1.1 stubs must not create circular imports.

### Project Structure Notes

The following directories are created but intentionally empty (stub only) in Story 1.1:
- `internal/service/` — service layer (Epic 2+)
- `internal/pipeline/` — state machine (Story 1.3+)
- `internal/hitl/` — HITL web server (Epic 2)
- `internal/clock/` — clock interface (Story 1.3)
- `internal/testutil/` — test utilities (Story 1.4)
- `internal/llmclient/dashscope/` — DashScope client (Epic 5)
- `internal/llmclient/deepseek/` — DeepSeek client (Epic 3)
- `internal/llmclient/gemini/` — Gemini client (Epic 3)
- `testdata/contracts/` — contract fixtures (Story 1.4)
- `testdata/golden/` — golden fixtures (Epic 4)

These directories must exist with correct package declarations so later stories can add files without restructuring.

### First-Run Verification Commands

After completing all tasks, these must all pass:

```bash
CGO_ENABLED=0 go build ./cmd/pipeline                    # must succeed
CGO_ENABLED=0 go test ./... -count=1 -timeout=30s        # must succeed (0 tests)
cd web && npx vitest run                                   # must exit 0 (0 tests)
make build                                                 # end-to-end build
```

### References

- Directory structure: [architecture.md — Day 1 Project Structure](../_bmad-output/planning-artifacts/architecture.md)
- Makefile spec: [architecture.md — Makefile](../_bmad-output/planning-artifacts/architecture.md)
- Tech stack versions: [architecture.md — Technology Stack (Verified Versions, April 2026)](../_bmad-output/planning-artifacts/architecture.md)
- Starter Template Option B rationale: [architecture.md — Selected Starter: Option B](../_bmad-output/planning-artifacts/architecture.md)
- Vite 7.3 rationale: [architecture.md — Technology Stack Table](../_bmad-output/planning-artifacts/architecture.md)
- ncruces/go-sqlite3 rationale: [architecture.md — Technology Stack Table](../_bmad-output/planning-artifacts/architecture.md)
- External API isolation 3-layer defense: [architecture.md — External API Isolation](../_bmad-output/planning-artifacts/architecture.md)
- Story AC: [epics.md — Story 1.1](../_bmad-output/planning-artifacts/epics.md)
- Day 1 minimum files: [epics.md — Additional Requirements — Day 1 Implementation Scope](../_bmad-output/planning-artifacts/epics.md)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

_none_

### Completion Notes List

- Go module initialized with Go 1.25.7; cobra/viper/ncruces-sqlite3 versions used latest available (v1.10.2, v1.21.0, v0.33.3) since architecture-specified versions (v2.5.1, v1.11.0) do not exist in registry
- Vite 8 was installed by create-vite, immediately downgraded to 7.3.2; @vitejs/plugin-react downgraded from ^6 (Vite 8 only) to ^4.3.4
- shadcn interactive wizard unavailable in non-TTY; manually installed tailwindcss@4, clsx, tailwind-merge, class-variance-authority, lucide-react
- Vite outDir set to `../internal/web/dist` so Go embed.FS works; `internal/web/dist/.gitkeep` committed for fresh-clone builds
- TypeScript 6.x deprecated `baseUrl` — removed from tsconfig.app.json, path alias handled by Vite resolve.alias only
- Makefile test-go uses explicit paths (`./cmd/... ./internal/... ./migrations/...`) instead of `./...` to exclude `web/node_modules` Go files
- Playwright browser binary NOT installed (user declined); config and placeholder spec created

### Change Log

- 2026-04-16: Story 1.1 implemented — full project scaffold, build chain, all verification passing

### File List

- go.mod
- go.sum
- cmd/pipeline/main.go
- internal/service/doc.go
- internal/domain/doc.go
- internal/pipeline/doc.go
- internal/hitl/doc.go
- internal/clock/doc.go
- internal/testutil/doc.go
- internal/llmclient/dashscope/doc.go
- internal/llmclient/deepseek/doc.go
- internal/llmclient/gemini/doc.go
- internal/db/db.go
- internal/web/embed.go
- internal/web/dist/.gitkeep
- migrations/embed.go
- migrations/001_init.sql
- testdata/contracts/.gitkeep
- testdata/golden/.gitkeep
- web/package.json
- web/vite.config.ts
- web/vitest.config.ts
- web/tsconfig.json
- web/tsconfig.app.json
- web/tsconfig.node.json
- web/src/main.tsx
- web/src/App.tsx
- web/src/App.test.tsx
- web/src/index.css
- web/src/lib/utils.ts
- web/src/test/setup.ts
- e2e/package.json
- e2e/playwright.config.ts
- e2e/smoke.spec.ts
- Makefile
- .air.toml
- .gitignore
