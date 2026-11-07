package graphexec

import "github.com/cloudvane/litefactory/internal/agent"

// JudgeAction challenges a pinned saved result without decision authority.
const JudgeAction = "lf_judge"

// JudgeFormatAttribute names how an advisory comment renders its judge report.
// Only an advisory lf_comment may declare it.
const JudgeFormatAttribute = "lf_format"

// JudgeSpec declares the shared advisory execution contract.
func JudgeSpec(mode agent.Mode) ActionSpec {
	return ActionSpec{Name: JudgeAction, Mode: mode, Outcomes: []string{"success", "failed"}, Attributes: map[string]AttributeSpec{
		"lf_agent":      {Values: []string{"claude", "codex", "other"}},
		BudgetAttribute: {Values: budgetValues()},
	}}
}
