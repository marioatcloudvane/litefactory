package turn

import (
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// spikeGoSources are the non-test Go files that make up graph Spike in this package.
func spikeGoSources(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("spike_*.go")
	mustProgress(t, err)
	files = append(files, "workflow_access.go")
	out := map[string]string{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		mustProgress(t, err)
		out[name] = string(raw)
	}
	if len(out) < 4 {
		t.Fatalf("expected the Spike sources, found %v", out)
	}
	return out
}

func TestSpikeGoNamesNoGraphNodeAndPicksNoStep(t *testing.T) {
	specs, err := SpikeSpecs()
	mustProgress(t, err)
	definition, err := teamsourcetest.CaptureSpike(t.Context(), specs)
	mustProgress(t, err)
	for name, source := range spikeGoSources(t) {
		for _, node := range definition.Profile.Nodes {
			quoted := regexp.MustCompile(`["` + "`" + `]` + regexp.QuoteMeta(node.Name) + `["` + "`" + `]`)
			if quoted.MatchString(source) || (strings.Contains(node.Name, "_") && strings.Contains(source, node.Name)) {
				t.Errorf("%s names graph node %q", name, node.Name)
			}
		}
		for _, forbidden := range []string{"Decide(", "SetGraphInstanceCurrentNode", "NextNode", "ValidateSpike", "ValidatePlanControlGraph"} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s chooses or validates flow through %q", name, forbidden)
			}
		}
	}
}

func TestSpikeHasNoTopologyValidatorInTheGenericEngine(t *testing.T) {
	files, err := filepath.Glob("../graphexec/*.go")
	mustProgress(t, err)
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		mustProgress(t, err)
		if strings.Contains(strings.ToLower(string(raw)), "spike") {
			t.Errorf("%s carries Spike vocabulary; the engine must stay generic", name)
		}
	}
	policy := SpikeWorkflowPolicy()
	if policy.Control.Mode != agent.ModeSpike || strings.Join(policy.Control.Responses, ",") != "ask,done" || policy.Control.Approved != "" {
		t.Fatalf("Spike control policy: %+v", policy.Control)
	}
	count := 0
	for _, registered := range RegisteredWorkflowPolicies() {
		if registered.Mode == agent.ModeSpike {
			count++
			if err := registered.Admit(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if count != 1 {
		t.Fatalf("Spike registered %d times", count)
	}
}

func TestSpikeLimitsAreDeclaredInTheGraphFile(t *testing.T) {
	specs, err := SpikeSpecs()
	mustProgress(t, err)
	definition, err := teamsourcetest.CaptureSpike(t.Context(), specs)
	mustProgress(t, err)
	limits := map[string]int{}
	for _, node := range definition.Profile.Nodes {
		if node.Action != "" {
			limits[node.Action] += node.Limit
			if node.Limit < 1 {
				t.Errorf("node %q is unbounded", node.Name)
			}
		}
		switch node.Action {
		case "lf_agent":
			switch node.Attrs["lf_agent_markdown"] {
			case "spike-investigator":
				if node.Limit != 2 || node.Attrs["lf_access"] != "read" {
					t.Errorf("investigator must run at most twice and read-only: %+v", node)
				}
			case "spike-fixer":
				if node.Limit != 1 || node.Attrs["lf_access"] != "write" {
					t.Errorf("fixer must run once with write access: %+v", node)
				}
			default:
				t.Errorf("unexpected agent role: %+v", node)
			}
		case "lf_ask", "lf_spike_gate":
			if node.Limit != 1 {
				t.Errorf("%s must be limited to one pass: %+v", node.Action, node)
			}
		case "lf_comment":
			if node.Limit != 1 {
				t.Errorf("a report is published at most once: %+v", node)
			}
		}
	}
}

// A failed check gets exactly one repair: it hands over to a second write-access
// fixer pass whose change is checked again, and a second failure is reported
// as unverified with no further agent reachable.
func TestSpikeRepairsOnceAfterAFailedCheck(t *testing.T) {
	specs, err := SpikeSpecs()
	mustProgress(t, err)
	definition, err := teamsourcetest.CaptureSpike(t.Context(), specs)
	mustProgress(t, err)
	profile := definition.Profile
	next := func(from, outcome string) graphexec.Node {
		t.Helper()
		name, ok := graphexec.Decide(profile, from, outcome)
		node, found := profile.Node(name)
		if !ok || !found {
			t.Fatalf("%s has no %q edge", from, outcome)
		}
		return node
	}
	checkOf := func(fixer graphexec.Node) graphexec.Node {
		t.Helper()
		if fixer.Action != "lf_agent" || fixer.Attrs["lf_agent_markdown"] != "spike-fixer" || fixer.Attrs[graphexec.AccessAttribute] != "write" {
			t.Fatalf("not a write-access fixer: %+v", fixer)
		}
		gate := next(fixer.Name, "success")
		check := next(gate.Name, "false")
		if check.Action != SpikeCheckAction || check.Attrs["lf_source"] != fixer.Name {
			t.Fatalf("fixer %s is not checked: %+v", fixer.Name, check)
		}
		return check
	}
	var sizeGate graphexec.Node
	for _, node := range profile.Nodes {
		if node.Action == SpikeGateAction {
			sizeGate = node
		}
	}
	first := next(sizeGate.Name, "small")
	firstCheck := checkOf(first)
	second := next(firstCheck.Name, "failed")
	if second.Name == first.Name {
		t.Fatal("the repair loops back to the same fixer node")
	}
	secondCheck := checkOf(second)
	if note := next(secondCheck.Name, "failed"); note.Action != SpikeNoteAction || note.Attrs["lf_status"] != "fix_unverified" {
		t.Fatalf("a second failed check is not reported as unverified: %+v", note)
	}
	seen := map[string]bool{}
	var walk func(string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		node, _ := profile.Node(name)
		if node.Action == "lf_agent" {
			t.Fatalf("an agent (%s) is reachable after the repair's check", name)
		}
		for _, outcome := range node.Outcomes {
			if target, ok := graphexec.Decide(profile, name, outcome); ok {
				walk(target)
			}
		}
	}
	for _, outcome := range secondCheck.Outcomes {
		if target, ok := graphexec.Decide(profile, secondCheck.Name, outcome); ok {
			walk(target)
		}
	}
}
