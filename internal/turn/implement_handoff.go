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
	text, counts := renderImplementHandoffProgress(a, rows, progress)
	text += implementValidateNextStep(opts.ValidateAutoStart)
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
		posted, err := b.Forge.PostComment(ctx, repo, a.Parent.Issue, body)
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

// renderImplementHandoff writes the report in plain language. When the report
// would exceed its budget, the later entries of a section are summarized as a
// count instead of listed.
func renderImplementHandoff(a ImplementAuthority, rows []store.ImplementStory) (string, implementHandoffCounts) {
	return renderImplementHandoffProgress(a, rows, nil)
}

func renderImplementHandoffProgress(a ImplementAuthority, rows []store.ImplementStory, progress []store.ImplementProgressReport) (string, implementHandoffCounts) {
	state := map[string]store.ImplementStory{}
	for _, row := range rows {
		state[row.Key] = row
	}
	// Completed stories are absent from this run's ledger, but still satisfy
	// dependencies in the approved story set.
	dependencies := maps.Clone(state)
	for _, story := range a.Stories {
		if _, exists := dependencies[story.Key]; story.AlreadyComplete && !exists {
			dependencies[story.Key] = store.ImplementStory{Key: story.Key, State: store.ImplementMerged}
		}
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
		switch {
		case story.AlreadyComplete && !ok:
			merged = append(merged, fmt.Sprintf("- %s: already complete before this run.", label))
		case row.State == store.ImplementMerged:
			merged = append(merged, fmt.Sprintf("- %s: merged as commit `%s`.", label, row.MergeCommit))
		case row.State == store.ImplementParked:
			parked = append(parked, fmt.Sprintf("- %s: set aside. Reason: %s", label, clipText(oneLine(row.Reason), implementReasonLimit)))
		default:
			line := fmt.Sprintf("- %s: %s", label, waitingSentence(story, row, dependencies))
			if row.State == store.ImplementRunning {
				for _, kind := range []string{"implementation", "testing", "repair"} {
					var latest *store.ImplementProgressReport
					for i := range progress {
						p := &progress[i]
						if p.StoryKey == row.Key && p.Kind == kind && p.StoryAttempts == row.Attempts && p.Generation == a.Generation && p.Parent == a.Parent.String() && !p.RecordedAt.Before(row.StateAt) && (latest == nil || p.RecordedAt.After(latest.RecordedAt) || p.RecordedAt.Equal(latest.RecordedAt) && p.ID > latest.ID) {
							latest = p
						}
					}
					if latest != nil {
						line += fmt.Sprintf("\n  Latest activity (advisory, not completion): %s at %s, attempt %d: %s", kind, latest.RecordedAt.UTC().Format(time.RFC3339), row.Attempts+1, safeImplementProgress(latest.Message))
					}
				}
			}
			waiting = append(waiting, line)
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

// safeImplementProgress keeps worker text inert even when it contains Markdown,
// autolinks, mentions or the hidden hand-off marker.
func safeImplementProgress(text string) string {
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
