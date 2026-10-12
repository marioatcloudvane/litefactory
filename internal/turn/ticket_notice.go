package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type ticketNoticePoster interface {
	PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error)
}

type guardedTicketNotice interface {
	PostNotice(context.Context, string, string, int64, github.Repo, int, redact.Body) (github.Comment, error)
}

func hasTicketNoticeGuard(forge any) bool { _, ok := forge.(guardedTicketNotice); return ok }

// postTicketNotice preserves original markers and workflow receipt ownership.
// Legacy fixtures may provide their historical poster; production uses the
// qualified workflow bridge and durable dispatch metadata.
func postTicketNotice(ctx context.Context, forge ticketNoticePoster, marker, parent string, botID int64, repo github.Repo, number int, body redact.Body) (github.Comment, error) {
	if guarded, ok := forge.(guardedTicketNotice); ok {
		return guarded.PostNotice(ctx, marker, parent, botID, repo, number, body)
	}
	return forge.PostComment(ctx, repo, number, body)
}

// PostNotice keeps auxiliary workflow publications on their recorded ticket.
func (b *TicketWorkflowBridge) PostNotice(ctx context.Context, marker, parent string, botID int64, repo github.Repo, number int, body redact.Body) (github.Comment, error) {
	selected, selectionErr := b.selected(ctx, repo, number, 0)
	if selectionErr != nil {
		return github.Comment{}, selectionErr
	}
	if selected != b {
		return selected.PostNotice(ctx, marker, parent, botID, repo, number, body)
	}
	if botID == 0 {
		botID = b.ActorID()
	}
	if botID != b.ActorID() || b.Bot.Origin != b.Publication.origin {
		return github.Comment{}, store.ErrTicketIdentityConflict
	}
	return b.Publication.PostNotice(ctx, marker, parent, botID, repo, number, body)
}

// PostNotice claims one durable dispatch before publishing prepared notice bytes.
// A lost response requires exact remote reconciliation and never permits a resend.
func (b *TicketPublicationBridge) PostNotice(ctx context.Context, marker, parent string, botID int64, repo github.Repo, number int, body redact.Body) (github.Comment, error) {
	if marker == "" || parent == "" || botID < 1 || !exactProgressMarker(body.String(), marker) {
		return github.Comment{}, errors.New("invalid ticket notice identity")
	}
	digest := sha256.Sum256([]byte(parent + "\x00" + marker))
	key := "notice:" + hex.EncodeToString(digest[:])
	locator, err := b.locator(ctx, repo, number)
	if err != nil {
		return github.Comment{}, err
	}
	actor, err := compatibilityIdentity(ctx, b.db, b.provider, b.origin, "actor", botID)
	if err != nil {
		return github.Comment{}, err
	}
	snapshot, err := b.resolve(ctx, key, locator, actor)
	if err != nil {
		return github.Comment{}, err
	}
	debt, err := b.db.PrepareTicketNotice(ctx, key, body.String())
	if err != nil {
		return github.Comment{}, err
	}
	uncertain := func(cause error) (github.Comment, error) {
		return github.Comment{}, &ticket.Failure{Kind: ticket.Uncertain, Operation: "ticket notice reconciliation", Cause: cause}
	}
	confirm := func(reply ticket.Reply) (github.Comment, error) {
		if reply.Ticket.Locator != snapshot.Ref.Locator || !reply.Ticket.Equal(snapshot.Ref) || !reply.Author.Identity.Equal(actor) || reply.Body != debt.Body || !exactProgressMarker(reply.Body, marker) {
			return uncertain(store.ErrTicketIdentityConflict)
		}
		out, e := compatibilityReply(ctx, b.db, reply, snapshot.Ref)
		if e != nil {
			return uncertain(e)
		}
		if e = b.rememberReply(ctx, reply); e != nil {
			return uncertain(e)
		}
		if e = b.db.ConfirmTicketNotice(context.WithoutCancel(ctx), key, reply.Identity.RemoteID); e != nil {
			return uncertain(e)
		}
		return out, nil
	}

	cursor, e := compatibilityReplyCursor(ctx, b.db, snapshot.Ref, 0)
	if e != nil {
		return uncertain(e)
	}
	observed, e := completeCompatibilityReplies(ctx, b.tickets, snapshot.Ref, cursor)
	if e != nil {
		return uncertain(e)
	}
	if !observed.Complete {
		return uncertain(errors.New("incomplete ticket notice observation"))
	}
	var match *ticket.Reply
	for i := range observed.Replies {
		reply := &observed.Replies[i]
		if !exactProgressMarker(reply.Body, marker) {
			continue
		}
		if reply.Ticket.Locator != snapshot.Ref.Locator || !reply.Ticket.Equal(snapshot.Ref) || !reply.Author.Identity.Equal(actor) || reply.Body != debt.Body || (debt.ReplyID != "" && reply.Identity.RemoteID != debt.ReplyID) || match != nil {
			return uncertain(store.ErrTicketIdentityConflict)
		}
		match = reply
	}
	if match != nil {
		if !debt.Attempted {
			if _, _, e = b.db.ClaimTicketNotice(ctx, key, body.String()); e != nil {
				return uncertain(e)
			}
		}
		return confirm(*match)
	}
	if debt.Attempted {
		return uncertain(errors.New("original notice dispatch remains unconfirmed; no duplicate send"))
	}
	_, claimed, e := b.db.ClaimTicketNotice(ctx, key, body.String())
	if e != nil {
		return uncertain(e)
	}
	if !claimed {
		return uncertain(errors.New("original notice dispatch is owned by another invocation"))
	}

	reply, e := b.publishEffect(ctx, key, snapshot.Ref, body)
	if e != nil {
		return uncertain(e)
	}
	return confirm(reply)
}
