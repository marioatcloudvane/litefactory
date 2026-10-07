package rights

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Refusals of the admin service.
var (
	ErrUnknownRepository = errors.New("rights: unknown repository")
	ErrNotInCatalogue    = errors.New("rights: action is not in the catalogue")
	ErrExpiryPast        = errors.New("rights: expiry is not in the future")
	ErrDuplicate         = errors.New("rights: an active record with this scope already exists")
	ErrNotFound          = errors.New("rights: no open record with that id")
	ErrInvalid           = errors.New("rights: invalid input")
)

// Store is what the rights package needs from the database.
type Store interface {
	GetRepo(ctx context.Context, id int64) (store.Repo, error)
	CreateRightsGrant(ctx context.Context, g store.RightsGrant) (store.RightsGrant, error)
	RevokeRightsGrant(ctx context.Context, repoID, id, actor int64, at time.Time) (store.RightsGrant, error)
	ListRightsGrants(ctx context.Context, repoID int64, activeOnly bool) ([]store.RightsGrant, error)
	MatchingRightsGrants(ctx context.Context, repoID, subjectID int64, workflow, stage, action string) ([]store.RightsGrant, error)
	ReplaceReviewerAssignment(ctx context.Context, a store.ReviewerAssignment, replace bool) (store.ReviewerAssignment, error)
	RemoveReviewerAssignment(ctx context.Context, repoID, id, actor int64, at time.Time) (store.ReviewerAssignment, error)
	ListReviewerAssignments(ctx context.Context, repoID int64, taskRef string, openOnly bool) ([]store.ReviewerAssignment, error)
	ListRightsHistory(ctx context.Context, repoID int64) ([]store.RightsHistoryEntry, error)
}

// Service holds the admin rules and the shared check. OwnerID is the
// configured owner's numeric GitHub ID: it is always allowed and is the actor
// of every change.
type Service struct {
	Store     Store
	Catalogue Source
	OwnerID   int64
	Now       func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) repo(ctx context.Context, id int64) (store.Repo, error) {
	repo, err := s.Store.GetRepo(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Repo{}, fmt.Errorf("%w: %d", ErrUnknownRepository, id)
	}
	return repo, err
}

func (s *Service) ready() error {
	if s.Store == nil || s.Catalogue == nil || s.OwnerID < 1 {
		return errors.New("rights: service needs a store, a catalogue source and the owner's numeric ID")
	}
	return nil
}

// Entries returns the catalogue for a repository.
func (s *Service) Entries(ctx context.Context, repoID int64) ([]Entry, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	repo, err := s.repo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	return s.Catalogue.Catalogue(ctx, repo)
}

// GrantInput describes one grant. Zero TaskRef means any task; zero ExpiresAt
// means no expiry.
type GrantInput struct {
	RepoID       int64
	SubjectID    int64
	SubjectLogin string
	Workflow     string
	Stage        string
	Action       string
	TaskRef      string
	ExpiresAt    time.Time
}

// Grant stores one right after checking it against the repository and the
// catalogue.
func (s *Service) Grant(ctx context.Context, in GrantInput) (store.RightsGrant, error) {
	if err := s.ready(); err != nil {
		return store.RightsGrant{}, err
	}
	if in.SubjectID < 1 || in.SubjectLogin == "" {
		return store.RightsGrant{}, fmt.Errorf("%w: person needs a numeric ID and a login", ErrInvalid)
	}
	entries, err := s.Entries(ctx, in.RepoID)
	if err != nil {
		return store.RightsGrant{}, err
	}
	if !findEntry(entries, in.Workflow, in.Stage, in.Action) {
		return store.RightsGrant{}, fmt.Errorf("%w: %s | %s | %s", ErrNotInCatalogue, in.Workflow, in.Stage, in.Action)
	}
	now := s.now()
	if !in.ExpiresAt.IsZero() && !in.ExpiresAt.After(now) {
		return store.RightsGrant{}, ErrExpiryPast
	}
	g, err := s.Store.CreateRightsGrant(ctx, store.RightsGrant{
		RepoID: in.RepoID, SubjectID: in.SubjectID, SubjectLogin: in.SubjectLogin,
		Workflow: in.Workflow, Stage: in.Stage, Action: in.Action, TaskRef: in.TaskRef,
		ExpiresAt: in.ExpiresAt.UTC(), GrantedBy: s.OwnerID, CreatedAt: now,
	})
	if errors.Is(err, store.ErrRightsDuplicate) {
		return store.RightsGrant{}, ErrDuplicate
	}
	return g, err
}

// Revoke closes an active grant; the row stays.
func (s *Service) Revoke(ctx context.Context, repoID, grantID int64) (store.RightsGrant, error) {
	if err := s.ready(); err != nil {
		return store.RightsGrant{}, err
	}
	if _, err := s.repo(ctx, repoID); err != nil {
		return store.RightsGrant{}, err
	}
	g, err := s.Store.RevokeRightsGrant(ctx, repoID, grantID, s.OwnerID, s.now())
	if errors.Is(err, store.ErrNotFound) {
		return store.RightsGrant{}, fmt.Errorf("%w: grant %d", ErrNotFound, grantID)
	}
	return g, err
}

// ListGrants lists a repository's grants.
func (s *Service) ListGrants(ctx context.Context, repoID int64, activeOnly bool) ([]store.RightsGrant, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if _, err := s.repo(ctx, repoID); err != nil {
		return nil, err
	}
	return s.Store.ListRightsGrants(ctx, repoID, activeOnly)
}

// AssignInput describes one reviewer assignment.
type AssignInput struct {
	RepoID        int64
	TaskRef       string
	Workflow      string
	Stage         string
	ReviewerID    int64
	ReviewerLogin string
}

// Assign names a reviewer for a task and stage. It grants no right.
func (s *Service) Assign(ctx context.Context, in AssignInput) (store.ReviewerAssignment, error) {
	return s.assign(ctx, in, false)
}

// Replace closes the open reviewers of the task and stage and assigns the new
// one in one transaction.
func (s *Service) Replace(ctx context.Context, in AssignInput) (store.ReviewerAssignment, error) {
	return s.assign(ctx, in, true)
}

func (s *Service) assign(ctx context.Context, in AssignInput, replace bool) (store.ReviewerAssignment, error) {
	if err := s.ready(); err != nil {
		return store.ReviewerAssignment{}, err
	}
	if in.ReviewerID < 1 || in.ReviewerLogin == "" || in.TaskRef == "" {
		return store.ReviewerAssignment{}, fmt.Errorf("%w: reviewer needs a numeric ID and a login, and a task is required", ErrInvalid)
	}
	entries, err := s.Entries(ctx, in.RepoID)
	if err != nil {
		return store.ReviewerAssignment{}, err
	}
	if !findStage(entries, in.Workflow, in.Stage) {
		return store.ReviewerAssignment{}, fmt.Errorf("%w: stage %s | %s", ErrNotInCatalogue, in.Workflow, in.Stage)
	}
	a, err := s.Store.ReplaceReviewerAssignment(ctx, store.ReviewerAssignment{
		RepoID: in.RepoID, TaskRef: in.TaskRef, Workflow: in.Workflow, Stage: in.Stage,
		ReviewerID: in.ReviewerID, ReviewerLogin: in.ReviewerLogin, AssignedBy: s.OwnerID, CreatedAt: s.now(),
	}, replace)
	if errors.Is(err, store.ErrRightsDuplicate) {
		return store.ReviewerAssignment{}, ErrDuplicate
	}
	return a, err
}

// Remove closes an open assignment.
func (s *Service) Remove(ctx context.Context, repoID, assignmentID int64) (store.ReviewerAssignment, error) {
	if err := s.ready(); err != nil {
		return store.ReviewerAssignment{}, err
	}
	if _, err := s.repo(ctx, repoID); err != nil {
		return store.ReviewerAssignment{}, err
	}
	a, err := s.Store.RemoveReviewerAssignment(ctx, repoID, assignmentID, s.OwnerID, s.now())
	if errors.Is(err, store.ErrNotFound) {
		return store.ReviewerAssignment{}, fmt.Errorf("%w: assignment %d", ErrNotFound, assignmentID)
	}
	return a, err
}

// ListReviewers lists assignments, optionally for one task.
func (s *Service) ListReviewers(ctx context.Context, repoID int64, taskRef string, openOnly bool) ([]store.ReviewerAssignment, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if _, err := s.repo(ctx, repoID); err != nil {
		return nil, err
	}
	return s.Store.ListReviewerAssignments(ctx, repoID, taskRef, openOnly)
}

// History lists a repository's rights and reviewer changes in order.
func (s *Service) History(ctx context.Context, repoID int64) ([]store.RightsHistoryEntry, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if _, err := s.repo(ctx, repoID); err != nil {
		return nil, err
	}
	return s.Store.ListRightsHistory(ctx, repoID)
}
