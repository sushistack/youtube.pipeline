# Input Spec
## SCP Multi-Agent Script Pipeline — Brownfield Quality Uplift

> **Mode**: Brownfield (existing system, incremental refactor)
> **Reference benchmark**: SCP Explained YouTube channel (2.42M subs)
> **Pipeline language**: Go
> **Target output**: Korean SCP scripts for 3-5min animated YouTube videos

---

## 0. Brownfield Context (READ FIRST)

This is **NOT a greenfield rewrite**. There is an existing, working Go-based 
multi-agent pipeline. The Quick-Dev cycle must:

1. **Preserve** all currently passing pipeline runs — no behavioral regressions
2. **Discover before modifying** — Barry must read existing code structure 
   before proposing changes
3. **Isolate changes** — refactors land behind feature flags or in additive 
   files; existing files modified only when necessary
4. **Migrate incrementally** — one agent at a time, each validated by 
   regression tests before moving on
5. **Honor existing conventions** — match current package layout, error 
   handling style, logging, and config patterns rather than imposing new ones

### Mandatory pre-implementation steps for Barry

Before writing any code, Barry MUST:

- [ ] Run a repo scan and produce `docs/brownfield/current_state.md` with:
  - [ ] Current package structure (`internal/`, `cmd/`, etc.)
  - [ ] Existing agent interfaces and how they communicate (channels, function 
        calls, message bus, MCP, HTTP, etc.)
  - [ ] Current prompt storage location and format (.md, .tmpl, embedded 
        strings, JSON, YAML)
  - [ ] LLM client abstraction (Anthropic SDK directly? wrapper? streaming?)
  - [ ] Existing test coverage and test style (table-driven, testify, etc.)
  - [ ] Any existing schemas, validators, or contract definitions
  - [ ] Config loading mechanism (`viper`, `envconfig`, raw `os.Getenv`)
- [ ] Produce `docs/brownfield/change_impact.md` mapping each proposed change 
      to:
  - [ ] Files touched
  - [ ] Public API surface affected
  - [ ] Backward compatibility risk (none / low / medium / high)
  - [ ] Rollback strategy
- [ ] Wait for explicit user approval of `change_impact.md` before coding

If any of the above is unclear from the codebase, Barry asks ONE consolidated 
clarification question rather than guessing.

---

## 1. Goal of This Quick-Dev Cycle

Refactor agent prompts, pipeline contracts, and the critic gate so that 
generated scripts match SCP Explained's storytelling quality — while 
remaining authentic to Korean SCP fandom standards (KO wiki terminology, 
CC BY-SA attribution, no AI-generated SCP content).

**Out of scope** for this cycle (do NOT touch):
- LLM provider switching
- Adding new agents to the pipeline
- Animation rendering / asset generation code
- Database or storage layer changes
- Deployment or CI configuration

---

## 2. Existing Pipeline Stages (As Documented by User)

```
Research → Structure → Script writing → Shot planning → Review 
  → Critic pass → Scenario revision (loop)
```

Barry must verify this matches reality during the repo scan. If the actual 
pipeline differs, surface the discrepancy before proceeding.

---

## 3. Quality Gap (What "Good" Looks Like)

### SCP Explained's signature elements to replicate

1. **Cold open hook within 15 seconds** — most striking image first, never 
   channel intro
2. **Information drip** — never dump containment procedures upfront; reveal 
   in layers
3. **One concrete incident dramatized per video** — not pure exposition
4. **Twist/revelation in 70–85% of duration** — narrative climax position
5. **Unresolved cliffhanger outro** — bridges to next video
6. **Sensory-specific language** — "검은 액체가 흘러내렸죠" not "끔찍했어요"
7. **Visual direction reusable across scenes** — shot planner gets clear cues
8. **2nd-person immersion sections** alternating with 3rd-person clinical

### Likely current pain points (Barry to verify, not assume)

- Inconsistent JSON schemas between agents
- No shared style guide enforced across agents
- Critic pass criteria ad-hoc rather than rubric-based
- Korean narration cadence not specifically tuned

---

## 4. Pipeline Contract Specification

Each agent must consume and produce JSON conforming to a versioned schema. 
Barry should add these as Go structs in a NEW package (e.g. 
`internal/contract/v2/`) without breaking existing v1 contracts. Run both in 
parallel via a feature flag until v2 is validated.

### 4.1 Research Agent Output

```go
type ResearchOutput struct {
    SCPNumber           string            `json:"scp_number"`
    ObjectClass         string            `json:"object_class"`
    Author              string            `json:"author"`
    WikiURL             string            `json:"wiki_url"`
    Branch              string            `json:"branch"`
    OriginalSummary     string            `json:"original_summary"`
    AnomalousProperties []string          `json:"anomalous_properties"`
    KeyIncidents        []Incident        `json:"key_incidents"`
    RelatedSCPs         []string          `json:"related_scps"`
    LoreConnections     []string          `json:"lore_connections"`
    KoreanTerms         map[string]string `json:"korean_terms"`
}
```

### 4.2 Structure Agent Output

```go
type StructureOutput struct {
    NarrativeArc   string `json:"narrative_arc"`
    HookAngle      string `json:"hook_angle"`
    TwistPoint     string `json:"twist_point"`
    EndingHook     string `json:"ending_hook"`
    Tone           string `json:"tone"`
    TargetDuration int    `json:"target_duration_seconds"`
    SceneCount     int    `json:"scene_count"`
}
```

### 4.3 Script Writing Agent Output

```go
type ScriptOutput struct {
    TitleCandidates   []string    `json:"title_candidates"`
    Scenes            []Scene     `json:"scenes"`
    OutroHookKO       string      `json:"outro_hook_ko"`
    SourceAttribution Attribution `json:"source_attribution"`
}

type Scene struct {
    SceneID         int    `json:"scene_id"`
    Section         string `json:"section"`
    DurationSeconds int    `json:"duration_seconds"`
    NarrationKO     string `json:"narration_ko"`
    VisualDirection string `json:"visual_direction"`
    EmotionCurve    string `json:"emotion_curve"`
    SFXHint         string `json:"sfx_hint"`
}
```

### 4.4 Shot Planning Agent Output

```go
type ShotPlanOutput struct {
    Shots         []Shot           `json:"shots"`
    AssetReuseMap map[string][]int `json:"asset_reuse_map"`
}

type Shot struct {
    SceneID         int     `json:"scene_id"`
    ShotID          int     `json:"shot_id"`
    Background      string  `json:"background"`
    Foreground      []string `json:"foreground"`
    CameraMove      string  `json:"camera_move"`
    DurationSeconds float64 `json:"duration_seconds"`
    Lighting        string  `json:"lighting"`
    Notes           string  `json:"notes"`
}
```

### 4.5 Critic Pass Output

```go
type CriticReport struct {
    OverallScore     int            `json:"overall_score"`
    Passed           bool           `json:"passed"`
    RubricScores     map[string]int `json:"rubric_scores"`
    Failures         []Failure      `json:"failures"`
    RevisionPriority string         `json:"revision_priority"`
}
```

### Migration constraint
- v1 contracts must keep working — implement an adapter layer
- Behind feature flag `PIPELINE_CONTRACT_VERSION=v2`, route through new schema
- Old callers without the flag continue to use v1

---

## 5. Critic Rubric (Hard Quality Gate)

The Critic agent scores 10 criteria, 10 points each. Total ≥80 to pass; 
below 80 routes back to Scenario Revision.

| # | Criterion | Pass condition |
|---|-----------|---------------|
| 1 | Hook lands ≤15s | Scene 1 starts with striking image, no meta-intro |
| 2 | Information drip | Containment info revealed across ≥3 scenes |
| 3 | Concrete incident | At least one dramatized event with sensory detail |
| 4 | Twist position | Reveal lands at 70-85% of total duration |
| 5 | Unresolved outro | Final line is question/hint/cross-reference |
| 6 | Sentence rhythm | Avg KR sentence ≤18 chars in tense sections |
| 7 | Sensory language | <2 abstract emotion words ("끔찍", "무서운") per script |
| 8 | POV consistency | No 2nd/3rd person mixing within same scene |
| 9 | SCP fidelity | All anomalous properties match research input |
| 10 | Visual reusability | ≥40% of shots reuse a background/asset |

The Critic must return a structured `CriticReport` with per-criterion scores 
and specific failure quotes for any score <8.

---

## 6. Style Guide (Single Source of Truth)

Create `/configs/style_guide.yaml` (NEW file, no existing config disturbed). 
All agents load this at init.

```yaml
narration:
  avg_sentence_length_tense: 18
  avg_sentence_length_calm: 28
  rhetorical_question_per_minute: 1
  forbidden_openings:
    - "안녕하세요"
    - "오늘 소개할"
    - "이번 영상에서는"
  preferred_endings: ["...죠.", "...니다.", "...어요."]
  forbidden_endings: ["...했다."]

korean_scp_terms:
  Foundation: 재단
  MTF: 기동특무대
  D-class: D계급
  Keter: 케테르
  Euclid: 유클리드
  Safe: 안전
  Thaumiel: 타우미엘
  Site: 기지
  Containment Breach: 격리 실패

attribution_template_ko: |
  본 영상은 SCP 재단 위키 {scp_number}({author})를 각색했습니다.
  원문: {wiki_url}
  CC BY-SA 3.0 라이선스에 따라 제작되었습니다.
```

Loader package: `internal/style/` (new). Existing config loader remains 
untouched.

---

## 7. Prompt Refactor Strategy

### Current state assumption (Barry to verify)
Prompts are likely embedded as Go string constants or scattered .md files.

### Target state
- Move all agent prompts to `/prompts/agents/{agent_name}.tmpl`
- Load via `embed.FS` at compile time
- Each prompt template receives a typed Go struct as data

```go
//go:embed prompts/agents/*.tmpl
var agentPrompts embed.FS
```

### Migration approach
1. Step 1: Create `/prompts/` directory with new templates ALONGSIDE existing 
   prompt code
2. Step 2: Add a switch in each agent: if `USE_TEMPLATE_PROMPTS=true`, load 
   from FS; otherwise use legacy embedded string
3. Step 3: A/B test on a small batch of SCPs
4. Step 4: Once parity confirmed, remove legacy strings (but only with user 
   approval)

### New prompt template — Script Writing agent

Template path: `/prompts/agents/script_writer.tmpl`

The template should incorporate the writing rules from this spec (hook ≤15s, 
twist at 70-85%, sensory language, KR sentence rhythm, POV consistency, 
forbidden openings/endings, source attribution). All non-trivial logic stays 
in Go; the template is a prompt with `{{.Variable}}` placeholders only.

Variables passed in:
- `{{.ResearchOutput}}` — full Research agent output JSON
- `{{.Structure}}` — full Structure agent output JSON
- `{{.StyleGuide}}` — relevant subsection of style_guide.yaml
- `{{.TargetDuration}}` — int seconds

Same pattern for the other agents.

---

## 8. Testing Strategy (Brownfield-Critical)

### 8.1 Regression test suite
Before any modification, Barry must:
- Identify 3-5 known-good outputs from the existing pipeline (golden files)
- Save them under `testdata/golden/v1/`
- Add a regression test that runs the v1 pipeline (feature flag off) and 
  asserts output structurally matches the golden files

### 8.2 New v2 contract tests
For each new schema, add:
- JSON schema validation test
- Roundtrip serialization test
- Adapter test (v1 ↔ v2 conversion)

### 8.3 Critic rubric tests
- Synthetic "bad script" fixtures that should fail each criterion
- Synthetic "good script" fixture that passes ≥80
- Edge case: script that scores exactly 80

### 8.4 Run command
All tests must pass via the project's existing test command (likely 
`go test ./...` or `make test`). Do NOT introduce new test runners.

---

## 9. Acceptance Criteria

This Quick-Dev cycle is DONE when:

- [ ] `docs/brownfield/current_state.md` exists and is reviewed
- [ ] `docs/brownfield/change_impact.md` is approved by user
- [ ] All v2 contracts defined as Go structs in `internal/contract/v2/`
- [ ] `style_guide.yaml` loaded by all agents
- [ ] Critic rubric implemented with all 10 criteria
- [ ] At least Script Writing agent migrated to new prompt template
- [ ] Regression tests pass with feature flag off (v1 behavior intact)
- [ ] New tests pass with feature flag on (v2 path)
- [ ] One end-to-end run on a known SCP (recommend SCP-173) produces a 
      script that scores ≥80 on the new critic rubric
- [ ] Diff summary in PR description lists every file touched, every public 
      API change, and rollback steps

---

## 10. Anti-Patterns (Do NOT Do)

- ❌ Don't rewrite agents from scratch
- ❌ Don't change function signatures of public agent interfaces unless the 
  spec explicitly requires it
- ❌ Don't introduce new dependencies without justification — match existing 
  ones
- ❌ Don't modify the LLM client wrapper
- ❌ Don't enable v2 by default in this cycle — flag-gated only
- ❌ Don't delete legacy prompts in this cycle — deprecation comes after 
  validation
- ❌ Don't add channel CTAs ("구독", "좋아요") into narration prompts — those 
  belong in a separate end-card system

---

## 11. Risks & Mitigations

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| v1 pipeline regression | Medium | Golden file regression suite, flag-gate v2 |
| LLM output non-conforming to new JSON schema | High | Add retry-with-correction loop in agent runner; surface in test fixtures |
| Critic too harsh, blocks all output | Medium | Calibrate rubric on 5 known-good SCP Explained transcripts first |
| Korean term mapping incomplete | Low | Style guide is YAML — extensible without code change |
| Prompt template size overrun (context limit) | Medium | Pass only the structured fields needed per agent, not full upstream output |

---

## 12. References

- BMAD Method: https://github.com/bmad-code-org/BMAD-METHOD
- SCP Explained channel: https://www.youtube.com/@scp
- SCP Foundation Korean Wiki: https://scpko.wikidot.com
- License: CC BY-SA 3.0 (https://creativecommons.org/licenses/by-sa/3.0/)

---

## 13. One-Liner for Slash Command

When invoking `/bmad-quick-dev`, paste this spec as the input. Barry should 
treat sections 0 and 9 as the contract: section 0 = how to start, section 9 = 
how to finish.