package graphexec

import (
	"fmt"
	"strconv"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/pkg/execution/graph"
)

// Profile is a byte-digested workflow definition, frozen after construction.
type Profile struct {
	Mode           agent.Mode
	Workflow       agent.Workflow `json:",omitempty"`
	Team           string
	ProfileVersion int
	Digest         string
	Start          string
	Nodes          []Node
	Edges          []Edge
	Attrs          map[string]string
}

// Node retains the factory wire representation using a neutral value.
type Node = graph.Node

// Edge retains the factory wire representation using a neutral value.
type Edge = graph.Edge

func (p *Profile) neutral() *graph.Graph {
	if p == nil {
		return nil
	}
	return &graph.Graph{Start: p.Start, Nodes: p.Nodes, Edges: p.Edges, Attrs: p.Attrs}
}

// Node returns an independent copy of the named node. Validated profiles have
// unique names; ambiguous unvalidated names are refused.
func (p *Profile) Node(name string) (Node, bool) { return p.neutral().Node(name) }

// StepBudgetAttribute names the optional graph attribute that caps how many
// node dispatches one instance may make without an owner reply.
const StepBudgetAttribute = "lf_step_budget"

// DefaultStepBudget applies when a graph declares no step budget. It protects
// money and quota against a graph that runs on in circles; every accepted owner
// reply starts a fresh budget.
const DefaultStepBudget = 200

// MaxStepBudget bounds a declared step budget, so a typo cannot turn the guard off.
const MaxStepBudget = 10000

// StepBudget is the number of dispatches the instance may make since its most
// recent accepted owner reply.
func (p *Profile) StepBudget() int {
	if p == nil {
		return DefaultStepBudget
	}
	budget, err := stepBudget(p.Attrs)
	if err != nil {
		return DefaultStepBudget
	}
	return budget
}

func stepBudget(attrs map[string]string) (int, error) {
	value, present := attrs[StepBudgetAttribute]
	if !present {
		return DefaultStepBudget, nil
	}
	budget, err := strconv.Atoi(value)
	if err != nil || budget < 1 || budget > MaxStepBudget {
		return 0, fmt.Errorf("%s %q must be a whole number from 1 to %d", StepBudgetAttribute, value, MaxStepBudget)
	}
	return budget, nil
}
