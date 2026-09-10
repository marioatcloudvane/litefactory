package planbrief

import (
	"fmt"
	"strings"
)

// ConvertLegacyStory converts only recognizable legacy structure. Ambiguous
// obligations are conflicts, never silently discarded or inferred from prose.
// The owner can clarify the issue and preview again before any publication.
func ConvertLegacyStory(key, title, body string, dependencies []string, scope ContentScope) (TypedStory, error) {
	s := TypedStory{Key: key, Title: title, Intent: "update", UserStory: UserStoryContent{Notes: []string{}}, AcceptanceCriteria: []CriterionContent{}, Tasks: []TaskContent{}, Architecture: ArchitectureContent{Components: []ComponentContent{}, Interfaces: []InterfaceContent{}, Constraints: []string{}, Decisions: []DecisionContent{}}, Dependencies: DependencyContent{StoryKeys: append([]string{}, dependencies...), IssueRefs: []string{}}}
	if len(body) > MaxStoryBodyBytes {
		return s, fmt.Errorf("legacy body exceeds size budget")
	}
	sections := map[string][]string{}
	section := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") || strings.HasPrefix(strings.TrimSpace(line), "~~~") {
			return s, fmt.Errorf("fenced legacy content requires clarification before conversion")
		}
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if _, ok := sections[section]; ok {
				return s, fmt.Errorf("duplicate %s section", section)
			}
			sections[section] = []string{}
			continue
		}
		sections[section] = append(sections[section], line)
	}
	for _, name := range []string{"User Story", "Acceptance Criteria", "Tasks", "Architecture", "Dependencies"} {
		if _, ok := sections[name]; !ok {
			return s, fmt.Errorf("missing %s section", name)
		}
	}
	story := strings.TrimSpace(strings.Join(sections["User Story"], "\n"))
	line, notes, _ := strings.Cut(story, "\n")
	actor, rest, ok := strings.Cut(strings.TrimPrefix(line, "As "), ", I want ")
	if !ok || !strings.HasPrefix(line, "As ") {
		return s, fmt.Errorf("user story needs an identifiable actor, request and benefit")
	}
	want, benefit, ok := strings.Cut(rest, ", so that ")
	if !ok {
		return s, fmt.Errorf("user story benefit is ambiguous")
	}
	s.UserStory.Actor = actor
	s.UserStory.Want = want
	s.UserStory.Benefit = strings.TrimSuffix(benefit, ".")
	if strings.TrimSpace(notes) != "" {
		s.UserStory.Notes = append(s.UserStory.Notes, strings.TrimSpace(notes))
	}
	ids := []string{}
	for _, line := range sections["Acceptance Criteria"] {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		text = acceptancePrefix.ReplaceAllString(text, "")
		for _, prefix := range []string{"[ ] ", "[x] ", "[X] "} {
			text = strings.TrimPrefix(text, prefix)
		}
		id, text, ok := strings.Cut(text, ":")
		if !ok || !ValidCriterionID(id) || strings.TrimSpace(text) == "" {
			return s, fmt.Errorf("acceptance criteria must identify each obligation explicitly with a canonical AC ID; clarify prose or unsupported rows")
		}
		s.AcceptanceCriteria = append(s.AcceptanceCriteria, CriterionContent{ID: id, Text: strings.TrimSpace(text)})
		ids = append(ids, id)
	}
	for _, line := range sections["Tasks"] {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		if !strings.HasPrefix(text, "|") {
			return s, fmt.Errorf("tasks contains prose outside the recognized legacy task table")
		}
		cells := strings.Split(strings.Trim(text, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if len(cells) == 0 {
			continue
		}
		if strings.HasPrefix(cells[0], "---") || cells[0] == "Task ID" {
			continue
		}
		if len(cells) != 6 {
			return s, fmt.Errorf("tasks requires Task ID, Description, Agent, Size, Depends On and Status")
		}
		deps := []string{}
		if cells[4] != "—" && cells[4] != "-" && cells[4] != "None." && cells[4] != "" {
			for _, dep := range strings.Split(cells[4], ",") {
				deps = append(deps, strings.TrimSpace(dep))
			}
		}
		description := cells[1] + "\nLegacy size: " + cells[3] + "; status: " + cells[5]
		s.Tasks = append(s.Tasks, TaskContent{ID: cells[0], Description: description, OwnerRole: cells[2], CriterionIDs: append([]string{}, ids...), DependsOn: deps})
	}
	s.Architecture.Summary = strings.TrimSpace(strings.Join(sections["Architecture"], "\n"))
	parsedDeps := []string{}
	for _, line := range sections["Dependencies"] {
		text := strings.TrimSpace(line)
		if text == "" || text == "None." || text == "None" {
			continue
		}
		text = strings.TrimPrefix(text, "- ")
		for _, dep := range strings.Split(text, ",") {
			dep = strings.TrimSpace(dep)
			if !proposalKey.MatchString(dep) {
				return s, fmt.Errorf("dependencies has an ambiguous or unresolved reference")
			}
			parsedDeps = append(parsedDeps, dep)
		}
	}
	if !sameStringSet(parsedDeps, dependencies) {
		return s, fmt.Errorf("issue Dependencies differs from the feature's managed dependency graph; reconcile it before upgrade")
	}
	if err := s.Validate(scope); err != nil {
		return s, err
	}
	return s, nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, v := range a {
		if seen[v] {
			return false
		}
		seen[v] = true
	}
	for _, v := range b {
		if !seen[v] {
			return false
		}
	}
	return true
}

// PreserveLegacyStoryText keeps sections outside the five converted fields.
// Their content is copied verbatim; the converted sections already retain their
// source meaning in typed fields and must not be appended a second time.
func PreserveLegacyStoryText(source, rendered string) string {
	keep := true
	lines := []string{}
	for _, line := range strings.Split(source, "\n") {
		if strings.HasPrefix(line, "## ") {
			switch strings.TrimPrefix(line, "## ") {
			case "User Story", "Acceptance Criteria", "Tasks", "Architecture", "Dependencies":
				keep = false
			default:
				keep = true
			}
		}
		if keep {
			lines = append(lines, line)
		}
	}
	return PreserveStoryText(strings.Join(lines, "\n"), rendered)
}
