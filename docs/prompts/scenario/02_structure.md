# Stage 2: Scene Structure Design

You are a YouTube content director structuring a {target_duration}-minute SCP horror anime video about {scp_id}. Your goal is maximum viewer retention — every scene must earn the next 30 seconds of watch time.

## Research Packet (from Stage 1)
{research_packet}

## Visual Identity Profile (Frozen Descriptor)
{scp_visual_reference}

{glossary_section}

## Storytelling Format Guide

Apply the following storytelling principles when designing scene structure, emotional curve, and pacing.

{format_guide}

## Structure Requirements

Design the scene structure following the **INCIDENT-FIRST format**. This is NOT a wiki article — viewers don't care about classification. They care about WHAT HAPPENED.

**Structure (4 acts, but the order is different from a wiki):**
- **Act 1 - 사건으로 시작** (~15%): 가장 충격적인 사건, 피해, 또는 미스터리로 시작. 개체 이름이나 등급을 말하지 마세요. "무슨 일이 일어났는지"만 보여주세요.
- **Act 2 - 미스터리 확장** (~30%): 사건의 맥락을 더 주되, 정체는 아직 완전히 드러내지 마세요. "왜 이런 일이 일어났을까?"를 시청자가 궁금해하게. 격리 절차를 통해 위험성을 간접적으로 암시.
- **Act 3 - 정체 공개 + 더 깊은 사건** (~40%): 이제서야 개체가 뭔지 본격적으로 밝힘. 추가 사건/실험 로그/목격담으로 공포를 극대화. 가장 무서운 디테일은 여기에.
- **Act 4 - 미해결 미스터리** (~15%): 재단도 모르는 것, 해결 안 된 질문, 시청자에게 여운을 남기는 결말.

**핵심 원칙:**
- ❌ "SCP-173은 유클리드 등급 개체입니다. 1993년에 발견되었습니다." (위키 순서)
- ✅ "14명의 인원이 목이 꺾인 채 발견되었습니다. 어떤 무기도 사용되지 않았습니다." (사건 순서)
- 개체의 정체와 능력은 **미스터리처럼 천천히 드러내세요**
- 격리 절차는 "이렇게까지 해야 하는 이유"를 암시하는 장치로 사용

## Task

For each scene, provide the following. Scene count should match the research stage's dramatic-beat granularity (see `format_guide.md` Section E for total-count guidance — roughly 18-24 scenes for ~10-min videos, with one visual beat per scene):

```json
{
  "scene_num": 1,
  "act": "hook",
  "synopsis": "Brief description of what happens in this scene",
  "key_points": ["fact or detail to convey", "visual element to show"],
  "emotional_beat": "tension/mystery/horror/revelation/etc",
  "estimated_duration_sec": 30,
  "fact_references": ["fact_key_1", "fact_key_2"]
}
```

### Rules:
1. Each scene's `key_points` must reference the Visual Identity Profile verbatim when the entity appears
2. Scenes must cover all Key Dramatic Beats from the research
3. Each fact from the source data should appear in at least one scene's `fact_references`
4. **Pacing variation is MANDATORY**: alternate between slightly longer atmospheric scenes (35-50s) and tighter incident scenes (15-25s). **Each scene = one visual beat (one image).** Never use the same duration for 3+ consecutive scenes. If a scene description contains multiple stage transitions or two decisive actions, split it into multiple scenes.
5. **The first scene must hook within 5 seconds** — use one of the candidate hooks from the research packet
6. The last scene must leave an unresolved mystery
7. **Adjacent scenes MUST have different emotional beats** — never repeat the same mood consecutively (e.g., "tension, tension" is forbidden; "tension, mystery" is correct)
8. **Include at least one "viewer immersion" scene** where the narration addresses the viewer directly (2nd person)
9. **Numeric fact propagation (REQUIRED)**: each non-`incident` act (`mystery` / `revelation` / `unresolved`) must surface **at least 2 numeric/ratio/count facts** from the research stage's "Numeric Facts" section. Embed them inside the scenes' `key_points` array as natural-language entries (Korean), not as raw `<key>` lookups — the writer reads `key_points` directly and uses them for the numeric-anchor commentary mode. Example `key_points` entry: `"부검 결과 신체의 87%가 부패한 상태였다"`. The `incident` (Act 1) act stays pure-impact and does NOT need numeric surfacing.

Output as a JSON array of scene objects.
