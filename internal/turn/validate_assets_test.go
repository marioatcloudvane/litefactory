package turn

import (
	"path"
	"regexp"
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
	definition, err := teamsource.CaptureValidate(t.Context(), specs)
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
	if strings.Join(skills, ",") != "skills/validate-repair-report/SKILL.md,skills/validate-review/SKILL.md" {
		t.Fatalf("skills %v", skills)
	}
	for _, key := range []string{"repair_agent", "review:validate-acceptance", "review:validate-code", "review:validate-architecture"} {
		if _, ok := definition.Nodes[key]; !ok {
			t.Errorf("node %q is not captured: %v", key, definition.Nodes)
		}
	}
}

func TestValidateRolesDescribeTheJobNotTheFlow(t *testing.T) {
	definition := captureValidate(t)
	for name, raw := range definition.Files {
		if path.Ext(name) != ".md" {
			continue
		}
		text := string(raw)
		if strings.Contains(text, "lf_") || strings.Contains(text, "->") {
			t.Errorf("%s carries graph syntax", name)
		}
		for _, node := range definition.Profile.Nodes {
			if node.Terminal {
				continue
			}
			if regexp.MustCompile(`\b`+regexp.QuoteMeta(node.Name)+`\b`).MatchString(text) && node.Name != "review" && node.Name != "outcome" && node.Name != "suite" {
				t.Errorf("%s names graph node %q", name, node.Name)
			}
		}
	}
	for _, name := range []string{"validate-repair.md", "validate-acceptance.md", "validate-code.md", "validate-architecture.md"} {
		role := strings.ToLower(string(definition.Files[name]))
		for _, banned := range []string{"next step", "then the factory runs", "goes to"} {
			if strings.Contains(role, banned) {
				t.Errorf("%s describes flow: %q", name, banned)
			}
		}
	}
}

// AC-001: the graph declares the repair loop, the bound, the limits, the review
// and the three ends; and the roles have the declared access.
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
	budget, agentNode, check, review, outcome := node("repair_budget"), node("repair_agent"), node("repair_check"), node("review"), node("outcome")
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
	for _, want := range [][3]string{
		{"suite", "passed", "review"}, {"suite", "failed", "repair_budget"}, {"suite", "blocked", "review"},
		{"repair_budget", "run", "repair_agent"}, {"repair_budget", "spent", "review"},
		{"repair_agent", "failed", "repair_check"}, {"repair_check", "kept", "suite"},
		{"repair_check", "too_big", "review"}, {"repair_check", "handback", "review"}, {"repair_check", "blocked", "review"},
		{"repair_check", "unfinished", "repair_budget"}, {"review", "done", "outcome"}, {"review", "retry", "review"},
		{"outcome", "assessed", "owner_resolve"}, {"outcome", "handed_back", "owner_resolve"}, {"outcome", "blocked", "owner_resolve"},
		{"owner_resolve", "push", "owner_push"}, {"owner_resolve", "merge", "owner_merge"}, {"owner_resolve", "close", "owner_close"}, {"owner_resolve", "create_issue", "owner_issue"},
		{"owner_resolve", "wait", "owner_report"}, {"owner_resolve", "stale", "owner_report"}, {"owner_report", "done", "owner_ask"}, {"owner_ask", "published", "waiting"},
		{"owner_close", "closed", "delivered"}, {"owner_close", "open", "owner_report"},
	} {
		if got, ok := graphexec.Decide(p, want[0], want[1]); !ok || got != want[2] {
			t.Errorf("%s on %s goes to %q, want %s", want[0], want[1], got, want[2])
		}
	}
	for _, end := range []string{"waiting", "delivered"} {
		if n := node(end); !n.Terminal {
			t.Errorf("%s is not a terminal", end)
		}
	}
	_ = outcome
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
