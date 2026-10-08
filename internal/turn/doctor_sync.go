package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
)

// DoctorSyncOptions binds branch synchronization to the approved immutable heads.
// RecordBackup must durably retain the head before returning success.
type DoctorSyncOptions struct {
	Worktree                           *git.Worktree
	CloneGate                          CloneGate
	CloneRoot                          string
	Branch, BeforeHead, Base, BaseHead string
	Author                             git.Author
	Advisor                            ConflictAdvisor
	RecordBackup                       func(context.Context, string) error
	OperationID                        int64
	Task, Owner, Model                 string
}

// DoctorSyncResult records the retained head and the verified synchronization.
type DoctorSyncResult struct {
	BackupHead, Head, BaseHead string
	Rechecked                  bool
}

// SyncDoctorBranch performs an approved synchronization outside turn preparation.
// The caller owns admission and exclusion. Failed mutations restore the saved head.
func SyncDoctorBranch(ctx context.Context, o DoctorSyncOptions) (result DoctorSyncResult, err error) {
	if o.Worktree == nil || o.CloneGate == nil || o.CloneRoot == "" || o.CloneRoot != o.Worktree.ClonePath() || o.RecordBackup == nil || o.BeforeHead == "" || o.BaseHead == "" || o.Branch == "" || o.Base == "" {
		return result, errors.New("incomplete approved branch synchronization or clone gate binding")
	}
	preview, snapshots, err := prepareDoctorSync(ctx, o)
	if err != nil {
		return result, err
	}
	result.BackupHead, result.BaseHead = o.BeforeHead, o.BaseHead
	var files []git.ResolvedFile
	if !preview.Clean {
		if o.Advisor == nil {
			return result, errors.New("conflict advisor unavailable")
		}
		req := agent.ConflictResolutionRequest{Branch: o.Branch, Base: o.Base, LocalSHA: o.BeforeHead, TargetSHA: o.BaseHead, TaskInputSHA: o.BeforeHead, BaseInputSHA: o.BaseHead, Files: conflictAdvisorFiles(snapshots)}
		resolution, e := o.Advisor.ProposeConflictResolution(ctx, req)
		if e != nil {
			return result, e
		}
		if e = agent.ValidateRegionConflictResolution(resolution, req); e != nil {
			return result, e
		}
		validated, e := validateCollectedConflictResolution(snapshots, resolution)
		if e != nil {
			return result, e
		}
		for _, v := range validated {
			files = append(files, git.ResolvedFile{Path: v.Path, Format: git.RegionProposalVersion, BranchSHA: o.BeforeHead, BaseSHA: o.BaseHead, ContentLimit: 1 << 20, Validated: v})
		}
	}
	return applyDoctorSync(ctx, o, preview, files, result)
}

// prepareDoctorSync snapshots immutable inputs and records backup in one clone
// acquisition. The advisor is invoked only after this section returns.
func prepareDoctorSync(ctx context.Context, o DoctorSyncOptions) (git.MergePreview, []git.ConflictSnapshot, error) {
	release, err := o.CloneGate.Acquire(ctx, o.CloneRoot)
	if err != nil {
		return git.MergePreview{}, nil, err
	}
	defer release()
	if err = checkDoctorSyncInputs(ctx, o); err != nil {
		return git.MergePreview{}, nil, err
	}
	preview, err := o.Worktree.MergePreview(ctx, o.BaseHead)
	if err != nil {
		return preview, nil, err
	}
	var snapshots []git.ConflictSnapshot
	if !preview.Clean {
		snapshots, err = o.Worktree.CollectConflictSnapshots(ctx, preview.TreeSHA, o.BeforeHead, o.BaseHead, preview.Conflicts, 1<<20)
		if err != nil {
			return preview, nil, err
		}
	}
	if err = o.RecordBackup(ctx, o.BeforeHead); err != nil {
		return preview, nil, err
	}
	return preview, snapshots, nil
}

// checkDoctorSyncInputs is called only while the caller holds the clone gate.
func checkDoctorSyncInputs(ctx context.Context, o DoctorSyncOptions) error {
	w := o.Worktree
	if w.Branch() != o.Branch {
		return errors.New("task branch differs from approved branch")
	}
	actual, err := w.RevParse(ctx, "HEAD", git.RevParseOptions{AbbrevRef: true})
	if err != nil {
		return err
	}
	if actual != o.Branch {
		return errors.New("checked-out branch differs from approved branch")
	}
	head, err := w.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil {
		return err
	}
	base, err := w.RevParse(ctx, o.Base, git.RevParseOptions{})
	if err != nil {
		return err
	}
	if head != o.BeforeHead || base != o.BaseHead {
		return errors.New("branch synchronization inputs drifted")
	}
	changes, err := w.Status(ctx)
	if err != nil {
		return err
	}
	if len(changes) != 0 {
		return git.ErrWorktreeUncommittedChanges
	}
	return nil
}

// applyDoctorSync reacquires once after advisory work and revalidates every
// input before the first write. Commit, push, recheck and rollback stay inside
// this acquisition. Git owns per-command deadlines, including rollback.
func applyDoctorSync(ctx context.Context, o DoctorSyncOptions, preview git.MergePreview, files []git.ResolvedFile, result DoctorSyncResult) (out DoctorSyncResult, err error) {
	release, err := o.CloneGate.Acquire(ctx, o.CloneRoot)
	if err != nil {
		return result, err
	}
	defer release()
	if err = checkDoctorSyncInputs(ctx, o); err != nil {
		return result, err
	}
	w := o.Worktree
	defer func() {
		if err != nil {
			err = errors.Join(err, w.ResetHard(context.WithoutCancel(ctx), o.BeforeHead))
		}
	}()
	if preview.Clean {
		_, err = w.MergeBase(ctx, o.BaseHead, git.MergeOptions{Author: o.Author})
	} else {
		if err = w.WriteResolvedFiles(ctx, o.BaseHead, files); err == nil {
			err = w.StageResolvedFiles(ctx, files)
		}
		if err == nil {
			_, err = w.CompleteMerge(ctx, git.CompleteMergeOptions{Task: o.Task, Mode: "doctor", Author: o.Author, Files: files, ProposalID: o.OperationID, ConfirmedBy: o.Owner, Model: o.Model})
		}
	}
	if err != nil {
		return result, err
	}
	divergence, err := w.Divergence(ctx, o.BaseHead)
	if err != nil {
		return result, err
	}
	changes, err := w.Status(ctx)
	if err != nil {
		return result, err
	}
	if divergence.Behind != 0 || len(changes) != 0 {
		return result, fmt.Errorf("branch synchronization recheck failed")
	}
	result.Head, err = w.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil {
		return result, err
	}
	if err = w.Push(ctx, git.DefaultRemote); err != nil {
		return result, err
	}
	result.Rechecked = true
	return result, nil
}
