// Package maintenance owns local factory maintenance operations. It is usable
// while the daemon and optional transports are stopped.
package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// Service is composed by the trusted local host; consumers receive bounded
// operations and values rather than the factory database.
type Service struct{ db *store.DB }

// New composes the local service without starting a daemon.
func New(db *store.DB) *Service { return &Service{db: db} }

// CleanTask keeps Git's own dirty-worktree refusal and clears the saved path
// only after removal or confirmation that the directory has already gone.
func (s *Service) CleanTask(ctx context.Context, request contracts.CleanTaskRequest) (contracts.CleanTaskResult, error) {
	var result contracts.CleanTaskResult
	session, err := s.db.FindSessionByIssue(ctx, request.Issue, request.Owner, request.Repository)
	if err != nil {
		var ambiguous *store.AmbiguousIssueError
		if errors.As(err, &ambiguous) {
			lookup := &contracts.CleanTaskLookupError{Issue: request.Issue, Err: errors.New(err.Error())}
			for _, candidate := range ambiguous.Candidates {
				lookup.Candidates = append(lookup.Candidates, candidate.TaskRef)
			}
			return result, lookup
		}
		if errors.Is(err, store.ErrNotFound) {
			return result, &contracts.CleanTaskLookupError{Issue: request.Issue, Missing: true, Err: err}
		}
		return result, err
	}
	result.TaskRef, result.Path, result.Branch = session.TaskRef, session.WorktreePath, session.Branch
	if session.WorktreePath == "" {
		result.State = contracts.CleanTaskNoWorktree
		return result, nil
	}
	repo, err := s.db.GetRepo(ctx, session.RepoID)
	if err != nil {
		return result, err
	}
	if _, statErr := os.Stat(session.WorktreePath); errors.Is(statErr, fs.ErrNotExist) {
		result.State = contracts.CleanTaskAlreadyGone
	} else {
		binary, err := git.Resolve(ctx)
		if err != nil {
			return result, &contracts.CleanTaskUnavailable{Err: err}
		}
		clone, err := binary.Open(ctx, repo.Path)
		if err != nil {
			return result, &contracts.CleanTaskUnavailable{Err: err}
		}
		err = clone.WorktreeRemove(ctx, session.WorktreePath, git.WorktreeRemoveOptions{Force: request.Force})
		var dirty *git.WorktreeDirtyError
		if errors.As(err, &dirty) {
			refusal := &contracts.CleanTaskDirty{TaskRef: session.TaskRef, Path: dirty.Path}
			for _, change := range dirty.Changes {
				refusal.Changes = append(refusal.Changes, fmt.Sprint(change))
			}
			return result, refusal
		}
		if err != nil {
			return result, err
		}
		result.State = contracts.CleanTaskRemoved
	}
	return result, s.db.SetSessionWorktree(ctx, session.ID, "")
}
