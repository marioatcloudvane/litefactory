package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
)

// Pass summary counters the bundle step adds to ArchivePass.Sweep.Counts.
const (
	ArchiveBundlesRemoved  = "bundles_removed"
	ArchiveBundlesFailures = "bundles_failures"
)

// archiveBundleGrace keeps a bundle folder of a brand-new task with a reused
// reference while its sessions row and its first bundle are being written.
const archiveBundleGrace = 24 * time.Hour

// ArchiveBundleTasks is the read-only store view the bundle step needs.
type ArchiveBundleTasks interface {
	TaskRefInUse(ctx context.Context, alias string) (bool, error)
}

// ArchiveBundlesOptions configures the bundle pass-start step.
type ArchiveBundlesOptions struct {
	// Root is the instruction bundle root (config.Paths.BundlesDir).
	Root string
	// Tasks reports whether a task still has its sessions row.
	Tasks  ArchiveBundleTasks
	Logger *slog.Logger
}

// NewArchiveBundlesStep returns the step that removes the instruction bundles
// of tasks whose sessions row is gone, which happens only once the family was
// archived, its archive expired and it was forgotten. A task that still has a
// row keeps every bundle, so a reopened task can run on its stored bundle. The
// step writes no database rows; per-folder failures are logged and counted.
func NewArchiveBundlesStep(opts ArchiveBundlesOptions) ArchiveStep {
	return ArchiveStep{Name: "archive_bundles", Run: func(ctx context.Context, pass *ArchivePass) error {
		b := archiveBundles{opts: opts, pass: pass}
		if b.opts.Logger == nil {
			b.opts.Logger = slog.New(slog.DiscardHandler)
		}
		return b.run(ctx)
	}}
}

type archiveBundles struct {
	opts              ArchiveBundlesOptions
	pass              *ArchivePass
	root              *os.Root
	removed, failures int
}

func (b *archiveBundles) run(ctx context.Context) error {
	if b.opts.Root == "" || b.opts.Tasks == nil {
		return nil
	}
	root, err := os.OpenRoot(b.opts.Root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open bundles root: %w", err)
	}
	defer root.Close()
	b.root = root
	defer b.record()
	return b.walk(ctx, ".", nil)
}

// walk descends exactly <forge>/<owner>/<repo>/<issue>. Parents are pruned
// only when something below them was removed.
func (b *archiveBundles) walk(ctx context.Context, rel string, parts []string) error {
	entries, err := fs.ReadDir(b.root.FS(), rel)
	if err != nil {
		b.fail(rel, fmt.Errorf("read: %w", err))
		return nil
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			continue
		}
		child := path.Join(rel, entry.Name())
		next := append(append([]string(nil), parts...), entry.Name())
		if len(next) < 4 {
			before := b.removed
			if err := b.walk(ctx, child, next); err != nil {
				return err
			}
			if b.removed > before {
				b.prune(child)
			}
			continue
		}
		b.visit(ctx, child, next)
	}
	return nil
}

func (b *archiveBundles) visit(ctx context.Context, rel string, parts []string) {
	issue, err := strconv.Atoi(parts[3])
	if err != nil || strconv.Itoa(issue) != parts[3] {
		return
	}
	ref := naming.Ref{Forge: parts[0], Owner: parts[1], Repo: parts[2], Issue: issue}
	if ref.Validate() != nil {
		return
	}
	// The folder path has no qualification, so a qualified task with the
	// same alias keeps it in use as well.
	inUse, err := b.opts.Tasks.TaskRefInUse(ctx, ref.String())
	if err != nil {
		b.fail(rel, fmt.Errorf("task lookup: %w", err))
		return
	}
	if inUse {
		return
	}
	old, err := b.olderThanGrace(rel)
	if err != nil {
		b.fail(rel, fmt.Errorf("stat: %w", err))
		return
	}
	if !old {
		return
	}
	if err := b.root.RemoveAll(rel); err != nil {
		b.fail(rel, fmt.Errorf("remove: %w", err))
		return
	}
	b.removed++
	b.opts.Logger.Info("instruction bundles removed", slog.String("path", rel), slog.String("task", ref.String()))
}

// olderThanGrace reports whether the folder and everything in it changed
// before the grace period.
func (b *archiveBundles) olderThanGrace(rel string) (bool, error) {
	cutoff := b.pass.Now.Add(-archiveBundleGrace)
	old := true
	err := fs.WalkDir(b.root.FS(), rel, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().Before(cutoff) {
			old = false
			return fs.SkipAll
		}
		return nil
	})
	return old, err
}

// prune removes rel when it is empty; the bundles root is never passed here.
func (b *archiveBundles) prune(rel string) {
	entries, err := fs.ReadDir(b.root.FS(), rel)
	if err != nil || len(entries) != 0 {
		return
	}
	if err := b.root.Remove(rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
		b.fail(rel, fmt.Errorf("prune: %w", err))
	}
}

func (b *archiveBundles) fail(rel string, err error) {
	b.failures++
	b.opts.Logger.Error("instruction bundle cleanup failure", slog.String("path", rel), slog.String("error", err.Error()))
}

func (b *archiveBundles) record() {
	counts := b.pass.Sweep.Counts
	if counts == nil {
		counts = make(map[string]int)
		b.pass.Sweep.Counts = counts
	}
	counts[ArchiveBundlesRemoved] += b.removed
	counts[ArchiveBundlesFailures] += b.failures
}
