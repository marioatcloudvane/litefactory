package graphexec

import "github.com/cloudvane/litefactory/internal/agent"

// PlanGateAction is the Plan-only generic owner stop. It publishes the agent's
// review with the owner command the graph names and waits for that command.
const PlanGateAction = "lf_gate"

// Plan gate vocabulary. Both attributes are closed to what Go can honor today.
const (
	PlanGateCommand = "/approve-plan"
	PlanGateArgs    = "story,stage,candidate"
)

// PlanGateSpec declares the gate. Only Plan registers it.
func PlanGateSpec() ActionSpec {
	return ActionSpec{Name: PlanGateAction, Mode: agent.ModePlan, Outcomes: []string{"published"}, Attributes: map[string]AttributeSpec{
		"lf_command": {Required: true, Values: []string{PlanGateCommand}},
		"lf_args":    {Required: true, List: true, Values: []string{"story", "stage", "candidate"}},
	}}
}
