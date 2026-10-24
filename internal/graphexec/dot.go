package graphexec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"gonum.org/v1/gonum/graph/formats/dot"
	"gonum.org/v1/gonum/graph/formats/dot/ast"

	"github.com/cloudvane/litefactory/internal/agent"
)

// ParseProfile decodes exactly one directed DOT graph. The AST adapter preserves
// declarations (including duplicates), graph statements and defaults; unlike a
// graph builder it never silently materializes an undeclared endpoint. Semantic
// faults are reported by Validate, separately from DOT and scalar syntax errors.
func ParseProfile(data []byte) (*Profile, error) {
	file, err := dot.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse graph profile: %w", err)
	}
	if len(file.Graphs) != 1 || !file.Graphs[0].Directed {
		return nil, errors.New("parse graph profile: require exactly one directed graph")
	}
	digest := sha256.Sum256(data)
	p := &Profile{Digest: hex.EncodeToString(digest[:]), Attrs: make(map[string]string)}
	nodeDefaults := make(map[string]string)
	edgeDefaults := make(map[string]string)
	var faults []error
	for _, statement := range file.Graphs[0].Stmts {
		switch stmt := statement.(type) {
		case *ast.Attr:
			faults = append(faults, validateDOTSlotAttrs([]*ast.Attr{stmt}))
			p.Attrs[dotText(stmt.Key)] = dotText(stmt.Val)
		case *ast.AttrStmt:
			switch stmt.Kind {
			case ast.GraphKind:
				faults = append(faults, validateDOTSlotAttrs(stmt.Attrs))
				mergeDOTAttrs(p.Attrs, stmt.Attrs)
			case ast.NodeKind:
				faults = append(faults, validateDOTSlotAttrs(stmt.Attrs))
				mergeDOTAttrs(nodeDefaults, stmt.Attrs)
			case ast.EdgeKind:
				mergeDOTAttrs(edgeDefaults, stmt.Attrs)
			}
		case *ast.NodeStmt:
			faults = append(faults, validateDOTSlotAttrs(stmt.Attrs))
			name, err := dotNodeName(stmt.Node)
			if err != nil {
				faults = append(faults, err)
				continue
			}
			attrs := maps.Clone(nodeDefaults)
			mergeDOTAttrs(attrs, stmt.Attrs)
			node, err := decodeNode(name, attrs)
			if err != nil {
				faults = append(faults, err)
			}
			p.Nodes = append(p.Nodes, node)
		case *ast.EdgeStmt:
			edges, err := decodeEdges(stmt, edgeDefaults)
			if err != nil {
				faults = append(faults, err)
			}
			p.Edges = append(p.Edges, edges...)
		default:
			faults = append(faults, errors.New("graph profile does not support subgraphs"))
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

// Check declarations before merging so an invalid supplied default or earlier
// value cannot be hidden by a later valid override.
func validateDOTSlotAttrs(attrs []*ast.Attr) error {
	var faults []error
	for _, attr := range attrs {
		if dotText(attr.Key) == slotBudgetAttribute {
			_, err := slotCostAttribute(map[string]string{slotBudgetAttribute: dotText(attr.Val)})
			faults = append(faults, err)
		}
	}
	return errors.Join(faults...)
}

func mergeDOTAttrs(dst map[string]string, attrs []*ast.Attr) {
	for _, attr := range attrs {
		dst[dotText(attr.Key)] = dotText(attr.Val)
	}
}

// DOT strings do not use Go escapes. Preserve arbitrary backslash sequences
// (including agent data) and decode only DOT's escaped quote and continuation.
func dotText(value string) string {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return value
	}
	value = value[1 : len(value)-1]
	value = strings.ReplaceAll(value, "\\\r\n", "")
	value = strings.ReplaceAll(value, "\\\n", "")
	return strings.ReplaceAll(value, `\"`, `"`)
}

func dotNodeName(vertex ast.Vertex) (string, error) {
	node, ok := vertex.(*ast.Node)
	if !ok {
		return "", errors.New("graph profile does not support subgraph endpoints")
	}
	if node.Port != nil {
		return "", errors.New("graph profile does not support node ports")
	}
	return dotText(node.ID), nil
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

func decodeEdges(stmt *ast.EdgeStmt, defaults map[string]string) ([]Edge, error) {
	from, err := dotNodeName(stmt.From)
	if err != nil {
		return nil, err
	}
	attrs := maps.Clone(defaults)
	mergeDOTAttrs(attrs, stmt.Attrs)
	on := attrs["on"]
	if alternate, exists := attrs["lf_on"]; exists {
		if on != "" && on != alternate {
			return nil, errors.New("edge has conflicting on and lf_on attributes")
		}
		on = alternate
	}
	var edges []Edge
	for next := stmt.To; next != nil; next = next.To {
		to, err := dotNodeName(next.Vertex)
		if err != nil {
			return nil, err
		}
		edges = append(edges, Edge{From: from, To: to, On: on, Attrs: maps.Clone(attrs)})
		from = to
	}
	return edges, nil
}
