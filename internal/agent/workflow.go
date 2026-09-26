package agent

import "fmt"

// Workflow is the closed execution identity. Bug is standalone, not a Mode.
type Workflow string

// Supported workflow identities include the five modes and standalone Bug.
const (
	WorkflowResearch  Workflow = "research"
	WorkflowPlan      Workflow = "plan"
	WorkflowImplement Workflow = "implement"
	WorkflowValidate  Workflow = "validate"
	WorkflowSpike     Workflow = "spike"
	WorkflowBug       Workflow = "bug"
)

// WorkflowForMode maps only the five supported modes.
func WorkflowForMode(m Mode) (Workflow, bool) {
	switch m {
	case ModeResearch:
		return WorkflowResearch, true
	case ModePlan:
		return WorkflowPlan, true
	case ModeImplement:
		return WorkflowImplement, true
	case ModeValidate:
		return WorkflowValidate, true
	case ModeSpike:
		return WorkflowSpike, true
	default:
		return "", false
	}
}

// Mode returns the compatibility mode; standalone workflows have none.
func (w Workflow) Mode() (Mode, bool) {
	switch w {
	case WorkflowResearch:
		return ModeResearch, true
	case WorkflowPlan:
		return ModePlan, true
	case WorkflowImplement:
		return ModeImplement, true
	case WorkflowValidate:
		return ModeValidate, true
	case WorkflowSpike:
		return ModeSpike, true
	default:
		return "", false
	}
}

// Valid rejects unknown workflow identities.
func (w Workflow) Valid() bool { _, ok := w.Mode(); return ok || w == WorkflowBug }

// ResolveWorkflow validates an explicit binding or maps a historical mode.
func ResolveWorkflow(w Workflow, m Mode) (Workflow, error) {
	if w == "" {
		if mapped, ok := WorkflowForMode(m); ok {
			return mapped, nil
		}
		return "", fmt.Errorf("invalid mode %q", m)
	}
	if !w.Valid() {
		return "", fmt.Errorf("invalid workflow %q", w)
	}
	if m != "" {
		mapped, ok := WorkflowForMode(m)
		if !ok || mapped != w {
			return "", fmt.Errorf("workflow %q conflicts with mode %q", w, m)
		}
	}
	return w, nil
}

// ParseWorkflow accepts an exact canonical execution identity.
func ParseWorkflow(value string) (Workflow, bool) {
	w := Workflow(value)
	return w, w.Valid()
}
