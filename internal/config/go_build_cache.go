package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GoBuildCaches contains the exact data directories granted to build processes.
// It describes routing, not ownership or permission to delete cache contents.
type GoBuildCaches struct {
	Main, Reviewer string
	factoryHome    string
}

// ResolveGoBuildCaches validates and prepares the selected cache directories.
// Existing populated directories are reused without manifests or claims.
func (p Paths) ResolveGoBuildCaches(settings Settings) (GoBuildCaches, error) {
	environment, err := readGoCacheEnvironment(nil)
	if err != nil {
		return GoBuildCaches{}, err
	}
	home, err := canonicalCacheAncestor(p.home)
	if err != nil {
		return GoBuildCaches{}, err
	}
	main := strings.TrimSpace(settings.GoBuildCachePath)
	if main == "" {
		main = filepath.Join(home+"-go-cache", "go-build")
	}
	reviewer := filepath.Join(p.ReviewCacheDir(), "go-build")
	caches := GoBuildCaches{Main: main, Reviewer: reviewer, factoryHome: home}
	for _, target := range []string{main, reviewer} {
		clean, err := validateCacheTarget(target)
		if err != nil {
			return GoBuildCaches{}, err
		}
		if configuredHost := environment["GOCACHE"]; filepath.IsAbs(configuredHost) {
			host, err := canonicalCacheAncestor(configuredHost)
			if err != nil {
				return GoBuildCaches{}, err
			}
			if cachePathsOverlap(clean, host) {
				return GoBuildCaches{}, fmt.Errorf("dedicated Go cache %q overlaps effective host GOCACHE", target)
			}
		}
		if target == main {
			caches.Main = clean
		} else {
			caches.Reviewer = clean
		}
	}
	if cachePathsOverlap(caches.Main, caches.Reviewer) {
		return GoBuildCaches{}, fmt.Errorf("dedicated main and reviewer Go caches overlap")
	}
	if err := caches.Revalidate(settings.WorktreeRoot); err != nil {
		return GoBuildCaches{}, err
	}
	for _, target := range []string{caches.Main, caches.Reviewer} {
		if err := os.MkdirAll(target, 0o700); err != nil {
			return GoBuildCaches{}, fmt.Errorf("create dedicated Go cache %q: %w", target, err)
		}
		if _, err := validateCacheTarget(target); err != nil {
			return GoBuildCaches{}, err
		}
		probe, err := os.CreateTemp(target, ".lf-write-probe-")
		if err != nil {
			return GoBuildCaches{}, fmt.Errorf("write dedicated Go cache %q: %w", target, err)
		}
		name := probe.Name()
		closeErr := probe.Close()
		removeErr := os.Remove(name)
		if closeErr != nil {
			return GoBuildCaches{}, closeErr
		}
		if removeErr != nil {
			return GoBuildCaches{}, removeErr
		}
	}
	return caches, nil
}

// Revalidate checks the effective targets immediately before a sandbox grant.
// worktree may be a checkout or configured worktree root; empty skips that check.
func (c GoBuildCaches) Revalidate(worktree string) error {
	if strings.TrimSpace(os.Getenv("GOCACHEPROG")) != "" {
		return fmt.Errorf("dedicated Go cache: GOCACHEPROG is incompatible with on-disk cache routing")
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	userHome, err = canonicalCacheAncestor(userHome)
	if err != nil {
		return err
	}
	hostCache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	hostCache, err = canonicalCacheAncestor(filepath.Join(hostCache, "go-build"))
	if err != nil {
		return err
	}
	var canonical []string
	for _, target := range []string{c.Main, c.Reviewer} {
		clean, err := validateCacheTarget(target)
		if err != nil {
			return err
		}
		canonical = append(canonical, clean)
		if cachePathWithin(userHome, clean) || cachePathsOverlap(clean, hostCache) || (c.factoryHome != "" && cachePathsOverlap(clean, c.factoryHome)) {
			return fmt.Errorf("dedicated Go cache %q overlaps a protected home or host cache", target)
		}
		if worktree != "" {
			root, err := canonicalCacheAncestor(worktree)
			if err != nil {
				return err
			}
			if cachePathsOverlap(clean, root) {
				return fmt.Errorf("dedicated Go cache %q overlaps worktree %q", target, worktree)
			}
		}
	}
	if cachePathsOverlap(canonical[0], canonical[1]) {
		return fmt.Errorf("dedicated main and reviewer Go caches overlap")
	}
	return nil
}

func cachePathsOverlap(a, b string) bool { return cachePathWithin(a, b) || cachePathWithin(b, a) }
func cachePathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// canonicalCacheAncestor resolves the existing prefix without requiring creation.
func canonicalCacheAncestor(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("dedicated Go cache path %q must be absolute", path)
	}
	path = filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve dedicated Go cache path %q: %w", path, err)
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	base, err := canonicalCacheAncestor(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.Base(path)), nil
}

func validateCacheTarget(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("dedicated Go cache path %q must be absolute", path)
	}
	clean := filepath.Clean(path)
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(clean, string(filepath.Separator)), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect dedicated Go cache %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// Darwin's system aliases are canonical ancestors, not cache redirects.
			if current == "/var" || current == "/tmp" || current == "/etc" {
				resolved, err := filepath.EvalSymlinks(current)
				if err != nil {
					return "", err
				}
				current = resolved
				continue
			}
			return "", fmt.Errorf("dedicated Go cache %q has symlink component %q", path, current)
		}
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return "", fmt.Errorf("dedicated Go cache %q is inside a Git worktree", path)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect worktree ancestor %q: %w", current, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("dedicated Go cache %q has non-directory component %q", path, current)
		}
	}
	return current, nil
}
