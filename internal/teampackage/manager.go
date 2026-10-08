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
	"net/http"
	"net/url"
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

// Options supplies machine paths, a TLS transport and mandatory workflow validation.
type Options struct {
	Paths     config.Paths
	Transport http.RoundTripper
	Validate  Validator
	// InUse is called under the catalogue lock before any removal mutation.
	// Application composition supplies all registered clone paths using the version.
	InUse func(context.Context, Identity) ([]string, error)
}

// Manager has no repository, runtime, credential or SQL dependency.
type Manager struct {
	paths      config.Paths
	client     *http.Client
	validate   Validator
	inUse      func(context.Context, Identity) ([]string, error)
	rename     func(string, string) error
	removeAll  func(string) error
	writeClone func(string, []byte) error
}

// New constructs an anonymous bounded package manager.
func New(o Options) (*Manager, error) {
	if o.Paths.Home() == "." || o.Paths.Home() == "" || o.Validate == nil {
		return nil, errors.New("team manager requires machine paths and workflow validation")
	}
	transport := o.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client := &http.Client{Transport: transport, Timeout: time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many redirects")
		}
		normalized, err := config.NormalizeTeamSource(req.URL.String())
		if err != nil {
			return err
		}
		u, err := url.Parse(normalized)
		if err != nil {
			return err
		}
		if u.Host != via[0].URL.Host {
			return errors.New("cross-origin team redirect refused")
		}
		if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
			return errors.New("credentials refused")
		}
		return nil
	}}
	return &Manager{paths: o.Paths, client: client, validate: o.Validate, inUse: o.InUse, rename: os.Rename, removeAll: os.RemoveAll, writeClone: writeStagedFile}, nil
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
func (m *Manager) fetch(ctx context.Context, source string, limit int64) ([]byte, error) {
	normalized, err := config.NormalizeTeamSource(source)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(normalized, "file://") {
		return readSourceFile(normalized, limit)
	}
	//nolint:gosec // Anonymous HTTPS sources are explicitly user-selected; normalization and redirect policy prohibit credentials and origin changes.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid request", ErrTransport)
	}
	//nolint:gosec // This performs the intended anonymous fetch from the validated catalogue origin.
	response, err := m.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %w", ErrTransport, ctx.Err())
		}
		return nil, fmt.Errorf("%w: request failed", ErrTransport)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrTransport, response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("%w: response exceeds size limit", ErrTransport)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading response failed", ErrTransport)
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%w: response exceeds size limit", ErrTransport)
	}
	return raw, nil
}

// readSourceFile reads one catalogue or release file of a file:// source.
func readSourceFile(normalized string, limit int64) ([]byte, error) {
	u, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid file source", ErrTransport)
	}
	//nolint:gosec // The operator selects this file; normalization requires an absolute path without traversal segments.
	f, err := os.Open(filepath.FromSlash(u.Path))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTransport, err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading file failed", ErrTransport)
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%w: file exceeds size limit", ErrTransport)
	}
	return raw, nil
}

func (m *Manager) catalogue(ctx context.Context, source string) (Catalogue, error) {
	raw, err := m.fetch(ctx, source, MaxCatalogueBytes)
	if err != nil {
		return Catalogue{}, err
	}
	return parseCatalogue(raw)
}
func selectRelease(c Catalogue, slug, ver string) (Release, error) {
	if !slugPattern.MatchString(slug) {
		return Release{}, fmt.Errorf("%w: invalid slug", ErrInvalidPackage)
	}
	if ver != "" {
		if _, err := version(ver); err != nil {
			return Release{}, err
		}
	}
	var chosen Release
	for _, r := range c.Releases {
		if r.Slug != slug {
			continue
		}
		if ver != "" {
			if r.Version == ver {
				return r, nil
			}
			continue
		}
		parts, _ := version(r.Version)
		if parts[4] != "" {
			continue
		}
		if chosen.Version == "" || compare(r.Version, chosen.Version) > 0 || (compare(r.Version, chosen.Version) == 0 && r.Version > chosen.Version) {
			chosen = r
		}
	}
	if chosen.Version == "" {
		return chosen, fmt.Errorf("%w: %s %s", ErrNotFound, slug, ver)
	}
	return chosen, nil
}

// Install verifies a complete release before publishing it beside existing versions.
func (m *Manager) Install(ctx context.Context, source, slug, ver string) (Installed, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	normalized, err := config.NormalizeTeamSource(source)
	if err != nil {
		return Installed{}, false, err
	}
	c, err := m.catalogue(ctx, normalized)
	if err != nil {
		return Installed{}, false, err
	}
	r, err := selectRelease(c, slug, ver)
	if err != nil {
		return Installed{}, false, err
	}
	return m.install(ctx, normalized, r)
}
func (m *Manager) install(ctx context.Context, source string, r Release) (Installed, bool, error) {
	if err := ensureDir(m.paths.TeamsStagingDir()); err != nil {
		return Installed{}, false, err
	}
	stage, err := os.MkdirTemp(m.paths.TeamsStagingDir(), "install-")
	if err != nil {
		return Installed{}, false, err
	}
	defer os.RemoveAll(stage)
	content := filepath.Join(stage, "content")
	if err := ensureDir(content); err != nil {
		return Installed{}, false, err
	}
	catalogueURL, err := url.Parse(source)
	if err != nil {
		return Installed{}, false, err
	}
	base := catalogueURL.ResolveReference(&url.URL{Path: "./"})
	total := 0
	for _, f := range r.Files {
		if err := ctx.Err(); err != nil {
			return Installed{}, false, err
		}
		fileURL := base.ResolveReference(&url.URL{Path: r.Download + "/" + f.Path})
		limit := teamsource.MaxSourceBytes
		if f.Path == "team.config" {
			limit = MaxCatalogueBytes
		}
		if int64(MaxReleaseBytes-total) < limit {
			limit = int64(MaxReleaseBytes - total)
		}
		raw, err := m.fetch(ctx, fileURL.String(), limit)
		if err != nil {
			return Installed{}, false, fmt.Errorf("%s/%s %s: %w", r.Slug, r.Version, f.Path, err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			return Installed{}, false, fmt.Errorf("%w: %s checksum mismatch", ErrInvalidPackage, f.Path)
		}
		total += len(raw)
		target := filepath.Join(content, filepath.FromSlash(f.Path))
		if err := ensureDir(filepath.Dir(target)); err != nil {
			return Installed{}, false, err
		}
		if err := writeStagedFile(target, raw); err != nil {
			return Installed{}, false, err
		}
	}
	raw, err := readLocal(filepath.Join(content, "team.config"), MaxCatalogueBytes)
	if err != nil {
		return Installed{}, false, err
	}
	workflows, err := parseConfig(raw)
	if err != nil {
		return Installed{}, false, err
	}
	if err := m.validate(ctx, os.DirFS(content), workflows); err != nil {
		return Installed{}, false, fmt.Errorf("%w: %w", ErrIncompatibleWorkflow, err)
	}
	// Verify the actual filesystem representation too: case or Unicode aliases
	// must not turn a pinned inventory into overwritten or missing local files.
	workflows, err = verifyContent(ctx, content, r)
	if err != nil {
		return Installed{}, false, fmt.Errorf("%w: %w", ErrInvalidPackage, err)
	}
	rec := receipt{Schema: 1, Source: source, Release: r, Checksum: r.Checksum}
	record, err := json.Marshal(rec)
	if err != nil {
		return Installed{}, false, err
	}
	if len(record) > MaxCatalogueBytes {
		return Installed{}, false, fmt.Errorf("%w: receipt too large", ErrInvalidPackage)
	}
	if err := os.WriteFile(filepath.Join(stage, "receipt.json"), record, 0o600); err != nil {
		return Installed{}, false, err
	}
	unlock, err := m.lock(ctx)
	if err != nil {
		return Installed{}, false, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return Installed{}, false, err
	}
	if local := m.destination("local", r.Slug, r.Version); local != m.destination(source, r.Slug, r.Version) {
		if _, err := os.Lstat(local); err == nil {
			return Installed{}, false, fmt.Errorf("%w: local team already exists at %s", ErrConflict, local)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Installed{}, false, err
		}
	}
	dest := m.destination(source, r.Slug, r.Version)
	if _, err := os.Lstat(dest); err == nil {
		installed, err := m.inspectPath(ctx, dest)
		if err != nil {
			return Installed{}, false, fmt.Errorf("%w: %w", ErrConflict, err)
		}
		if installed.Identity.Digest != r.Checksum {
			return Installed{}, false, fmt.Errorf("%w: %s checksum changed", ErrConflict, dest)
		}
		return installed, true, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Installed{}, false, err
	}
	if err := ensureDir(filepath.Dir(dest)); err != nil {
		return Installed{}, false, err
	}
	if err := m.rename(stage, dest); err != nil {
		return Installed{}, false, err
	}
	installed := Installed{Identity: Identity{Source: source, Slug: r.Slug, Version: r.Version, Digest: r.Checksum}, Release: r, Path: filepath.Join(dest, "content"), Workflows: workflows, Integrity: true}
	return installed, false, nil
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
	var rec receipt
	if err := strictJSON(raw, &rec); err != nil {
		return fail(err)
	}
	source, err := config.NormalizeTeamSource(rec.Source)
	if err != nil {
		return fail(err)
	}
	if rec.Source == "" || source != rec.Source || rec.Schema != 1 || rec.Checksum != rec.Release.Checksum || m.destination(source, rec.Release.Slug, rec.Release.Version) != dir {
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
	return Installed{Identity: Identity{Source: source, Slug: rec.Release.Slug, Version: rec.Release.Version, Digest: rec.Checksum}, Release: rec.Release, Path: content, Workflows: workflows, Integrity: true}, nil
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
	if _, err := version(ver); err != nil {
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
func (m *Manager) Update(ctx context.Context, source, slug string) (Installed, bool, error) {
	if source == "local" {
		return Installed{}, false, fmt.Errorf("%w: local teams are editable and cannot be updated", ErrInvalidPackage)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	normalized, err := config.NormalizeTeamSource(source)
	if err != nil {
		return Installed{}, false, err
	}
	all, err := m.List(ctx)
	if err != nil {
		return Installed{}, false, err
	}
	var baseline Installed
	for _, entry := range all {
		if entry.Identity.Source != normalized || entry.Identity.Slug != slug {
			continue
		}
		parts, _ := version(entry.Identity.Version)
		if parts[4] != "" {
			continue
		}
		if baseline.Identity.Version == "" || compare(entry.Identity.Version, baseline.Identity.Version) > 0 {
			baseline = entry
		}
	}
	if baseline.Identity.Version == "" {
		return Installed{}, false, errors.New("no installed stable baseline; use team install with an explicit version")
	}
	c, err := m.catalogue(ctx, normalized)
	if err != nil {
		return Installed{}, false, err
	}
	r, err := selectRelease(c, slug, "")
	if errors.Is(err, ErrNotFound) {
		return baseline, false, nil
	}
	if err != nil {
		return Installed{}, false, err
	}
	if compare(r.Version, baseline.Identity.Version) <= 0 {
		return baseline, false, nil
	}
	entry, _, err := m.install(ctx, normalized, r)
	return entry, err == nil, err
}

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
