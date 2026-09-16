package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// publicationRecovery is tick-owned bounded traversal state. Durable operation
// records survive restart; losing these cursors only repeats read-only pages.
type publicationRecovery struct {
	next          time.Time
	comment       *store.CommentOperation
	commentAfter  store.CommentOperation
	commentPage   int
	creation      *store.PendingCreationOperation
	creationAfter int64
	creationPage  int
	creationState string
}

func (d *Daemon) pollPublications(ctx context.Context, now time.Time) error {
	e, ok := d.turn.(turnExecutor)
	if !ok || e.opts.MCP == nil || now.Before(d.publications.next) {
		return nil
	}
	d.publications.next = now.Add(time.Minute)
	// One page per family per tick bounds both I/O and retained cursor memory.
	return errors.Join(d.pollCommentPublication(ctx, e), d.pollCreationPublication(ctx, e))
}

func (d *Daemon) pollCommentPublication(ctx context.Context, e turnExecutor) error {
	p := &d.publications
	if e.opts.MCP.Comments == nil {
		return nil
	}
	if p.comment == nil {
		rows, err := d.db.PendingCommentOperations(ctx, p.commentAfter.Scope.RunID, p.commentAfter.Scope.Attempt, p.commentAfter.RequestID, 1)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			p.commentAfter = store.CommentOperation{}
			return nil
		}
		p.comment = &rows[0]
		p.commentPage = 1
	}
	observed, next, err := e.opts.MCP.ObserveComment(ctx, *p.comment, p.commentPage)
	if err != nil {
		return err
	}
	if observed.State == "confirmed" || next <= p.commentPage {
		p.commentAfter = *p.comment
		p.comment = nil
	} else {
		p.commentPage = next
	}
	return nil
}

func (d *Daemon) pollCreationPublication(ctx context.Context, e turnExecutor) error {
	p := &d.publications
	if e.opts.MCP.Creation == nil {
		return nil
	}
	if p.creation == nil {
		rows, err := d.db.PendingCreationOperations(ctx, p.creationAfter, 1)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			p.creationAfter = 0
			return nil
		}
		p.creation = &rows[0]
		p.creationPage = 1
		p.creationState = "open"
	}
	observed, next, err := e.opts.MCP.ObserveCreation(ctx, *p.creation, p.creationState, p.creationPage)
	if err != nil {
		return err
	}
	if observed.State == "confirmed" {
		p.creationAfter = p.creation.ID
		p.creation = nil
		return nil
	}
	if next > p.creationPage {
		p.creationPage = next
		return nil
	}
	if p.creationState == "open" {
		p.creationState = "closed"
		p.creationPage = 1
		return nil
	}
	p.creationAfter = p.creation.ID
	p.creation = nil
	return nil
}
