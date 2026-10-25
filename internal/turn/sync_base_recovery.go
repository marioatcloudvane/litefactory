package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// verifySyncHandoffBeforeAgent rechecks the latest sync in the current agent
// handoff. An intervening agent makes earlier sync evidence obsolete: that
// agent may legitimately have changed the retained checkout.
func verifySyncHandoffBeforeAgent(ctx context.Context, opts Options, profile *graphexec.Profile, instance store.GraphInstance, binding SyncBaseBinding) (err error) {
	attempts, err := opts.DB.ListGraphNodeAttempts(ctx, instance.ID)
	if err != nil {
		return err
	}
	for i := len(attempts) - 1; i >= 0; i-- {
		attempt := attempts[i]
		node, ok := profile.Node(attempt.Node)
		if !ok {
			return errors.New("sync recovery attempt is absent from pinned workflow")
		}
		if node.Action == ResearchAgentAction {
			return nil
		}
		if node.Action != graphexec.SyncBaseAction {
			continue
		}
		target, err := binding.Bind(ctx, opts, ActivityRequest{Workflow: profile.Workflow, Mode: profile.Mode, Team: profile.Team, Node: attempt.Node, InstanceID: instance.ID, NodeAttemptID: attempt.ID})
		if err != nil {
			return err
		}
		// Coordinator bindings have no retained checkout and keep their resolver routes.
		if target.Checkout == nil {
			return nil
		}
		if attempt.Status != store.GraphSucceeded || (attempt.Outcome != "synced" && attempt.Outcome != "current" && attempt.Outcome != "deferred") {
			return errors.New("dependent agent requires a successful base synchronization")
		}
		outputs, err := opts.DB.ListGraphOutputsBounded(ctx, attempt.ID, 1<<20, 16)
		if err != nil {
			return err
		}
		var evidence syncBaseOutput
		found := false
		for _, output := range outputs {
			if output.Kind != store.GraphOutputJSON {
				continue
			}
			var out syncBaseOutput
			if err = json.Unmarshal(output.Payload, &out); err != nil {
				return fmt.Errorf("read retained sync handoff: %w", err)
			}
			if out.Result == attempt.Outcome {
				evidence = out
				found = true
			}
		}
		if !found || !evidence.CheckoutVerified || evidence.Commit == "" || evidence.Working != target.Working || evidence.Base != target.Base {
			return errors.New("base synchronization lacks verified retained checkout evidence")
		}
		if target.Lock == nil {
			return errors.New("sync handoff recovery requires factory writer exclusion")
		}
		unlock, acquired, lockErr := target.Lock(ctx)
		if lockErr != nil {
			return fmt.Errorf("sync handoff recovery writer exclusion: %w", lockErr)
		}
		if !acquired {
			return errors.New("sync handoff recovery deferred while another writer is active")
		}
		if unlock == nil {
			return errors.New("sync handoff recovery writer exclusion has no release")
		}
		defer func() { err = errors.Join(err, unlock()) }()
		if attempt.Outcome == "deferred" || evidence.CheckoutUnchanged {
			if !slices.Contains(node.Outcomes, "deferred") {
				return errors.New("sync handoff deferred route is absent")
			}
			retained, ok := target.Checkout.(interface {
				DeferredHead(context.Context, string) (string, error)
			})
			if !ok {
				return errors.New("deferred handoff cannot verify retained checkout")
			}
			head, verifyErr := retained.DeferredHead(ctx, target.Working)
			if verifyErr != nil {
				return verifyErr
			}
			if head != evidence.Commit {
				return errors.New("retained checkout changed since preparation deferral")
			}
			return nil
		}
		if err = target.Checkout.Refresh(ctx, target.Working, evidence.Commit); err != nil {
			return fmt.Errorf("reverify synchronized checkout before agent preparation: %w", err)
		}
		return nil
	}
	return nil
}
