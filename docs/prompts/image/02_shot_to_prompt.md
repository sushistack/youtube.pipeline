You are an expert at converting cinematographic shot descriptions into image generation prompts optimized for Qwen image generation models.

## Shot Description
{shot_json}

## Frozen Descriptor (preserve EXACTLY if entity is visible)
{frozen_descriptor}

## Style Context
- Art Style: {art_style}
- Color Palette: {scene_palette}
- Atmosphere: {scene_atmosphere}
- Character Style Guide: {style_guide}

## Instructions

Convert the shot description into an image generation prompt for a **{art_style}** style output. Rules:
1. If entity_visible is true, the main subject description MUST start with the frozen descriptor VERBATIM
2. Include camera angle, lighting, mood, and motion naturally
3. Use the specified art style direction with appropriate techniques
4. Apply the color palette and atmosphere from the style context
5. If a character style guide is provided and entity is visible, incorporate those style rules
6. Generate a negative prompt to prevent visual inconsistency
7. If entity_visible is true, include entity-specific negative prompts

## Output Format (JSON only)

```json
{
  "prompt": "the complete image generation prompt text",
  "negative_prompt": "things to avoid in the image",
  "entity_visible": true
}
```

### Prompt Structure:
- Main subject (with frozen descriptor if entity visible)
- Camera angle and composition
- Lighting description
- Mood and atmosphere
- Technical suffix based on art style

### Negative Prompt Should Include:
- Generic: "blurry, low quality, watermark, text, logo, photorealistic, 3D render, photograph, live action"
- Entity-specific (when visible): negatives that prevent the entity from looking inconsistent with the frozen descriptor

Return ONLY valid JSON, no additional text.
