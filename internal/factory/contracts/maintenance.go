package contracts

// CleanTaskRequest selects the existing local-operator cleanup operation.
// Owner and Repository disambiguate an issue using the established CLI syntax.
type CleanTaskRequest struct {
	Issue             int
	Owner, Repository string
	Force             bool
}

// CleanTaskState identifies the observed worktree cleanup outcome.
type CleanTaskState string

// The following constants retain the existing local operation states.
const (
	CleanTaskNoWorktree  CleanTaskState = "no-worktree"
	CleanTaskAlreadyGone CleanTaskState = "already-gone"
	CleanTaskRemoved     CleanTaskState = "removed"
)

// CleanTaskResult describes the filesystem effect even if recording it fails.
type CleanTaskResult struct {
	State                 CleanTaskState
	TaskRef, Path, Branch string
}

// CleanTaskLookupError keeps storage rows private while preserving lookup detail.
type CleanTaskLookupError struct {
	Issue      int
	Missing    bool
	Candidates []string
	Err        error
}

func (e *CleanTaskLookupError) Error() string { return e.Err.Error() }
func (e *CleanTaskLookupError) Unwrap() error { return e.Err }

// CleanTaskUnavailable marks an unavailable Git executable or clone.
type CleanTaskUnavailable struct{ Err error }

func (e *CleanTaskUnavailable) Error() string { return e.Err.Error() }
func (e *CleanTaskUnavailable) Unwrap() error { return e.Err }

// CleanTaskDirty describes Git's refusal without exposing the saved session.
type CleanTaskDirty struct {
	TaskRef, Path string
	Changes       []string
}

func (e *CleanTaskDirty) Error() string { return "worktree has uncommitted changes" }

// RetirementDecisionView describes one resource and its advisory retirement decision.
type RetirementDecisionView struct {
	Resource, Kind, Action, Reason, ExpectedSHA string
}

// RetirementPreviewView contains one task’s advisory resource decisions.
type RetirementPreviewView struct {
	TaskRef   string
	Decisions []RetirementDecisionView
}

// RetirementPresentation renders preview output at the existing inventory boundaries.
type RetirementPresentation struct {
	Start func() error
	Owner func(RetirementPreviewView) error
}
