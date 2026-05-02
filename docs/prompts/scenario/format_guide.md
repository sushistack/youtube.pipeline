## Format Reference Guide: SCP YouTube Storytelling Principles

> Evidence-based storytelling principles for audience retention. Derived from channel format analysis and first-principles audience psychology.

---

### A. Hook Type Library

Every SCP video must open with a hook within the first 15 seconds. Choose one:

| Hook Type | Pattern | SCP Example |
|-----------|---------|-------------|
| **Question** | Pose an unanswerable question about the anomaly | "What happens when you blink near SCP-173?" |
| **Shock** | Lead with the most extreme consequence | "This sculpture has killed 14 Foundation personnel." |
| **Mystery** | Present a redacted or unexplained detail | "There are 7 pages of containment procedures. Six of them are [REDACTED]." |
| **Contrast** | Juxtapose the mundane with the anomalous | "It looks like an ordinary concrete statue in a storage room. It moves when you look away." |

**Rules:**
- The hook must relate directly to the SCP's core anomaly
- Never start with classification or background lore
- The first sentence of Scene 1 narration IS the hook

---

### B. Progressive Disclosure Pattern

Never reveal everything at once. Structure information in 3 waves:

| Wave | Content | Timing |
|------|---------|--------|
| **Wave 1: The Surface** | Basic appearance, classification, what it seems to be | Act 1 (first 15%) |
| **Wave 2: The Depth** | True anomalous properties, containment details, incident logs | Act 2-3 (middle 70%) |
| **Wave 3: The Abyss** | Unresolved mysteries, [REDACTED] implications, what the Foundation doesn't know | Act 4 (final 15%) |

**SCP-specific techniques:**
- Use [REDACTED] and [DATA EXPUNGED] as narrative tension tools — mention them early, explore implications later
- Clearance level escalation: start with public-facing info, end with O5-level secrets
- "What the document doesn't say" is often more compelling than what it does

---

### C. Emotional Curve Design

Adjacent scenes MUST differ in emotional tone. Monotone pacing kills retention.

**Mood transition rules:**
- Never place two scenes with identical mood back-to-back
- Minimum 2-3 major tone shifts per scenario (e.g., mystery -> horror -> awe -> lingering unease)
- After a high-tension scene, allow a brief decompression before the next escalation
- The final scene should leave emotional residue — unease, wonder, or dread that lingers after the video ends

**Emotional arc template:**
```
Scene 1: Intrigue/curiosity (hook)
Scene 2-3: Building tension (properties, early incidents)
Scene 4-5: Peak horror/shock (worst incidents, true nature revealed)
Scene 6-7: Shift to awe or empathy (recontextualization, human cost)
Final scene: Lingering unease (unresolved mystery, open questions)
```

---

### D. Viewer Immersion Devices

Transform passive viewers into participants. Use at minimum 3 of these per scenario:

| Device | Technique | Example |
|--------|-----------|---------|
| **2nd Person Address** | Place the viewer inside the scenario | "Imagine you're the D-class personnel sent into the chamber." |
| **Sensory Description** | Engage senses beyond sight | "The air smells of wet concrete and copper. The only sound is your own breathing." |
| **Situation Hypothetical** | Pose "what would you do" moments | "You hear it move behind you. Do you turn around?" |
| **Foundation Employee POV** | Narrate as if briefing the viewer | "Your assignment: maintain visual contact at all times. Do not blink." |
| **Scale Comparison** | Make abstract threats concrete | "Its containment cell costs more per day than an aircraft carrier." |

---

### E. Scene Count & Pacing Guide

**Core principle: ONE SCENE = ONE VISUAL BEAT.** Each scene maps to a single image in the final video, so every scene must depict a single moment that can be captured in one frame. Cramming multiple stage transitions or distinct actions into one scene is forbidden — it produces narration the visual track cannot support.

Match scene count to target video duration:

| Target Duration | Scene Count | Avg Scene Length | Pacing Notes |
|----------------|-------------|------------------|--------------|
| ~5 minutes  | 10-14 scenes | 25-35 sec | One visual beat per scene; tight cutting |
| ~10 minutes | 18-24 scenes | 25-35 sec | More breathing room, atmosphere scenes can run slightly longer |
| ~15 minutes | 28-36 scenes | 25-35 sec | Sub-incidents and deeper lore, still one-beat-per-scene |

**Per-scene narration length (Korean):** 100-180 chars target, 220 chars hard cap.

**Pacing principles:**
- **Scene = single visual unit.** If a scene contains a stage change, a new character entering, or two decisive actions, split it.
- Vary scene durations within the band — not every scene the same length.
- Action/incident scenes: shorter (15-25 sec), rapid delivery, single decisive moment.
- Atmosphere/buildup scenes: slightly longer (35-50 sec), sensory detail, but still one beat.
- The hook scene (Scene 1) should be 15-20 seconds — get to the point fast.

**Note on upstream scene count:** total scene count = `len(dramatic_beats) × scenesPerBeat` (currently `scenesPerBeat = 2`). The structurer fans each beat out to ~2 scenes, and the writer expands a single beat across multiple scenes by separating its visual moments — do NOT compensate at writing time by cramming multiple visual moments into one scene. Typical SCP corpus produces 6-10 raw beats → 12-20 scenes → 6-10 min videos. To go longer, the corpus needs more `key_visual_moments` / `anomalous_properties`, or scenesPerBeat must be tuned upward (currently fixed in `structurer.go`).

---

### F. Act Structure & Ratios (INCIDENT-FIRST)

Use an incident-first 4-act structure. Start with WHAT HAPPENED, not WHAT IT IS. The entity's identity is a mystery that unfolds gradually.

| Act | Purpose | Ratio | Key Deliverable |
|-----|---------|-------|-----------------|
| **Act 1: 사건으로 시작** | 가장 충격적인 사건/피해로 시작. 개체 이름/등급 언급 안 함 | ~15% | Hook + "무슨 일이 일어났는가" |
| **Act 2: 미스터리 확장** | 맥락 추가. 격리 절차로 위험성 암시. 정체 아직 미공개 | ~30% | "왜 이런 일이?" + 간접적 공포 |
| **Act 3: 정체 공개 + 깊은 공포** | 본격적으로 개체 설명 + 추가 사건/실험 로그 | ~40% | 핵심 능력 + 가장 무서운 디테일 |
| **Act 4: 미해결 미스터리** | 재단도 모르는 것, 열린 결말, 여운 | ~15% | 미해결 질문 + closing hook |

**핵심:**
- ❌ 위키 순서: 분류 → 설명 → 격리 → 사건
- ✅ 유튜브 순서: 사건 → 미스터리 → 정체 공개 → 미해결
- 시청자는 "이게 뭔지"가 아니라 "무슨 일이 일어났는지"에 먼저 반응함
- 개체의 정체를 미스터리로 활용하면 Act 3까지의 retention이 극적으로 올라감
