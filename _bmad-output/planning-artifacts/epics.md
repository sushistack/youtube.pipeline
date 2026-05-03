---
stepsCompleted:
  - step-01-validate-prerequisites
  - step-02-design-epics
  - step-03-create-stories
  - step-04-final-validation
inputDocuments:
  - _bmad-output/planning-artifacts/prd.md
  - _bmad-output/planning-artifacts/architecture.md
  - _bmad-output/planning-artifacts/ux-design-specification.md
---

# youtube.pipeline - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for youtube.pipeline, decomposing the requirements from the PRD, UX Design, and Architecture requirements into implementable stories.

## Requirements Inventory

### Functional Requirements

FR1: Operator can start a new pipeline run for a given SCP ID.
FR2: Operator can resume a failed run from the last successful stage.
FR3: Operator can cancel an in-flight run.
FR4: Operator can inspect the state of any individual run or of all runs.
FR5: System persists complete run state for every run, including stage progression, status, retry count, and retry reason.
FR6: System captures per-stage observability data — duration, token in/out, retry count, retry reason, critic score, cost, and human-override flag — for every stage of every run.
FR7: *Moved to NFR-C1* (per-stage cost budget cap is a threshold constraint, not a capability). Number intentionally preserved for traceability; no active FR at this index.
FR8: System stops a retry loop and escalates to the operator when the cosine similarity of consecutive retry outputs exceeds a configured threshold (default 0.92); the threshold is operator-configurable.
FR9: System generates research output for an SCP ID by drawing from the local data corpus.
FR10: System produces a structured scene plan, Korean narration text, per-scene shot breakdown (1–5 shots per scene with visual descriptors, estimated durations, and transition types), and review report by composing six distinct LLM agents in sequence (Researcher, Structurer, Writer, VisualBreakdowner, Reviewer, Critic). Shot count is determined automatically from TTS duration estimates (≤8s→1, 8–15s→2, 15–25s→3, 25–40s→4, 40s+→5); operator can override at scenario_review HITL checkpoint.
FR11: System validates each agent's output against a defined schema before passing it to the next agent.
FR12: System enforces that the Writer LLM provider and the Critic LLM provider are different, both at preflight and at run entry.
FR13: System invokes the Critic agent at two checkpoints: immediately after the Writer, and after Phase A overall completion (Reviewer fact-check pass).
FR14: System generates per-shot images (1–5 shots per scene, ~30 images per run), preserving cross-scene and cross-shot character continuity through a frozen visual descriptor reused verbatim across all image prompts. Each shot's image prompt is derived from the VisualBreakdowner's shot-level visual descriptor.
FR15: System generates per-scene Korean TTS audio, with numerals and English terms transliterated to Korean orthography prior to synthesis.
FR16: System executes the image and TTS tracks concurrently via errgroup.Group (NOT errgroup.WithContext — one track's failure must not cancel the other); both tracks complete before assembly begins. Image track volume is ~30 images per run (~3× single-image model). Wall-clock time captured as operational metric.
FR17: For SCPs with a named character, System surfaces a character-reference selection prerequisite to the operator (10 candidate images from cached image search) before image generation proceeds for that character.
FR18: System generates the canonical character reference image from a selected source through reference-based image editing.
FR19: System caches character-search results aggressively to avoid repeated external lookups across runs.
FR20: System assembles video in two stages: (1) per-scene clip assembly — each scene's shot images (1–5) composed with transitions (Ken Burns pan/zoom, cross-dissolve, or hard cut), timed to shot durations, overlaid with TTS audio to produce a scene clip; (2) final concatenation of all scene clips into the output MP4. V1 includes 3 transition types; audio fade and BGM are V1.5.
FR21: System produces, alongside each video, a metadata bundle declaring AI-generated content (narration, imagery), the models used, and source attribution.
FR22: System produces, alongside each video, a source manifest including the originating SCP article and any sub-work license chain it references.
FR23: System gates the "ready-for-upload" status of any video on explicit operator acknowledgment of its metadata bundle.
FR24: System produces a verdict (pass / retry / accept-with-notes) from the Critic for each evaluated output, based on a rubric of weighted sub-scores (hook strength, fact accuracy, emotional variation, immersion).
FR25: System runs rule-based pre-checks (schema validation, forbidden-term regex) before any LLM Critic invocation; the forbidden-term list is an operator-authorable, version-controlled artifact (add / remove / revert).
FR26: Operator can author, version, and maintain a Golden eval set of fixtures, with a positive-to-negative ratio enforced at 1:1; System emits a staleness warning when the set has not been re-validated within N days (configurable, default 30) or when the Critic prompt has changed since last validation.
FR27: System runs the Critic against the Golden eval set on demand and reports detection rate.
FR28: System runs the Critic in shadow mode against the N most recently passed scenes (default N=10) when a Critic prompt change is proposed; reports any false-rejection regressions.
FR29: System tracks a calibration metric between Critic verdicts and operator override decisions over a rolling 25-run window, with a "provisional" indicator while n<25. (Implemented as Cohen's kappa in V1.)
FR30: System flags scene content depicting minors in policy-sensitive contexts and blocks downstream processing until the operator reviews. **V1 detection mechanism**: keyword/regex pre-check (operator-authorable list, version-controlled) PLUS LLM Critic sub-check (dedicated rubric category). Golden eval fixtures must include this failure category alongside fact-error / descriptor-violation / weak-hook (per NFR-T5).
FR31a: System supports auto-approval of review items whose Critic score meets a configured threshold; auto-approved items are recorded with a distinguishing decision type.
FR31b: Operator can batch-review non-auto-approved scenes through composed per-scene cards (scene clip mini-video or shot thumbnail strip + narration text + audio playback).
FR31c: Operator can precision-review high-leverage scenes (character first appearance, hook scene, act-boundary scenes) through an extended-detail surface showing individual shots, transitions, and per-shot image quality.
FR32: Operator can approve, reject, or edit any item presented in any review surface.
FR33: Operator can undo the most recent decision at any review surface.
FR34: Operator can approve all remaining items in the current batch with a single action.
FR35: Operator can mark a decision as "skip and remember" so the pattern is recorded for future learning.
FR36: System records every operator HITL action — target item, decision type, timestamp, optional note — to a persistent decisions store.
FR37: Operator can browse the full decisions history in a read-only view (date / mode / target / decision / note).
FR38: Operator can initialize a fresh project: configuration files, database schema, output directory layout, and an .env template listing required secrets.
FR39: Operator can run a preflight check that validates secret presence, filesystem path writability, and required system binary availability through an extensible registry.
FR40: Operator can launch the local web UI server, which binds to localhost only.
FR41: System surfaces operator mode (Production, Tuning, Settings & History) as the top-level web UI organization.
FR42: Operator can request machine-readable JSON output for any CLI command, wrapped in a versioned envelope.
FR43: Operator can request human-readable, color-coded, hierarchical status output for any CLI command (default).
FR44: Operator can export per-run decisions or other artifact data to JSON files on demand.
FR45: System records the LLM provider that served each generation, persistently, to support retroactive ToS audit.
FR46: System rejects any pipeline configuration where the Writer and Critic providers are identical, both at preflight and at run entry.
FR47: System rejects any voice profile whose identifier is present in an operator-maintained "blocked voice-ID" list; the list is the compliance-policy interface for preventing identifiable-real-person voice generation.
FR48: System enforces forbidden-term lists (KCSC-derived) at the narration generation stage so generated content can be steered away from age-restricted territory.
FR49: Operator can replay a paused HITL session from the exact decision point at which it was paused, with a state-aware summary of where the run left off.
FR50: Operator can view a "what changed since I paused" summary for any run, produced as a diff between the latest state and the state at the operator's most recent interaction timestamp.
FR51: System provides a test-infrastructure capability as a first-class artifact: contract test suite (CLI <-> web parity, with single-source-of-truth contract schema as JSON Schema files in `testdata/contracts/`, validated by both Go unmarshal and Zod parse — `testdata/contracts/` is NEVER auto-updated), integration test harness (stage-boundary schema verification), Golden / Shadow eval runner, and a seed-able run-state test fixture facility. All execute in CI.
FR52: System provides two end-to-end smoke tests, both of which must pass before any deployment / release: (a) **FR52-go**: Go-side full-pipeline E2E test (`go test -run E2E`) exercising Phase A -> B -> C on a single canonical seed input (one SCP ID) with mocked external APIs; (b) **FR52-web**: Playwright smoke test (Chromium only, exactly 1 spec in V1) verifying the SPA loads, the `/production` route renders, and basic interaction works.
FR53: When presenting a scene to the operator for review, System surfaces any prior rejection of a semantically similar scene, with the earlier rejection's reason and target; the operator can act on the warning or dismiss it. **V1 similarity definition**: same SCP ID + same scene_index. **V1.5**: embedding-based cosine similarity (consistent with FR8 threshold mechanism).

### NonFunctional Requirements

NFR-P3: Pipeline stages must back off on API rate-limit responses (HTTP 429) without advancing stage status, and must emit retry_reason="rate_limit" in the observability record.
NFR-P4: First-video end-to-end wall-clock time is captured in pipeline_runs throughout V1; the target threshold is formalized within 30 days of V1.5 entry, using the first five videos' distribution data.
NFR-C1: Every pipeline stage has a configured maximum cost in USD; when a stage's accumulated cost_usd exceeds the cap, the stage hard-stops and escalates to the operator.
NFR-C2: Every full run has a configured maximum total cost in USD; exceeding it hard-stops the run.
NFR-C3: Cost data (cost_usd, token_in, token_out) is captured per stage in pipeline_runs with no sampling or truncation.
NFR-R1: Stage-level resume produces the same downstream schema, the same stage-status progression, a non-null required metadata bundle, and a validating source manifest, given the same inputs. Bit-equivalence on LLM outputs is not required.
NFR-R2: Anti-progress detector false-positive rate is measured throughout V1 on a rolling 50-run window; the <=5% target gate is applied from V1.5 onward. V1 ships with the default cosine threshold of 0.92 and a tunable configuration field.
NFR-R3: Database writes to pipeline_runs, decisions, and Golden eval fixtures are durable across process restarts and crashes mid-stage.
NFR-R4: Web UI client-side state is transient; canonical state lives in SQLite. Closing and reopening the web UI reconstructs the operator's view from the database without data loss.
NFR-S1: API keys and other secrets are read from a local .env file in V1 (OS keyring is V1.5); secrets are never committed to version control.
NFR-S2: The local web UI server binds exclusively to 127.0.0.1; external network access is blocked at the bind interface.
NFR-S3: The SQLite database file is created with operator-only read/write permissions (mode 0600 on POSIX or equivalent on Windows).
NFR-S4: V1 assumes localhost-only communication with no authentication and no TLS. Any change that exposes the product to a network surface requires a recorded Architecture Decision Record (ADR) prior to implementation.
NFR-T1: The CI pipeline executes, on every commit: unit tests, integration tests (stage-boundary schema validation), contract tests (CLI <-> web parity), Golden eval (detection rate >= 80%), Shadow eval (zero false-reject regressions on recent passed scenes), the E2E smoke tests (FR52-go + FR52-web), **and the layer-import linter (NFR-M4) gate**.
NFR-T2: Unit-test line coverage is enforced as a hard gate at >= 60% from V1, >= 70% from V1.5. Soft gates are not used. **Scope: all `internal/` packages except `internal/testutil/` and generated code; `web/` coverage is measured separately by Vitest.**
NFR-T3: Every FR has at least one acceptance test or an explicit "not-directly-testable, covered by X" annotation. The proportion of annotated FRs is capped at <= 15% of total FRs. **Mapping artifact: `testdata/fr-coverage.json` (schema: { fr_id, test_ids[], annotation? }); CI script validates that every FR appears, referenced test IDs resolve to existing `Test*` functions, and annotated FR count <= 15% of total.**
NFR-T4: Seed-able run-state fixtures (SQLite snapshots for arbitrary run states) are stored with the repository and version-controlled alongside code.
NFR-T5: The Golden / Shadow evaluation runner itself is tested as a first-class artifact: at least three canonical known-pass and three known-fail fixtures **(covering distinct failure categories: fact error, descriptor violation, weak hook — plus the minors-in-policy-sensitive-contexts category per FR30)** included in CI. A failure here blocks merges on the Critic prompt or evaluation-runner code.
NFR-T6: CI total wall-clock execution time is a hard gate at <= 10 minutes.
NFR-O1: pipeline_runs retains all eight specified columns for every stage of every run; no truncation, no sampling.
NFR-O2: Retention of pipeline_runs and decisions rows is indefinite in V1 (no purge mechanism).
NFR-O3: Diagnostic queries against pipeline_runs through the standard SQLite CLI are sufficient for operational diagnosis.
NFR-O4: The database migration set includes the SQL indexes required for rolling-window metric queries to complete without full-table scans.
NFR-M1: Model identifiers (LLM models, TTS voice profiles, image generation models) are configuration-driven. No model ID is hard-coded in source code.
NFR-M2: Authorable artifacts — forbidden-term lists, Golden eval fixtures, voice-ID blocklist, pipeline config — live in version-controlled files separate from source code.
NFR-M3: Stage-boundary schemas are formally defined (JSON Schema or Go struct tags with validation) and referenced in stage validation (FR11).
NFR-M4: Layer boundaries (web -> cmd -> service -> infra) are enforced at CI time by a layer-import linter such as `go-cleanarch` or `depguard`; **Go's native import-cycle check is a necessary but insufficient mechanism**. A linter violation blocks merge.
NFR-A1: Keyboard navigation for HITL review surfaces is required, covering the 8-key shortcut set (Enter, Esc, Ctrl+Z, Shift+Enter, Tab, S, 1-9, J/K).
NFR-A2: Broad WCAG 2.x compliance (screen reader support, color-contrast auditing, ARIA attribution) is explicitly out of scope for V1.
NFR-A3: Mobile and tablet breakpoints are out of scope; HITL review is desktop-only.
NFR-L1: Every video produced by the pipeline must have an associated metadata bundle and source manifest before being marked "ready-for-upload". There is no bypass path.
NFR-L2: Compliance artifacts (metadata bundles, source manifests) are version-controlled alongside video outputs in the same output directory tree.
NFR-L3: The provider field recording which LLM served each generation is non-null for every row in pipeline_runs that represents an LLM call.
NFR-L4: When a YouTube / platform policy change alters required disclosure fields, only the metadata bundle generator changes; pipeline behavior is unaffected.

### Additional Requirements

- **Starter Template: Manual Go + shadcn Vite Template (Option B)**. Go project hand-scaffolded; frontend uses `npx shadcn@latest init --base vite`. Project initialization is explicitly the first implementation story.
- Go 1.25.7, Cobra v2.5.1, Viper v1.11.0 tech stack pinned
- ncruces/go-sqlite3 (pure Go, no CGO) for SQLite driver
- Vite 7.3 (not 8.x), React 19.x, shadcn/ui CLI v4, Tailwind CSS 4.x (dark-only)
- Vitest ^4.1.4, React Testing Library, jsdom, Playwright (Chromium only, 1 smoke test in V1)
- Go testing: stdlib only (no testify); custom `assertEqual[T]` generic helper
- TanStack Query for server state, Zustand with localStorage persist for UI state
- React Router v7 (BrowserRouter + Routes), MSW for frontend API mocking
- SQLite WAL mode + busy_timeout=5000 enforced at connection open
- Manual embedded SQL migration runner (~50 LoC) using embed.FS + PRAGMA user_version; no external migration tool
- V1 schema: 3 tables (runs, decisions, segments) with exact DDL specified
- Phase B resume semantics: DELETE all existing segments + re-insert (clean slate)
- Go 1.22+ standard net/http ServeMux (no external router); SPA catch-all handler required
- **17 REST endpoints across 5 groups** (Pipeline Lifecycle, Scene Review, Character Reference, History/Metrics, Compliance) with mandatory response envelope `{"version": 1, "data": {...}}` or `{"version": 1, "error": {...}}`. Implementation consideration: prune to <= 12 where redundancy is found; endpoint additions follow the New Endpoint Checklist (route registration in `internal/api/routes.go`, domain type, service method, handler, contract fixture in `testdata/contracts/`, Go+Zod contract test, error mapping via `errors.Is()`).
- Error classification: 7 categories — **RATE_LIMITED**, **UPSTREAM_TIMEOUT**, **STAGE_FAILED**, **VALIDATION_ERROR**, **CONFLICT**, **COST_CAP_EXCEEDED**, **NOT_FOUND** — each with HTTP status code and retryable flag (429/502/503/504/context.DeadlineExceeded retryable; 400/401/403/404 fatal).
- Polling: 5s interval on `/api/runs/{id}/status`
- snake_case for ALL JSON fields, DB columns, query params
- Run ID format: `scp-{scp_id}-run-{sequential_number}` (human-readable)
- State machine: Go switch + enum (~100 LoC), 15 stages from pending to complete
- 4 HITL wait points: scenario_review, character_pick, batch_review, metadata_ack
- State transition as pure function: NextStage(current, event) (Stage, error)
- Agent chain: AgentFunc for Phase A; errgroup.Group for Phase B parallelism (NOT errgroup.WithContext)
- Inter-agent schema validation at every handoff; inter-phase data flow: within Phase A = in-memory, between phases = file-based
- Agent purity rule: agents are pure functions (no state mutation, no DB, no HTTP)
- Provider interfaces: TextGenerator, ImageGenerator (Generate + Edit), TTSSynthesizer
- All implementations accept *http.Client via constructor (no http.DefaultClient)
- Vendor implementations in internal/llmclient/{dashscope,deepseek,gemini}/
- Rate limiting: dual layer — `golang.org/x/sync/semaphore` (concurrency) + `golang.org/x/time/rate` token bucket (RPM). **Critical constraint**: Phase B DashScope image and TTS tracks share the same pair of limiter instances (same vendor, same rate budget) — the two parallel tracks compete for budget. LLM providers each get separate limiter instances. Rate-limit coordination is a Tier 1 cross-cutting concern and must be designed before Phase B implementation begins.
- Retry: exponential backoff with clock interface, max 30s delay, jitter
- V1 undo scope: exactly 5 action types — (1) scene approve, (2) scene reject, (3) scene skip, (4) batch "approve all remaining" (undoes entire batch as one action), (5) Vision Descriptor text edit. Out of scope for V1 undo: pipeline stage transitions (resume/cancel/create), configuration changes. Undo stack depth >= 10 actions; mechanics: reversal row in `decisions` table with `superseded_by` FK; undo available until scene enters Phase C rendering; undo includes focus restoration. Full 10-type undo is V1.5.
- GitHub Actions CI: 4 jobs (test-go, test-web parallel, then test-e2e + build); <= 10 min total
- External API isolation: 3-layer defense (constructor injection, blocking transport, no CI keys)
- Configuration (project-root layout): ./.env (secrets, gitignored) + ./config.yaml (model IDs, paths, cost caps; git-tracked) + CLI flags
- `pipeline serve --dev` proxies to Vite dev server; cosmtrek/air for Go hot-reload
- Per-run directory tree: ./output/{run-id}/ with defined subdirectories
- Filesystem state and DB state consistency verified at resume entry
- slog (stdlib) via constructor injection; no external logging library
- Domain sentinel errors in internal/domain/errors.go
- Clock abstraction: internal/clock/ package with Clock interface (RealClock, FakeClock)
- **Implementation sequence (12 steps, epic/story ordering is derived from this)**:
  1. SQLite schema + migration runner + DB open (WAL + busy_timeout)
  2. Domain types + LLM provider interfaces (`TextGenerator`, `ImageGenerator`, `TTSSynthesizer`)
  3. State machine (pure function `NextStage`) + `runs` table CRUD
  4. Phase A agent chain (6 agents with mocked LLM providers)
  5. Phase B parallel tracks (per-shot image generation ~30/run + TTS, shared DashScope limiter, mocked APIs)
  6. Phase C FFmpeg two-stage assembly (per-scene clips with Ken Burns/cross-dissolve/hard-cut + TTS overlay, then final concat)
  7. REST API endpoints + JSON envelope + error mapping
  8. React SPA shell + TanStack Query hooks + Zustand store
  9. HITL review surface (Master-Detail, keyboard shortcuts)
  10. Command Pattern undo + `decisions` table
  11. Contract tests + CI pipeline (Go+Zod schema SSoT)
  12. End-to-end integration (FR52-go + FR52-web)
- **Day 1 Implementation Scope (first story acceptance criteria)**: minimum 10 files to get `go test ./... && go build ./cmd/pipeline` passing — `go.mod`, `cmd/pipeline/main.go`, `internal/domain/types.go` (stub), `internal/db/open.go`, `internal/db/migrate.go`, `internal/db/migrations/0001_init.sql`, `Makefile`, `web/package.json`, `web/vite.config.ts`, `.gitignore`. First green CI run is the DoD.
- domain/ 300-line file cap; handler 400-line file cap; split by concept when approaching limits
- Import direction enforced in CI: api -> service -> domain/db/llmclient/pipeline/clock; `domain/` imports nothing from `internal/`
- **Interface Definition Rule**: interfaces defined in consuming package (e.g., `service/` defines `RunStore` interface, `db/` implements it). Exception: `domain/` defines capability interfaces (`TextGenerator`, `ImageGenerator`, `TTSSynthesizer`) that are consumed across layers.
- **Circular Dependency Prevention**: for `engine ↔ run_service` tension, define `pipeline/runner.go` with Runner interface; engine depends on `db/` concretely; service depends on `Runner` interface only. Co-located test files everywhere.
- Makefile-based build chain: `web-build` -> `go-build` -> `test`; E2E excluded from default test target
- Middleware chain (4 middlewares composed via `Chain()`): `WithRequestID`, `WithRecover`, `WithCORS`, `WithRequestLog`.
- SPA catch-all handler serves `index.html` for non-`/api/*` paths (embed.FS).
- Optimistic UI scope: **only** HITL review actions (approve, reject, skip, batch-approve, undo) use optimistic mutations; pipeline state transitions (resume, cancel, create) use standard mutations with pending state visible.
- Review Mode List Stability: during batch review, scene list is snapshot-frozen; polling updates are queued and surfaced as a badge ("N updates available — press R to refresh") via `useStableList` hook. Prevents reorder/re-render disruption mid-review.

### Architecture Priority Weighting (epic-ordering input)

Nine domains tiered by investment depth for V1:

- **First-class** (full design, comprehensive tests, exhaustive ACs): Pipeline Lifecycle, Phase A Scenario Generation, HITL Review Surface, Test Infrastructure
- **Solid** (complete implementation, standard test coverage): Phase B Media Generation, Quality Gating (Critic Stack), Operator Tooling, Observability
- **Minimum viable** (simplest shipping implementation, boundary tests only): Phase C Assembly, Compliance & Risk Surface

Epic ordering should front-load first-class domains and Tier-1 cross-cutting concerns; minimum-viable domains stage last.

### Cross-Cutting Concerns (3-tier, design-before-use)

- **Tier 1 — Architecture Skeleton (must be designed before any domain implementation begins)**: LLM provider abstraction (`domain.TextGenerator` etc.), external API mock boundary (3-layer defense), inter-agent schema contracts (stage-boundary JSON Schema), rate-limit coordination (shared DashScope limiter for Phase B).
- **Tier 2 — Important, Incrementally Applicable**: error classification taxonomy, response envelope, clock abstraction, cost tracking / circuit breaker, slog structured logging, domain sentinel errors, test utilities (`testutil/`).
- **Tier 3 — V1 Minimal (apply lightly, expand in V1.5)**: SSE/WebSocket upgrade (deferred), per-scene resume granularity (stage-level only in V1), graphical metrics dashboard (deferred), Command Palette / Ctrl+K (deferred).

### Cross-Component Dependency Chain (epic dependency order)

`domain/types` ← everything consumes → `db/sqlite` ← `pipeline/engine`, `service/*` → `pipeline/engine` ← `cmd/*` → `service/*` ← `web/api` → `web/src/*`.

Practical implication: no domain-level story can start before `domain/types` + `db/sqlite` stories are complete; no frontend story can start before `web/api` contract is established.

### UX Design Requirements

UX-DR1: Implement dark-only CSS custom property color token system in styles/tokens.css with 16 semantic tokens: background (#0F1117), bg-subtle (#13151D), bg-raised (#18181B), bg-input (#1E1E24), border-subtle (#222228), border (#27272A), border-active (#3F3F46), foreground (#E4E4E7), muted (#71717A), muted-subtle (#52525B), accent (#5B8DEF), accent-hover (#7BA4F4), accent-muted (#5B8DEF20), warning (#F59E0B), success (#22C55E), destructive (#EF4444). Values stored as RGB triplets for Tailwind alpha compositing. No dark: prefix, no light mode, no toggle.
UX-DR2: Configure Tailwind theme extension to reference CSS custom property color tokens. Enforce color usage rules: amber for recoverable errors, blue for interactive semantics, green only on completion, no color carries meaning alone.
UX-DR3: Set up typography system with Geist Sans (primary) and Geist Mono (monospace), both bundled in Go embed.FS. Korean fallback: system gothic stack. font-display: swap.
UX-DR4: Implement 8-level type scale: display (30px/700), h1 (24px/600), h2 (20px/600), h3 (18px/500), body (15px/400 — Korean readability baseline), body-sm (14px/400), caption (12px/400 — floor), mono (14px/400). All in rem. Monospace for machine identifiers, sans for prose.
UX-DR5: Define spacing scale using 4px base unit with 7 named tokens: space-1 (4px) through space-12 (48px).
UX-DR6: Implement affordance density system via data-density attribute with 3 CSS custom properties: --content-expand, --preview-size, --motion-duration. Two densities: standard and elevated.
UX-DR7: Build application layout shell using CSS Grid: Sidebar (220px/48px) + Content pane (flex-1, min 70%). Header 48px. Status bar conditional 36px. Chrome budget 84px. Sidebar auto-collapses below 1280px.
UX-DR8: Build Sidebar component: 220px expanded with icons + labels + scrollable RunCard list; 48px collapsed. CSS media query auto-collapse below 1280px. State persistence in localStorage.
UX-DR9: Build StatusBar component: conditional visibility during active pipeline runs, collapses to 0px when idle. Shows stage icon + name + elapsed time. Hover reveals run ID + cost.
UX-DR10: Build StageStepper component with 6 nodes (pending, scenario, character, assets, assemble, complete). Node states: completed/active/upcoming/failed. Two variants: full (with labels) and compact (icons only).
UX-DR11: Build SceneCard component for batch review list pane. Collapsed state (auto-approved): 56px thumbnail + scene number + narration excerpt + CriticScore badge. Expanded state (pending/flagged): 80px thumbnail + 2-line excerpt + badges. Selected state: accent border + bg-subtle. ARIA attributes required.
UX-DR12: Build DetailPanel component for master-detail right pane. Contains: large scene image, full Korean narration text (scrollable), AudioPlayer, Critic sub-scores (4 bars: hook strength, fact accuracy, emotional variation, immersion), ActionBar at bottom. Audio resets on scene change.
UX-DR13: Build ActionBar component pinned to DetailPanel bottom. Ghost-with-hint buttons: [Enter] Approve, [Esc] Reject, [Tab] Edit, [S] Skip, [Ctrl+Z] Undo. Keyboard labels always visible inline (Linear style).
UX-DR14: Build InlineConfirmPanel component as overlay-modal replacement. Push-up from bottom, ~60px. Contains message + [Enter] Confirm + [Esc] Cancel. No dimming. Slide-up 150ms. ARIA role="alertdialog", focus trapped.
UX-DR15: Build RunCard component for sidebar run inventory. Shows SCP ID (mono) + run number + auto-generated summary + compact StageStepper + timestamp + status badge. States: active/paused/failed/completed.
UX-DR16: Build FailureBanner component. Amber left border + failure message + cost-cap status + "No work was lost" message + [Enter] Resume. Auto-dismisses on resume; Esc to dismiss manually.
UX-DR17: Build CharacterGrid component inline within CharacterPick slot. 2x5 grid for 10 candidates. Keyboard: 1-9/0 direct select, Enter confirm, Esc re-search. Selection confirmation 150-250ms transition.
UX-DR18: Build AudioPlayer component: compact TTS preview. Play/pause + progress bar + duration. 40px height. Wraps HTML5 <audio>. Resets on scene change. Spacebar toggles. Auto-play toggle (default off).
UX-DR19: Build TimelineView component for Settings & History tab. Filter bar (SCP ID, stage, decision type) with 100ms debounced live filtering. Scrollable decision rows. J/K keyboard navigation.
UX-DR20: Implement master-detail split layout for batch review. List pane 30%, Detail panel 70%. Running scene count. Selection state in React context or Zustand. Fixed split in V1.
UX-DR21: Implement 3-route SPA structure: /production, /tuning, /settings. Mode selector idiom (VS Code activity bar). Slot + Strategy composition pattern.
UX-DR22: Implement 8-key keyboard shortcut system as global hook. Enter, Esc, J, K, Tab, Ctrl+Z, Shift+Enter, S, 1-9/0. Hints persistently visible. Custom ESLint rule for key meaning invariance.
UX-DR23: Implement optimistic UI for all review-surface actions with feedback latency < 100ms. Test by order (MSW delays), not wall-clock timing.
UX-DR24: Implement Ctrl+Z undo stack (minimum 10 depth). Command Pattern; undo inserts reversal row with superseded_by. Available until Phase C rendering.
UX-DR25: Implement stale-while-revalidate polling (~5s). No spinner, no layout shift during polls. Skeleton screens for cold-start only.
UX-DR26: Implement inline edit with blur-save for narration and Vision Descriptor editing. Always-visible text field, Tab to enter, save on blur, Ctrl+Z to revert. No explicit Save button.
UX-DR27: Implement continuity banner on every tab entry. Auto-generated one-line summary. Dismisses after 5s or on interaction.
UX-DR28: Implement onboarding modal on empty-state first-time detection. One-time display. Only overlay modal allowed in app.
UX-DR29: Implement progressive hint dismissal for contextual hints. Auto-dismisses after action performed. Stored in localStorage.
UX-DR30: Implement empty state messages for every surface: no runs, all approved, no decisions, no fixtures. Each explains why and what to do next.
UX-DR31: Implement action hierarchy visual system. Primary (accent fill + Enter), Secondary (ghost + Esc), Tertiary (ghost + muted), Destructive (ghost + warning + InlineConfirmPanel). Enforce exactly one primary per surface via data-primary + RTL test.
UX-DR32: Implement universal state-color mapping: approved=green+checkmark, active=accent+pulse, pending=muted+circle, failed=amber+warning, flagged=muted+amber dot, selected=accent border. Same state = same color everywhere.
UX-DR33: Implement Focus-Follows-Selection for master-detail. J/K moves selection, detail updates instantly. Detail panel never empty. K at first wraps to last. Audio resets on change.
UX-DR34: Implement notification pattern system: (a) milestone via Sonner toast 10s auto-dismiss, (b) stage completion via status bar + stepper, (c) failure via FailureBanner, (d) regen completion via card pulse 200ms, (e) batch confirm via InlineConfirmPanel. No unsolicited modals.
UX-DR35: Implement prefers-reduced-motion global CSS rule setting all animation/transition durations to 0ms.
UX-DR36: Implement 2px accent focus ring on all focusable elements via focus-visible. Enforce semantic HTML. Ban outline-none.
UX-DR37: Implement responsive degradation: >= 1280px full layout, 1024-1279px sidebar collapses, < 1024px show "Requires desktop viewport" banner.
UX-DR38: Implement scene review loop keyboard flow: J/K navigate, Enter approve, Esc reject, Tab edit, S skip, Shift+Enter approve all, Ctrl+Z undo. Running count "7/10 scenes reviewed".
UX-DR39: Implement rejection and re-generation flow. Inline reason prompt -> regeneration offer -> progress overlay (non-blocking). Max 2 retries per scene; after 2, offer manual edit or skip & flag.
UX-DR40: Build Production tab scenario read/edit surface. Paragraph-level view of Korean narration with act boundaries. Inline edit per paragraph (contenteditable, blur-save). Max 2 full regeneration retries.
UX-DR41: Build character pick surface. Full-content grid, CharacterGrid 2x5, Vision Descriptor pre-fill below. Confirm (Enter) or edit inline. Elevated affordance density.
UX-DR42: Build run completion surface. Thumbnail + 5s auto-play of final MP4. Metadata bundle confirmation. Next-action CTA as inline panel.
UX-DR43: Build Tuning tab. Critic prompt editor, Golden eval runner, Shadow eval runner, fixture management (1:1 ratio), calibration view (kappa trend). Sequence-dependent: Golden -> Shadow -> commit.
UX-DR44: Build Settings & History tab. TimelineView, persistent configuration management, override rate metric, production velocity delta, burnout-detection note.
UX-DR45: Implement diagnostic deep-link exception pattern. Cross-tab links only in error context to read-only views.
UX-DR46: Implement Fail-Loud-with-Fix error message pattern. Every error names problem, cause, and fix. Recovery action is Enter-key default.
UX-DR47: Implement Critic score ambient visual token: color-coded bar + numeric score + state badge. Sub-scores on detail view. Consistent across all scene surfaces.
UX-DR48: Set up 7 shadcn/ui base components with customizations: Button (ghost-with-hint), Card (dark tokens), Badge (score-high/mid/low/rejected/flagged), Progress (accent/green/muted), Tabs (activity bar), Collapsible (150ms), Tooltip (always-visible). Plus Sonner toast integration.
UX-DR49: Implement Vite + React + Tailwind + shadcn/ui project scaffold with defined directory structure: web/src/components/{ui,shells,slots,shared}, web/src/hooks/, web/src/lib/, web/src/styles/.
UX-DR50: Set up Go embed.FS integration for SPA serving with catch-all routing. Vite config base: "./". Makefile: web-build -> go-build. web/dist/ in .gitignore.
UX-DR51: Implement testing infrastructure: Vitest + RTL for unit/component tests, Playwright for E2E smoke. Contract tests via shared fixtures. CSS custom property verification via getComputedStyle().
UX-DR52: Implement custom ESLint rule enforcing keyboard shortcut invariance (Enter=primary, Esc=secondary). Combined with RTL data-primary count assertion and MSW order tests.
UX-DR53: Implement auto-approved scene card collapse behavior. Collapsed by default (not hidden). Pending/flagged expanded. shadcn/ui Collapsible with 150ms animation.
UX-DR54: Implement Critic override flow. "Override" badge on operator-Critic disagreement. No friction dialog. Tracked in decisions for calibration.
UX-DR55: Implement "Based on your past choices" recommendation hint system for elevated-density phases. One-line transparent reason. Operator can dismiss or drill in.
UX-DR56: Implement production velocity delta burnout-detection system. Rolling 4-week window. Non-blocking note when velocity drops >= 30%.
UX-DR57: Implement milestone acknowledgment banners. Factual one-line at session start for 25th video, Day-90 gate, automation rate. Sonner toast, dismiss once.
UX-DR58: Implement Lucide icon integration. Icons always paired with text labels (except collapsed sidebar). Sizes: 16px inline, 20px buttons, 24px navigation.
UX-DR59: Implement minimum-viewport-width guard. Below 1024px: "Requires desktop viewport (>= 1024px)." banner, no app content.
UX-DR60: Implement batch review master-detail split **maintained** at 1024-1279px (degraded but functional): sidebar auto-collapses from 220px to 48px, content pane gains ~172px, and the 30/70 master-detail split is preserved. True stacked fallback is reserved for < 1024px (where UX-DR59's viewport guard banner displays instead).
UX-DR61: Implement scenario edit flow propagation. Inline edits flow through to Phase B (TTS) and Phase C (assembly).
UX-DR62: Implement Vision Descriptor pre-fill from most recent prior run's descriptor. Plain textarea with blur-save.
UX-DR63: Implement run inventory view in Production sidebar. Searchable, state-aware RunCards with auto-generated summaries.
UX-DR64: Implement returning-user session restoration. Auto-restore last-active tab and interaction point from SQLite.
UX-DR65: Implement inline rejection reason prompt. Inline text prompt (not modal) on Esc. Reason stored in decisions. Followed by regeneration offer.
UX-DR66: Implement Phase C assembly progress display. Stepper advances, progress percentage shown. Completion-moment reward surface on finish.
UX-DR67: Implement cost-cap telemetry display during active runs. Green/amber status on status bar and co-present with failure banners.
UX-DR68: Implement "New Run" creation flow. Button copies `pipeline create <scp-id>` to clipboard + terminal guidance. V1.5 upgrades to web-triggered.

### FR Coverage Map

| FR | Epic | Brief Description |
|---|---|---|
| FR1 | Epic 2 | Operator starts a new pipeline run |
| FR2 | Epic 2 | Operator resumes a failed run from last successful stage |
| FR3 | Epic 2 | Operator cancels an in-flight run |
| FR4 | Epic 2 | Operator inspects run state |
| FR5 | Epic 2 | System persists complete run state |
| FR6 | Epic 2 | System captures per-stage observability data (8 columns) |
| FR8 | Epic 2 | Anti-progress detection (cosine similarity > 0.92 → escalate) |
| FR9 | Epic 3 | System generates research output from local data corpus |
| FR10 | Epic 3 | System produces structured scenario via 6-agent chain |
| FR11 | Epic 3 | System validates each agent's output against schema |
| FR12 | Epic 3 | System enforces Writer ≠ Critic provider at runtime |
| FR13 | Epic 3 | Critic invoked at two checkpoints (post-Writer, post-Reviewer) |
| FR14 | Epic 5 | System generates per-scene images with character continuity |
| FR15 | Epic 5 | System generates per-scene Korean TTS audio |
| FR16 | Epic 5 | Image and TTS tracks overlap (wall-clock ≤ max × 1.2) |
| FR17 | Epic 5 | Character-reference selection prerequisite (10 candidates) |
| FR18 | Epic 5 | Canonical character reference via reference-based image editing |
| FR19 | Epic 5 | System caches character-search results aggressively |
| FR20 | Epic 9 | System assembles per-scene images + audio into video |
| FR21 | Epic 9 | Per-video metadata bundle (AI-generated content disclosure) |
| FR22 | Epic 9 | Per-video source manifest (SCP article + license chain) |
| FR23 | Epic 9 | System gates ready-for-upload on operator metadata ack |
| FR24 | Epic 3 | Critic produces pass/retry/accept-with-notes verdict |
| FR25 | Epic 3 | Rule-based pre-checks (schema + forbidden-term regex) before Critic |
| FR26 | Epic 4 | Golden eval set with 1:1 positive:negative ratio |
| FR27 | Epic 4 | Critic runs against Golden eval set on demand |
| FR28 | Epic 4 | Shadow eval on Critic prompt change (N=10 recent passed) |
| FR29 | Epic 4 | Calibration metric (Cohen's kappa, rolling 25-run window) |
| FR30 | Epic 4 | Minor-content safeguard (blocks downstream until operator review) |
| FR31a | Epic 4 | Auto-approval of review items meeting Critic score threshold |
| FR31b | Epic 8 | Batch-review non-auto-approved scenes via composed cards |
| FR31c | Epic 8 | Precision-review high-leverage scenes (extended-detail surface) |
| FR32 | Epic 8 | Operator can approve, reject, or edit any review item |
| FR33 | Epic 8 | Operator can undo the most recent decision |
| FR34 | Epic 8 | Operator can approve all remaining items in batch |
| FR35 | Epic 8 | Operator can mark decision as "skip and remember" |
| FR36 | Epic 8 | System records every operator HITL action to decisions store |
| FR37 | Epic 8 | Operator can browse full decisions history |
| FR38 | Epic 1 | Operator initializes fresh project (config, schema, .env template) |
| FR39 | Epic 1 | Operator runs preflight check (secrets, paths, binaries) |
| FR40 | Epic 6 | Operator launches local web UI (localhost only) |
| FR41 | Epic 6 | System surfaces operator mode as top-level web UI tabs |
| FR42 | Epic 1 | Machine-readable JSON output for CLI commands |
| FR43 | Epic 1 | Human-readable color-coded hierarchical status output |
| FR44 | Epic 10 | Operator exports per-run decisions/artifact data to JSON |
| FR45 | Epic 9 | System records LLM provider per generation for ToS audit |
| FR46 | Epic 1 | System rejects Writer = Critic provider config (preflight + run entry) |
| FR47 | Epic 9 | System rejects blocked voice-ID profiles |
| FR48 | Epic 3 | System enforces forbidden-term lists at narration generation |
| FR49 | Epic 2 | Operator replays paused HITL session from exact decision point |
| FR50 | Epic 2 | Operator views "what changed since I paused" diff summary |
| FR51 | Epic 1 | Test infrastructure as first-class artifact (contract, integration, Golden/Shadow, fixtures) |
| FR52 | Epic 1 (Go) + Epic 6 (Web) | E2E smoke tests: FR52-go in Epic 1, FR52-web in Epic 6 |
| FR53 | Epic 8 | System surfaces prior rejection of semantically similar scene |

### UX-DR Distribution

| Epic | UX-DRs |
|---|---|
| Epic 6 | UX-DR1–DR8, DR21, DR22, DR25, DR28–DR32, DR35–DR37, DR48–DR52, DR57–DR59, DR64 |
| Epic 7 | UX-DR9, DR10, DR15, DR16, DR17, DR26, DR27, DR40, DR41, DR47, DR62, DR63, DR67, DR68 |
| Epic 8 | UX-DR11–DR14, DR18, DR20, DR23, DR24, DR33, DR34, DR38, DR39, DR46, DR53–DR55, DR60, DR61, DR65 |
| Epic 9 | UX-DR42, DR66 |
| Epic 10 | UX-DR19, DR43–DR45, DR56 |

## Epic List

### Epic 1: Project Foundation & Architecture Skeleton
Operator can initialize the project, verify system readiness, and begin development atop a complete architecture skeleton with provider interfaces, test infrastructure, and mock boundaries — all ready for domain implementation. CI pipeline runs green from Day 1 with contract tests, integration harness, and FR52-go E2E smoke.

**FRs covered:** FR38, FR39, FR42, FR43, FR46, FR51, FR52-go

**Scope:**
- Go + Vite project scaffolding (Option B: Manual Go + shadcn Vite template)
- SQLite schema (3 tables: runs, decisions, segments) + embedded migration runner
- `init` / `doctor` CLI commands
- Renderer abstraction (JSON envelope + human-readable output)
- `.env` secret management + Viper config hierarchy
- Writer ≠ Critic preflight enforcement (defense in depth)
- LLM provider interfaces (`TextGenerator`, `ImageGenerator`, `TTSSynthesizer`) in `internal/domain/`
- `internal/testutil/` (assertEqual[T], fixture loader, nohttp blocking transport)
- External API mock boundary (3-layer defense: injection, blocking transport, CI env lockout)
- Clock abstraction (`internal/clock/`)
- Contract fixture structure (`testdata/contracts/`, `testdata/golden/`)
- Makefile build chain + `.air.toml` + dev workflow
- Domain sentinel errors + error classification system

**NFRs addressed:** NFR-S1 (secrets in .env), NFR-S2 (localhost-only bind), NFR-S3 (DB file permissions), NFR-M3 (stage-boundary schemas), NFR-M4 (layer-import linter), NFR-T1–T6 (CI pipeline, coverage gates, contract tests, E2E smoke)

**Scope additions (relocated from Epic 10):**
- Contract test suite (CLI ↔ Web parity; JSON Schema SSoT in `testdata/contracts/`) — skeleton fixtures now, domain-specific fixtures added per-epic as stories ship
- Integration test harness (stage-boundary schema verification)
- Seed-able run-state test fixture facility (SQLite snapshots, version-controlled)
- CI pipeline: GitHub Actions 4 jobs (`test-go`, `test-web` parallel → `test-e2e` + `build`; ≤ 10 min total)
- FR52-go: Go-side full-pipeline E2E test with mocked external APIs (canonical seed input, must pass before deployment)
- Layer-import linter in CI (NFR-M4)
- `testdata/fr-coverage.json` validator with `grace: true` for unmapped FRs until strict mode enabled (post-Epic 6)

---

### Epic 2: Pipeline Lifecycle & State Machine
Operator can create, resume, cancel, and inspect pipeline runs. System persists complete run state with observability data, detects anti-progress loops, supports HITL session pause/resume with contextual diff summaries, and reports rolling-window pipeline metrics for Day-90 gate evaluation.

**FRs covered:** FR1, FR2, FR3, FR4, FR5, FR6, FR8, FR29 (metrics surface), FR49, FR50

**Scope:**
- 15-stage state machine (Go switch + enum, ~100 LoC)
- `NextStage(current, event) → (Stage, error)` pure function
- 4 HITL wait points (scenario_review, character_pick, batch_review, metadata_ack)
- Stage-level resume with artifact cleanup (DELETE + re-insert for segments)
- Anti-progress detection (cosine similarity > 0.92 → early-stop → human escalation)
- `pipeline_runs` 8-column observability (duration_ms, token_in/out, retry_count, retry_reason, critic_score, cost_usd, human_override)
- Run ID format: `scp-{scp_id}-run-{sequential_number}`
- HITL session replay from exact decision point
- "What changed since I paused" diff summary
- Filesystem ↔ DB state consistency verification at resume entry
- **REST API skeleton (home for all 17 endpoints)**: `internal/api/routes.go` route registration, middleware chain (`WithRequestID`, `WithRecover`, `WithCORS`, `WithRequestLog` via `Chain()`), response envelope (`writeJSON`/`writeError`), `mapDomainError()` error mapping — subsequent epics (7, 8, 9, 10) add domain-specific handlers to this skeleton
- `pipeline serve` command (localhost-only bind, `--dev` flag proxies to Vite dev server)
- Pipeline lifecycle endpoints: `POST /api/runs`, `GET /api/runs`, `GET /api/runs/{id}`, `POST /api/runs/{id}/resume`, `POST /api/runs/{id}/cancel`, `GET /api/runs/{id}/status` (5s polling)

**NFRs addressed:** NFR-P3 (429 backoff), NFR-P4 (wall-clock capture), NFR-C1/C2/C3 (cost caps + telemetry), NFR-R1 (resume idempotency), NFR-R2 (anti-progress FP tracking), NFR-R3 (durable writes), NFR-O1–O4 (observability), NFR-S2 (localhost-only bind)

---

### Epic 3: Scenario Generation & Basic Quality Gate (Phase A)
System generates a complete structured scenario from an SCP ID — research, structure, narration, visual breakdown, review report — with inter-agent schema validation and basic Critic judgment at two checkpoints.

**FRs covered:** FR9, FR10, FR11, FR12, FR13, FR24, FR25, FR48

**Scope:**
- 6-agent sequential chain: Researcher → Structurer → Writer → VisualBreakdowner → Reviewer → Critic
- `AgentFunc` signature with `PipelineState` in-memory passing
- Inter-agent schema validation at every handoff (fail-fast on violation)
- VisualBreakdowner produces per-scene shot breakdown: 1–5 shots with visual descriptors, estimated durations, and transition types (Ken Burns / cross-dissolve / hard cut). Shot count derived from TTS duration estimate; operator override available at scenario_review
- Writer ≠ Critic runtime enforcement (defense in depth with preflight)
- Critic invocation at two checkpoints: post-Writer and post-Reviewer
- Critic verdict: pass / retry / accept-with-notes with rubric sub-scores (hook strength, fact accuracy, emotional variation, immersion)
- Rule-based pre-checks before Critic: JSON schema validation + forbidden-term regex
- Forbidden-term list as operator-authorable, version-controlled artifact
- Forbidden-term list enforcement at narration generation stage (KCSC-derived) — prevents costly downstream rejection at Phase B/C
- `scenario.json` output to per-run directory
- Agent purity rule: agents are pure functions (no state mutation, no DB, no HTTP)

**NFRs addressed:** NFR-M1 (model IDs config-driven), NFR-M2 (authorable artifacts version-controlled), NFR-M3 (stage-boundary schemas), NFR-L1 (compliance at generation time)

---

### Epic 4: Advanced Quality Infrastructure
System operates a comprehensive quality evaluation framework: Golden/Shadow eval sets, calibration tracking, minor-content safeguards, and auto-approval thresholds — enabling the operator to tune quality gates with confidence through regression-tested, data-driven processes.

**FRs covered:** FR26, FR27, FR28, FR29, FR30, FR31a

**Scope:**
- Golden eval set authoring + governance (positive:negative ratio 1:1 enforced)
- Staleness warning (N days since last re-validation or Critic prompt change)
- `go test ./critic -run Golden` execution (detection rate reporting)
- Shadow eval: `go test ./critic -run Shadow` (N=10 recent passed scenes, false-rejection regression)
- Cohen's kappa calibration tracking (rolling 25-run window, "provisional" when n < 25)
- Minor-content safeguard (scenes depicting minors in policy-sensitive contexts → block downstream + operator review)
- Auto-approval threshold configuration (Critic score-based; auto-approved items recorded with distinguishing decision type)
- Golden/Shadow eval runner meta-testing: ≥ 3 known-pass and 3 known-fail fixtures included, validated alongside eval infrastructure

**NFRs addressed:** NFR-T5 (Golden/Shadow eval runner meta-testing), NFR-R2 (anti-progress FP measurement)

---

### Epic 5: Media Generation (Phase B — Image & TTS)
System generates per-scene images and Korean TTS audio in parallel, maintaining cross-scene character visual consistency through Frozen Descriptor propagation and reference-based image editing.

**FRs covered:** FR14, FR15, FR16, FR17, FR18, FR19

**Scope:**
- Frozen Descriptor: verbatim visual descriptor reused across all shots in all scenes (~30 images per run)
- Per-shot image generation: 1–5 shots per scene based on TTS duration (≤8s→1, 8–15s→2, 15–25s→3, 25–40s→4, 40s+→5), operator override via scenario.ShotOverrides
- DashScope TTS (qwen3-tts-flash): Korean transliteration (numerals, English terms → Korean orthography)
- `errgroup.Group` (NOT `errgroup.WithContext`) parallel execution
- Wall-clock overlap enforcement (total ≤ max(image, TTS) × 1.2)
- Character-reference prerequisite: 10 DuckDuckGo candidates surfaced to operator
- Canonical character reference generation via Qwen-Image-Edit
- Aggressive character-search result caching
- Rate-limit coordination: dual layer — semaphore (concurrency) + token bucket (RPM)
- DashScope image and TTS share rate-limit budgets
- Retry: exponential backoff with clock interface, max 30s delay, jitter
- Vendor implementations in `internal/llmclient/{dashscope,deepseek,gemini}/`

**NFRs addressed:** NFR-P3 (429 backoff + retry_reason), NFR-C1/C3 (per-stage cost cap + telemetry), NFR-M1 (model IDs config-driven)

---

### Epic 6: Web UI — Design System & Application Shell
Operator can launch the local web UI, navigate the 3-mode tab structure (Production / Tuning / Settings & History), and interact via keyboard shortcuts — all built on a polished dark-only design system with foundational components. FR52-web Playwright smoke validates `/production` route renders.

**FRs covered:** FR40, FR41, FR52-web

**UX-DRs covered:** UX-DR1–DR8, DR21, DR22, DR25, DR28–DR32, DR35–DR37, DR48–DR52, DR57–DR59, DR64

**Scope:**
- Dark-only CSS custom property color token system (15 semantic tokens, RGB triplets)
- Tailwind theme extension referencing CSS tokens
- Geist Sans + Geist Mono typography (bundled in Go embed.FS, Korean fallback)
- 8-level type scale + 4px-base spacing scale (7 named tokens)
- Affordance density system (data-density attribute, standard + elevated)
- Application layout shell: CSS Grid (Sidebar 220px/48px + Content flex-1)
- Sidebar component (expanded/collapsed, auto-collapse < 1280px, localStorage persist)
- 3-route SPA: /production, /tuning, /settings (React Router v7, BrowserRouter)
- 8-key keyboard shortcut system as global hook (Enter, Esc, J, K, Tab, Ctrl+Z, Shift+Enter, S, 1-9/0)
- 7 shadcn/ui base components (Button, Card, Badge, Progress, Tabs, Collapsible, Tooltip)
- Sonner toast integration
- Stale-while-revalidate polling (~5s, no spinner, skeleton screens for cold-start)
- Onboarding modal (one-time, empty-state detection)
- Progressive hint dismissal (localStorage)
- Empty state messages for every surface
- Action hierarchy visual system (primary/secondary/tertiary/destructive)
- Universal state-color mapping
- 2px accent focus ring (focus-visible), prefers-reduced-motion
- Responsive degradation (≥1280 full, 1024-1279 sidebar collapse, <1024 desktop-required banner)
- Lucide icon integration (16/20/24px sizes)
- Go embed.FS SPA serving + catch-all routing
- Vite + React + Tailwind + shadcn/ui scaffold (web/src/ directory structure)
- Session restoration from SQLite (last active tab + interaction point)
- Milestone acknowledgment banners
- FR52-web: Playwright smoke test (Chromium only, 1 spec) — SPA loads, `/production` route renders, basic interaction works
- Custom ESLint rule enforcing keyboard shortcut invariance (Enter=primary, Esc=secondary) — co-located with keyboard hook to prevent drift (relocated from Epic 10)
- UX testing infrastructure: Vitest + RTL unit/component tests, contract test web-side (Zod schema parsing of `testdata/contracts/`), CSS custom property verification via `getComputedStyle()`

**NFRs addressed:** NFR-A1 (8-key shortcut set), NFR-A3 (desktop-only), NFR-R4 (web UI client-side transient, DB reconstructs)

---

### Epic 7: Production Tab — Scenario Review & Character Selection
Operator can manage runs, review/edit generated scenarios, select character references, and monitor pipeline progress through a complete production workflow surface (full-stack: backend HITL logic + frontend components).

**FR cross-references:** FR10 (scenario review is the operator-facing surface for Phase A output), FR17 (character-reference selection UI — candidate generation logic lives in Epic 5, selection UI lives here)

**UX-DRs covered:** UX-DR9, DR10, DR15, DR16, DR17, DR26, DR27, DR40, DR41, DR47, DR62, DR63, DR67, DR68

**Scope:**
- StatusBar component (conditional visibility, stage icon + name + elapsed time, hover: run ID + cost)
- StageStepper component (6 nodes, 4 states, full + compact variants)
- RunCard component (SCP ID mono + run number + summary + compact stepper + status badge + CriticScore badge)
- Critic score ambient visual token (UX-DR47: color-coded horizontal bar + numeric score + pass/retry/fail state badge; ≥80 green, 50-79 accent, <50 amber) — defined here so RunCard and subsequent Epic 8 DetailPanel share the same token
- FailureBanner component (amber border + failure message + cost-cap status + Resume action)
- CharacterGrid component (2×5 grid, 1-9/0 keyboard select, Enter confirm, Esc re-search)
- Scenario read/edit surface (paragraph-level Korean narration, inline edit, blur-save)
- Character pick surface (full-content grid + Vision Descriptor pre-fill + inline edit)
- Vision Descriptor pre-fill from prior run
- Inline edit with blur-save (always-visible text field, Tab to enter, Ctrl+Z revert)
- Continuity banner on tab entry (auto-generated one-line summary, 5s dismiss)
- Run inventory view in Production sidebar (searchable, state-aware RunCards)
- Cost-cap telemetry display during active runs (green/amber on status bar)
- "New Run" creation flow (copy CLI command to clipboard + terminal guidance)

---

### Epic 8: Batch Review & Decision Management
Operator can review all generated scenes through a master-detail batch review interface, approve/reject/edit individual or batch items, undo decisions, and browse full decision history — with all HITL actions persistently recorded (full-stack: backend HITL logic + frontend components).

**FRs covered:** FR31b, FR31c, FR32, FR33, FR34, FR35, FR36, FR37, FR53

**UX-DRs covered:** UX-DR11–DR14, DR18, DR20, DR23, DR24, DR33, DR34, DR38, DR39, DR46, DR53–DR55, DR60, DR61, DR65

**Scope:**
- SceneCard component (collapsed: auto-approved 56px; expanded: pending/flagged 80px; selected: accent border)
- DetailPanel component (large image + full narration + AudioPlayer + Critic sub-scores + ActionBar)
- AudioPlayer component (play/pause + progress + duration, 40px, Spacebar toggle, reset on scene change)
- ActionBar component (ghost-with-hint buttons: Enter/Esc/Tab/S/Ctrl+Z, keyboard labels visible)
- InlineConfirmPanel component (push-up 60px, Enter/Esc, ARIA alertdialog, focus trapped)
- Master-detail split layout (list 30%, detail 70%, fixed in V1)
- Focus-Follows-Selection (J/K moves selection, detail updates instantly, detail never empty)
- Scene review keyboard flow (J/K, Enter, Esc, Tab, S, Shift+Enter, Ctrl+Z; running count "7/10")
- Rejection + re-generation flow (inline reason prompt → regen offer → progress overlay; max 2 retries)
- Inline rejection reason prompt (not modal; reason stored in decisions)
- Command Pattern undo (≥ 10 depth; superseded_by reference; 5 action types in V1)
- "Skip and remember" pattern recording
- Batch "approve all remaining" with confirmation
- Optimistic UI for all review actions (feedback < 100ms; MSW order test)
- Prior rejection similarity warning (surfaces earlier rejection's reason)
- `decisions` table: target item, decision type, timestamp, optional note
- Decisions history read-only view (date / mode / target / decision / note)
- Auto-approved scene card collapse behavior (collapsed default, pending/flagged expanded)
- Critic override flow ("Override" badge on disagreement, tracked for calibration)
- "Based on your past choices" recommendation hints
- Notification pattern system (milestone toast, stage completion, failure banner, regen pulse, batch confirm)
- Fail-Loud-with-Fix error message pattern (names problem, cause, fix; Enter-key default recovery)
- Critic score ambient visual token (color-coded bar + numeric + state badge)
- Scenario edit propagation to Phase B (TTS) and Phase C (assembly)
- Stacked fallback for 1024-1279px viewport

---

### Epic 9: Video Assembly & Compliance (Phase C)
System assembles per-scene images and audio into a final video, generates AI-content metadata and source attribution, and gates upload-readiness on operator acknowledgment — with full compliance artifact chain.

**FRs covered:** FR20, FR21, FR22, FR23, FR45, FR47

**UX-DRs covered:** UX-DR42, DR66

**Scope:**
- FFmpeg two-stage assembly: (1) per-scene clip — compose 1–5 shot images with transitions (Ken Burns `zoompan`, cross-dissolve `xfade`, hard cut) timed to shot durations, overlaid with scene TTS audio; (2) final concat of all scene clips into output MP4 via concat demuxer
- Per-video metadata bundle (AI-generated narration, AI-generated imagery, models used)
- Per-video source manifest (originating SCP article + sub-work license chain)
- Compliance gate: video not "ready-for-upload" until operator acknowledges metadata bundle
- LLM provider recording per generation (non-null for every LLM call row in pipeline_runs)
- Voice-ID blocklist enforcement (runtime check against operator-maintained blocked list)
- Run completion surface (thumbnail + 5s auto-play of final MP4 + metadata confirmation + next-action CTA)
- Phase C assembly progress display (stepper advances, progress percentage, completion reward surface)
- Per-run directory output structure (clips/, output.mp4, metadata.json, manifest.json)

**NFRs addressed:** NFR-L1–L4 (compliance artifacts), NFR-M2 (forbidden-term lists version-controlled)

---

### Epic 10: Tuning, Settings & Operational Tooling
Operator can edit Critic prompts, run Golden/Shadow evals, manage fixtures, review calibration trends, browse decision history with filters, manage configuration, and export data.

**FRs covered:** FR44

**UX-DRs covered:** UX-DR19, DR43–DR45, DR56

**Scope:**
- Tuning tab: Critic prompt editor, Golden eval runner UI, Shadow eval runner UI, fixture management (1:1 ratio), calibration view (kappa trend)
- Settings & History tab: TimelineView (filter bar with 100ms debounced live filtering, J/K keyboard navigation), persistent configuration management
- Override rate metric + production velocity delta + burnout-detection note
- Diagnostic deep-link exception pattern (cross-tab links in error context only)
- Data export to JSON on demand (per-run decisions, artifact data)

**NFRs addressed:** (none — test/CI NFRs relocated to Epic 1)

---

### Party Mode Insights (Step 2)

**Round 1 — Agents:** John (PM), Winston (Architect), Sally (UX), Amelia (Developer)

**Key changes from original proposal:**
1. Epic 1 expanded from "init/doctor only" to full architecture skeleton (provider interfaces, testutil, mock boundary, clock abstraction, contract fixtures)
2. Basic Critic judgment (FR13, FR24, FR25) merged into Epic 3 so Phase A chain is end-to-end complete
3. Epic 4 narrowed to "Advanced Quality Infrastructure" (Golden/Shadow eval, calibration, fixture governance)
4. FR31a (auto-approval) moved to Epic 4 (Critic-score-based behavior, not review-surface behavior)
5. Test infrastructure basics (testutil, nohttp, contract fixtures) moved to Epic 1 from Epic 10
6. Old "Web UI Production Surfaces" epic split into Epic 7 (Scenario Review + Character Pick) and Epic 8 (Batch Review + Decisions)
7. UX-DRs distributed across functional epics (not isolated in UI-only epics) — each epic delivers complete user-facing value
8. Epic 6 narrowed to design system + application shell only (common foundation)

**Round 2 — Agents:** Murat (TEA), Mary (Analyst), Winston (Architect), Amelia (Developer)

**Additional refinements:**
1. FR48 (forbidden-term enforcement) moved from Epic 9 to Epic 3 — enforcement at narration generation (Phase A) prevents costly Phase B/C rejection
2. Epic 7 gains FR10/FR17 cross-references for traceability (scenario review = FR10 operator surface, character selection UI = FR17 operator surface)
3. Golden/Shadow eval runner meta-testing (NFR-T5) moved from Epic 10 to Epic 4 — eval infrastructure and its validation belong together
4. Noted for Step 3: tests should be distributed across Epic acceptance criteria, not deferred to Epic 10; implementation sequence (Architecture doc 12 steps) should guide story ordering within epics

**Round 3 — Agents:** John (PM), Winston (Architect), Sally (UX), Murat (TEA)

**Structural refinements (requirement validation + epic re-validation):**
1. FR51/FR52 split (FR52-go: Go pipeline E2E + FR52-web: Playwright smoke) with timing correction: FR52-go → Epic 1, FR52-web → Epic 6 (route must exist first)
2. Contract test suite, CI pipeline (4-job), integration test harness, seed-able fixtures, layer-import linter, `fr-coverage.json` validator all moved from Epic 10 → Epic 1 (CI green from Day 1)
3. REST API skeleton (routes.go, middleware chain, response envelope, error mapping, `pipeline serve`) defined as Epic 2 home — subsequent epics (7, 8, 9, 10) add domain-specific handlers
4. UX-DR47 (CriticScore ambient visual token) moved from Epic 8 → Epic 7 (RunCard needs score badge before DetailPanel)
5. UX-DR52 (ESLint keyboard invariance rule) moved from Epic 10 → Epic 6 (co-locate with keyboard hook to prevent drift)
6. Epic 5 Story 5.5 gains 3 rate-limiter ACs: AC-RL1 (deterministic backoff via FakeClock), AC-RL2 (shared DashScope limiter ±5% allocation), AC-RL3 (30s circuit-break, no goroutine leak)
7. Epic 10 renamed to "Tuning, Settings & Operational Tooling" — CI/test infra removed, Golden/Shadow CI gates remain as Epic 1 extension (Story 10.4)
8. `fr-coverage.json` validator ships with `grace: true` mode (unmapped FRs warn-only until strict mode enabled post-Epic 6)

---

## Stories

### Epic 1: Project Foundation & Architecture Skeleton

#### Story 1.1: Go + React SPA Project Scaffolding & Build Chain

As an operator,
I want the full project structure initialized with Go module, React SPA, Makefile, and development workflow,
So that I can build and run both backend and frontend from day one.

**Acceptance Criteria:**

**Given** the repository is cloned on a new machine
**When** `go mod init github.com/sushistack/youtube.pipeline` and directory creation complete
**Then** the directory structure matches the Architecture Day 1 layout (`cmd/pipeline/`, `internal/{service,domain,llmclient/{dashscope,deepseek,gemini},db,pipeline,hitl,web,clock,testutil}/`, `migrations/`, `testdata/{contracts,golden}/`, `web/`, `e2e/`)

**Given** `npx shadcn@latest init --base vite --name web` has been run
**When** Vite version is checked
**Then** Vite ^7.3 is installed (not 8.x), React 19.x, Tailwind CSS 4.x, shadcn/ui CLI v4 are present
**And** `web/vite.config.ts`, `web/vitest.config.ts` (jsdom environment), `web/tsconfig.json` exist
**And** Vitest ^4.1.4, React Testing Library, jsdom are installed as devDependencies

**Given** Playwright is initialized
**When** `npx playwright install --with-deps chromium` completes
**Then** `e2e/playwright.config.ts` (Chromium only) and `e2e/smoke.spec.ts` placeholder exist

**Given** all scaffolding is done
**When** `make build` is run
**Then** `web-build` runs first, then `go-build` produces `bin/pipeline`
**And** `make test` runs `test-go` and `test-web` successfully
**And** `make dev` starts Vite dev server and air in parallel

**Given** `.air.toml` exists
**When** `air` is started
**Then** Go files are hot-reloaded on change

**Tests:** Unit — `go build ./cmd/pipeline` succeeds; `cd web && npx vitest run` succeeds with 0 tests.

---

#### Story 1.2: SQLite Database & Migration Infrastructure

As an operator,
I want the database schema initialized automatically on first run,
So that pipeline state tracking works from the start.

**Acceptance Criteria:**

**Given** no database file exists
**When** `internal/db/sqlite.go` opens a new connection
**Then** WAL mode is enforced (PRAGMA journal_mode = wal)
**And** busy_timeout is set to 5000ms
**And** the database file has 0600 permissions on POSIX

**Given** a fresh database
**When** the embedded migration runner (`internal/db/migrate.go`, ~50 LoC) executes
**Then** `migrations/001_init.sql` creates 3 tables: `runs`, `decisions`, `segments`
**And** `PRAGMA user_version` is set to 1
**And** `UNIQUE(run_id, scene_index)` constraint exists on `segments`
**And** all column types and defaults match the Architecture DDL exactly

**Given** migration 001 has already been applied
**When** the migration runner executes again
**Then** no error occurs and `PRAGMA user_version` remains 1 (idempotent)

**Tests:** Unit — `sqlite_test.go`: WAL mode assertion, busy_timeout assertion, permission check, migration idempotency test, schema verification (table and column existence).

---

#### Story 1.3: Domain Types, Error System & Architecture Interfaces

As a developer,
I want core domain types, error classifications, and provider interfaces defined,
So that all subsequent domain implementation has a stable foundation.

**Acceptance Criteria:**

**Given** `internal/domain/types.go` is created
**When** the Stage type is inspected
**Then** 15 stage constants exist: pending, research, structure, write, visual_break, review, critic, scenario_review, character_pick, image, tts, batch_review, assemble, metadata_ack, complete
**And** Run, Episode, NormalizedResponse structs are defined with snake_case JSON tags

**Given** `internal/domain/errors.go` is created
**When** error classification is inspected
**Then** 7 sentinel error categories exist: ErrRateLimited, ErrUpstreamTimeout, ErrStageFailed, ErrValidation, ErrConflict, ErrCostCapExceeded, ErrNotFound
**And** each error has HTTP status code and retryable flag attributes

**Given** `internal/domain/` contains provider interfaces
**When** interfaces are inspected
**Then** `TextGenerator`, `ImageGenerator` (with both Generate and Edit methods), and `TTSSynthesizer` interfaces exist
**And** all implementations accept `*http.Client` via constructor (no http.DefaultClient)

**Given** `internal/clock/clock.go` is created
**When** the Clock interface is inspected
**Then** `RealClock` and `FakeClock` implementations exist
**And** FakeClock supports deterministic time advancement for testing

**Tests:** Unit — compile-time interface satisfaction checks; FakeClock advancement test.

---

#### Story 1.4: Test Infrastructure & External API Isolation

As a developer,
I want test utilities and API isolation in place,
So that every subsequent story can be safely tested without hitting real APIs.

**Acceptance Criteria:**

**Given** `internal/testutil/assert.go` is created
**When** `assertEqual[T]` is called with matching values
**Then** test passes
**When** called with non-matching values
**Then** test fails with clear diff output including file/line via `t.Helper()`

**Given** `internal/testutil/fixture.go` is created
**When** `LoadFixture(t, "contracts/pipeline_state.json")` is called
**Then** the fixture file content is returned as `[]byte` from `testdata/` directory

**Given** `internal/testutil/nohttp.go` is active
**When** any test attempts an HTTP call to a non-localhost URL
**Then** the call fails immediately with message: "external HTTP call blocked in test: {url}"

**Given** `web/src/test/setup.ts` is configured
**When** any Vitest test attempts `globalThis.fetch` to a non-localhost URL
**Then** the fetch is rejected with a clear error

**Given** `CI=true` environment variable is set
**When** any API key env var (DASHSCOPE_API_KEY, DEEPSEEK_API_KEY, GEMINI_API_KEY) is also set
**Then** Go test init panics with message: "API keys must not be set in CI environment"

**Given** `testdata/contracts/pipeline_state.json` exists
**Then** it contains a valid contract fixture for pipeline state schema
**And** `testdata/golden/` directory exists (empty, ready for Epic 4)

**Given** a contract fixture in `testdata/contracts/` (FR51 — contract test execution)
**When** `go test ./internal/db/ -run Contract` executes
**Then** the JSON Schema fixture is loaded, unmarshalled into the corresponding Go struct, and validated without error
**And** `testdata/contracts/` files are NEVER auto-updated — manual review only

**Given** a seed-able run-state fixture is needed (FR51 — seed-able fixtures)
**When** `testutil.LoadRunStateFixture(t, "paused_at_batch_review.db")` is called
**Then** a pre-seeded SQLite snapshot from `testdata/` is loaded into a temporary test DB
**And** the fixture contains a run at a specific stage with decisions, segments, and observability data
**And** fixtures are version-controlled alongside code (NFR-T4)

**Tests:** Unit — assertEqual pass/fail test; fixture loading test; seed-able fixture load + query test. Integration — nohttp blocking test (attempt real URL, assert failure); CI lockout test (subprocess panic verification); contract test Go-side parse validation.

---

#### Story 1.5: CLI Foundation — Init & Doctor Commands

As an operator,
I want `init` and `doctor` commands,
So that I can set up a fresh project and verify all prerequisites are met before running the pipeline.

**Acceptance Criteria:**

**Given** a fresh project root with no `./config.yaml`, `./pipeline.db`, or `./output/`
**When** `pipeline init` is executed
**Then** config files are created (`./config.yaml` with model IDs, paths, cost caps; project-root layout)
**And** `./.env` template is generated listing required secrets: DASHSCOPE_API_KEY, DEEPSEEK_API_KEY, GEMINI_API_KEY
**And** SQLite database is initialized with schema (calls migration runner from Story 1.2)
**And** output directory layout is created (`./output/`)

**Given** `config/loader.go` (Viper configuration) is implemented
**When** config is loaded
**Then** hierarchy is respected: `.env` (secrets) → `config.yaml` (non-secret) → CLI flags
**And** all model IDs, paths, and cost caps are accessible via typed config struct

**Given** all prerequisites are met (API keys in .env, FS paths writable, FFmpeg installed, Writer ≠ Critic provider)
**When** `pipeline doctor` is executed
**Then** 4 checks pass with green status:
  (a) API key presence (3 required keys)
  (b) Filesystem path writability (output dir, data dir)
  (c) FFmpeg binary availability (`ffmpeg -version`)
  (d) Writer ≠ Critic provider validation (FR46)
**And** each check outputs a pass/fail indicator with remediation hint on failure

**Given** the Writer and Critic provider are configured to the same value
**When** `pipeline doctor` is executed
**Then** the preflight check fails with: "Writer and Critic must use different LLM providers"

**Given** doctor checks are implemented via extensible registry
**When** a new check is added
**Then** it only requires implementing a `Check` interface and registering it

**Tests:** Integration — init creates expected directory tree; doctor passes with valid config; doctor fails on missing key with correct remediation message; doctor fails on Writer=Critic; config hierarchy test (env overrides yaml).

---

#### Story 1.6: CLI Renderer & Output Formatting

As an operator,
I want both human-readable and JSON output for CLI commands,
So that I can use the tool interactively and script against it programmatically.

**Acceptance Criteria:**

**Given** a `Renderer` interface with `RenderSuccess(data)` and `RenderError(err)` methods
**When** the default (human-readable) renderer is used
**Then** output is color-coded and hierarchically formatted (FR43)
**And** status indicators use semantic colors (green=success, amber=warning, red=error)

**Given** the `--json` flag is passed to any CLI command (FR42)
**When** the JSON renderer produces a success response
**Then** output matches envelope: `{"version": 1, "data": {...}}`
**When** the JSON renderer produces an error response
**Then** output matches envelope: `{"version": 1, "error": {"code": "...", "message": "...", "recoverable": true/false}}`
**And** error codes map to the 7-category error classification from Story 1.3

**Given** `pipeline doctor` is called without `--json`
**When** checks are displayed
**Then** each check shows ✓/✗ with color and remediation hints

**Given** `pipeline doctor --json` is called
**When** output is parsed
**Then** it is valid JSON matching the versioned envelope schema

**Tests:** Unit — Renderer interface compliance; JSON envelope structure validation; human-readable output contains ANSI color codes; round-trip JSON parsing test.

---

#### Story 1.7: CI Pipeline & E2E Smoke Test (FR52-go)

As a developer,
I want CI running green from Day 1 with contract tests, layer-import linting, and an E2E smoke test,
So that every commit is validated against the full quality gate suite.

**Acceptance Criteria:**

**Given** `.github/workflows/ci.yml` is created
**When** a commit is pushed
**Then** 4 jobs execute: `test-go` and `test-web` in parallel, then `test-e2e` and `build` sequentially after both pass
**And** Go module cache, npm cache, and Playwright Chromium binary are cached via `actions/setup-go` and `actions/setup-node`
**And** no API keys are injected into CI environment (Layer 3 defense)

**Given** contract fixtures exist in `testdata/contracts/`
**When** `go test ./internal/db/ -run Contract` executes
**Then** each JSON Schema fixture is loaded, unmarshalled into the corresponding Go struct, and validated without error

**Given** the layer-import linter is configured (NFR-M4)
**When** `go-cleanarch` or `depguard` runs in CI
**Then** import direction violations (`api` → `service` → `domain/db/llmclient/pipeline/clock`) cause build failure

**Given** `testdata/fr-coverage.json` validator is configured
**When** CI runs the validator
**Then** it warns (not fails) for unmapped FRs (`grace: true` mode until strict mode is enabled post-Epic 6)

**Given** a canonical seed input (one SCP ID) with mocked external APIs (FR52-go)
**When** `go test -run E2E` executes
**Then** the full pipeline (Phase A → B → C) completes with mocked providers
**And** output artifacts (scenario.json, images/, tts/, output.mp4, metadata.json, manifest.json) exist in the run output directory
**And** this test must pass before any deployment/release

**Tests:** CI validation — all 4 jobs green; contract test pass; layer-import lint pass; E2E smoke pass with mocked APIs. Total CI wall-clock ≤ 10 minutes (NFR-T6).

---

### Epic 2: Pipeline Lifecycle & State Machine

#### Story 2.1: State Machine Core & Stage Transitions

As a developer,
I want the pipeline state machine implemented as a pure function,
So that all stage transitions are testable, deterministic, and form the backbone of the pipeline lifecycle.

**Acceptance Criteria:**

**Given** `internal/pipeline/engine.go` implements `NextStage(current Stage, event Event) → (Stage, error)`
**When** a valid transition is requested (e.g., pending → research on StartEvent)
**Then** the correct next stage is returned with no error

**Given** an invalid transition is requested (e.g., pending → complete)
**When** `NextStage` is called
**Then** an error is returned describing the invalid transition

**Given** the current stage is a HITL wait point (scenario_review, character_pick, batch_review, metadata_ack)
**When** the run status is queried
**Then** status = "waiting" (not "running")

**Given** the current stage is an automated stage (research, write, image, etc.)
**When** the run status is queried
**Then** status = "running"

**Given** 15 stages and all valid/invalid transition combinations
**When** the full transition matrix is tested
**Then** every valid path produces the expected next stage and every invalid path returns an error

**Tests:** Unit — exhaustive transition matrix test covering all 15 stages × valid/invalid events; HITL status assertion; pure function (no DB, no side effects).

---

#### Story 2.2: Run Create, Cancel & Inspect

As an operator,
I want to create, cancel, and inspect pipeline runs,
So that I can manage the lifecycle of video production.

**Acceptance Criteria:**

**Given** the operator runs `pipeline create scp-049`
**When** the command completes
**Then** a new row is created in `runs` table with id=`scp-049-run-1`, scp_id=`049`, stage=`pending`, status=`pending`
**And** the run ID follows the format `scp-{scp_id}-run-{sequential_number}` (FR1)
**And** a per-run output directory is created at `./output/scp-049-run-1/`

**Given** run `scp-049-run-2` already exists
**When** the operator creates another run for scp-049
**Then** the new run ID is `scp-049-run-3` (sequential increment)

**Given** a run is in `running` status
**When** the operator runs `pipeline cancel scp-049-run-1` (FR3)
**Then** run status is set to `cancelled` and any in-flight stage is marked `failed`

**Given** at least one run exists
**When** the operator runs `pipeline status` (FR4)
**Then** all runs are displayed with their current stage, status, and timestamps
**When** the operator runs `pipeline status scp-049-run-1`
**Then** detailed stage-by-stage progression is displayed for that run

**Tests:** Integration — run creation with correct ID format and DB row; sequential ID increment; cancel state transition; status output for single and all runs.

---

#### Story 2.3: Stage-Level Resume & Artifact Lifecycle

As an operator,
I want to resume a failed run from the last successful stage,
So that I don't lose completed work when a stage fails.

**Acceptance Criteria:**

**Given** run `scp-049-run-1` failed at stage `tts` with stages `research` through `image` completed
**When** the operator runs `pipeline resume scp-049-run-1` (FR2)
**Then** execution resumes from the `tts` stage (the failed stage)
**And** all previously completed stages are NOT re-executed
**And** their artifacts remain intact on disk

**Given** a run is being resumed for a Phase B stage
**When** resume entry validation runs
**Then** filesystem state is checked against DB state (artifact paths in `segments` match existing files)
**And** if inconsistency is found, a warning is emitted and the operator is prompted

**Given** Phase B resume triggers
**When** segments for the run are processed
**Then** all existing segment rows for that run are DELETED
**And** new segments are re-inserted (clean slate semantics)
**And** partial artifact files from the failed stage are cleaned from disk

**Given** the same inputs are provided to a resumed stage
**When** the stage completes
**Then** output conforms to the same schema and stage-status progression as a fresh run (NFR-R1)
**And** metadata bundle is non-null and source manifest validates

**Tests:** Integration — resume from mid-pipeline failure; segments clean-slate verification; filesystem artifact cleanup on resume; FS↔DB consistency check.

---

#### Story 2.4: Per-Stage Observability & Cost Tracking

As an operator,
I want observability data and cost tracking for every stage,
So that I can diagnose issues and control spending.

**Acceptance Criteria:**

**Given** a stage completes (success or failure)
**When** the observability record is saved to `runs`
**Then** all 8 columns are populated: duration_ms, token_in, token_out, retry_count, retry_reason (nullable), critic_score (nullable), cost_usd, human_override (FR5, FR6)
**And** no column is truncated or sampled (NFR-O1)

**Given** a stage's accumulated `cost_usd` exceeds the configured per-stage cap
**When** the cost accumulator checks the threshold
**Then** the stage hard-stops immediately (NFR-C1)
**And** error type is `ErrCostCapExceeded` with retryable=false
**And** the operator is notified with stage name and cost amount

**Given** a run's total accumulated `cost_usd` exceeds the configured per-run cap
**When** the cost accumulator checks the threshold
**Then** the entire run hard-stops (NFR-C2)

**Given** a stage receives an HTTP 429 response from an external API
**When** the stage handles the error
**Then** stage status does NOT advance
**And** `retry_reason` = "rate_limit" is recorded in the observability row (NFR-P3)
**And** the stage backs off without marking as permanently failed

**Given** cost data is queried via SQLite CLI
**When** standard diagnostic queries are run
**Then** results are sufficient for operational diagnosis without external tooling (NFR-O3)

**Tests:** Unit — cost accumulator threshold tests (deterministic, no real API); 429 backoff behavior with FakeClock. Integration — 8-column DB verification after stage completion; cost cap hard-stop with correct error type.

---

#### Story 2.5: Anti-Progress Detection

As an operator,
I want the system to detect when retry loops are making no progress,
So that I'm not wasting API costs on structurally unfixable outputs.

**Acceptance Criteria:**

**Given** a stage is in a retry loop
**When** cosine similarity between consecutive retry outputs exceeds the configured threshold (default 0.92)
**Then** the retry loop early-stops (FR8)
**And** `retry_reason` = "anti_progress" is recorded
**And** the operator is escalated with a message: "Retries producing similar output — human review required"

**Given** the cosine similarity threshold is changed via config
**When** the anti-progress detector runs
**Then** the new threshold is used

**Given** consecutive retry outputs have cosine similarity below 0.92
**When** the detector runs
**Then** retries continue normally (no false positive)

**Given** a rolling 50-run window is available
**When** false-positive rate is calculated
**Then** the measurement is captured for V1.5 gating (NFR-R2; target ≤ 5% applied from V1.5)

**Tests:** Unit — cosine similarity calculation accuracy; threshold crossing detection; configurable threshold; no false positive on dissimilar outputs. All tests use deterministic inputs, no LLM calls.

---

#### Story 2.6: HITL Session Pause/Resume & Change Diff

As an operator,
I want to pause a HITL session and resume exactly where I left off with a summary of what changed,
So that I don't lose context when stepping away from the tool.

**Acceptance Criteria:**

**Given** the operator is in a HITL review (e.g., batch_review at scene 5/10) and the session is interrupted
**When** the backend persists the pause state to DB (exact decision point: run_id, stage, scene_index, last_interaction_timestamp)
**Then** the pause state is durable across process restarts (NFR-R3)
**And** subsequent API calls (`GET /api/runs/{id}/status`) return the paused position (FR49)
**Note:** Frontend rendering of the resumed session (scroll position, state-aware summary banner) is tested in Epic 7/8 when the web UI surfaces exist.

**Given** the pause state is loaded via API
**When** the response is inspected
**Then** it includes: run_id, current_stage, scene_index, total_scenes, decisions_summary (approved/rejected/pending counts)
**And** a state-aware summary string: "Run scp-049-run-1: reviewing scene 5 of 10, 4 approved, 0 rejected"

**Given** a run was paused at the operator's last interaction at timestamp T1
**When** the run has progressed (e.g., regeneration completed) since T1
**Then** a "what changed since I paused" diff summary is produced (FR50)
**And** the diff contains: changed items, their before/after status, and timestamps

**Given** no changes have occurred since pause
**When** the operator resumes
**Then** no diff is shown, only the state-aware summary

**Tests:** Integration — pause state persistence in DB; resume position accuracy; diff generation with correct items; no-change scenario produces no diff.

---

#### Story 2.7: Pipeline Metrics CLI Report (Day-90 Gate)

As an operator,
I want to view rolling-window pipeline metrics via a CLI command,
So that I can evaluate Day-90 acceptance gates and diagnose quality trends without external tooling.

**Acceptance Criteria:**

**Given** at least one completed run exists
**When** `pipeline metrics --window 25` is executed
**Then** the following 5 metrics are calculated over the specified rolling window and displayed:
  (a) Automation rate: (auto-completed stages / total stages), averaged across window runs
  (b) Critic calibration: Cohen's kappa between Critic verdicts and operator overrides
  (c) Critic regression detection: detection rate on the Golden eval set
  (d) Defect escape rate: fraction of Critic auto-passed scenes subsequently rejected by operator
  (e) Stage-level resume idempotency: same input + resume produces functionally equivalent output
**And** each metric shows: current value, Day-90 target, pass/fail status
**And** if n < 25 runs exist, all metrics display a "provisional" label (per PRD §Success Criteria)

**Given** `pipeline metrics --window 25 --json` is executed
**When** the JSON renderer produces output
**Then** it matches the versioned envelope: `{"version": 1, "data": {"window": 25, "metrics": [...], "provisional": true/false}}`

**Given** the underlying data is in `pipeline_runs` and `decisions` tables
**When** the metrics query executes
**Then** it uses the SQL indexes defined in migration (NFR-O4) without full-table scans
**And** query execution completes within 1 second for up to 1000 runs

**Tests:** Unit — metric calculation accuracy with known fixture data (deterministic kappa, automation rate, escape rate). Integration — CLI output format verification (human-readable + JSON); provisional label when n < window; SQL index usage (EXPLAIN QUERY PLAN).

---

### Epic 3: Scenario Generation & Basic Quality Gate (Phase A)

#### Story 3.1: Agent Function Chain & Pipeline Runner

As a developer,
I want a pipeline runner that orchestrates pure-function agents,
So that I can execute the sequential Phase A chain with in-memory state passing.

**Acceptance Criteria:**

**Given** the `internal/pipeline/runner.go` is implemented
**When** the Phase A chain starts
**Then** agents are executed sequentially: Researcher → Structurer → Writer → VisualBreakdowner → Reviewer → Critic
**And** the `PipelineState` is passed in-memory between agents
**And** each agent follows the purity rule (no DB, no side effects)

**Given** the chain completes successfully
**When** the output is inspected
**Then** a `scenario.json` is produced in the run's output directory

**Tests:** Unit — mock agent chain execution; state passing verification. Integration — runner produces valid scenario.json file.

---

#### Story 3.2: Researcher & Structurer Agents

As a system,
I want to research SCP data and produce a 4-act scenario structure,
So that the story has a factual basis and a narrative arc.

**Acceptance Criteria:**

**Given** the Researcher agent receives an SCP ID
**When** it queries the local data corpus (FR9)
**Then** it produces a structured research summary
**And** the summary is validated against the Researcher schema

**Given** the Structurer agent receives the research summary
**When** it executes
**Then** it produces a 4-act narrative structure (FR10)
**And** the output is validated against the Structurer schema (FR11)

**Tests:** Unit — agent output validation against JSON schemas; Researcher data retrieval from mock corpus.

---

#### Story 3.3: Writer Agent & Critic (Post-Writer Checkpoint)

As an operator,
I want the system to generate Korean narration and pass a quality gate,
So that the script is immersive and meets safety standards before continuing.

**Acceptance Criteria:**

**Given** the Writer agent receives the scenario structure
**When** it generates Korean narration
**Then** it enforces forbidden-term lists (FR48)
**And** the output is validated against the Writer schema

**Given** the Writer task is dispatched
**When** runtime providers are checked
**Then** the Writer provider is NOT equal to the Critic provider (FR12)

**Given** the post-Writer checkpoint is reached
**When** the Critic agent is invoked (FR13)
**Then** it performs rule-based pre-checks: JSON schema + forbidden-term regex (FR25)
**And** it produces a verdict: pass/retry/accept-with-notes (FR24)
**And** it provides rubric sub-scores (hook, accuracy, etc.)

**Tests:** Unit — Writer forbidden-term regex enforcement; Critic verdict logic; Provider inequality check at runner level.

---

#### Story 3.4: VisualBreakdowner & Reviewer Agents

As a system,
I want to generate visual descriptors and perform a fact-check review,
So that the script is ready for media generation with high accuracy.

**Acceptance Criteria:**

**Given** the VisualBreakdowner agent receives the narration and TTS duration estimates
**When** it executes
**Then** it produces per-scene shot breakdowns: each scene contains 1–5 shots
**And** each shot has: `visual_descriptor` (dense text), `estimated_duration_s`, `transition` (ken_burns | cross_dissolve | hard_cut)
**And** shot count per scene follows the duration formula (≤8s→1, 8–15s→2, 15–25s→3, 25–40s→4, 40s+→5)
**And** the Frozen Descriptor is propagated verbatim into every shot's visual_descriptor as a prefix
**And** the output is validated against its schema (FR11): `Scenes[i].Shots[]` array, each element has required fields

**Given** the operator reviews the shot breakdown at scenario_review HITL
**When** the operator wants to override shot count or transition type for a specific scene
**Then** the override is persisted in scenario.json as `shot_overrides[scene_index]`
**And** Phase B respects these overrides

**Given** the Reviewer agent receives the breakdown
**When** it executes
**Then** it produces a review report flagging any factual or consistency issues

**Tests:** Unit — agent output schema validation (shots array per scene); shot count derivation from duration; Frozen Descriptor prefix presence in every shot; Reviewer consistency check logic.

---

#### Story 3.5: Phase A Completion & Post-Reviewer Critic

As an operator,
I want a final quality check after the full scenario is complete,
So that the entire Phase A output is reliable.

**Acceptance Criteria:**

**Given** the post-Reviewer checkpoint is reached
**When** the Critic agent is invoked for the second time (FR13)
**Then** it evaluates the full scenario including visual breakdown
**And** it produces a cumulative quality score

**Given** Phase A completes
**When** the final `scenario.json` is written
**Then** all agent outputs, schemas, and critic verdicts are included in the version-controlled artifact (NFR-M2)

**Tests:** Integration — full Phase A run with 2 critic checkpoints; final scenario.json integrity check.

---

### Epic 4: Advanced Quality Infrastructure

#### Story 4.1: Golden Eval Set Governance & Validation

As a developer,
I want to manage a Golden eval set and validate the Critic against it,
So that I can measure the quality gate's baseline effectiveness.

**Acceptance Criteria:**

**Given** the `internal/critic/eval/` directory
**When** a Golden set is authored
**Then** it must maintain a 1:1 ratio of positive (pass) and negative (fail) examples
**And** `go test ./internal/critic/eval -run Golden` reports a detection rate (recall)

**Given** an operator adds a new fixture pair to the Golden eval set (FR26 — authoring workflow)
**When** `pipeline golden add --positive <path> --negative <path>` is executed
**Then** both fixtures are validated against the expected schema before acceptance
**And** the 1:1 ratio is re-verified (reject if adding only positive or only negative without a pair)
**And** fixtures are stored in version-controlled `testdata/golden/` with a monotonic index
**And** `pipeline golden list` shows all fixture pairs with their indices and creation timestamps

**Given** a Golden set has not been updated in N days (configurable, default 30)
**When** the runner executes
**Then** a "Staleness Warning" is emitted to logs and surfaced via `pipeline doctor` output + Tuning tab banner

**Given** the Critic prompt has been modified since the last Golden validation run (FR26 — prompt-change staleness)
**When** `pipeline doctor` or the Tuning tab is accessed
**Then** a "Staleness Warning: Critic prompt changed since last Golden validation" is surfaced
**And** the Golden runner is recommended before any new pipeline runs

**Given** a change to the Critic prompt or rubric
**When** the Golden test is run
**Then** the pass rate must not decrease from the baseline (NFR-T5)

**Tests:** Unit — Golden set ratio validation logic; fixture pair add/reject on ratio violation; staleness threshold calculation. Integration — Golden runner execution and reporting; prompt-change staleness detection; `pipeline golden add` CLI flow.

---

#### Story 4.2: Shadow Eval Runner

As a developer,
I want to run Shadow evaluations on recent passed cases after prompt changes,
So that I can suppress false-rejections (regressions).

**Acceptance Criteria:**

**Given** a change to the Critic prompt
**When** `go test ./internal/critic/eval -run Shadow` is executed
**Then** it pulls N=10 (configurable) recently passed scenes from the `runs` table
**And** it verifies the new prompt still passes those cases
**And** results are logged with pass/fail and diff of sub-scores

**Tests:** Integration — Shadow runner pulling data from SQLite and executing Critic against it.

---

#### Story 4.3: Cohen's Kappa Calibration Tracking

As an operator,
I want to track Cohen's kappa between my decisions and the Critic's scores,
So that I know when the quality gate is misaligned with my standards.

**Acceptance Criteria:**

**Given** a pool of recent runs
**When** the calibration engine runs
**Then** it joins the `decisions` table (operator actions) with the `runs` table (Critic scores)
**And** it calculates Cohen's kappa for a rolling 25-run window
**And** the result is "provisional" if n < 25
**And** the kappa value and trend data are persisted to the database

**Tests:** Unit — Cohen's kappa formula verification using mock agreement/disagreement data.

---

#### Story 4.4: Minor-Content Safeguard & Auto-Approval Thresholds

As an operator,
I want automated safeguards for sensitive content and auto-approval for high-quality items,
So that I can focus my review effort where it matters most.

**Acceptance Criteria:**

**Given** a scene contains potential minor-related sensitive content (regex/LLM detection)
**When** the safeguard executes
**Then** it blocks downstream automation regardless of Critic score
**And** status is set to `waiting_for_review`
**And** a "Safeguard Triggered: Minors" flag is recorded

**Given** a safeguard has triggered
**When** the operator provides a manual "Override" decision (with mandatory note)
**Then** the run continues to the next stage

**Given** a scene receives a Critic score above the auto-approval threshold
**When** no safeguards have triggered
**Then** the scene status is set to `auto_approved`
**And** the decision is recorded as `system_auto_approved` in the `decisions` table

**Given** the Golden eval fixture set (from Story 4.1) is being maintained (FR30)
**When** the minors-in-policy-sensitive-contexts detection mechanism is active
**Then** at least 1 known-fail fixture of the "minors" category exists in `testdata/golden/` alongside the 3 standard categories (fact error, descriptor violation, weak hook) per NFR-T5

**Tests:** Unit — Safeguard trigger logic; override state transition; auto-approval threshold 판정 logic; Golden fixture set includes minors category.

---

### Epic 5: Media Generation (Phase B — Image & TTS)

#### Story 5.1: Common Rate-limiting & Exponential Backoff (Tier 1 Cross-Cutting — must land before other Phase B stories)

As a developer,
I want a common rate-limiter and retry infrastructure for all LLM calls,
So that the system gracefully handles provider quotas and transient errors before any Phase B media generation begins.

**Acceptance Criteria:**

**Given** any LLM client in `internal/llmclient/`
**When** it makes an external API call
**Then** it must pass through a common rate-limiter (Semaphore + Token Bucket)
**And** it implements exponential backoff with jitter on 429/500 errors
**And** every retry attempt and reason (rate_limit, timeout, etc.) is recorded in the run's observability data

**Given** N concurrent requests fire simultaneously against the shared DashScope limiter (AC-RL1)
**When** a 429 response is received
**Then** exponential backoff activates with deterministic timing via FakeClock (no real sleeps in tests)

**Given** TTS and ImageGen tracks run concurrently on the shared DashScope limiter (AC-RL2)
**When** both tracks compete for the same rate budget
**Then** the combined throughput respects the configured RPM limit (measured allocation within ±5% of target split)

**Given** a goroutine acquires the semaphore but the downstream provider is unresponsive (AC-RL3)
**When** 30 seconds elapse without release
**Then** the circuit breaker fires, the goroutine is cancelled, and the stage escalates to the operator (no goroutine leak, no deadlock)

**Tests:** Unit — Token bucket threshold crossing; backoff sequence timing (using FakeClock); concurrent goroutine contention test with shared limiter; circuit-break timeout test (FakeClock advance 30s → cancellation assertion).

---

#### Story 5.2: Parallel Media Generation Runner (Errgroup)

As a developer,
I want to execute image and TTS generation in parallel for each scene,
So that the overall pipeline wall-clock time is optimized.

**Acceptance Criteria:**

**Given** a run enters Phase B (image/tts)
**When** the Phase B runner executes
**Then** it uses `errgroup.Group` (NOT `errgroup.WithContext`) to spawn parallel tracks for image and TTS
**And** if one track fails, the other track continues to completion — one track's failure must NOT cancel the other (FR16)
**And** the assembly stage begins only after both tracks complete (whether success or failure)
**And** total elapsed wall-clock time is captured as an operational metric (NFR-P4 family, not a CI-enforced gate)
**And** both tracks use the rate-limiter infrastructure from Story 5.1

**Given** the image track fails but the TTS track succeeds
**When** both tracks have completed
**Then** the run records the image-track failure in observability (retry_reason, cost_usd)
**And** the run status transitions to `failed` with the image-track error
**And** the TTS artifacts are preserved (not rolled back)
**And** resume from this stage re-executes only the failed track

**Tests:** Unit — `errgroup.Group` concurrency test: inject failure into one track, assert the other completes; verify both tracks' observability rows are recorded; verify TTS artifacts survive image-track failure.

---

#### Story 5.3: Character Reference Selection & Search Result Cache

As an operator,
I want to choose a character reference from cached search results,
So that I can maintain visual continuity without redundant search costs.

**Acceptance Criteria:**

**Given** the operator requests a character search
**When** results are retrieved from DuckDuckGo
**Then** 10 candidates are surfaced to the operator via the `CharacterGroup` schema
**And** the search result is cached in a SQLite-based persistent cache table

**Given** a character reference is selected by the operator
**When** the choice is submitted
**Then** the `selected_character_id` is persisted in the run's state
**And** subsequent image generation story (5.4) uses this ID to fetch the canonical image

**Tests:** Integration — SQLite-based cache hit/miss behavior; `selected_character_id` persistence check.

---

#### Story 5.4: Frozen Descriptor Propagation & Per-Shot Image Generation (DashScope)

As a developer,
I want to generate 1–5 images per scene using shot-level visual descriptors and the Frozen Descriptor,
So that the character remains visually consistent across all shots and scenes.

**Acceptance Criteria:**

**Given** a run enters the image generation stage
**When** the image track processes each scene
**Then** it reads the shot breakdown from scenario.json (1–5 shots per scene, with operator overrides if present)
**And** for each shot, it composes the image prompt: Frozen Descriptor prefix + shot-level visual_descriptor
**And** for character-bearing shots, it uses Qwen-Image-Edit (reference-based) with `selected_character_id`
**And** for non-character shots, it uses Qwen-Image standard generation
**And** produced images are saved to `images/scene_{idx}/shot_{idx}.png`
**And** the `segments.shots` JSON array is updated with each shot's `image_path`, `duration_s`, `transition`

**Given** a 10-scene run with average 3 shots per scene
**When** image generation completes
**Then** ~30 image files exist across all scene directories
**And** all images share the same Frozen Descriptor prefix (cross-shot + cross-scene consistency)

**Tests:** Integration — DashScope client (mocked/recorded) verifies per-shot prompt composition, Frozen Descriptor prefix propagation, directory structure, segments.shots JSON integrity.

---

#### Story 5.5: Korean TTS with Regex Transliteration (DashScope)

As an operator,
I want Korean TTS generated with correct pronunciation of numbers and English terms,
So that the narration sounds natural and professional.

**Acceptance Criteria:**

**Given** a narration text contains numerals (e.g., "SCP-049") or English terms (e.g., "doctor")
**When** the Regex-based Korean transliteration engine executes
**Then** it converts text to Korean orthography (e.g., "에스씨피-공사구", "닥터") before calling TTS
**And** the DashScope TTS client produces a high-quality Korean audio file (MP3/WAV)

**Tests:** Unit — Transliteration engine regex coverage (currency, dates, numbers, common English terms).

---

### Epic 6: Web UI — Design System & App Shell

#### Story 6.1: Theme Engine & Global Styling (Dark-only)

As a designer,
I want a dark-only design system based on CSS variables and Geist typography,
So that the application has a consistent, premium feel.

**Acceptance Criteria:**

**Given** the theme engine is initialized
**When** the root element is rendered
**Then** it applies 15 semantic CSS color tokens (RGB triplets)
**And** it bundles and loads Geist Sans and Geist Mono typography with Korean fallbacks
**And** it implements an 8-level type scale and a 4px-base spacing scale (7 named tokens)

**Given** the `data-density` attribute is set on a container
**When** elements are rendered within it
**Then** the Affordance Density system (UX-DR48–DR50) scales padding and margins accordingly to balance information density.

**Tests:** Unit — CSS variable accessibility check. Visual — Manual verification of contrast ratios and spacing scale.

---

#### Story 6.2: SPA Architecture & Navigation Shell

As an operator,
I want a responsive application shell with a sidebar and 3-mode tab routing,
So that I can navigate between different workflows easily.

**Acceptance Criteria:**

**Given** the main layout is rendered
**When** the sidebar is interacted with
**Then** it can be expanded/collapsed and its state is persisted in localStorage
**And** it auto-collapses when the viewport width is less than 1280px

**Given** the application shell is active
**When** the user clicks a tab
**Then** React Router v7 navigates to `/production`, `/tuning`, or `/settings` without full page reload
**And** current active tab is highlighted

**Tests:** Unit — Router mapping and initial state loading. Integration — Sidebar persistency test.

---

#### Story 6.3: Keyboard Shortcut Engine (8-key system)

As an operator,
I want a global keyboard shortcut engine for core actions,
So that I can operate the tool efficiently without relying solely on the mouse.

**Acceptance Criteria:**

**Given** the shortcut engine is active
**When** a registered key is pressed (Enter, Esc, J, K, Tab, Ctrl+Z, Shift+Enter, S, 1-9/0)
**Then** the corresponding global or contextual action is triggered
**And** keyboard labels for these actions are visible in the UI (e.g., in ActionBars)

**Given** an input field or textarea has focus
**When** a global shortcut key (like 'J' or 'K') is pressed
**Then** the shortcut is suppressed and the character is typed into the field (Amelia's collision prevention)

**Tests:** Integration — Shortcut event bubbling, focus-trapping during input focus, and handler execution.

---

#### Story 6.4: Go Embed & Static File Serving

As an operator,
I want the web UI to be served directly from the Go binary,
So that I only need to manage a single file for deployment and execution.

**Acceptance Criteria:**

**Given** the Go backend is running in production mode
**When** a request is made to `http://localhost:port/`
**Then** it serves the embedded React SPA via `embed.FS`
**And** any unmatched routes are served by `index.html` (catch-all routing)

**Given** the system is in development mode
**When** the backend starts
**Then** it proxies API requests to the Vite dev server

**Tests:** Integration — Server status check, index.html fallback verification, and dev/prod proxy/serving logic.

---

#### Story 6.5: Playwright Smoke Test & UX Testing Infrastructure (FR52-web)

As a developer,
I want a Playwright smoke test and UX testing infrastructure,
So that the SPA is verified end-to-end and component tests have a solid foundation.

**Acceptance Criteria:**

**Given** the Go server is running with the embedded SPA (Story 6.4)
**When** `npx playwright test` executes (Chromium only, 1 spec)
**Then** the SPA loads without errors
**And** the `/production` route renders the Production tab content
**And** basic keyboard interaction (Enter, Esc) triggers the expected UI response (FR52-web)

**Given** `web/src/test/setup.ts` is configured with `renderWithProviders.tsx` (MemoryRouter + QueryClient wrapper)
**When** a component test runs via Vitest + RTL
**Then** components render correctly inside the provider wrapper
**And** at least 1 component test passes using the `renderWithProviders` utility

**Given** `testdata/contracts/` contains JSON Schema fixtures
**When** `npm run test` executes web-side contract tests
**Then** each fixture is validated by Zod schema parse without error (Go unmarshal ↔ Zod parse parity)

**Given** `styles/tokens.css` defines 16 semantic color tokens
**When** a CSS custom property verification test runs via RTL + `getComputedStyle()`
**Then** all 16 tokens return non-empty values in the rendered DOM

**Given** the ESLint keyboard invariance rule (UX-DR52) is configured
**When** `npm run lint` executes
**Then** any `onKeyDown` handler that binds `Enter` to a non-primary action or `Esc` to a non-secondary action causes lint failure

**Tests:** E2E — Playwright smoke (app loads, /production renders, keyboard responds). Unit — renderWithProviders component test, Zod contract parse, CSS token verification, ESLint keyboard invariance.

---

### Epic 7: Production Tab — Scenario Review & Character Selection

#### Story 7.1: Pipeline Dashboard & Run Status (StatusBar/RunCard)

As an operator,
I want to monitor the current run's progress and cost in real-time,
So that I can see the pipeline's status at a glance.

**Acceptance Criteria:**

**Given** a run is active
**When** the StatusBar is rendered
**Then** it shows the stage name, icon, elapsed time, and total cost
**And** the StageStepper visualizes 6 nodes in one of 4 states (pending, running, complete, failed)

**Given** the run inventory sidebar is open
**When** a run is listed
**Then** the RunCard shows the Run ID, SCP summary, and a compact stepper version.

**Tests:** Unit — Stepper state logic. Integration — Compact API endpoint verification for real-time polling (Winston's refinement).

---

#### Story 7.2: Scenario Inspector & Inline Editor

As an operator,
I want to inspect and edit the generated narration paragraphs inline,
So that I can refine the script before committing to image/TTS generation.

**Acceptance Criteria:**

**Given** the production view is open at the Scenario Review wait point
**When** I click a narration paragraph
**Then** it transforms into an inline editor with focused textarea
**And** visual feedback (border/background) indicates 'Editing Mode'

**Given** I finish editing (blur-save or Enter)
**When** the save is triggered
**Then** a 'Saving...' status is displayed
**And** the change is persisted to the backend via API
**And** if an error occurs, an error message is shown and the original text is preserved (Sally's feedback)

**Tests:** Integration — Inline edit persistence cycle and error state handling.

---

#### Story 7.3: Character Selection Interface (Candidate Grid)

As an operator,
I want to select a character reference from 10 generated candidates,
So that I can establish a consistent visual identity for the SCP.

**Acceptance Criteria:**

**Given** the Character Selection surface is active
**When** the 10 candidates are presented
**Then** the UI uses image preloading to ensure smooth rendering (Amelia's refinement)
**And** I can select a candidate using keys 1-9/0 or mouse click
**And** clicking 'Confirm' (Enter) persists the selection to the run's state

**Given** a character candidate is selected (UX-DR41, UX-DR62 — Vision Descriptor editing)
**When** the Vision Descriptor panel appears below the grid
**Then** a plain textarea is pre-filled with the auto-extracted Vision Descriptor for the selected character
**And** if a prior run for the same SCP ID exists, the previous descriptor is loaded as pre-fill instead (UX-DR62)
**And** the operator can edit the descriptor text inline (Tab to enter edit mode, blur-save on focus loss)
**And** Ctrl+Z reverts to the pre-fill text (UX-DR26)
**And** the edited descriptor is persisted as the Frozen Descriptor used verbatim in all subsequent image prompts (FR14)

**Given** no prior run exists for this SCP ID
**When** the Vision Descriptor panel loads
**Then** the auto-extracted descriptor from the VisualBreakdowner is shown as pre-fill
**And** the operator can edit before confirming

**Tests:** Integration — Selection state persistence and multi-candidate rendering; Vision Descriptor pre-fill from prior run; blur-save persistence; Frozen Descriptor propagation to image prompts (verified via segments.shots JSON).

---

#### Story 7.4: Progressive Failure Handling (FailureBanner)

As an operator,
I want clear feedback and remediation actions when a stage fails,
So that I can quickly resolve issues and resume the pipeline.

**Acceptance Criteria:**

**Given** a stage fails
**When** the FailureBanner is rendered
**Then** it prioritizes the error message and remediation hint
**And** it distinguishes visually between retryable errors (e.g., 429 - orange) and fatal errors (e.g., 400 - red) (John's refinement)
**And** it provides a 'Resume' button that triggers back-to-backend stage re-entry

**Tests:** Integration — Failure state UI consistency and resume flow.

---

#### Story 7.5: Onboarding & Continuity Experience

As an operator,
I want an onboarding guide and a continuity summary when resuming,
So that I always know what my next task is.

**Acceptance Criteria:**

**Given** I launch the tool for the first time
**When** the web UI loads
**Then** an onboarding modal provides a brief overview of the 3-mode workflow.

**Given** I return to the tool after a pause or run advancement
**When** I enter the Production tab
**Then** a 5s auto-dismissing banner summarizes 'What changed' since my last session (e.g., 'Scenario completed while you were away')

**Tests:** Unit — First-run detection logic. Integration — Continuity banner triggering.

---

### Epic 8: Batch Review & Decision Management

#### Story 8.1: Master-Detail Review Layout (SceneCard/DetailPanel)

As an operator,
I want a 30:70 review layout to manage scenes efficiently,
So that I can see the list of all scenes while focusing on one at a time.

**Acceptance Criteria:**

**Given** the Batch Review tab is open
**When** scenes are rendered
**Then** a Sidebar (30%) shows SceneCards (each showing shot thumbnail strip: 1–5 thumbnails per scene) and a DetailPanel (70%) shows the full content
**And** the DetailPanel shows the scene clip (shots composed with transitions + TTS audio) as an inline video player, or a scrollable shot gallery with transition indicators between shots
**And** I can navigate the list using 'J' (Next) and 'K' (Previous) keys

**Given** a scene has been regenerated
**When** I view its DetailPanel
**Then** a 'Before/After Diff' visualization is shown for both narration text and images (Sally's refinement)
**And** a toggle switch allows me to switch between version views.

**Given** scenes are loaded for batch review (FR31c)
**When** the system identifies high-leverage scenes (character first appearance, hook scene, act-boundary scenes)
**Then** these scenes are visually distinguished with a "High-Leverage" badge in the SceneCard list
**And** the DetailPanel shows an extended-detail surface for these scenes: larger image preview, full Critic sub-score breakdown (hook strength, fact accuracy, emotional variation, immersion with color coding), and a "Why high-leverage" annotation explaining the classification reason (e.g., "First appearance of SCP-049")
**And** high-leverage scenes are sorted to the top of the pending review queue

**Tests:** Integration — Navigation synchronization between Sidebar and DetailPanel; Diff visualization logic; high-leverage scene identification for character-first-appearance, hook, act-boundary; extended-detail surface rendering.

---

#### Story 8.2: Scene Review Actions & AudioPlayer

As an operator,
I want to listen to narration and commit review actions,
So that I can refine the audio-visual quality of each scene.

**Acceptance Criteria:**

**Given** a scene is selected
**When** the AudioPlayer is rendered
**Then** it allows toggling playback with 'Space'
**And** it displays a Seekbar for navigating the audio timeline (UX-DR20)
**And** it resets to 0:00 when a new scene is selected

**Given** a review action is triggered (Approve/Reject/Skip)
**When** the API call executes
**Then** a new record is created in the `decisions` table with the operator ID and timestamp
**And** the next scene is auto-selected for review.

**Given** the operator presses 'S' to skip a scene (FR35 — skip and remember)
**When** the skip action completes
**Then** a `decisions` row is created with `decision_type = 'skip_and_remember'`
**And** the skip reason pattern is stored (scene characteristics: Critic sub-scores, scene_index position, content flags)
**And** future runs can query the `decisions` table for `skip_and_remember` patterns to surface "Based on your past choices" hints (UX-DR55)

**Tests:** Integration — Audio sync status; decision persistence in DB; skip-and-remember pattern recording; pattern queryability for future hint generation.

---

#### Story 8.3: Decision Undo & Command Pattern

As an operator,
I want to undo my recent review decisions,
So that I can correct accidental clicks.

**Acceptance Criteria:**

**Given** I have made one or more review decisions
**When** I press 'Ctrl+Z'
**Then** the most recent decision is undone (up to 10 steps)
**And** the previous state is restored in the UI
**And** the database record is marked as `superseded_by` the undo event.

**Given** a decision resulted in file modification
**When** I undo it
**Then** the previous file state is restored from the versions folder (no hard deletion) (Winston's refinement).

**Tests:** Unit — Undo stack logic and 10-step limit. Integration — DB state restoration.

---

#### Story 8.4: Rejection & Regeneration Flow

As an operator,
I want to provide a reason when rejecting a scene,
So that the system can attempt regeneration with corrected context.

**Acceptance Criteria:**

**Given** I click 'Reject'
**When** the RejectionDialog appears
**Then** I must provide a reason string
**And** a regeneration task is dispatched to the background (Max 2 retries)

**Given** I enter a rejection reason
**When** the system analyzes it
**Then** it warns me if the reason is structurally similar to a previously failed attempt for the same scene (FR53).

**Tests:** Integration — Regeneration trigger and retry limit enforcement; similarity check logic.

---

#### Story 8.5: Batch "Approve All Remaining"

As an operator,
I want to approve all remaining unreviewed scenes at once,
So that I can move to the final assembly stage faster.

**Acceptance Criteria:**

**Given** unreviewed scenes exist
**When** I click 'Approve All Remaining'
**Then** a confirmation dialog appears
**And** upon confirmation, the system updates all remaining scenes to 'Approved' status
**And** updates are processed in Chunks of 50 to prevent DB locking (Amelia's refinement).

**Tests:** Integration — Bulk update reliability and data consistency after chunked processing.

---

#### Story 8.6: Decisions History & Timeline View

As an operator,
I want to view a history of all review decisions,
So that I can Audit my own workflow and understand the run's evolution.

**Acceptance Criteria:**

**Given** the History tab is open
**When** I view the timeline
**Then** it shows a chronological list of all decisions with their timestamps, types, and reasons
**And** I can filter the list by decision type (e.g., 'Rejections Only') or search through reasons (John's refinement).

**Tests:** Integration — Timeline query performance with large decision sets; filter logic.

---

#### Story 8.7: New Run Creation from UI

As an operator,
I want to create a new pipeline run directly from the web UI,
So that I can exercise the Production and Batch Review surfaces end-to-end without dropping into the terminal.

**Acceptance Criteria:**

**Given** the Production tab is open
**When** I click the `New Run` button in the sidebar header or press `⌘N` / `Ctrl+N`
**Then** an inline `role="alertdialog"` panel opens with a focused SCP-ID input
**And** the input is validated against the backend regex `^[A-Za-z0-9_-]+$` with a clear inline error for invalid characters

**Given** a valid SCP ID is entered
**When** I submit (click Create or press Enter)
**Then** the UI calls `POST /api/runs` with `{"scp_id": "..."}`, refreshes the sidebar inventory, auto-selects the newly created run via `?run=<id>`, and closes the panel
**And** on any failure (400 validation, network, 5xx) the panel surfaces a Fail-Loud-with-Fix inline error and remains open so the operator can correct and retry without reloading

**Given** the new run has been created
**When** `ProductionShell` renders for it
**Then** the view shows a pending empty-state card (stage=pending, status=pending) with guidance copy and a "Copy command" button that copies `pipeline resume <run-id>` to the clipboard
**And** the run is NOT auto-resumed — starting Phase A remains an explicit, cost-controlled terminal action (create-only scope)

**Scope boundary:** Create-only. This story picks up UX-DR68 (originally parked in Epic 7) and skips its V1 clipboard-copy intermediate because `POST /api/runs` already exists. Starting the run (`POST /api/runs/{id}/resume`) and any "Start now" UI affordance are deliberately out of scope; a future story may add a UI-triggered start once cost telemetry is judged sufficient.

**Tests:** Component — `NewRunPanel` alertdialog + focus trap + validation + submit + error branches. Unit — `keyboardShortcuts.ts` platform-aware `mod+n` normalization. Contract — new `createRunRequestSchema` / `createRunResponseSchema` parse the authoritative backend response. Integration — Sidebar click → panel → POST → inventory refetch → URL update → panel close. E2E — Playwright smoke `web/e2e/new-run-creation.spec.ts` covers the full flow end-to-end.

---

### Epic 9: Video Assembly & Compliance (Phase C)

#### Story 9.1: FFmpeg Two-Stage Assembly Engine

As a developer,
I want to assemble per-scene clips from shots+transitions+audio, then concat into the final video,
So that each scene has visual rhythm and the final output is upload-ready.

**Acceptance Criteria:**

**Given** all scenes are approved and each scene has 1–5 shot images + TTS audio
**When** the per-scene clip assembly executes
**Then** for each scene, FFmpeg composes the shots with their specified transitions:
  - **Ken Burns (pan/zoom)**: `zoompan` filter applied to the shot image for its duration
  - **Cross-dissolve**: `xfade` filter between consecutive shots (duration: 0.5s)
  - **Hard cut**: direct concatenation with no transition filter
**And** the scene's TTS audio is overlaid on the composed shot sequence
**And** the output is saved as `clips/scene_{idx}.mp4` (H.264 + AAC, 1080p)
**And** sync padding (stretch/silence) ensures audio and video stay aligned

**Given** all per-scene clips are assembled
**When** the final concatenation executes
**Then** FFmpeg concat demuxer merges all scene clips in scene_index order into `output.mp4`
**And** the total video duration equals the sum of all scene clip durations (no gap, no overlap)

**Given** a scene has only 1 shot (≤8s TTS)
**When** its scene clip is assembled
**Then** a single Ken Burns effect is applied to the image for the full TTS duration (no inter-shot transition needed)

**Tests:** Integration — FFmpeg per-scene clip generation (verify shot count in clip matches segments.shots); transition type application (zoompan for Ken Burns, xfade for cross-dissolve); final concat output plays without gaps; sync verification between audio and video tracks.

---

#### Story 9.2: Metadata & Attribution Bundle

As a developer,
I want to generate a metadata package with full attributions,
So that the output complies with SCP Wiki CC BY-SA 3.0 license.

**Acceptance Criteria:**

**Given** the assembly is complete
**When** the metadata builder executes
**Then** it produces `metadata.json` (YT-ready) and `manifest.json` (license audit)
**And** the manifest contains a full 'License Chain' including Source URLs and Author names for every used SCP component (Amelia's refinement).

**Tests:** Unit — Manifest schema validation; author mapping accuracy.

---

#### Story 9.3: Compliance Audit Logging

As a developer,
I want a permanent record of all LLM and media provider interactions,
So that I can verify compliance and debug provider shifts.

**Acceptance Criteria:**

**Given** any media generation or narration task
**When** the audit logger captures the event
**Then** it records the prompt, the provider used, the cost, and any blocked IDs (e.g., forbidden voices)
**And** the log is appended to the run's `audit.log` file.

**Given** a TTS request specifies a voice profile whose identifier is on the operator-maintained "blocked voice-ID" list (FR47)
**When** the TTSSynthesizer is invoked
**Then** the system rejects the request before making any external API call
**And** the stage fails with a clear error: "Voice profile '{id}' is blocked by compliance policy"
**And** the blocked-voice rejection event is recorded in the audit log
**And** the operator can update `config.yaml` to change the voice profile and resume

**Tests:** Integration — Audit log generation and content verification; blocked voice-ID rejection at runtime; error message clarity; resume after voice-ID fix.

---

#### Story 9.4: Pre-Upload Compliance Gate

As an operator,
I want a final manual check of the video and metadata,
So that I can ensure everything is correct before public upload.

**Acceptance Criteria:**

**Given** Phase C is complete
**When** the Compliance Gate wait point is reached
**Then** the UI presents a 'Final Review' surface with video preview and metadata checklist
**And** the 'Ready-for-upload' status is NOT set until I verify all items and click 'Finalize'.

**Tests:** Integration — Compliance gate state movement and final run completeness check.

---

### Epic 10: Tuning, Settings & CI Pipeline

#### Story 10.1: Settings Dashboard — LLM & Provider Config

As an operator,
I want to manage system settings and provider configurations via a dashboard,
So that I can tune performance and costs without editing raw files.

**Acceptance Criteria:**

**Given** the Settings tab is open
**When** I modify a value (e.g., Gemini model ID or a cost cap)
**Then** the change is validated and persisted to `config.yaml` or `.env` as appropriate.

**Given** active runs are in progress
**When** a configuration change is saved
**Then** the change is queued for the next stage entry or the next new run to prevent mid-stage state corruption (Winston's refinement).

**Given** the current cost usage
**When** I view the settings
**Then** a visual indicator shows the remaining budget against the hard/soft caps (Mary's refinement).

**Tests:** Integration — Config persistence cycle; active run queue logic.

---

#### Story 10.2: Tuning Surface — Prompt & Rubric Lab

As an operator,
I want to edit agent prompts and rubrics with immediate feedback,
So that I can continuously improve the pipeline's output quality.

**Acceptance Criteria:**

**Given** the Tuning interface is open
**When** I modify an agent's prompt
**Then** a 'Fast Feedback' run can be triggered, which executes the specific stage against a sample of 10 scenes instead of the full run (Amelia's refinement)
**And** any saved change automatically suggests a Shadow Eval run (Epic 4) to check for regressions.

**Given** a prompt is saved
**When** a new run starts
**Then** the run is tagged with the prompt version (timestamp/short-sha) to enable statistical quality comparison (Mary's refinement).

**Tests:** Integration — Prompt versioning in run metadata; Fast Feedback execution.

---

#### Story 10.3: Database Vacuum & Data Retention (Soft Archive)

As an operator,
I want the system to manage its disk footprint by archiving old data,
So that I don't run out of local storage over months of operation.

**Acceptance Criteria:**

**Given** a run or artifact exceeds the configured retention period
**When** the `pipeline clean` utility runs
**Then** it performs a 'Soft Archive': artifact files are moved/deleted, but the database records (runs, segments) are preserved with their file references set to NULL (Winston's refinement)
**And** the SQLite database performs a `VACUUM` during idle time to optimize size.

**Tests:** Unit — Retention calculation logic. Integration — File/Null-ref cycle; VACUUM invocation.

---

#### Story 10.4: Golden/Shadow CI Quality Gates (Epic 1 CI Extension)

As a developer,
I want Golden and Shadow eval tests to run automatically in CI alongside the existing pipeline (from Epic 1),
So that Critic prompt changes never silently degrade quality.

**Acceptance Criteria:**

**Given** a PR changes files under `internal/pipeline/agents/critic*` or `testdata/golden/`
**When** the CI workflow (GitHub Actions) triggers
**Then** it executes Golden eval (detection rate ≥ 80%) and Shadow eval (zero false-reject regressions on N=10 recent passed scenes)
**And** it fails the build if the pass rate decreases (NFR-T1)
**And** on failure, it produces a 'Failed Scenes Summary' including a diff of the problematic outputs directly in the CI report

**Note:** The CI pipeline skeleton (4-job structure, contract tests, E2E smoke, layer-import linter) was established in Epic 1 Story 1.4. This story adds Golden/Shadow eval as additional CI gates once Epic 4 quality infrastructure is complete.

**Tests:** Integration — CI workflow failure/success propagation; report artifact generation.

---

#### Story 10.5: Data Export to JSON (FR44)

As an operator,
I want to export per-run decisions and artifact data to JSON files on demand,
So that I can analyze data externally or archive it for offline review.

**Acceptance Criteria:**

**Given** a completed run exists
**When** `pipeline export --run-id scp-049-run-3 --type decisions` is executed
**Then** all decisions for that run are exported to `{output_dir}/scp-049-run-3/export/decisions.json`
**And** the JSON matches the versioned envelope schema (`{"version": 1, "data": [...]}`)
**And** each decision row includes: target item, decision type, timestamp, optional note, superseded_by (if undone)

**Given** `pipeline export --run-id scp-049-run-3 --type artifacts` is executed
**When** the export completes
**Then** artifact metadata (scenario.json path, image paths, TTS paths, metadata.json, manifest.json) is exported as JSON
**And** file paths are relative to the run output directory

**Given** `--format csv` flag is passed
**When** the export executes
**Then** the same data is exported in CSV format (alternative to JSON)

**Tests:** Integration — JSON export schema validation; round-trip export-then-read test; CSV format correctness.

---

### End of Epic & Story Breakdown








