package graphexec

import (
	"errors"
	"fmt"

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
// answer: completed goes on to be saved, stopped (blocked or incomplete) parks.
func ImplementResultSpec() ActionSpec {
	return ActionSpec{Name: ImplementResultAction, Mode: agent.ModeImplement, Outcomes: []string{"completed", "stopped"}}
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
// lf_source names the node an owner reply resumes the coordinator at.
func ImplementAskSpec() ActionSpec {
	return ActionSpec{Name: ImplementAskAction, Mode: agent.ModeImplement, Outcomes: []string{"published"}}
}

// ImplementReportDoneSpec declares the one completion reply on the parent once
// every story of the generation is merged. Its lf_source names the merge step
// whose attempt identifies the reply, so a replay posts nothing new.
func ImplementReportDoneSpec() ActionSpec {
	return ActionSpec{Name: ImplementReportDoneAction, Mode: agent.ModeImplement, Outcomes: []string{"published"}}
}

// ImplementSpecs lists every action of the Implement graph, the agent aside.
func ImplementSpecs() []ActionSpec {
	return []ActionSpec{
		ImplementSelectSpec(), ImplementReadySpec(), ImplementMergeSpec(), ImplementHandoffSpec(), ImplementAwaitSpec(),
		ImplementAnyParkedSpec(), ImplementAskSpec(), ImplementReportDoneSpec(), ImplementBudgetSpec(), ImplementResultSpec(), ImplementStoryDoneSpec(), ImplementParkSpec(),
	}
}

// ValidateImplementGraph checks the structure an Implement graph declares. It
// pins no node names; it enforces the properties every Implement graph keeps:
//
//   - the entry is the path selector, and every node has a positive, bounded
//     lf_limit, which bounds every loop;
//   - only the Implement actions and one worker agent appear;
//   - the worker's accepted answer reaches the save step only through the result
//     decision, so nothing but a completed answer marks a story done;
//   - a failed attempt, a failed save and an exhausted budget route to the
//     budget decision and the park step, so a failure never ends a story quietly;
//   - the batched question ends at a terminal, where the owner wait rests;
//   - the completion reply, when a graph declares one, reads the merge step and
//     ends at a terminal. A graph without it still validates.
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
			agents++
			c.worker(n)
		case ImplementResultAction:
			c.result(n)
		case ImplementStoryDoneAction:
			c.storyDone(n)
		case ImplementBudgetAction:
			c.edge(n, "run", func(t Node) bool { return t.Action == "lf_agent" }, "must run the worker agent")
			c.edge(n, "park", func(t Node) bool { return t.Action == ImplementParkAction }, "must park the story")
		case ImplementAskAction:
			c.ask(n)
		case ImplementReportDoneAction:
			c.reportDone(n)
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
	c.edge(n, "stopped", func(t Node) bool { return t.Action == ImplementParkAction }, "must park the story")
}

func (c *implementCheck) storyDone(n Node) {
	c.edge(n, "retry", func(t Node) bool { return t.Action == ImplementBudgetAction }, "must reach the budget decision")
	if target, ok := c.next(n, "done"); ok && !target.Terminal {
		c.fail("implement node %s outcome done must end at a terminal", n.Name)
	}
}

func (c *implementCheck) ask(n Node) {
	if source, ok := c.p.Node(n.Attrs["lf_source"]); !ok || source.Action != ImplementMergeAction {
		c.fail("implement node %s lf_source %q must name the merge step an owner reply resumes at", n.Name, n.Attrs["lf_source"])
	}
	c.edge(n, "published", func(t Node) bool { return t.Terminal }, "must end at a terminal owner wait")
}

func (c *implementCheck) reportDone(n Node) {
	if source, ok := c.p.Node(n.Attrs["lf_source"]); !ok || source.Action != ImplementMergeAction {
		c.fail("implement node %s lf_source %q must name the merge step", n.Name, n.Attrs["lf_source"])
	}
	c.edge(n, "published", func(t Node) bool { return t.Terminal }, "must end at a terminal")
}
