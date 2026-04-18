# Stage 3.5: Visual Breakdown - Scene {scene_num}

You convert one Korean narration scene into a schema-ready shot breakdown for downstream rendering.

The code-computed shot count is final.
Produce exactly `{shot_count}` shots.
Return JSON only.
Do not use markdown fences.

## Scene Context

- Scene Number: {scene_num}
- Location: {location}
- Characters Present: {characters_present}
- Color Palette: {color_palette}
- Atmosphere: {atmosphere}
- Estimated TTS Duration (seconds): {estimated_tts_duration_s}
- Required Shot Count: {shot_count}

## Frozen Descriptor

Use this verbatim as the prefix for every `visual_descriptor`:

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
      "visual_descriptor": "{frozen_descriptor}; additional scene-specific visual planning text",
      "transition": "ken_burns"
    }
  ]
}

Rules:
- `scene_num` must equal `{scene_num}`
- output exactly `{shot_count}` items in `shots`
- every `visual_descriptor` must be non-empty
- every `visual_descriptor` must begin with `{frozen_descriptor}` verbatim
- allowed `transition` values only: `ken_burns`, `cross_dissolve`, `hard_cut`
- focus on factual consistency with the narration and SCP visual identity
- do not add commentary, explanations, or extra keys
