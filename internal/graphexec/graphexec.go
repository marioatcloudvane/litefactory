package graphexec

import (
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
