package planbrief

import (
	"fmt"
	"html"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ReconciledStory is a derivation of the supplied issue bytes, not another
// editable source. Body preserves owner text; only allocated IDs are rewritten.
type ReconciledStory struct {
	Story            TypedStory
	Body             string
	RetiredIDs       []string
	PresentationOnly bool
}

// ReconcileTypedStory recognizes the factory's five sections. Semantic ambiguity
// is returned as a pending explanation for ordinary-language Plan refinement.
// usedIDs includes every historical ID, including permanently retired IDs.
func ReconcileTypedStory(prior TypedStory, title, body string, usedIDs []string, scope ContentScope) (ReconciledStory, error) {
	if len(body) > MaxStoryBodyBytes {
		return ReconciledStory{}, fmt.Errorf("pending: story body exceeds %d bytes", MaxStoryBodyBytes)
	}
	parsed, err := parseEditedStory(prior.Key, title, body)
	if err != nil {
		return ReconciledStory{}, err
	}
	parsed.Story.Intent = prior.Intent
	old := map[string]string{}
	used := map[string]bool{}
	maxID := 0
	for _, id := range usedIDs {
		used[id] = true
		n, _ := strconv.Atoi(strings.TrimPrefix(id, "AC-"))
		maxID = max(maxID, n)
	}
	for _, c := range prior.AcceptanceCriteria {
		old[c.ID] = c.Text
		used[c.ID] = true
		n, _ := strconv.Atoi(strings.TrimPrefix(c.ID, "AC-"))
		maxID = max(maxID, n)
	}
	seen := map[string]bool{}
	replacements := map[string]string{}
	retired := []string{}
	for i := range parsed.Story.AcceptanceCriteria {
		c := &parsed.Story.AcceptanceCriteria[i]
		originalID := c.ID
		if c.ID != "" && seen[c.ID] {
			return ReconciledStory{}, fmt.Errorf("pending: criterion %s appears more than once", c.ID)
		}
		seen[c.ID] = true
		previous, existed := old[c.ID]
		allocate := c.ID == "" || !existed
		if !existed && c.ID != "" && used[c.ID] {
			return ReconciledStory{}, fmt.Errorf("pending: retired criterion %s cannot be reused", c.ID)
		}
		if existed && normalizedEdit(previous) != normalizedEdit(c.Text) {
			// A literal value replacement is unambiguous. Other meaning changes
			// require the existing Plan conversation; no similarity heuristic can
			// establish that a free-text clarification preserves an obligation.
			a, _, aok := strings.Cut(previous, " must be ")
			b, _, bok := strings.Cut(c.Text, " must be ")
			if !aok || !bok || a != b {
				return ReconciledStory{}, fmt.Errorf("pending: clarify whether %s changes the obligation or only explains it", c.ID)
			}
			allocate = true
			retired = append(retired, c.ID)
		}
		if allocate {
			maxID++
			if maxID > 999999 {
				return ReconciledStory{}, fmt.Errorf("pending: no unused criterion IDs remain")
			}
			c.ID = fmt.Sprintf("AC-%03d", maxID)
			if originalID != "" {
				replacements[originalID] = c.ID
			}
			line := parsed.criterionLines[i]
			prefix, text, _ := criterionItem(parsed.lines[line])
			if originalID != "" {
				_, text, _ = strings.Cut(text, ":")
			}
			parsed.lines[line] = prefix + c.ID + ": " + strings.TrimSpace(text)
		}
	}
	for _, c := range prior.AcceptanceCriteria {
		if !seen[c.ID] {
			retired = append(retired, c.ID)
		}
	}
	for i := range parsed.Story.Tasks {
		for j, id := range parsed.Story.Tasks[i].CriterionIDs {
			if replacement := replacements[id]; replacement != "" {
				parsed.Story.Tasks[i].CriterionIDs[j] = replacement
			}
		}
		line := parsed.taskLines[i]
		cells := strings.Split(parsed.lines[line], "|")
		for j, id := range strings.Split(cells[4], ",") {
			if replacement := replacements[editedText(id)]; replacement != "" {
				parts := strings.Split(cells[4], ",")
				parts[j] = " " + replacement + " "
				cells[4] = strings.Join(parts, ",")
			}
		}
		parsed.lines[line] = strings.Join(cells, "|")
	}
	if err := parsed.Story.Validate(scope); err != nil {
		return ReconciledStory{}, fmt.Errorf("pending: story structure needs clarification: %w", err)
	}
	return ReconciledStory{Story: parsed.Story, Body: strings.Join(parsed.lines, "\n"), RetiredIDs: retired, PresentationOnly: EquivalentTypedStory(prior, parsed.Story)}, nil
}

// EquivalentTypedStory proves only whitespace and item-order equivalence. It
// never infers semantic equivalence from wording similarity.
func EquivalentTypedStory(a, b TypedStory) bool {
	normalize := func(s TypedStory) TypedStory {
		// Clone slices before sorting so frozen caller values stay immutable.
		s.AcceptanceCriteria = slices.Clone(s.AcceptanceCriteria)
		s.Tasks = slices.Clone(s.Tasks)
		s.Dependencies.StoryKeys = slices.Clone(s.Dependencies.StoryKeys)
		s.Dependencies.IssueRefs = slices.Clone(s.Dependencies.IssueRefs)
		s.Intent = "unchanged"
		for i := range s.AcceptanceCriteria {
			s.AcceptanceCriteria[i].Text = normalizedEdit(s.AcceptanceCriteria[i].Text)
		}
		slices.SortFunc(s.AcceptanceCriteria, func(a, b CriterionContent) int { return strings.Compare(a.ID, b.ID) })
		slices.SortFunc(s.Tasks, func(a, b TaskContent) int { return strings.Compare(a.ID, b.ID) })
		slices.Sort(s.Dependencies.StoryKeys)
		slices.Sort(s.Dependencies.IssueRefs)
		return s
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}

func normalizedEdit(s string) string { return strings.Join(strings.Fields(s), " ") }

// TypedStoryBodyMatches verifies declared content independently of unrelated
// issue prose and checkbox presentation. Undeclared list items cannot match.
func TypedStoryBodyMatches(s TypedStory, body string) bool {
	p, err := parseEditedStory(s.Key, s.Title, body)
	return err == nil && EquivalentTypedStory(s, p.Story)
}

// PreserveStoryText carries owner prose and unknown sections into a new rendering.
// Recognized typed fields are replaced by the contribution; the rest is copied
// verbatim, never interpreted as additional obligations.
func PreserveStoryText(source, rendered string) string {
	section, sub := "", ""
	extras := map[string][]string{}
	unknown := []string{}
	known := map[string]bool{"User Story": true, "Acceptance Criteria": true, "Tasks": true, "Architecture": true, "Dependencies": true, "Story Guidance": true}
	continuation := false
	architectureSummary := false
	unknownSubsection := false
	for _, line := range strings.Split(source, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimPrefix(line, "## ")
			sub = ""
			unknownSubsection = false
			continuation = false
			if !known[section] {
				unknown = append(unknown, line)
			}
			continue
		}
		if section == "" {
			if trim != "" && !strings.HasPrefix(line, "<!-- lf:story ") {
				unknown = append(unknown, line)
			}
			continue
		}
		if !known[section] {
			unknown = append(unknown, line)
			continue
		}
		if strings.HasPrefix(line, "### ") {
			sub = strings.TrimPrefix(line, "### ")
			unknownSubsection = section != "Architecture" || (sub != "Components" && sub != "Interfaces" && sub != "Constraints" && sub != "Decisions")
			if unknownSubsection {
				extras[section] = append(extras[section], line)
			}
			continue
		}
		if unknownSubsection {
			extras[section] = append(extras[section], line)
			continue
		}
		if trim == "" {
			if len(extras[section]) > 0 {
				extras[section] = append(extras[section], line)
			}
			continue
		}
		consumed := false
		switch section {
		case "User Story":
			consumed = strings.HasPrefix(line, "As ") || strings.HasPrefix(line, "- ")
		case "Acceptance Criteria":
			_, _, item := criterionItem(line)
			consumed = item || (continuation && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")))
			continuation = consumed
		case "Tasks":
			consumed = strings.HasPrefix(trim, "|")
		case "Architecture":
			consumed = (sub == "" && !architectureSummary) || strings.HasPrefix(trim, "|") || (sub == "Constraints" && (strings.HasPrefix(line, "- ") || trim == "None."))
			if sub == "" {
				architectureSummary = true
			}
		case "Dependencies":
			consumed = strings.HasPrefix(line, "Story keys:") || strings.HasPrefix(line, "Issue references:")
		case "Story Guidance":
			consumed = line == StoryGuidance
		}
		if !consumed {
			extras[section] = append(extras[section], line)
		}
	}
	lines := strings.Split(rendered, "\n")
	out := []string{}
	section = ""
	flush := func() {
		if len(extras[section]) > 0 {
			out = append(out, extras[section]...)
			out = append(out, "")
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			section = strings.TrimPrefix(line, "## ")
			if section == "Story Guidance" && len(unknown) > 0 {
				out = append(out, unknown...)
				out = append(out, "")
				unknown = nil
			}
		}
		if line == StoryGuidance && len(extras["Story Guidance"]) > 0 {
			out = append(out, extras["Story Guidance"]...)
			out = append(out, "")
			delete(extras, "Story Guidance")
		}
		out = append(out, line)
	}
	flush()
	if len(unknown) > 0 {
		out = append(out, "")
		out = append(out, unknown...)
	}
	return strings.Join(out, "\n")
}
func editedText(s string) string {
	return html.UnescapeString(strings.ReplaceAll(strings.TrimSpace(s), "<br>", "\n"))
}
func editedList(s string) []string {
	if strings.TrimSpace(s) == "None." || strings.TrimSpace(s) == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = editedText(parts[i])
	}
	return parts
}
func criterionItem(line string) (string, string, bool) {
	for _, prefix := range []string{"- [ ] ", "- [x] ", "- [X] "} {
		if strings.HasPrefix(line, prefix) {
			return prefix, strings.TrimPrefix(line, prefix), true
		}
	}
	if strings.HasPrefix(line, "- AC-") {
		return "- ", strings.TrimPrefix(line, "- "), true
	}
	return "", "", false
}

type editedStory struct {
	Story                     TypedStory
	lines                     []string
	criterionLines, taskLines []int
}

func parseEditedStory(key, title, body string) (editedStory, error) {
	p := editedStory{lines: strings.Split(body, "\n")}
	if len(body) > MaxStoryBodyBytes {
		return p, fmt.Errorf("pending: story body exceeds %d bytes", MaxStoryBodyBytes)
	}
	s := TypedStory{Key: key, Intent: "update", Title: title, UserStory: UserStoryContent{Notes: []string{}}, AcceptanceCriteria: []CriterionContent{}, Tasks: []TaskContent{}, Architecture: ArchitectureContent{Components: []ComponentContent{}, Interfaces: []InterfaceContent{}, Constraints: []string{}, Decisions: []DecisionContent{}}, Dependencies: DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}}}
	section, sub := "", ""
	unknownSubsection := false
	seen := map[string]bool{}
	dependencyLines := map[string]bool{}
	lastCriterion := -1
	for n, line := range p.lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			return p, fmt.Errorf("pending: fenced story content needs ordinary-language clarification")
		}
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimPrefix(line, "## ")
			sub = ""
			unknownSubsection = false
			lastCriterion = -1
			if seen[section] {
				return p, fmt.Errorf("pending: %s section appears more than once", section)
			}
			seen[section] = true
			continue
		}
		if strings.HasPrefix(line, "### ") {
			sub = strings.TrimPrefix(line, "### ")
			unknownSubsection = section != "Architecture" || (sub != "Components" && sub != "Interfaces" && sub != "Constraints" && sub != "Decisions")
			continue
		}
		if unknownSubsection {
			continue
		}
		if trim == "" {
			continue
		}
		switch section {
		case "User Story":
			if strings.HasPrefix(trim, "As ") && s.UserStory.Actor == "" {
				actor, rest, ok := strings.Cut(strings.TrimPrefix(trim, "As "), ", I want ")
				if !ok {
					return p, fmt.Errorf("pending: explain the actor and requested behavior in User Story")
				}
				want, benefit, ok := strings.Cut(rest, ", so that ")
				if !ok {
					return p, fmt.Errorf("pending: explain the benefit in User Story")
				}
				s.UserStory.Actor = editedText(actor)
				s.UserStory.Want = editedText(want)
				s.UserStory.Benefit = editedText(strings.TrimSuffix(benefit, "."))
			} else if strings.HasPrefix(line, "- ") {
				s.UserStory.Notes = append(s.UserStory.Notes, editedText(line[2:]))
			}
		case "Acceptance Criteria":
			_, text, item := criterionItem(line)
			switch {
			case item:
				id, description, hasID := strings.Cut(text, ":")
				if !hasID || !ValidCriterionID(id) {
					if strings.HasPrefix(text, "AC-") {
						return p, fmt.Errorf("pending: criterion identifier is not canonical")
					}
					id = ""
					description = text
				}
				s.AcceptanceCriteria = append(s.AcceptanceCriteria, CriterionContent{ID: id, Text: editedText(description)})
				p.criterionLines = append(p.criterionLines, n)
				lastCriterion = len(s.AcceptanceCriteria) - 1
			case lastCriterion >= 0 && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")):
				s.AcceptanceCriteria[lastCriterion].Text += "\n" + editedText(line)
			default:
				lastCriterion = -1
			}
		case "Tasks":
			if strings.HasPrefix(trim, "|") {
				cells, err := editedCells(trim, 5)
				if err != nil {
					return p, err
				}
				if cells[0] == "ID" || strings.HasPrefix(cells[0], "---") {
					continue
				}
				s.Tasks = append(s.Tasks, TaskContent{ID: cells[0], Description: cells[1], OwnerRole: cells[2], CriterionIDs: editedList(cells[3]), DependsOn: editedList(cells[4])})
				p.taskLines = append(p.taskLines, n)
			}
		case "Architecture":
			switch {
			case sub == "" && s.Architecture.Summary == "":
				s.Architecture.Summary = editedText(line)
			case sub == "Constraints" && strings.HasPrefix(line, "- "):
				s.Architecture.Constraints = append(s.Architecture.Constraints, editedText(line[2:]))
			case strings.HasPrefix(trim, "|"):
				count := 2
				if sub == "Interfaces" {
					count = 3
				}
				cells, err := editedCells(trim, count)
				if err != nil {
					return p, err
				}
				if cells[0] == "Name" || cells[0] == "Decision" || strings.HasPrefix(cells[0], "---") {
					continue
				}
				switch sub {
				case "Components":
					s.Architecture.Components = append(s.Architecture.Components, ComponentContent{Name: cells[0], Responsibility: cells[1]})
				case "Interfaces":
					s.Architecture.Interfaces = append(s.Architecture.Interfaces, InterfaceContent{Name: cells[0], Input: cells[1], Output: cells[2]})
				case "Decisions":
					s.Architecture.Decisions = append(s.Architecture.Decisions, DecisionContent{Decision: cells[0], Reason: cells[1]})
				default:
					return p, fmt.Errorf("pending: identify the Architecture table")
				}
			}
		case "Dependencies":
			if strings.HasPrefix(trim, "US-") || strings.HasPrefix(trim, "- US-") || trim == "None." {
				return p, fmt.Errorf("pending: dependency list contradicts or does not identify the Story keys and Issue references fields")
			}
			for _, prefix := range []string{"Story keys:", "Issue references:"} {
				if strings.HasPrefix(line, prefix) {
					if dependencyLines[prefix] {
						return p, fmt.Errorf("pending: contradictory %s dependencies", prefix)
					}
					dependencyLines[prefix] = true
					v := editedList(strings.TrimPrefix(line, prefix))
					if prefix == "Story keys:" {
						s.Dependencies.StoryKeys = v
					} else {
						s.Dependencies.IssueRefs = v
					}
				}
			}
		}
	}
	for _, section := range []string{"User Story", "Acceptance Criteria", "Tasks", "Architecture", "Dependencies"} {
		if !seen[section] {
			return p, fmt.Errorf("pending: restore the missing %s section or comment with the intended change", section)
		}
	}
	if !dependencyLines["Story keys:"] || !dependencyLines["Issue references:"] {
		return p, fmt.Errorf("pending: explain the story keys and issue references in Dependencies")
	}
	if !strings.Contains(body, StoryGuidance) {
		if !seen["Story Guidance"] {
			p.lines = append(p.lines, "", "## Story Guidance", "", StoryGuidance, "")
		} else {
			active := false
			at := len(p.lines)
			for i, line := range p.lines {
				if line == "## Story Guidance" {
					active = true
					continue
				}
				if active && strings.HasPrefix(line, "## ") {
					at = i
					break
				}
			}
			p.lines = slices.Insert(p.lines, at, "", StoryGuidance, "")
		}
	}
	p.Story = s
	return p, nil
}

func editedCells(line string, count int) ([]string, error) {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	if len(parts) != count {
		return nil, fmt.Errorf("pending: table row has %d cells; expected %d", len(parts), count)
	}
	for i := range parts {
		parts[i] = editedText(parts[i])
	}
	return parts, nil
}
