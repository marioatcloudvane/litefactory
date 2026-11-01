package graphexec

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/pkg/execution/graph"
)

// AttributeSpec is a closed vocabulary supplied by factory action descriptions.
type AttributeSpec = graph.AttributeSpec

// ActionSpec defines membership and outcomes, never executable attribute text.
type ActionSpec struct {
	Name       string
	Mode       agent.Mode
	Workflow   agent.Workflow
	Outcomes   []string
	Attributes map[string]AttributeSpec
}

// Registry is build-then-freeze: registration finishes before any shared use.
// The zero value is ready for registration. Both boundaries clone outcome slices.
type Registry struct {
	actions map[string]ActionSpec
	Policy  func(*Profile) error
}

var capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validWorkflowActionName(name string, mode agent.Workflow) bool {
	if slices.Contains([]string{"lf_research_agent", "lf_research_if", "lf_research_loop", "lf_research_comment"}, name) {
		return false
	}
	if mode.Valid() && slices.Contains([]string{"lf_agent", "lf_if", "lf_loop", "lf_comment", "lf_ask", PlanGateAction, JudgeAction, SyncBaseAction}, name) {
		return true
	}
	prefix := "lf_" + string(mode) + "_"
	return mode.Valid() && strings.HasPrefix(name, prefix) && capabilityPattern.MatchString(strings.TrimPrefix(name, prefix))
}

// Register adds one valid spec; duplicate names never overwrite earlier specs.
func (r *Registry) Register(spec ActionSpec) error {
	if r == nil {
		return errors.New("register graph action: nil registry")
	}
	workflow, err := agent.ResolveWorkflow(spec.Workflow, spec.Mode)
	if err != nil {
		return fmt.Errorf("register graph action %q: %w", spec.Name, err)
	}
	if !validWorkflowActionName(spec.Name, workflow) {
		return fmt.Errorf("register graph action %q: name must be a generic mechanism or admitted lf_<mode>_<capability> for valid mode %q", spec.Name, spec.Mode)
	}
	if len(spec.Outcomes) == 0 {
		return fmt.Errorf("register graph action %q: outcomes must be nonempty", spec.Name)
	}
	seen := make(map[string]bool)
	for _, outcome := range spec.Outcomes {
		if strings.TrimSpace(outcome) == "" || seen[outcome] {
			return fmt.Errorf("register graph action %q: empty or duplicate outcome %q", spec.Name, outcome)
		}
		seen[outcome] = true
	}
	if _, exists := r.actions[spec.Name]; exists {
		return fmt.Errorf("register graph action %q: already registered", spec.Name)
	}
	if r.actions == nil {
		r.actions = make(map[string]ActionSpec)
	}
	spec = cloneActionSpec(spec)
	r.actions[spec.Name] = spec
	return nil
}

// Resolve reports membership and returns a spec with independently owned slices.
func (r Registry) Resolve(name string) (ActionSpec, bool) {
	spec, ok := r.actions[name]
	spec = cloneActionSpec(spec)
	return spec, ok
}

func cloneActionSpec(spec ActionSpec) ActionSpec {
	spec.Outcomes = slices.Clone(spec.Outcomes)
	spec.Attributes = maps.Clone(spec.Attributes)
	for name, attribute := range spec.Attributes {
		attribute.Values = slices.Clone(attribute.Values)
		spec.Attributes[name] = attribute
	}
	return spec
}

// AccessAttribute names the agent node attribute that declares its tool access.
const AccessAttribute = "lf_access"

// AccessValues are the closed access levels an agent node may declare.
var AccessValues = []string{"read", "write"}

// BudgetAttribute names the optional agent node attribute that bounds how long
// one run of the node may take, in whole minutes. Absent means no bound.
const BudgetAttribute = "lf_budget"

// MaxAgentBudgetMinutes is the largest time budget a node may declare, so a typo
// cannot turn a bounded agent into an effectively unbounded one.
const MaxAgentBudgetMinutes = 1440

// AgentBudget reads the time budget a node declares, in whole minutes. The second
// result is false when it declares none. A value that is not a whole number of
// minutes from 1 to [MaxAgentBudgetMinutes] is an error. The graph package keeps
// no clock, so the caller turns minutes into a duration.
func AgentBudget(attrs map[string]string) (int, bool, error) {
	value, present := attrs[BudgetAttribute]
	if !present {
		return 0, false, nil
	}
	minutes, err := strconv.Atoi(value)
	if err != nil || minutes < 1 || minutes > MaxAgentBudgetMinutes {
		return 0, false, fmt.Errorf("%s %q must be a whole number of minutes from 1 to %d", BudgetAttribute, value, MaxAgentBudgetMinutes)
	}
	return minutes, true, nil
}

func budgetValues() []string {
	values := make([]string, 0, MaxAgentBudgetMinutes)
	for minutes := 1; minutes <= MaxAgentBudgetMinutes; minutes++ {
		values = append(values, strconv.Itoa(minutes))
	}
	return values
}

// AgentAttributes declares the optional attributes of the shared agent action.
func AgentAttributes() map[string]AttributeSpec {
	attrs := OwnerStopAttributes()
	attrs[AccessAttribute] = AttributeSpec{Values: slices.Clone(AccessValues)}
	attrs[BudgetAttribute] = AttributeSpec{Values: budgetValues()}
	attrs["lf_response"] = AttributeSpec{Values: []string{"result"}}
	return attrs
}
