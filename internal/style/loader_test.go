package style_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/style"
)

func TestLoadDefaultYAML(t *testing.T) {
	t.Parallel()
	root := projectRoot(t)
	sg, err := style.Load(filepath.Join(root, style.DefaultPath))
	if err != nil {
		t.Fatalf("Load default: %v", err)
	}
	if got := sg.Narration.AvgSentenceLengthTense; got != 18 {
		t.Errorf("AvgSentenceLengthTense=%d, want 18", got)
	}
	if got := sg.KoreanSCPTerms["Foundation"]; got != "재단" {
		t.Errorf("Foundation term=%q, want 재단", got)
	}
	if !strings.Contains(sg.AttributionTemplateKO, "{scp_number}") {
		t.Errorf("attribution template missing {scp_number}: %q", sg.AttributionTemplateKO)
	}
	for _, opening := range []string{"안녕하세요", "오늘 소개할", "이번 영상에서는"} {
		found := false
		for _, f := range sg.Narration.ForbiddenOpenings {
			if f == opening {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("forbidden opening %q missing from style guide", opening)
		}
	}
}

func TestLoadMissingFileReturnsNotConfigured(t *testing.T) {
	t.Parallel()
	_, err := style.Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if !errors.Is(err, style.ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestLoadEmptyPathRejected(t *testing.T) {
	t.Parallel()
	_, err := style.Load("")
	if !errors.Is(err, style.ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		yaml    string
		wantSub string
	}{
		{
			name:    "tense<=0",
			yaml:    "narration:\n  avg_sentence_length_tense: 0\n  avg_sentence_length_calm: 28\nkorean_scp_terms:\n  Foundation: 재단\nattribution_template_ko: x\n",
			wantSub: "tense",
		},
		{
			name:    "tense>calm",
			yaml:    "narration:\n  avg_sentence_length_tense: 30\n  avg_sentence_length_calm: 20\nkorean_scp_terms:\n  Foundation: 재단\nattribution_template_ko: x\n",
			wantSub: "calm average",
		},
		{
			name:    "no terms",
			yaml:    "narration:\n  avg_sentence_length_tense: 18\n  avg_sentence_length_calm: 28\nattribution_template_ko: x\n",
			wantSub: "korean_scp_terms",
		},
		{
			name:    "no attribution",
			yaml:    "narration:\n  avg_sentence_length_tense: 18\n  avg_sentence_length_calm: 28\nkorean_scp_terms:\n  Foundation: 재단\n",
			wantSub: "attribution_template_ko",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "style.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := style.Load(path)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestEnvOverridePreferred(t *testing.T) {
	yaml := "narration:\n  avg_sentence_length_tense: 19\n  avg_sentence_length_calm: 30\nkorean_scp_terms:\n  Foundation: 재단\nattribution_template_ko: |\n  override\n"
	path := filepath.Join(t.TempDir(), "override.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(style.EnvOverride, path)
	sg, err := style.LoadFromProjectRoot("/nonexistent/should-be-ignored")
	if err != nil {
		t.Fatal(err)
	}
	if got := sg.Narration.AvgSentenceLengthTense; got != 19 {
		t.Errorf("override not applied; got %d", got)
	}
}

func TestCountAbstractEmotionHits(t *testing.T) {
	t.Parallel()
	root := projectRoot(t)
	sg, err := style.Load(filepath.Join(root, style.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]int{
		"":                                    0,
		"고요한 격리실":                         0,
		"끔찍한 광경":                          1,
		"끔찍하고 무서운 분위기":                  2,
		"매우 두려운 무서운 끔찍한 사건":           3,
	}
	for in, want := range cases {
		if got := sg.CountAbstractEmotionHits(in); got != want {
			t.Errorf("CountAbstractEmotionHits(%q)=%d, want %d", in, got, want)
		}
	}
}

// projectRoot walks up from the test file until it finds go.mod; the
// repo root is where configs/ and prompts/ live. Tests use this rather
// than os.Getwd so they pass under `go test ./...` from any directory.
func projectRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := cwd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find go.mod above %s", cwd)
	return ""
}
