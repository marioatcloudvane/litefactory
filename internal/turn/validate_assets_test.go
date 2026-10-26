package turn

import (
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func captureValidate(t *testing.T) *teamsource.WorkflowDefinition {
	t.Helper()
	specs, err := ValidateFlowSpecs()
	if err != nil {
		t.Fatal(err)
	}
	definition, err := teamsourcetest.CaptureValidate(t.Context(), specs)
	if err != nil {
		t.Fatalf("the packaged Validate graph does not capture: %v", err)
	}
	return definition
}

// AC-001: the four roles, the two skills and the one graph ship as one set.
func TestValidateShipsFourRolesTwoSkillsAndOneGraph(t *testing.T) {
	definition := captureValidate(t)
	var roles, skills, graphs []string
	for name := range definition.Files {
		switch {
		case path.Ext(name) == ".dot":
			graphs = append(graphs, name)
		case strings.HasPrefix(name, "skills/") && path.Base(name) == "SKILL.md":
			skills = append(skills, name)
		case !strings.Contains(name, "/") && path.Ext(name) == ".md":
			roles = append(roles, name)
		}
	}
	slices.Sort(roles)
	slices.Sort(skills)
	if len(graphs) != 1 || strings.Join(roles, ",") != "validate-acceptance.md,validate-architecture.md,validate-code.md,validate-repair.md" {
		t.Fatalf("shipped package: graphs=%v roles=%v", graphs, roles)
	}
	if strings.Join(skills, ",") != "skills/litefactory-rules/SKILL.md,skills/validate-repair-report/SKILL.md,skills/validate-review/SKILL.md" {
		t.Fatalf("skills %v", skills)
	}
	for _, key := range []string{"repair_agent", "review:validate-acceptance", "review:validate-code", "review:validate-architecture"} {
		if _, ok := definition.Nodes[key]; !ok {
			t.Errorf("node %q is not captured: %v", key, definition.Nodes)
		}
	}
}

// AC-001: the graph declares the bound, the limits, the review and the ends;
// its edges are the DOT's own and are not copied here.
func TestValidateGraphDeclaresLoopBoundLimitsReviewAndEnds(t *testing.T) {
	p := captureValidate(t).Profile
	node := func(name string) graphexec.Node {
		n, ok := p.Node(name)
		if !ok {
			t.Fatalf("node %q missing", name)
		}
		return n
	}
	if p.Start != "suite" || node("suite").Action != graphexec.ValidateSuiteAction {
		t.Fatalf("start %q", p.Start)
	}
	budget, agentNode, check, review := node("repair_budget"), node("repair_agent"), node("repair_check"), node("review")
	if budget.Attrs["lf_rounds"] != "2" || agentNode.Limit != 2 {
		t.Errorf("the repair bound is not two rounds: %v %d", budget.Attrs, agentNode.Limit)
	}
	if agentNode.Attrs["lf_access"] != "write" || agentNode.Attrs["lf_budget"] != "15" || agentNode.Attrs["lf_agent_markdown"] != "validate-repair" {
		t.Errorf("repair agent: %v", agentNode.Attrs)
	}
	if check.Attrs["lf_max_files"] != "3" || check.Attrs["lf_forbid"] != "package,dependency,schema,api,tests,criteria" {
		t.Errorf("repair limits: %v", check.Attrs)
	}
	if review.Attrs["lf_roles"] != "validate-acceptance,validate-code,validate-architecture" {
		t.Errorf("reviewers: %v", review.Attrs)
	}
	for _, n := range p.Nodes {
		if n.Action == "lf_agent" && n.Name != "repair_agent" {
			t.Errorf("unexpected agent node %s: reviewers are started by the review step", n.Name)
		}
	}
	for _, end := range []string{"waiting", "delivered"} {
		if n := node(end); !n.Terminal {
			t.Errorf("%s is not a terminal", end)
		}
	}
}

func TestValidateAgentRolesHaveTheirAccessLevels(t *testing.T) {
	policy := ValidateWorkflowPolicy()
	p := captureValidate(t).Profile
	repair, _ := p.Node("repair_agent")
	if got := nodeAccessLevel(policy, repair); got != "write" {
		t.Errorf("repair access %q", got)
	}
	// The reviewers are started by the review step, never as graph agent nodes, so
	// the policy grants no default access; they run read-only by construction.
	if policy.DefaultAccess != "" || ValidateReviewAccess != "read" {
		t.Errorf("default access %q, reviewer access %q", policy.DefaultAccess, ValidateReviewAccess)
	}
	applied, err := applyNodeAccess(policy, repair, Request{Agent: agent.Kind(store.AgentCodex), CodexSandbox: "workspace-write"})
	if err != nil || applied.CodexSandbox != "workspace-write" {
		t.Errorf("repair under Codex: %q %v", applied.CodexSandbox, err)
	}
	reviewer := graphexec.Node{Name: "reviewer", Action: "lf_agent", Attrs: map[string]string{"lf_access": ValidateReviewAccess}}
	applied, err = applyNodeAccess(policy, reviewer, Request{Agent: agent.Kind(store.AgentCodex), CodexSandbox: "workspace-write"})
	if err != nil || applied.CodexSandbox != "read-only" {
		t.Errorf("reviewer under Codex: %q %v", applied.CodexSandbox, err)
	}
}
