package teampackage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// Validator validates every mapped workflow against factory-owned specifications.
type Validator func(context.Context, fs.FS, map[string]string) error

// Rebind records a reinstalled release's new digest on every repository bound
// to it. It runs under the catalogue lock and calls swap, which replaces the
// installed folder, inside its own transaction: a swap error rolls the binding
// update back, and any error after a successful swap makes the manager restore
// the previous folder.
type Rebind func(ctx context.Context, previous, next Identity, swap func() error) error

// Options supplies machine paths and mandatory workflow validation.
type Options struct {
	Paths    config.Paths
	Validate Validator
	// InUse is called under the catalogue lock before any removal mutation.
	// Application composition supplies all registered clone paths using the version.
	InUse func(context.Context, Identity) ([]string, error)
	// Rebind is called under the catalogue lock when an install replaces an
	// installed release with different bytes. Nil only replaces the folder.
	Rebind Rebind
}

// Manager has no repository, runtime, credential or SQL dependency.
type Manager struct {
	paths      config.Paths
	validate   Validator
	inUse      func(context.Context, Identity) ([]string, error)
	rebind     Rebind
	rename     func(string, string) error
	removeAll  func(string) error
	writeClone func(string, []byte) error
}

// New constructs a bounded local package manager.
func New(o Options) (*Manager, error) {
	if o.Paths.Home() == "." || o.Paths.Home() == "" || o.Validate == nil {
		return nil, errors.New("team manager requires machine paths and workflow validation")
	}
	return &Manager{paths: o.Paths, validate: o.Validate, inUse: o.InUse, rebind: o.Rebind, rename: os.Rename, removeAll: os.RemoveAll, writeClone: writeStagedFile}, nil
}
func sourceID(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}
func (m *Manager) destination(source, slug, ver string) string {
	namespace := sourceID(source)
	if source == "local" {
		namespace = "local"
	}
	return filepath.Join(m.paths.TeamsInstalledDir(), namespace, slug, ver)
}
func ordinaryComponents(name string, directory bool) error {
	abs, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(abs, string(filepath.Separator)), string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		last := i == len(parts)-1
		if (!last || directory) && !info.IsDir() {
			return fmt.Errorf("unsafe directory %s", current)
		}
		if last && !directory && !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe ordinary file %s", current)
		}
	}
	return nil
}
func ensureDir(name string) error {
	if err := ordinaryComponents(name, true); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(name)
	if parent == name {
		return errors.New("invalid root")
	}
	if err := ensureDir(parent); err != nil {
		return err
	}
	if err := os.Mkdir(name, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return ordinaryComponents(name, true)
}
func readLocal(name string, limit int64) ([]byte, error) {
	if err := ordinaryComponents(name, false); err != nil {
		return nil, err
	}
	//nolint:gosec // Path components have been checked as ordinary confined files above.
	file, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("unsafe or oversized file %s", name)
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("oversized file %s", name)
	}
	return raw, nil
}
func (m *Manager) lock(ctx context.Context) (func(), error) {
	if err := ensureDir(m.paths.TeamsDir()); err != nil {
		return nil, err
	}
	name := m.paths.TeamsLockPath()
	if _, err := os.Lstat(name); err == nil {
		if err := ordinaryComponents(name, false); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	fd, err := syscall.Open(name, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("unsafe catalogue lock %s", name)
	}
	current, err := os.Lstat(name)
	if err != nil || !os.SameFile(info, current) {
		_ = file.Close()
		return nil, fmt.Errorf("catalogue lock changed at %s", name)
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); _ = file.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = file.Close()
			return nil, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// InstallResult says what an install did.
type InstallResult int

// The outcomes of InstallBuilt.
const (
	// InstallNew is a release that was not installed before.
	InstallNew InstallResult = iota
	// InstallUnchanged is a release whose installed bytes already match.
	InstallUnchanged
	// InstallReplaced is a release whose installed bytes were replaced in place.
	InstallReplaced
)

// InstallBuilt verifies built files against their inventory and installs them
// under source. Installing the same source, name and version again with other
// bytes replaces the installed folder in place, under the catalogue lock, and
// rebinds linked repositories through Options.Rebind. Saved workflow captures
// keep in-flight work on the bytes it started with.
func (m *Manager) InstallBuilt(ctx context.Context, source string, files map[string][]byte, r Release, build BuildInfo) (Installed, InstallResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	normalized, err := config.NormalizeTeamSource(source)
	if err != nil {
		return Installed{}, InstallUnchanged, err
	}
	if err := validateRelease(r); err != nil {
		return Installed{}, InstallUnchanged, err
	}
	if len(files) != len(r.Files) {
		return Installed{}, InstallUnchanged, fmt.Errorf("%w: built files do not match the inventory", ErrInvalidPackage)
	}
	return m.install(ctx, normalized, files, r, build)
}

func (m *Manager) install(ctx context.Context, source string, files map[string][]byte, r Release, build BuildInfo) (Installed, InstallResult, error) {
	if err := ensureDir(m.paths.TeamsStagingDir()); err != nil {
		return Installed{}, InstallUnchanged, err
	}
	stage, err := os.MkdirTemp(m.paths.TeamsStagingDir(), "install-")
	if err != nil {
		return Installed{}, InstallUnchanged, err
	}
	defer os.RemoveAll(stage)
	content := filepath.Join(stage, "content")
	if err := ensureDir(content); err != nil {
		return Installed{}, InstallUnchanged, err
	}
	total := 0
	for _, f := range r.Files {
		if err := ctx.Err(); err != nil {
			return Installed{}, InstallUnchanged, err
		}
		raw, ok := files[f.Path]
		if !ok {
			return Installed{}, InstallUnchanged, fmt.Errorf("%w: %s missing from the built files", ErrInvalidPackage, f.Path)
		}
		limit := teamsource.MaxSourceBytes
		if f.Path == "team.config" {
			limit = MaxCatalogueBytes
		}
		total += len(raw)
		if int64(len(raw)) > limit || total > MaxReleaseBytes {
			return Installed{}, InstallUnchanged, fmt.Errorf("%w: %s exceeds the size limit", ErrInvalidPackage, f.Path)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			return Installed{}, InstallUnchanged, fmt.Errorf("%w: %s checksum mismatch", ErrInvalidPackage, f.Path)
		}
		target := filepath.Join(content, filepath.FromSlash(f.Path))
		if err := ensureDir(filepath.Dir(target)); err != nil {
			return Installed{}, InstallUnchanged, err
		}
		if err := writeStagedFile(target, raw); err != nil {
			return Installed{}, InstallUnchanged, err
		}
	}
	raw, err := readLocal(filepath.Join(content, "team.config"), MaxCatalogueBytes)
	if err != nil {
		return Installed{}, InstallUnchanged, err
	}
	workflows, err := parseConfig(raw)
	if err != nil {
		return Installed{}, InstallUnchanged, err
	}
	profile, err := captureStackProfile(os.DirFS(content), Installed{Identity: Identity{Slug: r.Slug, Version: r.Version}})
	if err != nil {
		return Installed{}, InstallUnchanged, err
	}
	if profile != nil {
		if _, err := teamsource.CaptureStackGuidance(ctx, os.DirFS(content), *profile); err != nil {
			return Installed{}, InstallUnchanged, fmt.Errorf("%w: stack-profile.guidance: %w", ErrInvalidPackage, err)
		}
	}
	if err := m.validate(ctx, os.DirFS(content), workflows); err != nil {
		return Installed{}, InstallUnchanged, fmt.Errorf("%w: %w", ErrIncompatibleWorkflow, err)
	}
	// Verify the actual filesystem representation too: case or Unicode aliases
	// must not turn a pinned inventory into overwritten or missing local files.
	workflows, err = verifyContent(ctx, content, r)
	if err != nil {
		return Installed{}, InstallUnchanged, fmt.Errorf("%w: %w", ErrInvalidPackage, err)
	}
	rec := builtReceipt{Schema: builtReceiptSchema, Source: source, Release: r, Checksum: r.Checksum, Build: build}
	record, err := json.Marshal(rec)
	if err != nil {
		return Installed{}, InstallUnchanged, err
	}
	if len(record) > MaxCatalogueBytes {
		return Installed{}, InstallUnchanged, fmt.Errorf("%w: receipt too large", ErrInvalidPackage)
	}
	if err := os.WriteFile(filepath.Join(stage, "receipt.json"), record, 0o600); err != nil {
		return Installed{}, InstallUnchanged, err
	}
	unlock, err := m.lock(ctx)
	if err != nil {
		return Installed{}, InstallUnchanged, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return Installed{}, InstallUnchanged, err
	}
	if local := m.destination("local", r.Slug, r.Version); local != m.destination(source, r.Slug, r.Version) {
		if _, err := os.Lstat(local); err == nil {
			return Installed{}, InstallUnchanged, fmt.Errorf("%w: local team already exists at %s", ErrConflict, local)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Installed{}, InstallUnchanged, err
		}
	}
	next := Identity{Source: source, Slug: r.Slug, Version: r.Version, Digest: r.Checksum}
	installed := Installed{Identity: next, Release: r, Path: filepath.Join(m.destination(source, r.Slug, r.Version), "content"), Workflows: workflows, Integrity: true, Build: &build}
	dest := m.destination(source, r.Slug, r.Version)
	if _, err := os.Lstat(dest); err == nil {
		return m.replace(ctx, stage, dest, next, installed)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Installed{}, InstallUnchanged, err
	}
	if err := ensureDir(filepath.Dir(dest)); err != nil {
		return Installed{}, InstallUnchanged, err
	}
	if err := m.rename(stage, dest); err != nil {
		return Installed{}, InstallUnchanged, err
	}
	return installed, InstallNew, nil
}

// replace swaps a staged release into an existing destination. The caller
// holds the catalogue lock. Equal bytes leave everything as it is. An existing
// folder that no longer verifies is replaced too, so a reinstall repairs it.
func (m *Manager) replace(ctx context.Context, stage, dest string, next Identity, installed Installed) (Installed, InstallResult, error) {
	if err := ordinaryComponents(dest, true); err != nil {
		return Installed{}, InstallUnchanged, fmt.Errorf("%w: %w", ErrConflict, err)
	}
	previous := Identity{Source: next.Source, Slug: next.Slug, Version: next.Version}
	if current, err := m.inspectPath(ctx, dest); err == nil {
		if current.Identity.Digest == next.Digest {
			return current, InstallUnchanged, nil
		}
		previous = current.Identity
	}
	if err := ensureDir(m.paths.TeamsTrashDir()); err != nil {
		return Installed{}, InstallUnchanged, err
	}
	trash, err := os.MkdirTemp(m.paths.TeamsTrashDir(), "replace-")
	if err != nil {
		return Installed{}, InstallUnchanged, err
	}
	old := filepath.Join(trash, "release")
	swapped := false
	swap := func() error {
		if swapped {
			return errors.New("installed release already replaced")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := m.rename(dest, old); err != nil {
			return err
		}
		if err := m.rename(stage, dest); err != nil {
			return errors.Join(err, m.rename(old, dest))
		}
		swapped = true
		return nil
	}
	if m.rebind != nil {
		err = m.rebind(ctx, previous, next, swap)
	} else {
		err = swap()
	}
	if err != nil {
		if swapped {
			// Put the previous release back so bindings and bytes agree again.
			if restore := errors.Join(m.rename(dest, stage), m.rename(old, dest)); restore != nil {
				return Installed{}, InstallUnchanged, fmt.Errorf("%w; restoring the previous release failed (kept in %s): %w", err, trash, restore)
			}
		}
		_ = os.RemoveAll(trash)
		return Installed{}, InstallUnchanged, err
	}
	if err := m.removeAll(trash); err != nil {
		return installed, InstallReplaced, fmt.Errorf("replaced; cleanup remaining at %s: %w", trash, err)
	}
	return installed, InstallReplaced, nil
}
func (m *Manager) inspectPath(ctx context.Context, dir string) (Installed, error) {
	fail := func(err error) (Installed, error) {
		return Installed{}, fmt.Errorf("%w: installed release %s: %w", ErrInvalidPackage, dir, err)
	}
	raw, err := readLocal(filepath.Join(dir, "receipt.json"), MaxCatalogueBytes)
	if err != nil {
		return fail(err)
	}
	if filepath.Base(filepath.Dir(filepath.Dir(dir))) == "local" {
		return m.inspectLocal(ctx, dir, raw)
	}
	rec, err := decodeReceipt(raw)
	if err != nil {
		return fail(err)
	}
	source, err := config.NormalizeTeamSource(rec.Source)
	if err != nil {
		return fail(err)
	}
	if rec.Source == "" || source != rec.Source || rec.Checksum != rec.Release.Checksum || m.destination(source, rec.Release.Slug, rec.Release.Version) != dir {
		return fail(errors.New("receipt identity does not match directory"))
	}
	if err := validateRelease(rec.Release); err != nil {
		return fail(err)
	}
	content := filepath.Join(dir, "content")
	workflows, err := verifyContent(ctx, content, rec.Release)
	if err != nil {
		return fail(err)
	}
	return Installed{Identity: Identity{Source: source, Slug: rec.Release.Slug, Version: rec.Release.Version, Digest: rec.Checksum}, Release: rec.Release, Path: content, Workflows: workflows, Integrity: true, Build: rec.build()}, nil
}
func (m *Manager) listLocked(ctx context.Context) ([]Installed, error) {
	root := m.paths.TeamsInstalledDir()
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return []Installed{}, nil
	}
	if err := ordinaryComponents(root, true); err != nil {
		return nil, err
	}
	var result []Installed
	var faults []error
	sources, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		sourcePath := filepath.Join(root, source.Name())
		if err := ordinaryComponents(sourcePath, true); err != nil {
			faults = append(faults, err)
			continue
		}
		slugs, err := os.ReadDir(sourcePath)
		if err != nil {
			faults = append(faults, err)
			continue
		}
		for _, slug := range slugs {
			slugPath := filepath.Join(sourcePath, slug.Name())
			if err := ordinaryComponents(slugPath, true); err != nil {
				faults = append(faults, err)
				continue
			}
			versions, err := os.ReadDir(slugPath)
			if err != nil {
				faults = append(faults, err)
				continue
			}
			for _, ver := range versions {
				dir := filepath.Join(slugPath, ver.Name())
				installed, err := m.inspectPath(ctx, dir)
				if err != nil {
					faults = append(faults, err)
					continue
				}
				result = append(result, installed)
			}
		}
	}
	slices.SortFunc(result, func(a, b Installed) int {
		if c := strings.Compare(a.Identity.Source, b.Identity.Source); c != 0 {
			return c
		}
		if c := strings.Compare(a.Identity.Slug, b.Identity.Slug); c != 0 {
			return c
		}
		return strings.Compare(a.Identity.Version, b.Identity.Version)
	})
	return result, errors.Join(faults...)
}

// List verifies and returns all local sources deterministically, without fetching.
func (m *Manager) List(ctx context.Context) ([]Installed, error) {
	unlock, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return m.listLocked(ctx)
}
func (m *Manager) resolve(ctx context.Context, source, slug, ver string) (Installed, error) {
	if !slugPattern.MatchString(slug) {
		return Installed{}, fmt.Errorf("%w: invalid slug", ErrInvalidPackage)
	}
	if err := version(ver); err != nil {
		return Installed{}, err
	}
	if source != "" {
		normalized, err := normalizeSelector(source)
		if err != nil {
			return Installed{}, err
		}
		dest := m.destination(normalized, slug, ver)
		if _, err := os.Lstat(dest); errors.Is(err, fs.ErrNotExist) {
			return Installed{}, fmt.Errorf("%w: %s from %s", ErrNotFound, dest, normalized)
		}
		return m.inspectPath(ctx, dest)
	}
	all, err := m.listLocked(ctx)
	if err != nil {
		return Installed{}, err
	}
	var matches []Installed
	var sources []string
	for _, entry := range all {
		if entry.Identity.Slug == slug && entry.Identity.Version == ver {
			matches = append(matches, entry)
			sources = append(sources, entry.Identity.Source)
		}
	}
	if len(matches) == 0 {
		return Installed{}, fmt.Errorf("%w: %s %s", ErrNotFound, slug, ver)
	}
	if len(matches) > 1 {
		return Installed{}, fmt.Errorf("ambiguous installed release %s %s; select --source from %s", slug, ver, strings.Join(sources, ", "))
	}
	return matches[0], nil
}

// Inspect verifies a selected release under the shared mutation lock.
func (m *Manager) Inspect(ctx context.Context, source, slug, ver string) (Installed, error) {
	unlock, err := m.lock(ctx)
	if err != nil {
		return Installed{}, err
	}
	defer unlock()
	return m.resolve(ctx, source, slug, ver)
}

// WithExact exposes verified content only while the removal coordination lock is held.
func (m *Manager) WithExact(ctx context.Context, id Identity, use func(Installed, fs.FS) error) error {
	if use == nil {
		return errors.New("exact release callback required")
	}
	if id.Source == "" {
		return fmt.Errorf("%w: exact release identity requires its source", ErrInvalidPackage)
	}
	unlock, err := m.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	entry, err := m.resolve(ctx, id.Source, id.Slug, id.Version)
	if err != nil {
		return err
	}
	if entry.Identity.Digest != id.Digest {
		return ErrConflict
	}
	root, err := os.OpenRoot(entry.Path)
	if err != nil {
		return err
	}
	defer root.Close()
	return use(entry, confinedFS{root: root})
}

// Update installs the highest newer stable version from the same source.

// Remove detaches a selected release atomically, then cleans only its trash tree.
// A true result with an error means removal occurred and cleanup remains.
func (m *Manager) Remove(ctx context.Context, source, slug, ver string) (bool, error) {
	unlock, err := m.lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	entry, err := m.resolve(ctx, source, slug, ver)
	if err != nil {
		return false, err
	}
	if m.inUse != nil {
		paths, err := m.inUse(ctx, entry.Identity)
		if err != nil {
			return false, fmt.Errorf("cannot check repository team bindings: %w", err)
		}
		if len(paths) > 0 {
			paths = slices.Clone(paths)
			slices.Sort(paths)
			return false, fmt.Errorf("team %s %s from %s is linked to repositories: %s; unlink them before removal", entry.Identity.Slug, entry.Identity.Version, entry.Identity.Source, strings.Join(paths, ", "))
		}
	}
	if err := ensureDir(m.paths.TeamsTrashDir()); err != nil {
		return false, err
	}
	trash, err := os.MkdirTemp(m.paths.TeamsTrashDir(), "remove-")
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(trash)
		return false, err
	}
	target := filepath.Join(trash, "release")
	if err := m.rename(filepath.Dir(entry.Path), target); err != nil {
		_ = os.Remove(trash)
		return false, err
	}
	if err := m.removeAll(trash); err != nil {
		return true, fmt.Errorf("removed; cleanup remaining at %s: %w", trash, err)
	}
	return true, nil
}

func verifyContent(ctx context.Context, content string, release Release) (map[string]string, error) {
	if err := ordinaryComponents(content, true); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	total := 0
	for _, f := range release.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		limit := teamsource.MaxSourceBytes
		if f.Path == "team.config" {
			limit = MaxCatalogueBytes
		}
		raw, err := readLocal(filepath.Join(content, filepath.FromSlash(f.Path)), limit)
		if err != nil {
			return nil, err
		}
		total += len(raw)
		sum := sha256.Sum256(raw)
		if total > MaxReleaseBytes || hex.EncodeToString(sum[:]) != f.SHA256 {
			return nil, fmt.Errorf("%s content integrity failed", f.Path)
		}
		seen[f.Path] = true
	}
	if err := filepath.WalkDir(content, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(content, p)
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || !seen[filepath.ToSlash(relative)] {
			return fmt.Errorf("unsafe or uninventoried content %s", p)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	raw, err := readLocal(filepath.Join(content, "team.config"), MaxCatalogueBytes)
	if err != nil {
		return nil, err
	}
	workflows, err := parseConfig(raw)
	if err != nil {
		return nil, err
	}
	return workflows, nil
}

func writeStagedFile(name string, raw []byte) error {
	//nolint:gosec // Inventory paths and parent directories are checked beneath the private staging root; exclusive creation also rejects filesystem aliases.
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("%w: cannot create staged file %s: %w", ErrInvalidPackage, name, err)
	}
	_, writeErr := file.Write(raw)
	return errors.Join(writeErr, file.Close())
}
