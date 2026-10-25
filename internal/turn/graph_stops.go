package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// GraphStopQuestion is immutable factory reporting data, not a result envelope.
// The origin and source identity remain fixed across publication recovery.
type GraphStopQuestion struct {
	OwnerStop       *store.OwnerStop      `json:"owner_stop,omitempty"`
	Diagnosis       *store.GraphDiagnosis `json:"diagnosis,omitempty"`
	Version         int                   `json:"version"`
	InstanceID      int64                 `json:"instance_id"`
	SourceAttemptID int64                 `json:"source_attempt_id,omitempty"`
	Origin          string                `json:"origin"`
	Cause           string                `json:"cause"`
	Reason          string                `json:"reason"`
}

func parkGraphAction(ctx context.Context, opts Options, req GraphNodeRequest, out GraphNodeResult, cause string, succeeded bool) (GraphNodeResult, error) {
	node, ok := req.Profile.Node(out.Node)
	if !ok || !graphexec.ParksOutcome(node, cause) {
		return out, errors.New("graph stop lacks declared owner disposition")
	}
	reason := strings.TrimSpace(out.Activity.Diagnostic)
	if reason == "" {
		reason = fmt.Sprintf("Step %q stopped with %s; no usable explanation was submitted.", out.Node, cause)
	}
	// A long explanation must never stop the stop itself from parking.
	if len(reason) > 16<<10 {
		reason = strings.ToValidUTF8(reason[:16<<10], "") + "\n\n[explanation shortened]"
	}
	var reparkReplyID int64
	if out.AttemptID == 0 {
		attempts, err := opts.DB.ListGraphNodeAttempts(context.WithoutCancel(ctx), out.InstanceID)
		if err != nil {
			return out, err
		}
		// A refusal allocates no attempt. Tie it to durable completed progress
		// so a later exhaustion cannot reuse an already consumed owner wait.
		// Append after shortening; retries at the same progress keep exact bytes.
		for _, attempt := range slices.Backward(attempts) {
			if !attempt.FinishedAt.IsZero() {
				reason += fmt.Sprintf("\n\nLatest completed attempt: %d.", attempt.ID)
				break
			}
		}
		// An owner reply cannot renew an exhausted sync budget. Record the new
		// refusal against that reply so the prior claim is parked again, even
		// though no additional attempt was dispatched.
		if node.Action == graphexec.SyncBaseAction {
			wait, err := opts.DB.GetGraphOwnerWait(context.WithoutCancel(ctx), out.InstanceID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return out, err
			}
			if err == nil && wait.State == "claimed" {
				reason += fmt.Sprintf("\n\nContinuation reply: %d.", wait.ReplyID)
				reparkReplyID = wait.ReplyID
			}
		}
	}
	question := GraphStopQuestion{Version: 1, InstanceID: out.InstanceID, SourceAttemptID: out.AttemptID, Origin: out.Node, Cause: cause, Reason: reason}
	if out.AttemptID != 0 {
		attempt, err := opts.DB.GetGraphNodeAttempt(context.WithoutCancel(ctx), out.AttemptID)
		if err != nil {
			return out, err
		}
		if attempt.RunID != 0 {
			run, err := opts.DB.GetRun(context.WithoutCancel(ctx), attempt.RunID)
			if err != nil {
				return out, err
			}
			if stop, ok := run.OwnerStop(); ok {
				question.OwnerStop = &stop
				reason = store.ReasonOwnerStop
				question.Reason = reason
			}
		}
	}
	raw, err := json.Marshal(question)
	if err != nil {
		return out, err
	}
	digest := sha256.Sum256(raw)
	stop := store.GraphStop{ReparkReplyID: reparkReplyID, Key: hex.EncodeToString(digest[:]), InstanceID: out.InstanceID, SourceAttemptID: out.AttemptID, Origin: out.Node, Cause: cause, Reason: reason, Candidate: raw, At: req.At, AllowRunless: node.Action != ResearchAgentAction}
	if out.AttemptID != 0 {
		completion := store.GraphCompletion{Status: store.GraphFailed, FinishedAt: req.At, InstanceStatus: store.GraphRunning}
		if succeeded {
			completion.Status, completion.Outcome = store.GraphSucceeded, out.Activity.Outcome
		}
		if out.Activity.Kind != "" || out.Activity.Output != nil {
			kind := out.Activity.Kind
			if kind == "" {
				kind = store.GraphOutputText
			}
			completion.Outputs = append(completion.Outputs, store.GraphOutput{Kind: kind, Payload: slices.Clone(out.Activity.Output)})
		}
		completion.Outputs = append(completion.Outputs, store.GraphOutput{Kind: store.GraphOutputDiagnostic, Payload: []byte(reason)})
		stop.Completion = &completion
	}
	if err := opts.DB.ParkGraphNode(context.WithoutCancel(ctx), stop); err != nil {
		return out, err
	}
	out.Status, out.NextNode = "waiting_owner", out.Node
	if question.OwnerStop != nil {
		return out, nil
	}
	if opts.ResearchReporter == nil {
		return out, errors.New("graph question saved; factory reporter unavailable, publication remains owed")
	}
	_, err = opts.ResearchReporter.RecoverGraphStopQuestion(ctx, stop.Key)
	return out, err
}

// RecoverGraphStopQuestion reconciles the original journal before any repost.
// Reporter.deliver preserves redacted immutable bytes and observes uncertain posts.
func (r *Reporter) RecoverGraphStopQuestion(ctx context.Context, key string) (Posted, error) {
	intent, err := r.db.GetGraphComment(ctx, key)
	if err != nil {
		return Posted{}, err
	}
	var question GraphStopQuestion
	if err = json.Unmarshal(intent.Candidate, &question); err != nil {
		return Posted{}, fmt.Errorf("decode graph stop question: %w", err)
	}
	if question.Version != 1 || question.InstanceID != intent.InstanceID || question.SourceAttemptID != intent.SourceAttemptID || question.Origin != intent.Node || intent.Kind != "question" || question.Cause == "" || strings.TrimSpace(question.Reason) == "" {
		return Posted{}, errors.New("graph stop question binding mismatch")
	}
	digest := sha256.Sum256(intent.Candidate)
	if hex.EncodeToString(digest[:]) != key {
		return Posted{}, errors.New("graph stop immutable question digest mismatch")
	}
	if question.OwnerStop != nil {
		attempt, err := r.db.GetGraphNodeAttempt(ctx, intent.SourceAttemptID)
		if err != nil {
			return Posted{}, err
		}
		run, err := r.db.GetRun(ctx, attempt.RunID)
		if err != nil {
			return Posted{}, err
		}
		stop, ok := run.OwnerStop()
		if !ok || stop != *question.OwnerStop || attempt.InstanceID != intent.InstanceID {
			return Posted{}, errors.New("owner stop source mismatch")
		}
		return Posted{}, nil
	}
	delivery, err := r.db.GetReportDelivery(ctx, key)
	if err == nil && !delivery.Completed {
		return r.dispatchDelivery(ctx, delivery)
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Posted{}, err
	}
	instance, err := r.db.GetGraphInstance(ctx, intent.InstanceID)
	if err != nil {
		return Posted{}, err
	}
	session, err := r.db.GetSession(ctx, instance.SessionID)
	if err != nil {
		return Posted{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return Posted{}, err
	}
	var runID, triggerID int64
	if intent.SourceAttemptID != 0 {
		attempt, err := r.db.GetGraphNodeAttempt(ctx, intent.SourceAttemptID)
		if err != nil {
			return Posted{}, err
		}
		runID = attempt.RunID
		if runID != 0 {
			run, err := r.db.GetRun(ctx, runID)
			if err != nil {
				return Posted{}, err
			}
			triggerID = run.TriggerCommentID
		}
	}
	delivery = r.deliveryIntent(ref, ref, session.ID, triggerID, runID, "graph-stop:"+key)
	delivery.Key, delivery.Marker = key, "<!-- lf:delivery v=1 key="+key+" -->"
	projection, err := ProjectGraphRecovery(ctx, r.db, session.ID)
	if err != nil {
		return Posted{}, err
	}
	if projection == nil {
		projection = &report.StopRecovery{Step: question.Origin, Reason: question.Reason, Disposition: "stopped", Route: "inspect", Unresolved: "Completion of this step is not confirmed.", NextStep: "Use `/doctor inspect this retained stop and its supported continuation` before repeating work. Diagnosis authorizes no repair."}
	}
	body := fmt.Sprintf("Work stopped at `%s` (%s).\n\n%s", question.Origin, question.Cause, question.Reason)
	return r.deliverRecoveryProjection(ctx, delivery, projection, body)
}

// RecoverGraphStoppedExecution reconciles an orphan agent allocation against
// durable run recovery before parking it. It never dispatches or reruns work.
// A run still active or a runless effect requires its existing factory recovery
// owner first; a missing completion cannot be promoted from a receipt to success.
func RecoverGraphStoppedExecution(ctx context.Context, opts Options, req GraphNodeRequest, attemptID int64) (GraphNodeResult, error) {
	out := GraphNodeResult{InstanceID: req.InstanceID, AttemptID: attemptID}
	if opts.DB == nil || req.Profile == nil || req.At.IsZero() {
		return out, errors.New("graph recovery requires store, pinned profile and factory timestamp")
	}
	attempt, err := opts.DB.GetGraphNodeAttempt(ctx, attemptID)
	if err != nil {
		return out, err
	}
	if attempt.InstanceID != req.InstanceID || (attempt.Status != store.GraphRunning && attempt.Status != store.GraphPending) {
		return out, errors.New("graph recovery allocation mismatch")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return out, err
	}
	definition, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return out, err
	}
	if definition.Digest != req.Profile.Digest || definition.Workflow != "bug" || definition.Team != req.Profile.Team || definition.ProfileVersion != req.Profile.ProfileVersion {
		return out, errors.New("orphan recovery must use exact pinned Bug profile")
	}
	out.Node, out.NextNode = attempt.Node, attempt.Node
	if attempt.RunID == 0 {
		return out, errors.New("orphan factory effect requires effect reconciliation before owner parking")
	}
	run, err := opts.DB.GetRun(ctx, attempt.RunID)
	if err != nil {
		return out, err
	}
	if run.Status == store.RunRunning || run.Status == store.RunPrelaunch {
		return out, errors.New("orphan agent requires durable run recovery before owner parking")
	}
	out.Activity.Diagnostic = "Allocated execution ended without a confirmed graph completion; its prior invocation will not be dispatched again."
	if run.Error != "" {
		out.Activity.Diagnostic += " " + run.Error
	}
	return parkGraphAction(ctx, opts, req, out, "failed", false)
}
