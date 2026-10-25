package daemon

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type ownerInputReporter interface {
	ReportOwnerInput(context.Context, naming.Ref, int64, int64, bool) (turn.Posted, error)
}

func (d *Daemon) reportOwnerInput(ctx context.Context, t *task, commentID int64, guidance bool) error {
	r, ok := d.reporter.(ownerInputReporter)
	if !ok {
		return errors.New("owner input reporting unavailable")
	}
	source := store.ReportRecovery{Key: recoveryKey(t.session.ID, commentID), SessionID: t.session.ID, TriggerID: commentID, Task: t.ref.String()}
	_, err := d.guardedReportAttempt(ctx, source, func() (turn.Posted, error) { return r.ReportOwnerInput(ctx, t.ref, t.session.ID, commentID, guidance) })
	return err
}

func (d *Daemon) retainOrdinaryOwnerInput(ctx context.Context, t *task, c github.Comment) (bool, error) {
	// An edited control source belongs to its retained decision/report recovery,
	// not to the ordinary-input queue. Leave the existing recovery path intact.
	source, err := d.db.GetComment(ctx, t.session.ID, c.ID)
	if err != nil {
		return false, err
	}
	if turn.ParseCommand(source.Body).Kind != turn.KindFreeText {
		return false, nil
	}
	owner := d.ticketIdentities(ctx).OwnerUserID
	if owner < 1 || c.Author.ID != owner {
		return false, errors.New("ordinary instruction requires numeric owner identity")
	}
	target, err := ownerInputTarget(c.Body)
	if err != nil {
		return false, err
	}
	_, err = d.db.RetainOwnerInstruction(ctx, t.session.ID, c.ID, owner, c.Body, target)
	return err == nil, err
}

// Input-Target is explicit metadata, never inferred from prose or remapped to a
// replacement instance. The immutable body (including this line) is retained.
func ownerInputTarget(body string) (store.OwnerInputTarget, error) {
	return store.ParseOwnerInputTarget(body)
}

func (d *Daemon) handleOwnerGuide(ctx context.Context, t *task, c github.Comment) error {
	owner := d.ticketIdentities(ctx).OwnerUserID
	if owner < 1 || c.Author.ID != owner {
		return errors.New("standing guidance requires numeric owner identity")
	}
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.Body), "/guide"))
	if body != "" {
		if body == "clear" {
			body = ""
		}
		if _, err := d.db.SetOwnerGuidance(ctx, t.session.ID, c.ID, owner, body); err != nil {
			return err
		}
	}
	return d.reportOwnerInput(ctx, t, c.ID, true)
}

// admittedOrdinaryComment pins routing to the first admitted ordinary source,
// including a crash between recording the comment and retaining its input row.
func (d *Daemon) admittedOrdinaryComment(ctx context.Context, t *task, c github.Comment) (github.Comment, error) {
	saved, err := d.db.GetComment(ctx, t.session.ID, c.ID)
	if errors.Is(err, store.ErrNotFound) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if saved.Classification == store.ClassificationInstruction && turn.ParseCommand(saved.Body).Kind == turn.KindFreeText {
		if saved.AuthorUserID != c.Author.ID {
			return c, errors.New("ordinary instruction source actor changed")
		}
		c.Body, c.CreatedAt = saved.Body, saved.CreatedAt
	}
	return c, nil
}
