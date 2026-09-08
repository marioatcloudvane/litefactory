// Package teampublish builds independent immutable team releases. It reuses
// consumer validation and source capture rather than defining another format.
package teampublish

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/team"
	"github.com/cloudvane/litefactory/internal/teamremote"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// Release contains deterministic bytes; Build has no timestamp or app version.
type Release struct {
	Team     string
	Version  string
	Files    map[string][]byte
	Manifest []byte
}

// Build captures every mode under the catalog lock, retaining only the complete
// referenced closure. Source packages must contain exactly the selected team.
func Build(ctx context.Context, source, name, version string) (*Release, error) {
	if !team.KeyPattern.MatchString(name) {
		return nil, fmt.Errorf("invalid team name")
	}
	if err := teamremote.ValidateVersion(version); err != nil {
		return nil, err
	}
	r := &Release{Team: name, Version: version, Files: map[string][]byte{}}
	err := team.WithCatalogLock(ctx, source, func() error {
		for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
			s, err := teamsource.CaptureUnlocked(ctx, source, name, mode)
			if err != nil {
				return fmt.Errorf("capture %s: %w", mode, err)
			}
			for p, b := range s.Files {
				if old, ok := r.Files[p]; ok && team.HashBytes(old) != team.HashBytes(b) {
					return fmt.Errorf("source changed across mode capture: %s", p)
				}
				r.Files[p] = append([]byte(nil), b...)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	idx, err := team.Decode(r.Files["index.json"])
	if err != nil {
		return nil, err
	}
	if len(idx.Teams) != 1 {
		return nil, fmt.Errorf("publisher source must contain exactly one team")
	}
	m := teamremote.Manifest{SchemaVersion: teamremote.SchemaVersion, CatalogSchemaVersion: 2, Team: name, Version: version}
	paths := make([]string, 0, len(r.Files))
	for p := range r.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		b := r.Files[p]
		m.Files = append(m.Files, teamremote.File{Path: p, Size: int64(len(b)), SHA256: team.HashBytes(b)})
	}
	r.Manifest, err = encode(m)
	if err != nil {
		return nil, err
	}
	if _, err := teamremote.DecodeManifest(r.Manifest); err != nil {
		return nil, err
	}
	return r, nil
}

// Publish writes one release into an operator-selected publication directory.
// No network or app build occurs. An interrupted publication may leave an
// unreferenced immutable release, never an index pointing at a partial release.
// Existing versions are refused, including identical re-publication.
func Publish(ctx context.Context, source, output, name, version string) error {
	r, err := Build(ctx, source, name, version)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0o700); err != nil {
		return err
	}
	return team.WithCatalogLock(ctx, output, func() (err error) {
		root, err := os.OpenRoot(output)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, root.Close()) }()
		d := &teamremote.Discovery{SchemaVersion: teamremote.SchemaVersion, Teams: map[string]teamremote.TeamReleases{}}
		if err := safePath(root, "teams.json"); err != nil {
			return err
		}
		old, err := readIndex(root)
		if err == nil {
			d, err = teamremote.DecodeDiscovery(old)
			if err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		t := d.Teams[name]
		if t.Releases == nil {
			t.Releases = map[string]teamremote.Release{}
		}
		if _, exists := t.Releases[version]; exists {
			return fmt.Errorf("release %s/%s already exists; publish a new version", name, version)
		}
		if t.Stable != "" {
			c, err := teamremote.CompareVersions(version, t.Stable)
			if err != nil {
				return err
			}
			if c <= 0 {
				return fmt.Errorf("new stable release must advance the installed publication index")
			}
		}
		rel := path.Join("releases", name, version)
		if err := safePath(root, rel); err != nil {
			return err
		}
		if _, err := root.Lstat(rel); err == nil {
			return fmt.Errorf("immutable release directory already exists: %s", rel)
		} else if !os.IsNotExist(err) {
			return err
		}
		t.Stable = version
		t.Releases[version] = teamremote.Release{Manifest: path.Join(rel, "manifest.json"), SHA256: team.HashBytes(r.Manifest)}
		d.Teams[name] = t
		index, err := encode(d)
		if err != nil {
			return err
		}
		if _, err := teamremote.DecodeDiscovery(index); err != nil {
			return err
		}
		stage := ".publish-" + rand.Text()
		if err := root.Mkdir(stage, 0o700); err != nil {
			return err
		}
		defer func() { err = errors.Join(err, root.RemoveAll(stage)) }()
		paths := make([]string, 0, len(r.Files))
		for p := range r.Files {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := writeNew(root, path.Join(stage, p), r.Files[p]); err != nil {
				return err
			}
		}
		if err := writeNew(root, path.Join(stage, "manifest.json"), r.Manifest); err != nil {
			return err
		}
		if err := root.MkdirAll(path.Dir(rel), 0o700); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := root.Rename(stage, rel); err != nil {
			return err
		}
		if err := syncParents(root, rel); err != nil {
			return err
		}
		// The discovery replacement is deliberately the last observable write.
		tmp := ".index-" + rand.Text()
		if err := writeNew(root, tmp, index); err != nil {
			return err
		}
		defer func() {
			e := root.Remove(tmp)
			if !os.IsNotExist(e) {
				err = errors.Join(err, e)
			}
		}()
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := root.Rename(tmp, "teams.json"); err != nil {
			return err
		}
		return syncParents(root, "teams.json")
	})
}

func encode(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func readIndex(root *os.Root) (_ []byte, err error) {
	f, err := root.Open("teams.json")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("discovery must be a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, teamremote.MaxIndexBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > teamremote.MaxIndexBytes {
		return nil, fmt.Errorf("discovery exceeds size limit")
	}
	return b, nil
}

func safePath(root *os.Root, p string) error {
	parts := strings.Split(p, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("publication path contains a symlink")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("publication parent is not a directory")
		}
	}
	return nil
}

func writeNew(root *os.Root, p string, b []byte) (err error) {
	if err := root.MkdirAll(path.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := root.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err := f.Write(b); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return syncParents(root, p)
}

// Directory synchronization makes payload renames durable before the stable
// pointer can become durable, not merely ordered in a running process.
func syncParents(root *os.Root, p string) error {
	for p = path.Dir(p); ; p = path.Dir(p) {
		f, err := root.Open(p)
		if err != nil {
			return err
		}
		err = errors.Join(f.Sync(), f.Close())
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
	}
}
