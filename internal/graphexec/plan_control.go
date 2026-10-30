package graphexec

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// PlanFailureNode names the shipped failure comment node every failing Plan
// path reaches. The graph checker pins it, so factory code may rely on the name.
const PlanFailureNode = "failed"

// PlanCreationBound is the dispatch bound of every node on a Plan cycle. The
// story creation cycle visits each of its nodes once per story of a full set,
// plus one pass for an owner-requested revision that runs before the cycle.
const PlanCreationBound = planbrief.MaxPlanStorySetEntries + 1

// PlanNodeLimitMax caps any declared Plan node bound, so a typo cannot turn a
// bounded cycle into an effectively unbounded one.
const PlanNodeLimitMax = 4 * PlanCreationBound

// Plan stage-decision and cycle conditions. They compare against the factory's
// fresh selection, never against anything an agent returned.
const (
	// PlanSetCondition selects a set proposal or parent discussion.
	PlanSetCondition = "set"
	// PlanCreateCondition is true while a promoted story has no draft yet.
	PlanCreateCondition = "create"
)

// planReviewReady is a response the runtime reports for an owner gate. It can
// never be a declared condition: review-ready is not approval.
const planReviewReady = "review_ready"

// PlanControlPolicy includes factory-only scheduling/promotion responses. The
// dedicated Plan node decoder admits neither stage selectors nor approved.
func PlanControlPolicy() ControlPolicy {
	return ControlPolicy{Mode: agent.ModePlan, Responses: []string{PlanSetCondition, "story", "technical", "acceptance", PlanCreateCondition, "ask", "answer", "discussion", planReviewReady, "approved"}, Defaults: []string{PlanSetCondition, "story", "technical", "acceptance"}, Ask: "ask", Approved: "approved"}
}

// planFailureOutcomes are the outcomes that mean a step did not finish. Whatever
// node declares one, the owner must hear about it, so each reaches the failure node.
var planFailureOutcomes = []string{"failed", "conflict", "uncertain"}

// ValidatePlanControlGraph checks the structure a Plan graph definition declares.
// It pins no node set and no edge list: which stages exist, how many nodes each
// has and how they connect are the definition's to declare. What it enforces are
// the properties every Plan graph must keep, whatever it declares:
//
//   - the entry is a stage decision, and every node is bounded;
//   - control nodes compare only the factory's closed responses, and never
//     review-ready, which is not approval;
//   - agents have a role, route success to a question decision reading that same
//     agent, and route failure to the failure comment;
//   - a story effect reads an agent, and only an owner gate follows it, or the
//     graph's own guarded approval (lf_plan_approve) with an owner stop as fallback;
//   - a failing outcome of any node reaches the one failure comment, which ends
//     at a terminal;
//   - a question wait ends at a terminal, a review wait at a terminal or a
//     bounded loop, and a loop that is exhausted leaves at a terminal;
//   - only owner gates reach a promotion terminal;
//   - every cycle passes through a loop and stays within the creation bound.
//
// Effect outcomes differ from Research document generation; no Research policy
// or evidence gate is changed to accommodate Plan.
func ValidatePlanControlGraph(p *Profile) error {
	var syncErr error
	p, syncErr = withoutExplicitSync(p)
	if syncErr != nil {
		return syncErr
	}
	if p == nil || p.Mode != agent.ModePlan {
		return errors.New("invalid Plan graph entry")
	}
	c := &planCheck{p: p}
	if start, ok := p.Node(p.Start); !ok || start.Terminal || start.Action != ResearchIfAction {
		c.fail("plan graph must start at a stage decision (lf_if), not %q", p.Start)
	}
	for _, n := range p.Nodes {
		c.node(n)
	}
	c.failureNode()
	c.promotion()
	c.cycles()
	return errors.Join(c.faults...)
}

type planCheck struct {
	p      *Profile
	faults []error
}

func (c *planCheck) fail(format string, args ...any) {
	c.faults = append(c.faults, fmt.Errorf(format, args...))
}

// next resolves the node one declared outcome leads to.
func (c *planCheck) next(n Node, outcome string) (Node, bool) {
	to, ok := Decide(c.p, n.Name, outcome)
	if !ok {
		c.fail("plan node %s has no single edge for outcome %s", n.Name, outcome)
		return Node{}, false
	}
	target, ok := c.p.Node(to)
	if ok {
		target, ok = c.advisoryContinuation(target, map[string]bool{})
	}
	if !ok {
		c.fail("plan node %s outcome %s reaches undeclared %s", n.Name, outcome, to)
	}
	return target, ok
}

func (c *planCheck) agentSource(n Node) (Node, bool) {
	source, ok := c.p.Node(n.Attrs["lf_source"])
	if !ok || (source.Action != "lf_agent" && source.Action != PlanParallelAction) {
		c.fail("plan node %s lf_source %q must name an agent node", n.Name, n.Attrs["lf_source"])
		return Node{}, false
	}
	return source, true
}

func (c *planCheck) node(n Node) {
	if n.Terminal {
		return
	}
	if n.Limit <= 0 || n.Limit > PlanNodeLimitMax {
		c.fail("plan node %s requires a positive bound of at most %d", n.Name, PlanNodeLimitMax)
	}
	for _, outcome := range planFailureOutcomes {
		if n.Action != JudgeAction && slices.Contains(n.Outcomes, outcome) {
			if target, ok := Decide(c.p, n.Name, outcome); !ok || target != PlanFailureNode {
				c.fail("plan node %s outcome %s must reach %s", n.Name, outcome, PlanFailureNode)
			}
		}
	}
	switch n.Action {
	case ResearchLoopAction, ResearchIfAction:
		c.control(n)
	case "lf_agent":
		c.agent(n)
	case PlanParallelAction:
		c.parallel(n)
	case PlanStoryAction:
		c.effect(n)
	case "lf_ask":
		c.ask(n)
	case PlanGateAction:
		c.stop(n)
	case PlanApproveAction:
		c.approve(n)
	case JudgeAction:
		c.agentSource(n)
		c.advisoryContinuation(n, map[string]bool{})
	case "lf_comment":
		switch n.Attrs["lf_kind"] {
		case "advisory":
			c.advisoryContinuation(n, map[string]bool{})
		case "final":
			c.agentSource(n)
			if target, ok := c.next(n, "published"); ok && !target.Terminal {
				c.fail("plan answer %s must end at a terminal", n.Name)
			}
		default:
			if n.Attrs["lf_kind"] != "failure" || n.Name != PlanFailureNode {
				c.fail("plan comment %s must be the %s failure comment", n.Name, PlanFailureNode)
			}
		}
	default:
		c.fail("plan node %s uses action %s the Plan runtime does not implement", n.Name, n.Action)
	}
}

func (c *planCheck) control(n Node) {
	condition, err := DecodeCondition(n.Action, n.Attrs, PlanControlPolicy())
	if err != nil {
		c.fail("plan node %s: %v", n.Name, err)
		return
	}
	if len(n.Outcomes) != 2 || !slices.Contains(n.Outcomes, "true") || !slices.Contains(n.Outcomes, "false") {
		c.fail("plan node %s requires exactly true and false outcomes", n.Name)
		return
	}
	if condition.Field != "response" {
		if n.Attrs["lf_source"] != "" {
			c.fail("plan fact %s may not name an agent source", n.Name)
		}
		return
	}
	switch condition.Condition {
	case planReviewReady:
		c.fail("plan node %s compares review_ready, which is never a declared condition", n.Name)
	case PlanControlPolicy().Approved:
		c.gate(n)
	case "answer":
		c.answer(n)
	case PlanControlPolicy().Ask:
		c.question(n)
	default:
		if _, sourced := n.Attrs["lf_source"]; sourced {
			c.fail("plan node %s reads a stage selection and may not name an agent source", n.Name)
		}
	}
	if n.Action != ResearchLoopAction {
		return
	}
	if target, ok := c.next(n, "true"); ok && target.Action != "lf_agent" {
		factEntry := false
		if condition.Condition == PlanCreateCondition && target.Action == ResearchIfAction && target.Attrs["lf_field"] != "response" {
			_, err := DecodeCondition(target.Action, target.Attrs, PlanControlPolicy())
			factEntry = err == nil
		}
		if !factEntry {
			c.fail("plan loop %s must continue at an agent or creation fact comparison, not %s", n.Name, target.Name)
		}
	}
	if target, ok := c.next(n, "false"); ok && !target.Terminal {
		c.fail("plan loop %s must leave at a terminal when it ends or is exhausted", n.Name)
	}
}

// gate is an owner gate: promotion comes from the owner's approval, never from
// an agent, so it names no source, and an unapproved result waits for review.
func (c *planCheck) gate(n Node) {
	if n.Action != ResearchIfAction {
		c.fail("plan owner gate %s must be an lf_if", n.Name)
	}
	if _, sourced := n.Attrs["lf_source"]; sourced {
		c.fail("plan owner gate %s may not read agent output", n.Name)
	}
	if target, ok := c.next(n, "true"); ok && !target.Terminal {
		c.fail("plan owner gate %s must promote at a terminal", n.Name)
	}
	if target, ok := c.next(n, "false"); ok && (target.Action != "lf_ask" || target.Attrs["lf_kind"] != "review") {
		c.fail("plan owner gate %s must wait for review when unapproved", n.Name)
	}
}

// question routes a finished agent: a question stops at an owner wait, an answer
// continues to the story effect. Both read the agent the decision names.
func (c *planCheck) question(n Node) {
	source, ok := c.agentSource(n)
	if n.Action != ResearchIfAction {
		c.fail("plan question decision %s must be an lf_if", n.Name)
	}
	if !ok {
		return
	}
	if target, found := c.next(n, "true"); found && (target.Action != "lf_ask" || target.Attrs["lf_kind"] != "question" || target.Attrs["lf_source"] != source.Name) {
		c.fail("plan question decision %s must publish %s's question", n.Name, source.Name)
	}
	if target, found := c.next(n, "false"); found && ((target.Action != PlanStoryAction && (target.Action != ResearchIfAction || target.Attrs["lf_condition"] != "answer")) || target.Attrs["lf_source"] != source.Name) {
		c.fail("plan question decision %s must continue at %s's answer decision", n.Name, source.Name)
	}
}

func (c *planCheck) agent(n Node) {
	if _, _, err := ResearchReferences(n.Attrs); err != nil {
		c.fail("plan agent %s has no valid role: %v", n.Name, err)
	}
	if target, ok := c.next(n, "success"); ok && (target.Action != ResearchIfAction || target.Attrs["lf_condition"] != PlanControlPolicy().Ask || target.Attrs["lf_source"] != n.Name) {
		c.fail("plan agent %s must route success to its own question decision", n.Name)
	}
}

func (c *planCheck) effect(n Node) {
	source, ok := c.agentSource(n)
	if !ok {
		return
	}
	if source.Action == PlanParallelAction && !slices.Contains([]string{"story", "technical", "acceptance"}, n.Attrs["lf_stage"]) {
		c.fail("parallel effect %s requires a story stage", n.Name)
	}
	gate, ok := c.next(n, "applied")
	if !ok {
		return
	}
	if source.Action == PlanParallelAction && gate.Attrs["lf_stage"] != n.Attrs["lf_stage"] {
		c.fail("parallel effect %s must retain its stage at approval", n.Name)
	}
	if gate.Action == PlanGateAction {
		if gate.Attrs["lf_source"] != source.Name {
			c.fail("plan effect %s must be reviewed as %s's result", n.Name, source.Name)
		}
		if source.Name == "set_agent" || source.Attrs["lf_agent_markdown"] == "plan-set" {
			c.fail("plan gate %s may not follow the set stage, which keeps its owner gate and review", gate.Name)
		}
		return
	}
	if gate.Action == PlanApproveAction {
		if gate.Attrs["lf_source"] != source.Name {
			c.fail("plan effect %s must be approved as %s's result", n.Name, source.Name)
		}
		return
	}
	if gate.Action != ResearchIfAction || gate.Attrs["lf_condition"] != PlanControlPolicy().Approved {
		c.fail("plan effect %s must be followed by an owner gate", n.Name)
		return
	}
	if review, found := c.next(gate, "false"); found && review.Attrs["lf_source"] != source.Name {
		c.fail("plan effect %s must be reviewed as %s's result", n.Name, source.Name)
	}
}

// ask is an owner wait. A question waits on the owner and nothing else, so it
// ends the graph; a review may hand on to a bounded loop that selects the next
// piece of work, because every review keeps its own durable owner wait.
func (c *planCheck) ask(n Node) {
	if _, ok := c.agentSource(n); !ok {
		return
	}
	target, ok := c.next(n, "published")
	if !ok {
		return
	}
	switch n.Attrs["lf_kind"] {
	case "question":
		if !target.Terminal {
			c.fail("plan question %s must end at a durable owner wait", n.Name)
		}
	case "review":
		if !target.Terminal && target.Action != ResearchLoopAction {
			c.fail("plan review %s must end at a terminal or a bounded loop", n.Name)
		}
	default:
		c.fail("plan wait %s has kind %q", n.Name, n.Attrs["lf_kind"])
	}
}

// stop is the generic owner stop: it publishes its agent's review with the
// closed approval command and waits. It names a source, never promotes, and
// ends at a terminal or a bounded loop like a review.
func (c *planCheck) stop(n Node) {
	if _, ok := c.agentSource(n); !ok {
		return
	}
	if n.Attrs["lf_command"] != PlanGateCommand {
		c.fail("plan gate %s lf_command must be %s", n.Name, PlanGateCommand)
	}
	if n.Attrs["lf_args"] != PlanGateArgs {
		c.fail("plan gate %s lf_args must be %s, in that order", n.Name, PlanGateArgs)
	}
	if target, ok := c.next(n, "published"); ok && !target.Terminal && target.Action != ResearchLoopAction {
		c.fail("plan gate %s must end at a terminal or a bounded loop", n.Name)
	}
}

// approve is the graph's own approval of an ungated stage. It may never promote:
// any doubt reaches an owner stop of the same agent, a replaced or rewound stage
// ends quietly at a terminal, and a block or failure reaches the failure comment.
// The set stage keeps its owner gate and may not use it.
func (c *planCheck) approve(n Node) {
	source, ok := c.agentSource(n)
	if !ok {
		return
	}
	if source.Action == PlanParallelAction {
		stage := n.Attrs["lf_stage"]
		if !slices.Contains([]string{"story", "technical", "acceptance"}, stage) {
			c.fail("parallel approval %s requires a story stage", n.Name)
		}
		if target, found := c.next(n, "approved"); found {
			next := map[string]string{"story": "technical", "technical": "acceptance"}[stage]
			if next != "" && (target.Action != PlanStoryAction || target.Attrs["lf_source"] != source.Name || target.Attrs["lf_stage"] != next) {
				c.fail("parallel approval %s must apply the next ordered stage", n.Name)
			}
			if next == "" && !target.Terminal && target.Action != ResearchLoopAction {
				c.fail("parallel acceptance approval %s must finish at a scheduling boundary", n.Name)
			}
		}
		if target, found := c.next(n, "review"); found && target.Attrs["lf_stage"] != stage {
			c.fail("parallel approval %s must retain its stage at owner review", n.Name)
		}
	}
	if source.Name == "set_agent" || source.Attrs["lf_agent_markdown"] == "plan-set" {
		c.fail("plan approval %s may not follow the set stage, which keeps its owner gate and review", n.Name)
	}
	if target, found := c.next(n, "approved"); found && !target.Terminal && target.Action != ResearchLoopAction && (source.Action != PlanParallelAction || target.Action != PlanStoryAction) {
		c.fail("plan approval %s must end approved at a terminal or a bounded loop", n.Name)
	}
	if target, found := c.next(n, "review"); found {
		stop := target.Action == PlanGateAction || (target.Action == "lf_ask" && target.Attrs["lf_kind"] == "review")
		if !stop || target.Attrs["lf_source"] != source.Name {
			c.fail("plan approval %s must hand review to %s's owner stop", n.Name, source.Name)
		}
	}
	if target, found := c.next(n, "superseded"); found && !target.Terminal {
		c.fail("plan approval %s must end superseded at a terminal", n.Name)
	}
	if to, found := Decide(c.p, n.Name, "blocked"); !found || to != PlanFailureNode {
		c.fail("plan approval %s outcome blocked must reach %s", n.Name, PlanFailureNode)
	}
}

func (c *planCheck) failureNode() {
	n, ok := c.p.Node(PlanFailureNode)
	if !ok || n.Action != "lf_comment" || n.Attrs["lf_kind"] != "failure" {
		c.fail("plan graph requires the %s failure comment", PlanFailureNode)
		return
	}
	if target, found := c.next(n, "published"); found && !target.Terminal {
		c.fail("plan failure comment must end at a terminal")
	}
}

// promotion keeps promotion an owner act: a terminal an owner gate promotes at
// is reachable from nothing else, so no failure, exhausted loop, question or
// review can end a graph as if the owner had approved.
func (c *planCheck) promotion() {
	promoted := map[string]bool{}
	for _, n := range c.p.Nodes {
		if n.Action == ResearchIfAction && n.Attrs["lf_condition"] == PlanControlPolicy().Approved {
			if to, ok := Decide(c.p, n.Name, "true"); ok {
				promoted[to] = true
			}
		}
	}
	for _, e := range c.p.Edges {
		from, ok := c.p.Node(e.From)
		if !ok || !promoted[e.To] {
			continue
		}
		if from.Action != ResearchIfAction || from.Attrs["lf_condition"] != PlanControlPolicy().Approved || e.On != "true" {
			c.fail("plan edge %s/%s may not reach promotion terminal %s", e.From, e.On, e.To)
		}
	}
}

// cycles requires every cycle to pass through a loop's continue edge, so each
// repetition is a bounded loop decision, and to carry the creation bound.
func (c *planCheck) cycles() {
	forward := map[string][]string{}
	acyclic := map[string][]string{}
	for _, e := range c.p.Edges {
		forward[e.From] = append(forward[e.From], e.To)
		if from, ok := c.p.Node(e.From); ok && from.Action == ResearchLoopAction && e.On == "true" {
			continue
		}
		acyclic[e.From] = append(acyclic[e.From], e.To)
	}
	for _, n := range c.p.Nodes {
		if walk(acyclic[n.Name], acyclic)[n.Name] {
			c.fail("plan node %s is on a cycle that bypasses a bounded loop", n.Name)
		}
		if !n.Terminal && n.Limit < PlanCreationBound && walk(forward[n.Name], forward)[n.Name] {
			c.fail("plan node %s is on a cycle and requires a bound of at least %d", n.Name, PlanCreationBound)
		}
	}
}

// answer separates a completed reply from a draft; neither branch grants approval.
func (c *planCheck) answer(n Node) {
	source, ok := c.agentSource(n)
	if !ok {
		return
	}
	if n.Action != ResearchIfAction {
		c.fail("plan answer decision %s must be an lf_if", n.Name)
	}
	if target, found := c.next(n, "true"); found && (target.Action != "lf_comment" || target.Attrs["lf_kind"] != "final" || target.Attrs["lf_source"] != source.Name) {
		c.fail("plan answer decision %s must publish its agent's answer", n.Name)
	}
	if target, found := c.next(n, "false"); found && (target.Action != PlanStoryAction || target.Attrs["lf_source"] != source.Name) {
		c.fail("plan answer decision %s must continue at its agent's story effect", n.Name)
	}
}

// advisoryContinuation follows report-only steps without treating advice as an
// answer, effect, measurement or approval. Every execution branch must resume
// at the same existing control boundary.
func (c *planCheck) advisoryContinuation(n Node, seen map[string]bool) (Node, bool) {
	if n.Action != JudgeAction && (n.Action != "lf_comment" || n.Attrs["lf_kind"] != "advisory") {
		return n, true
	}
	if seen[n.Name] {
		c.fail("plan advisory path cycles at %s", n.Name)
		return Node{}, false
	}
	seen[n.Name] = true
	defer delete(seen, n.Name)
	var continuation Node
	for _, outcome := range n.Outcomes {
		to, ok := Decide(c.p, n.Name, outcome)
		if !ok {
			c.fail("plan advisory %s lacks %s edge", n.Name, outcome)
			return Node{}, false
		}
		next, ok := c.p.Node(to)
		if !ok {
			c.fail("plan advisory %s reaches missing %s", n.Name, to)
			return Node{}, false
		}
		next, ok = c.advisoryContinuation(next, seen)
		if !ok {
			return Node{}, false
		}
		if continuation.Name != "" && continuation.Name != next.Name {
			c.fail("plan advisory %s outcomes must preserve the same continuation", n.Name)
			return Node{}, false
		}
		continuation = next
	}
	return continuation, continuation.Name != ""
}

func (c *planCheck) parallel(n Node) {
	if _, err := DecodePlanParallel(c.p, n); err != nil {
		c.fail("plan parallel %s: %v", n.Name, err)
		return
	}
	for _, outcome := range []string{"ready", "ask", "fallback", "stopped", "superseded", "failed"} {
		if !slices.Contains(n.Outcomes, outcome) {
			c.fail("plan parallel %s requires %s outcome", n.Name, outcome)
		}
	}
	if target, ok := c.next(n, "ready"); ok && (target.Action != PlanStoryAction || target.Attrs["lf_source"] != n.Name || target.Attrs["lf_stage"] != "story") {
		c.fail("plan parallel %s must begin ordered story application", n.Name)
	}
	if target, ok := c.next(n, "ask"); ok && (target.Action != "lf_ask" || target.Attrs["lf_kind"] != "question" || target.Attrs["lf_source"] != n.Name) {
		c.fail("plan parallel %s must retain its question at an owner wait", n.Name)
	}
	if target, ok := c.next(n, "stopped"); ok && !target.Terminal && (target.Name != PlanFailureNode || target.Action != "lf_comment" || target.Attrs["lf_kind"] != "failure") {
		c.fail("plan parallel %s stopped must reach a terminal or failure comment", n.Name)
	}
	if target, ok := c.next(n, "superseded"); ok && !target.Terminal {
		c.fail("plan parallel %s superseded must end at a terminal", n.Name)
	}
	if target, ok := c.next(n, "fallback"); ok && (target.Action != ResearchIfAction || target.Attrs["lf_field"] != "response") {
		c.fail("plan parallel %s fallback must return to normal stage selection", n.Name)
	}
}
