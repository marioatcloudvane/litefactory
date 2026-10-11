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
//	current   the working branch already holds the base; nothing changed
//	conflict  the base conflicts with the working branch, which is unchanged
//	retry     the working branch is busy or moved meanwhile; nothing changed
func SyncBaseSpec(mode agent.Mode) ActionSpec {
	return ActionSpec{Name: SyncBaseAction, Mode: mode, Outcomes: []string{"synced", "current", "conflict", "retry"}}
}
