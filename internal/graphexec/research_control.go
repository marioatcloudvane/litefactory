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

// ControlPolicy is the admitted mode's closed response and owner-gate vocabulary.
type ControlPolicy struct {
	Mode                agent.Mode
	Workflow            agent.Workflow
	Responses, Defaults []string
	Ask, Approved       string
	// MeasureActions are mode-declared steps that do one measurable thing and
	// return an outcome. ResponseReachesOwnerWait follows each of their outcome
	// edges. They never pick a next step themselves.
	MeasureActions []string
}

// ResearchControlPolicy supplies Research's closed response vocabulary: ask or done.
func ResearchControlPolicy() ControlPolicy {
	return ControlPolicy{Mode: agent.ModeResearch, Responses: ResearchResponses, Defaults: []string{"ask", "done"}, Ask: "ask"}
}

// ControlSpec declares the closed comparisons authorized by the admitted mode policy.
func ControlSpec(action string, policy ControlPolicy) ActionSpec {
	attrs := map[string]AttributeSpec{
		"lf_field":     {Required: true, Values: []string{"response"}},
		"lf_condition": {Required: true, Values: policy.Responses},
	}
	if policy.Mode == agent.ModePlan && action == ResearchIfAction {
		attrs["lf_field"] = AttributeSpec{Required: true, Values: []string{"response", "story.size", "story.sizing_valid", "story.fallback"}}
		attrs["lf_condition"] = AttributeSpec{Required: true, Values: append(slices.Clone(policy.Responses), "S", "M", "L", "true", "false")}
	}
	if policy.Mode == agent.ModeImplement && action == ResearchIfAction {
		attrs["lf_field"] = AttributeSpec{Required: true, Values: []string{"story.size", "story.sizing_valid"}}
		attrs["lf_condition"] = AttributeSpec{Required: true, Values: []string{"S", "M", "L", "true", "false"}}
	}
	if action == ResearchLoopAction {
		attrs["lf_default"] = AttributeSpec{Values: policy.Defaults}
	}
	return ActionSpec{Name: action, Mode: policy.Mode, Workflow: policy.Workflow, Outcomes: []string{"true", "false"}, Attributes: attrs}
}

// ResearchResponses are the two things a Research agent can say about its own
// turn: it asks the owner, or it is done.
var ResearchResponses = []string{"ask", "done"}

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
	if policy.Mode == agent.ModeImplement {
		return decodeImplementFact(action, attrs)
	}
	if policy.Mode == agent.ModePlan && c.Field != "response" {
		return decodePlanFact(action, attrs)
	}
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
		case JudgeAction:
			next, ok := Decide(p, name, "success")
			if !ok {
				return "", nil
			}
			name = next
		case "lf_comment", "lf_ask":
			if node.Action == "lf_comment" && node.Attrs["lf_kind"] == "advisory" {
				next, exists := Decide(p, name, "published")
				if !exists {
					return "", nil
				}
				name = next
				continue
			}
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

// ResponseReachesOwnerWait reports whether an agent's ask response can come to
// rest at an owner question. An agent whose ask never does has no owner to wait
// for, so it must not be offered the chance to ask. It follows the same edges
// the response validation follows, and stops at any other agent's boundary.
func ResponseReachesOwnerWait(p *Profile, agentName string, policy ControlPolicy) bool {
	if p == nil {
		return false
	}
	start := "success"
	if node, found := p.Node(agentName); found && RoutesResponse(node) {
		start = policy.Ask
	}
	next, ok := Decide(p, agentName, start)
	return ok && reachesOwnerWait(p, next, policy, map[string]bool{})
}

// RoutesResponse reports an agent node whose response is its outcome
// (lf_response=result), so its DOT routes each response on its own edge.
func RoutesResponse(node Node) bool {
	return node.Action == "lf_agent" && node.Attrs["lf_response"] == "result"
}

func reachesOwnerWait(p *Profile, name string, policy ControlPolicy, seen map[string]bool) bool {
	node, ok := p.Node(name)
	if !ok || node.Terminal || seen[name] {
		return false
	}
	seen[name] = true
	switch {
	case node.Action == "lf_ask":
		return true
	case node.Action == ResearchLoopAction || node.Action == ResearchIfAction:
		c, err := DecodeCondition(node.Action, node.Attrs, policy)
		if err != nil {
			return false
		}
		outcome, err := c.Evaluate(policy.Ask, true)
		if err != nil {
			return false
		}
		next, exists := Decide(p, name, outcome)
		return exists && reachesOwnerWait(p, next, policy, seen)
	case node.Action == "lf_comment":
		next, exists := Decide(p, name, "published")
		return exists && reachesOwnerWait(p, next, policy, seen)
	case node.Action == JudgeAction || slices.Contains(policy.MeasureActions, node.Action):
		for _, outcome := range node.Outcomes {
			if next, exists := Decide(p, name, outcome); exists && reachesOwnerWait(p, next, policy, seen) {
				return true
			}
		}
	}
	return false
}
