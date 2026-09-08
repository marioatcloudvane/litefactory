package codex

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// skillOverrides disables discovered ambient skills explicitly. ignore-rules is
// only an execpolicy switch; it does not disable skill discovery. Roots mirror
// documented project/ancestor, user and admin discovery, including symlinked
// directories. Only paths are inspected: no credential or instruction bodies.
func (r *Runner) skillOverrides(ctx context.Context, cwd string, selected []string) (string, error) {
	roots := []string{"/etc/codex/skills"}
	home := envValue(r.environ, "HOME")
	if home != "" {
		roots = append(roots, filepath.Join(home, ".agents/skills"), filepath.Join(home, ".codex/skills"))
	}
	if codexHome := envValue(r.environ, "CODEX_HOME"); codexHome != "" {
		roots = append(roots, filepath.Join(codexHome, "skills"))
	}
	for dir := filepath.Clean(cwd); filepath.IsAbs(dir); dir = filepath.Dir(dir) {
		roots = append(roots, filepath.Join(dir, ".agents/skills"), filepath.Join(dir, ".codex/skills"))
		if filepath.Dir(dir) == dir {
			break
		}
	}
	paths := map[string]bool{}
	visited := map[string]bool{}
	remaining := 4096
	var scan func(string, int) error
	scan = func(dir string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 32 || remaining == 0 {
			return fmt.Errorf("ambient skill discovery exceeds safety limit")
		}
		canonical, err := filepath.EvalSymlinks(dir)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("resolve ambient skill directory: %w", err)
		}
		if visited[canonical] {
			return nil
		}
		visited[canonical] = true
		remaining--
		entries, err := os.ReadDir(canonical)
		if err != nil {
			return fmt.Errorf("inspect ambient skill directory: %w", err)
		}
		for _, entry := range entries {
			if entry.Name() == "SKILL.md" {
				paths[filepath.Join(canonical, "SKILL.md")] = false
				paths[filepath.Join(dir, "SKILL.md")] = false
				return nil
			}
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				path := filepath.Join(canonical, entry.Name())
				info, err := os.Stat(path)
				if err != nil {
					return fmt.Errorf("inspect ambient skill entry: %w", err)
				}
				if info.IsDir() {
					if err := scan(path, depth+1); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	for _, root := range roots {
		if err := scan(root, 0); err != nil {
			return "", err
		}
	}
	for _, path := range selected {
		paths[path] = true
	}
	keys := make([]string, 0, len(paths))
	for path := range paths {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	entries := make([]string, 0, len(keys))
	for _, path := range keys {
		entries = append(entries, "{path="+strconv.Quote(path)+",enabled="+strconv.FormatBool(paths[path])+"}")
	}
	value := "skills.config=[" + strings.Join(entries, ",") + "]"
	if len(value) > 128<<10 {
		return "", fmt.Errorf("ambient skill override list exceeds 128 KiB")
	}
	return value, nil
}
