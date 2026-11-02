package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
)

// planDraftMarkerReserve keeps room for the story and request markers the story
// activity prepends, so a draft that renders within budget also publishes.
const planDraftMarkerReserve = 512

// PlanPartLimit bounds one full-draft part; the activity adds a short marker
// line, so a part plus marker stays within the comment limit.
const PlanPartLimit = 60000

// planBodyLimit is the largest rendered body that still publishes once the
// activity adds its markers: the redactor first cuts every body to
// redact.Budget of the story body limit.
var planBodyLimit = redact.Budget(planbrief.MaxStoryBodyBytes) - planDraftMarkerReserve

// planCuts are the cell lengths a too-long body is shortened to, in order.
var planCuts = []int{400, 200, 100, 50, 20, 0}

// planShortenedNotice ends every shortened body: the visible pointer to the
// part comments and the hidden shortened marker.
const planShortenedNotice = "\n> Draft shortened: the full draft is in the numbered part comments on this issue. Reply with changes instead of editing this text.\n\n" + planbrief.PlanShortenedMarker + "\n"

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

// planDraftSource is the validated, unfitted text of one Plan story.
type planDraftSource struct {
	title string
	full  string // the full visible draft
	body  string // full plus the hidden plan-content line
}

func newPlanDraftSource(parent, ordinal int64, d planbrief.PlanDraft, stage string, scope planbrief.ContentScope, retained bool, parentKey ...string) (planDraftSource, error) {
	if parent < 1 || ordinal < 1 {
		return planDraftSource{}, fmt.Errorf("positive authorized parent and reserved ordinal required")
	}
	var validation error
	if retained {
		validation = d.ValidateRetained(stage, scope)
	} else {
		validation = d.Validate(stage, scope)
	}
	if err := validation; err != nil {
		return planDraftSource{}, err
	}
	key := fmt.Sprint(parent)
	if len(parentKey) > 0 {
		key = parentKey[0]
	}
	title := fmt.Sprintf("%s-%03d: %s", key, ordinal, d.Name)
	if len(title) > 256 {
		return planDraftSource{}, fmt.Errorf("generated title exceeds 256 bytes")
	}
	full := planVisible(d, -1)
	raw, err := json.Marshal(d)
	if err != nil {
		return planDraftSource{}, err
	}
	// Hidden semantic metadata preserves links and proof obligations without adding
	// competing visible headings or delegating body generation to a role.
	body := fmt.Sprintf("%s\n<!-- lf:plan-content v=1 data=%s -->\n", full, raw)
	return planDraftSource{title: title, full: full, body: body}, nil
}

func renderPlanDraft(parent, ordinal int64, d planbrief.PlanDraft, stage string, scope planbrief.ContentScope, retained bool, parentKey ...string) (PlanDraftRender, error) {
	src, err := newPlanDraftSource(parent, ordinal, d, stage, scope, retained, parentKey...)
	if err != nil {
		return PlanDraftRender{}, err
	}
	limit := planBodyLimit
	if len(src.body) <= limit {
		return PlanDraftRender{Title: src.title, Body: src.body}, nil
	}
	// Too long: the body shrinks until it fits and the full visible draft moves to
	// numbered comments. Size never refuses a draft.
	short := ""
	for _, cut := range planCuts {
		short = shortPlanBody(d, cut)
		if len(short) <= limit {
			break
		}
	}
	if len(short) > limit {
		short = hardCutShort(planVisible(d, 0), limit)
	}
	return PlanDraftRender{Title: src.title, Body: short, Parts: SplitStoryParts(src.full, PlanPartLimit)}, nil
}

// hardCutShort cuts an outline at a line break so that it plus the shortened
// notice fits limit; the body still points to the part comments.
func hardCutShort(outline string, limit int) string {
	room := limit - len(planShortenedNotice)
	if room < 0 {
		room = 0
	}
	if len(outline) > room {
		cut := room
		if i := strings.LastIndex(outline[:room], "\n"); i > 0 {
			cut = i + 1
		}
		for cut > 0 && !utf8.RuneStart(outline[cut]) {
			cut--
		}
		outline = outline[:cut]
	}
	return outline + planShortenedNotice
}

// PlanStoryInput is what the Plan story activity publishes for one story.
type PlanStoryInput struct {
	Parent, Ordinal int64
	Draft           planbrief.PlanDraft
	Stage           string
	Scope           planbrief.ContentScope
	Retained        bool   // validate as a retained (non-create) draft
	ParentKey       string // display key of the parent in the title; empty uses Parent
	Prefix          string // hidden markers placed before the body
	PartHeader      func(n, total int) string
	CommentLimit    int // size limit of one part comment
	// Linear renders the part comments for a Linear parent; the issue body
	// stays as rendered for every destination.
	Linear bool
}

// PlanStoryText is the redacted publication text of one Plan story. Parts is
// empty when the full body fits.
type PlanStoryText struct {
	Title redact.Body
	Body  redact.Body
	Parts []redact.Body
}

// Errors PreparePlanStory returns when text cannot be published untruncated.
var (
	ErrPlanDraftCannotFit = errors.New("complete draft cannot be truncated")
	ErrPlanPartCannotFit  = errors.New("complete draft part cannot be truncated")
)

// PlanRenderError is a draft the renderer refuses; the request can never publish.
type PlanRenderError struct{ Err error }

func (e *PlanRenderError) Error() string { return e.Err.Error() }
func (e *PlanRenderError) Unwrap() error { return e.Err }

// PreparePlanStory renders, redacts and fits one Plan story for publication.
// Every candidate body is redacted completely before it is measured against
// the redactor budget, so redaction growth cannot cut it; the full visible
// draft is redacted completely before it is split into parts, so a credential
// is never split into exposed fragments. The result is deterministic for the
// same redactor policy.
func PreparePlanStory(ctx context.Context, red *redact.Redactor, in PlanStoryInput) (PlanStoryText, error) {
	var key []string
	if in.ParentKey != "" {
		key = []string{in.ParentKey}
	}
	src, err := newPlanDraftSource(in.Parent, in.Ordinal, in.Draft, in.Stage, in.Scope, in.Retained, key...)
	if err != nil {
		return PlanStoryText{}, &PlanRenderError{Err: err}
	}
	var out PlanStoryText
	if out.Title, err = red.Apply(ctx, src.title, 256); err != nil {
		return PlanStoryText{}, err
	}
	if out.Title.Truncated() {
		return PlanStoryText{}, ErrPlanDraftCannotFit
	}
	budget := redact.Budget(planbrief.MaxStoryBodyBytes)
	body, shortened := "", false
	for i := -1; i < len(planCuts) && body == ""; i++ {
		candidate := src.body
		if i >= 0 {
			candidate = shortPlanBody(in.Draft, planCuts[i])
		}
		v, prepErr := red.Prepare(ctx, in.Prefix+candidate)
		if prepErr != nil {
			return PlanStoryText{}, prepErr
		}
		if len(v.String()) <= budget {
			body, shortened = v.String(), i >= 0
		}
	}
	if body == "" {
		v, prepErr := red.Prepare(ctx, in.Prefix+planVisible(in.Draft, 0))
		if prepErr != nil {
			return PlanStoryText{}, prepErr
		}
		body, shortened = hardCutShort(v.String(), budget), true
	}
	// Already redacted and within budget, so this cannot cut; a cut is still
	// refused rather than published.
	if out.Body, err = red.Apply(ctx, body, planbrief.MaxStoryBodyBytes); err != nil {
		return PlanStoryText{}, err
	}
	if out.Body.Truncated() {
		return PlanStoryText{}, ErrPlanDraftCannotFit
	}
	if !shortened {
		return out, nil
	}
	visible := src.full
	if in.Linear {
		visible = planVisibleFor(in.Draft, -1, true)
	}
	full, err := red.Prepare(ctx, visible)
	if err != nil {
		return PlanStoryText{}, err
	}
	parts := SplitStoryParts(full.String(), PlanPartLimit)
	out.Parts = make([]redact.Body, len(parts))
	for i, part := range parts {
		if out.Parts[i], err = red.Apply(ctx, in.PartHeader(i+1, len(parts))+part, in.CommentLimit); err != nil {
			return PlanStoryText{}, err
		}
		if out.Parts[i].Truncated() {
			return PlanStoryText{}, ErrPlanPartCannotFit
		}
	}
	return out, nil
}

// shortPlanBody keeps identity, scope and every heading with cells cut to n runes.
func shortPlanBody(d planbrief.PlanDraft, cut int) string {
	return planVisible(d, cut) + planShortenedNotice
}

// planVisible renders the visible scaffold; cut < 0 keeps every cell whole.
func planVisible(d planbrief.PlanDraft, cut int) string {
	return planVisibleFor(d, cut, false)
}

// planVisibleFor renders the scaffold with the destination's text contract.
func planVisibleFor(d planbrief.PlanDraft, cut int, linear bool) string {
	q := func(v string) string { return storyTextFor(cutText(v, cut), linear) }
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
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", storyTextFor(t.ID, linear), q(t.Description), q(t.OwnerRole), storyListFor(t.DependsOn, linear))
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
