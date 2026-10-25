package report

import (
	"fmt"
	"strings"
	"time"
)

// Recovery describes an already-persisted factory action. Rendering never
// creates scheduling or continuation authority.
type Recovery struct {
	ActionID     int64
	DueAt        time.Time
	Status       string
	ManualReason string
}

func recoverySection(recovery *Recovery) string {
	if recovery == nil {
		return ""
	}
	if recovery.ActionID <= 0 {
		return "**Session-limit recovery**\n\n" + sanitize(recovery.ManualReason)
	}
	status := recovery.Status
	if status == "" {
		status = "pending"
	}
	switch status {
	case "pending", "reserved":
		if recovery.DueAt.IsZero() {
			return "**Session-limit recovery**\n\nThe automatic action has no confirmed eligibility time. Inspect its retained state before continuing."
		}
		return fmt.Sprintf("**Session-limit recovery**\n\nAutomatic action %d is %s; eligible at %s. Please wait. Eligibility is not a guaranteed start time; admission and capacity still apply. No recovery command is needed.", recovery.ActionID, status, recovery.DueAt.UTC().Format("01/02/2006 15:04:05 UTC"))
	case "running":
		return fmt.Sprintf("**Session-limit recovery**\n\nAutomatic action %d is running. Please wait for its result; completion is not yet confirmed.", recovery.ActionID)
	case "succeeded":
		return fmt.Sprintf("**Session-limit recovery**\n\nAutomatic action %d succeeded. Consult its retained result for confirmed completed work and remaining stops; report delivery may settle separately.", recovery.ActionID)
	default:
		return fmt.Sprintf("**Session-limit recovery**\n\nAutomatic action %d is %s. %s\n\nInspect the retained result before providing further guidance; do not repeat uncertain effects.", recovery.ActionID, oneLine(status), sanitize(recovery.ManualReason))
	}
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
