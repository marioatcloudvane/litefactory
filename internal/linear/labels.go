package linear

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

const agentLabelPrefix = "Coding Agent: "

var agentKeyPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9_-]{0,39}$`)

// labelNode is one label on an issue with the group it belongs to, if any.
type labelNode struct {
	ID, Name string
	Parent   *struct{ ID, Name string }
}

// KeyToLabel turns a coding-agent map key such as "Codex" into "Coding Agent: Codex".
func KeyToLabel(key string) string { return agentLabelPrefix + key }

// LabelToKind reads a "Coding Agent: Name" label and returns the lowercased agent kind.
func LabelToKind(label string) (string, bool) {
	rest, ok := strings.CutPrefix(label, agentLabelPrefix)
	if !ok || !agentKeyPattern.MatchString(rest) {
		return "", false
	}
	return strings.ToLower(rest), true
}

// KeyForKind returns the single stored key whose lowercase form is kind.
func KeyForKind(keys map[string]string, kind string) (string, bool) {
	found := ""
	for key := range keys {
		if kind != "" && strings.ToLower(key) == kind {
			if found != "" {
				return "", false
			}
			found = key
		}
	}
	return found, found != ""
}

// snapshotLabels turns label nodes into the flat names the daemon understands.
// Order: mode ID, agent ID, exact Mode/Coding Agent group, own name. A label that
// came from a recognised group is dropped once, one-to-one, for each flat label
// that reads as the same name.
func (c *Client) snapshotLabels(nodes []labelNode) []string {
	type entry struct {
		name    string
		grouped bool
	}
	entries := make([]entry, 0, len(nodes))
	agentKeys := slices.Sorted(maps.Keys(c.config.CodingAgentLabels))
	for _, node := range nodes {
		e := entry{name: node.Name}
		if node.Parent != nil && (node.Parent.Name == "Mode" || node.Parent.Name == "Coding Agent") && node.Name != "" {
			e.grouped = true
		}
		switch {
		case func() bool {
			for mode, id := range c.config.ModeLabels {
				if node.ID == id {
					e.name = canonicalModeLabel(mode)
					return true
				}
			}
			return false
		}():
		case func() bool {
			for _, key := range agentKeys {
				if id := c.config.CodingAgentLabels[key]; id != "" && node.ID == id {
					e.name = KeyToLabel(key)
					return true
				}
			}
			return false
		}():
		case e.grouped:
			e.name = node.Parent.Name + ": " + node.Name
		}
		entries = append(entries, e)
	}
	drop := make([]bool, len(entries))
	flatUsed := make([]bool, len(entries))
	for i, g := range entries {
		if !g.grouped {
			continue
		}
		for j, f := range entries {
			if !f.grouped && !flatUsed[j] && f.name == g.name {
				flatUsed[j], drop[i] = true, true
				break
			}
		}
	}
	var out []string
	for i, e := range entries {
		if !drop[i] {
			out = append(out, e.name)
		}
	}
	return out
}
