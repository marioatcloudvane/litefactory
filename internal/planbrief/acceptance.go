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
	lines := strings.Split(body, "\n")
	active, found, fenced := false, false, false
	var names []string
	seen := map[string]bool{}
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			if active {
				return nil, fmt.Errorf("acceptance criteria cannot contain fenced blocks")
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
					return nil, fmt.Errorf("duplicate acceptance criteria section")
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
				return nil, fmt.Errorf("invalid acceptance criteria table")
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
			return nil, fmt.Errorf("unsupported acceptance criterion")
		}
		// Formatting around an identifier is presentation, not part of its name.
		idText := strings.TrimLeft(t, "*`_")
		name := acceptanceID.FindString(idText)
		if name == "" {
			if table {
				return nil, fmt.Errorf("acceptance table rows require AC identifiers")
			}
			name = t
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate acceptance criterion %s", name)
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("acceptance criteria have no named obligations")
	}
	return names, nil
}
