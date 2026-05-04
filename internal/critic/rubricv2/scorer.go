// Package rubricv2 implements the 10-criterion SCP-Explained-aligned
// critic rubric described in spec section 5 of
// _bmad-output/planning-artifacts/next-session-enhance-prompts.md.
//
// Score is a pure function — no LLM call, no I/O. It is additive: the
// existing critic agent (internal/pipeline/agents/critic.go) is not
// touched in this cycle. A future cycle can wire this scorer in behind
// a feature flag once calibrated.
//
// v2 (D4): every criterion consumes domain.NarrationScript.Acts directly.
// "First scene" / "last scene" map to first/last beat of the script in
// flat order; per-scene checks become per-beat checks; act-position
// arithmetic uses NarrationBeatView.Index over total beat count. The v1
// LegacyScenes() bridge died with this scorer's migration in D4.
//
// Coverage by criterion:
//   1. Hook ≤15s            — deterministic (rune-count proxy + opening lint
//                              against the first beat's text)
//   2. Information drip     — heuristic floor; sets RequiresLLMReview
//   3. Concrete incident    — heuristic floor; sets RequiresLLMReview
//   4. Twist position       — deterministic (act-position arithmetic over
//                              flat beat order)
//   5. Unresolved outro     — deterministic (last beat's punctuation/keyword)
//   6. Sentence rhythm      — deterministic (KR sentence length avg)
//   7. Sensory language     — deterministic (style guide hit count)
//   8. POV consistency      — deterministic (per-beat marker scan)
//   9. SCP fidelity         — heuristic floor; sets RequiresLLMReview
//  10. Visual reusability   — deterministic when shots provided
package rubricv2

import (
	"fmt"
	"strings"

	contractv2 "github.com/sushistack/youtube.pipeline/internal/contract/v2"
	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/style"
)

// hookRunesPer15s is the rune-count proxy for "first beat ≤15 seconds of
// narration" when the script lacks explicit per-beat durations.
// Calibrated against Korean TTS at ~3.5 char/sec → 15s ≈ 52 runes;
// rounded up to 60 to allow short pauses.
const hookRunesPer15s = 60

// twistLowerPct / twistUpperPct define the spec-section-5 acceptance
// window for criterion 4 ("twist lands at 70-85% of total duration").
const (
	twistLowerPct = 0.70
	twistUpperPct = 0.85
)

// visualReuseFloor is the spec section 5 criterion-10 pass threshold
// (≥40% of shots reuse a background/asset).
const visualReuseFloor = 0.40

// failureQuoteThreshold mirrors spec section 5 — failure quotes are
// required for any criterion scored below this bar.
const failureQuoteThreshold = 8

// Input is the shape Score consumes.
type Input struct {
	Script   *domain.NarrationScript
	Style    *style.StyleGuide
	Research *contractv2.ResearchOutput
	Shots    []contractv2.Shot
}

// Score returns a CriticReport per spec section 4.5. Nil Script returns
// a zero report with Passed=false; the caller is responsible for
// rejecting nil inputs upstream.
func Score(in Input) contractv2.CriticReport {
	rep := contractv2.CriticReport{
		RubricScores: map[string]int{},
		Failures:     []contractv2.Failure{},
	}
	if in.Script == nil {
		rep.RevisionPriority = "no script provided"
		return rep
	}

	// Each criterion scorer returns (score, optional failure).
	for _, c := range []struct {
		key string
		fn  func(Input) (int, *contractv2.Failure)
	}{
		{"hook_under_15s", scoreHook},
		{"information_drip", scoreInformationDrip},
		{"concrete_incident", scoreConcreteIncident},
		{"twist_position", scoreTwistPosition},
		{"unresolved_outro", scoreUnresolvedOutro},
		{"sentence_rhythm", scoreSentenceRhythm},
		{"sensory_language", scoreSensoryLanguage},
		{"pov_consistency", scorePOVConsistency},
		{"scp_fidelity", scoreSCPFidelity},
		{"visual_reusability", scoreVisualReusability},
	} {
		s, f := c.fn(in)
		if s < 0 {
			s = 0
		}
		if s > contractv2.MaxScorePerCriterion {
			s = contractv2.MaxScorePerCriterion
		}
		rep.RubricScores[c.key] = s
		if s < failureQuoteThreshold {
			if f == nil {
				f = &contractv2.Failure{Criterion: c.key, Score: s}
			} else {
				f.Criterion = c.key
				f.Score = s
			}
			rep.Failures = append(rep.Failures, *f)
		}
		rep.OverallScore += s
	}

	rep.Passed = rep.OverallScore >= contractv2.PassingScore
	rep.RevisionPriority = pickRevisionPriority(rep.Failures)
	return rep
}

// --- Criterion 1: hook ≤15s ---------------------------------------------

func scoreHook(in Input) (int, *contractv2.Failure) {
	beats := in.Script.FlatBeats()
	if len(beats) == 0 {
		return 0, &contractv2.Failure{FailureQuote: "no beats"}
	}
	first := beats[0]
	runes := []rune(first.Text)
	score := contractv2.MaxScorePerCriterion
	notes := []string{}

	if len(runes) > hookRunesPer15s {
		score -= 4
		notes = append(notes, fmt.Sprintf("first beat is %d runes (cap %d)", len(runes), hookRunesPer15s))
	}
	if in.Style != nil {
		for _, opening := range in.Style.Narration.ForbiddenOpenings {
			if opening != "" && strings.HasPrefix(strings.TrimSpace(first.Text), opening) {
				score -= 4
				notes = append(notes, fmt.Sprintf("first beat begins with forbidden opening %q", opening))
				break
			}
		}
	}
	if score >= contractv2.MaxScorePerCriterion {
		return score, nil
	}
	return score, &contractv2.Failure{
		FailureQuote:   strings.TrimSpace(first.Text),
		Recommendation: "강렬한 이미지로 15초 이내 hook을 확보하고 채널 인사말을 제거하세요. " + strings.Join(notes, "; "),
	}
}

// --- Criterion 2: information drip --------------------------------------

func scoreInformationDrip(in Input) (int, *contractv2.Failure) {
	keywords := []string{"격리", "절차", "프로토콜", "수용", "수감", "수직"}
	hitBeats := 0
	for _, beat := range in.Script.FlatBeats() {
		for _, kw := range keywords {
			if strings.Contains(beat.Text, kw) {
				hitBeats++
				break
			}
		}
	}
	if hitBeats >= 3 {
		return 8, &contractv2.Failure{
			RequiresLLMReview: true,
			Recommendation:    "정보가 ≥3 beat에 분산되어 있으나 LLM 단계에서 미세 검증 필요.",
		}
	}
	return 5, &contractv2.Failure{
		RequiresLLMReview: true,
		FailureQuote:      fmt.Sprintf("containment-info keyword density across %d beats (need ≥3)", hitBeats),
		Recommendation:    "격리/절차 정보를 한 번에 쏟지 말고 3개 이상 beat에 흩뿌리세요.",
	}
}

// --- Criterion 3: concrete incident -------------------------------------

func scoreConcreteIncident(in Input) (int, *contractv2.Failure) {
	sensoryVerbs := []string{"흘러내렸", "찢어졌", "갈라졌", "터졌", "무너졌", "스며들었", "쏟아졌"}
	hitBeats := 0
	for _, beat := range in.Script.FlatBeats() {
		for _, v := range sensoryVerbs {
			if strings.Contains(beat.Text, v) {
				hitBeats++
				break
			}
		}
	}
	if hitBeats >= 1 {
		return 8, &contractv2.Failure{
			RequiresLLMReview: true,
			Recommendation:    "구체적 incident가 감지됨. 인물/시간/장소 명시 여부는 LLM 단계에서 추가 검증.",
		}
	}
	return 4, &contractv2.Failure{
		RequiresLLMReview: true,
		FailureQuote:      "no sensory verbs detected across beats",
		Recommendation:    "한 사건을 시간·장소·결과까지 드라마타이즈하세요. '흘러내렸다', '찢어졌다' 같은 감각 동사 권장.",
	}
}

// --- Criterion 4: twist position ----------------------------------------

func scoreTwistPosition(in Input) (int, *contractv2.Failure) {
	beats := in.Script.FlatBeats()
	total := len(beats)
	if total == 0 {
		return 0, &contractv2.Failure{FailureQuote: "no beats"}
	}
	revealIdx := -1
	for i, beat := range beats {
		if beat.ActID == domain.ActRevelation {
			revealIdx = i
			break
		}
	}
	if revealIdx < 0 {
		return 4, &contractv2.Failure{
			FailureQuote:   "no act tagged act_id=revelation",
			Recommendation: "twist를 담는 act을 act_id=revelation으로 태그하세요.",
		}
	}
	pos := float64(revealIdx) / float64(total)
	switch {
	case pos >= twistLowerPct && pos <= twistUpperPct:
		return contractv2.MaxScorePerCriterion, nil
	case pos >= twistLowerPct-0.05 && pos <= twistUpperPct+0.05:
		return 8, nil
	default:
		return 4, &contractv2.Failure{
			FailureQuote:   fmt.Sprintf("revelation starts at beat index %d/%d (≈%.2f)", revealIdx, total, pos),
			Recommendation: "twist는 전체 길이의 70–85% 지점에 두세요.",
		}
	}
}

// --- Criterion 5: unresolved outro --------------------------------------

func scoreUnresolvedOutro(in Input) (int, *contractv2.Failure) {
	beats := in.Script.FlatBeats()
	if len(beats) == 0 {
		return 0, &contractv2.Failure{FailureQuote: "no beats"}
	}
	last := strings.TrimSpace(beats[len(beats)-1].Text)
	if last == "" {
		return 0, &contractv2.Failure{FailureQuote: "outro empty"}
	}
	cliffhangerHints := []string{"?", "…", "...", "그것은 아직", "아무도 모른다", "무엇이", "어디로"}
	for _, h := range cliffhangerHints {
		if strings.Contains(last, h) {
			return contractv2.MaxScorePerCriterion, nil
		}
	}
	return 4, &contractv2.Failure{
		FailureQuote:   last,
		Recommendation: "마지막 문장은 질문, 줄임표, 또는 미해결 단서로 다음 영상에 연결되도록 하세요.",
	}
}

// --- Criterion 6: sentence rhythm ---------------------------------------

func scoreSentenceRhythm(in Input) (int, *contractv2.Failure) {
	if in.Style == nil {
		return 7, &contractv2.Failure{
			RequiresLLMReview: true,
			Recommendation:    "style guide 미주입 — sentence rhythm 검증 보류.",
		}
	}
	tenseAvgCap := in.Style.Narration.AvgSentenceLengthTense
	if tenseAvgCap <= 0 {
		tenseAvgCap = 18
	}
	totalLen := 0
	totalSentences := 0
	for _, beat := range in.Script.FlatBeats() {
		mood := beat.Anchor.Mood
		if mood == "" {
			mood = beat.ActMood
		}
		if !isTenseMood(mood) {
			continue
		}
		sentences := splitKoreanSentences(beat.Text)
		for _, snt := range sentences {
			runes := []rune(strings.TrimSpace(snt))
			if len(runes) == 0 {
				continue
			}
			totalLen += len(runes)
			totalSentences++
		}
	}
	if totalSentences == 0 {
		// No tense beats — the script may be all calm; don't penalize.
		return 8, nil
	}
	avg := totalLen / totalSentences
	switch {
	case avg <= tenseAvgCap:
		return contractv2.MaxScorePerCriterion, nil
	case avg <= tenseAvgCap+5:
		return 7, &contractv2.Failure{
			FailureQuote:   fmt.Sprintf("tense-beat avg sentence length=%d (cap %d)", avg, tenseAvgCap),
			Recommendation: "긴장 구간 평균 문장 길이를 줄이세요.",
		}
	default:
		return 4, &contractv2.Failure{
			FailureQuote:   fmt.Sprintf("tense-beat avg sentence length=%d (cap %d)", avg, tenseAvgCap),
			Recommendation: "긴장 구간 평균 문장 길이를 18자 이하로 단축하세요.",
		}
	}
}

// --- Criterion 7: sensory language --------------------------------------

func scoreSensoryLanguage(in Input) (int, *contractv2.Failure) {
	if in.Style == nil {
		return 7, &contractv2.Failure{
			RequiresLLMReview: true,
			Recommendation:    "style guide 미주입 — abstract emotion 검증 보류.",
		}
	}
	limit := in.Style.Narration.MaxAbstractEmotionWordsPerScript
	if limit <= 0 {
		limit = 1
	}
	allText := scriptText(in.Script)
	hits := in.Style.CountAbstractEmotionHits(allText)
	switch {
	case hits <= limit:
		return contractv2.MaxScorePerCriterion, nil
	case hits <= limit+1:
		return 7, &contractv2.Failure{
			FailureQuote:   fmt.Sprintf("abstract emotion hits=%d (cap %d)", hits, limit),
			Recommendation: "추상 형용사를 감각적 묘사로 교체하세요.",
		}
	default:
		return 3, &contractv2.Failure{
			FailureQuote:   fmt.Sprintf("abstract emotion hits=%d (cap %d)", hits, limit),
			Recommendation: "끔찍/무서운 같은 추상어 사용을 1회 이하로 제한하고 감각 묘사로 대체하세요.",
		}
	}
}

// --- Criterion 8: POV consistency ---------------------------------------

func scorePOVConsistency(in Input) (int, *contractv2.Failure) {
	secondPerson := []string{"당신", "너는 ", "너의 ", "여러분"}
	thirdPerson := []string{"재단은", "그것은", "조사관은", "요원은", "그들은"}
	violations := 0
	firstViolation := ""
	for _, beat := range in.Script.FlatBeats() {
		hasSecond := containsAny(beat.Text, secondPerson)
		hasThird := containsAny(beat.Text, thirdPerson)
		if hasSecond && hasThird {
			violations++
			if firstViolation == "" {
				firstViolation = beat.Text
			}
		}
	}
	score := contractv2.MaxScorePerCriterion - 2*violations
	if score >= contractv2.MaxScorePerCriterion {
		return contractv2.MaxScorePerCriterion, nil
	}
	return score, &contractv2.Failure{
		FailureQuote:   firstViolation,
		Recommendation: "한 beat 안에서 2인칭(당신/여러분)과 3인칭(재단은/그것은) 시점을 섞지 마세요.",
	}
}

// --- Criterion 9: SCP fidelity ------------------------------------------

func scoreSCPFidelity(in Input) (int, *contractv2.Failure) {
	if in.Research == nil {
		return 6, &contractv2.Failure{
			RequiresLLMReview: true,
			Recommendation:    "research input 미주입 — SCP fidelity는 LLM 검증으로 위임.",
		}
	}
	allText := scriptText(in.Script)
	hits := 0
	for _, prop := range in.Research.AnomalousProperties {
		prop = strings.TrimSpace(prop)
		if prop == "" {
			continue
		}
		if strings.Contains(allText, prop) {
			hits++
		}
	}
	props := len(in.Research.AnomalousProperties)
	if props == 0 {
		return 7, &contractv2.Failure{
			RequiresLLMReview: true,
			Recommendation:    "research에 anomalous_properties가 없어 자동 검증 불가.",
		}
	}
	cover := float64(hits) / float64(props)
	switch {
	case cover >= 0.8:
		return contractv2.MaxScorePerCriterion, nil
	case cover >= 0.5:
		return 8, &contractv2.Failure{
			RequiresLLMReview: true,
			Recommendation:    fmt.Sprintf("anomalous property coverage %.0f%% — LLM에서 누락 항목 검증 필요.", cover*100),
		}
	default:
		return 4, &contractv2.Failure{
			RequiresLLMReview: true,
			FailureQuote:      fmt.Sprintf("only %d/%d anomalous properties referenced", hits, props),
			Recommendation:    "research의 핵심 anomalous_properties를 narration에 모두 등장시키세요.",
		}
	}
}

// --- Criterion 10: visual reusability -----------------------------------

func scoreVisualReusability(in Input) (int, *contractv2.Failure) {
	if len(in.Shots) == 0 {
		return 5, &contractv2.Failure{
			RequiresLLMReview: true,
			Recommendation:    "shot plan 미주입 — visual reusability 자동 측정 보류.",
		}
	}
	bgCount := map[string]int{}
	for _, sh := range in.Shots {
		bg := strings.TrimSpace(sh.Background)
		if bg == "" {
			bg = "(empty)"
		}
		bgCount[bg]++
	}
	total := len(in.Shots)
	reused := 0
	for _, n := range bgCount {
		if n > 1 {
			reused += n
		}
	}
	pct := float64(reused) / float64(total)
	switch {
	case pct >= visualReuseFloor:
		return contractv2.MaxScorePerCriterion, nil
	case pct >= visualReuseFloor-0.1:
		return 7, &contractv2.Failure{
			FailureQuote:   fmt.Sprintf("background reuse=%.0f%% (target ≥%.0f%%)", pct*100, visualReuseFloor*100),
			Recommendation: "background asset 재사용을 늘려 제작 비용을 줄이세요.",
		}
	default:
		return 3, &contractv2.Failure{
			FailureQuote:   fmt.Sprintf("background reuse=%.0f%% (target ≥%.0f%%)", pct*100, visualReuseFloor*100),
			Recommendation: "shot 40% 이상이 background 또는 asset을 재사용하도록 재구성하세요.",
		}
	}
}

// --- helpers ------------------------------------------------------------

// scriptText returns one string with every act's monologue separated by
// newlines. Used by criteria that operate on whole-script text rather than
// per-beat (sensory language, SCP fidelity).
func scriptText(script *domain.NarrationScript) string {
	if script == nil {
		return ""
	}
	parts := make([]string, 0, len(script.Acts))
	for _, act := range script.Acts {
		parts = append(parts, act.Monologue)
	}
	return strings.Join(parts, "\n")
}

func isTenseMood(mood string) bool {
	mood = strings.ToLower(strings.TrimSpace(mood))
	switch mood {
	case "tense", "ominous", "horror", "dread", "panic", "긴장", "공포":
		return true
	default:
		return false
	}
}

// splitKoreanSentences splits on Korean and Latin sentence terminators.
// Empty fragments are kept by the caller's loop and skipped after trim.
func splitKoreanSentences(s string) []string {
	terminators := []string{".", "!", "?", "…"}
	out := []string{s}
	for _, t := range terminators {
		next := []string{}
		for _, frag := range out {
			next = append(next, strings.Split(frag, t)...)
		}
		out = next
	}
	return out
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// pickRevisionPriority returns the criterion key with the lowest score
// that has a recommendation, or an empty string when there are no
// failures. The critic agent uses this to focus the rewrite.
func pickRevisionPriority(failures []contractv2.Failure) string {
	if len(failures) == 0 {
		return ""
	}
	worst := failures[0]
	for _, f := range failures[1:] {
		if f.Score < worst.Score {
			worst = f
		}
	}
	if worst.Recommendation != "" {
		return worst.Criterion + ": " + worst.Recommendation
	}
	return worst.Criterion
}
