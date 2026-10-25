package graphexec

import (
	"fmt"
	"slices"
)

// withoutExplicitSync checks success-only sync chains before applying an
// existing policy to the unchanged authorization topology.
func withoutExplicitSync(p *Profile) (*Profile, error) {
	if p == nil {
		return nil, nil
	}
	resolve := func(name string) (string, error) {
		seen := map[string]bool{}
		for {
			n, ok := p.Node(name)
			if !ok || n.Action != SyncBaseAction {
				return name, nil
			}
			if seen[name] {
				return "", fmt.Errorf("base sync success cycle at %q", name)
			}
			seen[name] = true
			if n.Limit < 1 {
				return "", fmt.Errorf("base sync %q requires a finite bound", name)
			}
			if (len(n.Outcomes) != 4 && len(n.Outcomes) != 5) || !slices.Contains(n.Outcomes, "synced") || !slices.Contains(n.Outcomes, "current") || !slices.Contains(n.Outcomes, "conflict") || !slices.Contains(n.Outcomes, "retry") {
				return "", fmt.Errorf("base sync %q requires its four outcomes", name)
			}
			synced, a := Decide(p, name, "synced")
			current, b := Decide(p, name, "current")
			if !a || !b || synced != current {
				return "", fmt.Errorf("base sync %q success outcomes must continue to the same node", name)
			}
			if slices.Contains(n.Outcomes, "deferred") {
				deferred, routed := Decide(p, name, "deferred")
				if !routed || deferred != synced {
					return "", fmt.Errorf("base sync %q deferred outcome must continue with permitted work", name)
				}
			}
			for _, outcome := range []string{"conflict", "retry"} {
				next, routed := Decide(p, name, outcome)
				target, exists := p.Node(next)
				stopped := ParksOutcome(n, outcome) && !routed || routed && exists && target.Terminal
				if !stopped {
					return "", fmt.Errorf("base sync %q must stop on %s", name, outcome)
				}
			}
			name = synced
		}
	}
	projected := *p
	projected.Nodes = nil
	projected.Edges = nil
	for _, n := range p.Nodes {
		if n.Action == SyncBaseAction {
			if _, err := resolve(n.Name); err != nil {
				return nil, err
			}
			continue
		}
		projected.Nodes = append(projected.Nodes, n)
	}
	for _, e := range p.Edges {
		from, ok := p.Node(e.From)
		if ok && from.Action == SyncBaseAction {
			continue
		}
		next, err := resolve(e.To)
		if err != nil {
			return nil, err
		}
		e.To = next
		projected.Edges = append(projected.Edges, e)
	}
	start, err := resolve(p.Start)
	if err != nil {
		return nil, err
	}
	projected.Start = start
	return &projected, nil
}
