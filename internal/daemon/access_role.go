package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

// repositoryAccessRole returns the access-classifier role of the team linked
// to repo (access/access-classifier.md). The role is team content: there is no
// factory default, so an unlinked repository or a team without the file is an
// error, which the access gate turns into a "could not be checked" refusal.
func repositoryAccessRole(paths config.Paths, db *store.DB) func(ctx context.Context, repo store.Repo) (string, error) {
	read := repositoryTeamRole(paths, db, teamsource.AccessClassifierPath, "access check")
	return func(ctx context.Context, repo store.Repo) (string, error) {
		raw, err := read(ctx, repo)
		return string(raw), err
	}
}

// repositoryModeClassifier reads the mode classifier role from the team linked
// to the repository (mode/mode-classifier.md); a team may replace it. There is
// no fallback text when no team is linked.
func repositoryModeClassifier(paths config.Paths, db *store.DB) func(context.Context, store.Repo) ([]byte, error) {
	return repositoryTeamRole(paths, db, teamsource.ModeClassifierPath, "mode classifier")
}

// repositoryTeamRole reads one team-level role file from the installed team
// linked to a repository, with the release's per-file integrity check.
func repositoryTeamRole(paths config.Paths, db *store.DB, name, purpose string) func(context.Context, store.Repo) ([]byte, error) {
	return func(ctx context.Context, repo store.Repo) (role []byte, err error) {
		if db == nil {
			return nil, fmt.Errorf("%s: team bindings unavailable", purpose)
		}
		manager, err := teampackage.New(teampackage.Options{Paths: paths, Validate: turn.ValidateTeamWorkflows, InUse: func(ctx context.Context, id teampackage.Identity) ([]string, error) {
			return db.RepositoryTeamUses(ctx, id.Source, id.Slug, id.Version)
		}})
		if err != nil {
			return nil, err
		}
		err = manager.WithCatalogue(ctx, func(access *teampackage.CatalogueAccess) error {
			binding, e := db.GetRepositoryTeamBinding(ctx, repo.ID)
			if e != nil {
				if errors.Is(e, store.ErrNotFound) {
					return &workflowRefusal{kind: report.ReplyMissingTeam, cause: fmt.Errorf("repository %s has no linked installed team; link a team so its %s can run: %w", repo.Path, purpose, e)}
				}
				return e
			}
			entry, raw, e := access.ReadSelectedFile(ctx, binding.Source, binding.Slug, binding.Version, name)
			if errors.Is(e, fs.ErrNotExist) {
				return fmt.Errorf("team %s %s lacks %s; reinstall the team from teams/source: %w", binding.Slug, binding.Version, name, e)
			}
			if e != nil {
				return e
			}
			if binding.Kind == "fetched" && entry.Identity.Digest != binding.ReleaseDigest {
				return fmt.Errorf("repository %s linked release digest changed", repo.Path)
			}
			if strings.TrimSpace(string(raw)) == "" {
				return fmt.Errorf("team %s %s: %s is blank", binding.Slug, binding.Version, name)
			}
			role = raw
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("repository %s %s: %w", repo.Path, purpose, err)
		}
		return role, nil
	}
}
