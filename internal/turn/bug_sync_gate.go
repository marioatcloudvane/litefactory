package turn

import (
	"context"
	"errors"
	"slices"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// graphBugFreshSync requires this entry to follow the declared gate, rather
// than borrowing a successful synchronization from an earlier repair.
func graphBugFreshSync(ctx context.Context, db *store.DB, p *graphexec.Profile, instanceID, activeAttempt int64) error {
	gate, gated := graphexec.BugFixerSyncGate(p)
	if !gated {
		return nil
	}
	fixer, ok := graphexec.Decide(p, gate.Name, "synced")
	if !ok {
		return errors.New("bug synchronization has no fixer destination")
	}
	attempts, err := db.ListGraphNodeAttempts(ctx, instanceID)
	if err != nil {
		return err
	}
	if activeAttempt != 0 {
		if len(attempts) == 0 || attempts[len(attempts)-1].ID != activeAttempt || attempts[len(attempts)-1].Node != fixer {
			return errors.New("fixer synchronization allocation is not current")
		}
		attempts = attempts[:len(attempts)-1]
	}
	if len(attempts) == 0 {
		return errors.New("fixer requires a fresh successful base synchronization")
	}
	last := attempts[len(attempts)-1]
	permitted := last.Outcome == "synced" || last.Outcome == "current" || last.Outcome == "deferred" && slices.Contains(gate.Outcomes, "deferred")
	if last.Node != gate.Name || last.Status != store.GraphSucceeded || !permitted {
		return errors.New("fixer requires a fresh successful base synchronization")
	}
	transitions, err := db.ListGraphTransitions(ctx, instanceID)
	if err != nil {
		return err
	}
	if len(transitions) == 0 {
		return errors.New("fixer synchronization transition is missing")
	}
	tr := transitions[len(transitions)-1]
	if tr.FromNode != gate.Name || tr.ToNode != fixer || tr.OnOutcome != last.Outcome || !tr.At.Equal(last.FinishedAt) {
		return errors.New("fixer synchronization transition is not current")
	}
	return nil
}

// graphBugSyncActivity checks repair authority before any source publication.
// The generic activity retains its existing workflow-independent semantics.
type graphBugSyncActivity struct {
	profile *graphexec.Profile
	binding SyncBaseBinding
}

func (h graphBugSyncActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if gate, ok := graphexec.BugFixerSyncGate(h.profile); ok && gate.Name == req.Node {
		if _, err := GraphBugApprovedHandoff(ctx, opts.DB, req.InstanceID); err != nil {
			return ActivityResult{}, err
		}
	}
	return (SyncBaseActivity{h.binding}).Execute(ctx, opts, req)
}
