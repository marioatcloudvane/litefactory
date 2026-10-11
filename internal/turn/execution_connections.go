package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// executionRepository returns the original version for admitted work. Legacy
// callers remain supported until factory initialization attaches snapshots.
func executionRepository(ctx context.Context, db *store.DB, session store.Session) (store.Repo, error) {
	saved, err := db.GetExecutionSnapshot(ctx, session.ID)
	if err == nil {
		return saved.CodeTarget, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Repo{}, err
	}
	return db.GetRepo(ctx, session.RepoID)
}

func verifyExecutionClone(ctx context.Context, opts Options, session store.Session, clone *git.Clone) error {
	saved, err := opts.DB.GetExecutionSnapshot(ctx, session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if clone.Path() != saved.CodeTarget.Path {
		return errors.New("original execution clone changed; restore the retained target")
	}
	remote, err := clone.RemoteURL(ctx, opts.remote())
	if err != nil {
		return err
	}
	if remote != saved.CodeTarget.RemoteURL {
		return errors.New("original execution remote changed; restore the retained target")
	}
	parsed, err := git.ParseRemoteURL(remote)
	if err != nil {
		return err
	}
	if parsed.Owner != saved.CodeTarget.Owner || parsed.Name != saved.CodeTarget.Name {
		return fmt.Errorf("original clone does not target the retained code repository")
	}
	if opts.VerifyExecutionConnection != nil {
		return opts.VerifyExecutionConnection(ctx, session)
	}
	return nil
}
