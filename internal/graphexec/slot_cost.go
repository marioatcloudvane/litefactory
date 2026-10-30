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
