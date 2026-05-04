package text

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitKRSentences_KoreanEomiTerminators(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "다. terminator",
			in:   "이것은 SCP-049입니다.이상한 존재죠.",
			want: []string{"이것은 SCP-049입니다.", "이상한 존재죠."},
		},
		{
			name: "요. terminator with trailing space",
			in:   "보고 있어요. 보고서를 읽었죠.",
			want: []string{"보고 있어요. ", "보고서를 읽었죠."},
		},
		{
			name: "니다. terminator",
			in:   "안녕하십니까. 환영합니다.",
			want: []string{"안녕하십니까. ", "환영합니다."},
		},
		{
			name: "question and exclamation",
			in:   "정말로? 그럴 리가!",
			want: []string{"정말로? ", "그럴 리가!"},
		},
		{
			name: "ellipsis",
			in:   "그러나… 우리는 알고 있다.",
			want: []string{"그러나… ", "우리는 알고 있다."},
		},
		{
			name: "newline as boundary",
			in:   "첫 문단.\n둘째 문단.",
			want: []string{"첫 문단.\n", "둘째 문단."},
		},
		{
			name: "no terminator returns single element",
			in:   "끝없는 문장",
			want: []string{"끝없는 문장"},
		},
		{
			name: "empty input returns nil",
			in:   "",
			want: nil,
		},
		{
			name: "trailing tab absorbed",
			in:   "한 문장.\t다음 문장.",
			want: []string{"한 문장.\t", "다음 문장."},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitKRSentences(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len(got)=%d, want %d; got=%q", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got[%d]=%q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestSplitKRSentences_ConcatRecoversInputByteForByte(t *testing.T) {
	inputs := []string{
		"이것은 SCP-049입니다.이상한 존재죠.",
		"안녕하세요. 오늘은 049를 다룰 거예요. 준비됐나요? 시작합니다!",
		"그러나… 우리는 알고 있다. 다음 영상에서 만나도록 하죠. 안녕!",
		"첫 문단.\n둘째 문단.\n셋째 문단.",
		"끝없는 문장",
		"",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			parts := SplitKRSentences(in)
			recombined := strings.Join(parts, "")
			if recombined != in {
				t.Errorf("concat(parts) != input\ninput:    %q\nrecombined: %q\nparts:    %q", in, recombined, parts)
			}
		})
	}
}

func TestPackKRChunks_PacksUnderByteCap(t *testing.T) {
	sentences := []string{"가나다.", "라마바.", "사아자.", "차카타.", "파하."}
	chunks := PackKRChunks(sentences, 18)
	for i, c := range chunks {
		if len(c) > 18 {
			t.Errorf("chunk[%d] len=%d exceeds cap 18: %q", i, len(c), c)
		}
	}
	got := strings.Join(chunks, "")
	want := strings.Join(sentences, "")
	if got != want {
		t.Errorf("concat(chunks) != concat(sentences)\ngot:  %q\nwant: %q", got, want)
	}
}

func TestPackKRChunks_OversizeSentenceFallsThroughHardSplit(t *testing.T) {
	long := strings.Repeat("가", 100)
	chunks := PackKRChunks([]string{long}, 30)
	for i, c := range chunks {
		if len(c) > 30 {
			t.Errorf("chunk[%d] len=%d exceeds cap 30", i, len(c))
		}
		if !utf8ValidPrefix(c) {
			t.Errorf("chunk[%d] not UTF-8 valid: %q", i, c)
		}
	}
	if got := strings.Join(chunks, ""); got != long {
		t.Errorf("concat hard-split chunks != original\ngot:  %q\nwant: %q", got, long)
	}
}

func TestPackKRChunks_NonPositiveCapPassesThrough(t *testing.T) {
	in := []string{"가나다.", "라마바."}
	if got := PackKRChunks(in, 0); len(got) != 2 {
		t.Errorf("expected pass-through with cap=0, got %v", got)
	}
	if got := PackKRChunks(in, -1); len(got) != 2 {
		t.Errorf("expected pass-through with cap=-1, got %v", got)
	}
}

func TestChunkKR_FitsUnderCapReturnsSingleChunk(t *testing.T) {
	in := "짧은 문장입니다."
	chunks := ChunkKR(in, 100)
	if len(chunks) != 1 || chunks[0] != in {
		t.Errorf("expected single chunk equal to input, got %v", chunks)
	}
}

func TestChunkKR_LargeInputChunksAtSentenceBoundaries(t *testing.T) {
	body := strings.Repeat("문장 하나입니다. ", 100)
	chunks := ChunkKR(body, 60)
	if len(chunks) < 5 {
		t.Fatalf("expected multi-chunk output, got %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > 60 {
			t.Errorf("chunk[%d] len=%d exceeds cap 60", i, len(c))
		}
	}
	if got := strings.Join(chunks, ""); got != body {
		t.Errorf("concat(chunks) != input")
	}
}

func TestChunkKR_EmptyInput(t *testing.T) {
	if got := ChunkKR("", 100); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

func TestHardSplitUTF8_NeverSplitsRune(t *testing.T) {
	in := "가나다라마"
	chunks := HardSplitUTF8(in, 4)
	for i, c := range chunks {
		if !utf8ValidPrefix(c) {
			t.Errorf("chunk[%d] not UTF-8 valid: %q (% x)", i, c, []byte(c))
		}
		if len(c) > 4 {
			t.Errorf("chunk[%d] len=%d exceeds cap 4", i, len(c))
		}
	}
	if got := strings.Join(chunks, ""); got != in {
		t.Errorf("concat(chunks) != input")
	}
}

func utf8ValidPrefix(s string) bool {
	return utf8.ValidString(s)
}
