# bmad-quick-dev — frozen descriptor strip "Key visual moments" (grid layout fix)

> 한 세션에서 끝낼 수 있는 범위의 구조 결함 수정.
> bmad-quick-dev 스킬에 이 파일을 통째로 컨텍스트로 넣고 시작.

---

## 문제 진단 (2026-05-05 dogfood)

운영자(Jay)가 SCP-049 run의 production review UI를 확인한 결과, 모든 per-shot 이미지가 **단일 일관 장면이 아니라 3-4 panel comic strip 격자 레이아웃**으로 출력됨. 의도는 "한 beat = 한 단일 장면 이미지". 실제는 한 이미지 안에 SCP-049의 서로 다른 시나리오 3-4개가 panel로 분할되어 그려짐.

**근본 원인 — `BuildFrozenDescriptor`의 `Key visual moments:` 필드**

[internal/pipeline/agents/visual_breakdown_helpers.go:97-105](internal/pipeline/agents/visual_breakdown_helpers.go#L97-L105):
```go
func BuildFrozenDescriptor(v domain.VisualIdentity) string {
    return fmt.Sprintf(
        "Appearance: %s; Distinguishing features: %s; Environment: %s; Key visual moments: %s",
        v.Appearance,
        strings.Join(v.DistinguishingFeatures, ", "),
        v.EnvironmentSetting,
        strings.Join(v.KeyVisualMoments, ", "),  // ← list of distinct narrative scenarios joined into one string
    )
}
```

`v.KeyVisualMoments`는 research output의 `[]string` — SCP의 서로 무관한 visual scenario들 (예: SCP-049의 경우 "performing surgery on a corpse", "being restrained during transport", "writing in journal", "researcher presenting lavender" 4개). 이걸 `, `로 join해서 frozen descriptor에 단일 필드로 박으면, image-edit 모델이 받는 prompt는 다음과 같이 끝남:

```
Key visual moments: SCP-049 standing over a table performing crude surgery,
                    The entity being restrained by a heavy Class III harness...,
                    SCP-049 writing intently in its journal...,
                    A researcher presenting a sprig of lavender...
```

→ 모델이 list를 "이 4개를 모두 그려라"로 해석 → **4-panel grid 출력**.

**증거 (audit.log)**: `output/scp-049-run-1/audit.log`의 `image_generation` 이벤트 prompt에 위 4개 시나리오가 list로 그대로 박혀 있음. canonical은 정상 1-pose인데 (CartoonStylePrompt가 `"no multiple panels"` 명시), per-shot은 그 가드가 없어서 frozen의 list가 dominate.

**중요 컨텍스트**:
- 이 결함은 방금 직전 cycle(SceneStylePrompt 도입, commit b8bc412/c0fa88a)이 만든 게 **아님** — 그 이전부터 존재하는 pre-existing structural defect
- 직전 cycle은 photorealistic → cartoon 전환을 잡았고, grid layout은 frozen 쪽 결함이 따로 만들고 있었음

이번 세션 목표: `BuildFrozenDescriptor`에서 `Key visual moments:` 필드를 제거. 정보 손실 없음 — visual_breakdowner LLM은 같은 정보를 `{scp_visual_reference}` (full marshaled VisualIdentity JSON) placeholder로 이미 받고 있음.

---

## 사전 학습 자료 (읽고 시작)

순서대로 훑기:

1. **메모리 — 반드시 먼저**:
   - `feedback_pipeline_rigor.md` — 검증 외부 사례 모방
   - `feedback_no_dead_layers.md` — Vision/Growth로 미루지 말 것
   - `feedback_meta_principles.md` — 테스트 중심
   - `feedback_commit_scope.md` — out-of-scope 파일 편집 금지

2. **결함의 위치**:
   - [internal/pipeline/agents/visual_breakdown_helpers.go:97-105](internal/pipeline/agents/visual_breakdown_helpers.go#L97-L105) — `BuildFrozenDescriptor`. 4-field format. 이번 cycle에서 3-field로 축소.
   - [internal/pipeline/agents/visual_breakdowner.go:113](internal/pipeline/agents/visual_breakdowner.go#L113) — 호출부 (`frozen := BuildFrozenDescriptor(state.Research.VisualIdentity)`)
   - [internal/pipeline/image_track.go:135-180](internal/pipeline/image_track.go#L135-L180) — `ComposeImagePrompt(style, frozen, visual)` — frozen이 image prompt prefix로 들어감

3. **정보 중복 확인 (정보 손실 없음의 근거)**:
   - [internal/pipeline/agents/visual_breakdowner.go:291-303](internal/pipeline/agents/visual_breakdowner.go#L291-L303) — `renderVisualBreakdownActPrompt`에서 `state.Research.VisualIdentity` 전체를 JSON marshal해서 `{scp_visual_reference}`에 넣음. KeyVisualMoments 포함 전체 필드가 LLM에게 전달됨.
   - [prompts/agents/visual_breakdowner.tmpl](prompts/agents/visual_breakdowner.tmpl) (line 36)의 `## SCP Visual Identity (full reference)` 섹션이 그것. 따라서 frozen descriptor에서 KeyVisualMoments 빠져도 visual_breakdowner LLM 컨텍스트는 유지됨.

4. **Canonical 생성 경로 (frozen 사용처 확인)**:
   - [internal/service/scp_image_service.go:140-260](internal/service/scp_image_service.go#L140-L260) — `Generate(...)`. 캐노니컬 prompt = `CartoonStylePrompt + "; " + frozen`. CartoonStylePrompt는 `"no multiple panels, no action"` 명시 → frozen에 KeyVisualMoments가 있어도 canonical은 single-pose로 잘 나옴 (이미 검증). 그래도 canonical에서도 KeyVisualMoments는 정보 가치 없으니 (캐릭터 시트 용도) 같이 제거하는 게 일관됨.

5. **테스트 의존성 (frozen 4-field 형식을 hardcode한 곳)**:
   - 다음 cycle은 BuildFrozenDescriptor 출력을 3-field로 축소하므로, 그 형식을 가정한 모든 test fixture / assertion을 sync해야 함:
     - `internal/service/scp_image_service_test.go:320` — `"; Key visual moments: ..."` 문자열 비교
     - `internal/service/scp_image_service_test.go:340` — `Contains(prompt, "Key visual moments:")` 부정 assertion (현재 의도 의문)
     - `internal/domain/review_test.go:61` — 4-field frozen 문자열 fixture
     - `internal/domain/visual_breakdown_test.go:111` — 4-field frozen 문자열 fixture
     - `internal/domain/scenario_test.go:29` — `KeyVisualMoments: []string{...}` 사용 (도메인 type 그대로 유지하므로 이건 영향 없음, 확인용)
     - `internal/pipeline/agents/validator_test.go:223` + `internal/pipeline/agents/validator_test.go:57` — KeyVisualMoments 도메인 사용
     - `internal/pipeline/agents/reviewer_test.go:131,142` — 4-field frozen 문자열 fixture
     - `internal/pipeline/agents/structurer_test.go:273` + `internal/pipeline/agents/corpus_test.go:161` — KeyVisualMoments 도메인 사용
     - `internal/pipeline/phase_a_integration_test.go:816,880,901` — KeyVisualMoments + 4-field frozen 문자열
     - `internal/pipeline/image_track_test.go:314` — 4-field frozen 문자열 fixture

6. **Story 5.4 byte-stability 계약 (오해 방지)**:
   - `frozen_descriptor`의 byte-stability 계약은 **"한 run 내에서 frozen은 변하지 않는다"** (image_track.go의 `ComposeImagePrompt` 주석 + spec-5-4-frozen-descriptor-propagation-per-shot-image-generation.md 참조)
   - **"BuildFrozenDescriptor 함수의 출력 형식은 영원히 동결"이 아님**
   - 따라서 BuildFrozenDescriptor를 변경해도 byte-stability 위배 없음. 새 run부터 새 형식 적용, 기존 run의 scenario.json은 디스크에 저장된 옛 frozen 그대로 유지 (regenerate Phase B하면 옛 frozen으로 grid 재생성됨 — 운영자가 그 run을 새로 시작해야 새 frozen 적용)

---

## 핵심 결정 (구현 전 알고 시작)

### D1. `BuildFrozenDescriptor`에서 `Key visual moments` 필드 **완전 제거**

3-field로 축소:
```go
func BuildFrozenDescriptor(v domain.VisualIdentity) string {
    return fmt.Sprintf(
        "Appearance: %s; Distinguishing features: %s; Environment: %s",
        v.Appearance,
        strings.Join(v.DistinguishingFeatures, ", "),
        v.EnvironmentSetting,
    )
}
```

**왜 surgical strip(option 2)이 아니라 source 변경(option 1)인가**:
- Surgical strip은 `ComposeImagePrompt`에서 frozen 받은 후 `"Key visual moments:"` segment만 substring-strip하는 방식. byte-stability 계약을 더 보수적으로 해석.
- 하지만 (a) frozen은 visual_breakdowner LLM에게도 prompt placeholder `{frozen_descriptor}`로 전달되며 LLM 입장에서도 KeyVisualMoments가 거기 있을 필요 없음 (`{scp_visual_reference}`에 같은 정보 있음). (b) source 변경이 가독성/일관성 우월. (c) byte-stability 계약은 "함수 출력 영원 동결"이 아님 (메모리에서 명시).

### D2. `domain.VisualIdentity.KeyVisualMoments` 필드는 **유지**

- Research output 도메인 타입에는 그대로 존재 (writer가 act_synopsis 작성 시 참조할 수 있는 신호).
- visual_breakdowner LLM이 받는 `{scp_visual_reference}` JSON에도 그대로 노출.
- 이번 cycle은 frozen descriptor의 _포맷_만 바꾸지, _도메인 모델_을 바꾸지 않음. JSON 스키마 / DB 컬럼 / API 영향 없음.

### D3. Canonical generation도 같이 영향 받음 — OK

[scp_image_service.go:Generate](internal/service/scp_image_service.go) 의 canonical prompt = `CartoonStylePrompt + "; " + frozen`. frozen이 3-field로 축소되면 canonical prompt도 짧아짐. 하지만 canonical은 `CartoonStylePrompt`의 `"single character / no multiple panels / front-facing neutral pose"` 가드가 dominate하므로 출력 품질은 유지 (이미 dogfood 검증된 single-pose 결과).

운영자가 `scp_image_service_test.go:340`의 `Contains(prompt, "Key visual moments:")` 부정 assertion 의도를 확인하고 (그 테스트가 사실 KeyVisualMoments _제거_를 검증하던 것일 수도 있음 — 코드 읽고 정리), 새 BuildFrozenDescriptor 형식과 일관되게 sync.

### D4. Existing run scenario.json은 _migrate하지 않음_

기존 `output/{run-id}/scenario.json`에는 옛 4-field frozen이 저장되어 있음. 이 cycle은 그걸 다시 쓰지 않음:
- 새 run부터 새 frozen 적용 → grid 문제 해결됨
- 기존 run을 Phase B regenerate해도 옛 frozen 그대로 사용 → 여전히 grid (수용 가능 — 운영자는 새 run을 시작하면 됨)
- Migration script는 별 cycle (필요 시)

### D5. 이번 세션은 _이미지 모델 prompt에 grid 가드 추가_는 안 함

SceneStylePrompt에 `"single coherent scene, no comic strip, no multi-panel layout"` 같은 부정 가드를 추가할 수도 있지만 — 이번 cycle은 _root cause_(frozen의 list)만 제거. 가드 추가는 surface 처치이며, root cause가 사라지면 불필요. 만약 dogfood에서 root-cause 제거 후에도 grid가 남으면 다음 cycle에서 가드 추가 검토.

---

## 구현 범위 (체크리스트)

### 1. BuildFrozenDescriptor 축소
- [ ] [internal/pipeline/agents/visual_breakdown_helpers.go:97-105](internal/pipeline/agents/visual_breakdown_helpers.go#L97-L105) — `Key visual moments` 필드 제거. 3-field 포맷 (`Appearance: ...; Distinguishing features: ...; Environment: ...`). 함수 시그니처 / 호출부 변경 없음 (입력/반환 타입 동일).
- [ ] 짧은 주석 추가 — "왜 KeyVisualMoments를 빼는지" 한 줄 (image-edit grid 트리거 회피, info 중복은 `{scp_visual_reference}`)

### 2. Canonical service test 정합화
- [ ] [internal/service/scp_image_service_test.go:320,340](internal/service/scp_image_service_test.go#L320) — 옛 4-field 가정 sync. line 340의 `Contains(prompt, "Key visual moments:")` 부정 assertion이 의도하는 바 확인하고 (새 포맷에서는 자동 만족됨) 필요 시 단순화.

### 3. Domain / breakdown / validator / reviewer / structurer / corpus 테스트 sync
- [ ] [internal/domain/review_test.go:61](internal/domain/review_test.go#L61) — 4-field frozen 문자열 fixture → 3-field
- [ ] [internal/domain/visual_breakdown_test.go:111](internal/domain/visual_breakdown_test.go#L111) — 동일
- [ ] [internal/pipeline/agents/reviewer_test.go:131,142](internal/pipeline/agents/reviewer_test.go#L131) — 동일
- [ ] [internal/pipeline/agents/validator_test.go](internal/pipeline/agents/validator_test.go) — KeyVisualMoments 도메인 사용은 그대로 유지 (도메인 type 변경 없음). frozen 문자열 비교가 있으면 sync.
- [ ] [internal/pipeline/agents/structurer_test.go](internal/pipeline/agents/structurer_test.go) + [internal/pipeline/agents/corpus_test.go](internal/pipeline/agents/corpus_test.go) — KeyVisualMoments 도메인 사용 그대로 (frozen 문자열 없음, 영향 적음, 확인용)

### 4. Pipeline integration 테스트 sync
- [ ] [internal/pipeline/phase_a_integration_test.go:816,880,901](internal/pipeline/phase_a_integration_test.go#L816) — 4-field frozen 문자열 fixture → 3-field. KeyVisualMoments 도메인 사용은 그대로 유지.

### 5. Image track 테스트 sync
- [ ] [internal/pipeline/image_track_test.go:314](internal/pipeline/image_track_test.go#L314) — `frozen := "Appearance: ...; Distinguishing features: ...; Environment: ...; Key visual moments: ..."` → 3-field

### 6. BuildFrozenDescriptor unit test 신규 또는 갱신
- [ ] BuildFrozenDescriptor 자체에 대한 직접 테스트가 있다면 (검색해서 확인) 3-field 출력 검증으로 갱신. 없다면 간단한 unit test 1개 추가:
  - 모든 필드 채워진 input → expected 3-field 출력
  - 빈 KeyVisualMoments → 빈 필드가 어디에도 안 보임 (이미 자연 만족)

### 7. dogfood verification (코드 머지 전 마지막 게이트)
- [ ] 새 SCP run 1개 시작 (SCP-049 또는 다른 SCP). Phase A → Phase B 끝까지 진행.
- [ ] production review UI에서 임의 scene 3개 골라서 (a) 단일 일관 장면 (multi-panel 아님) (b) cartoon 스타일 (직전 cycle의 SceneStylePrompt와 결합) 둘 다 확인.
- [ ] 만약 grid가 여전히 보이면 → frozen 외에 다른 prompt segment(예: visual_descriptor에서 visual_breakdowner가 list-style 출력하는 패턴)에서 트리거되는지 audit.log로 추적. 그 경우 다음 cycle 검토.

---

## 세션 외 범위 (지금 건드리지 말 것)

- **`domain.VisualIdentity.KeyVisualMoments` 필드 / 스키마 / DB 변경** — 도메인 type 그대로. 이번은 frozen descriptor 포맷만.
- **Existing run scenario.json migration** — 별 cycle. 운영자는 새 run으로 옮겨감.
- **SceneStylePrompt에 negative grid guard 추가** — root cause 제거가 우선. dogfood 후 필요 시 별 cycle.
- **Visual_breakdowner output에서 list-style descriptor 회피 prompt rule** — 이번은 frozen만. visual_descriptor 자체는 별 cycle (필요 판명되면).
- **Writer retry feedback loop** — 별 next-session planning 파일 (`next-session-writer-retry-feedback-loop.md`)에 분리되어 있음.

---

## 커밋 분할 (feedback_commit_scope.md 준수)

권장 분할:
1. `feat(visual_breakdown): drop "Key visual moments" from frozen descriptor` — `BuildFrozenDescriptor` 축소 + BuildFrozenDescriptor unit test (있거나 신규)
2. `test: sync frozen descriptor fixtures with 3-field format` — 위에 나열한 모든 테스트 fixture를 3-field로 sync (한 commit으로 묶음 — 모두 동일한 mechanical 변경)

각 커밋은 build + 테스트 그린이어야 함. 1번이 단독으로는 테스트 깨질 수 있으므로 2번과 같은 PR/세션 안에서 머지 (커밋은 분리, 테스트는 2번 이후 그린).

대안: 한 커밋으로 묶어도 OK (mechanical refactor + 테스트 sync는 분리해도 가치 작음). bmad-quick-dev 결정사항.

---

## Acceptance signals

- [ ] `go build ./...` + `go test ./...` 그린
- [ ] BuildFrozenDescriptor 결과에 `"Key visual moments"` substring 없음 (assert)
- [ ] 새 dogfood run의 audit.log image_generation prompt에 `"Key visual moments:"` 없음
- [ ] 새 dogfood run의 production review UI에서 임의 scene 3개가 단일 일관 장면 (grid 아님)
- [ ] 캐노니컬 (single character reference sheet)은 여전히 정상 single-pose 출력
- [ ] visual_breakdowner LLM은 KeyVisualMoments 정보를 `{scp_visual_reference}`로 받아서 컨텍스트 손실 없음 (trace로 확인 가능)

---

## 참고 — 미해결로 남기는 항목 (deferred)

이 세션 후에도 남는 이슈, 메모해두기:

1. **Existing run의 grid 잔존** — 옛 frozen이 scenario.json에 저장된 채로 남음. 운영자가 그 run을 regenerate해도 grid 그대로. Migration 또는 명시적 frozen-rebuild API는 별 cycle.
2. **visual_descriptor 자체가 list-style일 가능성** — `visual_breakdowner`가 act 컨텍스트 보고 한 beat의 visual_descriptor에 여러 동작을 콤마로 나열할 수 있음. 만약 dogfood에서 frozen 제거 후에도 grid가 남으면 그쪽이 트리거. 별 cycle 검토 후보.
3. **KeyVisualMoments 정보의 활용처 재검토** — 도메인에 남기지만, writer/critic/reviewer 어디서도 적극 활용 안 하면 dead field일 수 있음. 별 cycle (필요 시) 데드 코드 청소.
