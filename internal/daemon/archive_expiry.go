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
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// Pass summary counters the expiry step adds to ArchivePass.Sweep.Counts.
const (
	ArchiveExpiredArchives = "expired_archives"
	ArchiveExpiredLogs     = "expired_logs_deleted"
	ArchiveExpiredBytes    = "expired_bytes_freed"
	ArchiveExpiredFailures = "expired_failures"
)

// ArchiveRunLister is the read-only store view expiry needs.
type ArchiveRunLister interface {
	ListArchivedRunArtefacts(ctx context.Context, taskRef string, archiveDate time.Time) (runs []store.ArchivedRun, found bool, err error)
}

// ArchiveExpiryOptions configures the expiry pass-start step.
type ArchiveExpiryOptions struct {
	// Runs matches an archive to the run logs it covers.
	Runs ArchiveRunLister
	// RunsDir is the root of the run artefacts (config.Paths.RunsDir).
	RunsDir string
	// RetainDays is settings.archive_retain_days.
	RetainDays int
	Logger     *slog.Logger
}

// NewArchiveExpiryStep returns the step that deletes archives older than the
// retention together with the run logs they cover. It takes no locks, writes no
// database rows and never compacts. Per-file failures are logged and counted; a
// failed artefact delete keeps its archive for the next pass.
func NewArchiveExpiryStep(opts ArchiveExpiryOptions) ArchiveStep {
	return ArchiveStep{Name: "archive_expiry", Run: func(ctx context.Context, pass *ArchivePass) error {
		e := archiveExpirer{opts: opts, pass: pass}
		if e.opts.Logger == nil {
			e.opts.Logger = slog.New(slog.DiscardHandler)
		}
		return e.run(ctx)
	}}
}

type archiveExpirer struct {
	opts ArchiveExpiryOptions
	pass *ArchivePass
	arch *os.Root
	runs *os.Root

	expired, logs, failures int
	bytes                   int64
}

// archiveCandidate is a regular archive file at its canonical location.
type archiveCandidate struct {
	rel   string
	ref   naming.Ref
	name  time.Time
	size  int64
	mtime time.Time
}

func (e *archiveExpirer) run(ctx context.Context) (err error) {
	if e.opts.RetainDays < 1 {
		return nil
	}
	arch, err := os.OpenRoot(e.pass.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open archive root: %w", err)
	}
	defer arch.Close()
	e.arch = arch
	defer func() {
		if e.runs != nil {
			e.runs.Close()
		}
		e.record()
	}()

	candidates, err := e.candidates(ctx)
	if err != nil {
		return err
	}
	for _, c := range candidates {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		e.expire(ctx, c)
	}
	return nil
}

func (e *archiveExpirer) record() {
	counts := e.pass.Sweep.Counts
	if counts == nil {
		counts = make(map[string]int)
		e.pass.Sweep.Counts = counts
	}
	counts[ArchiveExpiredArchives] += e.expired
	counts[ArchiveExpiredLogs] += e.logs
	counts[ArchiveExpiredBytes] += int(e.bytes)
	counts[ArchiveExpiredFailures] += e.failures
}

// candidates walks <forge>/<owner>/<repo>/<issue>/ under the archive root and
// keeps only regular files named <issue>-<timestamp>.jsonl.gz whose folder is
// that issue. Symlinks, directories, ".partial" files and lookalikes are
// skipped. A walk error ends the walk and is returned.
func (e *archiveExpirer) candidates(ctx context.Context) ([]archiveCandidate, error) {
	var out []archiveCandidate
	err := fs.WalkDir(e.arch.FS(), ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		parts := strings.Split(p, "/")
		if d.IsDir() {
			if len(parts) > 4 {
				return fs.SkipDir
			}
			return nil
		}
		if len(parts) != 5 || !d.Type().IsRegular() {
			return nil
		}
		issue, name, err := archive.ParseName(parts[4])
		if err != nil || parts[3] != strconv.Itoa(issue) {
			return nil
		}
		ref := naming.Ref{Forge: parts[0], Owner: parts[1], Repo: parts[2], Issue: issue}
		if ref.Validate() != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			e.fail(p, fmt.Errorf("stat: %w", err))
			return nil
		}
		out = append(out, archiveCandidate{rel: p, ref: ref, name: name, size: info.Size(), mtime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk archive root: %w", err)
	}
	return out, nil
}

func (e *archiveExpirer) fail(rel string, err error) {
	e.failures++
	e.opts.Logger.Error("archive expiry failure", slog.String("path", rel), slog.String("error", err.Error()))
}

func (e *archiveExpirer) headerDate(c archiveCandidate) (time.Time, bool) {
	f, err := e.arch.Open(c.rel)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()
	h, err := archive.ReadHeaderFrom(f)
	if err != nil {
		return time.Time{}, false
	}
	t, err := archive.HeaderDate(h)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func (e *archiveExpirer) expire(ctx context.Context, c archiveCandidate) {
	hdr, hdrOK := e.headerDate(c)
	choice := archive.ChooseDate(c.name, true, hdr, hdrOK, c.mtime)
	if !archive.Expired(e.pass.Now, choice.Date, e.opts.RetainDays) {
		return
	}
	if choice.Warning != "" {
		e.opts.Logger.Warn("archive dates differ", slog.String("path", c.rel), slog.String("detail", choice.Warning))
	}
	deleted, freed, failed, matched := e.deleteRunLogs(ctx, c, choice.Date)
	if failed > 0 {
		e.failures += failed
		e.logs += deleted
		e.bytes += freed
		e.opts.Logger.Error("archive expiry kept archive", slog.String("path", c.rel), slog.Time("date", choice.Date), slog.String("date_source", choice.Source), slog.Int("artefacts_deleted", deleted), slog.Int("failures", failed))
		return
	}
	if !matched {
		e.opts.Logger.Warn("archive run logs could not be matched", slog.String("path", c.rel))
	}
	if err := e.arch.Remove(c.rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
		e.failures++
		e.logs += deleted
		e.bytes += freed
		e.opts.Logger.Error("archive expiry delete failed", slog.String("path", c.rel), slog.String("error", err.Error()), slog.Int("artefacts_deleted", deleted))
		return
	}
	e.expired++
	e.logs += deleted
	e.bytes += freed + c.size
	e.opts.Logger.Info("archive expired", slog.String("path", c.rel), slog.Time("date", choice.Date), slog.String("date_source", choice.Source), slog.Int("artefacts_deleted", deleted), slog.Int("failures", 0))
}

// deleteRunLogs removes the artefacts of every family run that finished at or
// before the archive date. matched is false when the family cannot be read from
// the database, which is not a failure: the archive is then deleted alone.
func (e *archiveExpirer) deleteRunLogs(ctx context.Context, c archiveCandidate, date time.Time) (deleted int, freed int64, failed int, matched bool) {
	if e.opts.Runs == nil || e.opts.RunsDir == "" {
		return 0, 0, 0, false
	}
	runs, found, err := e.opts.Runs.ListArchivedRunArtefacts(ctx, c.ref.String(), date)
	if err != nil {
		e.opts.Logger.Error("archive expiry run lookup failed", slog.String("path", c.rel), slog.String("error", err.Error()))
		return 0, 0, 1, true
	}
	if !found {
		return 0, 0, 0, false
	}
	if e.runs == nil {
		e.runs, err = os.OpenRoot(e.opts.RunsDir)
		if errors.Is(err, fs.ErrNotExist) {
			return 0, 0, 0, true
		}
		if err != nil {
			e.opts.Logger.Error("archive expiry open runs root failed", slog.String("error", err.Error()))
			return 0, 0, 1, true
		}
	}
	dirs := make(map[string]bool)
	for _, run := range runs {
		stem, err := naming.FormatRunOrdinal(run.Ordinal)
		if err != nil {
			continue
		}
		dir := path.Join(c.ref.Forge, c.ref.Owner, c.ref.Repo, strconv.Itoa(run.Issue))
		dirs[dir] = true
		for _, suffix := range []string{naming.RunLogExt, naming.RunMetaExt, naming.AttachmentsDirSuffix} {
			rel := path.Join(dir, stem+suffix)
			n, size, err := e.removeArtefact(rel)
			deleted += n
			freed += size
			if err != nil {
				failed++
				e.opts.Logger.Error("archive expiry artefact delete failed", slog.String("path", rel), slog.String("error", err.Error()))
			}
		}
	}
	for dir := range dirs {
		e.pruneEmpty(dir)
	}
	return deleted, freed, failed, true
}

// removeArtefact deletes one file or attachments directory under the runs root.
// A missing artefact is not an error and is not counted.
func (e *archiveExpirer) removeArtefact(rel string) (count int, size int64, err error) {
	info, err := e.runs.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	if info.IsDir() {
		_ = fs.WalkDir(e.runs.FS(), rel, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				if fi, ierr := d.Info(); ierr == nil {
					size += fi.Size()
				}
			}
			return nil
		})
	} else {
		size = info.Size()
	}
	if err := e.runs.RemoveAll(rel); err != nil {
		return 0, 0, err
	}
	return 1, size, nil
}

// pruneEmpty removes the issue's runs folder and then its empty parents, never
// the runs root itself. A folder that still holds anything is left alone.
func (e *archiveExpirer) pruneEmpty(dir string) {
	for dir != "." && dir != "" {
		if err := e.runs.Remove(dir); err != nil {
			return
		}
		dir = path.Dir(dir)
	}
}
