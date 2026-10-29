package graphexec

import (
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"slices"
	"strconv"
	"strings"
)

// PlanParallelAction runs a bounded Plan contribution group.
const PlanParallelAction = "lf_plan_parallel"

// PlanParallelSpec opts a captured Plan graph into bounded member execution.
func PlanParallelSpec() ActionSpec {
	return ActionSpec{Name: PlanParallelAction, Mode: agent.ModePlan, Outcomes: []string{"ready", "ask", "fallback", "stopped", "superseded", "failed"}}
}

// PlanParallel names the captured role members and reconciliation bound.
type PlanParallel struct {
	Members map[string]string
	Rounds  int
}

// DecodePlanParallel validates references, unique ownership and a finite reconciliation bound.
func DecodePlanParallel(p *Profile, n Node) (PlanParallel, error) {
	out := PlanParallel{Members: map[string]string{}}
	if p == nil || p.Mode != agent.ModePlan || n.Action != PlanParallelAction {
		return out, errors.New("parallel groups require Plan")
	}
	rounds, err := strconv.Atoi(n.Attrs["lf_rounds"])
	if err != nil || rounds < 1 || rounds > PlanNodeLimitMax {
		return out, errors.New("lf_rounds requires a positive bounded integer")
	}
	out.Rounds = rounds
	members := strings.Split(n.Attrs["lf_members"], ",")
	if len(members) != 3 {
		return out, errors.New("parallel group requires exactly three members")
	}
	for _, name := range members {
		member, ok := p.Node(strings.TrimSpace(name))
		role := member.Attrs["lf_plan_role"]
		if !ok || member.Action != "lf_agent" || !slices.Contains([]string{"story", "technical", "acceptance"}, role) || out.Members[role] != "" {
			return out, fmt.Errorf("invalid or duplicate parallel member %q", name)
		}
		if _, _, err := ResearchReferences(member.Attrs); err != nil {
			return out, err
		}
		out.Members[role] = member.Name
	}
	return out, nil
}

func decodePlanFact(action string, attrs map[string]string) (ResearchCondition, error) {
	c := ResearchCondition{Field: attrs["lf_field"], Condition: attrs["lf_condition"]}
	valid := false
	switch c.Field {
	case "story.size":
		valid = slices.Contains([]string{"S", "M", "L"}, c.Condition)
	case "story.sizing_valid", "story.fallback":
		valid = c.Condition == "true" || c.Condition == "false"
	}
	_, hasDefault := attrs["lf_default"]
	if action != ResearchIfAction || !valid || hasDefault || attrs["lf_source"] != "" {
		return c, errors.New("invalid Plan fact comparison")
	}
	return c, nil
}

// EvaluatePlanFacts evaluates only the comparison declared by the captured graph.
// Missing facts fail closed even when the declared comparison is false.
func (c ResearchCondition) EvaluatePlanFacts(facts map[string]string) (string, error) {
	if _, err := decodePlanFact(ResearchIfAction, map[string]string{"lf_field": c.Field, "lf_condition": c.Condition}); err != nil {
		return "", err
	}
	value, present := facts[c.Field]
	if !present {
		return "false", nil
	}
	if value == c.Condition {
		return "true", nil
	}
	return "false", nil
}
