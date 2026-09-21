package graphexec

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
)

// Research control actions expose only closed, typed response comparisons.
const (
	ResearchLoopAction = "lf_loop"
	ResearchIfAction   = "lf_if"
)

// ResearchControlSpec declares data-only string comparisons, never expressions.
func ResearchControlSpec(action string) ActionSpec {
	return ControlSpec(action, ResearchControlPolicy())
}

// ControlPolicy is the admitted mode's closed response and owner-gate vocabulary.
type ControlPolicy struct {
	Generation          string
	Mode                agent.Mode
	Responses, Defaults []string
	Ask, Approved       string
}

// ResearchControlPolicy supplies Research response vocabulary and its document gate.
func ResearchControlPolicy() ControlPolicy {
	return ControlPolicy{Generation: "lf_research_brief", Mode: agent.ModeResearch, Responses: ResearchResponses, Defaults: []string{"ask", "done"}, Ask: "ask", Approved: "approved"}
}

// ControlSpec declares the closed comparisons authorized by the admitted mode policy.
func ControlSpec(action string, policy ControlPolicy) ActionSpec {
	attrs := map[string]AttributeSpec{
		"lf_field":     {Required: true, Values: []string{"response"}},
		"lf_condition": {Required: true, Values: policy.Responses},
	}
	if action == ResearchLoopAction {
		attrs["lf_default"] = AttributeSpec{Values: policy.Defaults}
	}
	return ActionSpec{Name: action, Mode: policy.Mode, Outcomes: []string{"true", "false"}, Attributes: attrs}
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
type ResearchCondition struct {
	Field, Condition, Default string
	Responses                 []string
}

// DecodeResearchCondition validates the declared field, equality value and initial default.
func DecodeResearchCondition(action string, attrs map[string]string) (ResearchCondition, error) {
	return DecodeCondition(action, attrs, ResearchControlPolicy())
}

// DecodeCondition validates comparison attributes against the admitted response vocabulary.
func DecodeCondition(action string, attrs map[string]string, policy ControlPolicy) (ResearchCondition, error) {
	c := ResearchCondition{Responses: policy.Responses, Field: attrs["lf_field"], Condition: attrs["lf_condition"], Default: attrs["lf_default"]}
	var faults []error
	if action != ResearchLoopAction && action != ResearchIfAction {
		faults = append(faults, errors.New("unknown workflow control action"))
	}
	if c.Field != "response" {
		faults = append(faults, errors.New("lf_field must equal response"))
	}
	if !slices.Contains(policy.Responses, c.Condition) {
		faults = append(faults, fmt.Errorf("lf_condition must be one of %v", policy.Responses))
	}
	if value, present := attrs["lf_default"]; present && (action != ResearchLoopAction || !slices.Contains(policy.Defaults, value)) {
		faults = append(faults, fmt.Errorf("lf_default is only permitted on a loop and must be one of %v", policy.Defaults))
	}
	return c, errors.Join(faults...)
}

// Evaluate selects a declared outcome, applying the default only before an attempt.
func (c ResearchCondition) Evaluate(response string, attempted bool) (string, error) {
	responses := c.Responses
	if responses == nil {
		responses = ResearchResponses
	}
	if c.Field != "response" || !slices.Contains(responses, c.Condition) {
		return "", errors.New("invalid workflow condition")
	}
	if !attempted {
		response = c.Default
	}
	if !slices.Contains(responses, response) {
		return "", errors.New("workflow response is missing or invalid; retained findings require an explicit owner continuation")
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
	return ValidateControlGraph(p, ResearchControlPolicy())
}

// ValidateControlGraph checks source bindings, generation prerequisites and owner-wait paths.
func ValidateControlGraph(p *Profile, policy ControlPolicy) error {
	if p == nil {
		return errors.New("research graph is nil")
	}
	var faults []error
	for _, node := range p.Nodes {
		if policy.Generation != "" && node.Action == policy.Generation {
			source, ok := p.Node(node.Attrs["lf_source"])
			if !ok || source.Action != "lf_agent" {
				faults = append(faults, fmt.Errorf("generation %q requires named agent source", node.Name))
			}
		}
		switch node.Action {
		case ResearchLoopAction, ResearchIfAction:
			if _, err := DecodeCondition(node.Action, node.Attrs, policy); err != nil {
				faults = append(faults, fmt.Errorf("node %q: %w", node.Name, err))
			}
			// An unscoped control node reads whichever agent ran last, which is
			// the single-loop behaviour. Naming a source is what lets a later
			// loop bootstrap from its own default instead of inheriting the
			// previous stage's response.
			if source, scoped := node.Attrs["lf_source"]; scoped {
				if from, exists := p.Node(source); !exists || from.Action != "lf_agent" {
					faults = append(faults, fmt.Errorf("node %q: lf_source %q does not name an agent node", node.Name, source))
				}
			}
			if len(node.Outcomes) != 2 || !slices.Contains(node.Outcomes, "true") || !slices.Contains(node.Outcomes, "false") {
				faults = append(faults, fmt.Errorf("node %q requires exactly true and false outcomes", node.Name))
			}
			if node.Action == ResearchLoopAction && node.Limit <= 0 {
				faults = append(faults, fmt.Errorf("node %q requires finite positive lf_limit", node.Name))
			}
		case "lf_agent":
			// Catalog-only agent graphs remain valid. Runtime control graphs must end
			// every ask path at a question publication and durable wait boundary.
			hasControl := false
			for _, candidate := range p.Nodes {
				if candidate.Action == ResearchLoopAction || candidate.Action == ResearchIfAction || candidate.Action == "lf_comment" || candidate.Action == "lf_ask" {
					hasControl = true
				}
			}
			if hasControl {
				next, ok := Decide(p, node.Name, "success")
				if !ok {
					faults = append(faults, fmt.Errorf("research agent %q has no deterministic success edge", node.Name))
					continue
				}
				for _, response := range policy.Responses {
					if err := validateResearchResponsePath(p, next, response, node.Name, map[string]bool{}, policy); err != nil {
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
	if node.Action != "lf_comment" {
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
func validateResearchResponsePath(p *Profile, name, response, source string, visiting map[string]bool, policy ControlPolicy) error {
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
	if policy.Generation != "" && node.Action == policy.Generation {
		if node.Attrs["lf_source"] != source {
			return fmt.Errorf("generation %q source does not match %q", name, source)
		}
		next, ok := Decide(p, name, "success")
		if !ok {
			return fmt.Errorf("generation %q lacks success edge", name)
		}
		visiting["generated:"+source] = true
		return validateResearchResponsePath(p, next, response, source, visiting, policy)
	}
	switch node.Action {
	case "lf_agent":
		return fmt.Errorf("response path invokes agent %q without owner authorization", name)
	case "lf_comment", "lf_ask":
		if policy.Generation != "" && !visiting["generated:"+source] {
			return fmt.Errorf("publication %q requires matching successful %s generation", name, policy.Generation)
		}
		if declared := node.Attrs["lf_source"]; declared != "" && declared != source {
			return fmt.Errorf("publication %q source differs from current agent", name)
		}
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
			if node.Action != "lf_ask" {
				return fmt.Errorf("owner wait requires lf_ask at %q", name)
			}
			// Both rest on the owner, for different reasons: a question is the
			// agent needing an answer, a review is the agent showing finished
			// work and waiting to be told it stands. Neither may run on
			// unanswered, and an approved response has already been answered.
			if response == policy.Approved {
				return fmt.Errorf("an approved response is settled and cannot end at the %s at %q", kind, name)
			}
			if (kind == "question") != (response == policy.Ask) {
				return fmt.Errorf("response %q cannot end at a %s publication at %q", response, kind, name)
			}
			if !after.Terminal {
				return fmt.Errorf("research %s %q must end at a durable owner wait", kind, name)
			}
			return nil
		case "final":
			if node.Action != "lf_comment" {
				return fmt.Errorf("report requires lf_comment at %q", name)
			}
			if response == policy.Ask {
				return fmt.Errorf("response path requires question comment at %q", name)
			}
			if after.Terminal {
				return nil
			}
			visiting["published:"+source] = true
			return validateResearchResponsePath(p, next, response, source, visiting, policy)
		default:
			return fmt.Errorf("response path reaches %q publication at %q", kind, name)
		}
	case ResearchLoopAction, ResearchIfAction:
		// A control node reading a different agent is that agent's boundary, not
		// this one's. Its own paths are validated from its own agent's edge.
		if scope, scoped := node.Attrs["lf_source"]; scoped && scope != source {
			if response == policy.Ask || !visiting["published:"+source] {
				return fmt.Errorf("stage handoff at %q bypasses required publication or wait", name)
			}
			return nil
		}
		c, err := DecodeCondition(node.Action, node.Attrs, policy)
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
		return validateResearchResponsePath(p, next, response, source, visiting, policy)
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
		case "lf_research_brief":
			next, ok := Decide(p, name, "success")
			if !ok {
				return "", nil
			}
			name = next
		case "lf_comment", "lf_ask":
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
