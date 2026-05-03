# Stage 3.5: Visual Breakdown - Scene {scene_num}

You convert one Korean narration scene into a schema-ready shot breakdown for downstream rendering.

The scene's narration is split into discrete visual beats by the upstream writer. Produce **one shot per beat, in beat order**.
Return JSON only.
Do not use markdown fences.

## Scene Context

- Scene Number: {scene_num}
- Location: {location}
- Characters Present: {characters_present}
- Color Palette: {color_palette}
- Atmosphere: {atmosphere}
- Estimated TTS Duration (seconds): {estimated_tts_duration_s}
- Required Shot Count: {shot_count}  ← equals number of narration beats below

## Narration Beats (one shot per beat, in this order)

{narration_beats}

## Frozen Visual Identity

Use this descriptor as the source of truth for visual identity (proportions, palette, distinguishing features). You no longer need to copy it verbatim into every `visual_descriptor` — instead, **preserve visual-identity continuity across shots while shifting focal subject per beat**:

{frozen_descriptor}

## SCP Visual Identity

{scp_visual_reference}

## Narration

{narration}

## Output Contract

Return:

{
  "scene_num": {scene_num},
  "shots": [
    {
      "visual_descriptor": "scene-specific visual planning text that preserves identity continuity for this beat",
      "transition": "ken_burns",
      "narration_beat_index": 0
    }
  ]
}

Rules:
- `scene_num` must equal `{scene_num}`
- output exactly `{shot_count}` items in `shots` (one per beat above)
- shot ordering MUST match beat ordering: `shots[i].narration_beat_index` MUST equal `i` (0-based)
- every `visual_descriptor` must be non-empty
- every `visual_descriptor` must preserve visual identity (entity proportions, palette, distinguishing features) so all shots feel cohesive across the video — but **shift the focal subject per beat** (entity / environment / character POV / artifact close-up). Do NOT make every shot start with the same identity description.
- the `transition` field MUST be present and non-empty on every shot — omitting it or returning `""` is a contract violation
- the `transition` value MUST be exactly one of: `ken_burns`, `cross_dissolve`, `hard_cut` — no other values are permitted
- focus on factual consistency with the narration beats and SCP visual identity
- do not add commentary, explanations, or extra keys
