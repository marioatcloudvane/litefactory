package daemon

import (
	"context"
	"errors"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// validateAutoID is the identity of one automatic Validate start: a pure
// function of the parent and the integration head it tests. The same result is
// the same schedule, so a restart or a repeated tick starts nothing twice.
func validateAutoID(session store.Session, commit string) int64 {
	return planWorkID("validate-auto", session.TaskRef, commit)
}

// validateAutoOwed reports the integration head Validate is owed on, when
// validate.auto_start is on, the parent's latest Implement finished with every
// approved story merged, and no admitted entry exists for that head. It reads
// saved state and the remote head only.
func (d *Daemon) validateAutoOwed(ctx context.Context, session store.Session) (string, bool, error) {
	if !d.validateAutoStart || d.validateHead == nil {
		return "", false, nil
	}
	gate, err := turn.ValidateEntryGate(ctx, d.db, session)
	if errors.Is(err, store.ErrNotFound) || err != nil || !gate.Open {
		return "", false, ignoreNotFound(err)
	}
	head, err := d.validateHead(ctx, session)
	if err != nil {
		return "", false, err
	}
	entry, err := d.db.GetValidateEntry(ctx, session.ID, head)
	switch {
	case err == nil && entry.State == store.ValidateEntryAdmitted:
		return "", false, nil
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return "", false, err
	}
	attempted, err := d.db.ScheduleAttempted(ctx, session.ID, validateAutoID(session, head))
	if err != nil || attempted {
		return "", false, err
	}
	return head, true, nil
}

func ignoreNotFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}

// graphValidateStarts offers every finished Implement parent that owes an
// automatic Validate start as a task of its own. It does nothing while
// validate.auto_start is off.
func (d *Daemon) graphValidateStarts(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate) error {
	if !d.validateAutoStart {
		return nil
	}
	ids, err := d.db.ListFinishedImplementParents(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		session, err := d.db.GetSession(ctx, id)
		if err != nil {
			return err
		}
		q := byRepo[session.RepoID]
		if q == nil {
			continue
		}
		if _, owed, err := d.validateAutoOwed(ctx, session); err != nil {
			d.log.WarnContext(ctx, "could not tell whether Validate is owed on a finished Implement parent",
				slog.String("task", session.TaskRef), slog.Any("error", err))
			continue
		} else if !owed {
			continue
		}
		if prior := seen[session.ID]; prior != nil {
			prior.addSource("validate_auto")
			continue
		}
		ref, err := naming.ParseRef(session.TaskRef)
		if err != nil {
			return err
		}
		c := q.candidate(session, ref)
		c.addSource("validate_auto")
		q.candidates = append(q.candidates, c)
		seen[session.ID] = c
	}
	return nil
}

// startValidateAuto starts Validate for the task's finished Implement result
// when the owner turned automatic start on. The entry is saved first, then the
// start runs as a schedule keyed by what it is, which gives the run the private
// identity every run needs and goes through the same admission as /validate.
func (d *Daemon) startValidateAuto(ctx context.Context, t *task) error {
	head, owed, err := d.validateAutoOwed(ctx, t.session)
	if err != nil || !owed {
		return err
	}
	if _, _, err := d.db.EnsureValidateEntry(ctx, store.ValidateEntry{SessionID: t.session.ID, Commit: head, Branch: t.session.Branch, Source: store.ValidateSourceAuto}, t.tickAt.UTC()); err != nil {
		return err
	}
	stored, _, err := d.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{
		SessionID: t.session.ID, SourceCommentID: validateAutoID(t.session, head), OwnerHandle: d.filter.Identities().OwnerHandle,
		Payload: turn.ValidateAutoPayload, DueAt: t.tickAt.UTC(),
	})
	if err != nil {
		return err
	}
	if stored.Status != store.SchedulePending {
		return nil
	}
	reserved, ok, err := d.db.ReserveSchedule(ctx, stored.ID, t.tickAt.UTC())
	if err != nil || !ok {
		return err
	}
	scheduled := *t
	scheduled.scheduled = &reserved
	d.runScheduled(ctx, &scheduled, reserved)
	return nil
}
