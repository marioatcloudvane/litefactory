package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// RenderTypedStory renders all content deterministically after local validation.
// New-contract consumers use frozen typed IDs, never parse names from this text.
func RenderTypedStory(s planbrief.TypedStory, scope planbrief.ContentScope) (string, error) {
	if err := s.Validate(scope); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("## User Story\n\n")
	fmt.Fprintf(&b, "As %s, I want %s, so that %s.\n", storyText(s.UserStory.Actor), storyText(s.UserStory.Want), storyText(s.UserStory.Benefit))
	for _, note := range s.UserStory.Notes {
		fmt.Fprintf(&b, "\n- %s\n", storyText(note))
	}
	b.WriteString("\n## Acceptance Criteria\n\n")
	for _, c := range s.AcceptanceCriteria {
		fmt.Fprintf(&b, "- [ ] %s: %s\n", c.ID, storyText(c.Text))
	}
	b.WriteString("\n## Tasks\n\n| ID | Description | Owner role | Criterion IDs | Depends on |\n|---|---|---|---|---|\n")
	for _, t := range s.Tasks {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", storyText(t.ID), storyText(t.Description), storyText(t.OwnerRole), storyList(t.CriterionIDs), storyList(t.DependsOn))
	}
	b.WriteString("\n## Architecture\n\n")
	fmt.Fprintf(&b, "%s\n", storyText(s.Architecture.Summary))
	b.WriteString("\n### Components\n\n| Name | Responsibility |\n|---|---|\n")
	for _, c := range s.Architecture.Components {
		fmt.Fprintf(&b, "| %s | %s |\n", storyText(c.Name), storyText(c.Responsibility))
	}
	b.WriteString("\n### Interfaces\n\n| Name | Input | Output |\n|---|---|---|\n")
	for _, v := range s.Architecture.Interfaces {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", storyText(v.Name), storyText(v.Input), storyText(v.Output))
	}
	b.WriteString("\n### Constraints\n\n")
	if len(s.Architecture.Constraints) == 0 {
		b.WriteString("None.\n")
	}
	for _, c := range s.Architecture.Constraints {
		fmt.Fprintf(&b, "- %s\n", storyText(c))
	}
	b.WriteString("\n### Decisions\n\n| Decision | Reason |\n|---|---|\n")
	for _, d := range s.Architecture.Decisions {
		fmt.Fprintf(&b, "| %s | %s |\n", storyText(d.Decision), storyText(d.Reason))
	}
	b.WriteString("\n## Dependencies\n\n")
	fmt.Fprintf(&b, "Story keys: %s\n\nIssue references: %s\n", storyList(s.Dependencies.StoryKeys), storyList(s.Dependencies.IssueRefs))
	fmt.Fprintf(&b, "\n## Story Guidance\n\n%s\n", planbrief.StoryGuidance)
	if b.Len() > planbrief.MaxStoryBodyBytes {
		return "", fmt.Errorf("story %s: rendered body exceeds %d bytes", s.Key, planbrief.MaxStoryBodyBytes)
	}
	return b.String(), nil
}

// RenderTypedPlan validates the full graph and both typed and escaped rendered
// transport budgets. Returned bodies correspond exactly to the input story order.
// A caller adding envelope metadata must check its final serialized transport too.
func RenderTypedPlan(p planbrief.TypedPlan, scope planbrief.ContentScope) ([]string, error) {
	if err := p.Validate(scope); err != nil {
		return nil, err
	}
	bodies := make([]string, len(p.Stories))
	var faults []error
	for i, s := range p.Stories {
		body, err := RenderTypedStory(s, scope)
		if err != nil {
			faults = append(faults, err)
		}
		bodies[i] = body
	}
	encoded, err := json.Marshal(bodies)
	if err != nil {
		faults = append(faults, err)
	} else if len(encoded) > planbrief.MaxProposalBytes {
		faults = append(faults, fmt.Errorf("rendered plan exceeds %d escaped JSON bytes", planbrief.MaxProposalBytes))
	}
	if err := errors.Join(faults...); err != nil {
		return nil, err
	}
	return bodies, nil
}

func storyList(values []string) string {
	if len(values) == 0 {
		return "None."
	}
	escaped := make([]string, len(values))
	for i, v := range values {
		escaped[i] = storyText(v)
	}
	return strings.Join(escaped, ", ")
}

// Encode Markdown punctuation as entities and every line separator explicitly.
// Input cannot create literal headings, tables, checklists, HTML or fence markers.
// Escaping ampersands first also prevents input entities from becoming structure.
func storyText(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r == '\n':
			b.WriteString("<br>")
		case r == '\r':
			b.WriteString("&#13;")
		case r < 32 || r == 127 || strings.ContainsRune("&<>|`*_[]{}()#+-!.\\", r):
			fmt.Fprintf(&b, "&#%d;", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
