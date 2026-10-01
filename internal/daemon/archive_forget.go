package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// Outcome and reasons of the forget step. Each visit of an owner with nothing
// left to archive ends in exactly one of these, logged as the visit line.
const (
	ArchiveOutcomeForgotten = "forgotten"

	ForgetReasonPlanRows   = "plan_rows_left"
	ForgetReasonResources  = "worktree_or_branch_present"
	ForgetReasonArchive    = "archive_file_present"
	ForgetReasonRunLogs    = "run_logs_present"
	ForgetReasonNotSettled = "not_settled"
)

// forgetFilesReason reports which leftover of the family, if any, keeps its
// skeleton: an archive file or directory of the task, or the run log folder of
// any family issue. It reads only the local filesystem and the database.
func (d *Daemon) forgetFilesReason(ctx context.Context, owner store.ArchiveOwner) (string, error) {
	repo := owner.Repo
	entries, err := os.ReadDir(archive.TaskDir(d.archive.Dir, repo.Forge, repo.Owner, repo.Name, owner.Session.IssueNumber))
	switch {
	case err == nil && len(entries) > 0:
		return ForgetReasonArchive, nil
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("read archive directory: %w", err)
	}
	if d.archive.RunsDir == "" {
		return "", errors.New("run log directory is not configured")
	}
	issues, err := d.db.ArchiveFamilyIssues(ctx, owner.Family)
	if err != nil {
		return "", err
	}
	for _, issue := range issues {
		_, err := os.Lstat(filepath.Join(d.archive.RunsDir, repo.Forge, repo.Owner, repo.Name, strconv.Itoa(issue)))
		switch {
		case err == nil:
			return ForgetReasonRunLogs, nil
		case !errors.Is(err, os.ErrNotExist):
			return "", fmt.Errorf("read run log directory: %w", err)
		}
	}
	return "", nil
}

// forgetFilesVisit is the cheap, network-free part of the forget decision.
func (d *Daemon) forgetFilesVisit(ctx context.Context, owner store.ArchiveOwner) (ArchiveVisit, bool, error) {
	reason, err := d.forgetFilesReason(ctx, owner)
	if err != nil {
		v, e := deferred("leftover_check_failed", err)
		return v, true, e
	}
	if reason != "" {
		v, e := kept(reason)
		return v, true, e
	}
	return ArchiveVisit{}, false, nil
}

// forgetFamily deletes the skeleton of an owner that has nothing left to archive.
// The caller holds the in-flight entry, the task lock and the parent story lock;
// this adds the clone gate for the git read. The skeleton goes only when every
// worktree and branch is already absent, the issue is still closed and the
// family still settled.
func (d *Daemon) forgetFamily(ctx context.Context, owner store.ArchiveOwner, now time.Time, issues RetirementIssues) (ArchiveVisit, error) {
	if v, stop, err := d.forgetFilesVisit(ctx, owner); stop {
		return v, err
	}
	releaseClone, err := d.retirement.CloneGate.Acquire(ctx, owner.Repo.Path)
	if err != nil {
		return deferred("clone_gate_failed", err)
	}
	defer releaseClone()
	fresh, err := d.db.GetArchiveOwner(ctx, owner.Session.ID, now)
	if err != nil {
		return ArchiveVisit{}, archiveStorageError(err)
	}
	if fresh.Session.LockOwner != d.instanceID {
		return deferred("task_ownership_lost", errors.New("archive task ownership lost"))
	}
	fresh.Active = fresh.BusyWithoutOwnerLock
	if archiveSettledReason(fresh) != "" {
		return kept(ForgetReasonNotSettled)
	}
	if fresh.ArchivableRows != 0 {
		return kept(ForgetReasonPlanRows)
	}
	openGit := d.retirement.OpenGit
	if openGit == nil {
		openGit = openRetirementClone
	}
	gitOps, err := openGit(ctx, fresh.RetirementOwner)
	if err != nil {
		return deferred("git_open_failed", err)
	}
	evaluation := EvaluateRetirement(ctx, RetirementOptions{
		Now: now, RetainDays: d.retirement.RetainDays, Issues: issues,
		ObserveGit: func(ctx context.Context, _ store.RetirementOwner) (git.RetirementObservation, error) {
			return gitOps.Observe(ctx)
		},
	}, fresh.RetirementOwner)
	if evaluation.Error != nil {
		return deferred("evaluation_failed", evaluation.Error)
	}
	if evaluation.IssueState != "closed" || evaluation.IssueError != "" {
		return kept(ForgetReasonNotSettled)
	}
	if reason := forgetResourceReason(evaluation); reason != "" {
		return kept(reason)
	}
	rows, err := d.db.ForgetArchivedFamily(ctx, fresh.Family, now)
	switch {
	case errors.Is(err, store.ErrArchiveNotSettled):
		return kept(ForgetReasonNotSettled)
	case errors.Is(err, store.ErrArchiveRowsLeft):
		return kept(ForgetReasonPlanRows)
	case err != nil:
		return deferred("forget_failed", err)
	}
	return ArchiveVisit{Outcome: ArchiveOutcomeForgotten, Rows: rows}, nil
}

// forgetResourceReason is "" only when the evaluator reached its per-resource
// decisions and every worktree and branch is already absent.
func forgetResourceReason(e RetirementEvaluation) string {
	if len(e.Decisions) == 0 {
		return ForgetReasonResources
	}
	for _, dec := range e.Decisions {
		if dec.Action == "already_absent" {
			continue
		}
		switch dec.Reason {
		case "active_work", "pending_effects":
			return ForgetReasonNotSettled
		}
		return ForgetReasonResources
	}
	return ""
}
