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

// ValidateSpecs lists every action of the Validate graph, the agent aside.
func ValidateSpecs() []ActionSpec {
	return []ActionSpec{ValidateSuiteSpec(), ValidateRepairBudgetSpec(), ValidateRepairCheckSpec(), ValidateReviewSpec(), ValidateOutcomeSpec()}
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
		}
	}
	for _, action := range []string{"lf_agent", ValidateSuiteAction, ValidateRepairBudgetAction, ValidateRepairCheckAction, ValidateReviewAction, ValidateOutcomeAction} {
		if counts[action] != 1 {
			c.fail("validate graph requires exactly one %s node, has %d", action, counts[action])
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
		c.edge(n, end, func(t Node) bool { return t.Terminal && t.Name == end }, "must end at the terminal of the same name")
	}
}
