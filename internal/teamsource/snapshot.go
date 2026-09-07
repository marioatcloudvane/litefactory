package teamsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/render"
	"github.com/cloudvane/litefactory/internal/team"
	"gopkg.in/yaml.v3"
)

// MaxSourceBytes bounds one source or resource read.
const MaxSourceBytes int64 = 1 << 20

// MaxSnapshotBytes bounds all source and resource bytes in one capture.
const MaxSnapshotBytes = 16 << 20

// CapturedAgent pairs registry metadata with its verified source.
type CapturedAgent struct {
	Definition team.AgentEntry
	Source     render.ResolvedSource
}

// Snapshot owns its bytes; files retain catalog-relative resource trees so
// references in skill Markdown work after delivery to a CLI.
type Snapshot struct {
	Team   string
	Mode   string
	Agents []CapturedAgent
	Files  map[string][]byte
	Hashes map[string]string
	Digest string
}

// Capture snapshots one centrally installed team's mode and dependency closure.
func Capture(ctx context.Context, paths config.Paths, name, mode string) (*Snapshot, error) {
	return CaptureRoot(ctx, paths.TeamsDir(), name, mode)
}

// CaptureRoot locks and snapshots a catalog root, including import candidates.
func CaptureRoot(ctx context.Context, root, name, mode string) (snapshot *Snapshot, err error) {
	err = team.WithCatalogLock(ctx, root, func() error {
		var e error
		snapshot, e = CaptureUnlocked(ctx, root, name, mode)
		return e
	})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

// CaptureUnlocked lets management preflight a catalog while already holding
// the same management lock. Callers must serialize all mutations until return.
func CaptureUnlocked(ctx context.Context, root, name, mode string) (*Snapshot, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open source catalog: %w", err)
	}
	defer r.Close()
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	c := captureReader{ctx: ctx, root: r, base: canonical, files: map[string][]byte{}, hashes: map[string]string{}, skills: map[string]int{}}
	index, err := c.read("index.json")
	if err != nil {
		return nil, err
	}
	idx, err := team.Decode(index)
	if err != nil {
		return nil, err
	}
	roster, err := idx.Roster(name, mode)
	if err != nil {
		return nil, err
	}
	agents := make([]CapturedAgent, 0, len(roster))
	var errs []error
	for _, a := range roster {
		portable, e := c.relative(a.Path)
		if e != nil {
			errs = append(errs, e)
			continue
		}
		a.Path = portable
		raw, e := c.read(a.Path)
		if e != nil {
			errs = append(errs, e)
			continue
		}
		src, e := parseAgentSource(raw)
		if e != nil {
			errs = append(errs, fmt.Errorf("%s: %w", a.Path, e))
			continue
		}
		if src.Frontmatter.Name != a.ID || src.Frontmatter.Description != a.Desc {
			errs = append(errs, fmt.Errorf("%s: registry id/desc disagree with frontmatter", a.Path))
			continue
		}
		tier, ok := src.Frontmatter.Extra["tier"].(string)
		if !ok || tier != a.Tier {
			errs = append(errs, fmt.Errorf("%s: registry tier disagrees with frontmatter", a.Path))
			continue
		}
		if src.Frontmatter.Model != "" {
			errs = append(errs, fmt.Errorf("%s: use registry tier, not a vendor model", a.Path))
			continue
		}
		if e := c.agentDependencies(a.Path, src); e != nil {
			errs = append(errs, e)
		}
		// Tier is resolved through config by the harness adapter, not a renderer.
		delete(src.Frontmatter.Extra, "tier")
		for _, skill := range a.Skills {
			if e := c.skill(skill); e != nil {
				errs = append(errs, e)
			}
		}
		agents = append(agents, CapturedAgent{Definition: a, Source: src})
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	// Detect edits by actors which did not honor the management lock. Every
	// selected dependency participates, including index and resource bytes.
	keys := make([]string, 0, len(c.files))
	for k := range c.files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		raw, e := c.readFresh(k)
		if e != nil {
			return nil, e
		}
		if team.HashBytes(raw) != c.hashes[k] {
			return nil, fmt.Errorf("catalog changed during capture: %s", k)
		}
	}
	manifest := struct {
		Team   string
		Mode   string
		Hashes map[string]string
	}{name, mode, c.hashes}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("hash snapshot: %w", err)
	}
	return &Snapshot{Team: name, Mode: mode, Agents: agents, Files: c.files, Hashes: c.hashes, Digest: team.HashBytes(encoded)}, nil
}

// agentDependencies captures declared edges and removes transport-only headers
// from the source before a renderer receives it.
func (c *captureReader) agentDependencies(sourcePath string, src render.ResolvedSource) error {
	var errs []error
	for _, field := range []string{"requires", "resources"} {
		if value, exists := src.Frontmatter.Extra[field]; exists {
			encoded, e := yaml.Marshal(value)
			if e != nil {
				errs = append(errs, e)
				continue
			}
			var refs []string
			if e := yaml.Unmarshal(encoded, &refs); e != nil {
				errs = append(errs, fmt.Errorf("%s.%s: %w", sourcePath, field, e))
				continue
			}
			from, e := c.relative(sourcePath)
			if e != nil {
				errs = append(errs, e)
				continue
			}
			for _, ref := range refs {
				target, e := relativeDependency(from, ref)
				if e == nil {
					if field == "requires" {
						e = c.dependency(target, "")
					} else {
						_, e = c.read(target)
					}
				}
				if e != nil {
					errs = append(errs, e)
				}
			}
			delete(src.Frontmatter.Extra, field)
		}
	}
	return errors.Join(errs...)
}

type captureReader struct {
	ctx    context.Context
	root   *os.Root
	base   string
	files  map[string][]byte
	hashes map[string]string
	skills map[string]int
	total  int
}

func (c *captureReader) relative(p string) (string, error) {
	if filepath.IsAbs(p) {
		var err error
		p, err = filepath.EvalSymlinks(p)
		if err != nil {
			return "", fmt.Errorf("resolve source: %w", err)
		}
		p, err = filepath.Rel(c.base, p)
		if err != nil {
			return "", err
		}
	}
	p = filepath.ToSlash(filepath.Clean(p))
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "\\") {
		return "", fmt.Errorf("source escapes catalog: %q", p)
	}
	return p, nil
}
func (c *captureReader) readFresh(p string) ([]byte, error) {
	if err := c.ctx.Err(); err != nil {
		return nil, err
	}
	p, err := c.relative(p)
	if err != nil {
		return nil, err
	}
	// os.Root prevents symlink and rename races escaping this catalog.
	info, err := c.root.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("source %s: %w", p, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source %s: must be a regular file", p)
	}
	if info.Size() > MaxSourceBytes {
		return nil, fmt.Errorf("source %s: exceeds %d bytes", p, MaxSourceBytes)
	}
	f, err := c.root.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("source %s: %w", p, err)
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source %s: must be regular", p)
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxSourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	if int64(len(raw)) > MaxSourceBytes {
		return nil, fmt.Errorf("source %s: exceeds limit", p)
	}
	if err := c.ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *captureReader) read(p string) ([]byte, error) {
	p, err := c.relative(p)
	if err != nil {
		return nil, err
	}
	if raw, ok := c.files[p]; ok {
		return raw, nil
	}
	if len(c.files) >= 4096 {
		return nil, fmt.Errorf("source snapshot exceeds 4096 files")
	}
	raw, err := c.readFresh(p)
	if err != nil {
		return nil, err
	}
	c.total += len(raw)
	if c.total > MaxSnapshotBytes {
		return nil, fmt.Errorf("source snapshot exceeds %d bytes", MaxSnapshotBytes)
	}
	c.files[p] = raw
	c.hashes[p] = team.HashBytes(raw)
	return raw, nil
}

type skillHeader struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Requires    []string `yaml:"requires"`
	Resources   []string `yaml:"resources"`
}

func (c *captureReader) skill(id string) error {
	if !team.KeyPattern.MatchString(id) {
		return fmt.Errorf("invalid required skill %q", id)
	}
	return c.dependency(path.Join("skills", id, "SKILL.md"), id)
}
func (c *captureReader) dependency(p, expectedName string) error {
	p, err := c.relative(p)
	if err != nil {
		return err
	}
	if c.skills[p] == 1 {
		return fmt.Errorf("skill dependency cycle at %s", p)
	}
	if c.skills[p] == 2 {
		return nil
	}
	c.skills[p] = 1
	raw, err := c.read(p)
	if err != nil {
		return err
	}
	// A plain Markdown dependency has no outgoing declared edges.
	if expectedName == "" && !strings.HasPrefix(string(raw), "---\n") {
		c.skills[p] = 2
		return nil
	}
	src, err := parseAgentSource(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	rest := strings.TrimPrefix(string(raw), "---\n")
	end := strings.Index(rest, "\n---")
	var h skillHeader
	d := yaml.NewDecoder(strings.NewReader(rest[:end]))
	d.KnownFields(true)
	if err := d.Decode(&h); err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	if (expectedName != "" && h.Name != expectedName) || strings.TrimSpace(h.Description) == "" || src.Body == "" {
		return fmt.Errorf("%s: invalid skill metadata or empty body", p)
	}
	var errs []error
	for _, dep := range h.Requires {
		var e error
		if team.KeyPattern.MatchString(dep) {
			e = c.skill(dep)
		} else {
			target, resolveErr := relativeDependency(p, dep)
			if resolveErr != nil {
				e = resolveErr
			} else {
				e = c.dependency(target, "")
			}
		}
		if e != nil {
			errs = append(errs, e)
		}
	}
	for _, resource := range h.Resources {
		target, e := relativeDependency(p, resource)
		if e == nil {
			_, e = c.read(target)
		}
		if e != nil {
			errs = append(errs, e)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	c.skills[p] = 2
	return nil
}
func relativeDependency(from, ref string) (string, error) {
	if ref == "" || filepath.IsAbs(ref) || strings.Contains(ref, "\\") {
		return "", fmt.Errorf("%s: dependency must be relative: %q", from, ref)
	}
	target := path.Clean(path.Join(path.Dir(from), ref))
	if target == ".." || strings.HasPrefix(target, "../") {
		return "", fmt.Errorf("%s: dependency escapes catalog: %q", from, ref)
	}
	return target, nil
}
