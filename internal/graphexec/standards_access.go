package graphexec

import (
	"fmt"
	"slices"
	"strings"
)

// StandardsAttribute names the optional node attribute that lets the listed
// roles read the standards book. Its value is a comma-separated list of role
// markdown names; each must be the node's own lf_agent_markdown or one of its
// lf_roles. A role that is not listed gets no standards tool. The grant is
// read-only, optional and advisory: the factory never requires a call.
const StandardsAttribute = "lf_standards"

// StandardsRole returns role when the node grants it the standards read, or ""
// when it does not. An absent or empty attribute grants nothing. The returned
// name selects the standards whose applies_to lists it.
func StandardsRole(attrs map[string]string, role string) string {
	role = strings.TrimSpace(role)
	if role == "" || !slices.Contains(standardsRoles(attrs), role) {
		return ""
	}
	return role
}

func standardsRoles(attrs map[string]string) []string {
	raw, ok := attrs[StandardsAttribute]
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for element := range strings.SplitSeq(raw, ",") {
		out = append(out, strings.TrimSpace(element))
	}
	return out
}

// validateStandardsAttr keeps the grant tied to roles the node actually runs.
func validateStandardsAttr(node Node) error {
	raw, present := node.Attrs[StandardsAttribute]
	if !present {
		return nil
	}
	allowed := []string{}
	if agent := node.Attrs["lf_agent_markdown"]; agent != "" {
		allowed = append(allowed, agent)
	}
	for role := range strings.SplitSeq(node.Attrs["lf_roles"], ",") {
		if role = strings.TrimSpace(role); role != "" {
			allowed = append(allowed, role)
		}
	}
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s must name at least one role", StandardsAttribute)
	}
	seen := map[string]bool{}
	for _, role := range standardsRoles(node.Attrs) {
		if !slices.Contains(allowed, role) {
			return fmt.Errorf("%s role %q is not a role this node runs", StandardsAttribute, role)
		}
		if seen[role] {
			return fmt.Errorf("%s repeats role %q", StandardsAttribute, role)
		}
		seen[role] = true
	}
	return nil
}
