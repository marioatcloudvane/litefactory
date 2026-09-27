package teamsource

import (
	"context"
	"errors"
	"io/fs"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// CaptureBugTests admits only the finite factory check child. Unlike an agent
// workflow, this package has no roles and grants no result-service authority.
func CaptureBugTests(ctx context.Context, registry graphexec.Registry) (*WorkflowDefinition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := fs.ReadFile(bugAssets, "bug/bug_tests.dot")
	if err != nil {
		return nil, err
	}
	p, err := graphexec.ParseProfile(raw)
	if err != nil {
		return nil, err
	}
	if err = graphexec.Validate(p, registry); err != nil {
		return nil, err
	}
	if err = ValidateBugTests(p); err != nil {
		return nil, err
	}
	return &WorkflowDefinition{Profile: p, Files: map[string][]byte{"bug_tests.dot": raw}, Hashes: map[string]string{"bug_tests.dot": p.Digest}}, nil
}

// ValidateBugTests refuses arbitrary graphs, roles, recursion, delivery and
// altered phase ordering even if those actions are registered elsewhere.
func ValidateBugTests(p *graphexec.Profile) error {
	if p == nil || p.Workflow != agent.WorkflowBug || p.Mode != "" || p.Team != "builtin" || p.Start != "regression" || len(p.Nodes) != 7 || len(p.Edges) != 12 {
		return errors.New("invalid Bug test graph topology")
	}
	if p.ProfileVersion != 1 {
		return errors.New("unsupported Bug test profile version")
	}
	for key := range p.Attrs {
		switch key {
		case "team", "workflow", "profile_version", "start":
		default:
			return errors.New("unsupported Bug test graph attribute")
		}
	}
	for _, n := range p.Nodes {
		for key := range n.Attrs {
			if n.Terminal {
				if key != "lf_terminal" {
					return errors.New("unsupported Bug test terminal attribute")
				}
				continue
			}
			switch key {
			case "lf_action", "lf_phase", "lf_outcomes", "lf_limit":
			default:
				return errors.New("unsupported Bug test node attribute")
			}
		}
	}
	for _, edge := range p.Edges {
		for key := range edge.Attrs {
			if key != "lf_on" {
				return errors.New("unsupported Bug test edge attribute")
			}
		}
	}
	for _, phase := range []string{"regression", "focused", "full"} {
		n, ok := p.Node(phase)
		if !ok || n.Terminal || n.Action != "lf_bug_checks" || n.Limit != 1 || n.Attrs["lf_phase"] != phase || len(n.Outcomes) != 4 {
			return errors.New("invalid Bug test phase")
		}
		next := "focused"
		if phase == "focused" {
			next = "full"
		}
		if phase == "full" {
			next = "passed"
		}
		for _, outcome := range []string{"passed", "failed", "blocked", "cancelled"} {
			expected := outcome
			if outcome == "passed" {
				expected = next
			}
			got, ok := graphexec.Decide(p, phase, outcome)
			if !ok || got != expected {
				return errors.New("invalid Bug test outcome route")
			}
		}
	}
	for _, name := range []string{"passed", "failed", "blocked", "cancelled"} {
		n, ok := p.Node(name)
		if !ok || !n.Terminal || n.Action != "" {
			return errors.New("invalid Bug test terminal")
		}
	}
	return nil
}
