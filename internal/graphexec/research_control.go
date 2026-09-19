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
		"lf_condition": {Required: true, Values: ResearchResponses},
	}
	if action == ResearchLoopAction {
		attrs["lf_default"] = AttributeSpec{Values: []string{"ask", "done"}}
	}
	return ActionSpec{Name: action, Mode: agent.ModeResearch, Outcomes: []string{"true", "false"}, Attributes: attrs}
}

// ResearchResponses are the three things a Research agent can say about its own
// turn. "ask" and "done" are its own judgement. "approved" is not: it reports
// that the owner said the work is good enough, so an agent may only return it
// on a resumed pass that actually carries such a reply. That distinction is the
// whole point — advancing a stage is the owner's decision, not the agent's.
var ResearchResponses = []string{"ask", "done", "approved"}

// ResearchResponse reports whether a value is one of the declared responses.
func ResearchResponse(value string) bool { return slices.Contains(ResearchResponses, value) }

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
	if !ResearchResponse(c.Condition) {
		faults = append(faults, errors.New("lf_condition must equal ask, done or approved"))
	}
	if value, present := attrs["lf_default"]; present && (action != ResearchLoopAction || (value != "ask" && value != "done")) {
		faults = append(faults, errors.New("lf_default is only permitted on a loop and must equal ask or done"))
	}
	return c, errors.Join(faults...)
}

// Evaluate selects a declared outcome, applying the default only before an attempt.
func (c ResearchCondition) Evaluate(response string, attempted bool) (string, error) {
	if c.Field != "response" || !ResearchResponse(c.Condition) {
		return "", errors.New("invalid Research condition")
	}
	if !attempted {
		response = c.Default
	}
	if !ResearchResponse(response) {
		return "", errors.New("research response is missing or invalid; retained findings require an explicit owner continuation")
	}
	if response == c.Condition {
		return "true", nil
	}
	return "false", nil
}

// ResearchSource names the agent node a control or comment node reads. An empty
// result means the legacy reading: whichever agent node ran most recently.
func ResearchSource(attrs map[string]string) string { return attrs["lf_source"] }

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
			// An unscoped control node reads whichever agent ran last, which is
			// the single-loop behaviour. Naming a source is what lets a later
			// loop bootstrap from its own default instead of inheriting the
			// previous stage's response.
			if source, scoped := node.Attrs["lf_source"]; scoped {
				if from, exists := p.Node(source); !exists || from.Action != "lf_research_agent" {
					faults = append(faults, fmt.Errorf("node %q: lf_source %q does not name an agent node", node.Name, source))
				}
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
				for _, response := range ResearchResponses {
					if err := validateResearchResponsePath(p, next, response, node.Name, map[string]bool{}); err != nil {
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

// validateResearchResponsePath follows one agent's response to the point where
// that response stops governing the graph. Publication is a step, not an ending:
// a report may advance to whatever comes next, and only an explicit terminal
// ends the workflow. Two boundaries are not negotiable. An ask must still come
// to rest at a durable owner wait, because nothing else stops the graph from
// running on without the answer. And no response path may reach an agent
// directly: crossing into a loop scoped to a different agent is what hands over,
// and that crossing always happens after this stage has published.
func validateResearchResponsePath(p *Profile, name, response, source string, visiting map[string]bool) error {
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
		kind := node.Attrs["lf_kind"]
		next, exists := Decide(p, name, "published")
		if !exists {
			return fmt.Errorf("research comment %q lacks a deterministic published edge", name)
		}
		after, found := p.Node(next)
		if !found {
			return fmt.Errorf("research comment %q publishes to missing node %q", name, next)
		}
		switch kind {
		case "question", "review":
			// Both rest on the owner, for different reasons: a question is the
			// agent needing an answer, a review is the agent showing finished
			// work and waiting to be told it stands. Neither may run on
			// unanswered, and an approved response has already been answered.
			if response == "approved" {
				return fmt.Errorf("an approved response is settled and cannot end at the %s at %q", kind, name)
			}
			if (kind == "question") != (response == "ask") {
				return fmt.Errorf("response %q cannot end at a %s publication at %q", response, kind, name)
			}
			if !after.Terminal {
				return fmt.Errorf("research %s %q must end at a durable owner wait", kind, name)
			}
			return nil
		case "final":
			if response == "ask" {
				return fmt.Errorf("response path requires question comment at %q", name)
			}
			if after.Terminal {
				return nil
			}
			return validateResearchResponsePath(p, next, response, source, visiting)
		default:
			return fmt.Errorf("response path reaches %q publication at %q", kind, name)
		}
	case ResearchLoopAction, ResearchIfAction:
		// A control node reading a different agent is that agent's boundary, not
		// this one's. Its own paths are validated from its own agent's edge.
		if scope, scoped := node.Attrs["lf_source"]; scoped && scope != source {
			return nil
		}
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
		return validateResearchResponsePath(p, next, response, source, visiting)
	default:
		return fmt.Errorf("response path uses unsupported activity %q", node.Action)
	}
}

// ResearchResponseReaches returns the publication one response would reach from
// a starting node, or empty when that response does not end in one. It exists
// so a caller can ask the graph what a node means rather than hardcoding a
// name: the same question activity is a clarification in one place and a review
// gate in another, and only the edges say which.
func ResearchResponseReaches(p *Profile, name, response string) (string, error) {
	for i := 0; i < 32; i++ {
		node, ok := p.Node(name)
		if !ok || node.Terminal {
			return "", nil
		}
		switch node.Action {
		case "lf_research_comment":
			return name, nil
		case ResearchLoopAction, ResearchIfAction:
			c, err := DecodeResearchCondition(node.Action, node.Attrs)
			if err != nil {
				return "", err
			}
			outcome, err := c.Evaluate(response, true)
			if err != nil {
				return "", err
			}
			next, exists := Decide(p, name, outcome)
			if !exists {
				return "", nil
			}
			name = next
		default:
			return "", nil
		}
	}
	return "", nil
}
