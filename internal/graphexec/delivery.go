package graphexec

import (
	"github.com/cloudvane/litefactory/internal/agent"
)

// DeliverAction reconciles a factory-owned candidate before and after owner review.
const DeliverAction = "lf_deliver"

// DeliverSpec exposes mechanics, never permission to merge.
func DeliverSpec(mode agent.Mode) ActionSpec {
	attrs := OwnerStopAttributes()
	attrs["lf_operation"] = AttributeSpec{Required: true, Values: []string{"push", "merge", "close"}}
	attrs["lf_owner_continuation"] = AttributeSpec{Values: []string{"required"}}
	return ActionSpec{Name: DeliverAction, Mode: mode, Outcomes: []string{"ready", "unchanged", "merged", "completed", "revalidate", "stale", "blocked", "done", "closed", "open"}, Attributes: attrs}
}

// ValidateDeliveryAction identifies a delivery capability without changing the
// captured node identity. Validate's effect ledger remains its admission gate.
func ValidateDeliveryAction(n Node) string {
	if n.Action != DeliverAction {
		return n.Action
	}
	switch n.Attrs["lf_operation"] {
	case "push":
		return ValidatePushAction
	case "merge":
		return ValidateMergeAction
	case "close":
		return ValidateCloseAction
	default:
		return ""
	}
}
