package pipeline

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Korean digit names used for digit-by-digit reading (e.g. SCP IDs).
var digitKorean = [10]string{"공", "일", "이", "삼", "사", "오", "육", "칠", "팔", "구"}

// Sino-Korean digit strings for positional number representation.
// Index 0 is unused because a coefficient of 1 is suppressed for 십/백/천.
var sinoKorean = [10]string{"", "일", "이", "삼", "사", "오", "육", "칠", "팔", "구"}

// englishWordTable is the table-driven dictionary for English-to-Korean conversion.
// Add new entries here; no control-flow edits are needed.
var englishWordTable = map[string]string{
	"doctor":      "닥터",
	"dr":          "닥터",
	"entity":      "엔터티",
	"containment": "격리",
	"foundation":  "재단",
	"personnel":   "인원",
	"agent":       "요원",
	"object":      "오브젝트",
	"class":       "등급",
	"safe":        "안전",
	"euclid":      "유클리드",
	"keter":       "케테르",
	"anomalous":   "변칙적인",
	"report":      "보고서",
	"specimen":    "표본",
	"incident":    "사건",
	"researcher":  "연구원",
	"security":    "보안",
	"mobile":      "기동",
	"task":        "임무",
	"force":       "부대",
	"procedure":   "절차",
	"breach":      "격리 위반",
	"protocol":    "프로토콜",
	"item":        "개체",
	"staff":       "직원",
}

// currencyTable maps currency symbols/codes to Korean unit names.
var currencyTable = map[string]string{
	"USD": "달러",
	"KRW": "원",
	"EUR": "유로",
	"JPY": "엔",
	"CNY": "위안",
}

var (
	reSCPID    = regexp.MustCompile(`(?i)SCP-(\d+)`)
	reFullDate = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`)
	// reDollar accepts an optional trailing currency code so that
	// "$100 USD" is consumed in one pass and the code does not leak.
	reDollar       = regexp.MustCompile(`(?i)\$(\d+)(?:\s+(USD|KRW|EUR|JPY|CNY))?`)
	reCurrencyCode = regexp.MustCompile(`(?i)(\d+)\s+(USD|KRW|EUR|JPY|CNY)`)
	// reNumber matches only word-boundary-delimited integer runs so embedded
	// digits in identifiers like "scene_01" or "v2" are not mangled. Korean
	// characters, spaces, and most punctuation are non-word chars so \b fires
	// between them and an adjacent digit.
	reNumber  = regexp.MustCompile(`\b\d+\b`)
	reEnglish = regexp.MustCompile(`[A-Za-z]+`)
)

// Transliterate converts numbers and English terms in Korean narration text
// to Korean orthography for natural TTS pronunciation. The function is
// deterministic and idempotent: Transliterate(Transliterate(x)) == Transliterate(x)
// for any input that contains only Korean orthography after the first pass.
//
// Rule application order (earlier rules run first and prevent later rules from
// re-matching the same token):
//
//  1. SCP-ID pattern (letter-by-letter + digit-by-digit)
//  2. ISO full-date  YYYY-MM-DD
//  3. Dollar-sign currency  $NNN
//  4. Currency-code suffix  NNN USD / NNN KRW / …
//  5. Generic integers
//  6. English word dictionary (unrecognised words are left untouched)
func Transliterate(text string) string {
	// Rule 1: SCP-ID — must run before generic number rule.
	text = reSCPID.ReplaceAllStringFunc(text, func(s string) string {
		m := reSCPID.FindStringSubmatch(s)
		if len(m) < 2 {
			return s
		}
		var b strings.Builder
		b.WriteString("에스씨피-")
		for _, ch := range m[1] {
			d := ch - '0'
			if d < 10 {
				b.WriteString(digitKorean[d])
			}
		}
		return b.String()
	})

	// Rule 2: ISO full date — must run before generic number rule.
	text = reFullDate.ReplaceAllStringFunc(text, func(s string) string {
		m := reFullDate.FindStringSubmatch(s)
		if len(m) < 4 {
			return s
		}
		year, _ := strconv.ParseInt(m[1], 10, 64)
		month, _ := strconv.ParseInt(m[2], 10, 64)
		day, _ := strconv.ParseInt(m[3], 10, 64)
		return fmt.Sprintf("%s년 %s월 %s일", koreanNumber(year), koreanNumber(month), koreanNumber(day))
	})

	// Rule 3: Dollar-sign currency with optional trailing code — must run
	// before generic number rule. A trailing code (e.g. "USD" in "$100 USD")
	// overrides the default "달러" reading.
	text = reDollar.ReplaceAllStringFunc(text, func(s string) string {
		m := reDollar.FindStringSubmatch(s)
		if len(m) < 2 {
			return s
		}
		n, _ := strconv.ParseInt(m[1], 10, 64)
		unit := "달러"
		if len(m) >= 3 && m[2] != "" {
			if u, ok := currencyTable[strings.ToUpper(m[2])]; ok {
				unit = u
			}
		}
		return koreanNumber(n) + " " + unit
	})

	// Rule 4: Currency-code suffix — must run before generic number rule.
	text = reCurrencyCode.ReplaceAllStringFunc(text, func(s string) string {
		m := reCurrencyCode.FindStringSubmatch(s)
		if len(m) < 3 {
			return s
		}
		n, _ := strconv.ParseInt(m[1], 10, 64)
		unit, ok := currencyTable[strings.ToUpper(m[2])]
		if !ok {
			return s
		}
		return koreanNumber(n) + " " + unit
	})

	// Rule 5: Generic integers, bounded by \b to avoid mangling identifiers
	// like "scene_01" or "v2".
	text = reNumber.ReplaceAllStringFunc(text, func(s string) string {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return s
		}
		return koreanNumber(n)
	})

	// Rule 6: English words — table-driven, unrecognised words left untouched.
	text = reEnglish.ReplaceAllStringFunc(text, func(s string) string {
		if kr, ok := englishWordTable[strings.ToLower(s)]; ok {
			return kr
		}
		return s
	})

	return text
}

// koreanNumber converts a non-negative integer to its Sino-Korean spoken form.
// Examples: 0→"영", 100→"백", 1998→"천구백구십팔".
func koreanNumber(n int64) string {
	if n < 0 {
		return "마이너스 " + koreanNumber(-n)
	}
	if n == 0 {
		return "영"
	}

	var b strings.Builder

	if n >= 100_000_000 {
		c := n / 100_000_000
		// Suppress coefficient 1 before 억 (standard Korean: 억, not 일억).
		if c > 1 {
			b.WriteString(koreanBelow10000(c))
		}
		b.WriteString("억")
		n %= 100_000_000
	}
	if n >= 10_000 {
		c := n / 10_000
		// Suppress coefficient 1 before 만 (standard Korean: 만, not 일만).
		if c > 1 {
			b.WriteString(koreanBelow10000(c))
		}
		b.WriteString("만")
		n %= 10_000
	}
	if n > 0 {
		b.WriteString(koreanBelow10000(n))
	}

	return b.String()
}

// koreanBelow10000 converts 1–9999 into Sino-Korean using 천/백/십/일 units.
// The coefficient "1" is suppressed for each unit (i.e. 천 not 일천, 백 not 일백).
func koreanBelow10000(n int64) string {
	var b strings.Builder
	if c := n / 1000; c > 0 {
		if c > 1 {
			b.WriteString(sinoKorean[c])
		}
		b.WriteString("천")
		n %= 1000
	}
	if c := n / 100; c > 0 {
		if c > 1 {
			b.WriteString(sinoKorean[c])
		}
		b.WriteString("백")
		n %= 100
	}
	if c := n / 10; c > 0 {
		if c > 1 {
			b.WriteString(sinoKorean[c])
		}
		b.WriteString("십")
		n %= 10
	}
	if n > 0 {
		b.WriteString(sinoKorean[n])
	}
	return b.String()
}
