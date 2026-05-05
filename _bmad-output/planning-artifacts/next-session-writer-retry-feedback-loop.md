# bmad-quick-dev — writer monologue retry feedback loop

> 한 세션에서 끝낼 수 있는 범위의 안정성 강화 작업.
> bmad-quick-dev 스킬에 이 파일을 통째로 컨텍스트로 넣고 시작.

---

## 문제 진단 (2026-05-05 dogfood)

운영자(Jay)가 SCP-049 run을 돌려본 결과 writer_monologue stage의 retry 메커니즘이 정보 없이 같은 prompt를 그대로 재호출하는 구조적 결함이 관측됨:

**Case A — under-floor 연속 실패 후 운으로 통과**:
- attempt 0: revelation monologue = 932 runes (floor=1248) → fail
- attempt 1: 936 runes → fail (직전과 거의 동일)
- attempt 2: 1517 runes → success (band 안)
- 같은 prompt에서 932 → 936 → 1517로 ~3× 변동성. attempt 1이 attempt 0와 거의 동일했던 건 LLM이 직전 실패 사실을 모르기 때문.

**Case B — over-cap retry exhaustion**:
- 직전 cycle에서 unresolved monologue가 1286 runes (cap=1120) → 2 retry 모두 같은 패턴으로 실패 → 전체 stage 실패. cap을 widening해서 일단 막았지만 (commit 169bc00) band-aid.

**근본 원인**: [internal/pipeline/agents/writer.go:230](internal/pipeline/agents/writer.go#L230)에서 prompt가 retry 루프 **밖**에서 한 번 렌더링되고, [line 271-275](internal/pipeline/agents/writer.go#L271-L275)에서 매 attempt마다 동일한 prompt가 재호출됨. LLM은 직전 attempt가 floor 미만이었는지 cap 초과였는지 모름.

이번 세션 목표: writer_monologue retry 시 직전 attempt의 actual rune count + 목표 band를 새 `{retry_feedback}` placeholder로 prompt에 inject하는 in-stage feedback loop 구현.

---

## 사전 학습 자료 (읽고 시작)

순서대로 훑기:

1. **메모리 — 반드시 먼저**:
   - `feedback_pipeline_rigor.md` — 검증 외부 사례 모방
   - `feedback_no_dead_layers.md` — Vision/Growth로 미루지 말 것
   - `feedback_meta_principles.md` — 테스트 중심
   - `feedback_commit_scope.md` — out-of-scope 파일 편집 금지

2. **현재 writer retry 구조**:
   - [internal/pipeline/agents/writer.go:140-175](internal/pipeline/agents/writer.go#L140-L175) — `runWriterAct()` 진입부, qualityFeedback 캡처
   - [internal/pipeline/agents/writer.go:225-330](internal/pipeline/agents/writer.go#L225-L330) — `runWriterActMonologue()` 본체. **prompt가 line 230에서 한 번 렌더되고 retry 루프(line 250)는 같은 prompt를 재사용**
   - [internal/pipeline/agents/writer.go:331-372](internal/pipeline/agents/writer.go#L331-L372) — `validateWriterMonologueResponse()` — rune length cap/floor 검증
   - [internal/pipeline/agents/writer.go:725-790](internal/pipeline/agents/writer.go#L725-L790) — `renderWriterActPrompt()` — `{quality_feedback}` placeholder 치환부

3. **Prompt placeholder 등록부**:
   - [prompts/agents/script_writer.tmpl](prompts/agents/script_writer.tmpl) — 전체 (line 12 근처에 rune length 안내, line 95-105에 self-check)
   - [docs/prompts/scenario/03_writing.md](docs/prompts/scenario/03_writing.md) — `.tmpl`과 byte-identical (lint test가 강제)
   - [internal/pipeline/agents/prompt_lint_test.go:32-41](internal/pipeline/agents/prompt_lint_test.go#L32-L41) — writer prompt의 substitutes 화이트리스트
   - [internal/pipeline/agents/prompt_lint_test.go:140-160](internal/pipeline/agents/prompt_lint_test.go#L140-L160) — `.tmpl`과 `.md` placeholder 집합 동일성 검증

4. **Rune cap/floor 정의**:
   - [internal/domain/scenario.go:165-220](internal/domain/scenario.go#L165-L220) — `ActMonologueRuneCap`, `ActMonologueRuneFloor` 맵 + 철학 코멘트 (`incident` widening + `unresolved` widening + `revelation` floor lowering 사례 참고)

5. **runWithRetry 메커니즘**:
   - [internal/pipeline/agents/writer.go:240-250](internal/pipeline/agents/writer.go#L240-L250)에서 `retryOpts` 구성 + `runWithRetry` 호출. 클로저 시그니처는 `func(attempt int) (result, retryReason, error)`. 클로저는 closure-capture로 outer scope 변수에 접근 가능.

---

## 핵심 결정 (구현 전 알고 시작)

### D1. `{retry_feedback}` 는 기존 `{quality_feedback}`과 **분리된 신규 placeholder**

기존 `{quality_feedback}`은 [writer.go:150](internal/pipeline/agents/writer.go#L150)에서 `state.PriorCriticFeedback`로 채움 — 이건 **cross-stage** 신호 (critic이 reject하고 writer로 되돌아갔을 때의 비평). 새로 추가할 `{retry_feedback}`은 **within-stage** 신호 (직전 retry attempt의 length 미스). 의미가 다르므로 placeholder도 분리.

두 신호는 prompt에서 공존 가능:
- Cross-stage retry (critic-driven) + within-stage retry (length-driven)이 동시에 발생하면 두 피드백이 따로 보임
- 둘 다 빈 문자열이 default → backwards-safe (기존 동작 유지)

### D2. Prompt 렌더링을 retry 루프 **안**으로 이동

현재 [writer.go:230](internal/pipeline/agents/writer.go#L230)에서 prompt가 루프 밖에서 한 번 렌더링됨. retry feedback을 inject하려면 매 attempt마다 re-render 필요.

```go
var retryFeedback string  // outer scope; closure-captured
out, err := runWithRetry(ctx, opts, func(attempt int) (...) {
    prompt, err := renderWriterActPrompt(
        state, prompts, terms, spec, priorTail,
        qualityFeedback,    // existing cross-stage signal
        retryFeedback,      // NEW within-stage signal
    )
    if err != nil { ... }
    // ... 기존 LLM 호출 + decode + validate ...
    if err := validateWriterMonologueResponse(spec, decoded); err != nil {
        // length 문제면 다음 attempt용 feedback 캡처
        n := utf8.RuneCountInString(decoded.Monologue)
        capV, capOK := domain.ActMonologueRuneCap[spec.Act.ID]
        floor, floorOK := domain.ActMonologueRuneFloor[spec.Act.ID]
        if capOK && floorOK && (n > capV || n < floor) {
            retryFeedback = formatMonologueLengthRetryFeedback(n, floor, capV)
        }
        // ... 기존 에러 핸들링 ...
    }
})
```

성능 영향: prompt rendering은 `strings.NewReplacer` 하나 — 무시할 비용. 기존 prompt_chars 로깅 (`prompt_chars=17826`)은 그대로 유효.

### D3. Length 분기는 error-string parsing 대신 **rune count 직접 비교**로

`validateWriterMonologueResponse`는 length 외에도 empty mood, empty key_points, sentence terminal count 등 다른 사유로도 fail함. retry feedback은 length 미스에만 의미 있으므로, validate 실패 시 직접 `utf8.RuneCountInString(decoded.Monologue)`로 length를 확인 → cap/floor와 비교 → 분기. 에러 문자열 파싱은 fragile하니 피함.

length 외 사유로 fail한 attempt에서는 `retryFeedback`을 갱신하지 않음 (기존 값 유지 또는 빈 문자열). 결과: length-unrelated 실패 retry는 기존과 동일 동작.

### D4. Feedback 메시지 — 구조화된 안내 (under-floor / over-cap 대칭)

```
PREVIOUS ATTEMPT FAILED: monologue was {actual} runes — BELOW the floor of {floor}.
Target the middle of the band [{floor}, {cap}] = ~{middle} runes.
Add factual anchors, sensory detail, and narrator-aside commentary to expand.
Do NOT pad with filler.
```

Over-cap 버전:
```
PREVIOUS ATTEMPT FAILED: monologue was {actual} runes — OVER the cap of {cap}.
Target the middle of the band [{floor}, {cap}] = ~{middle} runes.
Tighten by removing redundant phrases and shortening sentences.
```

**핵심 신호**:
- 실제 rune 수 (LLM이 자기 출력의 길이를 모르는 약점 보완)
- 방향성 ("BELOW the floor" / "OVER the cap" 명시)
- 목표값 (middle of band를 explicit하게 명시 — 기존 prompt의 "write toward the middle" 지시 강화)
- 액션 가이드 (expand vs tighten)

### D5. 적용 범위는 **writer_monologue stage 1만**

- writer **stage 2 (segmenter)**: schema/sentence-boundary 사유로 retry하지만 length band 검증 아님 (다른 종류의 신호). 별 cycle.
- visual_breakdowner: 자체 retry 있지만 1:1 invariant 검증 (commit 95a1e7f). 다른 종류. 별 cycle.
- post-writer critic 등: cross-stage 피드백은 이미 `{quality_feedback}`에 있음.

### D6. 1:N retry 누적 정책 — **마지막 실패 attempt만 반영**

여러 retry가 누적되면 prompt가 부풀어 오를 수 있음. 단순화 결정:
- `retryFeedback`은 마지막 실패 attempt의 정보만 담음 (덮어쓰기, 누적 X)
- 이유: 직전 attempt와 그 이전 attempt가 비슷한 미스를 했다면 두 정보의 가치는 거의 같음. 누적은 prompt cost만 늘림.
- 만약 향후 누적이 가치 있다고 판명되면 별 cycle.

---

## 구현 범위 (체크리스트)

### 1. Prompt template — `{retry_feedback}` placeholder 추가
- [ ] [prompts/agents/script_writer.tmpl](prompts/agents/script_writer.tmpl) — `{quality_feedback}` 위치 근처에 `{retry_feedback}` 섹션 추가. 빈 문자열일 때 어색하지 않도록 wrapper 형식 (e.g., 별도 `## Retry Feedback (in-stage)` 섹션, 빈 값이면 빈 줄로 대체)
- [ ] [docs/prompts/scenario/03_writing.md](docs/prompts/scenario/03_writing.md) — byte-identical sync (`diff` empty 유지; lint test가 강제)

### 2. Renderer — placeholder 치환 추가
- [ ] [internal/pipeline/agents/writer.go:725-790](internal/pipeline/agents/writer.go#L725-L790) — `renderWriterActPrompt` 시그니처에 `retryFeedback string` 추가, `strings.NewReplacer`에 `"{retry_feedback}", retryFeedback` 매핑 추가

### 3. Retry 루프 refactor
- [ ] [internal/pipeline/agents/writer.go:225-330](internal/pipeline/agents/writer.go#L225-L330) — `runWriterActMonologue` 본체:
  - prompt 렌더링을 retry 클로저 안으로 이동
  - `var retryFeedback string` outer scope에 추가
  - validate 실패 + length 미스 분기 시 `retryFeedback = formatMonologueLengthRetryFeedback(n, floor, capV)` 갱신
- [ ] 신규 함수 `formatMonologueLengthRetryFeedback(actual, floor, cap int) string` 추가 (writer.go 또는 신규 작은 파일). under-floor/over-cap/in-band 분기. 빈 band (둘 중 하나가 0)인 경우는 빈 문자열 반환.

### 4. Lint test 동기화
- [ ] [internal/pipeline/agents/prompt_lint_test.go:32-41](internal/pipeline/agents/prompt_lint_test.go#L32-L41) — `writer` substitutes 리스트에 `"retry_feedback"` 추가. `.tmpl`과 `.md`의 placeholder 집합 동일성은 자동 검증됨.

### 5. 테스트
- [ ] `internal/pipeline/agents/writer_test.go` — 기존 `sampleWriterAssets()` writer fixture에 `{retry_feedback}` placeholder 추가 (현재 그 placeholder가 없어서 lint test가 fail할 수 있음). `renderWriterActPrompt` 시그니처 변경에 따라 기존 호출 사이트 전수 업데이트 (테스트 + 본체 호출).
- [ ] `formatMonologueLengthRetryFeedback` unit tests:
  - under-floor: 메시지에 `actual`, `floor`, `BELOW`, `expand` 포함
  - over-cap: 메시지에 `actual`, `cap`, `OVER`, `tighten` 포함
  - in-band (이론상 호출 안 됨이지만 안전): 빈 문자열
- [ ] Integration test: actQueueTextGenerator로 [under-floor 응답, in-band 응답] 시퀀스를 큐잉. 첫 attempt는 fail, 두 번째 attempt에서는 generator에 들어온 prompt에 `PREVIOUS ATTEMPT FAILED` 문구가 포함되었는지 확인 (e.g., generator를 capturing wrapper로 감싸서 prompt 기록).

### 6. dogfood verification (코드 머지 전 마지막 게이트)
- [ ] dev server에서 새 run 한 개 실행. revelation/unresolved act에서 retry가 발생하면 (또는 강제로 trigger 가능하면) 두 번째 attempt의 prompt에 feedback이 inject됐는지 trace로 확인.
- [ ] retry 분산이 줄어드는지 관찰 (이번 세션의 932→936→1517 같은 ~3× variance가 사라지는지). 통계적 증명은 어려우니 single-run 정성 관찰로 ok.

---

## 세션 외 범위 (지금 건드리지 말 것)

- **writer stage 2 (segmenter) retry feedback** — 다른 종류의 검증 (sentence boundary, 8-10 beats). 별 cycle.
- **visual_breakdowner retry feedback** — 1:1 invariant 검증, 다른 신호 형태. 별 cycle.
- **rune cap/floor 추가 튜닝** — 이번에 already 한 commit 169bc00 (unresolved cap), 4525cb5 (revelation floor). 더 건드리지 말 것; feedback loop가 대부분의 케이스를 흡수해야 함.
- **retry budget 변경** — 현재 `writerPerStageRetryBudget = 2`. 같이 변경하면 신호 분리 안 됨. 별 cycle에서 재평가.
- **cross-stage critic feedback (`{quality_feedback}`) 형식 변경** — 이번 cycle은 새 placeholder 추가만. 기존 슬롯은 손대지 말 것.

---

## 커밋 분할 (feedback_commit_scope.md 준수)

권장 분할:
1. `feat(writer): add retry_feedback placeholder + formatter` — formatter 함수 + writer.go renderer 시그니처 + tmpl/.md placeholder + lint test + formatter unit tests
2. `feat(writer): inject in-stage length feedback into monologue retry` — runWriterActMonologue 클로저 refactor + integration test

각 커밋은 build + 테스트 그린이어야 함.

---

## Acceptance signals

- [ ] `go build ./...` + `go test ./...` 그린
- [ ] `formatMonologueLengthRetryFeedback` unit tests pass (under-floor / over-cap / in-band)
- [ ] Integration test: 첫 attempt under-floor 후 두 번째 attempt prompt에 `PREVIOUS ATTEMPT FAILED ... BELOW the floor of` 포함됨을 확인
- [ ] `diff prompts/agents/script_writer.tmpl docs/prompts/scenario/03_writing.md` empty
- [ ] `TestPromptPlaceholders_AreFullyCovered/writer` pass (substitutes 리스트에 `retry_feedback` 추가됨)
- [ ] dogfood single-run: writer_monologue retry가 발생했을 때 trace에 직전 attempt의 actual rune count가 다음 attempt의 prompt 안에 보임

---

## 참고 — 미해결로 남기는 항목 (deferred)

이 세션 후에도 남는 이슈, 메모해두기:

1. **Segmenter / visual_breakdowner / 기타 stage retry feedback 부재** — 같은 패턴 재현되면 별 cycle.
2. **Per-attempt prompt 길이 증가** — 매 retry마다 feedback 텍스트가 prompt에 추가됨 → tokens_in 증가 (이번 세션 dogfood 기준 ~17,800 chars). 비용 영향 미미하지만 모니터링.
3. **Retry feedback이 LLM에게 무시되는 경우** — feedback이 prompt에 들어갔는데도 LLM이 같은 길이로 응답하면 추가 대응 필요. 현 cycle 결과 보고 판단.
4. **Rune count 외의 length 강제 메커니즘** — provider-side max_tokens 튜닝, structured output schema의 length 강제 등 더 강력한 메커니즘. retry feedback이 충분치 않으면 검토.
