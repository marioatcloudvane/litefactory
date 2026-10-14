package turn

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// SourceLinks returns provider-observed display links for an exact source reply.
func (b *TicketWorkflowBridge) SourceLinks(ctx context.Context, ref naming.Ref, child int, replyID int64) (string, string, error) {
	selected, selectionErr := b.selected(ctx, github.RepoFromRef(ref), ref.Issue, 0)
	if selectionErr != nil {
		return "", "", selectionErr
	}
	if selected != b {
		return selected.SourceLinks(ctx, ref, child, replyID)
	}
	parent, err := b.Publication.destination(ctx, github.RepoFromRef(ref), ref.Issue)
	if err != nil {
		return "", "", err
	}
	source, err := b.Publication.destination(ctx, github.RepoFromRef(ref), child)
	if err != nil {
		return "", "", err
	}
	cursor, err := compatibilityReplyCursor(ctx, b.DB, source.Ref, replyID-1)
	if err != nil {
		return "", "", err
	}
	comments, err := completeCompatibilityReplies(ctx, b.Publication.tickets, source.Ref, cursor)
	if err != nil {
		return "", "", err
	}
	if !comments.Complete {
		return "", "", errors.New("incomplete source link observation")
	}
	for _, c := range comments.Replies {
		id, slotErr := compatibilitySlot(ctx, b.DB, "comment", c.Identity)
		if slotErr != nil {
			return "", "", slotErr
		}
		if id == replyID && c.Ticket.Equal(source.Ref) {
			return parent.BrowserURL, c.BrowserURL, nil
		}
	}
	return "", "", errors.New("recorded source reply is absent")
}

func reportSourceLinks(ctx context.Context, forge interface{}, ref naming.Ref, child int, replyID int64) (string, string, error) {
	if reader, ok := forge.(interface {
		SourceLinks(context.Context, naming.Ref, int, int64) (string, string, error)
	}); ok {
		return reader.SourceLinks(ctx, ref, child, replyID)
	}
	if ref.Forge == "linear" {
		return "", "", errors.New("provider source reply links unavailable")
	}
	return ref.IssueURL(), fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-%d", ref.Owner, ref.Repo, child, replyID), nil
}

// ticketDisplay is presentation only; compatibility numbers remain identities.
type ticketDisplay struct{ Label, URL, TitleKey string }

func (d ticketDisplay) Link() string {
	if d.URL == "" {
		return d.Label
	}
	return fmt.Sprintf("[%s](%s)", d.Label, d.URL)
}
func displayIssue(ref naming.Ref, issue github.Issue) ticketDisplay {
	if s := issue.QualifiedSnapshot; s != nil && s.Ref.Identity.Provider == ticket.Linear {
		if s.DisplayKey == "" {
			return ticketDisplay{Label: "this issue", TitleKey: "story"}
		}
		return ticketDisplay{Label: s.DisplayKey, URL: s.BrowserURL, TitleKey: s.DisplayKey}
	}
	if ref.Forge == "linear" {
		return ticketDisplay{Label: "this issue", TitleKey: "story"}
	}
	n := issue.Number
	if n == 0 {
		n = ref.Issue
	}
	ref.Issue = n
	url := issue.HTMLURL
	if url == "" {
		url = ref.IssueURL()
	}
	return ticketDisplay{Label: fmt.Sprintf("#%d", n), URL: url, TitleKey: strconv.Itoa(n)}
}
func lookupTicketDisplay(ctx context.Context, forge interface{}, ref naming.Ref, number int) (ticketDisplay, error) {
	if number < 1 {
		return ticketDisplay{Label: "not created yet", TitleKey: "story"}, nil
	}
	ref.Issue = number
	if ref.Forge != "linear" {
		return displayIssue(ref, github.Issue{Number: number}), nil
	}
	reader, ok := forge.(interface {
		GetIssue(context.Context, github.Repo, int) (github.Issue, error)
	})
	if !ok {
		return displayIssue(ref, github.Issue{}), nil
	}
	issue, err := reader.GetIssue(ctx, github.RepoFromRef(ref), number)
	if err != nil {
		return ticketDisplay{}, err
	}
	return displayIssue(ref, issue), nil
}

func (d ticketDisplay) Reference() string {
	if strings.HasPrefix(d.Label, "#") {
		return d.Label
	}
	return d.Link()
}
