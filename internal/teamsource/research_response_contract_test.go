package teamsource

import (
	"regexp"
	"strings"
	"testing"
)

var researchDotNode = regexp.MustCompile(`(?m)^\s*(\w+)\s*\[([^\]]*)\]`)
var researchDotAttr = regexp.MustCompile(`(\w+)="([^"]*)"`)

// A control node tests the response its source agent returns, so an agent whose
// instructions cannot emit that value leaves the edge behind it unreachable.
// That is how the shaping stage came to publish review after review: the graph
// advanced only on "approved" while the role's output contract named "ask" and
// "done" alone, and the agent duly reported the owner's approval in prose.
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
		if condition == "" {
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
	// The worked envelopes are what an agent copies from; a response with no
	// envelope is one it has no example of returning.
	envelopes := read("skills/research-evidence/node-result.md")
	for _, response := range []string{"ask", "done", "approved"} {
		if !strings.Contains(envelopes, "# response "+response) {
			t.Errorf("node-result carries no worked envelope for %q", response)
		}
	}
}
