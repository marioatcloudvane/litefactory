package teampackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// CatalogueAccess is valid only inside WithCatalogue. Its methods never acquire
// the lock again. Callbacks must not retain it or perform execution or network I/O.
type CatalogueAccess struct {
	manager *Manager
	active  bool
}

// WithCatalogue coordinates repository binding reads and writes with removal
// across processes. No SQL transaction may be held while acquiring this lock.
func (m *Manager) WithCatalogue(ctx context.Context, use func(*CatalogueAccess) error) error {
	if use == nil {
		return errors.New("catalogue callback required")
	}
	unlock, err := m.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	access := &CatalogueAccess{manager: m, active: true}
	defer func() { access.active = false }()
	return use(access)
}

// WithSelected resolves and revalidates all mapped workflows from confined files
// while the catalogue lock is held. Selection is offline and may be ambiguous.
func (a *CatalogueAccess) WithSelected(ctx context.Context, source, slug, version string, use func(Installed, fs.FS) error) error {
	return a.withSelected(ctx, source, slug, version, "", use)
}

// WithSelectedWorkflow validates and copies the requested workflow for fresh
// admission. Linking uses WithSelected to validate every mapped workflow.
func (a *CatalogueAccess) WithSelectedWorkflow(ctx context.Context, source, slug, version, workflow string, use func(Installed, fs.FS) error) error {
	if workflow == "" {
		return errors.New("workflow selection required")
	}
	return a.withSelected(ctx, source, slug, version, workflow, use)
}

func (a *CatalogueAccess) withSelected(ctx context.Context, source, slug, version, workflow string, use func(Installed, fs.FS) error) error {
	if !a.active || use == nil {
		return errors.New("inactive catalogue access or missing callback")
	}
	entry, err := a.manager.resolve(ctx, source, slug, version)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(entry.Path)
	if err != nil {
		return err
	}
	defer root.Close()
	files := &stablePackageFiles{ctx: ctx, source: confinedFS{root: root}, files: map[string][]byte{}, expected: map[string]string{}, editable: entry.Editable}
	for _, file := range entry.Release.Files {
		files.expected[file.Path] = file.SHA256
	}
	teamConfig, err := fs.ReadFile(files, "team.config")
	if err != nil {
		return err
	}
	copiedMap, err := parseConfig(teamConfig)
	if err != nil {
		return err
	}
	if !maps.Equal(copiedMap, entry.Workflows) {
		return errors.New("team.config changed during capture")
	}
	profile, err := captureStackProfile(files, entry)
	if err != nil {
		return err
	}
	if profile != nil {
		if slices.Contains(profile.OverrideFields, "guidance") {
			if err := captureOverrideInventory(ctx, root, files, entry); err != nil {
				return err
			}
		}
		if _, err := teamsource.CaptureStackGuidance(ctx, files, *profile); err != nil {
			return fmt.Errorf("%w: stack-profile.guidance: %w", ErrInvalidPackage, err)
		}
	}
	mapped := entry.Workflows
	if workflow != "" {
		mapped = map[string]string{}
		if graph, exists := entry.Workflows[workflow]; exists {
			mapped[workflow] = graph
		}
	}
	if err := a.manager.validate(ctx, files, mapped); err != nil {
		return errors.Join(ErrIncompatibleWorkflow, err)
	}
	if err := files.verify(); err != nil {
		return err
	}
	return use(entry, teamsource.RetainedFiles(files.files))
}

// stablePackageFiles reads each file once for validation, then checks the owned
// bytes against both the pinned inventory and a second filesystem observation.
type stablePackageFiles struct {
	ctx      context.Context
	source   fs.FS
	files    map[string][]byte
	expected map[string]string
	total    int
	editable bool
}

func (f *stablePackageFiles) Open(name string) (fs.File, error) {
	if err := f.ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := f.files[name]; !ok {
		raw, err := fs.ReadFile(f.source, name)
		if err != nil {
			return nil, err
		}
		if len(raw) > MaxReleaseBytes-f.total {
			return nil, fmt.Errorf("package capture bounds exceeded at %s", name)
		}
		sum := sha256.Sum256(raw)
		if expected, ok := f.expected[name]; !f.editable && (!ok || expected != hex.EncodeToString(sum[:])) {
			return nil, fmt.Errorf("package changed during capture at %s", name)
		}
		f.total += len(raw)
		f.files[name] = raw
	}
	return teamsource.RetainedFiles(f.files).Open(name)
}
func (f *stablePackageFiles) verify() error {
	names := make([]string, 0, len(f.files))
	for name := range f.files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := f.ctx.Err(); err != nil {
			return err
		}
		raw, err := fs.ReadFile(f.source, name)
		if err != nil {
			return err
		}
		if !bytes.Equal(raw, f.files[name]) {
			return fmt.Errorf("package changed during capture at %s", name)
		}
	}
	return nil
}

// IsLegacyProfileless identifies the historical Go releases whose original
// inventories predate stack profiles. No other team or version inherits Go.
func IsLegacyProfileless(identity Identity) bool {
	return identity.Slug == "go-development" && (identity.Version == "1.0.0" || identity.Version == "1.1.1")
}

func captureStackProfile(files fs.FS, entry Installed) (*config.StackProfile, error) {
	raw, err := fs.ReadFile(files, config.StackProfilePath)
	if err != nil {
		legacy := IsLegacyProfileless(entry.Identity)
		if entry.Editable {
			legacy = entry.Origin != nil && IsLegacyProfileless(Identity{Slug: entry.Origin.Slug, Version: entry.Origin.Version})
		}
		if errors.Is(err, fs.ErrNotExist) && legacy {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: stack-profile: required profile unavailable", ErrInvalidPackage)
	}
	profile, err := config.DecodeStackProfile(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: stack-profile: %w", ErrInvalidPackage, err)
	}
	return &profile, nil
}

// Permitted guidance overrides are resolved after repository bytes are admitted.
// Retain the complete verified inventory so that resolution never reopens disk.
func captureOverrideInventory(ctx context.Context, root *os.Root, files *stablePackageFiles, entry Installed) error {
	if !entry.Editable {
		for _, file := range entry.Release.Files {
			if _, err := fs.ReadFile(files, file.Path); err != nil {
				return err
			}
		}
		return nil
	}
	count := 0
	return fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		count++
		if !safePath(name) || count > MaxFiles {
			return errors.New("stack-profile.guidance: local inventory exceeds bounds or contains an unsafe path")
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("stack-profile.guidance: local inventory contains an unsafe file")
		}
		_, err = fs.ReadFile(files, name)
		return err
	})
}
