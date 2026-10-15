package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// workerSyncBlockage reads only the declared gate's durable evidence. A retry
// becomes a story blockage only after the cumulative dispatch bound is reached.
func (rt *implementRuntime) workerSyncBlockage(ctx context.Context, instanceID int64, exhausted bool) (string, error) {
	if rt.worker == nil || rt.definition == nil {
		return "", nil
	}
	gate, gated := graphexec.ImplementWorkerSyncNode(rt.definition.Profile)
	if !gated {
		return "", nil
	}
	instance, err := rt.opts.DB.GetGraphInstance(ctx, instanceID)
	if err != nil {
		return "", err
	}
	if instance.SessionID != rt.worker.WorkerSession {
		return "", errors.New("worker sync blockage session differs from story")
	}
	attempts, err := rt.opts.DB.ListGraphNodeAttempts(ctx, instanceID)
	if err != nil {
		return "", err
	}
	var latest store.GraphNodeAttempt
	count := 0
	for _, attempt := range attempts {
		if attempt.Node == gate.Name {
			count++
			latest = attempt
		}
	}
	if latest.ID == 0 || latest.Status != store.GraphSucceeded {
		return "", nil
	}
	if exhausted {
		if instance.CurrentNode != gate.Name || count < gate.Limit {
			return "", nil
		}
	} else {
		park, _ := graphexec.Decide(rt.definition.Profile, gate.Name, "conflict")
		if instance.CurrentNode != park || latest.Outcome != "conflict" {
			return "", nil
		}
	}
	outputs, err := rt.opts.DB.ListGraphOutputsBounded(ctx, latest.ID, 1<<20, 16)
	if err != nil {
		return "", err
	}
	var evidence syncBaseOutput
	found := false
	for _, output := range outputs {
		if output.Kind == store.GraphOutputJSON {
			var candidate syncBaseOutput
			if err = json.Unmarshal(output.Payload, &candidate); err != nil {
				return "", err
			}
			if candidate.Result == latest.Outcome {
				evidence, found = candidate, true
				break
			}
		}
	}
	if !found || evidence.Result != latest.Outcome || evidence.Working != rt.worker.Branch || evidence.Base == "" {
		return "", errors.New("worker sync blockage lacks trusted target evidence")
	}
	reason := fmt.Sprintf("Base synchronization of %s with %s stopped: %s", evidence.Working, evidence.Base, evidence.Reason)
	if exhausted {
		reason = fmt.Sprintf("Base synchronization exhausted its cumulative limit of %d dispatches. %s", gate.Limit, reason)
	}
	if len(evidence.Conflicts) != 0 {
		reason += " Conflicting paths: " + strings.Join(evidence.Conflicts, ", ") + "."
	}
	if evidence.Commit != "" {
		reason += " Retained commit: " + evidence.Commit + "."
	}
	if evidence.Publication != "" {
		reason += " Publication: " + evidence.Publication + "."
	}
	return reason, nil
}

// parkSyncStory retains the redacted delivery before parking, then attempts
// delivery. A failed or uncertain report cannot make discovery relaunch a worker;
// the existing pending-delivery recovery owns the retained report thereafter.
func (rt *implementRuntime) parkSyncStory(ctx context.Context, reason string) (ActivityResult, error) {
	opts := rt.opts
	defer rt.refreshStatus(ctx, opts)
	row, err := rt.workerRow(ctx, opts)
	if err != nil {
		return ActivityResult{}, err
	}
	if row.State != store.ImplementRunning && row.State != store.ImplementParked {
		return ActivityResult{}, fmt.Errorf("sync-blocked story is %s", row.State)
	}
	r := opts.ResearchReporter
	if r == nil {
		return ActivityResult{}, errors.New("sync parking requires durable reporting")
	}
	revision, err := implementStoryRevision(ctx, opts.DB, row)
	if err != nil {
		return ActivityResult{}, err
	}
	parent, err := naming.ParseRef(row.Parent)
	if err != nil {
		return ActivityResult{}, err
	}
	destination := parent
	destination.Issue = revision.IssueNumber
	if err = destination.Validate(); err != nil {
		return ActivityResult{}, err
	}
	intent := implementParkIntent(r, parent, destination, row)
	delivery, err := opts.DB.GetReportDelivery(ctx, intent.Key)
	if errors.Is(err, store.ErrNotFound) {
		body, renderErr := renderImplementParkAnswer(ctx, r, agentresult.ResearchNode{Result: agentresult.Result{Reason: reason}})
		if renderErr != nil {
			return ActivityResult{}, renderErr
		}
		body, renderErr = r.renderer.RenderReportDelivery(ctx, body.String(), intent.Marker)
		if renderErr != nil {
			return ActivityResult{}, renderErr
		}
		intent.Body = body.String()
		intent.BotUserID, err = retainedTicketPublisher(ctx, opts.DB, intent.SessionID, intent.BotUserID)
		if err != nil {
			return ActivityResult{}, err
		}
		delivery, err = opts.DB.EnsureReportDelivery(ctx, intent)
	}
	if err != nil {
		return ActivityResult{}, err
	}
	if row.State == store.ImplementRunning {
		safe, renderErr := r.renderer.RenderReportDelivery(ctx, reason, "")
		if renderErr != nil {
			return ActivityResult{}, renderErr
		}
		row, err = opts.DB.TransitionImplementStory(ctx, store.ImplementTransition{Parent: row.Parent, Generation: row.Generation, Key: row.Key, From: store.ImplementRunning, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: clipText(safe.String(), maxImplementReason), At: rt.now()})
		if err != nil {
			return ActivityResult{}, err
		}
	}
	rt.wake(ctx, opts, row.Parent)
	_, err = r.dispatchDelivery(ctx, delivery)
	if err != nil {
		return ActivityResult{}, err
	}
	return implementResult("parked", map[string]any{"story": row.Key, "reason": row.Reason})
}

// recoverWorkerSyncParking settles a failed gate's retained blockage before a
// fresh infrastructure launch can acquire a new synchronization allowance.
// Owner resumption starts a new ledger epoch and intentionally skips old failures.
func (rt *implementRuntime) recoverWorkerSyncParking(ctx context.Context) (bool, error) {
	if rt.worker == nil {
		return false, nil
	}
	row, err := rt.workerRow(ctx, rt.opts)
	if err != nil {
		return false, err
	}
	if row.State != store.ImplementRunning {
		return false, nil
	}
	instance, err := rt.opts.DB.LatestWorkflowInstance(ctx, rt.req.SessionID, "implement")
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if instance.Status != store.GraphFailed || instance.StartedAt.Before(row.StateAt) {
		return false, nil
	}
	attempts, err := rt.opts.DB.ListGraphNodeAttempts(ctx, instance.ID)
	if err != nil {
		return false, err
	}
	if len(attempts) == 0 || attempts[len(attempts)-1].LaunchID == 0 {
		return false, nil
	}
	saved, err := rt.opts.DB.GetWorkflowCapture(ctx, attempts[len(attempts)-1].LaunchID)
	if err != nil {
		return false, err
	}
	if saved.InstanceID != instance.ID || saved.SessionID != rt.req.SessionID {
		return false, errors.New("worker sync parking recovery capture identity mismatch")
	}
	specs, err := ImplementSpecs()
	if err != nil {
		return false, err
	}
	definition, err := restoreWorkflow(ctx, rt.opts.DB, saved, agent.WorkflowImplement, specs)
	if err != nil {
		return false, err
	}
	previous := rt.definition
	rt.definition = definition
	defer func() { rt.definition = previous }()
	reason, err := rt.workerSyncBlockage(ctx, instance.ID, true)
	if err != nil {
		return false, err
	}
	if reason == "" {
		reason, err = rt.workerSyncBlockage(ctx, instance.ID, false)
	}
	if err != nil || reason == "" {
		return false, err
	}
	_, err = rt.parkSyncStory(ctx, reason)
	return true, err
}
