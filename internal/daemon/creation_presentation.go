package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// creationRecoveryPrefix marks report-recovery rows that belong to the
// presentation of a retained creation proposal. It follows the precedent of the
// "stopped:" prefix: the same machinery (cadence, backoff, escalation,
// durability across restarts), a distinct key, so a presentation debt can never
// be mistaken for, or resolved by, the run's own result report.
const creationRecoveryPrefix = "creation:"

// errPresentationDeferred is the reason recorded when another delivery is still
// unresolved on the same session and trigger. It is deliberately not a
// [turn.DeliveryError]: nothing was attempted, so there is no delivery state to
// report.
var errPresentationDeferred = errors.New("another delivery is unresolved on this trigger; the creation proposal presentation waits for the next eligible tick")

// creationRecoveryKey is the recovery key of one (session, trigger) source's
// presentation debt.
func creationRecoveryKey(session, trigger int64) string {
	return creationRecoveryPrefix + recoveryKey(session, trigger)
}

// presentationDeferred reports whether a presentation must wait: an unresolved
// delivery exists on the source and it is not the presentation's own. The store
// refuses a source with two unresolved purposes, so minting a second one is
// never an option; the presentation's own key (bound before its POST) is what
// lets a retry proceed instead of waiting on itself.
func presentationDeferred(unresolved store.ReportDelivery, found bool, ownKey string) bool {
	return found && unresolved.Key != ownKey
}

// presentCreationProposal posts the owner-facing comment for the proposal the
// finished run retained, exactly once and only for the last draft.
//
// It is called from both branches of [Daemon.completeTurn], after the run's own
// result was reported (legacy) or its trigger consumed (graph), on the worker
// goroutine under the tick context. Because [Daemon.repairStoppedResult] also
// ends in completeTurn, a repaired stopped result inherits the hook.
//
// The durable bookkeeping row is the whole decision: no row, or one already
// presented, means nothing is owed ([store.ErrNotFound]). The story-creation
// flag is deliberately not consulted again; a row can only exist because the
// tool was installed when the run retained the proposal, and a second gate could
// drift from the first. A run that retained a proposal and then failed still
// presents, for the same reason.
//
// The reporter is reached through an optional capability assertion. When a row
// is owed and the reporter lacks the capability the error is returned, not
// swallowed: a draft the owner never sees is a failure, not a no-op.
//
// The attempt goes through [Daemon.reportAttempt] under the "creation:"
// recovery key, so a forge failure records durable debt and backs off. A
// failure here never changes the run's outcome and never un-consumes its
// trigger; the caller routes the returned error through [Daemon.leave].
//
// # The session is held while a presentation is owed
//
// Recovery debt holds its session until due ([Daemon.sessionRecoveryHeld]).
// That is intended, because the draft is owed, but it means a forge outage also
// defers the task's next comment. [Daemon.resumeCreationPresentations] retries
// the debt before the session's comments, so ordering is kept.
// [Daemon.resumeReport] may also pick the same delivery up through the generic
// pending-report path under the "source:" key; both routes are idempotent
// because the delivery journal keys the comment by its purpose and proposal
// hash.
//
// # Deferral
//
// [store.DB.FindPendingReportDelivery] refuses a source with two unresolved
// purposes. When a different delivery is unresolved on this trigger the
// presentation is not minted; the attempt fails with [errPresentationDeferred],
// which records debt and lets the next eligible tick try again.
func (d *Daemon) presentCreationProposal(ctx context.Context, t *task, runID, triggerID int64) error {
	if runID < 1 {
		return nil
	}
	source := store.ReportRecovery{Key: creationRecoveryKey(t.session.ID, triggerID), SessionID: t.session.ID, TriggerID: triggerID, RunID: runID, Task: t.ref.String()}
	creation, err := store.NewCreationStore(d.db, nil)
	if err != nil {
		return fmt.Errorf("present creation proposal: %w", err)
	}
	row, err := creation.PendingCreationPresentation(ctx, runID)
	if errors.Is(err, store.ErrNotFound) {
		// Nothing owed. A debt left over from an earlier attempt whose comment
		// has since been marked presented is finished here.
		d.recoverySucceeded(ctx, source.Key)
		return nil
	}
	if err != nil {
		return fmt.Errorf("present creation proposal: %w", err)
	}
	presenter, ok := d.reporter.(turn.CreationPresenter)
	if !ok {
		return fmt.Errorf("creation proposal %s of run %d is owed but the reporter cannot present it", row.ProposalHash, runID)
	}
	_, err = d.reportAttempt(ctx, source, func() (turn.Posted, error) {
		unresolved, found, err := d.db.FindPendingReportDelivery(ctx, t.session.ID, triggerID)
		if err != nil {
			return turn.Posted{}, fmt.Errorf("check unresolved deliveries: %w", err)
		}
		if presentationDeferred(unresolved, found, row.DeliveryKey) {
			return turn.Posted{}, errPresentationDeferred
		}
		return presenter.ReportCreationPresentation(ctx, runID)
	})
	return err
}

// creationDebt is the session's unresolved presentation debt in key order.
func (d *Daemon) creationDebt(sessionID int64) []store.ReportRecovery {
	d.recoveries.mu.Lock()
	defer d.recoveries.mu.Unlock()
	var debt []store.ReportRecovery
	for key, row := range d.recoveries.items {
		if strings.HasPrefix(key, creationRecoveryPrefix) && row.State != "resolved" && row.SessionID == sessionID && row.RunID != 0 {
			debt = append(debt, row)
		}
	}
	sort.Slice(debt, func(i, j int) bool { return debt[i].Key < debt[j].Key })
	return debt
}

// resumeCreationPresentations retries a session's owed presentations before any
// of its comments are handled. The run's trigger has normally been consumed by
// the run's own report, so the comment queue would never bring the daemon back
// to it; the recovery row is what does.
//
// An error, pending or not, stops the worker before it drains comments: the
// session stays held until the presentation is posted.
func (d *Daemon) resumeCreationPresentations(ctx context.Context, t *task) error {
	for _, row := range d.creationDebt(t.session.ID) {
		if err := d.presentCreationProposal(ctx, t, row.RunID, row.TriggerID); err != nil {
			return err
		}
	}
	return nil
}
