package turn

import (
	"context"
	"errors"
	"strconv"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
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
	cursor, err := github.ReplyCursor(source.Ref, replyID-1)
	if err != nil {
		return "", "", err
	}
	comments, err := b.Publication.tickets.RepliesAfter(ctx, source.Ref, cursor)
	if err != nil {
		return "", "", err
	}
	if !comments.Complete {
		return "", "", errors.New("incomplete source link observation")
	}
	for _, c := range comments.Replies {
		if c.Identity.RemoteID == strconv.FormatInt(replyID, 10) && c.Ticket.Equal(source.Ref) {
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
	return "", "", nil // Explicit retained legacy renderer compatibility.
}
