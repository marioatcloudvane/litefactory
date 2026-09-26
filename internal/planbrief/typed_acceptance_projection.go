package planbrief

import (
	"fmt"
	"strings"
)

// ProjectTypedAcceptance changes only explicitly identified criterion checkboxes.
// The supplied IDs are frozen requirements; narrative cannot add obligations.
func ProjectTypedAcceptance(frozen, live string, ids []string, checked map[string]bool) (string, error) {
	if len(ids) == 0 || len(ids) != len(checked) {
		return "", fmt.Errorf("typed acceptance coverage conflict")
	}
	expected := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !ValidCriterionID(id) || expected[id] {
			return "", fmt.Errorf("invalid typed criterion identity")
		}
		if _, ok := checked[id]; !ok {
			return "", fmt.Errorf("missing acceptance decision: %s", id)
		}
		expected[id] = true
	}
	original, positions, err := typedAcceptanceLines(frozen, expected)
	if err != nil {
		return "", err
	}
	current, livePositions, err := typedAcceptanceLines(live, expected)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		if unmarkCriterion(original[positions[id]]) != unmarkCriterion(current[livePositions[id]]) {
			return "", fmt.Errorf("live requirement conflict: %s", id)
		}
		current[livePositions[id]] = markCriterion(current[livePositions[id]], checked[id])
	}
	return strings.Join(current, "\n"), nil
}

func typedAcceptanceLines(body string, expected map[string]bool) ([]string, map[string]int, error) {
	lines := strings.Split(body, "\n")
	positions := make(map[string]int, len(expected))
	active, found := false, false
	for i, line := range lines {
		// Graph Plan story bodies head the section with the level-1
		// PlanAcceptanceHeading; earlier typed stories use a level-2 heading.
		// Either section ends at the next level-1 or level-2 heading.
		if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
			active = line == "## Acceptance Criteria" || line == PlanAcceptanceHeading
			if active && found {
				return nil, nil, fmt.Errorf("duplicate typed acceptance section")
			}
			found = found || active
			continue
		}
		if !active {
			continue
		}
		text := line
		matched := false
		for _, prefix := range []string{"- [ ] ", "- [x] ", "- [X] "} {
			if strings.HasPrefix(text, prefix) {
				text = strings.TrimPrefix(text, prefix)
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		id, _, ok := strings.Cut(text, ": ")
		if !ok || !ValidCriterionID(id) {
			continue
		}
		if !expected[id] {
			return nil, nil, fmt.Errorf("unknown live criterion: %s", id)
		}
		if _, exists := positions[id]; exists {
			return nil, nil, fmt.Errorf("duplicate typed criterion: %s", id)
		}
		positions[id] = i
	}
	if len(positions) != len(expected) {
		return nil, nil, fmt.Errorf("typed acceptance layout conflict")
	}
	return lines, positions, nil
}
