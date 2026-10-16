package teamrelease

import "github.com/cloudvane/litefactory/internal/graphexec"

// Editable Implement sources may carry the worker gate before the owner releases
// them. Removing that validated addition must recover the released behavior;
// every other execution attribute and coordinator route still has to match.
func pendingWorkerSyncMatchesRelease(live, released []byte) bool {
	current, err := graphexec.ParseProfile(live)
	if err != nil || graphexec.ValidateImplementGraph(current) != nil {
		return false
	}
	previous, err := graphexec.ParseProfile(released)
	if err != nil {
		return false
	}
	gate, gated := graphexec.ImplementWorkerSyncNode(current)
	if !gated {
		return false
	}
	if _, alreadyGated := graphexec.ImplementWorkerSyncNode(previous); alreadyGated {
		return false
	}
	worker, ok := graphexec.Decide(current, gate.Name, "synced")
	if !ok {
		return false
	}
	normalized := *current
	normalized.Nodes = nil
	for _, node := range current.Nodes {
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
	want, err := graphexec.SemanticDigest(previous)
	if err != nil {
		return false
	}
	got, err := graphexec.SemanticDigest(&normalized)
	return err == nil && got == want
}
