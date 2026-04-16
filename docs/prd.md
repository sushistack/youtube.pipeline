# PRD

## 0. 요약

> SCP Foundation 유튜브 콘텐츠 자동 제작 파이프라인 (CLI 기반 로컬 프로그램 제작)
> SCP ID 입력 → 시나리오 → 병렬[이미지 → TTS → 자막(필요할지 의문)] → 프로그래밍적 영상 생성(FFmpeg, 어느정도까지 가능할지 미지수)

- **Module**: `github.com/sushistack/youtube.pipeline`
- **Go 1.25.7** / Cobra CLI / SQLite / Docker (?)
- **핵심 철학**: "80% 자동화, 20% 수동 마무리"
- Human-In-the-Loop 필수

---

## 1. specs.

- 언어: Go latest 버전
- DB: SQLite
- CLI: Cobra latest 버전
- config: viper latest 버전 

## 2. 외부 연동

- TTS: Dashcope(qwen3-tts-flash)
- LLM: deepseek, gemini, qwen
- image: dashcope, qwen-image-2.0 (1664, 928)


## 3. 수집된 데이터

- scp_data_path: "/mnt/data/raw"
- 문서들: docs/*


## 4. 예상 페이지
- 대시보드
- 파이프라인 화면
    1. 시나리오 (대본)
    2. 캐릭터 생성 (기본적으로 duckduckgo 검색 10개, 참고 이미지로 선택 후, qwen image-edit 으로 고품질 캐릭터 이미지 생성)
    3. 1,2 의 산출물을 기반으로 씬에 들어갈 이미지, 나레이션, TTS 를 생성하여 노출
    4. 3번의 자료들을 인간이 한개씩 확인 필요

## 5. 프롬프트

- docs/scenario
