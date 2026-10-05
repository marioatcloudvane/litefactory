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
	body := typedVisible(s, -1)
	if len(body) > planbrief.MaxStoryBodyBytes {
		return "", fmt.Errorf("story %s: rendered body exceeds %d bytes", s.Key, planbrief.MaxStoryBodyBytes)
	}
	return body, nil
}

// RenderTypedStoryParts renders like RenderTypedStory but never refuses on size:
// a body over the limit is shortened (hidden shortened marker) and the full text
// is returned as ordered parts split at line breaks.
func RenderTypedStoryParts(s planbrief.TypedStory, scope planbrief.ContentScope) (string, []string, error) {
	if err := s.Validate(scope); err != nil {
		return "", nil, err
	}
	full := typedVisible(s, -1)
	limit := planbrief.MaxStoryBodyBytes - planDraftMarkerReserve
	if len(full) <= limit {
		return full, nil, nil
	}
	short := ""
	for _, cut := range []int{400, 200, 100, 50, 20, 0} {
		short = typedVisible(s, cut) + "\n> Draft shortened: the full draft is in the numbered part comments on this issue. Reply with changes instead of editing this text.\n\n" + planbrief.PlanShortenedMarker + "\n"
		if len(short) <= limit {
			break
		}
	}
	if len(short) > limit {
		if i := strings.LastIndex(short[:limit], "\n"); i > 0 {
			short = short[:i+1]
		}
		short += planbrief.PlanShortenedMarker + "\n"
	}
	return short, SplitStoryParts(full, PlanPartLimit), nil
}

// typedVisible renders the story; cut < 0 keeps every cell whole.
func typedVisible(s planbrief.TypedStory, cut int) string {
	q := func(v string) string { return storyText(cutText(v, cut)) }
	var b strings.Builder
	b.WriteString("## User Story\n\n")
	fmt.Fprintf(&b, "As %s, I want %s, so that %s.\n", q(s.UserStory.Actor), q(s.UserStory.Want), q(s.UserStory.Benefit))
	for _, note := range s.UserStory.Notes {
		fmt.Fprintf(&b, "\n- %s\n", q(note))
	}
	b.WriteString("\n## Acceptance Criteria\n\n")
	for _, c := range s.AcceptanceCriteria {
		fmt.Fprintf(&b, "- [ ] %s: %s\n", c.ID, q(c.Text))
	}
	b.WriteString("\n## Tasks\n\n| ID | Description | Owner role | Criterion IDs | Depends on |\n|---|---|---|---|---|\n")
	for _, t := range s.Tasks {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", storyText(t.ID), q(t.Description), q(t.OwnerRole), storyList(t.CriterionIDs), storyList(t.DependsOn))
	}
	b.WriteString("\n## Architecture\n\n")
	fmt.Fprintf(&b, "%s\n", q(s.Architecture.Summary))
	b.WriteString("\n### Components\n\n| Name | Responsibility |\n|---|---|\n")
	for _, c := range s.Architecture.Components {
		fmt.Fprintf(&b, "| %s | %s |\n", q(c.Name), q(c.Responsibility))
	}
	b.WriteString("\n### Interfaces\n\n| Name | Input | Output |\n|---|---|---|\n")
	for _, v := range s.Architecture.Interfaces {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", q(v.Name), q(v.Input), q(v.Output))
	}
	b.WriteString("\n### Constraints\n\n")
	if len(s.Architecture.Constraints) == 0 {
		b.WriteString("None.\n")
	}
	for _, c := range s.Architecture.Constraints {
		fmt.Fprintf(&b, "- %s\n", q(c))
	}
	b.WriteString("\n### Decisions\n\n| Decision | Reason |\n|---|---|\n")
	for _, d := range s.Architecture.Decisions {
		fmt.Fprintf(&b, "| %s | %s |\n", q(d.Decision), q(d.Reason))
	}
	b.WriteString("\n## Dependencies\n\n")
	fmt.Fprintf(&b, "Story keys: %s\n\nIssue references: %s\n", storyList(s.Dependencies.StoryKeys), storyList(s.Dependencies.IssueRefs))
	fmt.Fprintf(&b, "\n## Story Guidance\n\n%s\n", planbrief.StoryGuidance)
	return b.String()
}

// RenderTypedPlan validates the full graph and the escaped rendered transport
// budget. A story over the body limit is returned shortened (see
// RenderTypedStoryParts); publication posts the full text as part comments. Returned bodies correspond exactly to the input story order.
// A caller adding envelope metadata must check its final serialized transport too.
func RenderTypedPlan(p planbrief.TypedPlan, scope planbrief.ContentScope) ([]string, error) {
	if err := p.Validate(scope); err != nil {
		return nil, err
	}
	bodies := make([]string, len(p.Stories))
	var faults []error
	for i, s := range p.Stories {
		body, _, err := RenderTypedStoryParts(s, scope)
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
