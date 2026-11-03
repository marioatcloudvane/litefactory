// Package localteams owns stopped-daemon team package and repository binding operations.
package localteams

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Options contains the trusted local host's provider transport.
type Options struct{ Transport http.RoundTripper }

// Service owns bounded local administration operations.
type Service struct {
	paths     config.Paths
	transport http.RoundTripper
}

// New composes the local service without starting a daemon.
func New(options Options) (*Service, error) {
	paths, err := config.Locate()
	if err != nil {
		return nil, err
	}
	return &Service{paths: paths, transport: options.Transport}, nil
}

// ValidatePackage applies the same factory workflow validation used at install.
func ValidatePackage(ctx context.Context, source fs.FS, workflows map[string]string) error {
	return turn.ValidateTeamWorkflows(ctx, source, workflows)
}
func (s *Service) manager() (*teampackage.Manager, error) {
	return teampackage.New(teampackage.Options{Paths: s.paths, Transport: s.transport, Validate: ValidatePackage, InUse: RemovalGuard(s.paths)})
}
func (s *Service) defaultSource(source string) (string, error) {
	if source != "" {
		return source, nil
	}
	file, err := os.Open(s.paths.ConfigPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	var cfg *config.Config
	if file != nil {
		defer file.Close()
		cfg, err = config.Decode(file)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if cfg != nil {
		return cfg.Settings.TeamSource, nil
	}
	return source, nil
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
	return view
}

// Install verifies and retains the selected team version.
func (s *Service) Install(ctx context.Context, source, slug, version string) (contracts.TeamEntryView, bool, error) {
	source, err := s.defaultSource(source)
	if err != nil {
		return contracts.TeamEntryView{}, false, err
	}
	manager, err := s.manager()
	if err != nil {
		return contracts.TeamEntryView{}, false, err
	}
	entry, already, err := manager.Install(ctx, source, slug, version)
	return entryView(entry), already, err
}

// Update checks for and installs the selected team’s next release.
func (s *Service) Update(ctx context.Context, source, slug string) (contracts.TeamEntryView, bool, error) {
	source, err := s.defaultSource(source)
	if err != nil {
		return contracts.TeamEntryView{}, false, err
	}
	manager, err := s.manager()
	if err != nil {
		return contracts.TeamEntryView{}, false, err
	}
	entry, updated, err := manager.Update(ctx, source, slug)
	return entryView(entry), updated, err
}

// Clone creates an editable team copy with retained origin metadata.
func (s *Service) Clone(ctx context.Context, source, slug, version, newSlug, newVersion string) (contracts.TeamEntryView, error) {
	manager, err := s.manager()
	if err != nil {
		return contracts.TeamEntryView{}, err
	}
	entry, err := manager.Clone(ctx, source, slug, version, newSlug, newVersion)
	return entryView(entry), err
}

// List returns the existing local administration inventory.
func (s *Service) List(ctx context.Context) ([]contracts.TeamEntryView, error) {
	manager, err := s.manager()
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
	manager, err := s.manager()
	if err != nil {
		return contracts.TeamEntryView{}, err
	}
	entry, err := manager.Inspect(ctx, source, slug, version)
	return entryView(entry), err
}

// Remove removes the selected local resource under its existing safety checks.
func (s *Service) Remove(ctx context.Context, source, slug, version string) (bool, error) {
	manager, err := s.manager()
	if err != nil {
		return false, err
	}
	return manager.Remove(ctx, source, slug, version)
}
