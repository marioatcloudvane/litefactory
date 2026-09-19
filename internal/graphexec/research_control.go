package graphexec

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
)

// Research control actions expose only closed, typed response comparisons.
const (
	ResearchLoopAction = "lf_research_loop"
	ResearchIfAction   = "lf_research_if"
)

// ResearchControlSpec declares data-only string comparisons, never expressions.
func ResearchControlSpec(action string) ActionSpec {
	attrs := map[string]AttributeSpec{
		"lf_field":     {Required: true, Values: []string{"response"}},
		"lf_condition": {Required: true, Values: []string{"ask", "done"}},
	}
	if action == ResearchLoopAction {
		attrs["lf_default"] = AttributeSpec{Values: []string{"ask", "done"}}
	}
	return ActionSpec{Name: action, Mode: agent.ModeResearch, Outcomes: []string{"true", "false"}, Attributes: attrs}
}

// ResearchCondition only inspects the response of an already validated result.
// Attempted distinguishes the initial empty state from a missing/invalid result.
type ResearchCondition struct{ Field, Condition, Default string }

// DecodeResearchCondition validates the declared field, equality value and initial default.
func DecodeResearchCondition(action string, attrs map[string]string) (ResearchCondition, error) {
	c := ResearchCondition{Field: attrs["lf_field"], Condition: attrs["lf_condition"], Default: attrs["lf_default"]}
	var faults []error
	if action != ResearchLoopAction && action != ResearchIfAction {
		faults = append(faults, errors.New("unknown Research control action"))
	}
	if c.Field != "response" {
		faults = append(faults, errors.New("lf_field must equal response"))
	}
	if c.Condition != "ask" && c.Condition != "done" {
		faults = append(faults, errors.New("lf_condition must equal ask or done"))
	}
	if value, present := attrs["lf_default"]; present && (action != ResearchLoopAction || (value != "ask" && value != "done")) {
		faults = append(faults, errors.New("lf_default is only permitted on a loop and must equal ask or done"))
	}
	return c, errors.Join(faults...)
}

// Evaluate selects a declared outcome, applying the default only before an attempt.
func (c ResearchCondition) Evaluate(response string, attempted bool) (string, error) {
	if c.Field != "response" || (c.Condition != "ask" && c.Condition != "done") {
		return "", errors.New("invalid Research condition")
	}
	if !attempted {
		response = c.Default
	}
	if response != "ask" && response != "done" {
		return "", errors.New("research response is missing or invalid; retained findings require an explicit owner continuation")
	}
	if response == c.Condition {
		return "true", nil
	}
	return "false", nil
}

// ValidateResearchControlGraph checks control attributes and all possible ask
// paths before dispatch. A question publication must end at a terminal: the
// continuation service alone can later resume the originating agent node.
func ValidateResearchControlGraph(p *Profile) error {
	if p == nil {
		return errors.New("research graph is nil")
	}
	var faults []error
	for _, node := range p.Nodes {
		switch node.Action {
		case ResearchLoopAction, ResearchIfAction:
			if _, err := DecodeResearchCondition(node.Action, node.Attrs); err != nil {
				faults = append(faults, fmt.Errorf("node %q: %w", node.Name, err))
			}
			if len(node.Outcomes) != 2 || !slices.Contains(node.Outcomes, "true") || !slices.Contains(node.Outcomes, "false") {
				faults = append(faults, fmt.Errorf("node %q requires exactly true and false outcomes", node.Name))
			}
			if node.Action == ResearchLoopAction && node.Limit <= 0 {
				faults = append(faults, fmt.Errorf("node %q requires finite positive lf_limit", node.Name))
			}
		case "lf_research_agent":
			// Catalog-only agent graphs remain valid. Runtime control graphs must end
			// every ask path at a question publication and durable wait boundary.
			hasControl := false
			for _, candidate := range p.Nodes {
				if candidate.Action == ResearchLoopAction || candidate.Action == ResearchIfAction {
					hasControl = true
				}
			}
			if hasControl {
				next, ok := Decide(p, node.Name, "success")
				if !ok {
					faults = append(faults, fmt.Errorf("research agent %q has no deterministic success edge", node.Name))
					continue
				}
				for _, response := range []string{"ask", "done"} {
					if err := validateResearchResponsePath(p, next, response, map[string]bool{}); err != nil {
						faults = append(faults, fmt.Errorf("research agent %q response %q: %w", node.Name, response, err))
					}
				}
				// A failure edge is optional. When declared it may only reach a
				// failure publication that ends the graph: a failed attempt owns
				// no response, so it can never authorize another agent.
				if target, declared := Decide(p, node.Name, "failed"); declared {
					if err := validateResearchFailurePath(p, target); err != nil {
						faults = append(faults, fmt.Errorf("research agent %q failure path: %w", node.Name, err))
					}
				}
			}
		}
	}
	return errors.Join(faults...)
}

// validateResearchFailurePath accepts only a failure publication that ends at a
// terminal. Publication cannot change failed process history, so the failed
// attempt's own run outcome is untouched by whatever this node reports.
func validateResearchFailurePath(p *Profile, name string) error {
	node, ok := p.Node(name)
	if !ok {
		return fmt.Errorf("failure path references missing node %q", name)
	}
	if node.Action != "lf_research_comment" {
		return fmt.Errorf("failure path requires a comment activity at %q", name)
	}
	if node.Attrs["lf_kind"] != "failure" {
		return fmt.Errorf("failure path requires a failure comment at %q", name)
	}
	next, exists := Decide(p, name, "published")
	terminal, found := p.Node(next)
	if !exists || !found || !terminal.Terminal {
		return fmt.Errorf("research failure comment %q must end at a terminal", name)
	}
	return nil
}

func validateResearchResponsePath(p *Profile, name, response string, visiting map[string]bool) error {
	node, ok := p.Node(name)
	if !ok {
		return fmt.Errorf("response path references missing node %q", name)
	}
	if visiting[name] {
		return fmt.Errorf("response path cycles at %q without owner authorization", name)
	}
	if node.Terminal {
		return fmt.Errorf("response path reaches %q without required publication", name)
	}
	visiting[name] = true
	defer delete(visiting, name)
	switch node.Action {
	case "lf_research_agent":
		return fmt.Errorf("response path invokes agent %q without owner authorization", name)
	case "lf_research_comment":
		kind := "final"
		if response == "ask" {
			kind = "question"
		}
		if node.Attrs["lf_kind"] != kind {
			return fmt.Errorf("response path requires %s comment at %q", kind, name)
		}
		next, exists := Decide(p, name, "published")
		terminal, found := p.Node(next)
		if !exists || !found || !terminal.Terminal {
			return fmt.Errorf("research comment %q must end at a terminal", name)
		}
		return nil
	case ResearchLoopAction, ResearchIfAction:
		c, err := DecodeResearchCondition(node.Action, node.Attrs)
		if err != nil {
			return err
		}
		outcome, err := c.Evaluate(response, true)
		if err != nil {
			return err
		}
		next, exists := Decide(p, name, outcome)
		if !exists {
			return fmt.Errorf("response path at %q lacks deterministic %q edge", name, outcome)
		}
		return validateResearchResponsePath(p, next, response, visiting)
	default:
		return fmt.Errorf("response path uses unsupported activity %q", node.Action)
	}
}
