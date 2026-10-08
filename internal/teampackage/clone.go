package teampackage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// Origin is permanent history of the immediate source's bytes at clone time.
type Origin struct {
	Source   string    `json:"source"`
	Slug     string    `json:"slug"`
	Version  string    `json:"version"`
	Checksum string    `json:"checksum"`
	ClonedAt time.Time `json:"cloned_at"`
}

type localReceipt struct {
	Schema  int    `json:"schema"`
	Kind    string `json:"kind"`
	Slug    string `json:"slug"`
	Version string `json:"version"`
	Origin  Origin `json:"origin"`
}

func normalizeSelector(source string) (string, error) {
	if source == "local" {
		return source, nil
	}
	return config.NormalizeTeamSource(source)
}

// scanLocal validates editable bytes without comparing them to historical hashes.
// Keeping a bounded snapshot ensures the copied bytes and origin checksum agree.
func scanLocal(ctx context.Context, content string) (map[string][]byte, []string, []File, map[string]string, error) {
	files := map[string][]byte{}
	var dirs []string
	var inventory []File
	total := 0
	if err := ordinaryComponents(content, true); err != nil {
		return nil, nil, nil, nil, err
	}
	err := filepath.WalkDir(content, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == content {
			return nil
		}
		rel, err := filepath.Rel(content, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !safePath(rel) {
			return fmt.Errorf("unsafe content path %s", rel)
		}
		if d.IsDir() {
			dirs = append(dirs, rel)
			return nil
		}
		if !d.Type().IsRegular() || len(files) >= MaxFiles {
			return fmt.Errorf("unsafe file or file count exceeded: %s", rel)
		}
		limit := teamsource.MaxSourceBytes
		if rel == "team.config" {
			limit = MaxCatalogueBytes
		}
		if int64(MaxReleaseBytes-total) < limit {
			limit = int64(MaxReleaseBytes - total)
		}
		raw, err := readLocal(p, limit)
		if err != nil {
			return err
		}
		total += len(raw)
		sum := sha256.Sum256(raw)
		inventory = append(inventory, File{Path: rel, SHA256: hex.EncodeToString(sum[:])})
		files[rel] = raw
		return nil
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%w: %w", ErrInvalidPackage, err)
	}
	workflows, err := parseConfig(files["team.config"])
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for _, graph := range workflows {
		if _, ok := files[graph]; !ok {
			return nil, nil, nil, nil, fmt.Errorf("%w: missing workflow %s", ErrInvalidPackage, graph)
		}
	}
	return files, dirs, inventory, workflows, nil
}

func (m *Manager) inspectLocal(ctx context.Context, dir string, raw []byte) (Installed, error) {
	var rec localReceipt
	if err := strictJSON(raw, &rec); err != nil {
		return Installed{}, fmt.Errorf("%w: local receipt: %w", ErrInvalidPackage, err)
	}
	source, err := normalizeSelector(rec.Origin.Source)
	_, versionErr := version(rec.Version)
	_, originVersionErr := version(rec.Origin.Version)
	if err != nil || source != rec.Origin.Source || rec.Schema != 1 || rec.Kind != "local" || !slugPattern.MatchString(rec.Slug) || versionErr != nil || !slugPattern.MatchString(rec.Origin.Slug) || originVersionErr != nil || !hashPattern.MatchString(rec.Origin.Checksum) || rec.Origin.ClonedAt.IsZero() || m.destination("local", rec.Slug, rec.Version) != dir {
		return Installed{}, fmt.Errorf("%w: invalid local receipt identity or origin", ErrInvalidPackage)
	}
	content := filepath.Join(dir, "content")
	_, _, inventory, workflows, err := scanLocal(ctx, content)
	if err != nil {
		return Installed{}, err
	}
	return Installed{Identity: Identity{Source: "local", Slug: rec.Slug, Version: rec.Version, Digest: digest(inventory)}, Release: Release{Name: rec.Slug, Slug: rec.Slug, Version: rec.Version}, Path: content, Workflows: workflows, Editable: true, Origin: &rec.Origin}, nil
}

func (m *Manager) cloneDestinationFree(slug, ver string) error {
	root := m.paths.TeamsInstalledDir()
	if err := ordinaryComponents(root, true); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	namespaces, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, namespace := range namespaces {
		parent := filepath.Join(root, namespace.Name())
		if err := ordinaryComponents(parent, true); err != nil {
			return err
		}
		dest := filepath.Join(parent, slug, ver)
		if _, err := os.Lstat(dest); err == nil {
			return fmt.Errorf("%w: %s %s already exists at %s", ErrConflict, slug, ver, dest)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Clone publishes a writable independent snapshot while holding the same lock
// used by install and remove. Workflow execution and capture are never invoked.
func (m *Manager) Clone(ctx context.Context, source, slug, ver, newSlug, newVersion string) (Installed, error) {
	if !slugPattern.MatchString(newSlug) {
		return Installed{}, fmt.Errorf("%w: invalid destination slug", ErrInvalidPackage)
	}
	if _, err := version(newVersion); err != nil {
		return Installed{}, err
	}
	unlock, err := m.lock(ctx)
	if err != nil {
		return Installed{}, err
	}
	defer unlock()
	entry, err := m.resolve(ctx, source, slug, ver)
	if err != nil {
		return Installed{}, err
	}
	if err := m.cloneDestinationFree(newSlug, newVersion); err != nil {
		return Installed{}, err
	}
	files, dirs, inventory, workflows, err := scanLocal(ctx, entry.Path)
	if err != nil {
		return Installed{}, err
	}
	checksum := digest(inventory)
	if !entry.Editable && checksum != entry.Identity.Digest {
		return Installed{}, fmt.Errorf("%w: source changed during clone", ErrInvalidPackage)
	}
	if err := ensureDir(m.paths.TeamsStagingDir()); err != nil {
		return Installed{}, err
	}
	stage, err := os.MkdirTemp(m.paths.TeamsStagingDir(), "clone-")
	if err != nil {
		return Installed{}, err
	}
	defer os.RemoveAll(stage)
	content := filepath.Join(stage, "content")
	if err := ensureDir(content); err != nil {
		return Installed{}, err
	}
	for _, dir := range dirs {
		if err := ensureDir(filepath.Join(content, filepath.FromSlash(dir))); err != nil {
			return Installed{}, err
		}
	}
	for _, item := range inventory {
		if err := ctx.Err(); err != nil {
			return Installed{}, err
		}
		if err := m.writeClone(filepath.Join(content, filepath.FromSlash(item.Path)), files[item.Path]); err != nil {
			return Installed{}, err
		}
	}
	origin := Origin{Source: entry.Identity.Source, Slug: slug, Version: ver, Checksum: checksum, ClonedAt: time.Now().UTC()}
	rec := localReceipt{Schema: 1, Kind: "local", Slug: newSlug, Version: newVersion, Origin: origin}
	raw, err := json.Marshal(rec)
	if err != nil {
		return Installed{}, err
	}
	if err := m.writeClone(filepath.Join(stage, "receipt.json"), raw); err != nil {
		return Installed{}, err
	}
	if err := ctx.Err(); err != nil {
		return Installed{}, err
	}
	if err := m.cloneDestinationFree(newSlug, newVersion); err != nil {
		return Installed{}, err
	}
	dest := m.destination("local", newSlug, newVersion)
	if err := ensureDir(filepath.Dir(dest)); err != nil {
		return Installed{}, err
	}
	if err := m.rename(stage, dest); err != nil {
		return Installed{}, err
	}
	return Installed{Identity: Identity{Source: "local", Slug: newSlug, Version: newVersion, Digest: checksum}, Release: Release{Name: newSlug, Slug: newSlug, Version: newVersion}, Path: filepath.Join(dest, "content"), Workflows: workflows, Editable: true, Origin: &origin}, nil
}
