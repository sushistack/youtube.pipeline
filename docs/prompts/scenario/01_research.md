# Stage 1: SCP Research & Visual Identity Analysis

You are a creative director preparing materials for a viral SCP YouTube video about {scp_id}. You need to identify the most dramatic, visually striking, and emotionally resonant elements.

## Source Data

### SCP Fact Sheet
{scp_fact_sheet}

### SCP Full Document
{main_text}

{glossary_section}

## Storytelling Format Guide

Use the following format guide to identify narrative hooks and dramatic structure during research.

{format_guide}

## Task

Analyze the provided SCP data and produce a structured research packet containing:

### 1. Core Identity Summary
- Official designation and object class
- Primary anomalous properties (list each distinctly)
- Containment procedures summary
- Discovery/origin context
- Key incidents or test logs

### 2. Visual Identity Profile (Frozen Descriptor)
Create a single dense physical description following this exact format:
- **Silhouette & Build**: overall body shape, height impression, posture
- **Head/Face**: head shape, facial features or covering, eyes
- **Body Covering**: surface material, texture, color, markings
- **Hands & Limbs**: limb structure, appendages, notable features
- **Carried Items**: objects held or attached
- **Organic Integration Note**: how organic/inorganic elements merge (if applicable)

This profile will be reused verbatim across all image prompts for visual consistency.

### 3. Key Dramatic Beats
Identify dramatic moments from the document suitable for video scenes. Target count depends on intended video length (each beat → ~1 scene → ~1 image of 25-35 sec narration; see `format_guide.md` Section E):
- ~5 min video: 10-14 beats
- ~10 min video: 18-24 beats
- ~15 min video: 28-36 beats

Rules for beat granularity:
- **One beat = one visual moment** (a single stage, a single decisive action, a single emotional shift). If you find yourself writing "X happens, then Y happens, then Z happens" in one beat, split it into multiple beats.
- Each beat must be visually depictable in a single image.
- Order from introduction to climax.
- Note the emotional tone of each beat.

### 4. Environment & Atmosphere Notes
- Primary settings/locations described
- Lighting conditions mentioned or implied
- Ambient sounds or environmental factors
- Overall mood and horror subgenre

### 5. Numeric Facts (REQUIRED — narrator commentary fuel)

Extract **at least 6** numbers / ratios / counts / dates from the source document. These feed the writer's numeric-anchor commentary mode (e.g. "신체의 87%가 부패한 상태에서도", "총 17번의 시도 중 12번이"). Without dense numeric anchors, narration drifts into vague description and loses the golden-channel feel.

Format each entry as:

- `<key>: <fact text in Korean>` (source: <which part of the source — incident log, containment proc, addendum, etc.>)

Rules:
- Numbers must come **directly** from the source document. Do NOT estimate, round, or fabricate. If the source says "approximately", preserve that hedge.
- Prefer concrete countables (incidents, casualties, percentages, durations, distances, dates) over derived figures.
- Each fact must be self-contained — narrator must be able to drop it into a sentence without further context.
- If the source genuinely lacks 6 numerics, list as many as exist and note the shortfall explicitly. Do not pad with vague figures.

### 6. Narrative Hooks (CRITICAL for YouTube retention)
- **Opening hook** (first 5 seconds): Write 3 candidate hooks using different hook types (Question, Shock, Mystery, Contrast). Each must be a single punchy Korean sentence that grabs attention WITHOUT mentioning SCP classification.
- **Mid-video twist**: The moment where the viewer's understanding of this SCP fundamentally changes
- **Closing mystery**: An unresolved element that leaves viewers with lingering unease
- **"What if" moment**: A hypothetical scenario that places the viewer inside the SCP encounter

Output the research packet as structured text with clear section headers.
