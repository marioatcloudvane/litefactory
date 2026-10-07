package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

const (
	// implementHandoffLimit is the most the hand-off comment may hold.
	implementHandoffLimit = 64 << 10
	// implementHandoffBudget leaves room under the limit for redaction.
	implementHandoffBudget = implementHandoffLimit - 4<<10
	implementReasonLimit   = 600
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
	a, err := h.b.bind(ctx, opts, req)
	if err != nil {
		return ActivityResult{}, err
	}
	rows, err := h.b.DB.ListImplementStories(ctx, a.Parent.String(), a.Generation)
	if err != nil {
		return ActivityResult{}, err
	}
	text, counts := renderImplementHandoff(a, rows)
	body, err := h.b.Redactor.Apply(ctx, text, implementHandoffLimit)
	if err != nil {
		return ActivityResult{}, fmt.Errorf("render hand-off: %w", err)
	}
	if body.Truncated() || body.Len() > implementHandoffLimit {
		return ActivityResult{}, errors.New("hand-off report exceeds its size limit")
	}
	repo := github.RepoFromRef(a.Parent)
	marker := report.ImplementHandoffMarker(a.Parent)
	var existing *github.Comment
scan:
	for page := 1; page > 0 && page <= implementCommentPages; {
		got, err := h.b.Forge.ListCommentsPage(ctx, repo, a.Parent.Issue, page, implementCommentLimit)
		if err != nil {
			return ActivityResult{}, fmt.Errorf("look for hand-off report: %w", err)
		}
		for i := range got.Comments {
			if strings.HasPrefix(got.Comments[i].Body, marker) {
				existing = &got.Comments[i]
				break scan
			}
		}
		page = got.NextPage
	}
	out := implementHandoffOutput{Merged: counts.merged, Parked: counts.parked, Waiting: counts.waiting}
	switch {
	case existing == nil:
		posted, err := h.b.Forge.PostComment(ctx, repo, a.Parent.Issue, body)
		if err != nil {
			return ActivityResult{}, fmt.Errorf("post hand-off report: %w", err)
		}
		out.CommentID, out.URL = posted.ID, posted.HTMLURL
	case existing.Body == body.String():
		out.CommentID, out.URL = existing.ID, existing.HTMLURL
	default:
		if err := h.b.Forge.EditComment(ctx, repo, existing.ID, body); err != nil {
			return ActivityResult{}, fmt.Errorf("edit hand-off report: %w", err)
		}
		out.CommentID, out.URL = existing.ID, existing.HTMLURL
	}
	return implementResult("posted", out)
}

type implementHandoffCounts struct{ merged, parked, waiting int }

// renderImplementHandoff writes the report in plain language. When the report
// would exceed its budget, the later entries of a section are summarized as a
// count instead of listed.
func renderImplementHandoff(a ImplementAuthority, rows []store.ImplementStory) (string, implementHandoffCounts) {
	state := map[string]store.ImplementStory{}
	for _, row := range rows {
		state[row.Key] = row
	}
	var merged, parked, waiting []string
	for _, story := range a.Stories {
		row, ok := state[story.Key]
		if !ok {
			row = store.ImplementStory{Key: story.Key, State: store.ImplementWaiting}
		}
		label := story.Key
		if story.Name != "" {
			label = fmt.Sprintf("%s (%s)", story.Name, story.Key)
		}
		switch row.State {
		case store.ImplementMerged:
			merged = append(merged, fmt.Sprintf("- %s: merged as commit `%s`.", label, row.MergeCommit))
		case store.ImplementParked:
			parked = append(parked, fmt.Sprintf("- %s: set aside. Reason: %s", label, clipText(oneLine(row.Reason), implementReasonLimit)))
		default:
			waiting = append(waiting, fmt.Sprintf("- %s: %s", label, waitingSentence(story, row, state)))
		}
	}
	var out strings.Builder
	out.WriteString(report.ImplementHandoffMarker(a.Parent) + "\n\n")
	out.WriteString("**Implementation hand-off**\n\n")
	fmt.Fprintf(&out, "Work is merged into `%s` one story at a time; each story is tested by its worker before it is merged. This report is updated as the work moves on.\n\n", a.IntegrationBranch)
	sections := []struct {
		title, empty string
		lines        []string
	}{
		{"Merged", "Nothing has been merged yet.", merged},
		{"Parked", "No story has been set aside.", parked},
		{"Still waiting", "No story is waiting.", waiting},
	}
	budget := implementHandoffBudget - out.Len()
	for _, s := range sections {
		fmt.Fprintf(&out, "**%s (%d)**\n\n", s.title, len(s.lines))
		if len(s.lines) == 0 {
			out.WriteString(s.empty + "\n\n")
			continue
		}
		share := budget / 3
		used := 0
		for i, line := range s.lines {
			if used+len(line)+1 > share {
				fmt.Fprintf(&out, "- and %d more.\n", len(s.lines)-i)
				break
			}
			out.WriteString(line + "\n")
			used += len(line) + 1
		}
		out.WriteString("\n")
	}
	return out.String(), implementHandoffCounts{len(merged), len(parked), len(waiting)}
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
