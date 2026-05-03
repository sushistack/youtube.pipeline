---
title: 'Fewshot Exemplar Injection (writer prompt)'
type: 'feature'
created: '2026-05-03'
status: 'done'
baseline_commit: '4242eb91f76197d5178804e385b2af03b043bda7'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/spec-structure-narrative-roles.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** writer prompt이 추상 rule (hook ≤15초, 2인칭 immersion, 100~180자 등)만으로 한국 SCP narration 톤·리듬·연결어를 학습시키고 있음. 실제 viral 한국 SCP 채널의 token-level 패턴을 LLM이 본 적이 없어, 출력이 구조적으론 맞지만 한국 native viewer가 느끼는 "viral 한국 SCP narration 톤"과 거리가 있음.

**Approach:** 이미 준비된 한국 native SCP YouTube 채널 narration exemplar 2편 (`docs/exemplars/scp-049.exemplar.md`, `scp-2790.exemplar.md`)을 writer prompt에 in-context learning 신호로 inject. 각 act 작성 시 매칭되는 act의 narration만 inject (per-act 매칭 — incident act 작성 시 두 exemplar의 Act 1만)해서 token cost 효율화. exemplar 파싱은 `LoadPromptAssets` 시점에 한 번만, runtime cost 없음. 변경 후 SCP-049 dogfood 재실행 시 narration 톤 변화로 가설 검증.

## Boundaries & Constraints

**Always:**
- Exemplar source는 `docs/exemplars/*.exemplar.md` 두 파일 (049 + 2790). 두 파일 모두 frontmatter + `## Act 1 — Incident (hook role)` ~ `## Act 4 — Unresolved (bridge role)` 4개 섹션 + `## Notes for fewshot use` 구조. 파서는 이 구조를 가정하고 act별 narration만 추출, Notes 섹션은 제외.
- `PromptAssets.ExemplarsByAct map[string]string` — key=act ID (`incident`/`mystery`/`revelation`/`unresolved`), value=두 exemplar의 해당 act narration을 빈 줄로 구분해 concatenate한 텍스트.
- `renderWriterActPrompt`는 `prompts.ExemplarsByAct[spec.Act.ID]`를 새 placeholder `{exemplar_scenes}`에 substitute. 모르는 act ID로 룩업 실패하면 `domain.ErrValidation` (act ID는 4가지 enum이라 발생 불가).
- exemplar 파일 누락 / 파싱 실패 (act 헤더 4개 중 하나라도 빠짐) → `LoadPromptAssets`가 `domain.ErrValidation` 반환. server start fail-fast.
- writer prompt token 부담 ~6KB 추가 (act당 ~1.5KB). max_tokens 8192 안에서 충분.

**Ask First:**
- `internal/api/`, `internal/service/`, `web/**` 변경 (mixed-uncommitted territory).
- exemplar 파일 추가/삭제 (현재 cycle은 049 + 2790 두 편 고정).

**Never:**
- Critic / reviewer / visual_breakdowner 프롬프트 변경 (별도 cycle).
- exemplar runtime 동적 교체 (e.g. SCP 카테고리별 exemplar 선택). 현재는 두 편 고정 inject.
- Shadow-eval / goldenset 통합 (Phase 3).
- 영문 exemplar 추가 (cross-lingual transfer 약함).
- Critic rubric thresholds 변경.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|---------------|----------------------------|----------------|
| Healthy load | 두 exemplar 파일 정상 | `ExemplarsByAct` 4개 act 모두 non-empty. 각 value는 두 exemplar의 해당 act narration concatenate | n/a |
| Healthy render | writer가 incident act 렌더 | 렌더된 prompt에 두 exemplar의 Act 1 narration 포함, 다른 act narration 미포함 | n/a |
| Exemplar 파일 누락 | `docs/exemplars/scp-049.exemplar.md` 없음 | `LoadPromptAssets` returns `domain.ErrValidation: load prompt asset` | server start fail-fast |
| Act 헤더 1개 누락 | exemplar에 `## Act 3 — Revelation` 없음 | `LoadPromptAssets` returns `domain.ErrValidation: missing act section` | server start fail-fast |
| Notes 섹션 없음 | exemplar에 `## Notes for fewshot use` 없음 | OK — 파서는 4번째 act 끝부터 EOF까지를 act 4 narration으로 처리 | n/a |
| 두 exemplar의 같은 act가 다른 길이 | act 1 = 049가 100 runes, 2790이 80 runes | 두 narration이 빈 줄로 구분되어 concatenate | n/a |

</frozen-after-approval>

## Code Map

- `internal/pipeline/agents/assets.go` — `PromptAssets`에 `ExemplarsByAct map[string]string` 필드 추가. `exemplarPaths` 상수 (`docs/exemplars/scp-049.exemplar.md`, `docs/exemplars/scp-2790.exemplar.md`). `LoadPromptAssets`에서 두 파일 read + `parseExemplar` 호출 + 4개 act 모두 채워졌는지 검증.
- `internal/pipeline/agents/assets_exemplar.go` (NEW) — `parseExemplar(raw string) (map[string]string, error)` 헬퍼. `## Act N — <Name> (<role> role)` 헤더 regex로 4개 섹션 추출. `## Notes` 섹션은 잘라냄. 4개 act 중 하나라도 빠지면 ErrValidation.
- `internal/pipeline/agents/assets_exemplar_test.go` (NEW) — happy path (parse 후 4개 act 키 모두 존재) + missing act header → ErrValidation + Notes 섹션 미포함.
- `internal/pipeline/agents/assets_test.go` — `TestLoadPromptAssets_Happy`에서 `ExemplarsByAct` 4개 act 모두 non-empty 확인 추가.
- `docs/prompts/scenario/03_writing.md` — `### Act-specific guidance` 섹션 위에 `## 참고 예시 (실제 한국 SCP narrator 톤)\n\n다음은 실제 한국 SCP YouTube 채널의 narration 예시입니다. 이 톤·리듬·연결어를 참고하되 그대로 베끼지는 마세요.\n\n{exemplar_scenes}` 블록 추가.
- `internal/pipeline/agents/writer.go` — `renderWriterActPrompt`의 `strings.NewReplacer` 호출에 `"{exemplar_scenes}", prompts.ExemplarsByAct[spec.Act.ID]` 추가. 시작부에 `if _, ok := prompts.ExemplarsByAct[spec.Act.ID]; !ok` 가드 (defensive — 4-enum이라 발생 불가지만 contract 명시).
- `internal/pipeline/agents/writer_test.go` — `sampleWriterAssets()`의 `PromptAssets`에 `ExemplarsByAct: map[string]string{...}` 4개 act 더미 값 추가. `WriterTemplate`에 `{exemplar_scenes}` placeholder 추가.
- `internal/pipeline/agents/prompt_lint_test.go` — writer 항목의 `substitutes` slice에 `"exemplar_scenes"` 추가.

## Tasks & Acceptance

**Execution:**
- [x] `internal/pipeline/agents/assets_exemplar.go` (NEW) — `parseExemplar` 헬퍼 + 4개 act 헤더 regex.
- [x] `internal/pipeline/agents/assets_exemplar_test.go` (NEW) — happy path / missing act / Notes 섹션 미포함.
- [x] `internal/pipeline/agents/assets.go` — `ExemplarsByAct` 필드 + `exemplarPaths` 상수 + `LoadPromptAssets` 통합 + 4개 act 모두 non-empty 검증.
- [x] `internal/pipeline/agents/assets_test.go` — happy path에서 `ExemplarsByAct` 4개 act 검증.
- [x] `docs/prompts/scenario/03_writing.md` — `## 참고 예시` 섹션 + `{exemplar_scenes}` placeholder.
- [x] `internal/pipeline/agents/writer.go` — `renderWriterActPrompt`에 substitution 추가.
- [x] `internal/pipeline/agents/writer_test.go` — `sampleWriterAssets`에 `ExemplarsByAct` + 템플릿에 placeholder 추가.
- [x] `internal/pipeline/agents/prompt_lint_test.go` — writer substitutes에 `exemplar_scenes` 추가.

**Acceptance Criteria:**
- Given `docs/exemplars/scp-049.exemplar.md` + `scp-2790.exemplar.md` 정상 존재, when `LoadPromptAssets` 호출, then `ExemplarsByAct` map의 `incident`/`mystery`/`revelation`/`unresolved` 4개 키 모두 non-empty이고 각 value는 두 exemplar의 해당 act narration을 포함.
- Given exemplar 파일 1개 missing 또는 act 헤더 누락, when `LoadPromptAssets` 호출, then `domain.ErrValidation` 반환.
- Given writer가 incident act 렌더링, when 프롬프트 string 검사, then 두 exemplar의 Act 1 narration이 포함되고 Act 2/3/4 narration은 미포함 (act-specific inject 검증).
- Given `go test ./...` 실행, then 전체 suite pass.
- Given `prompt_lint_test`의 placeholder 검증, then `{exemplar_scenes}`는 prompt + renderer 양쪽 모두에 등록되어 통과.

## Spec Change Log

## Design Notes

**Per-act inject가 핵심.** 두 exemplar 전체를 매번 inject하면 ~6KB × 4 acts = 24KB 추가 prompt 비용. Per-act 매칭이면 ~1.5KB × 4 acts = 6KB로 동일한 in-context 신호를 4분의 1 비용에 제공. 현재 writer prompt는 act당 한 번 LLM 호출이라 per-act inject가 자연스러움.

**파서는 단순 regex.** exemplar 파일 형식이 우리가 통제하는 markdown이라 정교한 파서 불필요. `^## Act ([1-4]) — ` 헤더로 4개 section 추출, `^## Notes` 등장 시 cutoff. 5번째 act_id 추가나 형식 변경 시점에 파서 update 필요하지만, 그건 별도 cycle.

**에러는 fail-fast.** exemplar 파일 누락이나 형식 mismatch는 server start 시점에 hard fail. runtime fallback 없음 (memory: feedback_no_dead_layers — fallback 코드는 dead layer). 운영자가 docs/exemplars 디렉토리를 신뢰할 수 있는 상태로 유지해야 함.

**Act 4 narration의 outro 처리.** exemplar cleanup 단계에서 이미 댓글 유도 outro가 제거됨 (각 exemplar의 `Notes for fewshot use`에 명시). 파서가 추가로 outro detection 로직을 둘 필요 없음.

**왜 049 + 2790인가.** 049: humanoid 감정형 + "이게 뭐냐" hook + 8분 분량. 2790: humanoid 위협 + "기지 폐쇄 미스터리" hook + 5.5분 분량. 두 편 모두 우리 4-act schema와 잘 매칭되며 hook 패턴이 다름 → LLM이 일반화 가능. 두 편 모두 하다Hada 채널이지만 Phase 1 가설 검증 목적상 single-channel imitation 수용 가능. Phase 2에서 다른 채널 (096/642-KO 등) 추가 검토.

## Verification

**Commands:**
- `go test ./internal/pipeline/agents/...`
- `go test ./...`
- `go build ./...`

**Manual checks:**
- `grep "{exemplar_scenes}" docs/prompts/scenario/03_writing.md` — placeholder 존재 확인.
- 패치 후 SCP-049 dogfood 재실행: 결과 narration이 이전(`docs/scenarios/SCP-049.example.md`) 대비 한국 native channel 톤에 가까워졌는지 (감각 묘사·연결어·문장 리듬) HITL 평가.

## Suggested Review Order

**Design intent (start here)**

- 진입점: writer가 act별로 매칭 exemplar만 룩업·주입하는 단일 경로.
  [`writer.go:420`](../../internal/pipeline/agents/writer.go#L420)

**Parser (new logic)**

- 4-act section 추출 + frontmatter/Notes 제외 + CRLF 정규화 핵심 흐름.
  [`assets_exemplar.go:34`](../../internal/pipeline/agents/assets_exemplar.go#L34)

- Notes 섹션을 "## Notes for fewshot use"로 정확히 anchor (loose match로 인한 narration 잘림 방지).
  [`assets_exemplar.go:22`](../../internal/pipeline/agents/assets_exemplar.go#L22)

- 다중 exemplar를 act별로 concat (블랭크 라인 + horizontal rule 구분자).
  [`assets_exemplar.go:104`](../../internal/pipeline/agents/assets_exemplar.go#L104)

**Integration & wiring**

- `PromptAssets`에 `ExemplarsByAct map[string]string` 필드 + 의도 문서.
  [`assets.go:19`](../../internal/pipeline/agents/assets.go#L19)

- Phase 1 fixed inputs (049 + 2790) 명시 — 채널 imitation 의도와 확장 시점 메모.
  [`assets.go:35`](../../internal/pipeline/agents/assets.go#L35)

- LoadPromptAssets fail-fast 통합 (read fail / parse fail → ErrValidation).
  [`assets.go:83`](../../internal/pipeline/agents/assets.go#L83)

**Prompt template changes**

- Default writer prompt에 `{exemplar_scenes}` placeholder 추가.
  [`03_writing.md:103`](../../docs/prompts/scenario/03_writing.md#L103)

- v2 opt-in template에도 동일 placeholder (편차로 인한 silent drop 방지).
  [`script_writer.tmpl:69`](../../prompts/agents/script_writer.tmpl#L69)

**Exemplar data (LLM에 노출되는 실제 텍스트)**

- 049 — humanoid 감정형, "이게 뭐냐" hook (outro CTA 패턴 제거 완료).
  [`scp-049.exemplar.md:14`](../../docs/exemplars/scp-049.exemplar.md#L14)

- 2790 — humanoid 위협, "기지 폐쇄 미스터리" hook.
  [`scp-2790.exemplar.md:15`](../../docs/exemplars/scp-2790.exemplar.md#L15)

**Tests (peripherals)**

- AC3 per-act 배타성 explicit assertion (각 act prompt가 자기 stub만 포함 + 다른 act stub 미포함).
  [`writer_test.go:213`](../../internal/pipeline/agents/writer_test.go#L213)

- Parser happy path + missing act / empty act / no frontmatter / concat / 다중 missing 에지 커버.
  [`assets_exemplar_test.go:46`](../../internal/pipeline/agents/assets_exemplar_test.go#L46)

