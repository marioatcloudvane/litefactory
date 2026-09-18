package graphexec

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
)

// Validate aggregates independent faults in declaration order. Bounded cycles
// are valid when every node is reachable and has a route to a terminal.
func Validate(p *Profile, registry Registry) error {
	if p == nil {
		return errors.New("graph profile is nil")
	}
	var faults []error
	if strings.TrimSpace(p.Team) == "" {
		faults = append(faults, errors.New("graph team is empty"))
	}
	if !p.Mode.Valid() {
		faults = append(faults, fmt.Errorf("graph mode %q is invalid", p.Mode))
	}
	if p.ProfileVersion < 1 {
		faults = append(faults, fmt.Errorf("profile_version %d must be positive", p.ProfileVersion))
	}
	nodes := make(map[string]Node, len(p.Nodes))
	var terminals []string
	for _, node := range p.Nodes {
		if strings.TrimSpace(node.Name) == "" {
			faults = append(faults, errors.New("node name is empty"))
		}
		if _, exists := nodes[node.Name]; exists {
			faults = append(faults, fmt.Errorf("duplicate node name %q", node.Name))
		} else {
			nodes[node.Name] = node
		}
		if node.Terminal {
			terminals = append(terminals, node.Name)
		}
		faults = append(faults, validateNode(node, p.Mode, registry)...)
	}
	if _, exists := nodes[p.Start]; !exists || p.Start == "" {
		faults = append(faults, fmt.Errorf("start node %q is undeclared", p.Start))
	}
	if len(terminals) == 0 {
		faults = append(faults, errors.New("graph has no terminal node"))
	}
	forward := make(map[string][]string)
	reverse := make(map[string][]string)
	counts := make(map[string]map[string]int)
	for _, edge := range p.Edges {
		from, fromOK := nodes[edge.From]
		_, toOK := nodes[edge.To]
		if !fromOK {
			faults = append(faults, fmt.Errorf("edge from undeclared node %q", edge.From))
		}
		if !toOK {
			faults = append(faults, fmt.Errorf("edge to undeclared node %q", edge.To))
		}
		if fromOK && from.Terminal {
			faults = append(faults, fmt.Errorf("terminal node %q has outgoing edge", edge.From))
		} else if fromOK && !slices.Contains(from.Outcomes, edge.On) {
			faults = append(faults, fmt.Errorf("node %q edge outcome %q is undeclared", edge.From, edge.On))
		}
		if counts[edge.From] == nil {
			counts[edge.From] = make(map[string]int)
		}
		counts[edge.From][edge.On]++
		if fromOK && toOK {
			forward[edge.From] = append(forward[edge.From], edge.To)
			reverse[edge.To] = append(reverse[edge.To], edge.From)
		}
	}
	reachable := walk([]string{p.Start}, forward)
	canTerminate := walk(terminals, reverse)
	for _, node := range p.Nodes {
		for _, outcome := range node.Outcomes {
			if count := counts[node.Name][outcome]; count != 1 {
				faults = append(faults, fmt.Errorf("node %q outcome %q has %d edges; want exactly one", node.Name, outcome, count))
			}
		}
		if !reachable[node.Name] {
			if node.Terminal {
				faults = append(faults, fmt.Errorf("unreachable terminal node %q", node.Name))
			} else {
				faults = append(faults, fmt.Errorf("unreachable node %q", node.Name))
			}
		}
		if len(terminals) > 0 && !canTerminate[node.Name] {
			faults = append(faults, fmt.Errorf("node %q has no route to a terminal", node.Name))
		}
		// Starting at successors, rather than the node itself, detects precisely
		// cycle membership, including self-loops, without rejecting legal cycles.
		if node.Limit <= 0 && walk(forward[node.Name], forward)[node.Name] {
			faults = append(faults, fmt.Errorf("node %q is on a cycle without positive lf_limit", node.Name))
		}
	}
	return errors.Join(faults...)
}

func validateNode(node Node, mode agent.Mode, registry Registry) []error {
	var faults []error
	if node.Limit < 0 || (node.Attrs["lf_limit"] != "" && node.Limit == 0) {
		faults = append(faults, fmt.Errorf("node %q lf_limit must be positive when supplied", node.Name))
	}
	if err := validateInvokeAttrs(node.Attrs); err != nil {
		faults = append(faults, fmt.Errorf("node %q: %w", node.Name, err))
	}
	if node.Terminal {
		if node.Action != "" || len(node.Outcomes) != 0 {
			faults = append(faults, fmt.Errorf("terminal node %q must not have action or outcomes", node.Name))
		}
		return faults
	}
	spec, exists := registry.Resolve(node.Action)
	if !exists {
		faults = append(faults, fmt.Errorf("node %q has unknown action %q", node.Name, node.Action))
	} else if spec.Mode != mode {
		faults = append(faults, fmt.Errorf("node %q action %q mode %q does not match graph mode %q", node.Name, node.Action, spec.Mode, mode))
	}
	if !validActionName(node.Action, mode) {
		faults = append(faults, fmt.Errorf("node %q action %q must use lf_%s_<capability>", node.Name, node.Action, mode))
	}
	if len(node.Outcomes) == 0 {
		faults = append(faults, fmt.Errorf("node %q outcomes must be nonempty", node.Name))
	}
	seen := make(map[string]bool)
	for _, outcome := range node.Outcomes {
		if strings.TrimSpace(outcome) == "" || seen[outcome] {
			faults = append(faults, fmt.Errorf("node %q has empty or duplicate outcome %q", node.Name, outcome))
		}
		seen[outcome] = true
		if exists && !slices.Contains(spec.Outcomes, outcome) {
			faults = append(faults, fmt.Errorf("node %q outcome %q is outside action %q outcomes", node.Name, outcome, node.Action))
		}
	}
	return faults
}

func walk(roots []string, edges map[string][]string) map[string]bool {
	seen := make(map[string]bool)
	pending := slices.Clone(roots)
	for len(pending) > 0 {
		last := len(pending) - 1
		node := pending[last]
		pending = pending[:last]
		if seen[node] {
			continue
		}
		seen[node] = true
		pending = append(pending, edges[node]...)
	}
	return seen
}
