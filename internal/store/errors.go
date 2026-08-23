package store

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound is returned by a single-row read whose row does not exist.
// Callers translate it: internal/cli turns it into a clean message and a
// non-zero exit, never a stack trace (AC-5.3).
//
// This package deliberately does not import internal/cli. Exit codes are the
// command layer's business; the dependency direction is cli -> domain.
var ErrNotFound = errors.New("store: not found")

// ErrDuplicateRun is returned when a run is allocated for a
// (session, trigger_comment_id) pair that already has one. That uniqueness is
// what makes crash replay safe (AC-3.4): a comment can cause at most one run,
// ever. Callers that might be replaying should look first with
// [DB.GetRunByTrigger] rather than allocating and catching this.
var ErrDuplicateRun = errors.New("store: a run already exists for this trigger comment")

// AmbiguousIssueError reports that an issue number matches sessions in more
// than one registered repository, so `litefactory logs 123` cannot tell which
// task was meant.
//
// It is a typed error rather than a sentinel because the command has to print
// the candidates and tell the operator which --repo values would disambiguate
// (AC-5.2).
type AmbiguousIssueError struct {
	// IssueNumber is the number that matched more than once.
	IssueNumber int
	// Candidates are the matching repositories, ordered by owner then name.
	Candidates []IssueCandidate
}

// IssueCandidate is one repository in which an ambiguous issue number exists.
type IssueCandidate struct {
	// RepoID is the repos.id of the candidate.
	RepoID int64
	// Forge, Owner and Name identify the repository.
	Forge string
	Owner string
	Name  string
	// TaskRef is the canonical task reference, github:owner/name#123.
	TaskRef string
}

// String renders the candidate as owner/name, the form --repo accepts.
func (c IssueCandidate) String() string { return c.Owner + "/" + c.Name }

func (e *AmbiguousIssueError) Error() string {
	names := make([]string, 0, len(e.Candidates))
	for _, c := range e.Candidates {
		names = append(names, c.String())
	}
	return fmt.Sprintf("issue %d exists in more than one registered repository (%s)",
		e.IssueNumber, strings.Join(names, ", "))
}
