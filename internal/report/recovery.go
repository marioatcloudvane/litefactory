package report

import (
	"fmt"
	"strings"
	"time"
)

// Recovery describes an already-persisted factory action. Rendering never
// creates scheduling or continuation authority.
type Recovery struct {
	ActionID          int64
	DueAt             time.Time
	Status            string
	ManualReason      string
	InspectionCommand string
	NoContinuation    bool
}

func recoverySection(recovery *Recovery) string {
	if recovery == nil {
		return ""
	}
	next := "Inspect on the factory host: `" + recovery.InspectionCommand + "`."
	if recovery.InspectionCommand == "" {
		next = "Ask the factory operator to inspect the retained recovery state in the daemon logs."
	}
	if recovery.ActionID <= 0 {
		disposition := "Whether an automatic action is scheduled is unknown."
		if recovery.NoContinuation {
			disposition = "Nothing is scheduled to resume this interrupted run."
		}
		reason := recoveryCause(recovery.ManualReason)
		if reason == "" {
			reason = "No reason for the scheduling decision was retained."
		}
		return "**Session-limit recovery**\n\n" + disposition + " " + sanitize(reason) + "\n\n" + next
	}
	status := recovery.Status
	if status == "" {
		status = "pending"
	}
	switch status {
	case "pending", "reserved":
		if recovery.DueAt.IsZero() {
			return fmt.Sprintf("**Session-limit recovery**\n\nAutomatic action %d is %s; its eligibility time is unknown.\n\n%s", recovery.ActionID, oneLine(status), next)
		}
		return fmt.Sprintf("**Session-limit recovery**\n\nAutomatic action %d is %s; eligible at %s. Please wait. Eligibility is not a guaranteed start time; admission and capacity still apply. No recovery command is needed.", recovery.ActionID, status, recovery.DueAt.UTC().Format("01/02/2006 15:04:05 UTC"))
	case "running":
		return fmt.Sprintf("**Session-limit recovery**\n\nAutomatic action %d is running. Please wait for its result; completion is not yet confirmed.", recovery.ActionID)
	case "succeeded":
		return fmt.Sprintf("**Session-limit recovery**\n\nAutomatic action %d succeeded; report delivery may settle separately.\n\n%s", recovery.ActionID, next)
	default:
		return fmt.Sprintf("**Session-limit recovery**\n\nAutomatic action %d is %s. %s\n\n%s", recovery.ActionID, oneLine(status), sanitize(recoveryCause(recovery.ManualReason)), next)
	}
}

// recoveryCause removes only known factory advice, retaining diagnostic clauses
// that may themselves contain semicolons. Presentation never rewrites the row.
func recoveryCause(reason string) string {
	for _, advice := range []string{"; resume manually after the limit lifts.", "; manual recovery is required.", "; resume manually"} {
		reason = strings.TrimSuffix(reason, advice)
	}
	return strings.TrimSpace(strings.Replace(reason, "; resume manually: ", ": ", 1))
}

// StopRecovery is a read-only projection of retained graph progress. IDs bind
// the explanation to existing work; they are never execution instructions.
type StopRecovery struct {
	InstanceID, DispatchID, LaunchID, ActionID             int64
	Step, Reason, Unresolved, NextStep, Disposition, Route string
	DueAt                                                  time.Time
}

// RenderStopRecovery gives one supported next step without claiming recovery.
func RenderStopRecovery(stop *StopRecovery) string {
	if stop == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Work at `%s`: %s\n\n%s", oneLine(stop.Step), oneLine(stop.Disposition), sanitize(stop.Reason))
	if stop.Unresolved != "" {
		fmt.Fprintf(&b, "\n\nUnresolved: %s", sanitize(stop.Unresolved))
	}
	fmt.Fprintf(&b, "\n\n%s", sanitize(stop.NextStep))
	return b.String()
}
