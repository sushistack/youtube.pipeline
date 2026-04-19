# Story 6.3: Keyboard Shortcut Engine

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As an operator,
I want a global keyboard shortcut engine for core actions,
so that I can operate the tool efficiently without relying solely on the mouse.

## Prerequisites

**Stories 6.1 and 6.2 are practical dependencies for this story.** Story 6.3 supplies the shared keyboard interaction layer for the entire Epic 6 web shell, but it assumes the design-token vocabulary, shell layout, and route structure from Stories 6.1 and 6.2 exist or are being created in the same branch.

- Story 6.1 defines the design-system language this story must honor: dark-only tokens, focus-visible treatment, action hierarchy, and persistent inline shortcut affordances.
- Story 6.2 defines the app-shell structure that will host global shortcut registration: `/production`, `/tuning`, `/settings`, sidebar + content layout, and viewport-aware shell behavior.
- The current repository still contains only the minimal Vite scaffold in `web/src/App.tsx`, `web/src/index.css`, and a basic flat ESLint config in `web/eslint.config.js`. Do not assume hooks, shared components, router setup, or shadcn/ui wrappers already exist in code just because Epic 6 planning documents describe them.
- This story should create reusable keyboard infrastructure and hint primitives, not hard-code Production-only behavior into `App.tsx`.

**Current codebase gap to close deliberately:** the product docs require a keyboard-first operator workflow, but the current web app has no route shell, no shared state, no keyboard hook, no ActionBar component, and no lint guardrail preventing semantic drift of key meanings. Story 6.3 must establish those primitives in the correct `web/src/` locations so later stories can consume them rather than re-implementing ad hoc `onKeyDown` handlers.

## Acceptance Criteria

Unless stated otherwise, new tests live beside the code under test, use Vitest + React Testing Library, import the shared `web/src/test/setup.ts` fetch blocker, and avoid real timers where deterministic event sequencing is sufficient. Follow the existing TypeScript + ESLint flat-config style already present in the repo. Keep implementation browser-only; do not add server-side keyboard assumptions.

1. **AC-GLOBAL-8-KEY-ENGINE:** implement a reusable global shortcut engine that registers the required 8-key set and dispatches the matching action when active.

   Required outcome:
   - registered keys cover `Enter`, `Escape`, `Tab`, `Ctrl+Z`, `Shift+Enter`, `S`, `J`, `K`, and digit selection `1-9` plus `0`
   - the engine supports both global actions and contextual actions, so later screens can bind the same key set differently within a scoped surface
   - key matching is normalized at the engine boundary (`event.key`, modifier state, and prevent-default behavior handled centrally rather than reimplemented per component)
   - the engine exposes a clean consumer API such as `useKeyboardShortcuts(...)` or an equivalent provider/registry pattern under `web/src/hooks/` or `web/src/lib/`

   Rules:
   - `Shift+Enter` must be treated as distinct from bare `Enter`
   - digit bindings must support keyboard row digits at minimum; numpad support is optional unless it comes nearly free from normalization
   - `Ctrl+Z` must only fire when `ctrlKey === true`; do not silently alias plain `z`
   - registration/unregistration must be cleanup-safe so route changes and conditional UI do not leak listeners

   Tests:
   - `useKeyboardShortcuts` registers all required combinations and invokes the mapped handler exactly once per keypress
   - cleanup removes listeners on unmount or dependency change
   - `Shift+Enter` and `Enter` do not cross-trigger
   - digit shortcuts dispatch the correct index for both `1-9` and `0`

2. **AC-INPUT-COLLISION-PREVENTION:** global shortcuts are suppressed when focus is inside an editable control, so typing into form fields never triggers navigation or decision actions.

   Required outcome:
   - when focus is inside `input`, `textarea`, or `contenteditable` elements, letter-based navigation shortcuts such as `J`, `K`, and `S` are ignored by the global engine
   - the typed character still reaches the control normally
   - Enter/Escape handling remains explicit and safe: if a focused field or local surface owns the event, the global engine must not double-handle it

   Rules:
   - editable-target detection must be centralized in one helper, not open-coded in every handler
   - collision prevention is about protecting text entry, not disabling the browser's normal tab-focus model
   - do not suppress shortcuts merely because focus is on a button, tab, or other non-editable control

   Tests:
   - pressing `j` inside an input types `"j"` and does not call the global `J` handler
   - pressing `k` inside a textarea types `"k"` and does not call the global `K` handler
   - pressing `Enter` in an editable field does not accidentally invoke a global primary action unless the focused surface intentionally opts into it
   - a `contenteditable` element is treated the same as `input`/`textarea`

3. **AC-PERSISTENT-KEY-LABELS:** UI actions that participate in the shortcut system render their key labels visibly and consistently in an ActionBar-style affordance.

   Required outcome:
   - create a reusable hint-bearing action primitive for inline shortcut labels, aligned with the planned `ActionBar` usage
   - labels are always visible without hover, following the documented Linear-style inline hint pattern
   - the visible label format is consistent across actions, e.g. `[Enter] Approve`, `[Esc] Reject`, `[Tab] Edit`, `[S] Skip`, `[Ctrl+Z] Undo`
   - the primitive is reusable for later scene-review, character-pick, and timeline surfaces

   Rules:
   - implement this as a shared component or button variant under `web/src/components/shared/` or `web/src/components/ui/`; do not hard-code raw spans in each feature screen
   - if Story 6.1's button variants are not yet present, create the minimum shared abstraction needed for a `ghost-with-hint` style rather than baking final visuals into business components
   - the label text must remain present in the DOM for RTL assertions; do not rely on tooltip-only rendering

   Tests:
   - rendering the hint action shows both the visible key label and the action text
   - keyboard-hint markup is stable enough for later RTL count assertions around primary actions
   - ActionBar-style grouping renders the expected ordered labels for Enter/Esc/Tab/S/Ctrl+Z

4. **AC-KEY-MEANING-INVARIANCE-LINT-RULE:** add a custom ESLint rule that enforces keyboard shortcut semantic invariance for the two highest-risk keys: Enter and Escape.

   Required outcome:
   - `npm run lint` fails when a React `onKeyDown` binding or shortcut registration maps `Enter` to a non-primary action
   - `npm run lint` fails when a binding maps `Escape`/`Esc` to a non-secondary action
   - compliant bindings for `Enter=primary` and `Esc=secondary` pass lint
   - the rule is co-located with the keyboard engine code and wired through the repo's flat ESLint config in `web/eslint.config.js`

   Rules:
   - the rule only needs to cover the patterns this codebase will actually use in Epic 6+, such as shortcut registration objects and JSX key handlers; do not over-engineer a generic JavaScript theorem prover
   - the lint rule should emit actionable messages that explain the invariance requirement and reference the UX intent
   - avoid false positives on unrelated uses of the words "Enter", "Esc", "primary", or "secondary" in plain strings/comments

   Tests:
   - rule test fixture where `Enter` triggers `"approve"` or `"primary"` passes
   - rule test fixture where `Enter` triggers `"reject"` or `"secondary"` fails
   - rule test fixture where `Escape` triggers `"reject"` or `"secondary"` passes
   - rule test fixture where `Escape` triggers `"approve"` or `"primary"` fails

5. **AC-INTEGRATION-READY-FOUNDATION:** the shortcut engine integrates cleanly with Epic 6's shell foundations without hijacking default accessibility behavior or later route-specific composition.

   Required outcome:
   - the keyboard layer operates independently of normal Tab navigation
   - consumers can enable/disable shortcuts by route or surface without reloading the app
   - the engine is written as foundational infrastructure for later stories 7.x and 8.x, which rely heavily on J/K navigation, Enter/Esc decisions, and digit selection

   Rules:
   - do not bind listeners in multiple feature components for the same top-level shortcut set
   - do not create global mutable singleton state outside React unless there is a clear lifecycle reason and tests cover teardown
   - avoid dependencies that are not already justified by Epic 6; prefer React, TypeScript, and the existing ESLint/Vitest stack

   Tests:
   - integration test proves a mounted shortcut surface responds to Enter/Esc while ordinary tab-focus movement still works
   - route/surface toggle test proves handlers stop firing when the owning surface unmounts or disables the shortcut set

## Tasks / Subtasks

- [x] **T1: Create shared keyboard normalization + registration layer** (AC: #1, #5)
  - [x] Add a reusable keyboard engine under `web/src/hooks/` and/or `web/src/lib/` (`useKeyboardShortcuts.ts`, `keyboardShortcuts.ts`, or equivalent).
  - [x] Centralize key normalization for Enter/Escape/Tab/Ctrl+Z/Shift+Enter/S/J/K/1-9/0.
  - [x] Ensure listener setup/cleanup is lifecycle-safe and testable.

- [x] **T2: Add editable-target suppression helper** (AC: #2)
  - [x] Create a helper such as `isEditableEventTarget(target)` in `web/src/lib/` or alongside the hook.
  - [x] Apply the helper inside the engine before dispatching letter-based/global actions.
  - [x] Cover `input`, `textarea`, and `contenteditable` with tests.

- [x] **T3: Create shared inline shortcut-hint UI primitive** (AC: #3)
  - [x] Add a shared component or button variant for always-visible shortcut labels, aligned with the ActionBar pattern planned in Epic 7/8.
  - [x] Keep markup reusable so later surfaces can render `[Key] Label` affordances without duplicating structure.
  - [x] Add RTL tests for visible labels and ActionBar-style grouping order.

- [x] **T4: Implement custom ESLint keyboard invariance rule** (AC: #4)
  - [x] Add a local ESLint rule module under `web/` that checks Enter/Escape semantic mappings.
  - [x] Wire the rule into `web/eslint.config.js` using the existing flat-config setup.
  - [x] Add rule fixtures/tests so failures are deterministic and easy to maintain.

- [x] **T5: Add integration tests and example consumption path** (AC: #1, #2, #3, #5)
  - [x] Add a small test harness component that consumes the shortcut hook and renders hint-bearing actions.
  - [x] Verify global dispatch, editable suppression, cleanup, and tab-navigation non-regression in Vitest/RTL.
  - [x] If Story 6.2 shell code is not yet merged, keep the example harness local to tests rather than inventing incomplete shell code just to demo the hook.

## Dev Notes

### Epic Intent and Story Boundary

- Epic 6 owns the web-shell foundations, including the global keyboard shortcut system and the custom ESLint invariance rule; this story is the foundation for keyboard-heavy HITL work in later epics.
- Story 6.3 is intentionally infrastructure-first. It should not prematurely implement full scene-review, character-grid, or timeline business flows from Epics 7 and 8, but it must expose the primitives those stories will need.
- The planning artifacts explicitly place the keyboard hook and ESLint invariance rule together to prevent drift of key semantics over time.

### Source Requirements to Preserve

- Epic 6 scope explicitly includes the 8-key global shortcut system and the custom ESLint rule enforcing `Enter=primary` and `Esc=secondary`.
- Story 6.3 acceptance criteria require global registration for `Enter`, `Esc`, `J`, `K`, `Tab`, `Ctrl+Z`, `Shift+Enter`, `S`, and `1-9/0`, plus visible labels in UI.
- UX collision prevention is mandatory: focused `input` / `textarea` must suppress global shortcuts like `J` and `K` so text entry wins.
- UX guidance requires persistent inline keyboard hints visible without hover, using an ActionBar/Linear-like pattern.
- The UX spec states the 8-key shortcut layer operates independently of Tab navigation and that all primary actions remain keyboard-reachable.

### Existing Code to Extend, Not Replace

- `web/eslint.config.js` already uses ESLint flat config with `typescript-eslint`, `eslint-plugin-react-hooks`, and Vite's React Refresh config. Extend this file for the custom rule; do not replace the config style with legacy `.eslintrc`.
- `web/src/test/setup.ts` already blocks non-localhost fetches. Reuse the existing Vitest setup instead of creating duplicate setup files.
- `web/src/App.tsx` and `web/src/App.test.tsx` are still minimal scaffold files. Avoid turning them into the permanent home for shortcut logic; create the architecture-aligned folders Epic 6 expects (`components/`, `hooks/`, `lib/`, `test/`) as needed.
- `web/package.json` currently has the baseline React/Vite/Vitest/ESLint toolchain only. Add dependencies sparingly and only if they directly support this story.

### Architecture Alignment

- The architecture document reserves `web/src/components/shared/` for cross-surface pieces such as `ActionBar`, and `web/src/hooks/` for keyboard shortcuts and other reusable UI behavior. Keep this story's primitives in those directories.
- The React organization contract also expects co-located React tests and shared utilities under `web/src/lib/` or `web/src/test/`.
- Keyboard behavior is a product-level concern for HITL review; this engine should be composable enough for future `ActionBar`, `CharacterGrid`, and `TimelineView` consumers.
- This story must not compromise accessibility basics: keep Tab focused on focus traversal, preserve focus-visible outlines, and avoid `tabindex` hacks.

### Library / Framework Requirements

- React 19.x, Vite 7.3.x, Vitest 4.1.x, TypeScript 6.x, and ESLint 9 flat config are the active web stack in this repository. Stay within those conventions.
- Prefer repository-native patterns over new state/shortcut libraries unless there is a clear, documented need. A small custom hook is the expected solution here.
- If ESLint rule testing needs local helper infrastructure, keep it lightweight and local to `web/`; do not introduce a full custom plugin packaging workflow.

### Implementation Guardrails

- Centralize keyboard parsing and editable-target checks once. Do not scatter raw `event.key === ...` comparisons across components.
- Treat key semantics as product language, not feature-local preferences: `Enter` means primary/confirm, `Esc` means secondary/back/reject.
- Distinguish between global shortcuts and component-local key handlers. The engine should support scoped ownership without double-dispatch.
- Keep persistent key labels visible inline in the rendered DOM. Tooltip-only hints violate the UX requirement and break future test assertions.
- Avoid solving future command-palette or modal-shortcut problems in this story. Reserved-key complexity such as `Ctrl+K` is out of scope.

### Testing Requirements

- Add unit/integration coverage for all required key combinations, cleanup behavior, editable collision prevention, and ActionBar-style label rendering.
- Add deterministic tests for the ESLint rule itself, not just manual lint verification.
- Keep tests browser-safe and deterministic under jsdom; no real network calls, no dependence on browser-specific key repeat timing.
- `npm run lint` and the relevant Vitest suite must both pass before marking the story implementation complete.

### Git Intelligence Summary

- Recent commits are backend-heavy (Epic 4 and earlier) and do not establish reusable frontend shortcut patterns yet. This means Story 6.3 should treat the planning artifacts as the source of truth rather than trying to imitate non-existent web implementations.
- The repository has so far favored small, explicit infrastructure over heavyweight abstractions. Mirror that posture in the keyboard engine and lint rule.

### Project Context Reference

- No separate `project-context.md` was present in the repository during story creation. Planning artifacts and the live codebase were used as the authoritative sources instead.

## Story Completion Status

- Story file created: `_bmad-output/implementation-artifacts/6-3-keyboard-shortcut-engine.md`
- Story status set to `review`
- Sprint status should reflect this story as `review`
- Completion note: Ultimate context engine analysis completed - comprehensive developer guide created

### Review Findings

- [x] [Review][Patch] Provider hook re-registers shortcuts on every render — consumer array identity unstable, causes churn and correlates with stale-closure bugs [web/src/hooks/useKeyboardShortcuts.tsx:159-165]
- [x] [Review][Patch] `order_ref` monotonic counter pollutes scope precedence after re-register / StrictMode double-mount [web/src/hooks/useKeyboardShortcuts.tsx:113-136]
- [x] [Review][Patch] ESLint rule does not lint JSX `onKeyDown` bindings — spec AC-4 requires both registration objects AND JSX key handlers [web/eslint-rules/keyboardShortcutInvariance.js:67-101]
- [x] [Review][Patch] ESLint rule test does not prove CI build failure — only checks `messages.length`, not `severity===2` or `errorCount>0` [web/eslint-rules/keyboardShortcutInvariance.test.ts:44-88]
- [x] [Review][Patch] ESLint rule bypassable by spread/computed/identifier/template-with-expression — boundary not tested or documented [web/eslint-rules/keyboardShortcutInvariance.js]
- [x] [Review][Patch] `normalizeShortcut` does not guard `event.isComposing` — IME-confirming Enter fires primary action [web/src/lib/keyboardShortcuts.ts:52]
- [x] [Review][Patch] `normalizeShortcut` does not guard `event.repeat` — holding a key fires N handlers per press [web/src/lib/keyboardShortcuts.ts:52]
- [x] [Review][Patch] Shift+letter collides with bare letter (`Shift+S` → `s`) [web/src/lib/keyboardShortcuts.ts:84-90]
- [x] [Review][Patch] `Ctrl+Shift+Z` (redo) collapses to `ctrl+z` undo — missing `!shiftKey` guard [web/src/lib/keyboardShortcuts.ts:77]
- [x] [Review][Patch] `Tab` matches with any modifier set — `Shift+Tab`/`Ctrl+Tab` fire edit handler [web/src/lib/keyboardShortcuts.ts:70-75]
- [x] [Review][Patch] `isEditableEventTarget` treats non-text inputs (checkbox, radio, button, submit) as editable and misses shadow DOM retargeting [web/src/lib/keyboardShortcuts.ts:103-123]
- [x] [Review][Patch] `event.defaultPrevented` early-return lets any upstream `preventDefault` silently kill every shortcut [web/src/hooks/useKeyboardShortcuts.tsx:66-68]
- [x] [Review][Patch] Dispatcher has no try/catch around handler — synchronous throw escapes into browser default handler [web/src/hooks/useKeyboardShortcuts.tsx:101-103]
- [x] [Review][Patch] No dev warning when multiple `KeyboardShortcutsProvider` instances nest — silently duplicates dispatch [web/src/hooks/useKeyboardShortcuts.tsx:55-143]
- [x] [Review][Patch] Inside-editable Enter suppression test lacks focus assertion — could pass on broken implementation [web/src/hooks/useKeyboardShortcuts.test.tsx:209-233]
- [x] [Review][Patch] Missing normalize tests for IME, key repeat, Shift+letter, Ctrl+Shift+Z, Cmd+Z, Shift+Tab, plain `z`, digit-9 [web/src/lib/keyboardShortcuts.test.ts]
- [x] [Review][Patch] Missing `isEditableEventTarget` tests for read-only text input, non-text input narrowing, contenteditable ancestor walk [web/src/lib/keyboardShortcuts.test.ts:54-65]
- [x] [Review][Defer] AltGr / non-US layout normalization — layout-dependent, spec does not require cross-layout parity; document limitation [web/src/lib/keyboardShortcuts.ts] — deferred, pre-existing
- [x] [Review][Defer] `userEvent.keyboard` dispatches to `document.body` when nothing focused — test harness constraint, not production bug [web/src/hooks/useKeyboardShortcuts.test.tsx] — deferred, pre-existing

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- Story execution workflow review on 2026-04-19
- Local implementation context review: `web/src/App.tsx`, `web/src/index.css`, `web/eslint.config.js`, `web/src/test/setup.ts`
- Existing shell inspection: `AppShell`, `ProductionShell`, `Sidebar`, `useUIStore`, `useViewportCollapse`
- Verification commands: `npm run lint`, `npm exec vitest run`

### Completion Notes List

- Implemented a provider-backed global keyboard shortcut engine with centralized normalization, scoped precedence, and cleanup-safe registration
- Added shared editable-target suppression so `input`, `textarea`, and `contenteditable` surfaces keep text-entry ownership by default
- Added reusable inline keyboard hint primitives with ActionBar grouping and a lightweight production-shell example consumption path
- Added a local ESLint invariance rule that enforces `Enter` as primary intent and `Escape` as secondary intent for shortcut registration objects
- Added unit and integration coverage for normalization, dispatch, cleanup, editable suppression, ActionBar rendering, and the ESLint rule
- Verified the story with `npm run lint` and `npm exec vitest run` before moving it to review

### File List

- web/src/App.tsx
- web/src/index.css
- web/src/components/shells/ProductionShell.tsx
- web/src/components/shared/ActionBar.tsx
- web/src/components/shared/ProductionShortcutPanel.tsx
- web/src/components/shared/ShortcutHintAction.tsx
- web/src/hooks/useKeyboardShortcuts.tsx
- web/src/hooks/useKeyboardShortcuts.test.tsx
- web/src/lib/keyboardShortcuts.ts
- web/src/lib/keyboardShortcuts.test.ts
- web/src/components/shared/ActionBar.test.tsx
- web/eslint.config.js
- web/eslint-rules/keyboardShortcutInvariance.js
- web/eslint-rules/keyboardShortcutInvariance.test.ts
- _bmad-output/implementation-artifacts/sprint-status.yaml
- _bmad-output/implementation-artifacts/6-3-keyboard-shortcut-engine.md

### Change Log

- 2026-04-19: Implemented Story 6.3 keyboard shortcut infrastructure, shared ActionBar hint primitives, and a production-shell demo surface.
- 2026-04-19: Added keyboard normalization, editable suppression, shortcut hook/provider tests, and ESLint shortcut invariance rule coverage.
- 2026-04-19: Verified Story 6.3 with frontend lint and Vitest, then marked the story ready for review.
- 2026-04-19: Code review addressed 17 patch findings — hardened `normalizeShortcut` (IME/repeat/Shift/Ctrl+Shift+Z/Cmd+Z/modifier-on-Tab guards), narrowed `isEditableEventTarget` to text-like inputs with shadow-DOM composedPath, refactored provider to a latest-ref supplier registry (removes render-churn, removes `defaultPrevented` swallow, adds try/catch + nested-provider warning), extended ESLint rule to cover JSX `onKeyDown` handlers, strengthened rule tests to assert severity/errorCount and JSX coverage, expanded lib/hook tests for the new guards. 2 items deferred. Lint + Vitest (61 tests) green. Story moved to `done`.
