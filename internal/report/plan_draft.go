package report

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// RenderPlanDraft is the only visible graph-Plan issue projection. The ordinal
// is reserved by the store; neither the parent nor sequence comes from a role.
func RenderPlanDraft(parent, ordinal int64, d planbrief.PlanDraft, stage string, scope planbrief.ContentScope) (string, string, error) {
	return renderPlanDraft(parent, ordinal, d, stage, scope, false)
}

// RenderRetainedPlanDraft preserves settled later sections during earlier-stage review.
// The caller separately validates stage-local authorship before merging.
func RenderRetainedPlanDraft(parent, ordinal int64, d planbrief.PlanDraft, stage string, scope planbrief.ContentScope) (string, string, error) {
	return renderPlanDraft(parent, ordinal, d, stage, scope, true)
}

func renderPlanDraft(parent, ordinal int64, d planbrief.PlanDraft, stage string, scope planbrief.ContentScope, retained bool) (string, string, error) {
	if parent < 1 || ordinal < 1 {
		return "", "", fmt.Errorf("positive authorized parent and reserved ordinal required")
	}
	var validation error
	if retained {
		validation = d.ValidateRetained(stage, scope)
	} else {
		validation = d.Validate(stage, scope)
	}
	if err := validation; err != nil {
		return "", "", err
	}
	title := fmt.Sprintf("%d-%03d: %s", parent, ordinal, d.Name)
	if len(title) > 256 {
		return "", "", fmt.Errorf("generated title exceeds 256 bytes")
	}
	var b strings.Builder
	b.WriteString("# Userstory\n\n")
	fmt.Fprintf(&b, "As %s, I want %s, so that %s.\n", storyText(d.UserStory.Actor), storyText(d.UserStory.Want), storyText(d.UserStory.Benefit))
	for _, note := range d.UserStory.Notes {
		fmt.Fprintf(&b, "\n- %s\n", storyText(note))
	}
	fmt.Fprintf(&b, "\n## What this story solves\n\n%s\n\n## Scope\n\nIn-Scope\n\n", storyText(d.Problem))
	for _, v := range d.InScope {
		fmt.Fprintf(&b, "- %s\n", storyText(v))
	}
	b.WriteString("\nNot in Scope\n\n")
	for _, v := range d.NotInScope {
		fmt.Fprintf(&b, "- %s\n", storyText(v))
	}
	b.WriteString("\n# Task\n\n| Task Id | Description | Owner | Dependencies |\n|---|---|---|---|\n")
	for _, t := range d.Tasks {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", storyText(t.ID), storyText(t.Description), storyText(t.OwnerRole), storyList(t.DependsOn))
	}
	b.WriteString("\n" + planbrief.PlanAcceptanceHeading + "\n\n")
	for _, c := range d.Criteria {
		fmt.Fprintf(&b, "- [ ] %s: %s\n", c.ID, storyText(c.Text))
	}
	b.WriteString("\n# Architecture\n")
	if a := d.Architecture; a != nil {
		b.WriteString("\n## Architecture - General\n\n| Area | Decision | Rationale |\n|---|---|---|\n")
		for _, r := range a.General {
			planRow(&b, r.Area, r.Decision, r.Rationale)
		}
		if len(a.DataModel) > 0 {
			b.WriteString("\n## Data Model\n\n| Entity / Relationship | Definition | Constraints / Rationale |\n|---|---|---|\n")
			for _, r := range a.DataModel {
				planRow(&b, r.Entity, r.Definition, r.Constraints)
			}
		}
		if len(a.ServiceDesign) > 0 {
			b.WriteString("\n## Service Design\n\n| Layer / Component | Responsibility | Dependencies / Access |\n|---|---|---|\n")
			for _, r := range a.ServiceDesign {
				planRow(&b, r.Component, r.Responsibility, r.Access)
			}
		}
		if len(a.SolutionDesign) > 0 {
			b.WriteString("\n## Solution Design\n\n| Element | Collaboration / Contract | Rationale |\n|---|---|---|\n")
			for _, r := range a.SolutionDesign {
				planRow(&b, r.Element, r.Contract, r.Rationale)
			}
		}
	}
	if d.Frontend && len(d.Design) > 0 {
		b.WriteString("\n# Design\n\n")
		for _, v := range d.Design {
			fmt.Fprintf(&b, "- %s\n", storyText(v))
		}
	}
	// Hidden semantic metadata preserves links and proof obligations without adding
	// competing visible headings or delegating body generation to a role.
	raw, err := json.Marshal(d)
	if err != nil {
		return "", "", err
	}
	fmt.Fprintf(&b, "\n<!-- lf:plan-content v=1 data=%s -->\n", string(raw))
	if b.Len() > planbrief.MaxStoryBodyBytes {
		return "", "", fmt.Errorf("rendered Plan draft exceeds byte budget")
	}
	return title, b.String(), nil
}

func planRow(b *strings.Builder, a, c, d string) {
	fmt.Fprintf(b, "| %s | %s | %s |\n", storyText(a), storyText(c), storyText(d))
}
