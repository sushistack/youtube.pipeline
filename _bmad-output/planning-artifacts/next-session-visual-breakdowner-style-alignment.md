# bmad-quick-dev — visual_breakdowner 스타일 누락 + narration alignment 보강

> 한 세션에서 끝낼 수 있는 범위의 버그 수정 + 프롬프트 강화 작업.
> bmad-quick-dev 스킬에 이 파일을 통째로 컨텍스트로 넣고 시작.

---

## 문제 진단 (2026-05-05 dogfood)

운영자(Jay)가 SCP-049 run을 돌려본 결과 두 가지 결함이 관측됨:

1. **카툰 스타일 미적용** — 의도는 "kid-friendly cartoon, Starcraft-inspired stylized art"인데 실제 산출 이미지가 photorealistic으로 나옴. canonical 이미지(Cast 단계)에는 스타일이 적용되지만, **Phase B per-shot 이미지에는 스타일 prefix가 전혀 propagate되지 않음**.

2. **Narration ↔ Visual alignment 약함** — beat의 monologue slice가 "수술대에 누운 환자"를 묘사해도 visual_descriptor가 그 구체적 행위/주체를 강제하지 않음. Stage 3.5 prompt는 "factual consistency with each beat's monologue slice"라는 한 줄만 있고, **"narration의 주어/동사를 반드시 시각화" 같은 hard rule이 없음**.

이번 세션 목표: 위 두 가지를 최소 침습적으로 수정. 1:1 beat-shot invariant(D2 load-bearing)는 **건드리지 않는다**.

---

## 사전 학습 자료 (읽고 시작)

순서대로 훑기:

1. **메모리 — 반드시 먼저**:
   - `feedback_pipeline_rigor.md` — 검증 외부 사례 모방
   - `feedback_no_dead_layers.md` — Vision/Growth로 미루지 말 것
   - `feedback_meta_principles.md` — 테스트 중심
   - `feedback_commit_scope.md` — out-of-scope 파일 편집 금지
   - `feedback_config_not_env.md` — 토글은 config.yaml로

2. **현재 visual_breakdowner**:
   - [prompts/agents/visual_breakdowner.tmpl](prompts/agents/visual_breakdowner.tmpl) — 전체 (76줄)
   - [internal/pipeline/agents/visual_breakdowner.go](internal/pipeline/agents/visual_breakdowner.go#L295-L351) — `renderVisualBreakdownActPrompt()` (placeholder 치환부) + `validateVisualBreakdownActResponse()` (1:1 invariant)
   - [docs/prompts/scenario/03_5_visual_breakdown.md](docs/prompts/scenario/03_5_visual_breakdown.md) — `.tmpl`과 동일 내용. **두 파일을 동기화해서 수정**

3. **이미지 prompt 합성 (Phase B)**:
   - [internal/pipeline/image_track.go:135-160](internal/pipeline/image_track.go#L135-L160) — `ComposeImagePrompt(frozen, visual)` 시그니처 + 주석 (Frozen Descriptor는 byte-stable, 정규화 금지)
   - [internal/pipeline/image_track.go:323](internal/pipeline/image_track.go#L323) — 호출부
   - [internal/pipeline/image_track.go:408](internal/pipeline/image_track.go#L408) — `cfg.Images.Edit(...)` 호출

4. **스타일 config**:
   - [internal/domain/config.go:97-102](internal/domain/config.go#L97-L102) — `CartoonStylePrompt` 정의
   - [internal/domain/config.go:225](internal/domain/config.go#L225) — default 값 ("Single-character reference sheet illustration: ... kid-friendly cartoon, Starcraft-inspired stylized art, clean vector lines, vibrant colors.")
   - [cmd/pipeline/serve.go:806](cmd/pipeline/serve.go#L806) + [cmd/pipeline/resume.go:91](cmd/pipeline/resume.go#L91) — 현재 canonical 생성에만 inject되는 위치

5. **canonical reuse 흐름** (스타일이 "carry"된다고 가정되는 부분):
   - [internal/service/scp_image_service.go:140-164](internal/service/scp_image_service.go#L140-L164) — `stylePrompt + "; " + frozen_descriptor`로 canonical 생성
   - [internal/pipeline/image_track.go:246-264](internal/pipeline/image_track.go#L246-L264) — canonical hit이면 ref image로 사용

---

## 핵심 결정 (구현 전 알고 시작)

### D1. 스타일은 이미 config로 들어와 있음 — 재사용한다

`config.CartoonStylePrompt`는 이미 정의되어 있고 default 값도 있음. **새 config 키를 만들지 말 것.** 같은 string을 visual_breakdowner와 Phase B 양쪽에서 재사용.

단, canonical용으로 쓰던 문구 "Single-character reference sheet illustration: full body, front-facing neutral standing pose..."는 캐릭터 시트용이라 per-shot에는 부적합. **두 개의 사용처가 같은 string을 공유하면 한 쪽이 다른 쪽을 망가뜨림.**

따라서 **config 키를 분리**:
- `CartoonStylePrompt` (기존, canonical 캐릭터 시트용) — **건드리지 말 것**
- `SceneStylePrompt` (신규, per-shot용) — Style: `kid-friendly cartoon, Starcraft-inspired stylized art, clean vector lines, vibrant colors, soft cinematic lighting, expressive character poses` 정도. "single character / reference sheet / front-facing" 류 표현은 빼야 함

config.yaml에 두 키가 모두 노출되도록 default 등록은 [internal/config/loader.go:53](internal/config/loader.go#L53) 패턴 그대로.

### D2. 1:1 beat-shot invariant는 절대 깨지 않는다

[validateVisualBreakdownActResponse()](internal/pipeline/agents/visual_breakdowner.go#L347-L351)의 `len(shots) == len(act.Beats)` + per-field byte-equality는 D2 (commit 95a1e7f)의 load-bearing 계약. 이를 변경하면 downstream image_track / TTS 슬라이싱이 전부 깨짐.

**유저가 "여러 씬에 한 이미지 재사용"을 언급했지만, 이 세션에서는 다루지 않는다.**
- segmenter(writer Stage 2)가 act당 8-10 beat을 만들고 → 각 beat이 1 shot이 되는 현재 구조는 의도된 설계
- "reuse"는 SCP 캐릭터 일관성 측면에서 canonical image library로 이미 부분 해결됨
- 임의 scene reuse는 별도 cycle에서 (필요하다면) 다룰 것 — 지금 끼워 넣으면 invariant 깨짐

이 결정은 [_bmad-output/planning-artifacts/next-session-monologue-mode-decoupling.md:130-138](_bmad-output/planning-artifacts/next-session-monologue-mode-decoupling.md#L130-L138)의 A3 resolution과도 일치.

### D3. 스타일은 `ComposeImagePrompt` 안이 아니라 **prompt 합성 단계의 새 layer**로

`ComposeImagePrompt(frozen, visual)`의 주석은 "frozen은 byte-stable, 정규화 금지"라고 명시. 스타일을 frozen 안에 섞으면 Story 5.4의 frozen descriptor 불변성 계약이 깨짐.

따라서:
```go
// 시그니처 변경
func ComposeImagePrompt(style, frozen, visual string) string
```
- 합성 순서: `style + "; " + frozen + "; " + visual` (또는 visual이 frozen prefix 포함이면 `style + "; " + visual`)
- style이 빈 문자열이면 기존 동작 유지 (backwards-safe — 이번 cycle에선 항상 non-empty이지만 fallback 보존)
- 호출자는 [image_track.go:323](internal/pipeline/image_track.go#L323) 한 곳뿐. cfg에서 style을 받아서 전달

### D4. visual_breakdowner.tmpl에는 스타일 **+** alignment rule을 추가

prompt에 두 가지 추가:

(a) **새 섹션 `## Style Directive`** — Frozen Visual Identity 위 또는 아래
```
## Style Directive

All shots are rendered in: {scene_style_prompt}

Every `visual_descriptor` you write MUST be compatible with this style.
Do NOT include words that conflict with it (e.g. "photorealistic",
"cinematic", "live action", "documentary photo", "hyperrealistic").
Use vocabulary that fits a stylized cartoon (e.g. "expressive pose",
"vibrant", "stylized", "clean line art").
```

(b) **Rules 섹션에 alignment 규칙 추가** — 기존 64-75줄 rules에 한두 줄 삽입:
```
- every `visual_descriptor` MUST visually realize the **literal subject
  and action** from the beat's monologue slice (start_offset/end_offset).
  If the slice mentions "수술대 위에 누운 사람", the descriptor MUST
  depict a person on an operating table — not a metaphor, not an
  ambient mood shot. Identity preservation does NOT override this:
  the focal action of the slice always wins.
```

`.tmpl`과 `docs/prompts/scenario/03_5_visual_breakdown.md` 두 파일 모두 동일하게 수정.

### D5. visual_breakdowner.go의 placeholder 추가

[renderVisualBreakdownActPrompt()](internal/pipeline/agents/visual_breakdowner.go#L295-L303)의 `strings.NewReplacer` 호출에 `{scene_style_prompt}` → 실제 값 매핑 추가.

`NewVisualBreakdowner()` 시그니처 / `TextAgentConfig` 또는 `PromptAssets`에 `SceneStylePrompt string` 필드 추가. 호출부 [cmd/pipeline/serve.go](cmd/pipeline/serve.go), [cmd/pipeline/resume.go](cmd/pipeline/resume.go)에서 `cfg.SceneStylePrompt`를 주입.

---

## 구현 범위 (체크리스트)

### 1. config 키 추가
- [ ] [internal/domain/config.go](internal/domain/config.go) — `SceneStylePrompt string` 필드 + 기본값 (D1 설명 참고)
- [ ] [internal/config/loader.go](internal/config/loader.go) — `SetDefault("scene_style_prompt", ...)` 등록
- [ ] [config.yaml](config.yaml) — 운영자 가시성을 위해 키와 기본값을 명시 (주석 포함)

### 2. visual_breakdowner prompt 수정
- [ ] [prompts/agents/visual_breakdowner.tmpl](prompts/agents/visual_breakdowner.tmpl) — D4(a) 스타일 섹션 + D4(b) alignment rule 추가, `{scene_style_prompt}` placeholder 사용
- [ ] [docs/prompts/scenario/03_5_visual_breakdown.md](docs/prompts/scenario/03_5_visual_breakdown.md) — 동일하게 동기화

### 3. visual_breakdowner agent 수정
- [ ] [internal/pipeline/agents/visual_breakdowner.go](internal/pipeline/agents/visual_breakdowner.go) — `renderVisualBreakdownActPrompt`에 `{scene_style_prompt}` replacer 매핑, `NewVisualBreakdowner` signature에 style 주입 경로 추가
- [ ] 1:1 invariant 검증 로직(`validateVisualBreakdownActResponse`)은 **수정하지 말 것** — alignment는 prompt에서만 강제 (post-hoc 검증은 LLM judge가 필요해서 이 cycle 범위 밖)

### 4. Phase B image prompt 합성에 스타일 propagate
- [ ] [internal/pipeline/image_track.go:141](internal/pipeline/image_track.go#L141) — `ComposeImagePrompt(style, frozen, visual)`로 시그니처 확장 (D3)
- [ ] 같은 파일의 호출부 (line 323 부근) — cfg에서 style 받아 전달
- [ ] `ImageTrackConfig` (또는 그에 상응하는 struct)에 `SceneStylePrompt string` 추가

### 5. 호출 wiring
- [ ] [cmd/pipeline/serve.go](cmd/pipeline/serve.go) — `cfg.SceneStylePrompt`를 visual_breakdowner와 Phase B builder 양쪽에 주입
- [ ] [cmd/pipeline/resume.go](cmd/pipeline/resume.go) — 동일

### 6. 테스트
- [ ] `internal/pipeline/agents/visual_breakdowner_test.go` — `{scene_style_prompt}` 치환 + 빈 값일 때 fallback 표 (treat-as-empty)
- [ ] `internal/pipeline/image_track_test.go` — `ComposeImagePrompt` 새 시그니처:
  - style + frozen + visual 모두 있을 때
  - style만 빈 문자열
  - visual이 frozen prefix를 이미 포함하는 경우
  - frozen이 "; "로 끝나는 경우 (sep 중복 가드)
- [ ] golden fixture가 있다면 (있을 가능성 높음) 새 prompt 형식에 맞춰 갱신 — `feedback_pipeline_rigor.md`의 외부 사례 모방 원칙

### 7. dogfood verification (코드 머지 전 마지막 게이트)
- [ ] dev server에서 새 run 한 개 실행. canonical → Phase B를 끝까지 돌려서 SCP-049 비슷한 케이스를 받아본다
- [ ] 산출 이미지가 (a) 카툰 스타일이고 (b) 해당 beat narration의 주어/동사가 시각적으로 보이는지 육안 확인
- [ ] 만약 여전히 photorealistic이면 — `cfg.Images.Edit`이 reference image의 스타일을 우선시할 가능성. 이 경우 canonical 의존성을 줄이고 prompt 자체를 강하게 만드는 추가 사이클 필요. **이번 세션에서는 prompt 강화까지만 적용하고 결과만 보고**.

---

## 세션 외 범위 (지금 건드리지 말 것)

- **Polisher v2 (D7)** — 별 cycle. 이 작업과 무관.
- **1:N beat-shot mapping** — D2 invariant 보존. canonical-style scene reuse는 별 cycle.
- **HITL gate에서 image diff 검토** — 별 cycle.
- **visual_breakdowner 실제 alignment 검증 (LLM judge)** — post-hoc 검증은 별도 critic stage 도입이 필요하므로 이 세션 밖.
- **canonical image library 자체** (commit f23ea72 산출물) — reference로만 활용.

---

## 커밋 분할 (feedback_commit_scope.md 준수)

권장 분할:
1. `feat(config): add SceneStylePrompt for per-shot cartoon style` — config struct + loader default + config.yaml
2. `feat(visual_breakdowner): inject scene style + narration alignment rule` — prompt .tmpl/.md + agent.go replacer + tests
3. `feat(image_track): propagate scene style into per-shot prompt` — ComposeImagePrompt signature + call site + tests
4. `feat(serve,resume): wire SceneStylePrompt into visual_breakdowner and Phase B`

각 커밋은 빌드 + 테스트 그린이어야 함. mixed 파일 (예: serve.go가 #2와 #4 양쪽에 걸치면) 어느 한쪽으로 몰거나 uncommitted로 남길 것.

---

## Acceptance signals

- [ ] `go build ./...` + `go test ./...` 그린
- [ ] 새 run 1회 dogfood — Phase A canonical → Phase B per-shot 모두 cartoon 스타일로 일관
- [ ] 임의의 beat 하나 골라서 monologue slice와 산출 이미지를 대조 → 주어/동사가 시각화되어 있음
- [ ] `validateVisualBreakdownActResponse`의 1:1 invariant 테스트 모두 그대로 통과 (변경 없음)
- [ ] config.yaml에 `scene_style_prompt` 키가 노출되어 운영자가 토글 가능

---

## 참고 — 미해결로 남기는 항목 (deferred)

이 세션 후에도 남는 이슈, 메모해두기:

1. **post-hoc alignment 검증 부재** — prompt 강화로 alignment를 _유도_하지만 _보장_하지 않음. 필요하면 별 cycle에서 visual critic agent (LLM judge) 도입. 이 세션 산출물의 dogfood 결과를 보고 판단.
2. **이미지 reuse across beats** — canonical library가 캐릭터 단위로만 처리. 임의 장면 단위 reuse는 미지원. 비용 issue가 커지면 다룰 가치 있음.
3. **canonical 없을 때 fallback** — Phase B에서 canonical miss이고 DDG URL도 없으면 어떻게 되는지 별도 검토 필요 (이번 세션 prompt 강화로 일부 완화되긴 함).
