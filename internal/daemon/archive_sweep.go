package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

const (
	archiveBatchTimeout   = 2 * time.Minute
	archiveCompactTimeout = 10 * time.Minute
	archiveOwnerRequests  = 2
	archivePassInterval   = 24 * time.Hour
)

// Owner outcomes of one archive visit.
const (
	ArchiveOutcomeArchived  = "archived"
	ArchiveOutcomeKept      = "kept"
	ArchiveOutcomeDeferred  = "deferred"
	ArchiveOutcomeNothingTo = "nothing_to_archive"
)

// ErrArchiveStorage distinguishes unreadable durable state, which stops the
// batch without advancing, from an owner deferral.
var ErrArchiveStorage = errors.New("archive storage failure")

func archiveStorageError(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrArchiveStorage, err)
}

// ArchivePass is what a pass-start step may read and update. Steps run once per
// pass, after leftover ".partial" files are removed and before the first owner.
// A step reports its totals by adding to Sweep.Counts, which the pass summary
// logs, and must be idempotent because a failed checkpoint reruns it.
type ArchivePass struct {
	Sweep *store.ArchiveSweep
	Dir   string
	Now   time.Time
}

// ArchiveStep is a pass-start step, the extension point for later stories such
// as expiry of old archives. A failing step is logged and never blocks the pass.
type ArchiveStep struct {
	Name string
	Run  func(ctx context.Context, pass *ArchivePass) error
}

// ArchiveOptions configures the daily archive pass. The coordination it needs
// (lock directory, clone gate presence) comes from the retirement options, so
// the archive is disabled wherever retirement is.
type ArchiveOptions struct {
	// Dir is the archive root (config.Paths.ArchiveDir); empty disables it.
	Dir string
	// RunsDir is the run log root (config.Paths.RunsDir). The forget step keeps a
	// family whose run log folder still exists, and defers when it is unset.
	RunsDir string
	// Issues optionally replaces the forge for the closure read.
	Issues RetirementIssues
	// StartSteps run in order at the start of each pass.
	StartSteps []ArchiveStep
	// BeforeDelete, when set, runs after the file is published and before the
	// delete transaction. It is a seam for exercising late changes.
	BeforeDelete func(context.Context) error
}

// ArchiveVisit is the outcome of one owner, logged as a single line.
type ArchiveVisit struct {
	Outcome           string
	Reason            string
	File              string
	Rows              map[string]int64
	BytesUncompressed int64
	BytesCompressed   int64
}

// RunArchiveBatch visits at most one owner after retirement. A completed pass
// waits 24 hours, measured from supplied daemon time, before the next one.
func (d *Daemon) RunArchiveBatch(ctx context.Context, now time.Time) (err error) {
	if d.archive.Dir == "" || d.retirement.CloneGate == nil || d.retirement.LockDir == "" {
		return nil
	}
	if _, held := d.backoff.hold(now); held {
		return nil
	}
	parentCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, archiveBatchTimeout)
	defer cancel()
	s, err := d.db.GetArchiveSweep(ctx)
	if err != nil {
		return err
	}
	if s.Complete && now.Before(s.NextDue) {
		return nil
	}
	if s.ID == "" || s.Complete {
		s, err = d.startArchivePass(ctx, now)
		if err != nil {
			return err
		}
	}
	defer func() {
		if err != nil {
			d.log.ErrorContext(ctx, "archive sweep incomplete", slog.String("sweep", s.ID), slog.Int64("cursor", s.Cursor), slog.Any("outcomes", s.Counts), slog.Int("deferred", s.Deferred), slog.Bool("completed", false))
		}
	}()
	owners, err := d.db.ListRetirementOwners(ctx, s.Cursor, s.Boundary, 1)
	if err != nil {
		return err
	}
	if len(owners) == 0 {
		return d.finishArchivePass(parentCtx, s, now)
	}
	owner := owners[0]
	reservation, err := d.discovery.acquire(fmt.Sprintf("archive:%d", owner.Session.ID), archiveOwnerRequests, false)
	if errors.Is(err, ErrDiscoveryDeferred) {
		d.log.InfoContext(ctx, "archive sweep deferred", slog.String("sweep", s.ID), slog.String("reason", "request_budget"), slog.Bool("completed", false))
		return nil
	}
	if err != nil {
		return err
	}
	defer reservation.finish()
	ctx = github.WithRequestObserver(ctx, reservation.observe)
	issues := d.archive.Issues
	if issues == nil {
		issues = d.retirement.Issues
	}
	if issues == nil {
		issues = d.forge
	}
	issues = retirementBudgetIssues{d: d, issues: issues, now: now}

	var visit ArchiveVisit
	var visitErr error
	if owner.ObservationError != nil {
		visit = ArchiveVisit{Outcome: ArchiveOutcomeDeferred, Reason: "owner_observation_failed"}
		visitErr = owner.ObservationError
	} else {
		visit, visitErr = d.archiveOwner(ctx, owner.Session.ID, now, issues)
	}
	d.logArchiveVisit(ctx, s.ID, owner.Session.TaskRef, visit, visitErr)
	if errors.Is(visitErr, ErrArchiveStorage) {
		return visitErr
	}
	if parentCtx.Err() != nil {
		return parentCtx.Err()
	}
	var rate *github.RateLimitError
	if errors.As(visitErr, &rate) {
		return d.observe(ctx, now, visitErr)
	}
	if errors.Is(visitErr, ErrDiscoveryDeferred) {
		return nil
	}
	if s.Counts == nil {
		s.Counts = make(map[string]int)
	}
	s.Counts[visit.Outcome]++
	if visit.Outcome == ArchiveOutcomeKept || visit.Outcome == ArchiveOutcomeDeferred {
		s.Deferred++
	}
	if visit.Outcome == ArchiveOutcomeArchived || visit.Outcome == ArchiveOutcomeForgotten {
		s.DeletedAny = true
	}
	s.Owners++
	s.Cursor = owner.Session.ID
	return d.db.SaveArchiveSweep(parentCtx, s)
}

// startArchivePass fixes the boundary, removes leftover temporary files, runs
// the pass-start steps and persists the new pass.
func (d *Daemon) startArchivePass(ctx context.Context, now time.Time) (store.ArchiveSweep, error) {
	boundary, err := d.db.RetirementBoundary(ctx)
	if err != nil {
		return store.ArchiveSweep{}, err
	}
	s := store.ArchiveSweep{ID: fmt.Sprintf("archive-%d", now.UnixNano()), StartedAt: now, Boundary: boundary, Counts: make(map[string]int)}
	if removed, err := archive.CleanPartials(d.archive.Dir); err != nil {
		d.log.ErrorContext(ctx, "archive partial cleanup failed", slog.String("sweep", s.ID), slog.Int("removed", removed))
	} else if removed > 0 {
		d.log.InfoContext(ctx, "archive partial files removed", slog.String("sweep", s.ID), slog.Int("removed", removed))
	}
	pass := &ArchivePass{Sweep: &s, Dir: d.archive.Dir, Now: now}
	for _, step := range d.archive.StartSteps {
		if err := step.Run(ctx, pass); err != nil {
			d.log.ErrorContext(ctx, "archive start step failed", slog.String("sweep", s.ID), slog.String("step", step.Name))
		}
	}
	if err := d.db.SaveArchiveSweep(ctx, s); err != nil {
		return store.ArchiveSweep{}, err
	}
	return s, nil
}

// finishArchivePass compacts once when this pass deleted anything, then closes
// the pass. A compaction failure is logged and never undoes an archive.
func (d *Daemon) finishArchivePass(ctx context.Context, s store.ArchiveSweep, now time.Time) error {
	var compact store.CompactResult
	compacted := false
	if s.DeletedAny {
		cctx, cancel := context.WithTimeout(ctx, archiveCompactTimeout)
		result, err := d.db.Compact(cctx)
		cancel()
		compact = result
		if err != nil {
			d.log.ErrorContext(ctx, "archive compaction failed", slog.String("sweep", s.ID), slog.Int64("db_bytes_before", result.Before), slog.Int64("db_bytes_after", result.After))
		} else {
			compacted = true
			d.log.InfoContext(ctx, "archive compaction complete", slog.String("sweep", s.ID), slog.Int64("db_bytes_before", result.Before), slog.Int64("db_bytes_after", result.After))
		}
	}
	s.Complete = true
	s.NextDue = now.Add(archivePassInterval)
	if err := d.db.SaveArchiveSweep(ctx, s); err != nil {
		return err
	}
	d.log.InfoContext(ctx, "archive sweep complete", slog.String("sweep", s.ID), slog.Int("owners", s.Owners), slog.Any("outcomes", s.Counts), slog.Int("deferred", s.Deferred), slog.Bool("compacted", compacted), slog.Int64("db_bytes_before", compact.Before), slog.Int64("db_bytes_after", compact.After), slog.Bool("completed", true), slog.Time("next_due", s.NextDue))
	return nil
}

func (d *Daemon) logArchiveVisit(ctx context.Context, sweep, taskRef string, v ArchiveVisit, err error) {
	attrs := []any{slog.String("sweep", sweep), slog.String("task_ref", taskRef), slog.String("outcome", v.Outcome), slog.String("reason", v.Reason), slog.Any("rows", v.Rows), slog.Int64("bytes_uncompressed", v.BytesUncompressed), slog.Int64("bytes_compressed", v.BytesCompressed), slog.String("file", filepath.Base(v.File))}
	if err != nil && v.Outcome == "" {
		d.log.ErrorContext(ctx, "archive owner incomplete", attrs...)
		return
	}
	if v.Outcome == ArchiveOutcomeDeferred && err != nil {
		d.log.WarnContext(ctx, "archive owner", attrs...)
		return
	}
	d.log.InfoContext(ctx, "archive owner", attrs...)
}

func kept(reason string) (ArchiveVisit, error) {
	return ArchiveVisit{Outcome: ArchiveOutcomeKept, Reason: reason}, nil
}

func deferred(reason string, err error) (ArchiveVisit, error) {
	return ArchiveVisit{Outcome: ArchiveOutcomeDeferred, Reason: reason}, err
}

// archiveSettledReason returns why the owner must be kept, or "" when settled.
func archiveSettledReason(owner store.ArchiveOwner) string {
	switch {
	case !owner.Canonical || owner.Active || owner.Pending:
		return "active_or_uncertain_owner"
	case owner.ArchivePending:
		return owner.ArchivePendingReason
	}
	return ""
}

// archiveOwner archives one owner's family under the same coordination
// retirement uses, in the same order: in-flight entry, task lock, parent story
// lock. Nothing is deleted until the written file has been verified and the
// delete transaction has re-proved settlement and matching digests.
func (d *Daemon) archiveOwner(ctx context.Context, ownerID int64, now time.Time, issues RetirementIssues) (ArchiveVisit, error) {
	owner, err := d.db.GetArchiveOwner(ctx, ownerID, now)
	if err != nil {
		return ArchiveVisit{}, archiveStorageError(err)
	}
	if reason := archiveSettledReason(owner); reason != "" {
		return kept(reason)
	}
	if owner.ArchivableRows == 0 {
		if v, stop, err := d.forgetFilesVisit(ctx, owner); stop {
			return v, err
		}
	}
	if !d.enterDoctorGuarded(ctx, ownerID) {
		return kept("active_or_uncertain_owner")
	}
	defer d.inflight.leave(ownerID)
	acquired, err := d.db.AcquireSessionLock(ctx, ownerID, d.instanceID)
	if err != nil {
		return ArchiveVisit{}, archiveStorageError(err)
	}
	if !acquired {
		return kept("owner_locked")
	}
	defer func() {
		if relErr := d.db.ReleaseSessionLock(context.WithoutCancel(ctx), ownerID, d.instanceID); relErr != nil {
			d.log.ErrorContext(ctx, "archive task lock release failed", slog.Int64("owner", ownerID))
		}
	}()
	releaseParent, err := d.db.LockStoryProgress(ctx, owner.Session.TaskRef, d.retirement.LockDir)
	if errors.Is(err, store.ErrStoryProgressBusy) {
		return kept("parent_busy")
	}
	if err != nil {
		return deferred("parent_lock_failed", err)
	}
	defer func() {
		if relErr := releaseParent(); relErr != nil {
			d.log.ErrorContext(ctx, "archive parent lock release failed", slog.Int64("owner", ownerID))
		}
	}()

	owner, err = d.db.GetArchiveOwner(ctx, ownerID, now)
	if err != nil {
		return ArchiveVisit{}, archiveStorageError(err)
	}
	if owner.Session.LockOwner != d.instanceID {
		return deferred("task_ownership_lost", errors.New("archive task ownership lost"))
	}
	owner.Active = owner.BusyWithoutOwnerLock
	if reason := archiveSettledReason(owner); reason != "" {
		return kept(reason)
	}
	if owner.ArchivableRows == 0 {
		return d.forgetFamily(ctx, owner, now, issues)
	}
	issue, err := issues.GetIssue(github.WithoutConditionalCache(ctx), github.Repo{Owner: owner.Repo.Owner, Name: owner.Repo.Name}, owner.Session.IssueNumber)
	if err != nil {
		return deferred("issue_lookup_failed", err)
	}
	if !RetirementIssueClosed(owner.Repo, owner.Session.IssueNumber, issue) {
		return kept("issue_not_confirmed_closed")
	}
	return d.archiveFamily(ctx, owner, now)
}

// archiveFamily performs write, verify, publish and the ordered delete.
func (d *Daemon) archiveFamily(ctx context.Context, owner store.ArchiveOwner, now time.Time) (ArchiveVisit, error) {
	repo, session := owner.Repo, owner.Session
	dir := archive.TaskDir(d.archive.Dir, repo.Forge, repo.Owner, repo.Name, session.IssueNumber)
	live, err := d.liveArchiveTrailer(ctx, owner.Family)
	if err != nil {
		return deferred("read_failed", err)
	}
	var visit ArchiveVisit
	trailer, path, reused, err := d.reuseArchive(dir, live)
	if err != nil {
		return deferred("existing_archive_unreadable", err)
	}
	if reused {
		visit.Reason = "reused_existing_file"
		if info, statErr := os.Stat(path); statErr == nil {
			visit.BytesCompressed = info.Size()
		}
	} else {
		var res archive.Result
		res, err = d.writeArchive(ctx, owner, dir, now)
		if err != nil {
			return deferred("write_failed", err)
		}
		trailer, path = res.Trailer, res.FinalPath
		visit.BytesUncompressed, visit.BytesCompressed = res.Uncompressed, res.Compressed
	}
	visit.File = path
	visit.Rows = trailer.Counts
	if d.archive.BeforeDelete != nil {
		if err := d.archive.BeforeDelete(ctx); err != nil {
			return d.undoArchiveFile(visit, path, reused, "before_delete_failed", err)
		}
	}
	if _, err := d.db.DeleteArchivedFamily(ctx, owner.Family, trailer, now); err != nil {
		reason := "delete_failed"
		switch {
		case errors.Is(err, store.ErrArchiveNotSettled):
			reason = "not_settled_at_delete"
		case errors.Is(err, store.ErrArchiveDigestMismatch):
			reason = "digest_mismatch"
		}
		return d.undoArchiveFile(visit, path, reused, reason, err)
	}
	visit.Outcome = ArchiveOutcomeArchived
	return visit, nil
}

// undoArchiveFile removes a file this run just published when its delete
// rolled back, so the retry writes a fresh, consistent archive. A reused file
// is left alone: it was valid before this run.
func (d *Daemon) undoArchiveFile(v ArchiveVisit, path string, reused bool, reason string, cause error) (ArchiveVisit, error) {
	v.Outcome, v.Reason = ArchiveOutcomeDeferred, reason
	if reused {
		return v, cause
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return v, errors.Join(cause, err)
	}
	if err := archive.SyncDir(filepath.Dir(path)); err != nil {
		return v, errors.Join(cause, err)
	}
	return v, cause
}

// liveArchiveTrailer hashes the family's current rows without writing them.
func (d *Daemon) liveArchiveTrailer(ctx context.Context, family store.ArchiveFamily) (archive.Trailer, error) {
	digester := archive.NewDigester()
	err := d.db.StreamArchiveRows(ctx, family, func(r archive.Row) error {
		_, err := digester.Add(r)
		return err
	})
	if err != nil {
		return archive.Trailer{}, err
	}
	return digester.Trailer(), nil
}

// reuseArchive finds a verified existing file of this task whose digest equals
// the live rows, which is what a crash between publish and delete leaves.
func (d *Daemon) reuseArchive(dir string, live archive.Trailer) (archive.Trailer, string, bool, error) {
	files, err := archive.TaskFiles(dir)
	if err != nil {
		return archive.Trailer{}, "", false, err
	}
	for i := len(files) - 1; i >= 0; i-- {
		c, err := archive.Verify(files[i])
		if err != nil {
			continue
		}
		if c.Trailer.Digest == live.Digest {
			return c.Trailer, files[i], true, nil
		}
	}
	return archive.Trailer{}, "", false, nil
}

// writeArchive streams the family to a temporary file, reads it back
// independently, compares counts and digests with what was written, and only
// then renames it into place.
func (d *Daemon) writeArchive(ctx context.Context, owner store.ArchiveOwner, dir string, now time.Time) (archive.Result, error) {
	repo, session := owner.Repo, owner.Session
	at := now.UTC().Truncate(time.Second)
	final := filepath.Join(dir, archive.BuildName(session.IssueNumber, at))
	ids, err := d.db.ArchiveFamilySessions(ctx, owner.Family)
	if err != nil {
		return archive.Result{}, err
	}
	schema, err := d.db.SchemaVersion(ctx)
	if err != nil {
		return archive.Result{}, err
	}
	w, err := archive.Create(final, archive.Header{
		ArchivedAt:    at.Format(time.RFC3339),
		TaskRef:       session.TaskRef,
		Repo:          repo.Owner + "/" + repo.Name,
		Issue:         session.IssueNumber,
		SessionIDs:    ids,
		SchemaVersion: schema,
	})
	if err != nil {
		return archive.Result{}, err
	}
	if err := d.db.StreamArchiveRows(ctx, owner.Family, w.WriteRow); err != nil {
		return archive.Result{}, errors.Join(err, w.Abort())
	}
	res, err := w.Finish()
	if err != nil {
		return archive.Result{}, err
	}
	read, err := archive.Verify(res.TempPath)
	if err != nil {
		return archive.Result{}, errors.Join(fmt.Errorf("verify temporary archive: %w", err), w.Abort())
	}
	if read.Trailer.Digest != res.Trailer.Digest || len(read.Trailer.Counts) != len(res.Trailer.Counts) {
		return archive.Result{}, errors.Join(errors.New("temporary archive differs from what was written"), w.Abort())
	}
	for table, n := range res.Trailer.Counts {
		if read.Trailer.Counts[table] != n || read.Trailer.Digests[table] != res.Trailer.Digests[table] {
			return archive.Result{}, errors.Join(fmt.Errorf("temporary archive differs for %s", table), w.Abort())
		}
	}
	if err := w.Publish(); err != nil {
		return archive.Result{}, err
	}
	return res, nil
}
