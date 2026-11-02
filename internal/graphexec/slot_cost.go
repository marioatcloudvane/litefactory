package graphexec

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

const slotBudgetAttribute = "lf_slot_budget"

// SlotCost resolves execution capacity from the admitted workflow definition.
// Effective node attributes (including DOT defaults) override the graph value.
// A terminal performs no activity and requires no reservation.
func SlotCost(p *Profile, nodeName string) (int, error) {
	if p == nil {
		return 0, errors.New("slot cost requires a retained graph profile")
	}
	node, ok := p.Node(nodeName)
	if !ok {
		return 0, fmt.Errorf("slot cost node %q is undeclared or ambiguous", nodeName)
	}
	graphCost, err := slotCostAttribute(p.Attrs)
	if err != nil {
		return 0, fmt.Errorf("graph: %w", err)
	}
	nodeCost, err := slotCostAttribute(node.Attrs)
	if err != nil {
		return 0, fmt.Errorf("node %q: %w", nodeName, err)
	}
	if node.Terminal {
		return 0, nil
	}
	cost := graphCost
	if _, supplied := node.Attrs[slotBudgetAttribute]; supplied {
		cost = nodeCost
	}
	if node.Action == PlanParallelAction {
		group, err := DecodePlanParallel(p, node)
		if err != nil {
			return 0, err
		}
		for _, member := range group.Members {
			memberCost, err := SlotCost(p, member)
			if err != nil {
				return 0, err
			}
			if cost > math.MaxInt-memberCost {
				return 0, errors.New("parallel group slot cost overflows int")
			}
			cost += memberCost
		}
	}
	return cost, nil
}

func slotCostAttribute(attrs map[string]string) (int, error) {
	value, supplied := attrs[slotBudgetAttribute]
	if !supplied {
		return 1, nil
	}
	if value != "" {
		valid := true
		for _, c := range value {
			if c < '0' || c > '9' {
				valid = false
				break
			}
		}
		if valid {
			cost, err := strconv.Atoi(value)
			if err == nil && cost > 0 {
				return cost, nil
			}
		}
	}
	return 0, fmt.Errorf("lf_slot_budget %q must be a positive decimal integer representable as int", value)
}

// AdmissionSlotCost is the capacity a node must hold before it may start. It
// equals SlotCost for every node except a Plan parallel group. The group's
// coordinator runs no agent of its own while its members run, so its grant
// doubles as the first member lane: the group needs only the larger of its own
// budget and its largest member, and runs its members one at a time on that
// grant. Requiring the full SlotCost instead starves the group whenever any
// other work holds a slot, because single-slot work takes every freed slot.
// SlotCost remains the ceiling the group may grow to for full parallelism.
func AdmissionSlotCost(p *Profile, nodeName string) (int, error) {
	full, err := SlotCost(p, nodeName)
	if err != nil {
		return 0, err
	}
	node, _ := p.Node(nodeName)
	if node.Action != PlanParallelAction {
		return full, nil
	}
	cost, err := slotCostAttribute(p.Attrs)
	if err != nil {
		return 0, err
	}
	if _, supplied := node.Attrs[slotBudgetAttribute]; supplied {
		if cost, err = slotCostAttribute(node.Attrs); err != nil {
			return 0, err
		}
	}
	group, err := DecodePlanParallel(p, node)
	if err != nil {
		return 0, err
	}
	for _, member := range group.Members {
		memberCost, err := SlotCost(p, member)
		if err != nil {
			return 0, err
		}
		cost = max(cost, memberCost)
	}
	return min(cost, full), nil
}
