package localstandards

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/standardsexceptions"
	"github.com/cloudvane/litefactory/internal/store"
)

// ErrOwnerRequired preserves the local numeric owner precondition.
var ErrOwnerRequired = errors.New("configured numeric owner identity required")

func localOwner(ctx context.Context, db *store.DB, required bool) (int64, error) {
	owner, err := db.GetIdentity(ctx, store.ForgeGitHub, store.RoleOwner)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return 0, err
	}
	if required && (err != nil || owner.UserID <= 0) {
		return 0, ErrOwnerRequired
	}
	return owner.UserID, nil
}

// Exceptions exposes only local exception proposal and review operations.
type Exceptions struct{ service standardsexceptions.Service }

// NewExceptions resolves the local owner gate for exception operations.
func NewExceptions(ctx context.Context, db *store.DB, mutation bool) (*Exceptions, error) {
	s := &Exceptions{service: standardsexceptions.Service{Store: db}}
	if mutation {
		owner, err := localOwner(ctx, db, true)
		if err != nil {
			return nil, err
		}
		s.service.OwnerID = owner
	}
	return s, nil
}

// Propose records the exact local proposal terms.
func (s *Exceptions) Propose(ctx context.Context, key string, r standardsexceptions.Request) (ExceptionView, error) {
	view, err := s.service.Propose(ctx, key, r)
	return exceptionView(view), err
}

// List reads the existing bounded administrative listing.
func (s *Exceptions) List(ctx context.Context, limit, offset int) ([]ExceptionView, error) {
	rows, err := s.service.List(ctx, limit, offset)
	var views []ExceptionView
	if rows != nil {
		views = make([]ExceptionView, len(rows))
		for i, row := range rows {
			views[i] = exceptionView(row)
		}
	}
	return views, err
}

// Show reads the requested saved version.
func (s *Exceptions) Show(ctx context.Context, id string) (ExceptionView, error) {
	view, err := s.service.Show(ctx, id)
	return exceptionView(view), err
}

// Approve records approval of the exact reviewed version and digest.
func (s *Exceptions) Approve(ctx context.Context, id, digest string) (ExceptionView, error) {
	view, err := s.service.Approve(ctx, id, digest)
	return exceptionView(view), err
}

// Revoke records the explicit local revocation.
func (s *Exceptions) Revoke(ctx context.Context, id, reason string) (ExceptionView, error) {
	view, err := s.service.Revoke(ctx, id, reason)
	return exceptionView(view), err
}
