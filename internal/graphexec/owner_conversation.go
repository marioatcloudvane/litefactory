package graphexec

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
)

// OwnerConversationSpec opts one loop into a factory-authenticated conversation.
// Ordinary comparison loops keep their existing specifications and semantics.
func OwnerConversationSpec() ActionSpec {
	attrs := OwnerStopAttributes()
	attrs["lf_contract"] = AttributeSpec{Required: true, Values: []string{"owner_conversation"}}
	return ActionSpec{Name: ResearchLoopAction, Workflow: agent.WorkflowBug, Outcomes: []string{"analyse", "approved", "blocked"}, Attributes: attrs}
}

// BugFinalizationSpec describes the downstream capability without providing it.
// Admission still requires its concrete factory implementation.
func BugFinalizationSpec() ActionSpec {
	attrs := OwnerStopAttributes()
	attrs["lf_owner_continuation"] = AttributeSpec{Required: true, Values: []string{"required"}}
	return ActionSpec{Name: "lf_bug_finalize", Workflow: agent.WorkflowBug, Outcomes: []string{"completed", "revalidate", "stale", "blocked"}, Attributes: attrs}
}

// ValidateOwnerConversationGraph checks the declared structure by capability,
// independent of node names. The only autonomous cycle is analyst discussion.
// A stale revalidation edge is explicitly released by a claimed owner reply.
func ValidateOwnerConversationGraph(p *Profile) error {
	if p == nil || p.Workflow != agent.WorkflowBug || p.Mode != "" {
		return errors.New("owner conversation requires standalone Bug identity")
	}
	var loop, analyst, fixer, final Node
	var loops, analysts, fixers, finals int
	var faults []error
	for _, n := range p.Nodes {
		if n.Terminal {
			continue
		}
		if !ParksOwner(n) || n.Limit < 1 {
			faults = append(faults, fmt.Errorf("activity %q requires finite owner parking", n.Name))
		}
		switch n.Action {
		case ResearchLoopAction:
			loops++
			loop = n
			if n.Attrs["lf_contract"] != "owner_conversation" {
				faults = append(faults, errors.New("bug loop must select owner conversation"))
			}
		case "lf_agent":
			if n.Attrs["lf_response"] != "result" {
				faults = append(faults, fmt.Errorf("agent %q needs retained response routing", n.Name))
			}
			switch n.Attrs["lf_agent_markdown"] {
			case "bug-analyst":
				analysts++
				analyst = n
				if n.Attrs["lf_model_tier"] != "flagship" || n.Attrs["lf_access"] != "read" {
					faults = append(faults, errors.New("analyst requires configured flagship read access"))
				}
			case "bug-fixer":
				fixers++
				fixer = n
			default:
				faults = append(faults, errors.New("bug graph contains unauthorized agent role"))
			}
		case SyncBaseAction:
			if len(n.Outcomes) != 4 || !slices.Contains(n.Outcomes, "synced") || !slices.Contains(n.Outcomes, "current") || !slices.Contains(n.Outcomes, "conflict") || !slices.Contains(n.Outcomes, "retry") {
				faults = append(faults, fmt.Errorf("base sync %q requires its four outcomes", n.Name))
			}
			for _, outcome := range []string{"conflict", "retry"} {
				if _, ok := Decide(p, n.Name, outcome); ok {
					faults = append(faults, fmt.Errorf("base sync %q must park on %s", n.Name, outcome))
				}
			}
		case "lf_bug_finalize":
			finals++
			final = n
			if n.Attrs["lf_owner_continuation"] != "required" {
				faults = append(faults, errors.New("revalidation requires claimed owner continuation"))
			}
		default:
			faults = append(faults, fmt.Errorf("bug activity %q is outside loop, agent and finalization contract", n.Action))
		}
	}
	if loops != 1 || analysts != 1 || fixers != 1 || finals != 1 {
		return errors.Join(append(faults, errors.New("bug requires one conversation, analyst, fixer and finalization"))...)
	}
	if p.Start != loop.Name {
		faults = append(faults, errors.New("bug starts at owner conversation"))
	}
	// Sync success chains are transparent to the existing authority routes.
	resolve := func(name string) (string, bool) {
		seen := map[string]bool{}
		for {
			n, ok := p.Node(name)
			if !ok {
				return "", false
			}
			if n.Action != SyncBaseAction {
				return name, true
			}
			if seen[name] {
				return "", false
			}
			seen[name] = true
			synced, a := Decide(p, name, "synced")
			current, b := Decide(p, name, "current")
			if !a || !b || synced != current {
				return "", false
			}
			name = synced
		}
	}
	route := func(from Node, outcome, to string) {
		next, ok := Decide(p, from.Name, outcome)
		resolved, valid := resolve(next)
		if !ok || !valid || resolved != to {
			faults = append(faults, fmt.Errorf("%q %q must route to declared capability %q", from.Name, outcome, to))
		}
	}
	exact := func(n Node, want []string) {
		if len(n.Outcomes) != len(want) {
			faults = append(faults, fmt.Errorf("%q has unsupported outcome set", n.Name))
			return
		}
		for _, v := range want {
			if !slices.Contains(n.Outcomes, v) {
				faults = append(faults, fmt.Errorf("%q lacks %q", n.Name, v))
			}
		}
	}
	exact(loop, []string{"analyse", "approved", "blocked"})
	exact(analyst, []string{"ask", "done", "blocked"})
	exact(fixer, []string{"succeeded", "reshape", "blocked"})
	exact(final, []string{"completed", "revalidate", "stale", "blocked"})
	route(loop, "analyse", analyst.Name)
	route(loop, "approved", fixer.Name)
	route(analyst, "ask", loop.Name)
	route(analyst, "done", loop.Name)
	route(fixer, "reshape", loop.Name)
	route(fixer, "succeeded", final.Name)
	route(final, "revalidate", fixer.Name)
	next, ok := Decide(p, final.Name, "completed")
	target, found := p.Node(next)
	if resolved, valid := resolve(next); valid {
		target, found = p.Node(resolved)
	}
	if !ok || !found || !target.Terminal {
		faults = append(faults, errors.New("confirmed finalization must lead to terminal"))
	}
	// No other edge can bypass approval, repeat repair, or create an extra loop.
	for _, edge := range p.Edges {
		allowed := edge.From == loop.Name && (edge.On == "analyse" || edge.On == "approved") || edge.From == analyst.Name && (edge.On == "ask" || edge.On == "done") || edge.From == fixer.Name && (edge.On == "reshape" || edge.On == "succeeded") || edge.From == final.Name && (edge.On == "completed" || edge.On == "revalidate")
		if node, exists := p.Node(edge.From); exists && node.Action == SyncBaseAction {
			allowed = edge.On == "synced" || edge.On == "current"
		}
		if !allowed {
			faults = append(faults, fmt.Errorf("edge %q to %q bypasses conversation authority", edge.From, edge.To))
		}
	}
	return errors.Join(faults...)
}
