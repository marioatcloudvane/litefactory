package teamsource

import (
	"regexp"
	"testing"
)

var researchDotNode = regexp.MustCompile(`(?m)^\s*(\w+)\s*\[([^\]]*)\]`)
var researchDotAttr = regexp.MustCompile(`(\w+)="([^"]*)"`)

// Agent-owned graph conditions must reference a shipped source role.
// Approval is factory-owned and does not require an agent source.
func TestResearchGraphConditionsReferenceShippedRoles(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		raw, err := researchAssets.ReadFile("research/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	nodes := map[string]map[string]string{}
	for _, match := range researchDotNode.FindAllStringSubmatch(read("research.dot"), -1) {
		attrs := map[string]string{}
		for _, attr := range researchDotAttr.FindAllStringSubmatch(match[2], -1) {
			attrs[attr[1]] = attr[2]
		}
		nodes[match[1]] = attrs
	}
	tested := 0
	for name, attrs := range nodes {
		condition, source := attrs["lf_condition"], attrs["lf_source"]
		if condition == "" || condition == "approved" {
			continue
		}
		role := nodes[source]["lf_agent_markdown"]
		if role == "" {
			t.Errorf("%s tests %q against source %q, which names no role", name, condition, source)
			continue
		}
		read(role + ".md")
		tested++
	}
	if tested == 0 {
		t.Fatal("no control node was checked; the DOT shape changed")
	}
}
