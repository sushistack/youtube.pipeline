package agents

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sushistack/youtube.pipeline/internal/domain"
)

// actHeaderPattern matches the four-act section headers our exemplar files
// use: `## Act 1 — Incident (hook role)`. The act number alone is
// authoritative — if the channel/translator changes the human-readable
// label later, the parser still routes the section to the right act.
var actHeaderPattern = regexp.MustCompile(`^## Act ([1-4]) — `)

// notesHeaderPattern marks the "## Notes for fewshot use" section that
// every exemplar file ends with. The parser cuts off there so the LLM
// never sees our annotation prose. Anchored to the full phrase so a
// stray "## Notes" inside narration prose can't truncate parsing
// silently.
var notesHeaderPattern = regexp.MustCompile(`^## Notes for fewshot use`)

// frontmatterDelimiter is the line that opens and closes the YAML
// frontmatter at the top of every exemplar file.
const frontmatterDelimiter = "---"

// parseExemplar extracts per-act narration from a single exemplar markdown
// file. Returns a map keyed by act ID (`incident`/`mystery`/`revelation`/
// `unresolved`) with the narration text for that act as the value. The
// frontmatter and any `## Notes` section are stripped — only the four
// `## Act N — ` sections survive. All four acts must be present;
// missing any returns ErrValidation.
func parseExemplar(raw string) (map[string]string, error) {
	// Normalize CRLF / lone-CR line endings so a Windows-saved exemplar
	// doesn't leak `\r` into the LLM-visible text and frontmatter
	// detection doesn't miss the `\r\n---\r\n` form.
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	body := stripFrontmatter(raw)
	lines := strings.Split(body, "\n")

	sections := make(map[int][]string, 4)
	currentAct := 0
	stopped := false
	for _, line := range lines {
		if stopped {
			break
		}
		if notesHeaderPattern.MatchString(line) {
			stopped = true
			continue
		}
		if m := actHeaderPattern.FindStringSubmatch(line); m != nil {
			actNum := int(m[1][0] - '0')
			currentAct = actNum
			sections[actNum] = []string{}
			continue
		}
		if currentAct == 0 {
			continue
		}
		sections[currentAct] = append(sections[currentAct], line)
	}

	out := make(map[string]string, 4)
	for actNum, actID := range actNumberToID {
		raw, ok := sections[actNum]
		if !ok {
			return nil, fmt.Errorf("parse exemplar: missing act section %d (%s): %w", actNum, actID, domain.ErrValidation)
		}
		text := strings.TrimSpace(strings.Join(raw, "\n"))
		if text == "" {
			return nil, fmt.Errorf("parse exemplar: act section %d (%s) is empty: %w", actNum, actID, domain.ErrValidation)
		}
		out[actID] = text
	}
	return out, nil
}

// actNumberToID maps the numeric act header (`## Act 1`...) to the domain
// act ID enum. Order follows domain.ActOrder.
var actNumberToID = map[int]string{
	1: domain.ActIncident,
	2: domain.ActMystery,
	3: domain.ActRevelation,
	4: domain.ActUnresolved,
}

// stripFrontmatter removes the leading `---\n...\n---\n` YAML frontmatter
// block if present. Any failure to find a closing delimiter leaves the
// content untouched so a malformed file still has a chance to parse the
// body if act headers are intact.
func stripFrontmatter(raw string) string {
	trimmed := strings.TrimLeft(raw, "\n")
	if !strings.HasPrefix(trimmed, frontmatterDelimiter+"\n") {
		return raw
	}
	rest := trimmed[len(frontmatterDelimiter)+1:]
	idx := strings.Index(rest, "\n"+frontmatterDelimiter+"\n")
	if idx < 0 {
		return raw
	}
	return rest[idx+len("\n"+frontmatterDelimiter+"\n"):]
}

// concatExemplars combines per-act maps from multiple exemplar files into
// a single map. Within each act, the contributing exemplars are joined by
// a blank-line separator so the LLM sees them as distinct passages.
// Returns ErrValidation if any input is missing one of the four acts.
func concatExemplars(parsed []map[string]string) (map[string]string, error) {
	out := make(map[string]string, 4)
	for _, actID := range domain.ActOrder {
		var parts []string
		for i, p := range parsed {
			text, ok := p[actID]
			if !ok || text == "" {
				return nil, fmt.Errorf("concat exemplars: input %d missing act %s: %w", i, actID, domain.ErrValidation)
			}
			parts = append(parts, text)
		}
		out[actID] = strings.Join(parts, "\n\n---\n\n")
	}
	return out, nil
}
