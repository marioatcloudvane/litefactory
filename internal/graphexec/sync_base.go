package graphexec

import "github.com/cloudvane/litefactory/internal/agent"

// SyncBaseAction is the generic step that brings a session's base branch (the
// repository default branch, usually main) into the branch a graph works on.
// Any graph may declare it; each graph binds its own working branch. For the
// Implement coordinator the working branch is the feature (integration) branch.
const SyncBaseAction = "lf_sync_base"

// SyncBaseSpec declares the outcomes of bringing the base into the working
// branch for one mode:
//
//	synced    the base was merged and the working branch was published
//	current   the working branch already holds the base; checkout is refreshed
//	conflict  merge conflict or retained-work safety blockage requiring owner action
//	retry     the working branch is busy or moved
func SyncBaseSpec(mode agent.Mode) ActionSpec {
	return ActionSpec{Name: SyncBaseAction, Mode: mode, Outcomes: []string{"synced", "current", "conflict", "retry"}}
}

// SyncBaseWorkflowSpec admits synchronization for a workflow identity, including
// standalone Bug, whose legacy mode is empty.
func SyncBaseWorkflowSpec(workflow agent.Workflow, mode agent.Mode) ActionSpec {
	spec := SyncBaseSpec(mode)
	spec.Workflow = workflow
	if workflow == agent.WorkflowBug {
		spec.Attributes = OwnerStopAttributes()
		stop := spec.Attributes["lf_stop"]
		stop.Values = append(stop.Values, "conflict", "retry")
		spec.Attributes["lf_stop"] = stop
	}
	return spec
}
