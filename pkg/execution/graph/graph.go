// Package graph supplies DOT mechanics, structural validation and navigation.
package graph

import (
	"maps"
	"slices"
)

// Graph is a caller-interpreted directed graph. It holds no execution state.
type Graph struct {
	Start string
	Nodes []Node
	Edges []Edge
	Attrs map[string]string
}

// Node describes a bounded activity or a terminal. Zero Limit means no explicit
// dispatch bound and is permitted only outside cycles.
type Node struct {
	Name     string
	Action   string
	Outcomes []string
	Limit    int
	Terminal bool
	Attrs    map[string]string
}

// Edge selects the destination for exactly one declared outcome.
type Edge struct {
	From  string
	To    string
	On    string
	Attrs map[string]string
}

// Node returns an independent copy of the named node. Validated profiles have
// unique names; ambiguous unvalidated names are refused.
func (p *Graph) Node(name string) (Node, bool) {
	if p == nil {
		return Node{}, false
	}
	var found *Node
	for i := range p.Nodes {
		if p.Nodes[i].Name == name {
			if found != nil {
				return Node{}, false
			}
			found = &p.Nodes[i]
		}
	}
	if found == nil {
		return Node{}, false
	}
	n := *found
	n.Outcomes = slices.Clone(n.Outcomes)
	n.Attrs = maps.Clone(n.Attrs)
	return n, true
}
