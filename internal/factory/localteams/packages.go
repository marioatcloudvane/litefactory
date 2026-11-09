// Package localteams owns stopped-daemon team package and repository binding operations.
package localteams

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/teambuild"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Options configures the local service. It has no settings today.
type Options struct{}

// Service owns bounded local administration operations.
type Service struct {
	paths config.Paths
}

// New composes the local service without starting a daemon.
func New(Options) (*Service, error) {
	paths, err := config.Locate()
	if err != nil {
		return nil, err
	}
	return &Service{paths: paths}, nil
}

// ValidatePackage applies the same factory workflow validation used at install.
func ValidatePackage(ctx context.Context, source fs.FS, workflows map[string]string) error {
	return turn.ValidateTeamWorkflows(ctx, source, workflows)
}
func (s *Service) manager(rebind teampackage.Rebind) (*teampackage.Manager, error) {
	return teampackage.New(teampackage.Options{Paths: s.paths, Validate: ValidatePackage, InUse: RemovalGuard(s.paths), Rebind: rebind})
}

// defaultSource returns the explicit source, else settings.team_source.
func (s *Service) defaultSource(source string) (string, error) {
	if source != "" {
		return source, nil
	}
	file, err := os.Open(s.paths.ConfigPath())
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	cfg, err := config.Decode(file)
	if err != nil {
		return "", err
	}
	return cfg.Settings.TeamSource, nil
}
func entryView(entry teampackage.Installed) contracts.TeamEntryView {
	view := contracts.TeamEntryView{Identity: contracts.TeamIdentityView(entry.Identity), Path: entry.Path, Editable: entry.Editable}
	for mode := range entry.Workflows {
		view.Workflows = append(view.Workflows, mode)
	}
	if entry.Origin != nil {
		origin := contracts.TeamOriginView(*entry.Origin)
		view.Origin = &origin
	}
	if entry.Build != nil {
		build := contracts.TeamBuildView(*entry.Build)
		view.Build = &build
	}
	return view
}

// Install builds one team from a teams/source folder and installs it. A
// reinstall with other bytes replaces the installed version in place and moves
// every repository linked to it onto the new digest in the same transaction.
func (s *Service) Install(ctx context.Context, source, slug string) (contracts.TeamInstallView, error) {
	source, err := s.defaultSource(source)
	if err != nil {
		return contracts.TeamInstallView{}, err
	}
	identity, dir, err := config.TeamSourceFolder(source)
	if err != nil {
		return contracts.TeamInstallView{}, err
	}
	files, release, err := teambuild.Build(ctx, dir, slug)
	if err != nil {
		return contracts.TeamInstallView{}, err
	}
	build := folderState(ctx, dir)
	var rebound []string
	manager, err := s.manager(func(ctx context.Context, _, next teampackage.Identity, swap func() error) error {
		db, err := openTeamBindingDB(ctx, s.paths)
		if errors.Is(err, fs.ErrNotExist) {
			if _, statErr := os.Lstat(s.paths.DBPath()); errors.Is(statErr, fs.ErrNotExist) {
				return swap()
			}
		}
		if err != nil {
			return err
		}
		paths, err := db.RebindRepositoryTeamDigest(ctx, next.Source, next.Slug, next.Version, next.Digest, swap)
		rebound = paths
		return errors.Join(err, db.Close())
	})
	if err != nil {
		return contracts.TeamInstallView{}, err
	}
	entry, result, err := manager.InstallBuilt(ctx, identity, files, release, build)
	view := contracts.TeamInstallView{Entry: entryView(entry), Rebound: rebound}
	switch result {
	case teampackage.InstallNew:
		view.Outcome = "installed"
	case teampackage.InstallReplaced:
		view.Outcome = "replaced"
	default:
		view.Outcome = "unchanged"
	}
	return view, err
}

// folderState records the git HEAD and dirtiness of the source folder. A
// folder outside a git checkout, or a machine without git, records neither.
func folderState(ctx context.Context, dir string) teampackage.BuildInfo {
	binary, err := git.Resolve(ctx)
	if err != nil {
		return teampackage.BuildInfo{}
	}
	clone, err := binary.Open(ctx, dir)
	if err != nil {
		return teampackage.BuildInfo{}
	}
	head, dirty, err := clone.FolderState(ctx, dir)
	if err != nil {
		return teampackage.BuildInfo{}
	}
	return teampackage.BuildInfo{SourceHead: head, SourceDirty: dirty}
}

// Clone creates an editable team copy with retained origin metadata.
func (s *Service) Clone(ctx context.Context, source, slug, version, newSlug, newVersion string) (contracts.TeamEntryView, error) {
	manager, err := s.manager(nil)
	if err != nil {
		return contracts.TeamEntryView{}, err
	}
	entry, err := manager.Clone(ctx, source, slug, version, newSlug, newVersion)
	return entryView(entry), err
}

// List returns the existing local administration inventory.
func (s *Service) List(ctx context.Context) ([]contracts.TeamEntryView, error) {
	manager, err := s.manager(nil)
	if err != nil {
		return nil, err
	}
	entries, err := manager.List(ctx)
	var views []contracts.TeamEntryView
	for _, entry := range entries {
		views = append(views, entryView(entry))
	}
	return views, err
}

// Inspect verifies and returns the selected installed team metadata.
func (s *Service) Inspect(ctx context.Context, source, slug, version string) (contracts.TeamEntryView, error) {
	manager, err := s.manager(nil)
	if err != nil {
		return contracts.TeamEntryView{}, err
	}
	entry, err := manager.Inspect(ctx, source, slug, version)
	return entryView(entry), err
}

// Remove removes the selected local resource under its existing safety checks.
func (s *Service) Remove(ctx context.Context, source, slug, version string) (bool, error) {
	manager, err := s.manager(nil)
	if err != nil {
		return false, err
	}
	return manager.Remove(ctx, source, slug, version)
}
