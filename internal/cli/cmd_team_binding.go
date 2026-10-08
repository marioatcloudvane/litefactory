package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
)

func newTeamBindingCommand(operation string) *Command {
	args := ArgSpec{}
	usage := "team unlink [--repo registered-clone-path]"
	if operation == "link" {
		args = ArgSpec{Names: []string{"name", "version"}, Min: 2}
		usage = "team link <name> <version> [--repo registered-clone-path] [--source catalogue-url|local]"
	}
	cmd := &Command{Name: "team " + operation, Usage: usage, Short: "Select an installed team version for a registered clone", Args: args}
	cmd.New = func(flags *flag.FlagSet) Handler {
		repoPath := flags.String("repo", "", "registered clone path (defaults to current repository)")
		var source string
		if operation == "link" {
			flags.StringVar(&source, "source", "", "exact installed source")
		}
		return func(ctx context.Context, env *Env, args []string) error {
			return runTeamBinding(ctx, env, operation, *repoPath, source, args)
		}
	}
	return cmd
}

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

func teamRemovalGuard(paths config.Paths) func(context.Context, teampackage.Identity) ([]string, error) {
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

func runTeamBinding(ctx context.Context, env *Env, operation, repoPath, source string, args []string) (err error) {
	paths, err := config.Locate()
	if err != nil {
		return err
	}
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
	db, err := openTeamBindingDB(ctx, paths)
	if errors.Is(err, fs.ErrNotExist) {
		return Preconditionf("repository %s is not registered; use `litefactory repo add %s`", clone.Path(), clone.Path())
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	repo, err := db.GetRepoByPath(ctx, clone.Path())
	if errors.Is(err, store.ErrNotFound) {
		return Preconditionf("repository %s is not registered; use `litefactory repo add %s`", clone.Path(), clone.Path())
	}
	if err != nil {
		return err
	}
	manager, err := teampackage.New(teampackage.Options{Paths: paths, Validate: validateTeamPackage, Transport: env.TeamTransport, InUse: teamRemovalGuard(paths)})
	if err != nil {
		return err
	}
	return manager.WithCatalogue(ctx, func(access *teampackage.CatalogueAccess) error {
		if operation == "unlink" {
			prior, changed, err := db.DeleteRepositoryTeamBinding(ctx, repo.ID)
			if err != nil {
				return err
			}
			if !changed {
				_, err = fmt.Fprintf(env.Stdout, "Repository %s is already unlinked (built-in workflows)\n", repo.Path)
				return err
			}
			_, err = fmt.Fprintf(env.Stdout, "Unlinked repository %s from %s %s from %s (built-in workflows)\n", repo.Path, prior.Slug, prior.Version, prior.Source)
			return err
		}
		return access.WithSelected(ctx, source, args[0], args[1], func(entry teampackage.Installed, _ fs.FS) error {
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
			status := "Linked"
			if !changed {
				status = "Already linked"
			}
			_, err = fmt.Fprintf(env.Stdout, "%s repository %s to %s %s from %s\n", status, repo.Path, id.Slug, id.Version, id.Source)
			return err
		})
	})
}
