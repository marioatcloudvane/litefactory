package turn

import (
	"context"
	"errors"
	"fmt"

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
	out := &report.StopRecovery{InstanceID: instance.ID, Step: instance.CurrentNode, Disposition: instance.Status, Route: "inspect",
		Reason: "The retained graph has work at this step.", Unresolved: "Completion of this step is not confirmed.",
		NextStep: "Use `/doctor inspect the latest stopped step and its saved launch; identify one supported continuation` before repeating work. Diagnosis authorizes no repair."}
	if instance.Status == store.GraphSucceeded {
		out.Route = "completed"
		out.Reason = "The graph records successful completion."
		out.Unresolved = "Any outstanding report or external effect must settle through its existing reconciliation."
		out.NextStep = "Read the retained result; do not reopen completed work because a report is missing."
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
	out.Disposition = dispatch.Status
	if dispatch.Node != instance.CurrentNode {
		return out, nil
	}
	switch dispatch.Status {
	case "pending":
		out.Route = "wait-scheduled"
		out.Reason = fmt.Sprintf("Graph action %d is pending; eligible at %s.", dispatch.ID, dispatch.DueAt.UTC().Format("01/02/2006 15:04:05 UTC"))
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
				out.NextStep = "Use `/doctor inspect the active attempt blocking this cancelled dispatch` before attempting continuation. Diagnosis authorizes no repair."
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
		out.NextStep = "Use `/doctor inspect the missing saved launch for the latest stopped step` to identify a supported successor or operator action. Diagnosis authorizes no repair."
	}
	return out, nil
}
