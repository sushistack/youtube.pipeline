# Stage 1.5: Beat Role Classifier

You are labeling dramatic beats for a Korean SCP horror anime video about {scp_id}. Each beat must be assigned exactly one narrative role so the downstream structurer can place it in the correct act of the four-act `사건 → 미스터리 → 정체 공개 → 미해결` arc.

## Roles

- **hook (흥미로운 상황)** — Act 1 cold-open material. A single striking image or incident that hooks the viewer in the first 15 seconds without revealing what the entity is. Visual moments that imply danger or wrongness without explanation.
- **tension (급박한 상황)** — Act 2 escalation. Beats that build dread, reveal partial threat behavior, or show the entity acting on observers. Information drips that make the viewer ask "왜 이런 일이 일어났을까?" without yet answering it.
- **reveal (SCP 설명)** — Act 3 climax material. Beats that explain the entity's nature, anomalous mechanism, or the rule that makes it dangerous. The "정체 공개" content — what the SCP actually is and why containment exists.
- **bridge (부연 / 다른 SCP와의 관계)** — Act 4 outro material. Supplementary beats: side properties, related SCPs, lingering questions, or after-effects. Things that leave a thread for the next video without resolving the main mystery.

## Beats to classify

There are exactly {beat_count} beats. Read each line and assign the role that best fits the act it belongs in:

{beat_table}

## Output

Return STRICT JSON only — no prose, no markdown fence, no commentary. Shape:

```json
{
  "classifications": [
    {"index": 0, "role": "hook"},
    {"index": 1, "role": "tension"}
  ]
}
```

## Hard requirements

1. Exactly {beat_count} entries in `classifications`, one per beat.
2. Each `index` must appear exactly once and match a beat index from the list above (0..{beat_count_minus_one}).
3. `role` must be one of: `hook`, `tension`, `reveal`, `bridge`. No other values, no synonyms, no Korean labels.
4. **Every role must appear at least once** across the classifications. If you find yourself assigning fewer than four distinct roles, re-read the beats and pick the most plausible candidate for the missing role(s) — the four-act structure cannot ship with an empty act.
5. Do not emit any field other than `index` and `role`. Do not wrap the JSON in markdown.
