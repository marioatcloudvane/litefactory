package graph

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gonum.org/v1/gonum/graph/formats/dot"
	"gonum.org/v1/gonum/graph/formats/dot/ast"
)

// Attribute preserves one supplied DOT attribute, including overridden values.
type Attribute struct{ Key, Value string }

// Declaration preserves source order and effective defaults. Kind is graph,
// node-default, edge-default, node, or edge. An edge chain has one declaration
// per pair, each retaining the original supplied attributes.
type Declaration struct {
	Kind           string
	Name, From, To string
	Raw            []Attribute
	Attrs          map[string]string
}

// Document retains declarations without assigning application meanings to them.
type Document struct {
	Declarations []Declaration
	Attrs        map[string]string
}

// Parse reads exactly one directed DOT graph without materializing endpoints.
// Attribute strings retain DOT escaping semantics rather than Go escaping.
func Parse(data []byte) (*Document, error) {
	file, err := dot.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse graph profile: %w", err)
	}
	if len(file.Graphs) != 1 || !file.Graphs[0].Directed {
		return nil, errors.New("parse graph profile: require exactly one directed graph")
	}
	d := &Document{Attrs: make(map[string]string)}
	nodeDefaults := make(map[string]string)
	edgeDefaults := make(map[string]string)
	var faults []error
	for _, statement := range file.Graphs[0].Stmts {
		switch stmt := statement.(type) {
		case *ast.Attr:
			attrs := attributes([]*ast.Attr{stmt})
			merge(d.Attrs, attrs)
			d.Declarations = append(d.Declarations, Declaration{Kind: "graph", Raw: attrs, Attrs: maps.Clone(d.Attrs)})
		case *ast.AttrStmt:
			attrs := attributes(stmt.Attrs)
			kind := "graph"
			target := d.Attrs
			switch stmt.Kind {
			case ast.NodeKind:
				kind = "node-default"
				target = nodeDefaults
			case ast.EdgeKind:
				kind = "edge-default"
				target = edgeDefaults
			}
			merge(target, attrs)
			d.Declarations = append(d.Declarations, Declaration{Kind: kind, Raw: attrs, Attrs: maps.Clone(target)})
		case *ast.NodeStmt:
			name, err := nodeName(stmt.Node)
			if err != nil {
				faults = append(faults, err)
				continue
			}
			raw := attributes(stmt.Attrs)
			attrs := maps.Clone(nodeDefaults)
			merge(attrs, raw)
			d.Declarations = append(d.Declarations, Declaration{Kind: "node", Name: name, Raw: raw, Attrs: attrs})
		case *ast.EdgeStmt:
			from, err := nodeName(stmt.From)
			if err != nil {
				faults = append(faults, err)
				continue
			}
			raw := attributes(stmt.Attrs)
			attrs := maps.Clone(edgeDefaults)
			merge(attrs, raw)
			var edges []Declaration
			for next := stmt.To; next != nil; next = next.To {
				to, err := nodeName(next.Vertex)
				if err != nil {
					faults = append(faults, err)
					edges = nil
					break
				}
				edges = append(edges, Declaration{Kind: "edge", From: from, To: to, Raw: slices.Clone(raw), Attrs: maps.Clone(attrs)})
				from = to
			}
			d.Declarations = append(d.Declarations, edges...)
		default:
			faults = append(faults, errors.New("graph profile does not support subgraphs"))
		}
	}
	if err := errors.Join(faults...); err != nil {
		return nil, fmt.Errorf("decode graph profile: %w", err)
	}
	return d, nil
}
func attributes(attrs []*ast.Attr) []Attribute {
	out := make([]Attribute, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, Attribute{Key: dotText(a.Key), Value: dotText(a.Val)})
	}
	return out
}
func merge(dst map[string]string, attrs []Attribute) {
	for _, a := range attrs {
		dst[a.Key] = a.Value
	}
}

// DOT strings decode only escaped quote and line continuation.
func dotText(value string) string {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return value
	}
	value = value[1 : len(value)-1]
	value = strings.ReplaceAll(value, "\\\r\n", "")
	value = strings.ReplaceAll(value, "\\\n", "")
	return strings.ReplaceAll(value, `\"`, `"`)
}
func nodeName(vertex ast.Vertex) (string, error) {
	node, ok := vertex.(*ast.Node)
	if !ok {
		return "", errors.New("graph profile does not support subgraph endpoints")
	}
	if node.Port != nil {
		return "", errors.New("graph profile does not support node ports")
	}
	return dotText(node.ID), nil
}
