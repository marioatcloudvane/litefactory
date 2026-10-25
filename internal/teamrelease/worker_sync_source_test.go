package teamrelease

import (
	"maps"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

// Editable Implement sources may carry the worker gate and the 120-minute
// story-worker budget before release. Removing only those additions must recover
// the released behavior. Every other execution attribute and coordinator route
// still has to match.
func pendingWorkerSyncMatchesRelease(live, released []byte) bool {
	current, err := graphexec.ParseProfile(live)
	if err != nil || graphexec.ValidateImplementGraph(current) != nil {
		return false
	}
	previous, err := graphexec.ParseProfile(released)
	if err != nil {
		return false
	}
	return pendingWorkerSourceMatchesRelease(current, previous)
}

func pendingWorkerSourceMatchesRelease(current, previous *graphexec.Profile) bool {
	normalized := *current
	normalized.Nodes = nil
	changed := false
	for _, node := range current.Nodes {
		previousNode, exists := previous.Node(node.Name)
		if node.Name == "worker_agent" && node.Action == "lf_agent" && node.Attrs["lf_agent_markdown"] == "story-worker" &&
			node.Attrs[graphexec.BudgetAttribute] == "120" && exists {
			if _, hadBudget := previousNode.Attrs[graphexec.BudgetAttribute]; !hadBudget {
				node.Attrs = maps.Clone(node.Attrs)
				delete(node.Attrs, graphexec.BudgetAttribute)
				changed = true
			}
		}
		normalized.Nodes = append(normalized.Nodes, node)
	}
	gate, gated := graphexec.ImplementWorkerSyncNode(current)
	_, alreadyGated := graphexec.ImplementWorkerSyncNode(previous)
	if gated && !alreadyGated {
		worker, ok := graphexec.Decide(current, gate.Name, "synced")
		if !ok {
			return false
		}
		nodes := normalized.Nodes
		normalized.Nodes = nil
		for _, node := range nodes {
			if node.Name != gate.Name {
				normalized.Nodes = append(normalized.Nodes, node)
			}
		}
		normalized.Edges = nil
		for _, edge := range current.Edges {
			if edge.From == gate.Name {
				continue
			}
			if edge.To == gate.Name {
				edge.To = worker
			}
			normalized.Edges = append(normalized.Edges, edge)
		}
		changed = true
	}
	if !changed {
		return false
	}
	want, err := graphexec.SemanticDigest(previous)
	if err != nil {
		return false
	}
	got, err := graphexec.SemanticDigest(&normalized)
	return err == nil && got == want
}
