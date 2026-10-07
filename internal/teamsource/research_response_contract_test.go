package teamsource

import (
	"regexp"
	"strings"
	"testing"
)

var researchDotNode = regexp.MustCompile(`(?m)^\s*(\w+)\s*\[([^\]]*)\]`)
var researchDotAttr = regexp.MustCompile(`(\w+)="([^"]*)"`)

// Agent-owned graph responses must have matching provider instructions.
// Approval is a factory-owned response derived from authenticated owner input.
func TestResearchRolesCanEmitEveryConditionTheGraphTests(t *testing.T) {
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
		output := read(role + ".md")
		if i := strings.Index(output, "# Output"); i >= 0 {
			output = output[i:]
		}
		if !strings.Contains(output, "`"+condition+"`") {
			t.Errorf("%s advances on %q but the %s role's output contract cannot return it", name, condition, role)
		}
		tested++
	}
	if tested == 0 {
		t.Fatal("no control node was checked; the DOT shape changed")
	}
	// The worked results are what an agent copies from; a response with no
	// section is one it has no guidance on reaching.
	contract := read("skills/research-evidence/node-result.md")
	for _, response := range []string{"ask", "done", "continue"} {
		if !strings.Contains(contract, "# response "+response) {
			t.Errorf("node-result carries no guidance for %q", response)
		}
	}
	if !strings.Contains(contract, "# Owner approval") || !strings.Contains(contract, "Do not submit an `approved` response") {
		t.Error("node-result must reserve approval for the factory")
	}
}
