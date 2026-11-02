package graphexec

import (
	"errors"
	"fmt"
	"slices"
	"strings"

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

// ValidateNodeLimitMax caps any declared Validate node bound.
const ValidateNodeLimitMax = 64

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

// ValidateFlowGraph checks the structure a Validate graph declares. It pins few
// node names; it enforces the properties every Validate graph keeps:
//
//   - it starts at the suite step and every node has a positive, bounded lf_limit;
//   - exactly one repair agent exists, with write access and a declared time
//     budget, and at most [ValidateMaxRepairRounds] rounds;
//   - a repair is measured before anything else sees it, and only a kept change
//     reaches the suite again;
//   - every path to an end passes through the review step, and the outcome step
//     reaches one terminal named for each of the three ends.
func ValidateFlowGraph(p *Profile) error {
	if p == nil || p.Mode != agent.ModeValidate {
		return errors.New("invalid Validate graph entry")
	}
	c := &flowCheck{p: p}
	if start, ok := p.Node(p.Start); !ok || start.Action != ValidateSuiteAction {
		c.fail("validate graph must start at the suite step (%s), not %q", ValidateSuiteAction, p.Start)
	}
	known := map[string]bool{"lf_agent": true}
	for _, spec := range ValidateSpecs() {
		known[spec.Name] = true
	}
	counts := map[string]int{}
	for _, n := range p.Nodes {
		if n.Terminal {
			continue
		}
		counts[n.Action]++
		if n.Limit <= 0 || n.Limit > ValidateNodeLimitMax {
			c.fail("validate node %s requires a positive lf_limit of at most %d", n.Name, ValidateNodeLimitMax)
		}
		if !known[n.Action] {
			c.fail("validate node %s uses action %s the Validate runtime does not implement", n.Name, n.Action)
		}
		switch n.Action {
		case "lf_agent":
			c.repairAgent(n)
		case ValidateSuiteAction:
			c.suite(n)
		case ValidateRepairBudgetAction:
			c.budget(n)
		case ValidateRepairCheckAction:
			c.check(n)
		case ValidateReviewAction:
			c.review(n)
		case ValidateOutcomeAction:
			c.outcome(n)
		case ValidateResolveAction:
			c.resolve(n)
		case ValidateReportAction:
			c.edge(n, "done", action(ValidateAskAction), "must reach the owner wait")
		case ValidateAskAction:
			c.ask(n)
		case ValidateIssueAction:
			c.edge(n, "done", action(ValidateReportAction), "must report")
		case ValidatePushAction, ValidateMergeAction:
			// A /validate-ship carries on from resolve; any other command waits.
			c.edge(n, "done", func(t Node) bool { return t.Action == ValidateReportAction || t.Action == ValidateResolveAction }, "must report or return to the owner resolution")
		case ValidateCloseAction:
			c.closeStep(n)
		case ValidateRepairApplyAction:
			c.edge(n, "applied", action(ValidateSuiteAction), "must run the suite on the adopted commit")
			c.edge(n, "blocked", action(ValidateReportAction), "must report the blocked adoption")
			for _, e := range p.Edges {
				if e.To != n.Name {
					continue
				}
				source, ok := p.Node(e.From)
				if !ok || source.Action != ValidateResolveAction || e.On != "apply_repair" {
					c.fail("validate adoption node %s may only be reached by owner resolution apply_repair", n.Name)
				}
			}
		case ValidateImplementAction:
			c.named(n, "lf_suite", ValidateSuiteAction)
			c.edge(n, "started", func(t Node) bool { return t.Terminal }, "must end at a terminal")
		case ValidateReporterAction:
			c.reporter(n)
		case ValidateFactoryBugAction:
			c.edge(n, "done", action(ValidateReportAction), "must report")
			for _, e := range p.Edges {
				if e.To != n.Name {
					continue
				}
				source, ok := p.Node(e.From)
				if !ok || source.Action != ValidateResolveAction || e.On != "factory_bug" {
					c.fail("validate factory Bug node %s may only be reached by owner resolution factory_bug", n.Name)
				}
			}
		}
	}
	for _, action := range []string{"lf_agent", ValidateSuiteAction, ValidateRepairBudgetAction, ValidateRepairCheckAction, ValidateReviewAction, ValidateOutcomeAction,
		ValidateResolveAction, ValidateReportAction, ValidateAskAction, ValidateIssueAction, ValidatePushAction, ValidateMergeAction, ValidateCloseAction} {
		if counts[action] != 1 {
			c.fail("validate graph requires exactly one %s node, has %d", action, counts[action])
		}
	}
	if counts[ValidateRepairApplyAction] > 1 {
		c.fail("validate graph allows at most one %s node, has %d", ValidateRepairApplyAction, counts[ValidateRepairApplyAction])
	}
	for _, single := range []string{ValidateImplementAction, ValidateReporterAction, ValidateFactoryBugAction} {
		if counts[single] > 1 {
			c.fail("validate graph allows at most one %s node, has %d", single, counts[single])
		}
	}
	return errors.Join(c.faults...)
}

type flowCheck struct {
	p      *Profile
	faults []error
}

func (c *flowCheck) fail(format string, args ...any) {
	c.faults = append(c.faults, fmt.Errorf(format, args...))
}

func (c *flowCheck) edge(n Node, outcome string, accept func(Node) bool, rule string) {
	to, ok := Decide(c.p, n.Name, outcome)
	if !ok {
		c.fail("validate node %s has no single edge for outcome %s", n.Name, outcome)
		return
	}
	target, ok := c.p.Node(to)
	if !ok {
		c.fail("validate node %s outcome %s reaches undeclared %s", n.Name, outcome, to)
		return
	}
	if !accept(target) {
		c.fail("validate node %s outcome %s %s, not %s", n.Name, outcome, rule, target.Name)
	}
}

func action(a string) func(Node) bool { return func(t Node) bool { return t.Action == a } }

func (c *flowCheck) named(n Node, attr, want string) {
	if source, ok := c.p.Node(n.Attrs[attr]); !ok || source.Action != want {
		c.fail("validate node %s %s %q must name the %s node", n.Name, attr, n.Attrs[attr], want)
	}
}

func (c *flowCheck) repairAgent(n Node) {
	if _, _, err := ResearchReferences(n.Attrs); err != nil {
		c.fail("validate agent %s has no valid role: %v", n.Name, err)
	}
	if n.Attrs[AccessAttribute] != "write" {
		c.fail("validate repair agent %s must declare lf_access=write", n.Name)
	}
	if _, ok, err := AgentBudget(n.Attrs); !ok || err != nil {
		c.fail("validate repair agent %s must declare a valid lf_budget", n.Name)
	}
	if n.Limit > ValidateMaxRepairRounds {
		c.fail("validate repair agent %s lf_limit %d exceeds %d repair rounds", n.Name, n.Limit, ValidateMaxRepairRounds)
	}
	c.edge(n, "success", action(ValidateRepairCheckAction), "must reach the repair check")
	c.edge(n, "failed", action(ValidateRepairCheckAction), "must reach the repair check")
}

func (c *flowCheck) suite(n Node) {
	if slices.Contains(n.Outcomes, "covered") {
		if n.Attrs["lf_coverage"] != "v1" {
			c.fail("covered suite requires versioned coverage capability")
		}
		c.edge(n, "covered", action(ValidateReviewAction), "must go to review")
	} else if n.Attrs["lf_coverage"] != "" {
		c.fail("coverage capability requires covered outcome")
	}
	c.edge(n, "passed", action(ValidateReviewAction), "must go to review")
	c.edge(n, "failed", action(ValidateRepairBudgetAction), "must reach the repair budget")
	c.edge(n, "blocked", action(ValidateReviewAction), "must go to review")
	c.edge(n, "retry", func(t Node) bool { return t.Name == n.Name }, "must run the suite again")
}

func (c *flowCheck) budget(n Node) {
	rounds := 0
	if _, err := fmt.Sscanf(n.Attrs["lf_rounds"], "%d", &rounds); err != nil || rounds < 1 || rounds > ValidateMaxRepairRounds {
		c.fail("validate node %s lf_rounds %q must be 1 to %d", n.Name, n.Attrs["lf_rounds"], ValidateMaxRepairRounds)
	}
	c.named(n, "lf_suite", ValidateSuiteAction)
	c.edge(n, "run", action("lf_agent"), "must run the repair agent")
	c.edge(n, "spent", action(ValidateReviewAction), "must go to review")
}

func (c *flowCheck) check(n Node) {
	c.named(n, "lf_source", "lf_agent")
	c.named(n, "lf_suite", ValidateSuiteAction)
	c.edge(n, "kept", action(ValidateSuiteAction), "must run the suite on the new commit")
	c.edge(n, "unfinished", action(ValidateRepairBudgetAction), "must reach the repair budget")
	for _, outcome := range []string{"too_big", "handback", "blocked"} {
		c.edge(n, outcome, action(ValidateReviewAction), "must go to review")
	}
}

func (c *flowCheck) review(n Node) {
	c.named(n, "lf_suite", ValidateSuiteAction)
	if roles := strings.Split(n.Attrs["lf_roles"], ","); !slices.Equal(roles, []string{"validate-acceptance", "validate-code", "validate-architecture"}) {
		c.fail("validate node %s lf_roles must name the acceptance, code and architecture reviewers, in that order", n.Name)
	}
	c.edge(n, "done", action(ValidateOutcomeAction), "must reach the outcome step")
	c.edge(n, "retry", func(t Node) bool { return t.Name == n.Name }, "must run the review again")
}

func (c *flowCheck) outcome(n Node) {
	c.named(n, "lf_suite", ValidateSuiteAction)
	for _, end := range ValidateEnds {
		c.edge(n, end, func(t Node) bool { return t.Action == ValidateResolveAction || t.Action == ValidateReporterAction },
			"must reach the reporter or the owner resolution")
	}
}

// reporter checks that the summary step is the one read-only reporter role,
// that it always goes on to the owner (its fallback included) and that a
// follow-up answer reports.
func (c *flowCheck) reporter(n Node) {
	c.named(n, "lf_suite", ValidateSuiteAction)
	if n.Attrs["lf_roles"] != "validate-reporter" {
		c.fail("validate node %s lf_roles must name the validate-reporter role only", n.Name)
	}
	if access := n.Attrs[AccessAttribute]; access != "" && access != "read" {
		c.fail("validate reporter %s must stay read-only", n.Name)
	}
	c.edge(n, "done", action(ValidateResolveAction), "must reach the owner resolution")
	c.edge(n, "fallback", action(ValidateResolveAction), "must reach the owner resolution")
	if slices.Contains(n.Outcomes, "answered") {
		c.edge(n, "answered", action(ValidateReportAction), "must report the answer")
	}
	for _, e := range c.p.Edges {
		if e.To != n.Name {
			continue
		}
		source, ok := c.p.Node(e.From)
		if !ok || (source.Action != ValidateOutcomeAction && (source.Action != ValidateResolveAction || e.On != "discuss")) {
			c.fail("validate reporter %s may only be reached from the outcome or by owner resolution discuss", n.Name)
		}
	}
}

// resolve checks that each remote effect is reached only through its own step
// from the resolution, and that a waiting or stale reply reports again. A
// declared repair outcome must start a repair round and a declared implement
// outcome must reach the implement step; a graph released without them still
// validates.
func (c *flowCheck) resolve(n Node) {
	c.named(n, "lf_suite", ValidateSuiteAction)
	c.edge(n, "wait", action(ValidateReportAction), "must report")
	c.edge(n, "stale", action(ValidateReportAction), "must report")
	c.edge(n, "create_issue", action(ValidateIssueAction), "must create the issue")
	c.edge(n, "push", action(ValidatePushAction), "must push")
	c.edge(n, "merge", action(ValidateMergeAction), "must merge")
	c.edge(n, "close", action(ValidateCloseAction), "must close")
	if slices.Contains(n.Outcomes, "repair") {
		c.edge(n, "repair", action(ValidateRepairBudgetAction), "must start a repair round")
	}
	if slices.Contains(n.Outcomes, "apply_repair") {
		c.edge(n, "apply_repair", action(ValidateRepairApplyAction), "must reach exact repair adoption")
	}
	if slices.Contains(n.Outcomes, "implement") {
		c.edge(n, "implement", action(ValidateImplementAction), "must hand the findings back to Implement")
	}
	if slices.Contains(n.Outcomes, "discuss") {
		c.edge(n, "discuss", action(ValidateReporterAction), "must wake the reporter")
	}
	if slices.Contains(n.Outcomes, "factory_bug") {
		c.edge(n, "factory_bug", action(ValidateFactoryBugAction), "must file the factory Bug")
	}
}

func (c *flowCheck) ask(n Node) {
	c.named(n, "lf_source", ValidateResolveAction)
	c.edge(n, "published", func(t Node) bool { return t.Terminal }, "must rest at a terminal")
}

// closeStep checks that the run succeeds only through the close step.
func (c *flowCheck) closeStep(n Node) {
	c.edge(n, "closed", func(t Node) bool { return t.Terminal }, "must end at a terminal")
	c.edge(n, "open", action(ValidateReportAction), "must report")
}
