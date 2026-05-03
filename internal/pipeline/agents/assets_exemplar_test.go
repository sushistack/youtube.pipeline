package agents

import (
	"errors"
	"strings"
	"testing"

	"github.com/sushistack/youtube.pipeline/internal/domain"
	"github.com/sushistack/youtube.pipeline/internal/testutil"
)

const sampleExemplar = `---
scp_id: SCP-TEST
language: ko
---

# SCP-TEST — Korean SCP Narration Exemplar

> some intro

## Act 1 — Incident (hook role)

This is the act-1 narration.
Continuation of act 1.

## Act 2 — Mystery (tension role)

Act-2 narration here.

## Act 3 — Revelation (reveal role)

Act-3 narration here.

## Act 4 — Unresolved (bridge role)

Act-4 narration here.

---

## Notes for fewshot use

- annotation that should NEVER reach the LLM
- another note
`

func TestParseExemplar_Happy(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	out, err := parseExemplar(sampleExemplar)
	if err != nil {
		t.Fatalf("parseExemplar: %v", err)
	}
	for _, actID := range domain.ActOrder {
		text, ok := out[actID]
		if !ok || text == "" {
			t.Fatalf("act %s missing or empty: %v", actID, out)
		}
	}
	if !strings.Contains(out[domain.ActIncident], "Continuation of act 1") {
		t.Fatalf("act 1 missing continuation line: %q", out[domain.ActIncident])
	}
	if strings.Contains(out[domain.ActUnresolved], "annotation") {
		t.Fatalf("Notes section leaked into act 4: %q", out[domain.ActUnresolved])
	}
	if strings.Contains(out[domain.ActUnresolved], "scp_id") {
		t.Fatalf("frontmatter leaked into act 4: %q", out[domain.ActUnresolved])
	}
}

func TestParseExemplar_MissingAct(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Drop Act 3 section.
	missingAct3 := strings.Replace(sampleExemplar,
		"## Act 3 — Revelation (reveal role)\n\nAct-3 narration here.\n\n",
		"",
		1,
	)
	_, err := parseExemplar(missingAct3)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "missing act section 3") {
		t.Fatalf("expected 'missing act section 3' in error, got %v", err)
	}
}

func TestParseExemplar_EmptyActSection(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Replace Act 3 body with whitespace only.
	emptyAct3 := strings.Replace(sampleExemplar,
		"Act-3 narration here.",
		"",
		1,
	)
	_, err := parseExemplar(emptyAct3)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("expected 'is empty' in error, got %v", err)
	}
}

func TestParseExemplar_NoFrontmatter(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	// Strip the frontmatter from the sample — the parser should still
	// extract sections cleanly when the file has no YAML header.
	body := strings.SplitN(sampleExemplar, "---\n\n#", 2)[1]
	body = "#" + body
	out, err := parseExemplar(body)
	if err != nil {
		t.Fatalf("parseExemplar: %v", err)
	}
	for _, actID := range domain.ActOrder {
		if out[actID] == "" {
			t.Fatalf("act %s empty after no-frontmatter parse", actID)
		}
	}
}

func TestConcatExemplars_BlankLineSeparator(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	a := map[string]string{
		domain.ActIncident:   "first incident",
		domain.ActMystery:    "first mystery",
		domain.ActRevelation: "first revelation",
		domain.ActUnresolved: "first unresolved",
	}
	b := map[string]string{
		domain.ActIncident:   "second incident",
		domain.ActMystery:    "second mystery",
		domain.ActRevelation: "second revelation",
		domain.ActUnresolved: "second unresolved",
	}
	out, err := concatExemplars([]map[string]string{a, b})
	if err != nil {
		t.Fatalf("concatExemplars: %v", err)
	}
	for _, actID := range domain.ActOrder {
		if !strings.Contains(out[actID], "first ") || !strings.Contains(out[actID], "second ") {
			t.Fatalf("act %s missing one of the inputs: %q", actID, out[actID])
		}
		if !strings.Contains(out[actID], "\n\n---\n\n") {
			t.Fatalf("act %s missing separator: %q", actID, out[actID])
		}
	}
}

func TestConcatExemplars_MissingActFails(t *testing.T) {
	testutil.BlockExternalHTTP(t)

	a := map[string]string{
		domain.ActIncident:   "first",
		domain.ActMystery:    "first",
		domain.ActRevelation: "first",
		domain.ActUnresolved: "first",
	}
	b := map[string]string{
		domain.ActIncident: "second",
		// missing mystery / revelation / unresolved
	}
	_, err := concatExemplars([]map[string]string{a, b})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}
