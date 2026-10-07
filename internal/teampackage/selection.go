package teampackage

import (
	"context"
	"errors"
	"io/fs"
	"os"
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
	files := confinedFS{root: root}
	if err := a.manager.validate(ctx, files, entry.Workflows); err != nil {
		return errors.Join(ErrIncompatibleWorkflow, err)
	}
	return use(entry, files)
}
