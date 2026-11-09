package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

// repositoryAccessRole returns the installed team's access/access-classifier.md text for repo, or "" when the team has none (the runner then uses the factory default). Missing team binding also returns "" with no error.
func repositoryAccessRole(paths config.Paths, db *store.DB) func(ctx context.Context, repo store.Repo) (string, error) {
	manager, managerErr := teampackage.New(teampackage.Options{Paths: paths, Validate: turn.ValidateTeamWorkflows, InUse: func(ctx context.Context, id teampackage.Identity) ([]string, error) {
		return db.RepositoryTeamUses(ctx, id.Source, id.Slug, id.Version)
	}})
	return func(ctx context.Context, repo store.Repo) (string, error) {
		if managerErr != nil {
			return "", managerErr
		}
		var text string
		err := manager.WithCatalogue(ctx, func(access *teampackage.CatalogueAccess) error {
			binding, e := db.GetRepositoryTeamBinding(ctx, repo.ID)
			if errors.Is(e, store.ErrNotFound) {
				return nil
			}
			if e != nil {
				return e
			}
			entry, raw, e := access.ReadSelectedFile(ctx, binding.Source, binding.Slug, binding.Version, teamsource.AccessClassifierPath)
			if errors.Is(e, fs.ErrNotExist) {
				return nil
			}
			if e != nil {
				return e
			}
			if binding.Kind == "fetched" && entry.Identity.Digest != binding.ReleaseDigest {
				return fmt.Errorf("repository %s linked release digest changed", repo.Path)
			}
			text = string(raw)
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("repository %s access role: %w", repo.Path, err)
		}
		return text, nil
	}
}
