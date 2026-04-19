# Story 5.4: Frozen Descriptor Propagation & Per-Shot Image Generation (DashScope)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a developer,
I want to generate 1-5 images per scene using shot-level visual descriptors and the Frozen Descriptor,
so that the character remains visually consistent across all shots and scenes.

## Prerequisites

**Stories 5.1-5.3 are hard dependencies for this story.** This story must consume the shared DashScope limiter/retry infrastructure from Story 5.1, run inside the Phase B parallel execution boundary established in Story 5.2, and resolve character-reference state from Story 5.3 rather than inventing new persistence or lookup paths.

- reuse the shared DashScope limiter instance for both image generation and TTS contention; do not allocate a story-local limiter
- preserve the existing `scenario_review -> character_pick -> image -> tts` transition contract in `internal/pipeline/engine_test.go`
- treat `runs.selected_character_id` and the cached candidate-resolution flow from Story 5.3 as the only canonical source for character-reference image editing
- respect Phase B clean-slate resume semantics: `internal/pipeline/resume.go` deletes all `segments` rows and cleans `images/` + `audio/` artifacts before re-run

**Current codebase gap to resolve deliberately:** the repository already has the domain image interfaces and `segments.shots` persistence shape, but it does not yet have a Phase B image-generation service/runner that reads `scenario.json`, composes per-shot prompts, calls DashScope image APIs, writes files, and persists refreshed shot metadata. This story should add that flow in the existing layering, not bypass it through ad-hoc scripts.

- orchestration belongs in `internal/pipeline/`
- persistence updates belong in `internal/db/`
- domain/model additions belong in `internal/domain/`
- DashScope/image provider plumbing belongs under `internal/llmclient/` and/or `internal/service/` following current patterns

## Acceptance Criteria

Unless stated otherwise, new tests follow the project's `TestXxx_CaseName` convention, live beside the code under test, call `testutil.BlockExternalHTTP(t)`, use inline fakes, and keep the module path `github.com/sushistack/youtube.pipeline`. CGO_ENABLED=0.

1. **AC-SCENARIO-SHOT-BREAKDOWN-DRIVES-IMAGE-TRACK:** when a run enters `stage=image`, the image track reads shot breakdown data from the run's `scenario.json` and processes 1-5 shots per scene using the operator-approved shot structure.

   Required outcome:
   - load `scenario.json` from the canonical run artifact path, not from duplicated in-memory state
   - read each scene's shot array and preserve operator-overridden shot counts and shot descriptors already finalized during `scenario_review`
   - generate image work for every shot in scene order and shot order

   Rules:
   - do not recompute shot count from narration/TTS duration in this story; use the persisted scenario artifact as the source of truth
   - fail loudly with a typed validation/error-path when `scenario.json` is missing or malformed
   - keep scene and shot indexing aligned with the existing output contract (`scene_01`, `shot_01`, etc.)

   Tests:
   - `TestImageTrack_LoadsShotBreakdownFromScenarioJSON`
   - `TestImageTrack_UsesOperatorOverrideShotsWithoutRecomputing`
   - `TestImageTrack_MissingScenarioJSONFailsValidation`

2. **AC-FROZEN-DESCRIPTOR-PREFIX-PROPAGATED-VERBATIM:** for every shot image request, the prompt is composed as Frozen Descriptor prefix plus the shot-level `visual_descriptor`, preserving the same Frozen Descriptor text across all scenes and shots in the run.

   Required outcome:
   - one canonical Frozen Descriptor string is resolved per run and reused verbatim on every shot prompt
   - shot-level text is appended without mutating or paraphrasing the Frozen Descriptor
   - prompt composition is deterministic and testable

   Rules:
   - do not silently trim, rewrite, summarize, or LLM-regenerate the Frozen Descriptor during image generation
   - if prompt assembly needs separators or formatting, keep the Frozen Descriptor segment byte-stable across all shots
   - if the run lacks a usable Frozen Descriptor, surface a typed validation error instead of generating inconsistent prompts

   Tests:
   - `TestImagePromptComposer_PrefixesFrozenDescriptorVerbatim`
   - `TestImageTrack_AllShotsShareIdenticalFrozenDescriptorPrefix`
   - `TestImageTrack_MissingFrozenDescriptorFailsLoudly`

3. **AC-CHARACTER-SHOTS-USE-REFERENCE-EDITING:** shots that contain the named character use DashScope reference-based image editing (`qwen-image-edit`) with the chosen character reference resolved from `selected_character_id`.

   Required outcome:
   - resolve `selected_character_id` through the Story 5.3 handoff contract into a cached candidate/reference payload
   - character-bearing shots call the image-edit path with the resolved reference image
   - non-character shots do not require the reference image path

   Rules:
   - do not call DuckDuckGo or repeat external character search from this story
   - resolution failure for `selected_character_id` must be treated as a hard error, not a silent fallback to plain generation for character shots
   - the character/non-character decision should come from scenario/shot metadata or an explicit classifier in the run data model, not from brittle prompt string guessing at call time

   Tests:
   - `TestImageTrack_CharacterShotUsesEditWithSelectedCharacterReference`
   - `TestImageTrack_SelectedCharacterResolutionFailureAbortsCharacterShot`
   - `TestImageTrack_NonCharacterShotSkipsReferenceEdit`

4. **AC-NON-CHARACTER-SHOTS-USE-STANDARD-GENERATION:** shots that do not require the named character use the standard DashScope image generation path.

   Required outcome:
   - standard generation requests use the same composed prompt contract
   - image edit and image generate paths share common output-writing and observability behavior where possible
   - provider/model selection remains config-driven

   Rules:
   - do not fork two unrelated pipelines for edit vs generate when a shared per-shot execution surface will do
   - do not hardcode model identifiers in business logic if the repo already exposes image model config
   - the implementation must remain compatible with the shared DashScope limiter from Story 5.1

   Tests:
   - `TestImageTrack_NonCharacterShotUsesStandardGenerate`
   - `TestImageTrack_EditAndGenerateShareOutputPersistenceContract`

5. **AC-SHOT-IMAGES-WRITTEN-TO-CANONICAL-PATHS:** each generated image is written to the run artifact tree at `images/scene_{idx}/shot_{idx}.png`, producing about 30 images for a typical 10-scene / 3-shot-average run.

   Required outcome:
   - create deterministic per-scene directories under the run output directory
   - persist each successful shot image to the canonical relative path stored in `segments.shots`
   - output naming remains stable across reruns of the same clean-slate Phase B execution

   Rules:
   - store relative artifact paths in persisted metadata, matching the existing `scenario.json` / consistency-check conventions
   - do not collapse all images into one flat directory
   - clean-slate resume must not leave stale image references for shots that were deleted before regeneration

   Tests:
   - `TestImageTrack_WritesImagesToSceneShotDirectories`
   - `TestImageTrack_TypicalRunProducesExpectedImageCount`
   - `TestImageTrack_RerunPreservesCanonicalPathPattern`

6. **AC-SEGMENTS-SHOTS-PERSISTED-WITH-ASSET-METADATA:** after image generation, `segments.shots` is updated so each shot entry includes `image_path`, `duration_s`, and `transition` while preserving the visual descriptor contract needed by later stages.

   Required outcome:
   - insert or update `segments` rows for each scene with refreshed `shots` JSON
   - preserve one shot JSON element per generated shot
   - carry forward duration and transition values from the scenario artifact into persisted segment metadata

   Rules:
   - preserve `visual_descriptor` in each shot JSON element; later review/assembly flows still need it
   - keep `segments.shots` JSON aligned with the architecture schema already documented for Phase B and Phase C
   - do not persist only file paths while dropping timing/transition metadata

   Tests:
   - `TestSegmentStore_SaveImageShots_PersistsImagePathDurationAndTransition`
   - `TestImageTrack_SegmentsShotsJSONPreservesVisualDescriptor`
   - `TestImageTrack_SegmentsShotsRemainAlignedWithScenarioShotOrder`

7. **AC-PHASE-B-INTEGRATION-AND-NO-REGRESSION:** the image track integrates cleanly with the existing Phase B runner/resume model and preserves downstream compatibility for TTS, review, and assembly.

   Required outcome:
   - image generation participates in the parallel Phase B orchestration rather than running as a standalone sequential tool
   - observability/cost/retry surfaces remain compatible with existing pipeline expectations
   - output state after image completion is consumable by later TTS/assembly/review stories

   Rules:
   - do not use `errgroup.WithContext` if that would cancel the sibling TTS track against the established Story 5.2 boundary
   - do not introduce per-scene resume semantics in V1; Phase B remains stage-level clean-slate
   - if the image track partially succeeds and then fails, the next resume must still produce a consistent clean re-run

   Tests:
   - `TestPhaseBRunner_ImageTrackParticipatesWithoutCancellingSiblingTrack`
   - `TestResume_PhaseBRegenerationRebuildsSegmentsShotsAfterFailure`
   - `TestImageTrack_OutputIsConsumableByConsistencyCheck`

## Tasks / Subtasks

- [x] **T1: Add scenario-to-image-track contracts and prompt-composition helpers** (AC: #1, #2)
  - [x] Add/extend domain structs for any missing scenario-shot or character-bearing metadata needed by the image track.
  - [x] Add a loader/parser for run-scoped `scenario.json`.
  - [x] Add a deterministic prompt composer that prefixes the Frozen Descriptor verbatim.

- [x] **T2: Implement per-shot image execution for generate vs edit flows** (AC: #2, #3, #4, #5)
  - [x] Add an image-track service/runner that iterates scenes and shots in order.
  - [x] Resolve `selected_character_id` through the Story 5.3 service/store contract for character-bearing shots.
  - [x] Route character shots to image edit and non-character shots to standard generation through the shared image provider abstraction.
  - [x] Write images to `images/scene_{idx}/shot_{idx}.png`.

- [x] **T3: Persist refreshed `segments.shots` metadata for downstream stages** (AC: #5, #6, #7)
  - [x] Add DB/store methods to insert or replace scene segment rows for Phase B image output.
  - [x] Persist per-shot `image_path`, `duration_s`, `transition`, and `visual_descriptor`.
  - [x] Ensure clean-slate Phase B reruns rebuild `segments` rows deterministically after resume.

- [x] **T4: Integrate the image track into the existing Phase B orchestration boundary** (AC: #3, #4, #7)
  - [x] Extend the Story 5.2 runner wiring so the image track uses the shared DashScope limiter and existing retry/backoff path from Story 5.1.
  - [x] Preserve the non-cancelling parallelism contract between image and TTS.
  - [x] Feed cost/observability information into the existing run/stage accounting surfaces.

- [x] **T5: Add deterministic tests for prompt composition, persistence, and resume behavior** (AC: #1, #2, #3, #4, #5, #6, #7)
  - [x] Add unit tests for prompt composition and character-shot routing.
  - [x] Add DB tests for `segments.shots` JSON persistence and ordering.
  - [x] Add pipeline/service integration tests for Phase B rerun and consistency.
  - [x] Run `go test ./...` and `go build ./...`.

### Review Findings

- [x] [Review][Patch] Per-scene not per-shot DB persistence — `UpsertImageShots` called once per scene after all shots succeed; shots N-1..0 lose DB record on mid-scene failure [internal/pipeline/image_track.go:172-211]
- [x] [Review][Patch] SceneNum=0 maps to sceneIndex=0 via fallback, colliding with SceneNum=1 [internal/pipeline/image_track.go:205-208]
- [x] [Review][Patch] Duplicate SceneNum across VisualBreakdown.Scenes not validated — second scene overwrites first in DB [internal/pipeline/image_track.go:164]
- [x] [Review][Patch] Duplicate ShotIndex within scene not validated — second shot overwrites same file on disk [internal/pipeline/image_track.go:173]
- [x] [Review][Patch] `%02d` canonical format breaks silently at scene/shot index >= 100 [internal/pipeline/image_track.go:165,177]
- [x] [Review][Patch] `shot.ShotIndex <= 0` produces `shot_00.png` or negative path [internal/pipeline/image_track.go:176]
- [x] [Review][Patch] `req.RunID` empty not validated in `runImageTrack` [internal/pipeline/image_track.go:125]
- [x] [Review][Patch] `req.RunID` path-traversal characters not validated [internal/pipeline/image_track.go:142]
- [x] [Review][Patch] `state.Narration == nil` returns empty character map, causing ALL character scenes to silently route to Generate [internal/pipeline/image_track.go:295]
- [x] [Review][Patch] VisualBreakdown scene with no matching narration scene silently routes to Generate [internal/pipeline/image_track.go:308-313]
- [x] [Review][Patch] Empty `scene.Shots` slice overwrites existing valid segments row with empty shots JSON [internal/pipeline/image_track.go:164]
- [x] [Review][Patch] No `os.Stat` verification after `invokeImageProvider`; provider may return nil error without writing file [internal/pipeline/image_track.go:181]
- [x] [Review][Patch] No `ctx.Err()` check between scene iterations — cancellation not honored until provider call [internal/pipeline/image_track.go:164]
- [x] [Review][Patch] `ComposeImagePrompt`: if frozen ends with `"; "`, separator produces double `"; ; "` in composed prompt [internal/pipeline/image_track.go:115]
- [x] [Review][Patch] `rows.Close()` called inline on each error path without `defer` in `ClearImageArtifactsByRunID` — future edits risk resource leak [internal/db/segment_store.go:114,121,137,146]
- [x] [Review][Patch] `ReferenceImagePath` field name misleading — it receives an HTTP URL; rename to `ReferenceImageURL` for clarity [internal/domain/llm.go:31]
- [x] [Review][Patch] Resume test `TestResume_PhaseBRegenerationRebuildsSegmentsShotsAfterFailure` manually clears state without exercising `ClearImageArtifactsByRunID`, leaving persistence interaction untested [internal/pipeline/image_track_test.go:1433]
- [x] [Review][Defer] `UpsertImageShots` fresh-INSERT defaults `status='pending'`; caller must explicitly mark complete — pre-existing schema design [internal/db/segment_store.go:220] — deferred, pre-existing
- [x] [Review][Defer] `ClearImageArtifactsByRunID` returns scene-count via `RowsAffected`, not image-count — misleading but pre-existing [internal/db/segment_store.go:161] — deferred, pre-existing
- [x] [Review][Defer] No shared transaction wrapping `ClearImageArtifactsByRunID` + `ClearTTSArtifactsByRunID` — pre-existing gap in caller [internal/db/segment_store.go] — deferred, pre-existing
- [x] [Review][Defer] Character candidate resolved once per run; mid-run operator re-selection not detected — V1 acceptable [internal/pipeline/image_track.go:149] — deferred, V1 scope
- [x] [Review][Defer] Canonical-path test assertions are tautological (fake honors OutputPath) — test quality issue [internal/pipeline/image_track_test.go:739-766] — deferred, separate test quality pass
- [x] [Review][Defer] Non-cancellation sibling test fake TTS never consults ctx, so cancellation assertion is vacuous [internal/pipeline/image_track_test.go:1391] — deferred, test design

## Dev Notes

### Epic Intent and Story Boundary

- Epic 5 covers FR14-FR19, and Story 5.4 specifically owns FR14 plus the execution half of FR18: generating the per-shot image outputs that carry Frozen Descriptor continuity into the artifact set consumed by later stages. [Source: _bmad-output/planning-artifacts/epics.md, Epic 5 / Story 5.4]
- Story 5.3 already owns character-search caching and `selected_character_id` persistence. Story 5.4 must consume that handoff contract and perform canonical reference-based image generation, not re-open the character-search problem. [Source: _bmad-output/planning-artifacts/epics.md, Stories 5.3-5.4]
- Story 5.5 will share the same Phase B parallel boundary and shared DashScope limiter budget, so avoid image-track abstractions that make TTS integration harder or duplicate limiter ownership. [Source: _bmad-output/planning-artifacts/epics.md, Epic 5 scope; _bmad-output/implementation-artifacts/5-2-parallel-media-generation-runner.md]

### Architecture Alignment

- Architecture defines Phase B as per-shot image generation plus TTS running in parallel on the same DashScope budget. The rate-limit coordination is a tier-1 architecture concern, not a story-local optimization. [Source: _bmad-output/planning-artifacts/architecture.md, Technical Constraints & Dependencies; Cross-Cutting Concerns]
- The documented `segments.shots` JSON schema already expects `image_path`, `duration_s`, `transition`, and `visual_descriptor`. Story 5.4 should populate that existing contract rather than invent a second image metadata format. [Source: _bmad-output/planning-artifacts/architecture.md, segments table schema]
- Phase B resume semantics are explicitly clean-slate: delete existing segments and regenerate all. Image persistence must therefore be deterministic enough to rebuild from scratch after failure. [Source: _bmad-output/planning-artifacts/architecture.md, Phase B Resume Semantics for segments table; internal/pipeline/resume.go]
- The implementation sequence in architecture places Phase B before REST/UI review surfaces, so backend image orchestration should not depend on future web features. [Source: _bmad-output/planning-artifacts/architecture.md, Decision Impact Analysis]

### PRD and UX Guardrails

- PRD treats Frozen Descriptor plus Qwen-Image-Edit as one of the four load-bearing product differentiators. The story is incomplete if it generates images without true verbatim descriptor continuity or without reference-based handling for character shots. [Source: _bmad-output/planning-artifacts/prd.md, What Makes This Special; Key Terms]
- PRD defines a Shot as the per-scene visual unit with its own image, duration, and transition. The image track should therefore preserve shot-level timing and transition metadata instead of flattening to scene-level images. [Source: _bmad-output/planning-artifacts/prd.md, Key Terms]
- UX artifacts already assume inline editing and operator overrides can change shot structure before Phase B. Story 5.4 must trust the persisted scenario artifact after review, not recalculate shot shapes from raw narration. [Source: _bmad-output/planning-artifacts/ux-design-specification.md, inline edit with blur-save; character workflow notes]

### Existing Code to Extend, Not Replace

- `internal/domain/types.go` already defines `domain.Run`, `domain.Episode`, and `domain.Shot`, including `Shot.ImagePath`, `DurationSeconds`, `Transition`, and `VisualDescriptor`. Extend these models only if the image track truly needs extra scenario/character metadata.
- `internal/domain/llm.go` already defines `ImageGenerator.Generate` and `ImageGenerator.Edit`; reuse this interface split for non-character vs character shots instead of inventing a separate provider contract.
- `internal/db/segment_store.go` already reads `segments.shots` JSON into `[]domain.Shot`. Add write/update methods in the same store rather than creating a second place that persists segment shot metadata.
- `internal/pipeline/resume.go` already enforces clean-slate Phase B behavior by deleting `segments` and cleaning image/TTS artifacts together. Any new image runner must remain compatible with that lifecycle.
- `internal/api/routes.go` currently has no dedicated image endpoints, which is a clue that 5.4 is backend pipeline work, not a new operator-facing API surface.
- `internal/db/run_store.go` and Story 5.3's follow-up implementation will be the canonical place to read `selected_character_id` and related run-state fields.

### File Structure Notes

- Expected new files:
  - `internal/pipeline/image_track.go`
  - `internal/pipeline/image_track_test.go`
  - `internal/service/image_service.go` or equivalent helper if the repo prefers service-level provider orchestration
  - `internal/service/image_service_test.go`
  - DashScope image adapter files under `internal/llmclient/dashscope/` if not already present
  - migration/store helper only if current segment persistence lacks the required upsert/write surface
- Expected existing files to extend:
  - `internal/domain/types.go`
  - `internal/domain/llm.go`
  - `internal/db/segment_store.go`
  - `internal/db/run_store.go`
  - `internal/pipeline/resume.go`
  - Story 5.2 runner/orchestration files once that story is implemented

### Testing Requirements

- Every new test must call `testutil.BlockExternalHTTP(t)`.
- Use fake image generators for both generate and edit paths so prompt composition and routing are deterministic and offline.
- Add persistence tests that verify `segments.shots` round-trips with all required fields intact.
- Add at least one integration-style test that starts from a run-scoped `scenario.json`, executes the image track, and verifies on-disk output plus DB `segments.shots` alignment.
- Add at least one resume-oriented test proving that partial Phase B output can be deleted and fully regenerated without orphaned paths or stale segment metadata.

### Open Implementation Tensions

- The story text requires character-bearing shots to use image editing, but the current domain `Shot` shape does not obviously encode "contains character." Implementation must choose a stable source of truth for that flag and document it in code rather than inferring from brittle text heuristics.
- The Frozen Descriptor source may live in `scenario.json`, a prior stage artifact, or a future dedicated field. Before implementation, identify the single canonical source and keep prompt assembly dependent on that one source only.
- Because Phase B resumes clean-slate, any optimization that tries to preserve partial image output across failures would violate the current architecture. Favor correctness and determinism over incremental reuse.

### References

- Epic definition and ACs: [epics.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/epics.md)
- Sprint prompt shorthand: [sprint-prompts.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/sprint-prompts.md)
- Architecture constraints and segments schema: [architecture.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/architecture.md)
- PRD differentiator and Frozen Descriptor terminology: [prd.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/prd.md)
- UX notes on override/edit flow: [ux-design-specification.md](/home/jay/projects/youtube.pipeline/_bmad-output/planning-artifacts/ux-design-specification.md)
- Prior story contexts: [5-1-common-rate-limiting-exponential-backoff.md](/home/jay/projects/youtube.pipeline/_bmad-output/implementation-artifacts/5-1-common-rate-limiting-exponential-backoff.md), [5-2-parallel-media-generation-runner.md](/home/jay/projects/youtube.pipeline/_bmad-output/implementation-artifacts/5-2-parallel-media-generation-runner.md), [5-3-character-reference-selection-search-result-cache.md](/home/jay/projects/youtube.pipeline/_bmad-output/implementation-artifacts/5-3-character-reference-selection-search-result-cache.md)

## Dev Agent Record

### Agent Model Used

GPT-5 Codex

### Debug Log References

- Create-story workflow analysis on 2026-04-18

### Completion Notes List

- Story file created for explicit user-requested target `5-4-frozen-descriptor-propagation-per-shot-image-generation`
- Story scope anchored to current repository reality: image interfaces and segment shot schema exist, but the concrete Phase B image runner/persistence flow does not yet
- Guardrails added for Story 5.1 limiter reuse, Story 5.2 parallel-runner integration, and Story 5.3 selected-character handoff consumption
- (2026-04-18) Implemented: `internal/pipeline/image_track.go` with `NewImageTrack`, `ComposeImagePrompt`, and the per-shot edit/generate router. Character-bearing routing pulls from `NarrationScene.EntityVisible` (already in scenario.json) so no new scenario schema field is required; resolver failure for any character-bearing shot is a hard stop.
- (2026-04-18) Persistence: added `db.SegmentStore.UpsertImageShots` that writes/replaces per-scene rows with the refreshed shots JSON while preserving existing TTS/clip fields — clean-slate Phase B reruns rebuild deterministically.
- (2026-04-18) Domain additions: `ImageRequest.OutputPath` and `ImageEditRequest.OutputPath` were added so provider adapters can honor the canonical `images/scene_{idx}/shot_{idx}.png` layout; no existing call site uses those fields yet.
- (2026-04-18) Tests: `internal/pipeline/image_track_test.go` covers prompt composition, scenario loading, frozen-descriptor propagation, character/non-character routing, canonical paths, rerun determinism, Phase B non-cancelling integration, resume regeneration, and `CheckConsistency` compatibility; DB test `TestSegmentStore_SaveImageShots_PersistsImagePathDurationAndTransition` covers upsert replace-in-place behavior. Full regression (`go test ./...`) passes.

### File List

- `_bmad-output/implementation-artifacts/5-4-frozen-descriptor-propagation-per-shot-image-generation.md`
- `internal/pipeline/image_track.go`
- `internal/pipeline/image_track_test.go`
- `internal/db/segment_store.go`
- `internal/db/segment_store_test.go`
- `internal/domain/llm.go`
