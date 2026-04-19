# Story 6.4: Go Embed & Static File Serving

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want the web UI to be served directly from the Go binary,
so that I only need to manage a single file for deployment and execution.

## Prerequisites

**Hard dependency:** Story 1.1 established the dual-surface project layout (`web/` Vite app + Go server) and the canonical embed pipeline location under `internal/web/dist`.

**Practical dependencies:** Stories 6.1 and 6.2 define the frontend asset shape this story must serve, and Story 6.3 may add more frontend chunks or route-level assets that must remain compatible with the same serving path. Story 6.4 should harden the serving contract, not redesign the SPA itself.

- The current repository already contains the core serving foundation:
  - `internal/web/embed.go` embeds `all:dist`
  - `web/vite.config.ts` builds to `../internal/web/dist` with `base: './'`
  - `internal/api/spa.go` serves static files and falls back to `index.html`
  - `cmd/pipeline/serve.go` switches `--dev` mode to a Vite reverse proxy
  - `Makefile` wires `go-build: web-build`
- Because these pieces already exist, this story is primarily about validating, tightening, and testing the contract end-to-end rather than inventing a second asset-serving approach.
- Do not move the embed source back to a separate `web/dist/` tree. In this repository, the canonical compiled asset location is `internal/web/dist`, produced by the Vite project in `web/`.
- The current `web/src/App.tsx` is still a design-system placeholder, so route-refresh verification should be written against the serving contract itself and future SPA routes such as `/production`, `/tuning`, and `/settings`, not against already-completed route content.

## Acceptance Criteria

1. **AC-EMBEDDED-SPA-BUNDLE-CANONICAL-PATH:** production serving uses the repository's single canonical embed pipeline, with Vite output compiled into `internal/web/dist` and embedded into the Go binary via `embed.FS`.

   Required outcome:
   - `web/vite.config.ts` keeps `base: './'`
   - Vite build output remains `../internal/web/dist`
   - `internal/web/embed.go` remains the single embedded asset entrypoint used by `pipeline serve`
   - no duplicate static-asset trees such as an independently managed `web/dist` are introduced

   Rules:
   - prefer extending the existing `internal/web/embed.go` + `web/vite.config.ts` contract over adding a second embed package or manual copy script
   - the embed path must work with `net/http` static serving and hashed asset filenames emitted by Vite
   - keep asset URLs relative-path safe for embedded serving

   Tests:
   - build-oriented verification proves `make web-build` produces `internal/web/dist/index.html` plus hashed assets
   - Go-level verification proves the embedded filesystem contains `dist/index.html` at runtime

2. **AC-PRODUCTION-SPA-CATCH-ALL-NON-API-ONLY:** in production mode, `/api/*` remains owned by the API mux while every non-API path is handled by the embedded SPA with `index.html` fallback for client-side routes.

   Required outcome:
   - real files in the embedded FS are served directly
   - unknown non-API paths fall back to `index.html`
   - `/api/*` routes are never swallowed by the SPA fallback
   - refresh on future SPA routes such as `/production` does not return 404

   Rules:
   - preserve the existing path-safety behavior in `internal/api/spa.go` (`fs.Sub`, `path.Clean`, no directory traversal shortcuts)
   - do not special-case only the current placeholder `/`; the handler must support arbitrary future client-side routes
   - do not let the SPA catch-all intercept API errors or rewrite `/api/*` responses into HTML

   Tests:
   - handler test serves an existing asset unchanged
   - handler test falls back to `index.html` for `/production`
   - route-registration test proves `/api/...` still resolves through the middleware chain rather than the SPA fallback

3. **AC-DEV-MODE-VITE-PROXY-CONTRACT:** `pipeline serve --dev` uses the Go server for `/api/*` and proxies non-API frontend requests to the Vite dev server on `localhost:5173`, enabling frontend HMR without rebuilding the binary.

   Required outcome:
   - `/api/*` continues to be handled by the Go backend in dev mode
   - `/`, `/production`, and other non-API requests proxy to Vite
   - the CLI output clearly indicates that frontend requests are being proxied to the Vite dev server
   - the dev-mode behavior matches the architecture intent for local frontend iteration

   Rules:
   - do not invert the responsibility boundary by proxying API requests away from the Go server
   - keep the Vite dev target local-only (`localhost:5173`) unless the repo later introduces explicit configuration for this
   - preserve localhost-only server binding for the Go process

   Tests:
   - handler/integration test proves `/api/*` still uses Go handlers in `--dev` mode
   - proxy-focused test proves non-API traffic is forwarded to a stub upstream server

4. **AC-BUILD-PIPELINE-ORDERING-WITHOUT-DOUBLE-WORK:** the build workflow guarantees frontend assets are available before Go compilation while avoiding unnecessary duplicate frontend builds.

   Required outcome:
   - `go-build` still depends on a completed frontend build
   - the top-level build flow preserves `web-build` before Go compilation
   - the Makefile structure does not accidentally run `web-build` twice during the common `make build` path

   Rules:
   - treat the current `build: web-build go-build` plus `go-build: web-build` arrangement as something to review carefully because phony targets can trigger duplicate work
   - prefer the simplest Makefile expression that preserves ordering and clarity
   - avoid introducing `go:generate` or ad-hoc shell scripts for asset copying

   Tests:
   - document and verify the expected build command path for local development and CI
   - if the Makefile changes, confirm `make build` still produces a runnable binary with embedded assets

5. **AC-INTEGRATION-COVERAGE-FOR-SERVE-CONTRACT:** Story 6.4 adds durable automated coverage for the dev/prod serving split so Story 6.5 can trust the server contract.

   Required outcome:
   - Go tests cover embedded static file serving
   - Go tests cover SPA fallback behavior
   - Go tests cover dev-mode proxy routing boundaries
   - story completion requires both server behavior and build-pipeline assumptions to be validated, not just manually exercised

   Rules:
   - prefer `httptest` and local stub servers over real Vite process boot in unit/integration tests
   - keep tests deterministic and localhost-only
   - avoid relying on whatever content happens to be in the current placeholder app; test the routing contract, content type, and ownership boundaries instead

   Tests:
   - `go test` coverage for `internal/api/spa.go`
   - `go test` coverage for `cmd/pipeline/serve.go` routing behavior
   - targeted verification that the embedded asset tree exists after web build

## Tasks / Subtasks

- [x] **T1: Audit and normalize the existing embed pipeline** (AC: #1)
  - [x] Confirm `web/vite.config.ts` and `internal/web/embed.go` remain the single canonical production asset path.
  - [x] Remove or avoid any duplicate dist-path assumptions in code, comments, or build logic.
  - [x] Keep `base: './'` intact for relative embedded asset resolution.

- [x] **T2: Harden the SPA catch-all contract** (AC: #2)
  - [x] Review `internal/api/spa.go` for non-API catch-all behavior, existing-file passthrough, and traversal safety.
  - [x] Ensure refresh-friendly fallback behavior is written against future client routes, not only the current placeholder root page.
  - [x] Add or refine tests around existing asset serving and `index.html` fallback.

- [x] **T3: Clarify and test `--dev` routing ownership** (AC: #3)
  - [x] Keep Go as the owner of `/api/*` in dev mode.
  - [x] Keep Vite as the upstream only for non-API frontend requests.
  - [x] Add tests using a stub upstream server instead of a real Vite process.

- [x] **T4: Fix or confirm Makefile build sequencing** (AC: #4)
  - [x] Review whether `make build` currently triggers `web-build` more than once because both `build` and `go-build` depend on it.
  - [x] Simplify the dependency graph if needed while preserving CI/local clarity.
  - [x] Verify the resulting binary still embeds the latest frontend assets.

- [x] **T5: Add end-to-end serving guardrail tests for later Epic 6 work** (AC: #5)
  - [x] Add focused Go tests for `internal/api/spa.go`.
  - [x] Add focused Go tests for `cmd/pipeline/serve.go` dev/prod behavior.
  - [x] Run the relevant `go test` targets plus the frontend build path before closing the story.

### Review Findings

- [x] [Review][Patch] Add `web/dist/` to `.gitignore` to enforce AC-1 "no duplicate static-asset trees" at the VCS layer [.gitignore]
- [x] [Review][Patch] Use `path.Ext` instead of `filepath.Ext` for URL path extension check (idiomatic, OS-independent) [internal/api/spa.go:45]

## Dev Notes

### Story Intent and Scope Boundary

- Story 6.4 is the delivery contract between the Go server and the SPA build output. It should make the existing shell and future routes deployable as a single local binary.
- This story must not absorb unrelated frontend feature work from Stories 6.2, 6.3, or 6.5. Its job is serving, proxying, fallback behavior, and build-pipeline correctness.
- The acceptance criteria in Epic 6 talk about serving the embedded SPA and catch-all routing. In this repository, that contract is already partially implemented; the story should convert that partial implementation into a trusted, tested platform layer.

### Current Codebase Reality

- `cmd/pipeline/serve.go` already binds to `127.0.0.1`, registers API routes, and swaps the root handler to a reverse proxy when `--dev` is enabled.
- `internal/api/routes.go` already keeps `/api/*` on the API mux and registers the SPA catch-all only when `deps.WebFS` is present.
- `internal/api/spa.go` already uses `fs.Sub`, `path.Clean`, and direct file existence checks before falling back to `index.html`.
- `internal/web/embed.go` already embeds `all:dist`, which means the actual runtime lookup path is `dist/...` inside the embedded filesystem.
- `web/vite.config.ts` already sets `base: './'` and builds into `../internal/web/dist`.
- `Makefile` already declares `go-build: web-build`, but because both targets are phony and `build` also directly depends on `web-build`, the current shape should be reviewed for duplicate execution during `make build`.

### Architecture Guardrails

- The architecture explicitly requires Go `net/http` ServeMux plus an SPA catch-all for all non-API paths so browser refresh on `/production` works.
- The architecture explicitly requires `pipeline serve --dev` to proxy to the Vite dev server instead of serving from `embed.FS`, preserving HMR during frontend development.
- The repository architecture already chose `internal/web/dist` as the embedded output directory. Follow that path exactly instead of translating the requirement back into a generic `web/dist` convention.
- Keep the serving layer dependency-light. No third-party Go router or static-file helper is needed here.

### UX and Product Requirements to Preserve

- PRD requires the web UI to ship as a single-page application served by the same Go binary so deployment remains a single-file workflow for the operator.
- The UI remains localhost-only and desktop-only. This story should preserve `127.0.0.1` binding and should not introduce network-facing server behavior.
- Future top-level client routes are `/production`, `/tuning`, and `/settings`; this story's fallback behavior must support refresh and deep-link entry for those paths once the shell lands.

### Implementation Guidance

- Prefer tightening the current `spaHandler` rather than replacing it; it already follows the right general pattern.
- Add tests before making risky behavior changes so the current routing contract is captured explicitly.
- If dev-mode proxy behavior needs refactoring for testability, extract the proxy construction or mux wiring into small helper functions rather than bloating `runServe`.
- Keep comments and command output precise about the ownership split:
  - Go backend owns `/api/*`
  - Vite dev server owns non-API frontend assets/routes in `--dev`
- If the Makefile is adjusted, preserve the mental model that Go compilation happens only after frontend assets are ready.

### Testing Requirements

- Add direct Go tests for SPA fallback behavior because the current repository has no dedicated `internal/api/spa_test.go`.
- Prefer `httptest.NewServer` and a stub reverse-proxy upstream for dev-mode verification.
- Verify production-mode behavior against embedded files or a minimal filesystem fixture so route fallback is deterministic.
- Before story closure, the relevant verification should include:
  - `go test` for the touched server/api packages
  - frontend build generation of `internal/web/dist`
  - runnable production binary path through `go-build` or `make build`

### Previous Story Intelligence

- Story 6.1 already treated the `internal/web/dist` embed flow and Vite `base: './'` setting as existing constraints that later Epic 6 work must not break.
- Story 6.2 explicitly scoped Go embed serving out of the shell story, which means Story 6.4 should now own the server-side contract cleanly instead of letting shell code grow ad hoc serving assumptions.
- Story 6.3 noted that the current frontend app is still mostly scaffold-level. Do not confuse "placeholder SPA content" with "serving contract complete"; this story is about the transport/build layer, not route richness.

### Latest Technical Note

- Go's official `embed` docs still describe `embed.FS` as implementing `io/fs.FS`, which keeps it compatible with `net/http` file serving and makes the current `embed.FS -> fs.Sub -> http.FileServer` approach the right baseline for this story.
- Vite's current production-build docs still support `base: './'` when the deployment base is not known ahead of time, which matches this embedded-SPA use case.
- Vite's backend-integration docs still describe development setups in terms of a backend serving HTML while proxying frontend asset requests to the Vite server. For this repository, the closest fit is the existing `pipeline serve --dev` split where Go keeps `/api/*` and proxies non-API frontend requests to Vite. This is an implementation inference from the official docs, not a literal one-size-fits-all recipe.

Official references:
- Go `embed` package: https://pkg.go.dev/embed
- Vite build guide: https://vite.dev/guide/build
- Vite backend integration guide: https://vite.dev/guide/backend-integration.html

### Project Structure Notes

- Existing files most likely to touch:
  - `cmd/pipeline/serve.go`
  - `internal/api/routes.go`
  - `internal/api/spa.go`
  - `internal/web/embed.go`
  - `web/vite.config.ts`
  - `Makefile`
- Expected new tests:
  - `internal/api/spa_test.go`
  - optionally `cmd/pipeline/serve_test.go` or another focused server-routing test file

### References

- Epic 6 story definition: [_bmad-output/planning-artifacts/epics.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/epics.md)
- PRD web UI surface and `pipeline serve`: [_bmad-output/planning-artifacts/prd.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/prd.md)
- Architecture API routing and dev workflow: [_bmad-output/planning-artifacts/architecture.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/architecture.md)
- UX embed + SPA routing note: [_bmad-output/planning-artifacts/ux-design-specification.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/ux-design-specification.md)
- Current serving implementation: [cmd/pipeline/serve.go](/home/jay/projects/youtube.pipeline/cmd/pipeline/serve.go), [internal/api/routes.go](/home/jay/projects/youtube.pipeline/internal/api/routes.go), [internal/api/spa.go](/home/jay/projects/youtube.pipeline/internal/api/spa.go), [internal/web/embed.go](/home/jay/projects/youtube.pipeline/internal/web/embed.go), [web/vite.config.ts](/home/jay/projects/youtube.pipeline/web/vite.config.ts), [Makefile](/home/jay/projects/youtube.pipeline/Makefile)

## Story Completion Status

- Story file created: `_bmad-output/implementation-artifacts/6-4-go-embed-static-file-serving.md`
- Story status set to `review`
- Sprint status now reflects this story as `review`
- Completion note: Embedded SPA serving, dev proxy ownership, and build-order guardrails are now implemented and covered by automated tests

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- Story execution workflow review on 2026-04-19
- Serving-contract inspection: `cmd/pipeline/serve.go`, `internal/api/routes.go`, `internal/api/spa.go`, `internal/web/embed.go`, `web/vite.config.ts`, `Makefile`
- Verification commands: `go test ./internal/api ./internal/web ./cmd/pipeline -count=1`, `npm run build`, `make build`

### Completion Notes List

- Kept `internal/web/dist` as the single canonical Vite output and embed source, with `internal/web/embed.go` remaining the sole Go embed entrypoint
- Hardened SPA serving so real embedded files pass through unchanged, route-like paths fall back to `index.html`, and missing asset-like paths stay 404s
- Refactored `cmd/pipeline/serve.go` dev frontend wiring into testable helpers and clarified CLI output that Go owns `/api/*` while Vite owns non-API frontend requests in `--dev`
- Simplified `Makefile` build ordering so `make build` relies on `go-build`'s `web-build` dependency instead of declaring `web-build` twice
- Added focused tests for SPA serving, API-vs-SPA route ownership, embedded asset presence, and dev/prod mux behavior
- Resolved narrow TypeScript typing issues in the existing keyboard-shortcut frontend work so `npm run build` and `make build` complete successfully with the updated embedded assets

### File List

- cmd/pipeline/serve.go
- cmd/pipeline/serve_test.go
- internal/api/spa.go
- internal/api/spa_test.go
- internal/api/routes_test.go
- internal/web/embed_test.go
- Makefile
- web/src/components/shared/ProductionShortcutPanel.tsx
- web/src/hooks/useKeyboardShortcuts.test.tsx
- _bmad-output/implementation-artifacts/sprint-status.yaml
- _bmad-output/implementation-artifacts/6-4-go-embed-static-file-serving.md

### Change Log

- 2026-04-19: Added automated coverage for embedded SPA serving, client-route fallback, missing-asset handling, and `/api/*` ownership boundaries.
- 2026-04-19: Refactored dev-mode frontend proxy wiring in `cmd/pipeline/serve.go` for clearer ownership messaging and direct mux-level tests.
- 2026-04-19: Simplified `make build` dependency ordering and verified `npm run build` plus `make build` produce fresh embedded assets and a runnable Go binary.
- 2026-04-19: Code review — added `web/dist/` to `.gitignore` (AC-1 defense-in-depth) and switched SPA extension check from `filepath.Ext` to `path.Ext`; verified with `go test ./cmd/pipeline ./internal/api ./internal/web -count=1`.
