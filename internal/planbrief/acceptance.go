package planbrief

import (
	"fmt"
	"regexp"
	"strings"
)

var acceptancePrefix = regexp.MustCompile(`^(?:[-*+]\s+|[0-9]+[.)]\s+)`)
var acceptanceID = regexp.MustCompile(`^AC(?:[-_.][A-Za-z0-9]+)+\b`)
var acceptanceTableRule = regexp.MustCompile(`^\|?\s*:?-+:?\s*(?:\|\s*:?-+:?\s*)+\|?$`)

// AcceptanceCriteria is the publication and completion contract. Each nonempty
// line is one obligation, named by its leading AC identifier or its exact text
// after a list/checklist prefix. Markdown table rows require AC identifiers.
// Ambiguous constructs fail closed rather than silently losing obligations.
func AcceptanceCriteria(body string) ([]string, error) {
	names, _, err := acceptanceCriteria(body)
	return names, err
}

// AcceptanceCriteriaText names each criterion exactly as AcceptanceCriteria
// does and also returns the text of the line it was read from, in the same
// order, so a caller can read markers that are not part of the name.
func AcceptanceCriteriaText(body string) (names, texts []string, err error) {
	return acceptanceCriteria(body)
}

// RequiresExecution reports whether a frozen criterion needs an executed
// passing check before it can be satisfied. Only an explicit (Manual) marker
// exempts it; unmarked criteria and criteria marked (Auto) fail closed.
func RequiresExecution(text string) bool {
	return !strings.Contains(text, "(Manual)") || strings.Contains(text, "(Auto)")
}

func acceptanceCriteria(body string) ([]string, []string, error) {
	lines := strings.Split(body, "\n")
	active, found, fenced := false, false, false
	var names, texts []string
	seen := map[string]bool{}
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			if active {
				return nil, nil, fmt.Errorf("acceptance criteria cannot contain fenced blocks")
			}
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if strings.HasPrefix(t, "## ") {
			active = strings.EqualFold(strings.TrimSpace(t[3:]), "acceptance criteria")
			if active {
				if found {
					return nil, nil, fmt.Errorf("duplicate acceptance criteria section")
				}
				found = true
			}
			continue
		}
		if !active || t == "" {
			continue
		}
		table := strings.HasPrefix(t, "|")
		if table {
			if acceptanceTableRule.MatchString(t) {
				continue
			}
			if i+1 < len(lines) && acceptanceTableRule.MatchString(strings.TrimSpace(lines[i+1])) {
				continue
			}
			cells := strings.Split(strings.Trim(t, "|"), "|")
			if len(cells) < 2 {
				return nil, nil, fmt.Errorf("invalid acceptance criteria table")
			}
			t = strings.TrimSpace(cells[0])
		} else {
			t = acceptancePrefix.ReplaceAllString(t, "")
			for _, prefix := range []string{"[ ] ", "[x] ", "[X] "} {
				t = strings.TrimPrefix(t, prefix)
			}
		}
		t = strings.TrimSpace(t)
		if t == "" || t == "-" || t == "*" || t == "+" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "<") {
			return nil, nil, fmt.Errorf("unsupported acceptance criterion")
		}
		// Formatting around an identifier is presentation, not part of its name.
		idText := strings.TrimLeft(t, "*`_")
		name := acceptanceID.FindString(idText)
		if name == "" {
			if table {
				return nil, nil, fmt.Errorf("acceptance table rows require AC identifiers")
			}
			name = t
		}
		if seen[name] {
			return nil, nil, fmt.Errorf("duplicate acceptance criterion %s", name)
		}
		seen[name] = true
		names = append(names, name)
		texts = append(texts, strings.TrimSpace(line))
	}
	if len(names) == 0 {
		return nil, nil, fmt.Errorf("acceptance criteria have no named obligations")
	}
	return names, texts, nil
}
