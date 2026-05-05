---
title: 'visual_breakdowner — scene style + narration alignment'
type: 'feature'
created: '2026-05-05'
status: 'done'
baseline_commit: 'd7f9e77a54234733f4d51dffbd5fd91805b4d357'
context:
  - '{project-root}/_bmad-output/planning-artifacts/next-session-visual-breakdowner-style-alignment.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** SCP-049 dogfood (2026-05-05) showed two regressions: (1) Phase B per-shot images render photorealistic — cartoon style is injected only for canonical-image generation, never propagated to per-shot prompts; (2) `visual_breakdowner` lacks a rule forcing `visual_descriptor` to depict the **literal subject/action** of the beat's monologue slice, so descriptors drift to ambient mood shots.

**Approach:** Add a new `SceneStylePrompt` config key (separate from canonical-only `CartoonStylePrompt`), thread it into (a) `ComposeImagePrompt` as a new front-prefix layer and (b) the `visual_breakdowner` template via `{scene_style_prompt}` feeding a new `## Style Directive` section, plus add a hard literal-subject/action rule under Rules. 1:1 beat→shot invariant and Frozen Descriptor byte-stability are preserved.

## Boundaries & Constraints

**Always:**
- 1:1 beat→shot invariant preserved — `validateVisualBreakdownActResponse` is **not modified**.
- Frozen Descriptor byte-stability (Story 5.4) preserved — style is a **new layer in front of frozen**, never spliced inside.
- `prompts/agents/visual_breakdowner.tmpl` and `docs/prompts/scenario/03_5_visual_breakdown.md` stay byte-identical (`diff` empty).
- Empty `SceneStylePrompt` is a no-op: per-shot prompt and rendered template both equal pre-cycle bytes.
- Each of the four commits below leaves `go build ./...` and `go test ./...` green.

**Ask First:**
- If post-merge dogfood still shows photorealistic output, do **not** auto-extend scope to weaken canonical reference influence — surface and stop (planning doc §7).

**Never:**
- Touch `CartoonStylePrompt` or its callers (`scp_image_service.go`, `serve.go:806`, `resume.go:91`).
- Modify the 1:1 invariant validator or `beatAnchorEqual`.
- Add LLM-judge alignment validation, 1:N beat-shot mapping, or scene reuse.
- Promote the toggle to env (memory: `feedback_config_not_env.md`).

## I/O & Edge-Case Matrix

| Scenario | Input | Output |
|---|---|---|
| Style + frozen + visual all set | `style="S"`, `frozen="F"`, `visual="V"` | `"S; F; V"`; frozen substring appears verbatim exactly once. |
| Style empty | others non-empty | Pre-cycle: `"F; V"` (or `V` if it already prefixes with `F`). |
| Visual already starts with frozen | `style="S"`, `visual="F; extra"` | `"S; F; extra"` (no double frozen). |
| Frozen ends with `"; "` | `frozen="F; "` | Single separator between each layer; frozen bytes byte-stable. |
| Render with style non-empty | `prompts.SceneStylePrompt="S"` | `## Style Directive` section contains substituted `S`. |
| Render with style empty | `prompts.SceneStylePrompt=""` | `{scene_style_prompt}` substitutes to `""`; no literal token leaks. |

</frozen-after-approval>

## Code Map

- `internal/domain/config.go` -- `SceneStylePrompt` field + default in `DefaultConfig()`
- `internal/config/loader.go` -- `SetDefault("scene_style_prompt", …)` next to `cartoon_style_prompt`
- `config.yaml` -- expose `scene_style_prompt:` with default + `#` comment
- `prompts/agents/visual_breakdowner.tmpl` -- `## Style Directive` section + new alignment bullet
- `docs/prompts/scenario/03_5_visual_breakdown.md` -- byte-identical sync of the .tmpl
- `internal/pipeline/agents/assets.go` -- `SceneStylePrompt string` on `PromptAssets` (caller-set; loader stays file-IO-only)
- `internal/pipeline/agents/visual_breakdowner.go` -- `renderVisualBreakdownActPrompt` replacer gains `{scene_style_prompt}` → `prompts.SceneStylePrompt`
- `internal/pipeline/agents/visual_breakdowner_test.go` -- substitution tests (non-empty + empty)
- `internal/pipeline/image_track.go` -- `ComposeImagePrompt(style, frozen, visual)`; `ImageTrackConfig.SceneStylePrompt`; `runImageTrack` passes style at existing call site
- `internal/pipeline/image_track_test.go` -- update composer + frozen-prefix tests for new signature; add I/O Matrix cases
- `cmd/pipeline/serve.go` -- `buildPhaseARunner` sets `prompts.SceneStylePrompt`; `buildPhaseBRunner`'s `ImageTrackConfig` literal sets `SceneStylePrompt`
- `cmd/pipeline/resume.go` -- verify only; resume reuses both shared builders

## Tasks & Acceptance

**Execution (one commit per group, in order):**

Commit 1 — `feat(config): add SceneStylePrompt for per-shot cartoon style`
- [x] `internal/domain/config.go` — `SceneStylePrompt string` with `yaml:"scene_style_prompt"` + `mapstructure:"scene_style_prompt"`; `DefaultConfig()` default = `"Style: kid-friendly cartoon, Starcraft-inspired stylized art, clean vector lines, vibrant colors, soft cinematic lighting, expressive character poses."`
- [x] `internal/config/loader.go` — `v.SetDefault("scene_style_prompt", cfg.SceneStylePrompt)` adjacent to existing `cartoon_style_prompt`
- [x] `config.yaml` — `scene_style_prompt:` with default + comment ("per-shot cartoon style; canonical character sheet uses cartoon_style_prompt")

Commit 2 — `feat(visual_breakdowner): inject scene style + narration alignment rule`
- [x] `prompts/agents/visual_breakdowner.tmpl` — insert `## Style Directive` (verbatim in Design Notes) above `## Frozen Visual Identity`; append new alignment bullet under Rules
- [x] `docs/prompts/scenario/03_5_visual_breakdown.md` — byte-identical sync (`diff` empty)
- [x] `internal/pipeline/agents/assets.go` — `SceneStylePrompt string` field on `PromptAssets`
- [x] `internal/pipeline/agents/visual_breakdowner.go` — add `"{scene_style_prompt}", prompts.SceneStylePrompt` to the replacer
- [x] `internal/pipeline/agents/visual_breakdowner_test.go` — render-with-style-set test (substituted text present); render-with-style-empty test (no `{scene_style_prompt}` literal)

Commit 3 — `feat(image_track): propagate scene style into per-shot prompt`
- [x] `internal/pipeline/image_track.go` — `ComposeImagePrompt(style, frozen, visual string) string`; layer order `style + "; " + frozen + "; " + visual` with empty-string guards (Design Notes); update doc comment for new layer order + frozen-stability invariant
- [x] `internal/pipeline/image_track.go` — `ImageTrackConfig.SceneStylePrompt`; `runImageTrack` calls `ComposeImagePrompt(cfg.SceneStylePrompt, frozen, shot.VisualDescriptor)` at the existing call site (~line 323)
- [x] `internal/pipeline/image_track_test.go` — update `TestImagePromptComposer_PrefixesFrozenDescriptorVerbatim` (pass empty style, asserts backwards-compatible behavior); add I/O Matrix cases; update `TestImageTrack_AllShotsShareIdenticalFrozenDescriptorPrefix` to assert frozen via `Contains` exactly once + style prefix when set

Commit 4 — `feat(serve,resume): wire SceneStylePrompt`
- [x] `cmd/pipeline/serve.go` — in `buildPhaseARunner`, after `LoadPromptAssets`: `prompts.SceneStylePrompt = cfg.SceneStylePrompt`
- [x] `cmd/pipeline/serve.go` — in `buildPhaseBRunner`'s `NewImageTrack(ImageTrackConfig{...})` literal, add `SceneStylePrompt: cfg.SceneStylePrompt`
- [x] `cmd/pipeline/resume.go` — verify (no edit expected); resume Phase A uses `buildPhaseARunner`, Phase B uses `buildPhaseBRunner`

**Acceptance Criteria:**
- Given default `SceneStylePrompt`, when a fresh run executes Phase A → Phase B, every per-shot image-edit/generate prompt starts with style, then frozen verbatim, then the shot's `visual_descriptor`.
- Given operator clears `scene_style_prompt` to empty, per-shot prompts equal pre-cycle behavior and `{scene_style_prompt}` substitutes to empty.
- Given the `visual_breakdowner` prompt is rendered for any act, it contains `## Style Directive` and the new alignment bullet.
- Given the existing `validateVisualBreakdownActResponse` test suite, all 1:1 invariant tests pass unchanged.
- Given a fresh dogfood run on an SCP-049-shaped scenario, the operator confirms ≥3 per-shot images are visibly cartoon-styled (not photorealistic) AND for ≥1 beat whose monologue slice names a concrete subject/action (e.g. "수술대 위에 누운 사람"), the image depicts that subject/action.
- Given `diff prompts/agents/visual_breakdowner.tmpl docs/prompts/scenario/03_5_visual_breakdown.md`, output is empty.

## Design Notes

**Compose order (Commit 3):**

```go
func ComposeImagePrompt(style, frozen, visual string) string {
    base := /* existing frozen+visual logic kept verbatim:
               HasPrefix guard, empty-string guards,
               "; " sep with HasSuffix("; ") collapse */
    if style == "" { return base }
    if base == ""  { return style }
    sep := "; "
    if strings.HasSuffix(style, "; ") { sep = "" }
    return style + sep + base
}
```
Existing frozen+visual logic is **kept verbatim**; style wraps it as a new outer layer.

**Visual_breakdowner.tmpl insertion — verbatim (Commit 2):**

Above `## Frozen Visual Identity`:

```
## Style Directive

All shots are rendered in: {scene_style_prompt}

Every `visual_descriptor` you write MUST be compatible with this style.
Do NOT include words that conflict with it (e.g. "photorealistic",
"cinematic", "live action", "documentary photo", "hyperrealistic").
Use vocabulary that fits a stylized cartoon (e.g. "expressive pose",
"vibrant", "stylized", "clean line art").
```

Bullet under Rules:

```
- every `visual_descriptor` MUST visually realize the **literal subject
  and action** from the beat's monologue slice (start_offset/end_offset).
  If the slice mentions "수술대 위에 누운 사람", the descriptor MUST
  depict a person on an operating table — not a metaphor, not an
  ambient mood shot. Identity preservation does NOT override this:
  the focal action of the slice always wins.
```

**Seam choices:** `PromptAssets` (not `TextAgentConfig`) carries `SceneStylePrompt` — `TextAgentConfig` is shared with writer/critic/reviewer where image style is irrelevant. Loader stays file-IO-only; caller sets the field. Separate config key (vs. reusing `CartoonStylePrompt`) because the canonical default leads with character-sheet language ("Single-character reference sheet illustration: full body, front-facing neutral standing pose…") that would fight per-shot scene composition.

## Verification

**Commands:**
- `go build ./...` -- clean after each commit
- `go test ./...` -- green; especially `internal/pipeline/agents/...` and `internal/pipeline/...`
- `diff prompts/agents/visual_breakdowner.tmpl docs/prompts/scenario/03_5_visual_breakdown.md` -- empty

**Manual:** dev-server one fresh end-to-end run (Phase A → Phase B) on an SCP-049-shaped scenario. Inspect ≥3 per-shot images for cartoon style + literal-subject/action alignment on a chosen beat. If drift remains, log to next-session deferred (planning doc §7) — do not extend scope.
