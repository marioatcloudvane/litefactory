package graphexec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// SemanticDigest excludes DOT layout, declaration order and comments, while
// retaining all execution attributes, actions, tasks, tiers and edge conditions.
// It is independent of the immutable foundation's exact-byte Digest.
func SemanticDigest(p *Profile) (string, error) {
	if p == nil {
		return "", errors.New("semantic digest requires a graph profile")
	}
	canonical := *p
	canonical.Digest = ""
	canonical.Attrs = semanticAttrs(p.Attrs)
	canonical.Nodes = append([]Node(nil), p.Nodes...)
	for i := range canonical.Nodes {
		canonical.Nodes[i].Attrs = semanticAttrs(canonical.Nodes[i].Attrs)
		canonical.Nodes[i].Outcomes = append([]string(nil), canonical.Nodes[i].Outcomes...)
		sort.Strings(canonical.Nodes[i].Outcomes)
	}
	sort.Slice(canonical.Nodes, func(i, j int) bool { return canonical.Nodes[i].Name < canonical.Nodes[j].Name })
	canonical.Edges = append([]Edge(nil), p.Edges...)
	for i := range canonical.Edges {
		canonical.Edges[i].Attrs = semanticAttrs(canonical.Edges[i].Attrs)
	}
	sort.Slice(canonical.Edges, func(i, j int) bool {
		a, b := canonical.Edges[i], canonical.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.On != b.On {
			return a.On < b.On
		}
		return a.To < b.To
	})
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func semanticAttrs(attrs map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range attrs {
		if strings.HasPrefix(key, "lf_") || key == "team" || key == "mode" || key == "profile_version" || key == "start" {
			out[key] = value
		}
	}
	return out
}
