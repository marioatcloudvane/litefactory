package graphexec

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/pkg/execution/graph"
)

// Validate retains factory policy around neutral structural checks.
func Validate(p *Profile, registry Registry) error {
	if p == nil {
		return errors.New("graph profile is nil")
	}
	var faults []error
	if _, err := slotCostAttribute(p.Attrs); err != nil {
		faults = append(faults, fmt.Errorf("graph: %w", err))
	}
	faults = append(faults, ValidateJudgeGraph(p))
	if strings.TrimSpace(p.Team) == "" {
		faults = append(faults, errors.New("graph team is empty"))
	}
	workflow, bindingErr := agent.ResolveWorkflow(p.Workflow, p.Mode)
	if bindingErr != nil {
		faults = append(faults, bindingErr)
	}
	if workflow == agent.WorkflowBug && registry.Policy == nil {
		faults = append(faults, errors.New("bug graph has no registered matching policy"))
	}
	if p.ProfileVersion < 1 {
		faults = append(faults, fmt.Errorf("profile_version %d must be positive", p.ProfileVersion))
	}
	r := graph.Registry{}
	for _, spec := range registry.actions {
		_ = r.Register(graph.ActionSpec{Name: spec.Name, Outcomes: spec.Outcomes, Attributes: spec.Attributes})
	}
	faults = append(faults, graph.Validate(p.neutral(), r, graph.ValidationOptions{
		NodeErrors: func(node Node) []error {
			var f []error
			if _, err := slotCostAttribute(node.Attrs); err != nil {
				f = append(f, fmt.Errorf("node %q: %w", node.Name, err))
			}
			return append(f, validateNode(node, workflow, registry)...)
		}, NoEdge: ParksOutcome, LimitName: "lf_limit"}))
	if registry.Policy != nil {
		faults = append(faults, registry.Policy(p))
	}
	return errors.Join(faults...)
}
func validateNode(node Node, mode agent.Workflow, registry Registry) []error {
	var faults []error
	if err := validateOwnerStop(node, mode); err != nil {
		faults = append(faults, fmt.Errorf("node %q: %w", node.Name, err))
	}
	if node.Attrs["lf_limit"] != "" && node.Limit == 0 {
		faults = append(faults, fmt.Errorf("node %q lf_limit must be positive when supplied", node.Name))
	}
	faults = append(faults, validateRightsTag(node, mode)...)
	if err := validateInvokeAttrs(node.Attrs); err != nil {
		faults = append(faults, fmt.Errorf("node %q: %w", node.Name, err))
	}
	if err := validateStandardsAttr(node); err != nil {
		faults = append(faults, fmt.Errorf("node %q: %w", node.Name, err))
	}
	if node.Terminal {
		return faults
	}
	spec, exists := registry.Resolve(node.Action)
	if exists {
		if owner, err := agent.ResolveWorkflow(spec.Workflow, spec.Mode); err != nil || owner != mode {
			faults = append(faults, fmt.Errorf("node %q action %q mode %q does not match graph mode %q", node.Name, node.Action, spec.Mode, mode))
		}
	}
	if !validWorkflowActionName(node.Action, mode) {
		faults = append(faults, fmt.Errorf("node %q action %q must use lf_%s_<capability>", node.Name, node.Action, mode))
	}
	return faults
}

func validateListAttribute(node, name, value string, allowed []string) []error {
	return graph.ValidateListAttribute(node, name, value, allowed)
}

// RightsAttribute tags a human wait node whose listed actions can be delegated
// as managed repository rights.
const RightsAttribute = "lf_rights"

// RightsActions is the closed vocabulary of delegable human actions.
var RightsActions = []string{"answer", "request_changes", "approve"}

// validateRightsTag accepts lf_rights only on lf_ask nodes, only with closed
// values, and never in the Implement graph, which stays owner-only.
func validateRightsTag(node Node, mode agent.Workflow) []error {
	value, present := node.Attrs[RightsAttribute]
	if !present {
		return nil
	}
	if mode == agent.WorkflowImplement {
		return []error{fmt.Errorf("node %q: %s is not allowed in the Implement graph", node.Name, RightsAttribute)}
	}
	if node.Terminal || node.Action != "lf_ask" {
		return []error{fmt.Errorf("node %q: %s is allowed only on lf_ask nodes", node.Name, RightsAttribute)}
	}
	return validateListAttribute(node.Name, RightsAttribute, value, RightsActions)
}
