package graph

import "slices"

// Decide navigates a validated, frozen profile without running validation or
// dispatch. Undeclared outcomes and ambiguous targets return no target and false.
func Decide(p *Graph, node, outcome string) (string, bool) {
	n, ok := p.Node(node)
	if !ok || n.Terminal || !slices.Contains(n.Outcomes, outcome) {
		return "", false
	}
	var target string
	count := 0
	for _, edge := range p.Edges {
		if edge.From == node && edge.On == outcome {
			target = edge.To
			count++
		}
	}
	if count != 1 || target == "" {
		return "", false
	}
	return target, true
}
