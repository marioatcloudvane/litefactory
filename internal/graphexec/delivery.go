package graphexec

import (
	"fmt"
	"slices"

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

// validateDeliveryNode constrains mechanical return and owner approval routes.
func validateDeliveryNode(p *Profile, n Node) error {
	source, ok := p.Node(n.Attrs["lf_source"])
	if !ok || source.Action != "lf_agent" {
		return fmt.Errorf("delivery %q needs an agent source", n.Name)
	}
	operation := n.Attrs["lf_operation"]
	want := []string{"ready", "unchanged", "revalidate", "blocked"}
	switch operation {
	case "merge":
		want = []string{"merged", "revalidate", "blocked"}
	case "close":
		want = []string{"completed", "revalidate", "blocked"}
	case "push":
	default:
		return fmt.Errorf("invalid delivery operation at %q", n.Name)
	}
	if len(n.Outcomes) != len(want) {
		return fmt.Errorf("delivery %q has invalid outcomes", n.Name)
	}
	for _, outcome := range want {
		if !slices.Contains(n.Outcomes, outcome) {
			return fmt.Errorf("delivery %q lacks %s", n.Name, outcome)
		}
		if ParksOutcome(n, outcome) {
			continue
		}
		next, exists := Decide(p, n.Name, outcome)
		target, found := p.Node(next)
		if !exists || !found {
			return fmt.Errorf("delivery %q lacks %s route or owner stop", n.Name, outcome)
		}
		switch outcome {
		case "revalidate":
			if next != source.Name {
				return fmt.Errorf("delivery revalidation must return to its source")
			}
		case "ready":
			if target.Action != "lf_ask" || target.Attrs["lf_kind"] != "review" || target.Attrs["lf_approve_reply"] == "" || target.Attrs["lf_source"] != source.Name {
				return fmt.Errorf("prepared delivery requires its owner review")
			}
		case "unchanged":
			if target.Action != "lf_comment" || target.Attrs["lf_kind"] != "final" || target.Attrs["lf_source"] != source.Name {
				return fmt.Errorf("unchanged delivery requires its final report")
			}
		case "merged":
			if target.Action != DeliverAction || target.Attrs["lf_operation"] != "close" || target.Attrs["lf_source"] != source.Name {
				return fmt.Errorf("merged delivery requires its close operation")
			}
		case "completed":
			if !target.Terminal {
				return fmt.Errorf("confirmed delivery must end at a terminal")
			}
		default:
			return fmt.Errorf("blocked delivery must park for its owner")
		}
	}
	if operation == "merge" || operation == "close" {
		admitted := false
		for _, e := range p.Edges {
			if e.To == n.Name {
				review, _ := p.Node(e.From)
				allowed := operation == "merge" && e.On == "approved" && review.Action == "lf_ask" && review.Attrs["lf_approve_reply"] != "" && review.Attrs["lf_source"] == source.Name
				allowed = allowed || operation == "close" && e.On == "merged" && review.Action == DeliverAction && review.Attrs["lf_operation"] == "merge" && review.Attrs["lf_source"] == source.Name
				if !allowed {
					return fmt.Errorf("shipping requires an explicit owner approval edge")
				}
				admitted = true
			}
		}
		if !admitted {
			return fmt.Errorf("shipping has no owner review")
		}
	}
	return nil
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
