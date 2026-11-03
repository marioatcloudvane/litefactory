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

// StoryUpgradeItemView contains a story’s observed and expected contract revisions.
type StoryUpgradeItemView struct {
	Story                                               string
	IssueNumber                                         int
	ObservedRevision, ExpectedRevision, DesiredRevision string
}

// StoryUpgradeView contains the bounded upgrade preview without saved publication intent.
type StoryUpgradeView struct {
	Parent                                                       string
	SourceGeneration, TargetGeneration, FromContract, ToContract int
	State                                                        string
	Items                                                        []StoryUpgradeItemView
	Conflicts                                                    []string
}

// StoryUpgradePresentation renders the preview and confirmed upgrade outcome.
type StoryUpgradePresentation struct {
	Preview     func(StoryUpgradeView) error
	PreviewOnly func() error
	Applied     func() error
}

// LocalMaintenanceUnavailable marks an existing local setup refusal.
type LocalMaintenanceUnavailable struct{ Err error }

func (e *LocalMaintenanceUnavailable) Error() string { return e.Err.Error() }
func (e *LocalMaintenanceUnavailable) Unwrap() error { return e.Err }
