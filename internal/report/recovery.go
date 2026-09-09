package report

import (
	"fmt"
	"time"
)

// Recovery describes an already-persisted factory action, never an instruction
// parsed out of agent text. Rendering retains local control of command syntax.
type Recovery struct {
	ActionID     int64
	DueAt        time.Time
	ManualReason string
}

func recoverySection(recovery *Recovery) string {
	if recovery == nil {
		return ""
	}
	if recovery.ActionID <= 0 || recovery.DueAt.IsZero() {
		return "**Session-limit recovery**\n\n" + sanitize(recovery.ManualReason)
	}
	return fmt.Sprintf("**Session-limit recovery**\n\nAutomatic action %d is persisted. The factory will continue this conversation when admission permits:\n\n`/schedule -to %s Continue the interrupted request in the original conversation.`\n\nThis factory announcement describes existing work; it does not create another schedule.", recovery.ActionID, recovery.DueAt.UTC().Format("01/02/2006 15:04:05 UTC"))
}
