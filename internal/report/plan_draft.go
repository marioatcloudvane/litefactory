package report

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// planDraftMarkerReserve keeps room for the story and request markers the story
// activity prepends, so a draft that renders within budget also publishes.
const planDraftMarkerReserve = 512

// PlanPartLimit bounds one full-draft part; the activity adds a short marker
// line, so a part plus marker stays within the comment limit.
const PlanPartLimit = 60000

// PlanDraftRender is a story issue projection. Parts is empty when the full body
// fits; otherwise Body is shortened and Parts hold the full visible draft, split
// at line breaks, to be posted as numbered comments.
type PlanDraftRender struct {
	Title string
	Body  string
	Parts []string
}

// RenderPlanDraft is the only visible graph-Plan issue projection. The ordinal
// is reserved by the store; neither the parent nor sequence comes from a role.
// A draft too long to fit yields the shortened body; use RenderPlanDraftParts
// to also obtain the full-draft parts.
func RenderPlanDraft(parent, ordinal int64, d planbrief.PlanDraft, stage string, scope planbrief.ContentScope, parentKey ...string) (string, string, error) {
	r, err := renderPlanDraft(parent, ordinal, d, stage, scope, false, parentKey...)
	return r.Title, r.Body, err
}

// RenderRetainedPlanDraft preserves settled later sections during earlier-stage review.
// The caller separately validates stage-local authorship before merging.
func RenderRetainedPlanDraft(parent, ordinal int64, d planbrief.PlanDraft, stage string, scope planbrief.ContentScope, parentKey ...string) (string, string, error) {
	r, err := renderPlanDraft(parent, ordinal, d, stage, scope, true, parentKey...)
	return r.Title, r.Body, err
}

// RenderPlanDraftParts is RenderPlanDraft returning body and full-draft parts.
func RenderPlanDraftParts(parent, ordinal int64, d planbrief.PlanDraft, stage string, scope planbrief.ContentScope, parentKey ...string) (PlanDraftRender, error) {
	return renderPlanDraft(parent, ordinal, d, stage, scope, false, parentKey...)
}

// RenderRetainedPlanDraftParts is RenderRetainedPlanDraft returning body and parts.
func RenderRetainedPlanDraftParts(parent, ordinal int64, d planbrief.PlanDraft, stage string, scope planbrief.ContentScope, parentKey ...string) (PlanDraftRender, error) {
	return renderPlanDraft(parent, ordinal, d, stage, scope, true, parentKey...)
}

// SplitStoryParts splits text at line breaks into parts of at most limit bytes.
// A single line longer than the limit is cut at a rune boundary.
func SplitStoryParts(text string, limit int) []string {
	var parts []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		for len(line) > limit {
			cut := limit
			for cut > 0 && !utf8.RuneStart(line[cut]) {
				cut--
			}
			flush()
			parts = append(parts, line[:cut])
			line = line[cut:]
		}
		if cur.Len()+len(line) > limit {
			flush()
		}
		cur.WriteString(line)
	}
	flush()
	return parts
}

// cutText shortens a raw cell to n runes, marking the cut.
func cutText(v string, n int) string {
	if n < 0 || utf8.RuneCountInString(v) <= n {
		return v
	}
	return string([]rune(v)[:n]) + "\u2026"
}

func renderPlanDraft(parent, ordinal int64, d planbrief.PlanDraft, stage string, scope planbrief.ContentScope, retained bool, parentKey ...string) (PlanDraftRender, error) {
	if parent < 1 || ordinal < 1 {
		return PlanDraftRender{}, fmt.Errorf("positive authorized parent and reserved ordinal required")
	}
	var validation error
	if retained {
		validation = d.ValidateRetained(stage, scope)
	} else {
		validation = d.Validate(stage, scope)
	}
	if err := validation; err != nil {
		return PlanDraftRender{}, err
	}
	key := fmt.Sprint(parent)
	if len(parentKey) > 0 {
		key = parentKey[0]
	}
	title := fmt.Sprintf("%s-%03d: %s", key, ordinal, d.Name)
	if len(title) > 256 {
		return PlanDraftRender{}, fmt.Errorf("generated title exceeds 256 bytes")
	}
	full := planVisible(d, -1)
	raw, err := json.Marshal(d)
	if err != nil {
		return PlanDraftRender{}, err
	}
	// Hidden semantic metadata preserves links and proof obligations without adding
	// competing visible headings or delegating body generation to a role.
	body := fmt.Sprintf("%s\n<!-- lf:plan-content v=1 data=%s -->\n", full, raw)
	limit := planbrief.MaxStoryBodyBytes - planDraftMarkerReserve
	if len(body) <= limit {
		return PlanDraftRender{Title: title, Body: body}, nil
	}
	// Too long: the body shrinks until it fits and the full visible draft moves to
	// numbered comments. Size never refuses a draft.
	short := ""
	for _, cut := range []int{400, 200, 100, 50, 20, 0} {
		short = shortPlanBody(d, cut)
		if len(short) <= limit {
			break
		}
	}
	if len(short) > limit {
		short = shortPlanBody(d, 0)
		if i := strings.LastIndex(short[:limit], "\n"); i > 0 {
			short = short[:i+1]
		}
		short += planbrief.PlanShortenedMarker + "\n"
	}
	return PlanDraftRender{Title: title, Body: short, Parts: SplitStoryParts(full, PlanPartLimit)}, nil
}

// shortPlanBody keeps identity, scope and every heading with cells cut to n runes.
func shortPlanBody(d planbrief.PlanDraft, cut int) string {
	return planVisible(d, cut) + "\n> Draft shortened: the full draft is in the numbered part comments on this issue. Reply with changes instead of editing this text.\n\n" + planbrief.PlanShortenedMarker + "\n"
}

// planVisible renders the visible scaffold; cut < 0 keeps every cell whole.
func planVisible(d planbrief.PlanDraft, cut int) string {
	q := func(v string) string { return storyText(cutText(v, cut)) }
	var b strings.Builder
	b.WriteString("# Userstory\n\n")
	fmt.Fprintf(&b, "As %s, I want %s, so that %s.\n", q(d.UserStory.Actor), q(d.UserStory.Want), q(d.UserStory.Benefit))
	for _, note := range d.UserStory.Notes {
		fmt.Fprintf(&b, "\n- %s\n", q(note))
	}
	fmt.Fprintf(&b, "\n## What this story solves\n\n%s\n\n## Scope\n\nIn-Scope\n\n", q(d.Problem))
	for _, v := range d.InScope {
		fmt.Fprintf(&b, "- %s\n", q(v))
	}
	b.WriteString("\nNot in Scope\n\n")
	for _, v := range d.NotInScope {
		fmt.Fprintf(&b, "- %s\n", q(v))
	}
	b.WriteString("\n# Task\n\n| Task Id | Description | Owner | Dependencies |\n|---|---|---|---|\n")
	for _, t := range d.Tasks {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", storyText(t.ID), q(t.Description), q(t.OwnerRole), storyList(t.DependsOn))
	}
	b.WriteString("\n" + planbrief.PlanAcceptanceHeading + "\n\n")
	for _, c := range d.Criteria {
		fmt.Fprintf(&b, "- [ ] %s: %s\n", c.ID, q(c.Text))
	}
	b.WriteString("\n# Architecture\n")
	if a := d.Architecture; a != nil {
		b.WriteString("\n## Architecture - General\n\n| Area | Decision | Rationale |\n|---|---|---|\n")
		for _, r := range a.General {
			planRow(&b, q, r.Area, r.Decision, r.Rationale)
		}
		if len(a.DataModel) > 0 {
			b.WriteString("\n## Data Model\n\n| Entity / Relationship | Definition | Constraints / Rationale |\n|---|---|---|\n")
			for _, r := range a.DataModel {
				planRow(&b, q, r.Entity, r.Definition, r.Constraints)
			}
		}
		if len(a.ServiceDesign) > 0 {
			b.WriteString("\n## Service Design\n\n| Layer / Component | Responsibility | Dependencies / Access |\n|---|---|---|\n")
			for _, r := range a.ServiceDesign {
				planRow(&b, q, r.Component, r.Responsibility, r.Access)
			}
		}
		if len(a.SolutionDesign) > 0 {
			b.WriteString("\n## Solution Design\n\n| Element | Collaboration / Contract | Rationale |\n|---|---|---|\n")
			for _, r := range a.SolutionDesign {
				planRow(&b, q, r.Element, r.Contract, r.Rationale)
			}
		}
	}
	if d.Frontend && len(d.Design) > 0 {
		b.WriteString("\n# Design\n\n")
		for _, v := range d.Design {
			fmt.Fprintf(&b, "- %s\n", q(v))
		}
	}
	return b.String()
}

func planRow(b *strings.Builder, t func(string) string, a, c, d string) {
	fmt.Fprintf(b, "| %s | %s | %s |\n", t(a), t(c), t(d))
}
