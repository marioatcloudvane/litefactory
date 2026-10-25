package graphexec

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
)

// The factory activities of an Implement job. They hold no agent: the factory
// decides which stories start, merges finished ones and writes the hand-off.
const (
	ImplementReadyAction   = "lf_implement_ready"
	ImplementMergeAction   = "lf_implement_merge"
	ImplementHandoffAction = "lf_implement_handoff"
)

// ImplementReadySpec declares the outcomes of picking stories to start:
// started when at least one story began, waiting when none can begin yet but
// work is still in progress, drained when no story can ever begin.
func ImplementReadySpec() ActionSpec {
	return ActionSpec{Name: ImplementReadyAction, Mode: agent.ModeImplement, Outcomes: []string{"started", "waiting", "drained"}}
}

// ImplementMergeSpec declares the outcomes of merging one done story: merged,
// parked with a reason, none when no story is done, retry when the lock is busy
// or the integration branch moved.
func ImplementMergeSpec() ActionSpec {
	return ActionSpec{Name: ImplementMergeAction, Mode: agent.ModeImplement, Outcomes: []string{"merged", "parked", "none", "retry"}}
}

// ImplementHandoffSpec declares the outcome of writing the hand-off report.
func ImplementHandoffSpec() ActionSpec {
	return ActionSpec{Name: ImplementHandoffAction, Mode: agent.ModeImplement, Outcomes: []string{"posted"}}
}

// The actions of the Implement graph that start, finish and park a story, and
// the factory decisions around them. Each does one measurable thing and returns
// an outcome; implement.dot, never Go, decides what follows it.
const (
	ImplementSelectAction     = "lf_implement_select"
	ImplementBudgetAction     = "lf_implement_budget"
	ImplementResultAction     = "lf_implement_result"
	ImplementStoryDoneAction  = "lf_implement_story_done"
	ImplementParkAction       = "lf_implement_park"
	ImplementAwaitAction      = "lf_implement_await"
	ImplementAnyParkedAction  = "lf_implement_any_parked"
	ImplementAskAction        = "lf_implement_ask"
	ImplementReportDoneAction = "lf_implement_report_done"
	ImplementResolvedAction   = "lf_implement_resolved"
)

// ImplementAwaitNode names the node where the coordinator rests while stories
// build. The graph checker pins it, so factory code that wakes a resting
// coordinator may rely on the name.
const ImplementAwaitNode = "await"

// ImplementNodeLimitMax caps any declared Implement node bound, so a typo cannot
// turn a bounded loop into an effectively unbounded one.
const ImplementNodeLimitMax = 256

// ImplementControlPolicy is the closed response vocabulary of an Implement
// worker: the outcome of its accepted result, never an approval.
func ImplementControlPolicy() ControlPolicy {
	return ControlPolicy{Mode: agent.ModeImplement, Responses: []string{"completed", "incomplete", "blocked"}}
}

// ImplementSelectSpec declares the factory decision that sends a parent issue
// down the coordinator path and a story sub-issue down the worker path.
func ImplementSelectSpec() ActionSpec {
	return ActionSpec{Name: ImplementSelectAction, Mode: agent.ModeImplement, Outcomes: []string{"coordinator", "worker"}}
}

// ImplementBudgetSpec declares the repair budget decision: run another attempt
// or park the story.
func ImplementBudgetSpec() ActionSpec {
	return ActionSpec{Name: ImplementBudgetAction, Mode: agent.ModeImplement, Outcomes: []string{"run", "park"}}
}

// ImplementResultSpec declares the decision that reads the worker's accepted
// answer: completed goes on to be saved, incomplete goes back to the budget for
// another attempt, blocked parks. stopped stands for blocked or incomplete in
// released graphs that do not declare the two separately; it parks.
func ImplementResultSpec() ActionSpec {
	return ActionSpec{Name: ImplementResultAction, Mode: agent.ModeImplement, Outcomes: []string{"completed", "incomplete", "blocked", "stopped"}}
}

// ImplementStoryDoneSpec declares the step that commits and pushes the story
// branch and only then moves the story to done: done, or retry when the save or
// the push failed.
func ImplementStoryDoneSpec() ActionSpec {
	return ActionSpec{Name: ImplementStoryDoneAction, Mode: agent.ModeImplement, Outcomes: []string{"done", "retry"}}
}

// ImplementParkSpec declares the step that parks a story with a plain reason.
func ImplementParkSpec() ActionSpec {
	return ActionSpec{Name: ImplementParkAction, Mode: agent.ModeImplement, Outcomes: []string{"parked"}}
}

// ImplementAwaitSpec declares the step where the coordinator rests while stories
// build. It returns go when the run resumes.
func ImplementAwaitSpec() ActionSpec {
	return ActionSpec{Name: ImplementAwaitAction, Mode: agent.ModeImplement, Outcomes: []string{"go"}}
}

// ImplementAnyParkedSpec declares the decision that asks whether any story is
// parked once the hand-off is written.
func ImplementAnyParkedSpec() ActionSpec {
	return ActionSpec{Name: ImplementAnyParkedAction, Mode: agent.ModeImplement, Outcomes: []string{"true", "false"}}
}

// ImplementAskSpec declares the one batched owner question on the parent. Its
// lf_source names the node an owner reply resumes the coordinator at: the
// coordinator base sync, or in older graphs the merge step.
func ImplementAskSpec() ActionSpec {
	return ActionSpec{Name: ImplementAskAction, Mode: agent.ModeImplement, Outcomes: []string{"published"}}
}

// ImplementReportDoneSpec declares the one completion reply on the parent once
// every story of the generation is merged. Its lf_source names the merge step
// whose attempt identifies the reply, so a replay posts nothing new.
func ImplementReportDoneSpec() ActionSpec {
	return ActionSpec{Name: ImplementReportDoneAction, Mode: agent.ModeImplement, Outcomes: []string{"published"}}
}

// ImplementResolvedSpec declares the decision that reads the sync resolver's
// accepted answer: resolved once a completed answer is committed and pushed to
// the feature branch, blocked once the resolver's files are put to the owner,
// retry when the save failed or the answer was incomplete.
func ImplementResolvedSpec() ActionSpec {
	return ActionSpec{Name: ImplementResolvedAction, Mode: agent.ModeImplement, Outcomes: []string{"resolved", "blocked", "retry"}}
}

// ImplementSpecs lists every action of the Implement graph, the agent aside.
func ImplementSpecs() []ActionSpec {
	return []ActionSpec{
		ImplementSelectSpec(), ImplementReadySpec(), ImplementMergeSpec(), ImplementHandoffSpec(), ImplementAwaitSpec(),
		ImplementAnyParkedSpec(), ImplementAskSpec(), ImplementReportDoneSpec(), ImplementBudgetSpec(), ImplementResultSpec(), ImplementStoryDoneSpec(), ImplementParkSpec(),
		SyncBaseSpec(agent.ModeImplement), ImplementResolvedSpec(),
	}
}

// ValidateImplementGraph checks the structure an Implement graph declares. It
// pins no node names; it enforces the properties every Implement graph keeps:
//
//   - the entry is the path selector, and every node has a positive, bounded
//     lf_limit, which bounds every loop;
//   - only the Implement actions, lf_sync_base, one worker agent and any number
//     of sync resolver agents appear. An agent whose success reaches
//     lf_implement_resolved is a resolver; every other agent is the worker;
//   - the worker's accepted answer reaches the save step only through the result
//     decision, so nothing but a completed answer marks a story done;
//   - a failed attempt, a failed save and an exhausted budget route to the
//     budget decision and the park step, so a failure never ends a story quietly;
//   - the result decision either parks stopped (released graphs) or sends
//     incomplete to the budget decision and parks blocked;
//   - the batched question ends at a terminal, where the owner wait rests;
//   - the completion reply, when a graph declares one, reads the merge step and
//     ends at a terminal. A graph without it still validates;
//   - a worker base-sync gate follows budget/run, admits only synced/current
//     to the worker, retries itself and parks conflicts; no incoming worker
//     or gate route may bypass those checks;
//   - a coordinator base sync, when a graph declares one, hands a conflict to a resolver
//     agent; the resolver's success reaches its own resolved decision and its
//     failure returns to a base sync; the decision returns resolved and retry to
//     a base sync and ends blocked at a terminal owner wait. A graph without a
//     base sync (every released graph) still validates.
func ValidateImplementGraph(p *Profile) error {
	if p == nil || p.Mode != agent.ModeImplement {
		return errors.New("invalid Implement graph entry")
	}
	c := &implementCheck{p: p}
	if start, ok := p.Node(p.Start); !ok || start.Action != ImplementSelectAction {
		c.fail("implement graph must start at the path selector (%s), not %q", ImplementSelectAction, p.Start)
	}
	known := map[string]bool{"lf_agent": true}
	for _, spec := range ImplementSpecs() {
		known[spec.Name] = true
	}
	if n, ok := p.Node(ImplementAwaitNode); !ok || n.Action != ImplementAwaitAction {
		c.fail("implement graph requires the %s node to run %s", ImplementAwaitNode, ImplementAwaitAction)
	}
	agents := 0
	for _, n := range p.Nodes {
		if n.Terminal {
			continue
		}
		if n.Limit <= 0 || n.Limit > ImplementNodeLimitMax {
			c.fail("implement node %s requires a positive lf_limit of at most %d", n.Name, ImplementNodeLimitMax)
		}
		if !known[n.Action] {
			c.fail("implement node %s uses action %s the Implement runtime does not implement", n.Name, n.Action)
		}
		switch n.Action {
		case "lf_agent":
			if c.isResolver(n) {
				c.resolver(n)
				continue
			}
			agents++
			c.worker(n)
		case ImplementResultAction:
			c.result(n)
		case ImplementStoryDoneAction:
			c.storyDone(n)
		case ImplementBudgetAction:
			c.budget(n)
			c.edge(n, "park", func(t Node) bool { return t.Action == ImplementParkAction }, "must park the story")
		case ImplementAskAction:
			c.ask(n)
		case ImplementReportDoneAction:
			c.reportDone(n)
		case SyncBaseAction:
			if slices.Contains(n.Outcomes, "deferred") {
				success, a := Decide(p, n.Name, "synced")
				deferred, b := Decide(p, n.Name, "deferred")
				if target, exists := p.Node(success); exists && target.Action == ImplementReportDoneAction {
					c.fail("final synchronization %s cannot defer before reporting completion", n.Name)
				}
				if !a || !b || success != deferred {
					c.fail("sync %s deferred outcome must continue with permitted work", n.Name)
				}
			}
			if c.isWorkerSync(n) {
				c.workerSync(n)
				continue
			}
			conflict, routed := Decide(p, n.Name, "conflict")
			conflictTarget, exists := p.Node(conflict)
			if routed && exists && conflictTarget.Terminal {
				synced, a := Decide(p, n.Name, "synced")
				current, b := Decide(p, n.Name, "current")
				retry, retryRouted := Decide(p, n.Name, "retry")
				retryTarget, d := p.Node(retry)
				if !a || !b || synced != current || !retryRouted || !d || !retryTarget.Terminal {
					c.fail("explicit sync %s requires common success and stopped failure paths", n.Name)
				}
				continue
			}
			c.edge(n, "conflict", c.isResolver, "must hand the conflict to a resolver agent")
		case ImplementResolvedAction:
			c.resolved(n)
		}
	}
	if agents != 1 {
		c.fail("implement graph requires exactly one worker agent, has %d", agents)
	}
	return errors.Join(c.faults...)
}

type implementCheck struct {
	p      *Profile
	faults []error
}

func (c *implementCheck) fail(format string, args ...any) {
	c.faults = append(c.faults, fmt.Errorf(format, args...))
}

func (c *implementCheck) next(n Node, outcome string) (Node, bool) {
	to, ok := Decide(c.p, n.Name, outcome)
	if !ok {
		c.fail("implement node %s has no single edge for outcome %s", n.Name, outcome)
		return Node{}, false
	}
	target, ok := c.p.Node(to)
	seen := map[string]bool{}
	for ok && target.Action == SyncBaseAction {
		conflict, routed := Decide(c.p, target.Name, "conflict")
		failure, exists := c.p.Node(conflict)
		if !routed || !exists || !failure.Terminal {
			break
		}
		if seen[target.Name] {
			c.fail("explicit sync success cycle at %s", target.Name)
			return Node{}, false
		}
		seen[target.Name] = true
		synced, a := Decide(c.p, target.Name, "synced")
		current, b := Decide(c.p, target.Name, "current")
		if !a || !b || synced != current {
			c.fail("explicit sync %s has inconsistent success paths", target.Name)
			return Node{}, false
		}
		to = synced
		target, ok = c.p.Node(to)
	}
	if !ok {
		c.fail("implement node %s outcome %s reaches undeclared %s", n.Name, outcome, to)
	}
	return target, ok
}

func (c *implementCheck) edge(n Node, outcome string, accept func(Node) bool, rule string) {
	if target, ok := c.next(n, outcome); ok && !accept(target) {
		c.fail("implement node %s outcome %s %s, not %s", n.Name, outcome, rule, target.Name)
	}
}

func (c *implementCheck) worker(n Node) {
	if _, _, err := ResearchReferences(n.Attrs); err != nil {
		c.fail("implement agent %s has no valid role: %v", n.Name, err)
	}
	c.edge(n, "success", func(t Node) bool { return t.Action == ImplementResultAction && t.Attrs["lf_source"] == n.Name }, "must reach its own result decision")
	c.edge(n, "failed", func(t Node) bool { return t.Action == ImplementBudgetAction }, "must reach the budget decision")
}

func (c *implementCheck) result(n Node) {
	if source, ok := c.p.Node(n.Attrs["lf_source"]); !ok || source.Action != "lf_agent" {
		c.fail("implement node %s lf_source %q must name the worker agent", n.Name, n.Attrs["lf_source"])
	}
	c.edge(n, "completed", func(t Node) bool { return t.Action == ImplementStoryDoneAction }, "must save the story")
	park := func(t Node) bool { return t.Action == ImplementParkAction }
	switch {
	case slices.Contains(n.Outcomes, "stopped"):
		if slices.Contains(n.Outcomes, "incomplete") || slices.Contains(n.Outcomes, "blocked") {
			c.fail("implement node %s declares stopped together with incomplete or blocked", n.Name)
		}
		c.edge(n, "stopped", park, "must park the story")
	case slices.Contains(n.Outcomes, "incomplete") || slices.Contains(n.Outcomes, "blocked"):
		c.edge(n, "incomplete", func(t Node) bool { return t.Action == ImplementBudgetAction }, "must reach the budget decision")
		c.edge(n, "blocked", park, "must park the story")
	default:
		c.fail("implement node %s must declare stopped, or incomplete and blocked", n.Name)
	}
}

func (c *implementCheck) storyDone(n Node) {
	c.edge(n, "retry", func(t Node) bool { return t.Action == ImplementBudgetAction }, "must reach the budget decision")
	if target, ok := c.next(n, "done"); ok && !target.Terminal {
		c.fail("implement node %s outcome done must end at a terminal", n.Name)
	}
}

// ask accepts an owner reply that resumes at the merge step, or at a
// coordinator base sync whose success reaches the merge step, so main reaches
// the feature branch before resumed stories start.
func (c *implementCheck) ask(n Node) {
	source, ok := c.p.Node(n.Attrs["lf_source"])
	if ok && source.Action == SyncBaseAction {
		synced, a := Decide(c.p, source.Name, "synced")
		current, b := Decide(c.p, source.Name, "current")
		target, exists := c.p.Node(synced)
		ok = a && b && synced == current && exists && target.Action == ImplementMergeAction
	} else if ok {
		ok = source.Action == ImplementMergeAction
	}
	if !ok {
		c.fail("implement node %s lf_source %q must name the merge step an owner reply resumes at, or a base sync whose success reaches it", n.Name, n.Attrs["lf_source"])
	}
	c.edge(n, "published", func(t Node) bool { return t.Terminal }, "must end at a terminal owner wait")
}

func (c *implementCheck) reportDone(n Node) {
	if source, ok := c.p.Node(n.Attrs["lf_source"]); !ok || source.Action != ImplementMergeAction {
		c.fail("implement node %s lf_source %q must name the merge step", n.Name, n.Attrs["lf_source"])
	}
	c.edge(n, "published", func(t Node) bool { return t.Terminal }, "must end at a terminal")
}

// isResolver reports whether n is a sync resolver agent: an agent whose success
// reaches the resolved decision.
func (c *implementCheck) isResolver(n Node) bool {
	if n.Action != "lf_agent" {
		return false
	}
	to, ok := Decide(c.p, n.Name, "success")
	if !ok {
		return false
	}
	target, ok := c.p.Node(to)
	return ok && target.Action == ImplementResolvedAction
}

func (c *implementCheck) resolver(n Node) {
	if _, _, err := ResearchReferences(n.Attrs); err != nil {
		c.fail("implement resolver %s has no valid role: %v", n.Name, err)
	}
	c.edge(n, "success", func(t Node) bool { return t.Attrs["lf_source"] == n.Name }, "must reach its own resolved decision")
	c.edge(n, "failed", func(t Node) bool { return t.Action == SyncBaseAction }, "must return to a base sync")
}

func (c *implementCheck) resolved(n Node) {
	if source, ok := c.p.Node(n.Attrs["lf_source"]); !ok || !c.isResolver(source) {
		c.fail("implement node %s lf_source %q must name the resolver agent whose success reaches it", n.Name, n.Attrs["lf_source"])
	}
	sync := func(t Node) bool { return t.Action == SyncBaseAction }
	c.edge(n, "resolved", sync, "must return to a base sync")
	c.edge(n, "retry", sync, "must return to a base sync")
	c.edge(n, "blocked", func(t Node) bool { return t.Terminal }, "must end at a terminal owner wait")
}

// ImplementWorkerSyncNode identifies a worker gate from the budget's run route.
// Legacy captured graphs that launch the worker directly have no worker gate.
func ImplementWorkerSyncNode(p *Profile) (Node, bool) {
	if p != nil {
		for _, n := range p.Nodes {
			if n.Action == ImplementBudgetAction {
				to, ok := Decide(p, n.Name, "run")
				if target, exists := p.Node(to); ok && exists && target.Action == SyncBaseAction && !implementTerminalSync(p, target) {
					return target, true
				}
			}
		}
	}
	return Node{}, false
}

// Earlier explicit-sync graphs stopped both unsuccessful outcomes at a terminal.
// They retain their generic synchronization binding and do not gain a worker gate.
func implementTerminalSync(p *Profile, n Node) bool {
	for _, outcome := range []string{"conflict", "retry"} {
		to, routed := Decide(p, n.Name, outcome)
		target, exists := p.Node(to)
		if !routed || !exists || !target.Terminal {
			return false
		}
	}
	return true
}

func (c *implementCheck) isWorkerSync(n Node) bool {
	if implementTerminalSync(c.p, n) {
		return false
	}
	if gate, ok := ImplementWorkerSyncNode(c.p); ok && gate.Name == n.Name {
		return true
	}
	for _, outcome := range []string{"synced", "current", "conflict"} {
		to, routed := Decide(c.p, n.Name, outcome)
		target, exists := c.p.Node(to)
		if routed && exists && (target.Action == ImplementParkAction || (target.Action == "lf_agent" && !c.isResolver(target))) {
			return true
		}
	}
	return false
}

func (c *implementCheck) budget(n Node) {
	to, routed := Decide(c.p, n.Name, "run")
	target, exists := c.p.Node(to)
	if routed && exists && target.Action == SyncBaseAction && c.isWorkerSync(target) {
		return
	}
	c.edge(n, "run", func(t Node) bool { return t.Action == "lf_agent" && !c.isResolver(t) }, "must run the worker agent or its sync gate")
}

func (c *implementCheck) workerSync(n Node) {
	if n.Limit != 8 {
		c.fail("worker sync %s requires lf_limit 8", n.Name)
	}
	synced, a := Decide(c.p, n.Name, "synced")
	current, b := Decide(c.p, n.Name, "current")
	worker, exists := c.p.Node(synced)
	if !a || !b || synced != current || !exists || worker.Action != "lf_agent" || c.isResolver(worker) {
		c.fail("worker sync %s requires synced and current to reach the same worker agent", n.Name)
	}
	c.edge(n, "retry", func(t Node) bool { return t.Name == n.Name }, "must retry the same worker sync")
	c.edge(n, "conflict", func(t Node) bool { return t.Action == ImplementParkAction }, "must park the story")
	budgetRun := false
	for _, edge := range c.p.Edges {
		source, _ := c.p.Node(edge.From)
		if edge.To == n.Name {
			if source.Action == ImplementBudgetAction && edge.On == "run" {
				budgetRun = true
				continue
			}
			if edge.From == n.Name && edge.On == "retry" {
				continue
			}
			c.fail("worker sync %s incoming path must pass the budget", n.Name)
		}
		if exists && edge.To == worker.Name && (edge.From != n.Name || (edge.On != "synced" && edge.On != "current" && edge.On != "deferred")) {
			c.fail("worker agent %s incoming path must pass its worker sync", worker.Name)
		}
	}
	if !budgetRun {
		c.fail("worker sync %s must follow the budget run decision", n.Name)
	}
}
