package contracts

import "time"

// RightsGrantView preserves the local administrative JSON format.
type RightsGrantView struct {
	ID           int64     `json:"id"`
	RepoID       int64     `json:"repo_id"`
	SubjectID    int64     `json:"subject_id"`
	SubjectLogin string    `json:"subject_login"`
	Workflow     string    `json:"workflow"`
	Stage        string    `json:"stage"`
	Action       string    `json:"action"`
	TaskRef      string    `json:"task_ref,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`
	GrantedBy    int64     `json:"granted_by"`
	CreatedAt    time.Time `json:"created_at"`
	RevokedAt    time.Time `json:"revoked_at,omitzero"`
	RevokedBy    int64     `json:"revoked_by,omitempty"`
}

// ReviewerView is a bounded local reviewer administration result.
type ReviewerView struct {
	ID            int64     `json:"id"`
	RepoID        int64     `json:"repo_id"`
	TaskRef       string    `json:"task_ref"`
	Workflow      string    `json:"workflow"`
	Stage         string    `json:"stage"`
	ReviewerID    int64     `json:"reviewer_id"`
	ReviewerLogin string    `json:"reviewer_login"`
	AssignedBy    int64     `json:"assigned_by"`
	CreatedAt     time.Time `json:"created_at"`
	RemovedAt     time.Time `json:"removed_at,omitzero"`
	RemovedBy     int64     `json:"removed_by,omitempty"`
}

// RightsHistoryView contains one append-only local rights audit entry.
type RightsHistoryView struct {
	Sequence int64
	At       time.Time
	ActorID  int64
	Kind     string
	RecordID int64
	Snapshot string
}

// RightsEntryView describes one grantable workflow action.
type RightsEntryView struct {
	Workflow    string `json:"workflow"`
	Stage       string `json:"stage"`
	Action      string `json:"action"`
	Description string `json:"description,omitempty"`
}

func (e RightsEntryView) String() string { return e.Workflow + " | " + e.Stage + " | " + e.Action }

// GrantRightRequest requests one owner-authorized local rights grant.
type GrantRightRequest struct {
	SubjectID                                      int64
	SubjectLogin, Workflow, Stage, Action, TaskRef string
	ExpiresAt                                      time.Time
}

// AssignReviewerRequest requests one reviewer assignment without granting rights.
type AssignReviewerRequest struct {
	ReviewerID                              int64
	ReviewerLogin, Workflow, Stage, TaskRef string
	Replace                                 bool
}

// LocalRightsUnavailable preserves setup refusals separately from runtime errors.
type LocalRightsUnavailable struct{ Err error }

func (e *LocalRightsUnavailable) Error() string { return e.Err.Error() }
func (e *LocalRightsUnavailable) Unwrap() error { return e.Err }
