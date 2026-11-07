package graphexec

import (
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
)

// The steps of the Validate graph that follow the suite. Each does one
// measurable thing and returns an outcome; validate.dot, never Go, decides what
// follows it. The suite step itself is [ValidateSuiteAction].
const (
	ValidateRepairBudgetAction = "lf_validate_repair_budget"
	ValidateRepairCheckAction  = "lf_validate_repair_check"
	ValidateReviewAction       = "lf_validate_review"
	ValidateOutcomeAction      = "lf_validate_outcome"
)

// The owner side of the Validate graph. After the outcome the run resolves the
// owner's reply, reports and waits; every remote effect is its own step and is
// reached only from the owner's command.
const (
	ValidateResolveAction = "lf_validate_resolve"
	ValidateReportAction  = "lf_validate_report"
	ValidateAskAction     = "lf_validate_ask"
	ValidateIssueAction   = "lf_validate_issue"
	ValidatePushAction    = "lf_validate_push"
	ValidateMergeAction   = "lf_validate_merge"
	ValidateCloseAction   = "lf_validate_close"
	// ValidateImplementAction reopens the stories with blocking findings and
	// starts the parent's Implement coordinator on the same feature branch.
	ValidateImplementAction = "lf_validate_implement"
	// ValidateRepairApplyAction adopts only an exactly authorized retained tree.
	ValidateRepairApplyAction = "lf_validate_repair_apply"
	// ValidateReporterAction runs the read-only reporter agent that writes the
	// owner summary, and answers owner comments on the waiting report.
	ValidateReporterAction = "lf_validate_reporter"
	// ValidateFactoryBugAction files one owner-confirmed upstream factory Bug.
	ValidateFactoryBugAction = "lf_validate_factory_bug"
)

// ValidateReporterOutcomes are the outcomes of the reporter step: done when
// its summary is stored, fallback when the factory's own report stands in, and
// answered after a follow-up answer to an owner comment.
var ValidateReporterOutcomes = []string{"done", "fallback", "answered"}

// ValidateMaxReporterBudget is the largest time budget the reporter may declare.
const ValidateMaxReporterBudget = 30

// ValidateMaxReporterReruns bounds the reporter's focused diagnostic re-runs.
const ValidateMaxReporterReruns = 3

// ValidateMaxReporterRounds bounds the follow-up answers of one Validate run.
const ValidateMaxReporterRounds = 5

// ValidateResolveOutcomes are the outcomes of resolving the owner's reply.
// The repair and implement outcomes are optional in a graph: profiles released
// before them keep validating without them.
var ValidateResolveOutcomes = []string{"wait", "create_issue", "push", "merge", "close", "stale", "repair", "implement", "apply_repair", "discuss", "factory_bug"}

// ValidateMaxRepairRounds is the most repair rounds any Validate graph may
// declare. The graph declares the bound it uses, at or below this.
const ValidateMaxRepairRounds = 2

// ValidateForbidKinds are the kinds of change a repair node may forbid. The
// first four are the structural kinds; tests means a deleted or skipped test and criteria
// means a changed acceptance criterion.
var ValidateForbidKinds = []string{"package", "dependency", "schema", "api", "tests", "criteria"}

// ValidateEnds are the three ends of the Validate graph, which are also the
// outcomes of the outcome step and the names of its terminals.
var ValidateEnds = []string{"assessed", "handed_back", "blocked"}

// ValidateRepairCheckOutcomes are the outcomes of measuring one repair round.
var ValidateRepairCheckOutcomes = []string{"kept", "too_big", "handback", "unfinished", "blocked"}

// ValidateControlPolicy is the closed response vocabulary of a Validate agent:
// the repair responses and a reviewer's done. It never approves.
func ValidateControlPolicy() ControlPolicy {
	return ControlPolicy{Mode: agent.ModeValidate, Responses: []string{"repaired", "handback", "blocked", "done"}}
}

// ValidateRepairBudgetSpec declares the step that starts a repair round: run
// while the declared bound allows another round, spent when it does not. It also
// settles a round a restart left unfinished before anything else.
func ValidateRepairBudgetSpec() ActionSpec {
	return ActionSpec{Name: ValidateRepairBudgetAction, Mode: agent.ModeValidate, Outcomes: []string{"run", "spent"}, Attributes: map[string]AttributeSpec{
		"lf_rounds": {Required: true, Values: []string{"1", "2"}},
	}}
}

// ValidateRepairCheckSpec declares the step that measures the real change of a
// repair round against the commit before it and keeps or rejects it.
func ValidateRepairCheckSpec() ActionSpec {
	return ActionSpec{Name: ValidateRepairCheckAction, Mode: agent.ModeValidate, Outcomes: slices.Clone(ValidateRepairCheckOutcomes), Attributes: map[string]AttributeSpec{
		"lf_max_files": {Required: true, Values: []string{"1", "2", "3", "4", "5"}},
		"lf_forbid":    {Required: true, List: true, Values: slices.Clone(ValidateForbidKinds)},
	}}
}

// ValidateReviewSpec declares the step that runs the three read-only reviewers
// on the tested commit: done when every reviewer has a recorded answer, retry
// when one still waits for a run slot.
func ValidateReviewSpec() ActionSpec {
	return ActionSpec{Name: ValidateReviewAction, Mode: agent.ModeValidate, Outcomes: []string{"done", "retry"}}
}

// ValidateOutcomeSpec declares the step that joins the reviewers' findings and
// records the end of the run.
func ValidateOutcomeSpec() ActionSpec {
	return ActionSpec{Name: ValidateOutcomeAction, Mode: agent.ModeValidate, Outcomes: slices.Clone(ValidateEnds)}
}

// ValidateResolveSpec declares the step that applies the owner's reply. It has
// no remote effect: it records a verdict or a choice, or the owner's
// confirmation, and says which step follows.
func ValidateResolveSpec() ActionSpec {
	return ActionSpec{Name: ValidateResolveAction, Mode: agent.ModeValidate, Outcomes: slices.Clone(ValidateResolveOutcomes), Attributes: map[string]AttributeSpec{}}
}

// ValidateReportSpec declares the step that keeps the one report comment current.
func ValidateReportSpec() ActionSpec {
	return ActionSpec{Name: ValidateReportAction, Mode: agent.ModeValidate, Outcomes: []string{"done"}}
}

// ValidateAskSpec declares the step that publishes the owner wait. Its lf_source
// names the node an owner reply resumes the run at.
func ValidateAskSpec() ActionSpec {
	return ActionSpec{Name: ValidateAskAction, Mode: agent.ModeValidate, Outcomes: []string{"published"}}
}

// ValidateIssueSpec declares the step that creates or recovers a follow-up issue.
func ValidateIssueSpec() ActionSpec {
	return ActionSpec{Name: ValidateIssueAction, Mode: agent.ModeValidate, Outcomes: []string{"done"}}
}

// ValidatePushSpec declares the step that pushes the tested commit and finds or
// creates the pull request.
func ValidatePushSpec() ActionSpec {
	return ActionSpec{Name: ValidatePushAction, Mode: agent.ModeValidate, Outcomes: []string{"done"}}
}

// ValidateMergeSpec declares the step that merges the pull request at its head.
func ValidateMergeSpec() ActionSpec {
	return ActionSpec{Name: ValidateMergeAction, Mode: agent.ModeValidate, Outcomes: []string{"done"}}
}

// ValidateCloseSpec declares the step that closes the accepted issues: closed
// when every one is closed, open when something is still open.
func ValidateCloseSpec() ActionSpec {
	return ActionSpec{Name: ValidateCloseAction, Mode: agent.ModeValidate, Outcomes: []string{"closed", "open"}}
}

// ValidateImplementSpec declares the step that hands the blocking findings back
// to Implement: started once the stories are reopened and the coordinator is
// scheduled.
func ValidateImplementSpec() ActionSpec {
	return ActionSpec{Name: ValidateImplementAction, Mode: agent.ModeValidate, Outcomes: []string{"started"}}
}

// ValidateRepairApplySpec declares the separately journaled owner adoption step.
// A verified tree returns to the suite; a refusal returns to the report.
func ValidateRepairApplySpec() ActionSpec {
	return ActionSpec{Name: ValidateRepairApplyAction, Mode: agent.ModeValidate, Outcomes: []string{"applied", "blocked"}}
}

// ValidateReporterSpec declares the reporter step. lf_budget bounds one run in
// minutes, lf_reruns the focused re-runs the reporter may make and lf_rounds
// the follow-up answers per run.
func ValidateReporterSpec() ActionSpec {
	budgets := make([]string, 0, ValidateMaxReporterBudget)
	for minutes := 1; minutes <= ValidateMaxReporterBudget; minutes++ {
		budgets = append(budgets, fmt.Sprint(minutes))
	}
	return ActionSpec{Name: ValidateReporterAction, Mode: agent.ModeValidate, Outcomes: slices.Clone(ValidateReporterOutcomes), Attributes: map[string]AttributeSpec{
		BudgetAttribute: {Required: true, Values: budgets},
		"lf_reruns":     {Required: true, Values: []string{"0", "1", "2", "3"}},
		"lf_rounds":     {Required: true, Values: []string{"0", "1", "2", "3", "4", "5"}},
	}}
}

// ValidateFactoryBugSpec declares the step that files one owner-confirmed
// upstream factory Bug.
func ValidateFactoryBugSpec() ActionSpec {
	return ActionSpec{Name: ValidateFactoryBugAction, Mode: agent.ModeValidate, Outcomes: []string{"done"}}
}

// ValidateSpecs lists every action of the Validate graph, the agent aside.
func ValidateSpecs() []ActionSpec {
	return []ActionSpec{ValidateSuiteSpec(), ValidateRepairBudgetSpec(), ValidateRepairCheckSpec(), ValidateReviewSpec(), ValidateOutcomeSpec(),
		ValidateResolveSpec(), ValidateReportSpec(), ValidateAskSpec(), ValidateIssueSpec(), ValidatePushSpec(), ValidateMergeSpec(), ValidateCloseSpec(), ValidateImplementSpec(), ValidateRepairApplySpec(),
		ValidateReporterSpec(), ValidateFactoryBugSpec()}
}
