# Story 6.1: Theme Engine & Global Styling (Dark-only)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a designer,
I want a dark-only design system based on CSS variables and Geist typography,
so that the application has a consistent, premium feel.

## Prerequisites

**This is the first implementation story in Epic 6.** It establishes the global styling contract that every later web-ui story in Epics 6-10 must inherit rather than re-define.

- keep all styling decisions compatible with the existing Vite 7.3 + React 19.2 + Tailwind CSS 4.2 + Go `embed.FS` setup already present in the repo (`web/package.json`, `web/vite.config.ts`, `internal/web/embed.go`)
- do not introduce light mode, a theme toggle, or any `dark:`-prefixed duplication; the product requirement is dark-only from first render
- do not wait for Story 6.4 to start font bundling; Story 6.1 may place font assets into the Vite build output so they are already included in the existing `internal/web/dist` embed pipeline
- preserve Story 1.1's `base: "./"` Vite build contract so self-hosted font URLs resolve correctly from embedded static assets

## Acceptance Criteria

Unless stated otherwise, frontend tests use Vitest + React Testing Library + jsdom, live under `web/src/`, and avoid network access. CSS assertions should rely on `getComputedStyle()` and DOM class inspection rather than screenshots. Keep all values in authored source as rem or raw RGB triplets except where asset URLs or comments require literals.

1. **AC-TOKENS-CSS-DARK-ONLY-16-SEMANTIC-COLORS:** a dedicated theme token file defines the dark-only color system as CSS custom properties using RGB triplets for Tailwind alpha compositing.

   Required outcome:
   - create `web/src/styles/tokens.css`
   - define exactly these 16 semantic tokens on `:root`:
     - `--background: 15 17 23` (`#0F1117`)
     - `--bg-subtle: 19 21 29` (`#13151D`)
     - `--bg-raised: 24 24 27` (`#18181B`)
     - `--bg-input: 30 30 36` (`#1E1E24`)
     - `--border-subtle: 34 34 40` (`#222228`)
     - `--border: 39 39 42` (`#27272A`)
     - `--border-active: 63 63 70` (`#3F3F46`)
     - `--foreground: 228 228 231` (`#E4E4E7`)
     - `--muted: 113 113 122` (`#71717A`)
     - `--muted-subtle: 82 82 91` (`#52525B`)
     - `--accent: 91 141 239` (`#5B8DEF`)
     - `--accent-hover: 123 164 244` (`#7BA4F4`)
     - `--accent-muted: 91 141 239 / 0.125` equivalent tokenization exposed for selected surfaces
     - `--warning: 245 158 11` (`#F59E0B`)
     - `--success: 34 197 94` (`#22C55E`)
     - `--destructive: 239 68 68` (`#EF4444`)
   - set `color-scheme: dark` at the root/global level
   - no light theme overrides, no `@media (prefers-color-scheme: light)`, no runtime theme toggle state

   Rules:
   - store the canonical palette as semantic tokens only; do not expose component-specific color names at this layer
   - token file must be importable from `web/src/index.css`
   - the story may correct the stale "15 semantic tokens" wording in older epic prose; UX-DR1 and the current design request require 16 tokens

   Tests:
   - `web/src/styles/tokens.test.ts` (or equivalent) asserts every required custom property exists on `document.documentElement`
   - include a regression assertion that no light-mode selector or `dark:` token duplication is introduced in source-controlled CSS

2. **AC-TAILWIND-4-THEME-BRIDGE-USES-CSS-CUSTOM-PROPERTIES:** Tailwind theme values are extended from the CSS token system in a way that matches the repo's Tailwind 4 setup.

   Required outcome:
   - bridge semantic tokens into Tailwind utility space using Tailwind CSS 4 idioms already compatible with `@import "tailwindcss"` in `web/src/index.css`
   - utilities/classes can reference the semantic palette without hard-coded hex literals in components
   - alpha-compatible color usage works for surfaces like selected/hover/outline states

   Rules:
   - prefer Tailwind 4 CSS theme configuration (`@theme` / CSS-driven token mapping) over introducing a legacy `tailwind.config.ts` unless the repo truly requires one
   - do not regress the current `@tailwindcss/vite` plugin workflow in `web/vite.config.ts`
   - do not scatter hex values across JSX or component-local CSS
   - preserve product color semantics: amber for recoverable errors, blue for interaction, green only for completion, no color as sole status signal

   Tests:
   - add at least one component or fixture test proving a Tailwind utility resolves to a CSS-token-backed value
   - add a structural assertion that the theme bridge references CSS custom properties rather than literal palette hex strings

3. **AC-GEIST-TYPOGRAPHY-BUNDLED-FOR-EMBEDDED-SPA:** Geist Sans and Geist Mono are self-hosted and globally available with Korean-safe fallbacks and `font-display: swap`.

   Required outcome:
   - create `web/src/styles/fonts.css`
   - declare `@font-face` for `Geist Sans` and `Geist Mono`
   - self-host variable font files in a path that survives Vite build and Go embed, preferably `web/public/assets/fonts/GeistVF.woff2` and `web/public/assets/fonts/GeistMonoVF.woff2`
   - expose a sans stack that uses Geist first and falls back to Korean-capable system gothic fonts:
     - `'Geist Sans', 'Apple SD Gothic Neo', 'Malgun Gothic', 'Noto Sans KR', system-ui, sans-serif`
   - expose a mono stack with Geist Mono first and a platform monospace fallback
   - every `@font-face` uses `font-display: swap`

   Rules:
   - do not fetch fonts from a CDN at runtime
   - do not depend on Next.js `geist/font/*` packages; this repo is a Vite SPA, not Next.js
   - do not bundle Pretendard or another large Korean font in V1 unless explicitly required; system gothic fallback is the approved V1 compromise
   - ensure the chosen asset path works with Vite `base: "./"` and the existing `internal/web/dist` embed flow

   Tests:
   - add a test or deterministic assertion covering that the global sans and mono font variables/stacks are present
   - verify built asset references are relative/static-path compatible, not remote URLs

4. **AC-8_LEVEL_TYPE_SCALE-IN-REM:** the global typography layer exposes the required 8-level type scale in rem with the specified weights and Korean readability baseline.

   Required outcome:
   - define reusable typography tokens/classes for:
     - `display` = `1.875rem` / weight `700`
     - `h1` = `1.5rem` / weight `600`
     - `h2` = `1.25rem` / weight `600`
     - `h3` = `1.125rem` / weight `500`
     - `body` = `0.9375rem` / weight `400`
     - `body-sm` = `0.875rem` / weight `400`
     - `caption` = `0.75rem` / weight `400`
     - `mono` = `0.875rem` / weight `400`
   - line-height rules are set consistently enough for later component reuse, with `body` tuned for Korean narration readability
   - prose defaults to sans; machine identifiers and numeric values can opt into mono

   Rules:
   - authored sizes must be rem values, not px values
   - `caption` is the floor; do not introduce global text smaller than `0.75rem`
   - type tokens should be reusable from Story 6.2 onward without redefinition

   Tests:
   - add CSS or DOM assertions that the exported type tokens/classes resolve to the required rem values

5. **AC-SPACING-SCALE-4PX-BASE-7-NAMED-TOKENS:** the global spacing system defines the 4px base scale with the seven approved named tokens.

   Required outcome:
   - expose:
     - `--space-1: 0.25rem`
     - `--space-2: 0.5rem`
     - `--space-3: 0.75rem`
     - `--space-4: 1rem`
     - `--space-6: 1.5rem`
     - `--space-8: 2rem`
     - `--space-12: 3rem`
   - make the scale easy to consume from Tailwind utilities and plain CSS

   Rules:
   - keep the canonical authored values in rem
   - do not create an alternate pixel-only spacing system in components
   - spacing tokens stay constant across density modes except for density-specific preview/container behavior called out by the UX spec

   Tests:
   - add a style test that asserts the named spacing custom properties exist and match the required rem values

6. **AC-DATA-DENSITY-AFFORDANCE-SYSTEM-STANDARD-ELEVATED:** the shell-level affordance density system is globally defined and switchable via `data-density`.

   Required outcome:
   - define default density variables at root:
     - `--content-expand`
     - `--preview-size`
     - `--motion-duration`
   - define `[data-density="standard"]` and `[data-density="elevated"]` behavior, with elevated mode increasing preview emphasis and motion duration as specified by the UX design
   - document that later shell components set `data-density`; slot components consume variables only

   Rules:
   - density is controlled by DOM attribute, not by React context alone
   - no runtime "density toggle" UI is added in this story
   - preserve the architecture separation: shell owns density mode, slots remain density-agnostic

   Tests:
   - add a DOM/style test proving `getComputedStyle()` changes when `data-density="elevated"` is applied
   - include a regression assertion that the selector is attribute-based, not class-name-specific

7. **AC-GLOBAL-ENTRYPOINT-INTEGRATION-AND-NO-REGRESSION:** the new style system becomes the single global styling entrypoint without breaking the current app bootstrap.

   Required outcome:
   - `web/src/index.css` imports the new token/font layers and remains the single root stylesheet entry
   - body/html defaults include dark background, foreground color, font smoothing, and margin reset
   - the current `web/src/main.tsx` / `App.tsx` bootstrap keeps working after the styling refactor
   - implementation is ready for Story 6.2 shell work and Story 6.5 CSS verification tests

   Rules:
   - preserve existing build success under `npm run build`
   - do not move unrelated component styling into this story
   - prefer `web/src/styles/` for new global CSS artifacts, matching the architecture directory contract

   Tests:
   - `cd web && npm run build`
   - `cd web && npx vitest run`

## Tasks / Subtasks

- [x] **T1: Global CSS token layer** (AC: #1, #5, #6, #7)
  - [x] Create `web/src/styles/tokens.css`
  - [x] Define the 16 semantic color tokens on `:root` as RGB-triplet-ready custom properties
  - [x] Define spacing tokens `space-1` through `space-12`
  - [x] Define default density variables and the `data-density="standard" | "elevated"` overrides
  - [x] Add root dark defaults (`color-scheme`, background, foreground) and any global reduced-motion/focus primitives that belong at the token layer

- [x] **T2: Font bundling and typography globals** (AC: #3, #4, #7)
  - [x] Add self-hosted Geist variable font files under a Vite-served static directory
  - [x] Create `web/src/styles/fonts.css` with `@font-face` declarations using `font-display: swap`
  - [x] Export sans and mono font stacks with Korean-safe fallbacks
  - [x] Define reusable typography tokens/classes for the 8-level scale in rem

- [x] **T3: Tailwind 4 theme bridge** (AC: #2, #4, #5)
  - [x] Update `web/src/index.css` so Tailwind 4 utilities can resolve semantic colors, fonts, and spacing from CSS custom properties
  - [x] Keep the setup compatible with `@tailwindcss/vite` and avoid introducing obsolete config files unless genuinely necessary
  - [x] Replace the current placeholder theme comments in `web/src/index.css` with the real import order and theme bridge

- [x] **T4: Test coverage for style contracts** (AC: #1, #2, #3, #4, #5, #6)
  - [x] Add Vitest coverage for root custom properties and density-variable switching using `getComputedStyle()`
  - [x] Add at least one assertion that token-backed utilities/classes resolve through the new theme bridge
  - [x] Add a regression assertion that font sources are self-hosted and no light-mode styling path exists

- [x] **T5: Verification and developer hygiene** (AC: #7)
  - [x] Run `cd web && npx vitest run`
  - [x] Run `cd web && npm run build`
  - [x] If font files are added, verify the built output contains them under the embedded SPA asset tree

## Dev Notes

### Epic Intent and Story Boundary

- Epic 6 owns the shared web-ui foundation for FR40, FR41, and FR52-web. Story 6.1 is the styling contract layer that later shell, routing, keyboard, and component stories must consume rather than replace. [Source: `_bmad-output/planning-artifacts/epics.md` Epic 6 Scope]
- Story 6.1 is intentionally narrow: no sidebar, no routing, no shell grid, no keyboard hook, no light mode, no theme toggle. Those belong to Stories 6.2-6.5. [Source: `_bmad-output/planning-artifacts/epics.md` Story 6.1 / Story 6.2]

### Architecture Compliance Guardrails

- The repo already uses Vite `7.3.0`, React `19.2.4`, Tailwind CSS `4.2.2`, Vitest `4.1.4`, and the `@tailwindcss/vite` plugin. Extend this setup; do not re-scaffold or downgrade/upgrade tooling as part of Story 6.1. [Source: `web/package.json`]
- `web/vite.config.ts` already outputs to `../internal/web/dist` with `base: "./"`. Any font or static asset path chosen here must remain compatible with relative-base embedded serving. [Source: `web/vite.config.ts`]
- `internal/web/embed.go` already embeds `dist/`. Story 6.1 should assume this embed path is canonical and avoid inventing a second asset-serving path. [Source: `internal/web/embed.go`]
- The architecture directory contract expects global CSS under `web/src/styles/`, plus future shell/ui/shared components under `web/src/components/`. Put new global CSS files in `web/src/styles/`. [Source: `_bmad-output/planning-artifacts/architecture.md` React Project Organization]
- Density ownership is architectural: shell sets `data-density`, slots only consume CSS custom properties. Story 6.1 defines variables and selectors; Story 6.2 later wires shell-level attribute changes. [Source: `_bmad-output/planning-artifacts/architecture.md` SPA Routing / Slot+Strategy; `_bmad-output/planning-artifacts/ux-design-specification.md` density integration section]

### UX and Design Requirements to Preserve Exactly

- UX-DR1 requires 16 dark-only semantic tokens, stored as RGB triplets, with no `dark:` prefix, no light mode, and no toggle. [Source: `_bmad-output/planning-artifacts/epics.md` UX-DR1; `_bmad-output/planning-artifacts/ux-design-specification.md` Visual Design Foundation]
- UX-DR2 requires Tailwind theme values to reference CSS custom properties, with color semantics constrained to accent/amber/green usage rules. [Source: `_bmad-output/planning-artifacts/epics.md` UX-DR2]
- UX-DR3 requires Geist Sans and Geist Mono bundled for self-hosting, with Korean system gothic fallback and `font-display: swap`. [Source: `_bmad-output/planning-artifacts/epics.md` UX-DR3; `_bmad-output/planning-artifacts/ux-design-specification.md` Typography System]
- UX-DR4 and UX-DR5 fix the exact type scale and spacing scale. Story 6.1 should encode those values once globally so later stories can compose them. [Source: `_bmad-output/planning-artifacts/epics.md` UX-DR4, UX-DR5]
- UX-DR6 requires the affordance density variables `--content-expand`, `--preview-size`, and `--motion-duration`, with `standard` and `elevated` densities only. [Source: `_bmad-output/planning-artifacts/epics.md` UX-DR6; `_bmad-output/planning-artifacts/ux-design-specification.md` density integration section]
- The UX specification also requires global reduced-motion handling and a 2px accent focus ring. If these fit naturally in the global style layer, implement them here instead of duplicating them per component later. [Source: `_bmad-output/planning-artifacts/epics.md` UX-DR35, UX-DR36]

### Existing Code to Extend, Not Replace

- `web/src/index.css` is currently a placeholder dark-theme file with a comment that Story 6.1 will populate the full token system. Replace the placeholder rather than creating a competing root stylesheet. [Source: `web/src/index.css`]
- `web/src/main.tsx` already imports `index.css`; keep that as the single global stylesheet entry. Do not add ad-hoc CSS imports inside random components for foundation concerns.
- There is no `tailwind.config.ts` in the current repo. That is a strong signal to implement Tailwind 4 theming in CSS first, not to recreate a Tailwind 3-style config by habit.

### Latest Technical Information

- Tailwind CSS 4's official guidance moved theme customization into CSS, which aligns with this repo's current `@import "tailwindcss"` setup. For this story, treat CSS-driven theme mapping as the default path and only introduce JS config if a concrete gap is proven. This is an inference from the official Tailwind v4 docs plus the repo's existing setup. [Source: https://tailwindcss.com/blog/tailwindcss-v4, https://tailwindcss.com/docs/customizing-spacing/]
- Vite's current build docs explicitly support `base: "./"` for unknown deployment bases, which is exactly what this embedded SPA needs. Keep asset references compatible with relative base paths. [Source: https://vite.dev/guide/build]
- Geist is officially distributed by Vercel with installable/self-hostable font assets. For this Vite app, use self-hosted font files rather than the Next.js-specific `geist/font/*` helper. [Source: https://vercel.com/font/]

### Testing Requirements

- Frontend contract tests in this repo use Vitest + jsdom. For this story, CSS verification should be DOM/computed-style-based, not screenshot-based. [Source: `web/package.json`, `_bmad-output/planning-artifacts/epics.md` UX-DR51]
- At minimum, cover:
  - all required root custom properties exist
  - spacing tokens resolve to the required rem values
  - `data-density="elevated"` changes computed values for the density variables
  - font stacks are registered and self-hosted paths are referenced
  - no light mode branch exists

### Project Structure Notes

- New global files should land in:
  - `web/src/styles/tokens.css`
  - `web/src/styles/fonts.css`
  - `web/src/styles/*.test.ts` for direct style-contract tests if that is the cleanest pattern
  - `web/public/assets/fonts/` for self-hosted font files if using Vite public assets
- Files that should probably not change in this story:
  - routing/shell components under `web/src/components/`
  - Go HTTP routing or catch-all server behavior
  - keyboard shortcut infrastructure
  - app-shell layout composition

### References

- `_bmad-output/planning-artifacts/epics.md` — Epic 6 scope, Story 6.1, UX-DR1 through UX-DR6, UX-DR35, UX-DR36, UX-DR49, UX-DR50, UX-DR51
- `_bmad-output/planning-artifacts/architecture.md` — Technology Stack, React Project Organization, SPA Routing, Slot+Strategy density ownership
- `_bmad-output/planning-artifacts/ux-design-specification.md` — Visual Design Foundation, Typography System, Spacing & Layout Foundation, Affordance density integration
- `web/package.json`
- `web/vite.config.ts`
- `web/src/index.css`
- `internal/web/embed.go`
- Tailwind CSS v4 official docs: https://tailwindcss.com/blog/tailwindcss-v4
- Tailwind theme variables docs: https://tailwindcss.com/docs/customizing-spacing/
- Vite build docs: https://vite.dev/guide/build
- Geist official distribution page: https://vercel.com/font/

## Story Completion Status

- Story file created with implementation guardrails, source references, and test expectations
- Ready for `dev-story`
- Completion note: Ultimate context engine analysis completed - comprehensive developer guide created

## Dev Agent Record

### Agent Model Used

gpt-5

### Debug Log References

- create-story workflow analysis
- architecture / UX / repo structure review
- Tailwind v4 / Vite / Geist official doc spot-check
- implemented token, font, typography, and Tailwind bridge CSS layers
- verified `npx vitest run` passes with 11/11 tests
- verified `npm run build` outputs embedded font assets under `internal/web/dist/assets/fonts/`

### Completion Notes List

- Story 6.1 uses the user-provided 16-token palette as the authoritative requirement
- Corrected the stale "15 semantic tokens" phrasing still present in parts of `epics.md`
- No previous-story intelligence exists because this is the first story in Epic 6
- Added `tokens.css` and `fonts.css` as the shared global styling contract for later Epic 6 UI stories
- Mapped semantic colors, fonts, spacing, and type scale into Tailwind 4 via CSS `@theme inline`
- Self-hosted Geist Sans and Geist Mono from the published `geist@1.3.1` package and confirmed the build emits local relative font URLs
- Added style-contract tests covering semantic tokens, spacing, density selectors, font sources, no-light-mode regression checks, and utility/theme bridge structure

### File List

- `_bmad-output/implementation-artifacts/6-1-theme-engine-global-styling.md`
- `_bmad-output/implementation-artifacts/sprint-status.yaml`
- `web/public/assets/fonts/GeistVF.woff2`
- `web/public/assets/fonts/GeistMonoVF.woff2`
- `web/src/App.tsx`
- `web/src/index.css`
- `web/src/styles/fonts.css`
- `web/src/styles/theme-contract.test.tsx`
- `web/src/styles/tokens.css`
- `web/tsconfig.app.json`
- `web/vitest.config.ts`
- `internal/web/dist/assets/fonts/GeistVF.woff2`
- `internal/web/dist/assets/fonts/GeistMonoVF.woff2`
- `internal/web/dist/assets/index-ByMWB5YR.css`
- `internal/web/dist/assets/index-BArRwOyz.js`
- `internal/web/dist/index.html`
- `internal/web/dist/favicon.svg`
- `internal/web/dist/.gitkeep`

## Change Log

- 2026-04-19: Implemented the dark-only theme engine foundation, self-hosted Geist fonts, Tailwind 4 CSS theme bridge, style-contract tests, and verified embedded build output.
- 2026-04-19: Code-review patches applied — reworked style-contract tests to use Vite `?raw` imports (removing `node:fs` + `process.cwd()` coupling and jsdom-dependent `getComputedStyle(var())` tautologies), reverted `"node"` tsconfig types leak into app build scope, added `:focus-visible` outline hex fallback.

### Review Findings

- [x] [Review][Patch] Reworked theme-contract tests — replaced `readFileSync(process.cwd(), …)` with Vite `?raw` imports; removed jsdom-tautological `getComputedStyle(...).fontSize === 'var(…)'` and `.backgroundColor === 'rgb(var(--background))'` assertions; kept source-string regression checks [web/src/styles/theme-contract.test.tsx]
- [x] [Review][Patch] Reverted `"node"` tsconfig types leak into app build scope [web/tsconfig.app.json]
- [x] [Review][Patch] Added `:focus-visible` outline hex fallback so a future token-format change (e.g. oklch) cannot silently break the focus ring [web/src/index.css]
- [x] [Review][Defer] CI build-order race — `internal/web/dist/*` is gitignored, so `go build` before `npm run build` embeds an empty `dist/` and SPA 404s fonts at runtime. Already tracked under deferred-work (1-1 AC-GITIGNORE); resolution belongs with the CI pipeline story, not Story 6.1.
- [x] [Review][Defer] Nested `[data-density]` + root-readers mismatch — JS components that read `--motion-duration` via `getComputedStyle(document.documentElement)` see the root value, not the nearest-ancestor override. Architectural contract decision for shell + slot consumers in Story 6.2+.
- [x] [Review][Defer] `internal/api/spa.go` 404 asset fallback — pre-existing from Story 1.1; SPA handler serves `index.html` even for missing `/assets/*` hashed files, masking stale-build regressions. Belongs with the serving-contract story.
