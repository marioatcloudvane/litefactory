package graphexec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/pkg/execution/graph"
)

// ParseProfile applies factory scalar meanings and byte identity to neutral DOT.
func ParseProfile(data []byte) (*Profile, error) {
	d, err := graph.Parse(data)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	p := &Profile{Digest: hex.EncodeToString(digest[:]), Attrs: d.Attrs}
	var faults []error
	for _, decl := range d.Declarations {
		if decl.Kind != "edge" && decl.Kind != "edge-default" {
			for _, a := range decl.Raw {
				if a.Key == slotBudgetAttribute {
					_, err := slotCostAttribute(map[string]string{a.Key: a.Value})
					faults = append(faults, err)
				}
			}
		}
		switch decl.Kind {
		case "node":
			node, err := decodeNode(decl.Name, decl.Attrs)
			faults = append(faults, err)
			p.Nodes = append(p.Nodes, node)
		case "edge":
			on := decl.Attrs["on"]
			if alternate, exists := decl.Attrs["lf_on"]; exists {
				if on != "" && on != alternate {
					faults = append(faults, errors.New("edge has conflicting on and lf_on attributes"))
					continue
				}
				on = alternate
			}
			p.Edges = append(p.Edges, Edge{From: decl.From, To: decl.To, On: on, Attrs: decl.Attrs})
		}
	}
	p.Team = p.Attrs["team"]
	p.Mode = agent.Mode(p.Attrs["mode"])
	p.Workflow = agent.Workflow(p.Attrs["workflow"])
	p.Start = p.Attrs["start"]
	if value, exists := p.Attrs["profile_version"]; exists {
		p.ProfileVersion, err = strconv.Atoi(value)
		if err != nil {
			faults = append(faults, fmt.Errorf("profile_version %q: %w", value, err))
		}
	}
	if err := errors.Join(faults...); err != nil {
		return nil, fmt.Errorf("decode graph profile: %w", err)
	}
	return p, nil
}
func decodeNode(name string, attrs map[string]string) (Node, error) {
	node := Node{Name: name, Action: attrs["lf_action"], Attrs: attrs}
	var faults []error
	if value, exists := attrs["lf_outcomes"]; exists {
		for outcome := range strings.SplitSeq(value, ",") {
			node.Outcomes = append(node.Outcomes, strings.TrimSpace(outcome))
		}
	}
	if value, exists := attrs["lf_limit"]; exists {
		limit, err := strconv.Atoi(value)
		if err != nil {
			faults = append(faults, fmt.Errorf("node %q lf_limit %q: %w", name, value, err))
		}
		node.Limit = limit
	}
	if value, exists := attrs["lf_terminal"]; exists {
		terminal, err := strconv.ParseBool(value)
		if err != nil {
			faults = append(faults, fmt.Errorf("node %q lf_terminal %q: %w", name, value, err))
		}
		node.Terminal = terminal
	}
	return node, errors.Join(faults...)
}
