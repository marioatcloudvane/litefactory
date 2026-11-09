package localteams

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
)

// openTeamBindingDB requires an existing ordinary database and no daemon,
// configuration, credentials or network. Migration failures fail closed.
func openTeamBindingDB(ctx context.Context, paths config.Paths) (*store.DB, error) {
	info, err := os.Lstat(paths.DBPath())
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("repository database is not an ordinary file")
	}
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return nil, err
	}
	if err = db.Migrate(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

// RemovalGuard checks durable repository uses before removing a team version.
func RemovalGuard(paths config.Paths) func(context.Context, teampackage.Identity) ([]string, error) {
	return func(ctx context.Context, id teampackage.Identity) (uses []string, err error) {
		db, err := openTeamBindingDB(ctx, paths)
		if errors.Is(err, fs.ErrNotExist) {
			// Only a genuinely absent database means no bindings. An existing broken
			// database must not be mistaken for an uninitialized installation.
			if _, statErr := os.Lstat(paths.DBPath()); errors.Is(statErr, fs.ErrNotExist) {
				return nil, nil
			}
		}
		if err != nil {
			return nil, err
		}
		defer func() { err = errors.Join(err, db.Close()) }()
		return db.RepositoryTeamUses(ctx, id.Source, id.Slug, id.Version)
	}
}

// Link selects verified installed bytes and records the binding under the
// existing catalogue lock. Presentation completes before that lock is released.
func (s *Service) Link(ctx context.Context, request contracts.TeamBindingRequest, present func(contracts.TeamBindingView) error) error {
	return s.bind(ctx, request, false, present)
}

// Unlink restores built-in workflows without discarding immutable captures.
func (s *Service) Unlink(ctx context.Context, request contracts.TeamBindingRequest, present func(contracts.TeamBindingView) error) error {
	return s.bind(ctx, request, true, present)
}

func (s *Service) bind(ctx context.Context, request contracts.TeamBindingRequest, unlink bool, present func(contracts.TeamBindingView) error) (err error) {
	repoPath := request.RepoPath
	if repoPath == "" {
		repoPath, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	binary, err := git.Resolve(ctx)
	if err != nil {
		return err
	}
	clone, err := binary.Open(ctx, repoPath)
	if err != nil {
		return err
	}
	db, err := openTeamBindingDB(ctx, s.paths)
	if errors.Is(err, fs.ErrNotExist) {
		return &contracts.TeamRepositoryUnavailable{Path: clone.Path()}
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	repo, err := db.GetRepoByPath(ctx, clone.Path())
	if errors.Is(err, store.ErrNotFound) {
		return &contracts.TeamRepositoryUnavailable{Path: clone.Path()}
	}
	if err != nil {
		return err
	}
	manager, err := s.manager(nil)
	if err != nil {
		return err
	}
	return manager.WithCatalogue(ctx, func(access *teampackage.CatalogueAccess) error {
		if unlink {
			prior, changed, err := db.DeleteRepositoryTeamBinding(ctx, repo.ID)
			if err != nil {
				return err
			}
			return present(contracts.TeamBindingView{RepoPath: repo.Path, Source: prior.Source, Slug: prior.Slug, Version: prior.Version, Changed: changed, Unlinked: true})
		}
		return access.WithSelected(ctx, request.Source, request.Slug, request.Version, func(entry teampackage.Installed, _ fs.FS) error {
			id := entry.Identity
			binding := store.RepositoryTeamBinding{RepoID: repo.ID, Source: id.Source, Slug: id.Slug, Version: id.Version, Kind: "fetched", ReleaseDigest: id.Digest}
			if entry.Editable {
				binding.Kind = "local"
				binding.ReleaseDigest = ""
			}
			changed, err := db.ReplaceRepositoryTeamBinding(ctx, binding)
			if err != nil {
				return err
			}
			return present(contracts.TeamBindingView{RepoPath: repo.Path, Source: id.Source, Slug: id.Slug, Version: id.Version, Changed: changed})
		})
	})
}
