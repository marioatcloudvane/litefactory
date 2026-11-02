package store

import "time"

// CIRun is the retained, nonsecret identity of an Actions run and rerun attempt.
type CIRun struct {
	ID, WorkflowID, RunNumber, Attempt       int64
	HeadSHA, Branch, Status, Conclusion, URL string
	CreatedAt, UpdatedAt                     time.Time
}

// CIObservation is source position within the shared alert journal. Transport
// admission is stored separately and cannot change these recovery facts.
type CIObservation struct {
	Generation                                                                               int64
	Configuration, Target, Origin, Repository, Branch, Workflow, WorkflowName, WorkflowState string
	RepositoryID, WorkflowID                                                                 int64
	LastAttempt, LastSuccess, NextDue, Cutoff                                                time.Time
	Ready, Pending                                                                           bool
	Reason, Conclusion                                                                       string
	Latest, Decisive, Failure                                                                *CIRun
}
