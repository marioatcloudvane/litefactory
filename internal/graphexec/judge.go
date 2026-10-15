package graphexec

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
)

// JudgeAction challenges a pinned saved result without decision authority.
const JudgeAction = "lf_judge"

// JudgeSpec declares the shared advisory execution contract.
func JudgeSpec(mode agent.Mode) ActionSpec {
	return ActionSpec{Name: JudgeAction, Mode: mode, Outcomes: []string{"success", "failed"}, Attributes: map[string]AttributeSpec{
		"lf_agent":      {Values: []string{"claude", "codex", "other"}},
		BudgetAttribute: {Values: budgetValues()},
	}}
}

// ValidateJudgeGraph checks explicit bindings and the report-only node contract.
func ValidateJudgeGraph(p *Profile) error {
	if p == nil {
		return errors.New("judge graph is nil")
	}
	var faults []error
	for _, n := range p.Nodes {
		if n.Action != JudgeAction && n.Attrs["lf_kind"] != "advisory" {
			continue
		}
		if p.Mode != agent.ModeResearch && p.Mode != agent.ModePlan {
			faults = append(faults, fmt.Errorf("node %q: judging is available only in Research and Plan", n.Name))
		}
		source, ok := p.Node(n.Attrs["lf_source"])
		required := "lf_agent"
		if n.Action != JudgeAction {
			required = JudgeAction
			if n.Action != "lf_comment" || len(n.Outcomes) != 1 || n.Outcomes[0] != "published" {
				faults = append(faults, fmt.Errorf("node %q: advisory publication requires lf_comment and published outcome", n.Name))
			}
		} else {
			if n.Limit <= 0 {
				faults = append(faults, fmt.Errorf("judge %q requires positive lf_limit", n.Name))
			}
			if len(n.Outcomes) != 2 || !slices.Contains(n.Outcomes, "success") || !slices.Contains(n.Outcomes, "failed") {
				faults = append(faults, fmt.Errorf("judge %q requires exactly success and failed outcomes", n.Name))
			}
			allowed := []string{"lf_action", "lf_outcomes", "lf_source", "lf_limit", "lf_agent", "lf_model", "lf_thinking", "lf_focus", BudgetAttribute}
			for _, key := range slices.Sorted(maps.Keys(n.Attrs)) {
				value := n.Attrs[key]
				if strings.HasPrefix(key, "lf_") && !slices.Contains(allowed, key) {
					faults = append(faults, fmt.Errorf("judge %q does not permit %s", n.Name, key))
				}
				if slices.Contains([]string{"lf_model", "lf_thinking", "lf_focus"}, key) && (strings.TrimSpace(value) == "" || len(value) > 65536) {
					faults = append(faults, fmt.Errorf("judge %q %s must contain 1–65536 bytes", n.Name, key))
				}
			}
			if _, _, err := AgentBudget(n.Attrs); err != nil {
				faults = append(faults, fmt.Errorf("judge %q: %w", n.Name, err))
			}
		}
		if !ok || source.Action != required {
			faults = append(faults, fmt.Errorf("node %q requires explicit lf_source naming a %s node", n.Name, required))
		}
	}
	return errors.Join(faults...)
}
