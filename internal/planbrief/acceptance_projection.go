package planbrief

import (
	"fmt"
	"regexp"
	"strings"
)

var projectedTableMark = regexp.MustCompile(`^(\s*\|\s*[*_` + "`" + `]*AC(?:[-_.][A-Za-z0-9]+)+)(?: \[[ xX]\])`)

// ProjectAcceptance changes only factory checkbox positions. Any live requirement
// change (including a same-ID rewrite) conflicts; unrelated sections stay intact.
func ProjectAcceptance(frozen, live string, checked map[string]bool) (string, error) {
	names, err := AcceptanceCriteria(frozen)
	if err != nil {
		return "", err
	}
	liveNames, err := AcceptanceCriteria(live)
	if err != nil {
		return "", err
	}
	if len(names) != len(liveNames) || len(checked) != len(names) {
		return "", fmt.Errorf("acceptance coverage conflict")
	}
	original, positions := acceptanceProjectionLines(frozen)
	current, livePositions := acceptanceProjectionLines(live)
	if len(positions) != len(names) || len(livePositions) != len(names) {
		return "", fmt.Errorf("acceptance layout conflict")
	}
	for i, name := range names {
		if liveNames[i] != name {
			return "", fmt.Errorf("acceptance identity conflict: %s", name)
		}
		if _, ok := checked[name]; !ok {
			return "", fmt.Errorf("missing acceptance decision: %s", name)
		}
		if unmarkCriterion(original[positions[i]]) != unmarkCriterion(current[livePositions[i]]) {
			return "", fmt.Errorf("live requirement conflict: %s", name)
		}
		current[livePositions[i]] = markCriterion(current[livePositions[i]], checked[name])
	}
	return strings.Join(current, "\n"), nil
}

func acceptanceProjectionLines(body string) ([]string, []int) {
	lines := strings.Split(body, "\n")
	var positions []int
	active := false
	fenced := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if strings.HasPrefix(t, "## ") {
			active = strings.EqualFold(strings.TrimSpace(t[3:]), "acceptance criteria")
			continue
		}
		if !active || t == "" {
			continue
		}
		if strings.HasPrefix(t, "|") && (acceptanceTableRule.MatchString(t) || (i+1 < len(lines) && acceptanceTableRule.MatchString(strings.TrimSpace(lines[i+1])))) {
			continue
		}
		positions = append(positions, i)
	}
	return lines, positions
}
func unmarkCriterion(line string) string {
	if strings.HasPrefix(strings.TrimSpace(line), "|") {
		return projectedTableMark.ReplaceAllString(line, "$1")
	}
	t := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(t)]
	prefix := acceptancePrefix.FindString(t)
	t = strings.TrimPrefix(t, prefix)
	for _, p := range []string{"[ ] ", "[x] ", "[X] "} {
		t = strings.TrimPrefix(t, p)
	}
	// A prose obligation becomes a task-list item. Normalization is confined to
	// the list prefix; the complete requirement text remains byte-for-byte equal.
	return indent + t
}
func markCriterion(line string, checked bool) string {
	mark := "[ ]"
	if checked {
		mark = "[x]"
	}
	if strings.HasPrefix(strings.TrimSpace(line), "|") {
		line = projectedTableMark.ReplaceAllString(line, "$1")
		start := strings.Index(line, "|") + 1
		tail := line[start:]
		trimmed := strings.TrimLeft(tail, " \t*_`")
		offset := start + len(tail) - len(trimmed)
		id := acceptanceID.FindString(trimmed)
		return line[:offset+len(id)] + " " + mark + line[offset+len(id):]
	}
	t := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(t)]
	prefix := acceptancePrefix.FindString(t)
	t = strings.TrimPrefix(t, prefix)
	for _, p := range []string{"[ ] ", "[x] ", "[X] "} {
		t = strings.TrimPrefix(t, p)
	}
	if prefix == "" {
		prefix = "- "
	}
	return indent + prefix + mark + " " + t
}
