package contracts

import "time"

// TaskRun is the latest observable run metadata, without execution authority or paths.
type TaskRun struct {
	Ordinal                                 int
	Mode, Workflow, Status, SemanticOutcome string
	StartedAt                               time.Time
}

// Active reports whether execution is allocated or running.
func (r *TaskRun) Active() bool {
	return r != nil && (r.Status == "prelaunch" || r.Status == "running")
}

// Task is a read-only projection of existing factory work.
type Task struct {
	IssueNumber      int
	Reference, State string
	LatestRun        *TaskRun
	PlanStages       []string
	OwnerWait        *OwnerWait
	OwnerBlocked     string
}

// OwnerWait is a published question or review still waiting for the owner.
type OwnerWait struct {
	Workflow, Kind, Node, Asked string
	CommentID                   int64
	Since                       time.Time
}

// TaskProject bounds exact task lookup to a registered project identity.
type TaskProject struct {
	ID          int64
	Owner, Name string
}

// ShownTask identifies one task actually present in a bounded status listing.
type ShownTask struct {
	RepoID  int64
	Project string
	Issue   int
}

// StatusListing is a bounded status projection and its visible task references.
type StatusListing struct {
	Text  string
	Shown []ShownTask
}
