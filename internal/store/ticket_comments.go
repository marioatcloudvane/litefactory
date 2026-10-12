package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// RecordTicketReply records a verified reply through the historical comment
// ledger. Its owning session supplies durable provider/origin qualification;
// no comment IDs, tags, classifications or consumption records are re-keyed.
func (db *DB) RecordTicketReply(ctx context.Context, sessionID int64, reply ticket.Reply, classification Classification) (Comment, error) {
	if err := reply.Ticket.Validate(); err != nil {
		return Comment{}, err
	}
	if err := reply.Identity.Validate(); err != nil {
		return Comment{}, err
	}
	if !classification.valid() {
		return Comment{}, fmt.Errorf("store: invalid reply classification %q", classification)
	}
	if reply.Identity.Provider != reply.Ticket.Identity.Provider || reply.Identity.Origin != reply.Ticket.Identity.Origin || reply.Author.Identity.Provider != reply.Identity.Provider || reply.Author.Identity.Origin != reply.Identity.Origin {
		return Comment{}, ErrTicketIdentityConflict
	}
	var result Comment
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		commentID, err := storedReplyID(ctx, tx, "comment", reply.Identity, false)
		if err != nil {
			return err
		}
		authorID, err := storedReplyID(ctx, tx, "actor", reply.Author.Identity, true)
		if err != nil {
			return err
		}
		binding, err := scanSessionTicket(tx.QueryRowContext(ctx, `SELECT `+sessionTicketColumns+` FROM session_ticket_identities WHERE session_id=?`, sessionID))
		if err != nil {
			return err
		}
		ref, err := binding.Ref()
		if err != nil {
			return err
		}
		if !ref.Equal(reply.Ticket) {
			return ErrTicketIdentityConflict
		}
		result, err = recordComment(ctx, tx, Comment{SessionID: sessionID, ForgeCommentID: commentID, AuthorUserID: authorID, AuthorHandle: reply.Author.Handle, AuthorIsBot: reply.Author.IsBot, Body: reply.Body, CreatedAt: reply.CreatedAt, Classification: classification})
		if err != nil {
			return err
		}
		// Replays may preserve older content/classification but never inherit the
		// authority of a different author under the same immutable reply ID.
		if result.AuthorUserID != authorID {
			return ErrTicketIdentityConflict
		}
		return nil
	})
	if err != nil {
		return Comment{}, fmt.Errorf("store: record qualified reply: %w", err)
	}
	return result, nil
}

// legacyReplyID is the explicit GitHub compatibility boundary. Opaque IDs from
// another provider must never pass through this historical numeric ledger.
func legacyReplyID(raw string, deletedActor bool) (int64, error) {
	if raw == "" && deletedActor {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != raw {
		return 0, ErrTicketIdentityConflict
	}
	return id, nil
}

func storedReplyID(ctx context.Context, tx *sql.Tx, kind string, identity ticket.Identity, deletedActor bool) (int64, error) {
	if identity.Provider == ticket.GitHub {
		return legacyReplyID(identity.RemoteID, deletedActor)
	}
	if identity.RemoteID == "" && deletedActor {
		return 0, nil
	}
	return localTicketSlot(ctx, tx, kind, identity)
}
