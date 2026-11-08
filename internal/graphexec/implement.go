package graphexec

import (
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

// ImplementSyncResolver reports whether the named node of an Implement graph is
// a sync resolver agent: an agent whose success reaches the resolved decision.
func ImplementSyncResolver(p *Profile, name string) bool {
	if p == nil {
		return false
	}
	n, ok := p.Node(name)
	if !ok || n.Action != "lf_agent" {
		return false
	}
	to, ok := Decide(p, n.Name, "success")
	if !ok {
		return false
	}
	target, ok := p.Node(to)
	return ok && target.Action == ImplementResolvedAction
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
