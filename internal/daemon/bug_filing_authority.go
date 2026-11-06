package daemon

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// errBugFilingRevoked reports that the owner's authority for a filing is gone:
// no forge write or start hand-off may follow.
var errBugFilingRevoked = errors.New("daemon: bug filing authority was revoked")

// bugFilingAuthority rechecks, immediately before a forge write or a private
// start hand-off, the owner action's effect authority bound to the filing's
// execution and the owner's consent for the filing's selected repository. nil
// means the write may be sent; errBugFilingRevoked means it never may again;
// any other error is a transient read failure and nothing may be sent now.
func (d *Daemon) bugFilingAuthority(ctx context.Context, f store.BugFiling) error {
	if d.db == nil || !strings.HasPrefix(f.ExecutionID, "owner-action:") || f.RepositoryID < 1 || f.RepositoryRef == "" {
		return errBugFilingRevoked
	}
	clock := d.brainstormTasks.wallNow
	at := clock().UTC()
	err := d.db.CheckOwnerActionEffect(store.WithOwnerActionEffect(ctx, f.ExecutionID, clock))
	if err == nil {
		var record store.OwnerAction
		record, err = d.db.ReadOwnerAction(ctx, strings.TrimPrefix(f.ExecutionID, "owner-action:"))
		if err == nil {
			if record.RepositoryID != f.RepositoryID || record.RepositoryRef != f.RepositoryRef || record.Principal.OwnerID != f.OwnerID || record.Principal.OwnerGeneration != f.OwnerGeneration {
				return errBugFilingRevoked
			}
			err = d.db.CheckOwnerActionAuthority(ctx, record.Principal, store.OwnerAction{Principal: record.Principal, Scope: "repository:actions", RepositoryID: f.RepositoryID, RepositoryRef: f.RepositoryRef}, at)
		}
	}
	return bugFilingAuthorityResult(err)
}

func bugFilingAuthorityResult(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrConnectorOwner), errors.Is(err, store.ErrConnectorInvalid), errors.Is(err, store.ErrConnectorScope), errors.Is(err, store.ErrDoctorFenced):
		return errBugFilingRevoked
	}
	return err
}
