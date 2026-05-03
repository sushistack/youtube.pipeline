# Stage 3 (v2): Beat Segmentation — Per-Act

Stage 1 (`03_writing.md`) produced a continuous Korean monologue for one act. Your job in stage 2 is to **segment that monologue into 8–10 visual beats**. Each beat anchors one downstream visual shot to a contiguous slice of the monologue text.

You do NOT rewrite the monologue. You do NOT generate visual prompts. You produce **rune-offset slices into the given monologue** plus per-beat visual metadata.

## Inputs

### Act under segmentation

- **act_id**: `{act_id}`
- **act mood (from stage 1)**: `{act_mood}`
- **act key_points (from stage 1)**:
{act_key_points}

### Monologue text (treat as immutable)

```
{monologue}
```

- Total rune count: `{monologue_rune_count}` (use this as the inclusive upper bound for `end_offset`).
- Offsets are **rune indices** (Unicode code points after NFC), NOT byte offsets. The monologue you see is exactly what the segmenter validator will count `utf8.RuneCountInString` on.

### SCP visual identity (for metadata)

{scp_visual_reference}

### SCP fact tags (canonical research, available for fact_tags assignment)

{fact_tag_catalog}

## Segmentation Rules (HARD)

1. **Beat count**: 8 ≤ `len(beats)` ≤ 10. Validation rejects anything outside this range.
2. **Coverage & ordering**: Beats are **contiguous and monotonically non-overlapping**:
   - `beats[0].start_offset = 0`.
   - For every adjacent pair: `beats[i].end_offset == beats[i+1].start_offset` (no gap, no overlap).
   - `beats[len-1].end_offset = {monologue_rune_count}` (cover to the end).
   - `start_offset < end_offset` for every beat (no zero-width).
3. **Slice respects sentence boundaries when feasible**: prefer beat cuts that land on `.`, `?`, `!`, `다.`, `요.`, `죠.`, `세요.` punctuation or paragraph breaks. Cutting mid-sentence is allowed only if the sentence is unusually long; never cut mid-word.
4. **Beats target ~10–15 seconds of voiceover each** (assuming hada-pace ~7–8 KR chars/sec). With monologues of ~480–2080 runes per act and 8–10 beats, beat slices typically run 50–250 runes.

## Per-Beat Metadata Rules

Every beat has the same metadata fields. Each MUST be present and non-empty (except `fact_tags` which can be `[]`).

- `mood`: short phrase ("dread", "panic", "calm reflection"). May differ from act_mood for moments that shift tone.
- `location`: where the visual shot takes place. Concrete English noun phrase ("research lab", "containment hallway", "rooftop at dusk"). NEVER empty.
- `characters_present`: array of identifiers, ≥1 entry. Use canonical names where available (`SCP-049`, `D-9341`, `Dr. Bright`, `Mobile Task Force Epsilon-11`); fall back to specific role labels (`Observer-2`, `Researcher-1`) — NEVER the generic placeholder `"unknown"` or empty `[]`.
- `entity_visible`: boolean. `true` if the SCP entity is on-camera in this beat; `false` otherwise.
- `color_palette`: short comma-separated list (`"alarm red, cold gray"`). Driven by act_mood + beat mood. NEVER empty.
- `atmosphere`: one-line sensory descriptor (`"creeping silence with low hum"`). NEVER empty.
- `fact_tags`: 0+ entries from the canonical fact tag catalog above. Attach only if the beat's monologue slice references that fact (numeric anchor, named entity, dated event). Stage-2 does NOT invent new facts — pull from the catalog or leave empty.

## Output Contract (Stage 2)

You MUST output **exactly one JSON object** (no surrounding prose, no markdown fences):

```json
{
  "act_id": "{act_id}",
  "beats": [
    {
      "start_offset": 0,
      "end_offset": 95,
      "mood": "...",
      "location": "...",
      "characters_present": ["..."],
      "entity_visible": true,
      "color_palette": "...",
      "atmosphere": "...",
      "fact_tags": [{"key": "...", "content": "..."}]
    }
    /* ... 7-9 more beats, contiguous ... */
  ]
}
```

## Pre-Output Self-Check (MANDATORY before outputting JSON)

1. `act_id` literally `{act_id}` ?
2. `len(beats)` ∈ [8, 10] ?
3. `beats[0].start_offset == 0` ?
4. `beats[-1].end_offset == {monologue_rune_count}` ?
5. For every adjacent pair, `prev.end_offset == next.start_offset` (no gap, no overlap) ?
6. Every beat has `start_offset < end_offset` ?
7. Every beat has non-empty `mood`, `location`, `color_palette`, `atmosphere` ?
8. Every beat has `len(characters_present) >= 1` with no `"unknown"` placeholder ?
9. Every `fact_tag` entry came from `{fact_tag_catalog}` (no fabricated facts) ?
10. JSON 외 다른 텍스트가 출력에 섞여 있지 않은가?
