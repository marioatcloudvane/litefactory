package turn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/git"
)

// doctorSyncAdvisorBudget leaves time for the merge, recheck and push before
// the repair broker's ten-minute tool deadline. Tests may shorten it.
var doctorSyncAdvisorBudget = 9 * time.Minute

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
			return result, &doctor.RepairConflictError{Category: doctor.ErrRepairAdvisorFailed, Paths: preview.Conflicts, Err: errors.New("conflict advisor unavailable")}
		}
		req := agent.ConflictResolutionRequest{Branch: o.Branch, Base: o.Base, LocalSHA: o.BeforeHead, TargetSHA: o.BaseHead, TaskInputSHA: o.BeforeHead, BaseInputSHA: o.BaseHead, Files: conflictAdvisorFiles(snapshots)}
		advisorCtx, cancel := context.WithTimeout(ctx, doctorSyncAdvisorBudget)
		resolution, e := o.Advisor.ProposeConflictResolution(advisorCtx, req)
		cancel()
		if e != nil {
			category := doctor.ErrRepairAdvisorFailed
			if errors.Is(e, context.DeadlineExceeded) || errors.Is(advisorCtx.Err(), context.DeadlineExceeded) {
				category = doctor.ErrRepairAdvisorTimedOut
			}
			return result, &doctor.RepairConflictError{Category: category, Paths: preview.Conflicts, Err: e}
		}
		if e = agent.ValidateRegionConflictResolution(resolution, req); e != nil {
			return result, &doctor.RepairConflictError{Category: doctor.ErrRepairConfirmationFailed, Paths: preview.Conflicts, Err: e}
		}
		validated, e := validateCollectedConflictResolution(snapshots, resolution)
		if e != nil {
			return result, &doctor.RepairConflictError{Category: doctor.ErrRepairConfirmationFailed, Paths: preview.Conflicts, Err: e}
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
	if err = fastForwardDoctorSync(ctx, o); err != nil {
		return git.MergePreview{}, nil, err
	}
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

// fastForwardDoctorSync advances a clean task branch that is only behind the
// approved head, such as after story merges pushed from scratch worktrees.
// A diverged or dirty worktree is refused unchanged. Called under the clone gate.
func fastForwardDoctorSync(ctx context.Context, o DoctorSyncOptions) error {
	w := o.Worktree
	head, err := w.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil || head == o.BeforeHead {
		return err
	}
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
	divergence, err := w.Divergence(ctx, o.BeforeHead)
	if err != nil {
		return err
	}
	if divergence.Ahead != 0 {
		return errors.New("task branch has local commits absent from the approved head")
	}
	_, err = w.MergeBase(ctx, o.BeforeHead, git.MergeOptions{Author: o.Author, FastForwardOnly: true})
	return err
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
	if head != o.BeforeHead {
		return doctor.ErrRepairBeforeHeadChanged
	}
	// The merge targets the pinned BaseHead, so a base that only moved forward
	// is harmless; a rewritten base no longer contains the approved head.
	if base != o.BaseHead {
		forward, e := w.IsAncestor(ctx, o.BaseHead, base)
		if e != nil {
			return e
		}
		if !forward {
			return doctor.ErrRepairBaseRewritten
		}
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
// this acquisition. Git owns per-command deadlines, including rollback, which
// never runs after a successful push.
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
	pushed := false
	defer func() {
		// Once origin accepted the push, the local branch must keep that head.
		if err != nil && !pushed {
			err = errors.Join(err, w.ResetHard(context.WithoutCancel(ctx), o.BeforeHead))
		}
	}()
	if preview.Clean {
		if _, err = w.MergeBase(ctx, o.BaseHead, git.MergeOptions{Author: o.Author}); err != nil {
			err = fmt.Errorf("%w: %w", doctor.ErrRepairMergeFailed, err)
		}
	} else {
		if err = w.WriteResolvedFiles(ctx, o.BaseHead, files); err == nil {
			err = w.StageResolvedFiles(ctx, files)
		}
		if err == nil {
			_, err = w.CompleteMerge(ctx, git.CompleteMergeOptions{Task: o.Task, Mode: "doctor", Author: o.Author, Files: files, ProposalID: o.OperationID, ConfirmedBy: o.Owner, Model: o.Model})
		}
		if err != nil {
			err = &doctor.RepairConflictError{Category: doctor.ErrRepairConfirmationFailed, Paths: preview.Conflicts, Err: err}
		}
	}
	if err != nil {
		return result, err
	}
	divergence, err := w.Divergence(ctx, o.BaseHead)
	if err != nil {
		return result, fmt.Errorf("%w: %w", doctor.ErrRepairRecheckFailed, err)
	}
	changes, err := w.Status(ctx)
	if err != nil {
		return result, fmt.Errorf("%w: %w", doctor.ErrRepairRecheckFailed, err)
	}
	if divergence.Behind != 0 || len(changes) != 0 {
		return result, fmt.Errorf("%w: branch synchronization recheck failed", doctor.ErrRepairRecheckFailed)
	}
	result.Head, err = w.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil {
		return result, fmt.Errorf("%w: %w", doctor.ErrRepairRecheckFailed, err)
	}
	// Git's per-command timeout bounds the push; a request deadline must not
	// cut it short after the remote may have accepted it.
	if err = w.Push(context.WithoutCancel(ctx), git.DefaultRemote); err != nil {
		return result, fmt.Errorf("%w: %w", doctor.ErrRepairPushRejected, err)
	}
	pushed = true
	result.Rechecked = true
	return result, nil
}
