package rights

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Request asks whether UserID may take Action at Stage of Workflow in one
// repository, for one task, at Now.
type Request struct {
	RepoID   int64
	UserID   int64
	Workflow string
	Stage    string
	Action   string
	TaskRef  string
	Now      time.Time
}

// Decision is the answer. GrantID names the matching grant; Owner is true when
// the configured owner was allowed without one. Reason explains a denial.
type Decision struct {
	Allowed bool
	Owner   bool
	GrantID int64
	Reason  string
}

// Denial reasons.
const (
	ReasonNoGrant      = "no active grant"
	ReasonExpired      = "grant expired"
	ReasonOtherTask    = "grant is restricted to another task"
	ReasonNotInCatalog = "action is not in the catalogue"
	ReasonError        = "rights check failed"
)

// Check is the one shared, read-only rights question. It fails closed: any
// store or catalogue error, an unknown repository or a missing catalogue entry
// returns Allowed false together with an error. Assignments are never read.
// Revocation and expiry affect only checks made after them.
func (s *Service) Check(ctx context.Context, req Request) (Decision, error) {
	deny := func(reason string, err error) (Decision, error) {
		return Decision{Reason: reason}, err
	}
	if err := s.ready(); err != nil {
		return deny(ReasonError, err)
	}
	repo, err := s.repo(ctx, req.RepoID)
	if err != nil {
		return deny(ReasonError, err)
	}
	if req.UserID == s.OwnerID {
		return Decision{Allowed: true, Owner: true}, nil
	}
	entries, err := s.Catalogue.Catalogue(ctx, repo)
	if err != nil {
		return deny(ReasonError, err)
	}
	if !findEntry(entries, req.Workflow, req.Stage, req.Action) {
		return deny(ReasonNotInCatalog, fmt.Errorf("%w: %s | %s | %s", ErrNotInCatalogue, req.Workflow, req.Stage, req.Action))
	}
	grants, err := s.Store.MatchingRightsGrants(ctx, req.RepoID, req.UserID, req.Workflow, req.Stage, req.Action)
	if err != nil {
		return deny(ReasonError, errors.Join(errors.New("rights: read grants"), err))
	}
	reason := ReasonNoGrant
	for _, g := range grants {
		if !g.ExpiresAt.IsZero() && !req.Now.Before(g.ExpiresAt) {
			if reason == ReasonNoGrant {
				reason = ReasonExpired
			}
			continue
		}
		if g.TaskRef != "" && g.TaskRef != req.TaskRef {
			reason = ReasonOtherTask
			continue
		}
		return Decision{Allowed: true, GrantID: g.ID}, nil
	}
	return Decision{Reason: reason}, nil
}
