package contracts

import "time"

// OwnerActionNotice identifies a pending outcome and its approved destination.
type OwnerActionNotice struct{ ID, ApprovalChannel string }

// NoticePosition retains the independent source watermark and unresolved work.
type NoticePosition struct {
	Source                      string
	LastID, InitialID, Revision int64
	Pending                     []int64
	InitialAt                   time.Time
	Seen                        map[string]string
}

// NoticeRecord is a factory event projection for delivery presentation.
type NoticeRecord struct {
	ID                                                                        int64
	TaskRef, Workflow, From, To, Key, Kind, StopOrigin, StopCause, StopReason string
	PreviousMode, State                                                       string
	Published                                                                 bool
	RunID                                                                     int64
	RunError                                                                  string
	RunOwnerStopped, RunSucceeded                                             bool
	At                                                                        time.Time
	Candidate                                                                 []byte
	CommentID                                                                 int64
	Ready, Ignore                                                             bool
}
