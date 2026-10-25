package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// ProjectGraphRecovery observes current retained work only. Admission remains
// with the ordinary owner-reply and workflow handlers, which recheck authority.
func ProjectGraphRecovery(ctx context.Context, db *store.DB, sessionID int64) (*report.StopRecovery, error) {
	instance, err := db.LatestSessionGraphInstance(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	session, err := db.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	inspect := "Ask the factory operator to inspect on the factory host: `" + report.TaskInspectionCommand(session.TaskRef) + "`."
	out := &report.StopRecovery{InstanceID: instance.ID, Step: instance.CurrentNode, Disposition: instance.Status, Route: "inspect",
		Reason: "The retained graph has work at this step.", Unresolved: "Completion of this step is not confirmed.",
		NextStep: inspect}
	if instance.Status == store.GraphSucceeded {
		out.Route = "completed"
		out.Reason = "The graph records successful completion."
		out.Unresolved = "Any outstanding report or external effect must settle through its existing reconciliation."
		out.NextStep = inspect
		return out, nil
	}
	if instance.Status != store.GraphRunning {
		dispatch, e := db.LatestGraphDispatch(ctx, instance.ID)
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return nil, e
		}
		if e == nil && dispatch.CancellationReason != "" {
			out.Reason = dispatch.CancellationReason
			out.DispatchID = dispatch.ID
		}
		if e == nil && dispatch.AllocatedRunID > 0 {
			if e := projectRecoveryRun(ctx, db, sessionID, dispatch.AllocatedRunID, out); e != nil {
				return nil, e
			}
		}
		return out, nil
	}
	wait, err := db.GetGraphOwnerWait(ctx, instance.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		switch wait.State {
		case "waiting":
			out.Route = "reply-question"
			out.Disposition = "waiting for owner input"
			out.Reason = "The current stopped step has a retained question."
			out.NextStep = "Reply to the published question with guidance to continue this step. Your reply does not approve an unapproved repair."
			return out, nil
		case "claimed":
			out.Route = "wait-admitted"
			out.Disposition = "continuation admitted"
			out.Reason = "The owner's reply is retained for the current stopped step."
			out.NextStep = "Please wait for the admitted continuation; completion is not yet confirmed."
			return out, nil
		}
	}
	dispatch, err := db.LatestGraphDispatch(ctx, instance.ID)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out.DispatchID, out.LaunchID, out.ActionID, out.DueAt = dispatch.ID, dispatch.LaunchID, dispatch.ID, dispatch.DueAt
	if dispatch.Node != instance.CurrentNode {
		out.Unresolved = fmt.Sprintf("The latest dispatch belongs to step %s; no dispatch for this current step is confirmed.", dispatch.Node)
		return out, nil
	}
	out.Disposition = dispatch.Status
	switch dispatch.Status {
	case "pending":
		out.Route = "wait-scheduled"
		out.Reason = fmt.Sprintf("Graph action %d is pending; eligible at %s.", dispatch.ID, dispatch.DueAt.UTC().Format("01/02/2006 15:04:05 UTC"))
		if dispatch.DueAt.IsZero() {
			out.Reason = fmt.Sprintf("Graph action %d is pending; its eligibility time is unknown.", dispatch.ID)
			out.NextStep = inspect
			return out, nil
		}
		out.NextStep = "Please wait. Eligibility is not a guaranteed start time; admission and capacity still apply. No recovery command is needed."
	case "reserved", "running":
		out.Route = "wait-running"
		out.Reason = fmt.Sprintf("Graph action %d is %s.", dispatch.ID, dispatch.Status)
		out.NextStep = "Please wait for its result; completion is not yet confirmed."
	case "cancelled":
		out.Reason = dispatch.CancellationReason
		if out.Reason == "" {
			out.Reason = "The dispatch was cancelled."
		}
		if dispatch.AllocatedRunID != 0 {
			if e := projectRecoveryRun(ctx, db, sessionID, dispatch.AllocatedRunID, out); e != nil {
				return nil, e
			}
			return out, nil
		}
		if !instance.NextDueAt.IsZero() {
			out.Route = "wait-scheduled"
			out.NextStep = "Please wait for the already scheduled continuation. Eligibility is not a guaranteed start time."
			return out, nil
		}
		attempts, err := db.ListGraphNodeAttempts(ctx, instance.ID)
		if err != nil {
			return nil, err
		}
		for _, attempt := range attempts {
			if attempt.Status == store.GraphPending || attempt.Status == store.GraphRunning {
				out.Route = "inspect-attempt"
				out.Unresolved = "The retained attempt has no run record establishing whether execution is active."
				out.NextStep = inspect
				if attempt.RunID > 0 {
					if e := projectRecoveryRun(ctx, db, sessionID, attempt.RunID, out); e != nil {
						return nil, e
					}
				}
				return out, nil
			}
		}
		runs, err := db.ListRuns(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if run.Status == store.RunRunning || run.Status == store.RunPrelaunch {
				out.Route = "wait-run"
				out.NextStep = "Please wait for the existing run to settle before continuing this stopped step."
				return out, nil
			}
		}
		if dispatch.LaunchID > 0 {
			capture, e := db.GetWorkflowCapture(ctx, dispatch.LaunchID)
			if e != nil && !errors.Is(e, store.ErrNotFound) {
				return nil, e
			}
			if e == nil && capture.InstanceID == instance.ID && capture.SessionID == sessionID {
				out.Route = "reply-saved"
				out.NextStep = "Reply on this open issue with guidance to resume this saved step once. Retained findings, answers, instructions and edits remain available; settled actions are not repeated."
				return out, nil
			}
		} else if _, e := db.ResearchLaunchFiles(ctx, instance.ID); e == nil {
			out.Route = "reply-saved"
			out.NextStep = "Reply on this open issue with guidance to resume this saved step once. Retained findings, answers, instructions and edits remain available; settled actions are not repeated."
			return out, nil
		} else if !errors.Is(e, store.ErrNotFound) {
			return nil, e
		}
		out.Route = "inspect-launch"
		out.Unresolved = "This stopped step has no saved launch to resume from; an ordinary reply cannot dispatch it."
		out.NextStep = inspect
	}
	if dispatch.Status != "pending" && dispatch.Status != "reserved" && dispatch.Status != "running" && dispatch.AllocatedRunID > 0 {
		if err := projectRecoveryRun(ctx, db, sessionID, dispatch.AllocatedRunID, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// projectRecoveryRun correlates the affected run rather than defaulting to the
// newest run, which may belong to different work in the session.
func projectRecoveryRun(ctx context.Context, db *store.DB, sessionID, runID int64, out *report.StopRecovery) error {
	run, err := db.GetRun(ctx, runID)
	if errors.Is(err, store.ErrNotFound) {
		out.Unresolved = "The allocated run record is missing; execution status is unknown."
		return nil
	}
	if err != nil {
		return err
	}
	if run.SessionID != sessionID {
		return fmt.Errorf("graph recovery run belongs to a different session")
	}
	session, err := db.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	out.Unresolved = ""
	if run.Status == store.RunRunning || run.Status == store.RunPrelaunch {
		out.Route = "wait-run"
		out.Reason += fmt.Sprintf(" Run %d is %s.", run.Ordinal, run.Status)
		out.NextStep = "Please wait for the existing run to settle; no owner action is needed."
		return nil
	}
	out.Route = "inspect-run"
	out.Reason += fmt.Sprintf(" Run %d has retained status %s.", run.Ordinal, run.Status)
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return err
	}
	out.NextStep = fmt.Sprintf("Inspect run %d on the factory host: `%s`.", run.Ordinal, report.RunInspectionCommand(ref, run.Ordinal))
	return nil
}
