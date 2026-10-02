package turn

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/store"
)

// Reconcile recovers phase bookkeeping without launching a command or signalling
// a saved PID. Quiescent means no possible running work; it does not mean passed.
// Pending phases with completed commands resume through RunPhase, which performs
// the fresh source/target/context guards before deriving a passing outcome.
func (h *BugChecksActivity) Reconcile(ctx context.Context, db *store.DB, session int64, operation string) (store.BugCheckPhaseRecord, bool, error) {
	if db == nil {
		return store.BugCheckPhaseRecord{}, false, errors.New("check reconciliation requires store")
	}
	phase, err := db.GetBugCheckPhase(ctx, session, operation)
	if err != nil {
		return phase, false, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	active := h.active[bugCheckKey(session, operation)] != nil
	if active {
		return phase, false, nil
	}
	if !phase.Cleanup {
		if len(phase.Commands) == 0 {
			return phase, false, nil
		}
		last := phase.Commands[len(phase.Commands)-1]
		// A claimed launch with no retained kernel identity cannot be certified as
		// undispatched. Keep it unresolved rather than launching or releasing it.
		if last.LaunchIdentity() == "" {
			return phase, false, nil
		}
		observer := h.ObserveStopped
		if observer == nil {
			observer = bugchecks.ProcessStopped
		}
		stopped, observeErr := observer(ctx, last.LaunchIdentity())
		if observeErr != nil {
			return phase, false, observeErr
		}
		if !stopped {
			return phase, false, nil
		}
		if err = db.ReconcileBugCheckStopped(ctx, session, operation, last.Ordinal, last.LaunchIdentity(), time.Now().UTC()); err != nil {
			return phase, false, err
		}
		phase, err = db.GetBugCheckPhase(ctx, session, operation)
		if err != nil {
			return phase, false, err
		}
	}
	if !phase.Cleanup {
		return phase, false, nil
	}
	if phase.Outcome == "pending" {
		outcome, reason := "", ""
		for _, command := range phase.Commands {
			if command.State != "completed" {
				return phase, false, nil
			}
			if verdict, why := bugCommandVerdict(command); verdict != "passed" {
				outcome, reason = verdict, why
				break
			}
		}
		if phase.Cancelled {
			outcome = "cancelled"
			reason = "check cancellation confirmed after process cleanup"
		}
		if outcome != "" {
			if err = db.FinishBugCheckPhase(ctx, session, operation, outcome, reason, false); err != nil {
				return phase, false, err
			}
			phase, err = db.GetBugCheckPhase(ctx, session, operation)
			if err != nil {
				return phase, false, err
			}
		}
	}
	if err = db.ReleaseQuiescentBugCheckWork(ctx, session, operation); err != nil {
		return phase, false, err
	}
	return phase, true, nil
}
