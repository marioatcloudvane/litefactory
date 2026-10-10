package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

func repositoryWorkflowSource(paths config.Paths, db *store.DB) (turn.WorkflowSourceResolver, error) {
	manager, err := teampackage.New(teampackage.Options{Paths: paths, Validate: turn.ValidateTeamWorkflows, InUse: func(ctx context.Context, id teampackage.Identity) ([]string, error) {
		return db.RepositoryTeamUses(ctx, id.Source, id.Slug, id.Version)
	}})
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, repo store.Repo, workflow agent.Workflow, specs graphexec.Registry) (definition *teamsource.WorkflowDefinition, err error) {
		err = manager.WithCatalogue(ctx, func(access *teampackage.CatalogueAccess) error {
			binding, e := db.GetRepositoryTeamBinding(ctx, repo.ID)
			if e != nil {
				if errors.Is(e, store.ErrNotFound) {
					return fmt.Errorf("repository %s requested workflow %s has no linked installed team: %w", repo.Path, workflow, e)
				}
				return e
			}
			return access.WithSelectedWorkflow(ctx, binding.Source, binding.Slug, binding.Version, string(workflow), func(entry teampackage.Installed, source fs.FS) error {
				if binding.Kind == "fetched" && entry.Identity.Digest != binding.ReleaseDigest {
					return fmt.Errorf("repository %s linked release digest changed", repo.Path)
				}
				graph, ok := entry.Workflows[string(workflow)]
				if !ok {
					available := make([]string, 0, len(entry.Workflows))
					for mode := range entry.Workflows {
						available = append(available, mode)
					}
					slices.Sort(available)
					return fmt.Errorf("repository %s: team %s %s from %s lacks requested workflow %s; available workflows: %s", repo.Path, binding.Slug, binding.Version, binding.Source, workflow, strings.Join(available, ", "))
				}
				raw, e := fs.ReadFile(source, graph)
				if e != nil {
					return e
				}
				profile, e := graphexec.ParseProfile(raw)
				if e != nil {
					return e
				}
				root, e := fs.Sub(source, path.Dir(graph))
				if e != nil {
					return e
				}
				definition, e = teamsource.CaptureWorkflowIdentity(ctx, root, path.Base(graph), profile.Team, workflow, specs)
				if e != nil {
					return e
				}
				teamConfig, e := fs.ReadFile(source, "team.config")
				if e != nil {
					return e
				}
				total := len(teamConfig)
				for _, raw := range definition.Files {
					total += len(raw)
				}
				if total > teamsource.MaxSnapshotBytes {
					return errors.New("workflow and team.config raw capture exceed bounds")
				}
				sum := sha256.Sum256(teamConfig)
				definition.Origin = teamsource.WorkflowOrigin{Kind: binding.Kind, Source: binding.Source, Slug: binding.Slug, Version: binding.Version, ReleaseDigest: binding.ReleaseDigest, GraphPath: graph, GraphFile: path.Base(graph), Team: profile.Team, TeamConfig: slices.Clone(teamConfig), ConfigDigest: hex.EncodeToString(sum[:])}
				base, e := fs.ReadFile(source, config.StackProfilePath)
				legacy := teampackage.IsLegacyProfileless(entry.Identity)
				if entry.Editable {
					legacy = entry.Origin != nil && teampackage.IsLegacyProfileless(teampackage.Identity{Slug: entry.Origin.Slug, Version: entry.Origin.Version})
				}
				if errors.Is(e, fs.ErrNotExist) && legacy {
					return nil
				}
				if e != nil {
					return errors.New("stack.profile: required captured profile unavailable")
				}
				resolved, e := config.ResolveStackProfile(base, nil)
				if e != nil {
					return e
				}
				definition.StackProfile = &resolved
				captured, ok := source.(teamsource.RetainedFiles)
				if !ok {
					return errors.New("stack.capture: expected owned package snapshot")
				}
				definition.StackPackageFiles = make(map[string][]byte, len(captured))
				for name, raw := range captured {
					definition.StackPackageFiles[name] = slices.Clone(raw)
				}
				return nil
			})
		})
		if err != nil {
			return nil, fmt.Errorf("repository %s requested workflow %s: %w", repo.Path, workflow, err)
		}
		return definition, nil
	}, nil
}
