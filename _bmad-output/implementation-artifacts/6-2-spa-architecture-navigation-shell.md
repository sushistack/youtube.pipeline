# Story 6.2: SPA Architecture & Navigation Shell

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want a responsive application shell with a sidebar and 3-mode tab routing,
so that I can navigate between different workflows easily.

## Prerequisites

**Hard dependency:** Story 1.1 established the React/Vite frontend scaffold and testing baseline. This story must extend that scaffold rather than replace it.

**Soft dependency:** Story 6.1 defines the final theme token system. If 6.1 is not implemented yet, this story may use the existing dark root styling in `web/src/index.css`, but it must not invent a competing token architecture. Keep shell styling compatible with the upcoming CSS-variable theme engine.

## Acceptance Criteria

1. **AC-SHELL-GRID-LAYOUT:** the SPA shell renders with a CSS Grid layout where the sidebar is `220px` expanded and `48px` collapsed, and the content pane remains flexible with a minimum effective width of 70% of the viewport.

   Required outcome:
   - the shell uses a stable left-navigation + content-pane layout, not ad-hoc per-route wrappers
   - the sidebar width is controlled by a single collapsed/expanded state source
   - the content area visually dominates the shell and stays usable at desktop widths

   Rules:
   - implement the layout in the shared shell layer, not separately inside each route page
   - prefer CSS Grid for the outer shell and keep the route content mounted inside the main pane
   - do not introduce mobile-only layouts; PRD explicitly scopes the UI to desktop-class local usage

   Tests:
   - `App` route-shell smoke test verifies shell renders on the default route
   - unit test verifies sidebar collapsed state changes the shell data attribute or class contract

2. **AC-SIDEBAR-PERSIST-AUTO-COLLAPSE:** the sidebar can be expanded and collapsed by the user, persists its preference in localStorage, and auto-collapses below `1280px` viewport width.

   Required outcome:
   - the operator can toggle the sidebar without leaving the current route
   - the persisted UI state survives a full browser refresh
   - sub-1280px viewport widths render the collapsed shell regardless of the saved desktop preference

   Rules:
   - persist shell UI state via Zustand `persist` middleware rather than custom `localStorage` helpers
   - preserve the stored preference for wider screens; the narrow-screen collapse is a responsive override, not destructive state loss
   - do not use `ResizeObserver`; UX spec already permits a CSS/media-query-driven collapse rule

   Tests:
   - unit test verifies persisted initial state hydration for `sidebar_collapsed`
   - integration test verifies narrow viewport forces collapsed presentation

3. **AC-THREE-ROUTE-SPA:** the application uses React Router v7 with `BrowserRouter` and declarative routes for `/production`, `/tuning`, and `/settings`, with navigation that does not reload the page.

   Required outcome:
   - `App.tsx` owns the `BrowserRouter` + `Routes` composition
   - each route renders its own shell content component (`ProductionShell`, `TuningShell`, `SettingsShell`)
   - route changes happen client-side and preserve SPA behavior

   Rules:
   - use the traditional declarative router pattern (`<BrowserRouter>`, `<Routes>`, `<Route>`) from the architecture, not `createBrowserRouter`
   - add an index/default redirect to `/production` so the shell has one canonical landing route
   - avoid full-page anchors or `window.location` navigation for tab switching

   Tests:
   - router mapping test verifies `/production`, `/tuning`, `/settings` each render the expected shell heading or landmark
   - navigation interaction test verifies clicking sidebar tabs changes content without a document reload

4. **AC-ACTIVE-TAB-HIGHLIGHT:** the active navigation item is visually highlighted and exposed accessibly so the operator can immediately tell which workflow is open.

   Required outcome:
   - the active tab has a distinct visual treatment in both expanded and icon-only sidebar modes
   - navigation semantics remain keyboard-accessible
   - active-state styling follows the current route rather than duplicated local component state

   Rules:
   - use `NavLink` active-state behavior rather than maintaining a parallel `activeTab` store field
   - keep labels available to assistive tech even when the sidebar is collapsed
   - do not block Story 6.3 keyboard work by baking shortcut logic into the sidebar component

   Tests:
   - integration test verifies the active nav item exposes `aria-current="page"`
   - unit test verifies collapsed mode still renders accessible nav labels/tooltips or equivalent text contract

5. **AC-UI-STORE-CONVENTION:** shell-only UI state is centralized in a Zustand store persisted under a stable localStorage key and follows the architecture's naming and file-location conventions.

   Required outcome:
   - create a dedicated `web/src/stores/useUIStore.ts`
   - store shape includes `sidebar_collapsed` and the actions needed by the shell
   - the implementation leaves room for future shell concerns such as last active route and keyboard mode without rewriting the store structure

   Rules:
   - keep state names in snake_case to match the architecture convention
   - keep server state out of this store; TanStack Query owns server data in later stories
   - use a single persist name such as `youtube-pipeline-ui` unless an existing local convention supersedes it

   Tests:
   - store unit test verifies default values and toggle action behavior
   - store persistence test verifies JSON serialization uses the configured persist key

## Tasks / Subtasks

- [x] **T1: Install and wire shell dependencies** (AC: #3, #5)
  - [x] Add `react-router` and `zustand` to `web/package.json`
  - [x] Keep React 19 / Vite 7.3 compatibility intact
  - [x] Do not introduce data-router or SSR-only packages for this story

- [x] **T2: Create the shared UI store** (AC: #2, #5)
  - [x] Add `web/src/stores/useUIStore.ts` using Zustand `persist`
  - [x] Include `sidebar_collapsed` plus shell actions such as `toggle_sidebar` and `set_sidebar_collapsed`
  - [x] Use a stable persist name and isolate browser-storage access behind Zustand middleware

- [x] **T3: Build the sidebar and route shells** (AC: #1, #2, #4)
  - [x] Create `web/src/components/shared/Sidebar.tsx`
  - [x] Create `web/src/components/shells/ProductionShell.tsx`
  - [x] Create `web/src/components/shells/TuningShell.tsx`
  - [x] Create `web/src/components/shells/SettingsShell.tsx`
  - [x] Keep shell components intentionally light: route-level placeholders are enough for this story

- [x] **T4: Replace the stub app with the SPA shell** (AC: #1, #3, #4)
  - [x] Update `web/src/App.tsx` to own `BrowserRouter`, `Routes`, `Route`, and default redirect
  - [x] Ensure route navigation is driven by `NavLink`
  - [x] Keep page transitions client-side with no full document reload

- [x] **T5: Add shell styles without pre-empting Story 6.1** (AC: #1, #2, #4)
  - [x] Extend `web/src/index.css` or a focused shell stylesheet for CSS Grid layout and responsive sidebar behavior
  - [x] Use `data-sidebar` / `data-collapsed` style hooks or an equivalent stable contract
  - [x] Respect the upcoming theme-engine boundary instead of hardcoding a second design system

- [x] **T6: Add frontend tests for router and persistence** (AC: #2, #3, #4, #5)
  - [x] Expand `web/src/App.test.tsx` for route rendering and active tab behavior
  - [x] Add a store test file if needed for persist behavior
  - [x] Use React Testing Library and jsdom, following the existing frontend test setup

### Review Findings

- [x] [Review][Patch][HIGH] Toggle on narrow viewport destructively overwrites persisted desktop preference — gate `on_toggle` and `aria/title` on `forced_collapsed` [web/src/components/shared/Sidebar.tsx:33-47, web/src/components/shared/AppShell.tsx:7-10]
- [x] [Review][Patch][MED] NavLink `aria-label` overrides visible text — only set `aria-label` when collapsed [web/src/components/shared/Sidebar.tsx:58]
- [x] [Review][Patch][MED] Toggle button missing `aria-expanded` state attribute [web/src/components/shared/Sidebar.tsx:33]
- [x] [Review][Patch][MED] NavLink lacks `end` prop — `/production` will stay active for any future nested path [web/src/components/shared/Sidebar.tsx:52]
- [x] [Review][Patch][MED] Collapsed labels hidden via `opacity:0` only — still in a11y tree and may overflow [web/src/index.css:276-280, 300-304]
- [x] [Review][Patch][MED] Unknown routes render blank content area — add catch-all redirect [web/src/App.tsx:10-17]
- [x] [Review][Patch][MED] Persist hydration test does not await `useUIStore.persist.rehydrate()` — relies on sync storage timing [web/src/stores/useUIStore.test.ts:23-37]
- [x] [Review][Patch][MED] No test that desktop preference survives a narrow→wide episode [web/src/App.test.tsx:96-107]
- [x] [Review][Patch][LOW] CSS `@media (max-width: 1279px)` and JS `'(max-width: 1279px)'` can desync at fractional widths — use `(width < 1280px)` range syntax [web/src/hooks/useViewportCollapse.ts:3, web/src/index.css:287]
- [x] [Review][Patch][LOW] `get_matches` not guarded against `matchMedia` throwing/returning undefined [web/src/hooks/useViewportCollapse.ts:10]
- [x] [Review][Patch][LOW] Tests do not cover the 1279/1280 boundary [web/src/App.test.tsx]
- [x] [Review][Patch][LOW] No test exercises `matchMedia` `change` event subscriber path [web/src/App.test.tsx:24]
- [x] [Review][Patch][LOW] Persist key literal duplicated in test #2 — import `UI_STORE_PERSIST_KEY` [web/src/stores/useUIStore.test.ts:25]
- [x] [Review][Patch][LOW] Add `partialize` to whitelist `sidebar_collapsed` so unknown persisted keys are stripped [web/src/stores/useUIStore.ts:12-29]
- [x] [Review][Patch][LOW] Sync existing test fixtures with the new range-syntax media query [web/src/App.test.tsx:18]
- [x] [Review][Defer][LOW] localStorage `QuotaExceededError` not handled by zustand persist — out-of-scope, not story-specific
- [x] [Review][Defer][LOW] Corrupt persisted JSON / wrong-type values not coerced — needs `migrate`, follow-up work
- [x] [Review][Defer][LOW] No SSR hydration two-pass guard in `useViewportCollapse` — no SSR planned for this story
- [x] [Review][Defer][LOW] Route change not announced via `aria-live` — belongs with Story 6.3 keyboard/a11y pass
- [x] [Review][Defer][LOW] `/` → `/production` `Navigate` drops `search`/`hash` — no deep links use them yet

## Dev Notes

### Story Intent and Scope Boundary

- Epic 6 is the shared web foundation epic. Story 6.2 should deliver the reusable SPA shell only: layout, route boundaries, sidebar navigation, and persisted UI state. It should not pull in keyboard shortcuts (Story 6.3), Go embed serving (Story 6.4), or production/tuning feature surfaces from Epics 7-10.
- The current frontend is still at the Story 1.1 scaffold stage: `web/src/App.tsx` renders a single heading and there are no router, shell, or store files yet. This story is expected to establish that structure from scratch, but within the already-selected React/Vite architecture.

### Architecture Guardrails

- Use React Router v7 with the declarative `BrowserRouter` + `Routes` + `Route` pattern. The architecture explicitly rejects `createBrowserRouter` for this project because the shell only needs simple client-side route composition.
- Use Zustand `persist` middleware for shell UI state. The architecture example already defines `sidebar_collapsed` in `useUIStore.ts` and persists it under a localStorage key.
- Keep server state out of Zustand. Future run-status polling belongs in TanStack Query hooks, not this story's UI store.
- Follow the documented frontend structure:
  - `web/src/components/shells/`
  - `web/src/components/shared/`
  - `web/src/stores/`
  - `web/src/App.tsx`
- State fields should remain snake_case to align with the architecture convention and future API-facing code.

### UX Guardrails

- UX spec locks the shell layout to a persistent collapsible sidebar plus dominant content pane. Sidebar widths are `220px` expanded and `48px` collapsed.
- Auto-collapse below `1280px` is explicitly allowed via CSS media query; no `ResizeObserver` is required.
- Content area must stay at or above an effective 70% viewport share on the production-facing shell.
- The three top-level cognitive modes are exactly `/production`, `/tuning`, and `/settings`. Avoid introducing extra top-level routes in this story.
- The active tab highlight should derive from routing state so future keyboard and onboarding work can trust one navigation truth.

### Implementation Guidance

- Replace the current `App.tsx` stub with a route-owning shell component tree.
- Keep shell route components intentionally placeholder-level for now: headings, short body copy, and layout landmarks are enough to satisfy the route contract for Story 6.2.
- Prefer `NavLink` for sidebar items because React Router v7 automatically exposes active-state styling hooks and `aria-current="page"`.
- Model responsive behavior as:
  - persisted user preference in Zustand
  - visual forced-collapse override under `1279px`
  - automatic return to stored preference when the viewport is wide again
- A reasonable store shape for this story is:

```ts
interface UIState {
  sidebar_collapsed: boolean
  toggle_sidebar: () => void
  set_sidebar_collapsed: (next: boolean) => void
}
```

- If the implementation needs a shell wrapper component, keep it under `components/shells/` or `components/shared/` rather than introducing a parallel top-level folder.

### Testing Requirements

- Use the existing Vitest + React Testing Library + jsdom stack from Story 1.1.
- Router tests can use memory-history helpers or render at specific URLs, but production code must use `BrowserRouter`.
- Verify behavior, not implementation details:
  - route-specific content renders for each path
  - `aria-current="page"` marks the active route
  - persisted state restores the sidebar preference
  - narrow viewport styling forces collapsed presentation
- Avoid brittle snapshot-only coverage for the shell layout.

### Previous Story Intelligence

**Story 1.1 established the frontend baseline that this story must extend:**

- Vite is pinned to `^7.3.0`; do not upgrade to Vite 8.x.
- The frontend uses React 19 and jsdom-based Vitest tests.
- `web/src/App.tsx`, `web/src/main.tsx`, and `web/src/index.css` already exist and should be evolved in place.
- `web/src/App.test.tsx` is currently a minimal render smoke test and is the natural starting point for SPA-shell coverage.

### Git Intelligence

Recent repo history is backend-heavy (`Epic 4` and `Epic 5` implementations). There is no later frontend-shell commit pattern to copy yet, so the safest move is to follow the architecture's prescribed folder layout and naming exactly for the first real SPA shell pass.

### Latest Technical Note

- React Router's current v7 docs still support the declarative `BrowserRouter` + `Routes` pattern, which matches the architecture choice for this story. `NavLink` remains the correct primitive for active-route highlighting and automatically applies `aria-current="page"` when active.
- Zustand's official persist docs still recommend `persist` with `createJSONStorage`, and localStorage remains the default synchronous browser storage for persisted UI state.

Official references:
- React Router `BrowserRouter`: https://api.reactrouter.com/v7/functions/react-router.BrowserRouter.html
- React Router `Routes`: https://api.reactrouter.com/v7/functions/react-router.Routes.html
- React Router `NavLink`: https://reactrouter.com/api/components/NavLink
- Zustand persist middleware: https://zustand.docs.pmnd.rs/reference/integrations/persisting-store-data

### Project Structure Notes

- Current relevant files:
  - `web/src/App.tsx`
  - `web/src/main.tsx`
  - `web/src/index.css`
  - `web/src/App.test.tsx`
- Expected new files for this story:
  - `web/src/components/shared/Sidebar.tsx`
  - `web/src/components/shells/ProductionShell.tsx`
  - `web/src/components/shells/TuningShell.tsx`
  - `web/src/components/shells/SettingsShell.tsx`
  - `web/src/stores/useUIStore.ts`
- Keep route-shell placeholders thin so later stories can add slot content without reworking the shell contract.

### References

- Epic 6 story definition: [_bmad-output/planning-artifacts/epics.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/epics.md)
- Sprint prompt shorthand: [_bmad-output/planning-artifacts/sprint-prompts.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/sprint-prompts.md)
- PRD web UI surface: [_bmad-output/planning-artifacts/prd.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/prd.md)
- Architecture frontend stack and routing/store conventions: [_bmad-output/planning-artifacts/architecture.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/architecture.md)
- UX shell layout and responsive behavior: [_bmad-output/planning-artifacts/ux-design-specification.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/ux-design-specification.md)
- Current frontend scaffold: [web/package.json](/home/jay/projects/youtube.pipeline/web/package.json), [web/src/App.tsx](/home/jay/projects/youtube.pipeline/web/src/App.tsx), [web/src/App.test.tsx](/home/jay/projects/youtube.pipeline/web/src/App.test.tsx), [web/src/index.css](/home/jay/projects/youtube.pipeline/web/src/index.css)

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- Create-story workflow analysis on 2026-04-19
- Local artifact review: `epics.md`, `prd.md`, `architecture.md`, `ux-design-specification.md`, `sprint-status.yaml`
- Current frontend scaffold review: `web/package.json`, `web/src/App.tsx`, `web/src/App.test.tsx`, `web/src/index.css`
- Official docs review: React Router v7 (`BrowserRouter`, `Routes`, `NavLink`), Zustand persist middleware

### Completion Notes List

- Comprehensive Story 6.2 implementation context created for the first SPA shell pass
- Scope boundary explicitly separated from Stories 6.1, 6.3, and 6.4 to avoid shell-layer scope creep
- Current frontend gap documented so the dev agent can build the shell from the existing minimal scaffold
- Routing, active-tab, and persisted-state guardrails aligned with architecture and official library docs
- Implemented a shared SPA shell with BrowserRouter, nested route shells, and NavLink-based workflow navigation
- Added a persisted Zustand UI store for `sidebar_collapsed` and a viewport override hook that forces collapsed presentation below 1280px
- Expanded frontend test coverage for redirects, route rendering, active navigation semantics, viewport collapse, and store persistence
- Verified the frontend shell with Vitest, TypeScript production build, and ESLint before moving the story to review

### File List

- web/package.json
- web/package-lock.json
- web/src/App.tsx
- web/src/App.test.tsx
- web/src/index.css
- web/src/components/shared/AppShell.tsx
- web/src/components/shared/Sidebar.tsx
- web/src/components/shells/ProductionShell.tsx
- web/src/components/shells/TuningShell.tsx
- web/src/components/shells/SettingsShell.tsx
- web/src/hooks/useViewportCollapse.ts
- web/src/stores/useUIStore.ts
- web/src/stores/useUIStore.test.ts
- web/src/test/setup.ts
- _bmad-output/implementation-artifacts/sprint-status.yaml
- _bmad-output/implementation-artifacts/6-2-spa-architecture-navigation-shell.md

### Change Log

- 2026-04-19: Implemented Story 6.2 SPA shell, persisted UI store, responsive collapse behavior, and frontend test coverage.
- 2026-04-19: Verified Story 6.2 with lint, full frontend tests, and production build; marked ready for review.
- 2026-04-19: Code review resolved 15 patches — forced-collapse toggle gate, NavLink `end`/`aria-label` scoping, `aria-expanded`, catch-all route, `visibility: hidden` for collapsed labels, range-syntax media query `(width < 1280px)`, `matchMedia` try/catch, `partialize` for persisted state, boundary + live change-event + preference-survival tests, awaited `persist.rehydrate()`. Status moved to done.
