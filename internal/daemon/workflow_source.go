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
	"github.com/cloudvane/litefactory/internal/factorylib"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

// workflowRefusal identifies only owner-fixable admission failures.
type workflowRefusal struct {
	kind  report.ReplyKind
	cause error
}

func (e *workflowRefusal) Error() string { return e.cause.Error() }
func (e *workflowRefusal) Unwrap() error { return e.cause }

// RepositoryWorkflowSource resolves a repository's selected installed team.
func RepositoryWorkflowSource(paths config.Paths, db *store.DB) (turn.WorkflowSourceResolver, error) {
	return repositoryWorkflowSource(paths, db)
}

func repositoryWorkflowSource(paths config.Paths, db *store.DB) (turn.WorkflowSourceResolver, error) {
	manager, err := teampackage.New(teampackage.Options{Paths: paths, Validate: turn.ValidateTeamWorkflows, InUse: func(ctx context.Context, id teampackage.Identity) ([]string, error) {
		return db.RepositoryTeamUses(ctx, id.Source, id.Slug, id.Version)
	}})
	if err != nil {
		return nil, err
	}
	library := factorylib.NewInstalled(paths)
	return func(ctx context.Context, repo store.Repo, workflow agent.Workflow, specs graphexec.Registry) (definition *teamsource.WorkflowDefinition, err error) {
		err = manager.WithCatalogue(ctx, func(access *teampackage.CatalogueAccess) error {
			binding, e := db.GetRepositoryTeamBinding(ctx, repo.ID)
			if e != nil {
				if errors.Is(e, store.ErrNotFound) {
					return &workflowRefusal{kind: report.ReplyMissingTeam, cause: fmt.Errorf("repository %s requested workflow %s has no linked installed team: %w", repo.Path, workflow, e)}
				}
				return e
			}
			return access.WithSelectedWorkflow(ctx, binding.Source, binding.Slug, binding.Version, string(workflow), func(entry teampackage.Installed, source fs.FS) error {
				if binding.Kind == "fetched" && entry.Identity.Digest != binding.ReleaseDigest {
					return fmt.Errorf("repository %s linked release digest changed", repo.Path)
				}
				graph, ok := entry.Workflows[string(workflow)]
				root := source
				if workflow == agent.WorkflowDoctor {
					// The Doctor is a factory library workflow, not team content:
					// its graph and role come from the installed library, while
					// the linked team still supplies team.config and the stack.
					lib, e := library.Library(ctx)
					if e != nil {
						return e
					}
					if root, e = lib.FS(factorylib.DoctorDir); e != nil {
						return e
					}
					graph, ok = factorylib.DoctorDir+"/"+factorylib.DoctorGraph, true
				}
				if !ok {
					available := make([]string, 0, len(entry.Workflows))
					for mode := range entry.Workflows {
						available = append(available, mode)
					}
					slices.Sort(available)
					return &workflowRefusal{kind: report.ReplyUnsupportedWorkflow, cause: fmt.Errorf("repository %s: team %s %s from %s lacks requested workflow %s; available workflows: %s", repo.Path, binding.Slug, binding.Version, binding.Source, workflow, strings.Join(available, ", "))}
				}
				if workflow != agent.WorkflowDoctor {
					var e error
					if root, e = fs.Sub(source, path.Dir(graph)); e != nil {
						return e
					}
				}
				raw, e := fs.ReadFile(root, path.Base(graph))
				if e != nil {
					return e
				}
				profile, e := graphexec.ParseProfile(raw)
				if e != nil {
					return e
				}
				// An installed snapshot is fresh input even when a root-level graph
				// makes fs.Sub preserve its RetainedFiles representation. Only saved
				// workflow reconstruction may select the retained capture path.
				definition, e = teamsource.CaptureWorkflowIdentity(ctx, struct{ fs.FS }{root}, path.Base(graph), profile.Team, workflow, specs, library)
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

// repositoryModeClassifier reads the mode classifier role from the team linked
// to the repository. The role is team content (base-team/mode), so a team may
// replace it; there is no fallback text when no team is linked.
func repositoryModeClassifier(paths config.Paths, db *store.DB) func(context.Context, store.Repo) ([]byte, error) {
	return func(ctx context.Context, repo store.Repo) (role []byte, err error) {
		if db == nil {
			return nil, errors.New("mode classifier: team bindings unavailable")
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
					return &workflowRefusal{kind: report.ReplyMissingTeam, cause: fmt.Errorf("repository %s has no linked installed team; link a team so its mode classifier can run: %w", repo.Path, e)}
				}
				return e
			}
			return access.WithSelected(ctx, binding.Source, binding.Slug, binding.Version, func(_ teampackage.Installed, source fs.FS) error {
				raw, e := fs.ReadFile(source, teamsource.ModeClassifierPath)
				if e != nil {
					return fmt.Errorf("team %s %s lacks %s; refresh and reinstall the team: %w", binding.Slug, binding.Version, teamsource.ModeClassifierPath, e)
				}
				if len(strings.TrimSpace(string(raw))) == 0 {
					return fmt.Errorf("team %s %s: %s is blank", binding.Slug, binding.Version, teamsource.ModeClassifierPath)
				}
				role = raw
				return nil
			})
		})
		if err != nil {
			return nil, fmt.Errorf("repository %s mode classifier: %w", repo.Path, err)
		}
		return role, nil
	}
}
