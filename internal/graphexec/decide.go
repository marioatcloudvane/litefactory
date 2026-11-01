package graphexec

import "github.com/cloudvane/litefactory/pkg/execution/graph"

// Decide navigates without dispatch or factory policy.
func Decide(p *Profile, node, outcome string) (string, bool) {
	return graph.Decide(p.neutral(), node, outcome)
}
