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
	attrs["lf_phase"] = AttributeSpec{Required: true, Values: []string{"prepare", "ship"}}
	return ActionSpec{Name: DeliverAction, Mode: mode, Outcomes: []string{"ready", "unchanged", "completed", "revalidate", "blocked"}, Attributes: attrs}
}

// validateDeliveryNode constrains mechanical return and owner approval routes.
func validateDeliveryNode(p *Profile, n Node) error {
	source, ok := p.Node(n.Attrs["lf_source"])
	if !ok || source.Action != "lf_agent" {
		return fmt.Errorf("delivery %q needs an agent source", n.Name)
	}
	phase := n.Attrs["lf_phase"]
	want := []string{"ready", "unchanged", "revalidate", "blocked"}
	if phase == "ship" {
		want = []string{"completed", "revalidate", "blocked"}
	} else if phase != "prepare" {
		return fmt.Errorf("invalid delivery phase at %q", n.Name)
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
		case "completed":
			if !target.Terminal {
				return fmt.Errorf("confirmed delivery must end at a terminal")
			}
		default:
			return fmt.Errorf("blocked delivery must park for its owner")
		}
	}
	if phase == "ship" {
		admitted := false
		for _, e := range p.Edges {
			if e.To == n.Name {
				review, _ := p.Node(e.From)
				if e.On != "approved" || review.Action != "lf_ask" || review.Attrs["lf_approve_reply"] == "" || review.Attrs["lf_source"] != source.Name {
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
