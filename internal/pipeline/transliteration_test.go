package pipeline_test

import (
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/pipeline"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

func TestTransliterate_SCPIDReadingDigitByDigit(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	got := pipeline.Transliterate("SCP-049")
	testutil.AssertEqual(t, got, "에스씨피-공사구")
}

func TestTransliterate_SCPIDCaseInsensitive(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	testutil.AssertEqual(t, pipeline.Transliterate("scp-049"), "에스씨피-공사구")
	testutil.AssertEqual(t, pipeline.Transliterate("SCP-173"), "에스씨피-일칠삼")
	testutil.AssertEqual(t, pipeline.Transliterate("SCP-001"), "에스씨피-공공일")
}

func TestTransliterate_CommonEnglishWords(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	cases := []struct {
		input string
		want  string
	}{
		{"doctor", "닥터"},
		{"entity", "엔터티"},
		{"containment", "격리"},
		{"foundation", "재단"},
		{"personnel", "인원"},
	}
	for _, tc := range cases {
		got := pipeline.Transliterate(tc.input)
		testutil.AssertEqual(t, got, tc.want)
	}
}

func TestTransliterate_CurrencyTokens(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	cases := []struct {
		input string
		want  string
	}{
		{"$100", "백 달러"},
		{"100 USD", "백 달러"},
		{"5000 KRW", "오천 원"},
		{"$1000", "천 달러"},
		{"250 USD", "이백오십 달러"},
		// Regression: "$N CCC" must not leak a trailing currency code to TTS.
		{"$100 USD", "백 달러"},
		{"$5000 KRW", "오천 원"},
		// Regression: lowercase currency codes are recognised.
		{"100 usd", "백 달러"},
	}
	for _, tc := range cases {
		got := pipeline.Transliterate(tc.input)
		testutil.AssertEqual(t, got, tc.want)
	}
}

func TestTransliterate_PreservesEmbeddedDigitIdentifiers(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Rule 5 must only convert boundary-delimited integers. Digits embedded
	// inside identifiers like "scene_01" or "v2" must stay intact so the TTS
	// provider does not receive mangled filenames / version strings.
	cases := []struct {
		input string
		want  string
	}{
		// Digits adjacent to an ASCII word char (letter, digit, or underscore)
		// do NOT fire \b and therefore are not mangled.
		{"scene_01", "scene_01"},
		{"v2", "v2"},
	}
	for _, tc := range cases {
		got := pipeline.Transliterate(tc.input)
		testutil.AssertEqual(t, got, tc.want)
	}
}

func TestTransliterate_DateTokens(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	cases := []struct {
		input string
		want  string
	}{
		// Year-only: converted as a generic number
		{"1998", "천구백구십팔"},
		// Year with 년 suffix: number rule converts digits, 년 stays
		{"1998년", "천구백구십팔년"},
		// Full ISO date
		{"1998-11-04", "천구백구십팔년 십일월 사일"},
		// Another full date
		{"2024-01-15", "이천이십사년 일월 십오일"},
	}
	for _, tc := range cases {
		got := pipeline.Transliterate(tc.input)
		testutil.AssertEqual(t, got, tc.want)
	}
}

func TestTransliterate_MixedSentence(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	input := "SCP-049는 doctor로 알려진 개체입니다. 1998년에 처음 격리되었으며, $100의 비용이 들었습니다."
	got := pipeline.Transliterate(input)

	// Verify specific tokens are converted
	want := "에스씨피-공사구는 닥터로 알려진 개체입니다. 천구백구십팔년에 처음 격리되었으며, 백 달러의 비용이 들었습니다."
	testutil.AssertEqual(t, got, want)
}

func TestTransliterate_Idempotent(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	cases := []string{
		"SCP-049",
		"doctor 100",
		"1998-11-04",
		"$100 USD",
		"SCP-049는 doctor로 알려진 entity입니다.",
		"순수한 한국어 텍스트는 변환되지 않습니다.",
	}
	for _, input := range cases {
		once := pipeline.Transliterate(input)
		twice := pipeline.Transliterate(once)
		testutil.AssertEqual(t, twice, once)
	}
}

func TestTransliterate_KoreanTextUntouched(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	input := "순수한 한국어 텍스트는 변환되지 않습니다."
	got := pipeline.Transliterate(input)
	testutil.AssertEqual(t, got, input)
}

func TestTransliterate_UnknownEnglishWordsPassthrough(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Words not in the dictionary are left untouched (chosen policy: passthrough)
	got := pipeline.Transliterate("xlkjqwerty")
	testutil.AssertEqual(t, got, "xlkjqwerty")
}

func TestTransliterate_NumberConversions(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	cases := []struct {
		input string
		want  string
	}{
		{"0", "영"},
		{"1", "일"},
		{"10", "십"},
		{"11", "십일"},
		{"20", "이십"},
		{"100", "백"},
		{"1000", "천"},
		{"10000", "만"},
		{"12345", "만이천삼백사십오"},
	}
	for _, tc := range cases {
		got := pipeline.Transliterate(tc.input)
		testutil.AssertEqual(t, got, tc.want)
	}
}
