package turn

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear/linearmd"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

const (
	// implementHandoffLimit is the most the hand-off comment may hold.
	implementHandoffLimit = 64 << 10
	// implementHandoffBudget leaves room under the limit for redaction.
	implementHandoffBudget = implementHandoffLimit - 4<<10
)

// ImplementHandoffActivity writes the one hand-off report of a parent: what
// merged, what is parked and why, and what is still waiting. It reads the
// ledger and changes nothing but its own comment, so it is safe at any time.
type ImplementHandoffActivity struct{ b ImplementBindings }

type implementHandoffOutput struct {
	Merged    int    `json:"merged"`
	Parked    int    `json:"parked"`
	Waiting   int    `json:"waiting"`
	CommentID int64  `json:"comment_id"`
	URL       string `json:"url,omitempty"`
}

// Execute posts the report once and edits that comment in place afterwards.
func (h ImplementHandoffActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	out, _, err := refreshImplementHandoff(ctx, h.b, opts, req)
	if err != nil {
		return ActivityResult{}, err
	}
	return implementResult("posted", out)
}

// refreshImplementHandoff always rebuilds from fresh authority under the status
// lock. No durable comment identity or delivery state is needed on restart.
func refreshImplementHandoff(ctx context.Context, b ImplementBindings, opts Options, req ActivityRequest) (out implementHandoffOutput, changed bool, err error) {
	if b.StatusBind != nil {
		b.Bind = b.StatusBind
	}
	parent, err := b.Bind(ctx, opts, req)
	if err != nil {
		return implementHandoffOutput{}, false, err
	}
	unlock, err := b.DB.LockImplementStatus(ctx, parent.Parent.String(), b.LockDir)
	if err != nil {
		return implementHandoffOutput{}, false, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	a, err := b.bind(ctx, opts, req)
	if err != nil {
		return implementHandoffOutput{}, false, err
	}
	rows, err := b.DB.ListImplementStories(ctx, a.Parent.String(), a.Generation)
	if err != nil {
		return implementHandoffOutput{}, false, err
	}
	readProgress := b.ProgressRead
	if readProgress == nil {
		readProgress = b.DB.LatestImplementProgress
	}
	progress, progressErr := readProgress(ctx, a.Parent.String(), a.Generation)
	progress = slices.Clone(progress)
	if progressErr != nil {
		progress = nil
	}
	// Redact before clipping or escaping, so a secret crossing the clip boundary
	// cannot escape detection through truncation or Markdown encoding.
	for i := range progress {
		safe, redactErr := b.Redactor.Apply(ctx, progress[i].Message, implementHandoffLimit)
		if redactErr != nil {
			progress = nil
			break
		}
		progress[i].Message = safe.String()
	}
	// Redact every untrusted cell before any clipping or encoding.
	rows = slices.Clone(rows)
	a.Stories = slices.Clone(a.Stories)
	for i := range rows {
		safe, err := b.Redactor.Apply(ctx, rows[i].Reason, implementHandoffLimit)
		if err != nil {
			return out, false, err
		}
		rows[i].Reason = safe.String()
	}
	for i := range a.Stories {
		safe, err := b.Redactor.Apply(ctx, a.Stories[i].Name, implementHandoffLimit)
		if err != nil {
			return out, false, err
		}
		a.Stories[i].Name = safe.String()
	}
	notes, err := implementParkNotes(ctx, b.DB, a, rows)
	if err != nil {
		return out, false, err
	}
	displays := map[string]implementDisplay{}
	for _, story := range a.Stories {
		display, err := lookupTicketDisplay(ctx, b.Forge, a.Parent, story.IssueNumber)
		if err != nil {
			return out, false, err
		}
		d := implementDisplay{Reference: display.Reference()}
		if note, ok := notes[story.Key]; ok {
			_, d.ReplyURL, err = reportSourceLinks(ctx, b.Forge, a.Parent, story.IssueNumber, note.CommentID)
			if err != nil {
				return out, false, err
			}
		}
		displays[story.Key] = d
	}
	text, counts := renderImplementHandoffNotes(a, rows, progress, notes, displays)
	if len(text) > implementHandoffBudget {
		return out, false, errors.New("hand-off report exceeds its size limit")
	}
	text += implementValidateNextStep(opts.ValidateAutoStart)
	if req.InstanceID != 0 {
		caveat, err := graphPreparationCaveat(ctx, b.DB, req.InstanceID)
		if err != nil {
			return out, false, err
		}
		if caveat != "" {
			text += "\n\n" + caveat
		}
	}
	body, err := b.Redactor.Apply(ctx, text, implementHandoffLimit)
	if err != nil {
		return implementHandoffOutput{}, false, fmt.Errorf("render hand-off: %w", err)
	}
	if body.Truncated() || body.Len() > implementHandoffLimit {
		return implementHandoffOutput{}, false, errors.New("hand-off report exceeds its size limit")
	}
	repo := github.RepoFromRef(a.Parent)
	marker := report.ImplementHandoffMarker(a.Parent)
	var existing *github.Comment
scan:
	for page := 1; page > 0 && page <= implementCommentPages; {
		got, err := b.Forge.ListCommentsPage(ctx, repo, a.Parent.Issue, page, implementCommentLimit)
		if err != nil {
			return implementHandoffOutput{}, false, fmt.Errorf("look for hand-off report: %w", err)
		}
		for i := range got.Comments {
			if strings.HasPrefix(got.Comments[i].Body, marker) {
				existing = &got.Comments[i]
				break scan
			}
		}
		page = got.NextPage
	}
	out = implementHandoffOutput{Merged: counts.merged, Parked: counts.parked, Waiting: counts.waiting}
	changed = true
	switch {
	case existing == nil:
		posted, err := postTicketNotice(ctx, b.Forge, marker, a.Parent.String(), 0, repo, a.Parent.Issue, body)
		if err != nil {
			return implementHandoffOutput{}, false, fmt.Errorf("post hand-off report: %w", err)
		}
		out.CommentID, out.URL = posted.ID, posted.HTMLURL
	case existing.Body == body.String():
		changed = false
		out.CommentID, out.URL = existing.ID, existing.HTMLURL
	default:
		if err := b.Forge.EditComment(ctx, repo, existing.ID, body); err != nil {
			return implementHandoffOutput{}, false, fmt.Errorf("edit hand-off report: %w", err)
		}
		out.CommentID, out.URL = existing.ID, existing.HTMLURL
	}
	return out, changed, nil
}

// ValidateStartCommand is the exact command that starts Validate by hand.
const ValidateStartCommand = "/validate Validate the finished work."

// implementValidateNextStep is the one line that tells the owner how Validate
// starts once every story is merged.
func implementValidateNextStep(autoStart bool) string {
	if autoStart {
		return "**Next step:** once every story is merged, Validate will start automatically.\n"
	}
	return "**Next step:** once every story is merged, start Validate by commenting `" + ValidateStartCommand + "`\n"
}

type implementHandoffCounts struct{ merged, parked, waiting int }

// renderImplementHandoffProgress preserves every approved story in plan order.
func renderImplementHandoffProgress(a ImplementAuthority, rows []store.ImplementStory, progress []store.ImplementProgressReport) (string, implementHandoffCounts) {
	return renderImplementHandoffNotes(a, rows, progress, nil)
}

func renderImplementHandoffNotes(a ImplementAuthority, rows []store.ImplementStory, progress []store.ImplementProgressReport, notes map[string]store.ReportDelivery, displays ...map[string]implementDisplay) (string, implementHandoffCounts) {
	state := map[string]store.ImplementStory{}
	for _, row := range rows {
		state[row.Key] = row
	}
	dependencies := maps.Clone(state)
	for _, story := range a.Stories {
		if _, exists := dependencies[story.Key]; story.AlreadyComplete && !exists {
			dependencies[story.Key] = store.ImplementStory{Key: story.Key, State: store.ImplementMerged}
		}
	}
	type tableRow struct{ prefix, info, suffix string }
	var table []tableRow
	var counts implementHandoffCounts
	onLinear := a.Parent.Forge == "linear"
	header := report.ImplementHandoffMarker(a.Parent) + "\n\n**Implementation hand-off**\n\n| Story | State | Information |\n|---|---|---|\n"
	// Reserve the longer next-step variant before assigning summary space.
	fixed := len(header) + max(len(implementValidateNextStep(false)), len(implementValidateNextStep(true))) + 1
	for _, story := range a.Stories {
		row, ok := state[story.Key]
		if !ok {
			row = store.ImplementStory{Key: story.Key, State: store.ImplementWaiting}
		}
		label := implementInline(onLinear, story.Key) + " " + safeImplementProgressFor(onLinear, story.Name)
		if story.IssueNumber > 0 {
			reference := fmt.Sprintf("#%d", story.IssueNumber)
			if a.Parent.Forge == "linear" {
				reference = "this issue"
			}
			if len(displays) > 0 {
				reference = displays[0][story.Key].Reference
			}
			label += " (" + reference + ")"
		}
		display, info := "Waiting", waitingSentence(story, row, dependencies)
		suffix := ""
		switch {
		case row.State == store.ImplementMerged || story.AlreadyComplete && !ok:
			display = "Finished"
			info = "already complete before this run."
			if ok {
				info = "merged as commit " + row.MergeCommit + "."
			}
			counts.merged++
		case row.State == store.ImplementParked:
			display, info = "Parked", row.Reason
			if row.ParkOrigin == store.ImplementOriginWorker && row.Attempts >= store.MaxImplementAttempts {
				display = "Failed"
			}
			if note, exists := notes[story.Key]; exists {
				if display != "Failed" && strings.HasPrefix(note.Body, "**Story parked**\n\nWorker response: blocked\n") {
					display = "Blocked"
				}
				if a.Parent.Forge != "linear" {
					suffix = fmt.Sprintf(" [Full answer](https://github.com/%s/%s/issues/%d#issuecomment-%d)", a.Parent.Owner, a.Parent.Repo, story.IssueNumber, note.CommentID)
				}
				if len(displays) > 0 && displays[0][story.Key].ReplyURL != "" {
					suffix = " [Full answer](" + displays[0][story.Key].ReplyURL + ")"
				}
			}
			counts.parked++
		default:
			counts.waiting++
			switch row.State {
			case store.ImplementDone:
				display = "Merging"
			case store.ImplementRunning:
				display = "Implementing"
				if latest := latestImplementActivity(a, row, progress); latest != nil {
					if latest.Kind == "testing" {
						display = "Testing"
					}
					// Metadata is fixed overhead; only the worker's summary is budgeted.
					suffix = fmt.Sprintf(" Latest activity (advisory, not completion): %s at %s, attempt %d.", latest.Kind, latest.RecordedAt.UTC().Format(time.RFC3339), row.Attempts+1)
					info += " " + latest.Message
				}
			default:
				for _, dep := range story.Dependencies {
					if dependencies[dep].State == store.ImplementParked {
						display = "Blocked"
					}
				}
			}
		}
		if !story.AlreadyComplete || ok {
			suffix += implementDecisionGuidance(a, story, row, dependencies, displays...)
		}
		tr := tableRow{prefix: "| " + label + " | " + display + " | ", info: info, suffix: suffix + " |\n"}
		fixed += len(tr.prefix) + len(tr.suffix)
		table = append(table, tr)
	}
	var out strings.Builder
	out.WriteString(header)
	share := max(0, implementHandoffBudget-fixed) / max(1, len(table))
	for _, row := range table {
		out.WriteString(row.prefix)
		out.WriteString(budgetImplementTextFor(onLinear, row.info, min(1800, share)))
		out.WriteString(row.suffix)
	}
	out.WriteString("\n")
	// If minimal rows exceed the budget, return them intact. The publication
	// boundary rejects the oversized body; it never posts a partial table.
	return out.String(), counts
}

func latestImplementActivity(a ImplementAuthority, row store.ImplementStory, progress []store.ImplementProgressReport) *store.ImplementProgressReport {
	var latest *store.ImplementProgressReport
	for i := range progress {
		p := &progress[i]
		if p.Kind != "implementation" && p.Kind != "testing" && p.Kind != "repair" {
			continue
		}
		if p.StoryKey == row.Key && p.StoryAttempts == row.Attempts && p.Generation == a.Generation && p.Parent == a.Parent.String() && !p.RecordedAt.Before(row.StateAt) && (latest == nil || p.RecordedAt.After(latest.RecordedAt) || p.RecordedAt.Equal(latest.RecordedAt) && p.ID > latest.ID) {
			latest = p
		}
	}
	return latest
}

// budgetImplementTextFor clips the escaped cell text to budget bytes. Linear
// clips between whole escape units, so no escape or code span is cut.
func budgetImplementTextFor(onLinear bool, text string, budget int) string {
	if !onLinear {
		return budgetImplementText(text, budget)
	}
	var out strings.Builder
	for _, next := range linearmd.InlineUnits(text) {
		if out.Len()+len(next) > budget-3 {
			if budget >= 3 {
				out.WriteString("...")
			}
			break
		}
		out.WriteString(next)
	}
	return out.String()
}

func budgetImplementText(text string, budget int) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	var out strings.Builder
	for _, r := range text {
		next := escapeImplementText(string(r))
		if out.Len()+len(next) > budget-3 {
			if budget >= 3 {
				out.WriteString("...")
			}
			break
		}
		out.WriteString(next)
	}
	return out.String()
}

// waitingSentence says what an unfinished story is waiting for.
func waitingSentence(story ImplementStory, row store.ImplementStory, state map[string]store.ImplementStory) string {
	switch row.State {
	case store.ImplementRunning:
		return "being built now."
	case store.ImplementDone:
		return "built, waiting to be merged."
	}
	var blockers []string
	for _, dep := range story.Dependencies {
		d, ok := state[dep]
		if ok && d.State == store.ImplementMerged {
			continue
		}
		switch {
		case !ok:
			blockers = append(blockers, fmt.Sprintf("%s (unknown story)", dep))
		case d.State == store.ImplementParked:
			blockers = append(blockers, fmt.Sprintf("%s (set aside, so this story cannot start)", dep))
		default:
			blockers = append(blockers, dep)
		}
	}
	if len(blockers) == 0 {
		return "ready to start."
	}
	return "waiting for " + strings.Join(blockers, ", ") + " to be merged."
}

// implementDecisionGuidance keeps actions and their effects outside the clipped
// worker summary. A long reason must not hide the route to its actual decision.
func implementDecisionGuidance(a ImplementAuthority, story ImplementStory, row store.ImplementStory, state map[string]store.ImplementStory, displays ...map[string]implementDisplay) string {
	onLinear := a.Parent.Forge == "linear"
	decision := func(key string) string {
		if len(displays) > 0 && displays[0][key].ReplyURL != "" {
			return "[blocking decision for " + implementInline(onLinear, key) + "](" + displays[0][key].ReplyURL + ")"
		}
		return "blocking decision for " + implementInline(onLinear, key) + " (confirmed report destination unavailable)"
	}
	if row.State == store.ImplementParked {
		var dependents []string
		for _, candidate := range a.Stories {
			if state[candidate.Key].State == store.ImplementMerged || candidate.AlreadyComplete {
				continue
			}
			if slices.Contains(candidate.Dependencies, story.Key) {
				dependents = append(dependents, implementInline(onLinear, candidate.Key))
			}
		}
		text := " Read the " + decision(story.Key) + "; reply on the parent issue naming " + implementInline(onLinear, story.Key) + " with the decision or changed guidance it needs. The existing restart rules apply; the reply does not approve or merge the story."
		if len(dependents) > 0 {
			text += " Must merge before " + strings.Join(dependents, ", ") + " can be admitted."
		}
		return text
	}
	if row.State == store.ImplementDone {
		return " The coordinator attempts the merge automatically; no owner decision is requested unless it parks."
	}
	if row.State == store.ImplementRunning || row.State == store.ImplementMerged {
		return ""
	}
	var steps []string
	for _, dep := range story.Dependencies {
		d, ok := state[dep]
		if ok && d.State == store.ImplementMerged {
			continue
		}
		switch {
		case !ok:
			steps = append(steps, implementInline(onLinear, dep)+": retained dependency state unavailable; no decision can be inferred")
		case d.State == store.ImplementParked:
			steps = append(steps, "first resolve the "+decision(dep)+" by replying on the parent with that story key and the requested decision or guidance")
		case d.State == store.ImplementRunning:
			steps = append(steps, implementInline(onLinear, dep)+" is running; wait for its result")
		case d.State == store.ImplementDone:
			steps = append(steps, implementInline(onLinear, dep)+" is queued for automatic merge")
		default:
			steps = append(steps, implementInline(onLinear, dep)+" is queued and must finish and merge first")
		}
	}
	if len(steps) == 0 {
		return " Existing admission will start it when capacity and prerequisites permit; no owner reply is needed."
	}
	return " " + strings.Join(steps, "; ") + ". After every prerequisite merges, existing admission can reconsider " + implementInline(onLinear, story.Key) + "; a reply alone does not start it."
}

// safeImplementProgress keeps worker text inert even when it contains Markdown,
// autolinks, mentions or the hidden hand-off marker.
func safeImplementProgress(text string) string {
	return safeImplementProgressFor(false, text)
}

// safeImplementProgressFor writes Linear text with backslash escapes and code
// spans, because Linear comments show numeric entities literally.
func safeImplementProgressFor(onLinear bool, text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 300 {
		text = string(runes[:300]) + "..."
	}
	if onLinear {
		return linearmd.InlineText(text)
	}
	var out strings.Builder
	for _, r := range text {
		if strings.ContainsRune("&<>@#[]()!*_`~\\:./=-+|", r) {
			fmt.Fprintf(&out, "&#%d;", r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

type implementDisplay struct{ Reference, ReplyURL string }
